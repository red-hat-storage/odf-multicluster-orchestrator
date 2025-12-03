package controllers

import (
	"context"
	"fmt"
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type DRPlacementControlReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

func (r *DRPlacementControlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPlacementControl{}).
		Owns(&multiclusterv1alpha1.ProtectedApplicationView{}).
		Complete(r)
}

// Reconcile creates/updates ProtectedApplicationView spec based on DRPlacementControl.
func (r *DRPlacementControlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("DRPlacementControl", req.NamespacedName)
	drpc := &ramenv1alpha1.DRPlacementControl{}
	if err := r.Get(ctx, req.NamespacedName, drpc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("DRPlacementControl resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to get DRPlacementControl", "error", err)
		return ctrl.Result{}, err
	}

	pav := &multiclusterv1alpha1.ProtectedApplicationView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      drpc.Name,
			Namespace: drpc.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pav, func() error {
		if err := controllerutil.SetControllerReference(
			drpc,
			pav,
			r.Scheme,
			controllerutil.WithBlockOwnerDeletion(false),
		); err != nil {
			return err
		}

		pav.Spec.DRPCRef = corev1.ObjectReference{
			APIVersion: ramenAPIVersion,
			Kind:       drpcKind,
			Name:       drpc.Name,
			Namespace:  drpc.Namespace,
			UID:        drpc.UID,
		}

		return nil
	})

	if err != nil {
		logger.Error("Failed to create/update PAV", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info(fmt.Sprintf("ProtectedApplicationView %s was %s successfully", drpc.Name, result))
	return ctrl.Result{}, nil
}
