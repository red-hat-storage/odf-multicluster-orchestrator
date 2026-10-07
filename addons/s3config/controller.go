package s3config

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	S3BucketName              = "BUCKET_NAME"
	S3BucketRegion            = "BUCKET_REGION"
	S3RouteName               = "s3"
	DefaultS3EndpointProtocol = "https"
	DefaultS3Region           = "noobaa"

	// S3ConfigAddonFinalizer is the finalizer added to OBC to ensure cleanup of hub secrets
	S3ConfigAddonFinalizer = "s3config.multicluster.odf.openshift.io/cleanup"
)

// S3ConfigAddonReconciler reconciles OBCs created by S3Configuration
type S3ConfigAddonReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3ConfigAddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to watch only S3Configuration OBCs
	isS3ConfigOBC := func(obj interface{}) bool {
		obc, ok := obj.(*obv1alpha1.ObjectBucketClaim)
		if !ok {
			return false
		}
		// Only watch OBCs with S3ConfigurationNameAnnotationKey
		_, hasAnnotation := obc.Annotations[utils.S3ConfigurationNameAnnotationKey]
		return hasAnnotation
	}

	s3ConfigOBCPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isS3ConfigOBC(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isS3ConfigOBC(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isS3ConfigOBC(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	r.Logger.Info("Setting up S3Config addon controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("s3config_addon_controller").
		Watches(&obv1alpha1.ObjectBucketClaim{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(s3ConfigOBCPredicate)).
		Complete(r)
}

func (r *S3ConfigAddonReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("OBC", req.NamespacedName.String())
	logger.Info("Reconciling S3Configuration OBC")

	var obc obv1alpha1.ObjectBucketClaim
	if err := r.SpokeClient.Get(ctx, req.NamespacedName, &obc); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("OBC not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to retrieve OBC", "error", err)
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !obc.DeletionTimestamp.IsZero() {
		return r.handleOBCDeletion(ctx, logger, &obc)
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&obc, S3ConfigAddonFinalizer) {
		logger.Info("Adding finalizer to OBC")
		if err := r.SpokeClient.Update(ctx, &obc); err != nil {
			logger.Error("Failed to add finalizer to OBC", "error", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if OBC is bound
	if obc.Status.Phase != obv1alpha1.ObjectBucketClaimStatusPhaseBound {
		logger.Info("OBC is not in 'Bound' status, requeuing", "status", obc.Status.Phase)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	s3ConfigName, ok := obc.Annotations[utils.S3ConfigurationNameAnnotationKey]
	if !ok {
		logger.Error("S3Configuration annotation not found on OBC")
		return ctrl.Result{}, fmt.Errorf("s3configuration annotation not found")
	}

	logger.Info("Syncing OBC secret to hub", "s3ConfigName", s3ConfigName)

	if err := r.syncSecretToHub(ctx, &obc, logger); err != nil {
		logger.Error("Failed to sync secret to hub", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully synced OBC secret to hub", "s3ConfigName", s3ConfigName)
	return ctrl.Result{}, nil
}

// handleOBCDeletion handles OBC deletion and cleans up the hub secret
func (r *S3ConfigAddonReconciler) handleOBCDeletion(ctx context.Context, logger *slog.Logger, obc *obv1alpha1.ObjectBucketClaim) (ctrl.Result, error) {
	logger.Info("Handling OBC deletion")

	if !controllerutil.ContainsFinalizer(obc, S3ConfigAddonFinalizer) {
		logger.Info("Finalizer not present, nothing to clean up")
		return ctrl.Result{}, nil
	}

	// Delete secret from hub
	hubSecret := &corev1.Secret{}
	hubSecret.Name = obc.Name
	hubSecret.Namespace = r.SpokeClusterName

	logger.Info("Deleting secret from hub", "secret", hubSecret.Name, "namespace", hubSecret.Namespace)
	if err := r.HubClient.Delete(ctx, hubSecret); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error("Failed to delete hub secret", "error", err)
			return ctrl.Result{}, err
		}
		logger.Info("Hub secret already deleted or not found")
	} else {
		logger.Info("Successfully deleted hub secret")
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(obc, S3ConfigAddonFinalizer)
	if err := r.SpokeClient.Update(ctx, obc); err != nil {
		logger.Error("Failed to remove finalizer from OBC", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Finalizer removed from OBC, cleanup complete")
	return ctrl.Result{}, nil
}

// syncSecretToHub syncs the OBC secret from spoke to hub
func (r *S3ConfigAddonReconciler) syncSecretToHub(ctx context.Context, obc *obv1alpha1.ObjectBucketClaim, logger *slog.Logger) error {
	// Fetch OBC secret from spoke
	var obcSecret corev1.Secret
	if err := r.SpokeClient.Get(ctx, client.ObjectKey{
		Name:      obc.Name,
		Namespace: obc.Namespace,
	}, &obcSecret); err != nil {
		return fmt.Errorf("failed to retrieve OBC secret %q in namespace %q: %w", obc.Name, obc.Namespace, err)
	}

	// Fetch OBC configmap from spoke
	var obcConfigMap corev1.ConfigMap
	if err := r.SpokeClient.Get(ctx, client.ObjectKey{
		Name:      obc.Name,
		Namespace: obc.Namespace,
	}, &obcConfigMap); err != nil {
		return fmt.Errorf("failed to retrieve OBC configmap %q in namespace %q: %w", obc.Name, obc.Namespace, err)
	}

	// Fetch S3 route from spoke
	var s3Route routev1.Route
	if err := r.SpokeClient.Get(ctx, client.ObjectKey{
		Name:      S3RouteName,
		Namespace: obc.Namespace,
	}, &s3Route); err != nil {
		return fmt.Errorf("failed to retrieve S3 route in namespace %q: %w", obc.Namespace, err)
	}

	// Get S3 region, default to "noobaa" if not found
	s3Region := obcConfigMap.Data[S3BucketRegion]
	if s3Region == "" {
		s3Region = DefaultS3Region
	}

	// Build S3 endpoint
	s3Endpoint := fmt.Sprintf("%s://%s", DefaultS3EndpointProtocol, s3Route.Spec.Host)

	// Get S3Configuration name from OBC annotations
	s3ConfigName := obc.Annotations[utils.S3ConfigurationNameAnnotationKey]

	// Create secret on hub - use cluster namespace for organization
	hubSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obc.Name,           // Use OBC name as secret name
			Namespace: r.SpokeClusterName, // Secret goes in cluster namespace on hub
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     obcSecret.Data[utils.AwsAccessKeyId],
			utils.AwsSecretAccessKey: obcSecret.Data[utils.AwsSecretAccessKey],
			utils.S3BucketName:       []byte(obcConfigMap.Data[S3BucketName]),
			utils.S3Region:           []byte(s3Region),
			utils.S3Endpoint:         []byte(s3Endpoint),
		},
	}

	utils.AddLabel(hubSecret, utils.CreatedByLabelKey, utils.S3ConfigAddonName)
	utils.AddAnnotation(hubSecret, utils.S3ConfigurationNameAnnotationKey, s3ConfigName)

	// Create or update secret on hub
	if err := r.HubClient.Create(ctx, hubSecret); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Secret already exists on hub, updating", "secret", hubSecret.Name, "namespace", hubSecret.Namespace)
			if err := r.HubClient.Update(ctx, hubSecret); err != nil {
				return fmt.Errorf("failed to update secret %q in namespace %q on hub: %w", hubSecret.Name, hubSecret.Namespace, err)
			}
			logger.Info("Successfully updated secret on hub", "secret", hubSecret.Name, "namespace", hubSecret.Namespace)
			return nil
		}
		return fmt.Errorf("failed to create secret %q in namespace %q on hub: %w", hubSecret.Name, hubSecret.Namespace, err)
	}

	logger.Info("Successfully created secret on hub", "secret", hubSecret.Name, "namespace", hubSecret.Namespace)
	return nil
}
