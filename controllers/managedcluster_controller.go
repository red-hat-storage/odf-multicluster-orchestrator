package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ManagedClusterReconciler struct {
	Client client.Client
	Logger *slog.Logger
}

const (
	OdfInfoClusterClaimNamespacedName = "odfinfo.odf.openshift.io"
)

func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.Logger.With("ManagedCluster", req.NamespacedName)
	logger.Info("Reconciling ManagedCluster")

	var managedCluster clusterv1.ManagedCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &managedCluster); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error("Failed to get ManagedCluster", "error", err)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.processManagedClusterViews(ctx, managedCluster); err != nil {
		logger.Error("Failed to ensure ManagedClusterView", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled ManagedCluster")

	return ctrl.Result{}, nil
}

func hasRequiredODFKey(mc *clusterv1.ManagedCluster) bool {
	claims := mc.Status.ClusterClaims
	for _, claim := range claims {
		if claim.Name == OdfInfoClusterClaimNamespacedName {
			return true
		}
	}
	return false

}
func (r *ManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up ManagedClusterReconciler with manager")
	managedClusterPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			obj, ok := e.ObjectNew.(*clusterv1.ManagedCluster)
			if !ok {
				return false
			}
			return hasRequiredODFKey(obj)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*clusterv1.ManagedCluster)
			if !ok {
				return false
			}
			return hasRequiredODFKey(obj)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.ManagedCluster{}, builder.WithPredicates(managedClusterPredicate, predicate.ResourceVersionChangedPredicate{})).
		Owns(&viewv1beta1.ManagedClusterView{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *ManagedClusterReconciler) processManagedClusterViews(ctx context.Context, managedCluster clusterv1.ManagedCluster) error {
	resourceType := "ConfigMap"
	odfInfoConfigMapNamespacedName, err := getNamespacedNameForClusterInfo(managedCluster)
	if err != nil {
		return fmt.Errorf("error while getting NamespacedName of the %s. %w", resourceType, err)
	}

	enabled := true
	disabled := false
	mcvOwnerRef := &metav1.OwnerReference{
		APIVersion:         managedCluster.APIVersion,
		Kind:               managedCluster.Kind,
		UID:                managedCluster.UID,
		Name:               managedCluster.Name,
		Controller:         &enabled,
		BlockOwnerDeletion: &disabled,
	}

	mcv, operationResult, err := utils.CreateOrUpdateManagedClusterView(ctx, r.Client, odfInfoConfigMapNamespacedName.Name, odfInfoConfigMapNamespacedName.Namespace, resourceType, managedCluster.Name, mcvOwnerRef)
	if err != nil {
		return fmt.Errorf("failed to create or update ManagedClusterView. %w", err)

	}
	r.Logger.Info(fmt.Sprintf("ManagedClusterView was %s", operationResult), "ManagedClusterView", mcv.Name)

	return nil
}

func getNamespacedNameForClusterInfo(managedCluster clusterv1.ManagedCluster) (types.NamespacedName, error) {
	clusterClaims := managedCluster.Status.ClusterClaims
	for _, claim := range clusterClaims {
		if claim.Name == OdfInfoClusterClaimNamespacedName {
			namespacedName := strings.Split(claim.Value, "/")
			if len(namespacedName) != 2 {
				return types.NamespacedName{}, fmt.Errorf("invalid format for namespaced name claim: expected 'namespace/name', got '%s'", claim.Value)
			}
			return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil
		}
	}

	return types.NamespacedName{}, fmt.Errorf("cannot find ClusterClaim %q in ManagedCluster status", OdfInfoClusterClaimNamespacedName)
}
