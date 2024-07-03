package addons

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// S3SecretReconciler reconciles a MirrorPeer object
type S3SecretReconciler struct {
	Scheme           *runtime.Scheme
	HubCluster       cluster.Cluster
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isOurOBC := func(obj interface{}) bool {
		s3MatchString := os.Getenv("S3_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
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

	s3SecretHubPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	mapSecretToOBC := func(ctx context.Context, obj client.Object) []reconcile.Request {
		if s, ok := obj.(*corev1.Secret); ok {
			if s.Labels[utils.SecretLabelTypeKey] == string(utils.InternalLabel) {
				data := make(map[string][]byte)
				if err := json.Unmarshal(s.Data[utils.SecretDataKey], &data); err != nil {
					r.Logger.Error("Failed to map S3 secret on hub to OBC in spoke cluster. Not requeueing request", "secret", s.Name, "error", err)
					return []reconcile.Request{}
				}
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: string(s.Data[utils.NamespaceKey]),
							Name:      string(data[utils.S3BucketName]),
						},
					},
				}
			}
		}
		return []reconcile.Request{}
	}

	r.Logger.Info("Setting up controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("s3secret_controller").
		Watches(&obv1alpha1.ObjectBucketClaim{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, s3BucketPredicate)).
		WatchesRawSource(source.Kind(r.HubCluster.GetCache(), &corev1.Secret{}), handler.EnqueueRequestsFromMapFunc(mapSecretToOBC),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, s3SecretHubPredicate)).
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

	err = r.syncBlueSecretForS3(ctx, obc.Name, obc.Namespace)
	if err != nil {
		logger.Error("Failed to sync Blue Secret for S3", "OBC", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled OBC and synced Blue Secret")
	return ctrl.Result{}, nil
}
