/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mirrorpeer

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger

	TestEnvFile      string
	CurrentNamespace string
}

const (
	mirrorPeerFinalizer = "hub.multicluster.odf.openshift.io"
	MirrorPeer          = "MirrorPeer"
)

// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/finalizers,verbs=get;create;update;patch;delete

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;delete;deletecollection
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update,resourceNames=odf-client-info
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete;watch,resourceNames=spoke-clusterrole-bindings
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create

// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve

// +kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/finalizers,verbs=update
// +kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch

// +kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;create;update

// +kubebuilder:rbac:groups=ramendr.openshift.io,resources=drclusters,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ramendr.openshift.io,resources=drpolicies,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Reconciling request")
	// Fetch MirrorPeer for given Request
	mirrorPeer := &multiclusterv1alpha1.MirrorPeer{}
	err := r.Get(ctx, req.NamespacedName, mirrorPeer)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error("Failed to get MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	result, reconcileErr := r.reconcilePhases(ctx, logger, mirrorPeer)

	statusErr := r.Client.Status().Update(ctx, mirrorPeer)
	if statusErr != nil {
		logger.Error("Failed to update MirrorPeer status.", "error", statusErr)
	}
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	} else if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return result, nil
}

func (r *MirrorPeerReconciler) reconcilePhases(ctx context.Context, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	mirrorPeer.Status.Phase = multiclusterv1alpha1.Initializing

	vendorType := mirrorPeer.Spec.StorageVendor
	if vendorType == "" {
		vendorType = multiclusterv1alpha1.ODF // default
	}

	vendor, err := storagevendor.New(ctx, r.Client, r.Scheme, logger, r.CurrentNamespace, vendorType)
	if err != nil {
		logger.Error("Failed to create storage vendor", "vendor", vendorType, "error", err)
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Failed
		mirrorPeer.Status.Message = multiclusterv1alpha1.InitializationFailed
		utils.SetValidatedFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonInitializationFailed)
		return ctrl.Result{}, err
	}

	if err = isMirrorPeerValid(ctx, r.Client, r.Logger, mirrorPeer); err != nil {
		logger.Error("MirrorPeer validation failed", "error", err)
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Failed
		mirrorPeer.Status.Message = multiclusterv1alpha1.ValidationFailed
		utils.SetValidatedFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonValidationFailed)
		return ctrl.Result{}, err
	}

	if res, err := vendor.ValidateVersion(ctx, mirrorPeer); err != nil || !res.IsZero() {
		logger.Error("MirrorPeer vendor validation failed", "error", err)
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Failed
		mirrorPeer.Status.Message = multiclusterv1alpha1.ValidationFailed
		utils.SetValidatedFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonInitializationFailed)
		return ctrl.Result{}, err
	}

	utils.SetValidatedTrueCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, multiclusterv1alpha1.MessageMirrorPeerValid, multiclusterv1alpha1.ReasonValidated)

	if !mirrorPeer.GetDeletionTimestamp().IsZero() {
		logger.Info("Deleting MirrorPeer")
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Deleting
		mirrorPeer.Status.Message = multiclusterv1alpha1.DeletionInProgress
		utils.SetDeletedFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, multiclusterv1alpha1.MessageDeletionInProgress, multiclusterv1alpha1.ReasonDeletionInProgress)

		result, err := r.deleteMirrorPeer(ctx, logger, mirrorPeer, vendor)
		if err != nil {
			mirrorPeer.Status.Message = multiclusterv1alpha1.DeletionFailed
			utils.SetDeletedFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonDeletionFailed)
			return result, err
		} else if result.RequeueAfter > 0 {
			return result, err
		}

		logger.Info("MirrorPeer deleted, skipping reconcilation")
		return result, err
	}

	updateRequired := false
	if controllerutil.AddFinalizer(mirrorPeer, mirrorPeerFinalizer) {
		logger.Info("Finalizer not found on MirrorPeer. Adding Finalizer")
		updateRequired = true

	}
	if utils.AddLabel(mirrorPeer, utils.HubRecoveryLabel, "resource") {
		logger.Info("Adding label to mirrorpeer for disaster recovery")
		updateRequired = true
	}

	if updateRequired {
		if err := r.Client.Update(ctx, mirrorPeer); err != nil {
			logger.Error("Failed to update mirror peer", "error", err)
			return ctrl.Result{}, err
		}
		logger.Info("Successfully updated mirrorpeer. Re-queuing request...")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	mirrorPeer.Status.Phase = multiclusterv1alpha1.Configuring
	utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, multiclusterv1alpha1.MessageConfigurationInProgress, multiclusterv1alpha1.ReasonConfigurationInProgress)

	if res, err := vendor.ConfigureStorageForDR(ctx, mirrorPeer); err != nil || !res.IsZero() {
		return res, err
	}

	logger.Info("Successfully reconciled MirrorPeer")
	mirrorPeer.Status.Phase = multiclusterv1alpha1.Ready
	mirrorPeer.Status.Message = multiclusterv1alpha1.MirrorPeerReady
	utils.SetConfiguredTrueCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, multiclusterv1alpha1.MessageConfigurationDone, multiclusterv1alpha1.ReasonConfigurationDone)
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) deleteMirrorPeer(ctx context.Context, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, vendor storagevendor.StorageVendor) (reconcile.Result, error) {
	drpolicyList := &ramenv1alpha1.DRPolicyList{}
	if err := r.Client.List(ctx, drpolicyList); err != nil {
		logger.Error("Failed to delete resources", "error", err)
		return reconcile.Result{}, err
	}

	for i := range drpolicyList.Items {
		drClusters := drpolicyList.Items[i].Spec.DRClusters
		if (mirrorPeer.Spec.Items[0].ClusterName == drClusters[0] &&
			mirrorPeer.Spec.Items[1].ClusterName == drClusters[1]) ||
			(mirrorPeer.Spec.Items[0].ClusterName == drClusters[1] &&
				mirrorPeer.Spec.Items[1].ClusterName == drClusters[0]) {
			return reconcile.Result{}, fmt.Errorf("DRPolicy '%s' exists and requires MirrorPeer '%s'. MirrorPeer can not be deleted",
				drpolicyList.Items[i].Name, mirrorPeer.Name)
		}
	}

	if slices.ContainsFunc(mirrorPeer.GetFinalizers(), func(finalizer string) bool {
		return strings.HasSuffix(finalizer, utils.SpokeMirrorPeerFinalizer)
	}) {
		logger.Info("Waiting for agent to delete resources")
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	if res, err := vendor.CleanupResources(ctx, mirrorPeer); err != nil || !res.IsZero() {
		return reconcile.Result{}, err
	}

	if controllerutil.RemoveFinalizer(mirrorPeer, mirrorPeerFinalizer) {
		if err := r.Client.Update(ctx, mirrorPeer); err != nil {
			logger.Error("Failed to remove finalizer from MirrorPeer", "error", err)
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up controller for MirrorPeer")

	cmToMirrorPeerMapFunc := func(ctx context.Context, object client.Object) []ctrl.Request {
		r.Logger.Debug("Mapping ConfigMap to MirrorPeer", "ConfigMap", client.ObjectKeyFromObject(object))
		var reqs []ctrl.Request
		_, ok := object.(*corev1.ConfigMap)
		if !ok {
			r.Logger.Debug("Unable to cast object into a ConfigMap. Not requeing any requests.")
			return reqs
		}
		var mpList multiclusterv1alpha1.MirrorPeerList
		err := r.Client.List(ctx, &mpList)
		if err != nil {
			r.Logger.Debug("Unable to fetch list of all MirrorPeers. Not requeing any requests.")
			return reqs
		}
		for _, mp := range mpList.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: mp.Name}})
		}
		r.Logger.Info("MirrorPeer reconcile requests generated based on ConfigMap change.", "RequestCount", len(reqs), "Requests", reqs)
		return reqs
	}

	addonToMirrorPeerMapFunc := func(ctx context.Context, object client.Object) []ctrl.Request {
		r.Logger.Debug("Mapping ManagedClusterAddon to MirrorPeer", "ManagedClusterAddon", client.ObjectKeyFromObject(object))
		var reqs []ctrl.Request
		addon, ok := object.(*addonapiv1alpha1.ManagedClusterAddOn)
		if !ok {
			r.Logger.Debug("Unable to cast object into ManagedClusterAddon. Not requeing any requests.")
			return reqs
		}
		for _, owners := range addon.GetOwnerReferences() {
			if owners.Kind == MirrorPeer {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: owners.Name}})
			}
		}
		r.Logger.Info("MirrorPeer reconcile requests generated based on ManagedClusterAddon change.", "RequestCount", len(reqs), "Requests", reqs)
		return reqs
	}

	clusterAddonToMirrorPeerMapFunc := func(ctx context.Context, object client.Object) []ctrl.Request {
		r.Logger.Debug("Mapping ClusterManagementAddOn to MirrorPeer", "ClusterManagementAddOn", client.ObjectKeyFromObject(object))
		var reqs []ctrl.Request
		addon, ok := object.(*addonapiv1alpha1.ClusterManagementAddOn)
		if !ok {
			r.Logger.Debug("Unable to cast object into ClusterManagementAddOn. Not requeing any requests.")
			return reqs
		}
		for _, owners := range addon.GetOwnerReferences() {
			if owners.Kind == MirrorPeer {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: owners.Name}})
			}
		}
		r.Logger.Info("MirrorPeer reconcile requests generated based on ClusterManagementAddOn change.", "RequestCount", len(reqs), "Requests", reqs)
		return reqs
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&addonapiv1alpha1.ManagedClusterAddOn{}, handler.EnqueueRequestsFromMapFunc(addonToMirrorPeerMapFunc),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
				mca, ok := object.(*addonapiv1alpha1.ManagedClusterAddOn)
				if !ok || mca.Name != utils.TokenExchangeName {
					return false
				}
				return true
			}))).
		Watches(&addonapiv1alpha1.ClusterManagementAddOn{}, handler.EnqueueRequestsFromMapFunc(clusterAddonToMirrorPeerMapFunc),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
				cma, ok := object.(*addonapiv1alpha1.ClusterManagementAddOn)
				if !ok || cma.Name != utils.TokenExchangeName {
					return false
				}
				return true
			}))).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(cmToMirrorPeerMapFunc),
			builder.WithPredicates(
				predicate.Or(
					utils.NamePredicate(odf.ClientInfoConfigMapName),
					utils.NamePredicate(utils.RamenHubOperatorConfigName),
				),
				utils.NamespacePredicate(r.CurrentNamespace),
				utils.EventTypePredicate(true, true, true, true),
			)).
		Complete(r)
}

// isMirrorPeerValid validates the MirrorPeer CR
func isMirrorPeerValid(ctx context.Context, client client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	logger.Info("Validating MirrorPeer")
	// MirrorPeer.Spec must be defined
	if err := undefinedMirrorPeerSpec(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec is undefined", "error", err)
		return err
	}
	// MirrorPeer.Spec.Items must be unique
	if err := uniqueSpecItems(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec items are not unique", "error", err)
		return err
	}
	for i := range mirrorPeer.Spec.Items {
		// MirrorPeer.Spec.Items must not have empty fields
		if err := emptySpecItems(mirrorPeer.Spec.Items[i]); err != nil {
			logger.Error("MirrorPeer spec items have empty fields", "error", err)
			return err
		}
		// MirrorPeer.Spec.Items[*].ClusterName must be a valid ManagedCluster
		if err := isManagedCluster(ctx, client, mirrorPeer.Spec.Items[i].ClusterName); err != nil {
			logger.Error("Invalid ManagedCluster", "ClusterName", mirrorPeer.Spec.Items[i].ClusterName, "error", err)
			return err
		}
	}

	logger.Info("All validations for MirrorPeer passed")
	return nil
}
