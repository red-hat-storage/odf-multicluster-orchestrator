package addons

import (
	"context"
	"log/slog"
	"strings"
	"time"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// S3SecretReconciler reconciles a MirrorPeer object
type S3SecretReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger

	testEnvFile      string
	CurrentNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isOurOBC := func(obj interface{}) bool {
		s3MatchString := utils.GetEnv("S3_EXCHANGE_SOURCE_SECRET_STRING_MATCH", r.testEnvFile)
		if s3MatchString == "" {
			s3MatchString = utils.BucketGenerateName
		}
		if obc, ok := obj.(*obv1alpha1.ObjectBucketClaim); ok {
			return strings.Contains(obc.Name, s3MatchString)
		}
		return false
	}

	s3BucketPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isOurOBC(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isOurOBC(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isOurOBC(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	r.Logger.Info("Setting up controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("s3secret_controller").
		Watches(&obv1alpha1.ObjectBucketClaim{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, s3BucketPredicate)).
		Complete(r)
}

func (r *S3SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var obc obv1alpha1.ObjectBucketClaim
	logger := r.Logger.With("OBC", req.NamespacedName.String())
	logger.Info("Reconciling OBC")
	err = r.SpokeClient.Get(ctx, req.NamespacedName, &obc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("OBC not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to retrieve OBC", "error", err)
		return ctrl.Result{}, err
	}

	if obc.Status.Phase != obv1alpha1.ObjectBucketClaimStatusPhaseBound {
		logger.Info("OBC is not in 'Bound' status, requeuing", "st,atus", obc.Status.Phase)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	if _, ok := obc.Annotations[utils.MirrorPeerNameAnnotationKey]; !ok {
		logger.Error("Failed to find MirrorPeer name on OBC")
		return ctrl.Result{}, err
	}

	if _, ok := obc.Annotations[OBCTypeAnnotationKey]; !ok {
		logger.Error("Failed to find OBC type on OBC")
		return ctrl.Result{}, err
	}

	mirrorPeerName := obc.Annotations[utils.MirrorPeerNameAnnotationKey]
	obcType := obc.Annotations[OBCTypeAnnotationKey]

	err = r.syncBlueSecretForS3(ctx, obc.Name, obc.Namespace, mirrorPeerName, obcType)
	if err != nil {
		logger.Error("Failed to sync Blue Secret for S3", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled OBC and synced Blue Secret")
	return ctrl.Result{}, nil
}
