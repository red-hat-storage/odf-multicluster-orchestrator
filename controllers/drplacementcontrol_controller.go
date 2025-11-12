package controllers

import (
	"context"
	"strings"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type DRPCReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DRPCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// findDRPCsForApplicationSet returns DRPCs to reconcile when ApplicationSet changes
func (r *DRPCReconciler) findDRPCsForApplicationSet(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	appSet, ok := obj.(*argov1alpha1.ApplicationSet)
	if !ok {
		return nil
	}

	placementName := r.extractPlacementFromApplicationSet(appSet)
	if placementName == "" {
		return nil
	}

	drpcList := &ramenv1alpha1.DRPlacementControlList{}
	if err := r.List(ctx, drpcList, client.InNamespace(appSet.Namespace)); err != nil {
		return nil
	}

	for _, drpc := range drpcList.Items {
		if drpc.Spec.PlacementRef.Name == placementName &&
			drpc.Spec.PlacementRef.Kind == "Placement" {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      drpc.Name,
					Namespace: drpc.Namespace,
				},
			}}
		}
	}

	return nil
}

func (r *DRPCReconciler) extractPlacementFromApplicationSet(
	appSet *argov1alpha1.ApplicationSet,
) string {
	for _, gen := range appSet.Spec.Generators {
		if gen.ClusterDecisionResource != nil {
			for key, value := range gen.ClusterDecisionResource.LabelSelector.MatchLabels {
				if strings.Contains(key, "cluster.open-cluster-management.io/placement") {
					return value
				}
			}
		}
	}
	return ""
}

func (r *DRPCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPlacementControl{}).
		Owns(&multiclusterv1alpha1.ProtectedApplicationView{}).
		Watches(
			&argov1alpha1.ApplicationSet{},
			handler.EnqueueRequestsFromMapFunc(r.findDRPCsForApplicationSet),
		).
		Complete(r)
}
