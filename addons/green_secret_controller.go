package addons

import (
	"context"
	"fmt"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// GreenSecretReconciler reconciles a MirrorPeer object
type GreenSecretReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
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

	return ctrl.NewControllerManagedBy(mgr).
		Named("greensecret_controller").
		Watches(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, greenSecretPredicate)).
		Complete(r)
}

func (r *GreenSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var greenSecret corev1.Secret

	klog.Infof("Reconciling green secret", "secret", req.NamespacedName.String())
	err = r.HubClient.Get(ctx, req.NamespacedName, &greenSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Could not find secret. Ignoring since it must have been deleted")
			return ctrl.Result{}, nil
		}
		klog.Errorf("Failed to get secret.", err)
		return ctrl.Result{}, err
	}

	if err = validateGreenSecret(greenSecret); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to validate secret %q", greenSecret.Name)
	}

	err = r.syncGreenSecretForRook(ctx, greenSecret)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
