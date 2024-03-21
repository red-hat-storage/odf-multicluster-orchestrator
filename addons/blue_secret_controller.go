package addons

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// BlueSecretReconciler reconciles a MirrorPeer object
type BlueSecretReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
}

// SetupWithManager sets up the controller with the Manager.
func (r *BlueSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isBlueSecret := func(obj interface{}) bool {
		return getBlueSecretFilterForRook(obj)
	}

	blueSecretPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isBlueSecret(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isBlueSecret(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isBlueSecret(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("bluesecret_controller").
		Watches(&corev1.Secret{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, blueSecretPredicate)).
		Complete(r)
}

func (r *BlueSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var secret corev1.Secret

	klog.Info("Reconciling blue secret", "secret", req.NamespacedName.String())
	err = r.SpokeClient.Get(ctx, req.NamespacedName, &secret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Could not find secret. Ignoring since it must have been deleted")
			return ctrl.Result{}, nil
		}
		klog.Error("Failed to get secret.", err)
		return ctrl.Result{}, err
	}

	err = r.syncBlueSecretForRook(ctx, secret)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
