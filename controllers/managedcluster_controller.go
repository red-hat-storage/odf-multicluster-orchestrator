package controllers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ManagedClusterReconciler struct {
	Client           client.Client
	Logger           *slog.Logger
	testEnvFile      string
	CurrentNamespace string
}

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

func (r *ManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up ManagedClusterReconciler with manager")
	managedClusterPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			obj, ok := e.ObjectNew.(*clusterv1.ManagedCluster)
			if !ok {
				return false
			}
			return utils.HasRequiredODFKey(obj)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*clusterv1.ManagedCluster)
			if !ok {
				return false
			}
			return utils.HasRequiredODFKey(obj)
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
	odfInfoConfigMapNamespacedName, err := utils.GetNamespacedNameForClusterInfo(managedCluster)
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

	mcvName := utils.GetManagedClusterViewName(managedCluster.Name)
	mcv, operationResult, err := utils.CreateOrUpdateManagedClusterView(ctx, r.Client, mcvName, odfInfoConfigMapNamespacedName.Name, odfInfoConfigMapNamespacedName.Namespace, resourceType, managedCluster.Name, mcvOwnerRef)
	if err != nil {
		return fmt.Errorf("failed to create or update ManagedClusterView. %w", err)

	}
	r.Logger.Info(fmt.Sprintf("ManagedClusterView was %s", operationResult), "ManagedClusterView", mcv.Name)

	// Create ManagedClusterView for token-exchange-agent deployment running on managedclusters.
	resourceType = "Deployment"
	deploymentNamespace := odfInfoConfigMapNamespacedName.Namespace
	deploymentName := utils.TokenExchangeDeployment
	tokenExchangeMCVName := utils.GetTokenExchangeManagedClusterViewName(managedCluster.Name)

	mcvDeployment, operationResult, err := utils.CreateOrUpdateManagedClusterView(ctx, r.Client, tokenExchangeMCVName, deploymentName, deploymentNamespace, resourceType, managedCluster.Name, mcvOwnerRef)
	if err != nil {
		return fmt.Errorf("failed to create or update ManagedClusterView. %w", err)

	}
	r.Logger.Info(fmt.Sprintf("ManagedClusterView was %s", operationResult), "ManagedClusterView", mcvDeployment.Name)

	return nil
}
