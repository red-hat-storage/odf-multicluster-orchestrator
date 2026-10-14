package managedcluster

import (
	"context"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
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
	Scheme           *runtime.Scheme
	Logger           *slog.Logger
	TestEnvFile      string
	CurrentNamespace string
}

// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=get;list;watch;create;update;patch;delete

func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.Logger.With("ManagedCluster", req.NamespacedName)
	logger.Info("Reconciling ManagedCluster")

	managedCluster := &clusterv1.ManagedCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, managedCluster); err != nil {
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
			return utils.HasRequiredStorageVendorKey(obj)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*clusterv1.ManagedCluster)
			if !ok {
				return false
			}
			return utils.HasRequiredStorageVendorKey(obj)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.ManagedCluster{}, builder.WithPredicates(managedClusterPredicate, predicate.ResourceVersionChangedPredicate{})).
		Owns(&viewv1beta1.ManagedClusterView{}).
		Complete(r)
}

func (r *ManagedClusterReconciler) processManagedClusterViews(ctx context.Context, managedCluster *clusterv1.ManagedCluster) error {
	// Get all storage vendor ConfigMaps for this ManagedCluster
	vendorConfigMaps := utils.GetAllStorageVendorConfigMaps(managedCluster)

	// If no storage vendor claims found, return
	if len(vendorConfigMaps) == 0 {
		return fmt.Errorf("no storage vendor cluster claims found in ManagedCluster %s", managedCluster.Name)
	}

	// Create ManagedClusterViews for each storage vendor ConfigMap
	for claimName, configMapNamespacedName := range vendorConfigMaps {
		var mcvName string

		// Determine the MCV name based on the vendor
		switch claimName {
		case utils.OdfInfoClusterClaimNamespacedName:
			mcvName = utils.GetODFManagedClusterViewName(managedCluster.Name)
		case utils.IbmScaleClusterClaimNamespacedName:
			mcvName = utils.GetIbmScaleManagedClusterViewName(managedCluster.Name)
		default:
			r.Logger.Info("Unknown storage vendor claim, Skipping", "ClaimName", claimName)
			continue
		}

		// Create ManagedClusterView for the storage vendor ConfigMap
		mcv, operationResult, err := utils.CreateOrUpdateManagedClusterView(
			ctx,
			r.Client,
			r.Scheme,
			managedCluster,
			mcvName,
			configMapNamespacedName.Name,
			configMapNamespacedName.Namespace,
			"ConfigMap",
		)
		if err != nil {
			return fmt.Errorf("failed to create or update ManagedClusterView for %s: %w", claimName, err)
		}
		r.Logger.Info(fmt.Sprintf("ManagedClusterView was %s", operationResult), "ManagedClusterView", mcv.Name, "Vendor", claimName)

		// Cleanup ManagedClusterView for token-exchange-agent deployment (only for ODF)
		if claimName == utils.OdfInfoClusterClaimNamespacedName {
			if err := r.cleanupTokenExchangeMCV(ctx, managedCluster.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanupTokenExchangeMCV deletes the tokenexchange MCV
func (r *ManagedClusterReconciler) cleanupTokenExchangeMCV(ctx context.Context, clusterName string) error {

	// Delete tokenexchange MCV if it exists
	tokenExchangeMCVName := utils.GetTokenExchangeManagedClusterViewName(clusterName)
	tokenExchangeMCV := &viewv1beta1.ManagedClusterView{}
	tokenExchangeMCV.Name = tokenExchangeMCVName
	tokenExchangeMCV.Namespace = clusterName

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(tokenExchangeMCV), tokenExchangeMCV)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Already deleted, nothing to do
			return nil
		}
		return fmt.Errorf("failed to get tokenexchange ManagedClusterView: %w", err)
	}

	// MCV exists, delete it
	if err := r.Client.Delete(ctx, tokenExchangeMCV); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete tokenexchange ManagedClusterView: %w", err)
		}
	}

	r.Logger.Info("Deleted tokenexchange ManagedClusterView", "ManagedClusterView", tokenExchangeMCVName)
	return nil
}
