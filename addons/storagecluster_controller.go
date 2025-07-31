package addons

import (
	"context"
	"fmt"
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageClusterReconciler reconciles a MirrorPeer object
type StorageClusterReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger

	CurrentNamespace     string
	HubOperatorNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	scToMirrorPeerMapFunc := func(ctx context.Context, obj client.Object) []ctrl.Request {
		reqs := []ctrl.Request{}
		var mirrorPeerList multiclusterv1alpha1.MirrorPeerList
		if err := r.HubClient.List(ctx, &mirrorPeerList); err != nil {
			r.Logger.Error("Unable to reconcile MirrorPeer based on storagecluster annotation changes.", "error", err)
			return reqs
		}
		for _, mirrorpeer := range mirrorPeerList.Items {
			if mirrorpeer.Status.Phase == multiclusterv1alpha1.IncompatibleVersion {
				continue
			}
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: mirrorpeer.Name}})
		}
		return reqs
	}

	scPredicate := predicate.And(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{})

	r.Logger.Info("Setting up storagecluster controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("storagecluster_controller").
		Watches(&ocsv1.StorageCluster{}, handler.EnqueueRequestsFromMapFunc(scToMirrorPeerMapFunc),
			builder.WithPredicates(scPredicate)).
		Complete(r)
}

func (r *StorageClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Running StorageCluster reconciler on spoke cluster")

	mirrorPeer, err := utils.FetchMirrorPeerByName(ctx, r.HubClient, req.Name)
	if err != nil {
		logger.Error("Failed to fetch  mirrorpeer", "MirrorPeer", req.Name)
		return ctrl.Result{}, err
	}

	hasStorageClientRef, err := utils.IsStorageClientType(ctx, r.HubClient, *mirrorPeer, r.HubOperatorNamespace)
	logger.Info("MirrorPeer has client reference?", "True/False", hasStorageClientRef)

	if err != nil {
		logger.Error("Failed to check if storage client ref exists", "error", err)
		return ctrl.Result{}, err
	}

	var scr *multiclusterv1alpha1.StorageClusterRef
	if hasStorageClientRef {
		sc, err := utils.GetStorageClusterFromCurrentNamespace(ctx, r.SpokeClient, r.CurrentNamespace)
		if err != nil {
			logger.Error("Failed to fetch StorageCluster for given namespace", "Namespace", r.CurrentNamespace)
			return ctrl.Result{}, err
		}
		scr = &multiclusterv1alpha1.StorageClusterRef{
			Name:      sc.Name,
			Namespace: sc.Namespace,
		}
	} else {
		scr, err = utils.GetCurrentStorageClusterRef(mirrorPeer, r.SpokeClusterName)
		if err != nil {
			logger.Error("Failed to get current storage cluster ref", "error", err)
			return ctrl.Result{}, err
		}
	}

	// Verify if all the mirrorpeers have the same keyType, if not default to "aes" type
	var sc ocsv1.StorageCluster
	err = r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      scr.Name,
		Namespace: scr.Namespace,
	}, &sc)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("could not find StorageCluster %v in namespace %v: %w", sc.Name, sc.Namespace, err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to retrieve StorageCluster %v in namespace %v: %w", sc.Name, sc.Namespace, err)
	}

	if err = utils.SetKeyTypeOnStorageCluster(ctx, logger, r.SpokeClient, r.HubClient, &sc); err != nil {
		logger.Error("Failed to set keyType annotation on storagecluster", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled StorageCluster")
	return ctrl.Result{}, nil
}
