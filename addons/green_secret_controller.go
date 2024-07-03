package addons

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// GreenSecretReconciler reconciles a MirrorPeer object
type GreenSecretReconciler struct {
	Scheme           *runtime.Scheme
	HubCluster       cluster.Cluster
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *GreenSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isGreenSecret := func(obj interface{}) bool {
		metaObj, has := obj.(metav1.Object)
		if has {
			return metaObj.GetLabels()[utils.SecretLabelTypeKey] == string(utils.DestinationLabel)
		}
		return false
	}

	greenSecretPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isGreenSecret(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isGreenSecret(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isGreenSecret(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	greebSecretSpokePredicate := predicate.Funcs{
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

	mapSecretToGreenSecret := func(ctx context.Context, obj client.Object) []reconcile.Request {
		if s, ok := obj.(*corev1.Secret); ok {
			if s.Labels[utils.CreatedByLabelKey] == setup.TokenExchangeName {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: r.SpokeClusterName,
							Name:      s.Name,
						},
					},
				}
			}
		}
		return []reconcile.Request{}
	}

	r.Logger.Info("Setting up controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("greensecret_controller").
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapSecretToGreenSecret),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, greebSecretSpokePredicate)).
		WatchesRawSource(source.Kind(r.HubCluster.GetCache(), &corev1.Secret{}), &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, greenSecretPredicate)).
		Complete(r)
}

func (r *GreenSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var greenSecret corev1.Secret
	logger := r.Logger.With("secret", req.NamespacedName.String())

	logger.Info("Reconciling green secret")
	err = r.HubClient.Get(ctx, req.NamespacedName, &greenSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Green secret not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to retrieve green secret", "error", err)
		return ctrl.Result{}, err
	}

	if err = validateGreenSecret(greenSecret); err != nil {
		logger.Error("Validation failed for green secret", "error", err)
		return ctrl.Result{}, fmt.Errorf("failed to validate green secret %q: %v", greenSecret.Name, err)
	}

	err = r.syncGreenSecretForRook(ctx, greenSecret)
	if err != nil {
		logger.Error("Failed to sync green secret for Rook", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled and synced green secret")
	return ctrl.Result{}, nil
}
