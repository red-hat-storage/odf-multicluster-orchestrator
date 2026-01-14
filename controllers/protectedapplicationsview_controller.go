package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1beta1 "sigs.k8s.io/application/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	labelAppType                     = "app.kubernetes.io/type"
	labelDRPolicy                    = "ramendr.openshift.io/dr-policy"
	placementLabel                   = "cluster.open-cluster-management.io/placement"
	applicationSetKind               = "ApplicationSet"
	subscriptionKind                 = "Subscription"
	drpcKind                         = "DRPlacementControl"
	placementKind                    = "Placement"
	requeueAfterSeconds              = 30 * time.Second
	subscriptionAnnotation           = "apps.open-cluster-management.io/subscriptions"
	ramenAPIVersion                  = "ramendr.openshift.io/v1alpha1"
	drPolicyKind                     = "DRPolicy"
	subscriptionAPIVersion           = "apps.open-cluster-management.io/v1"
	clusterDecisionResourceIndexName = "spec.generators.clusterDecisionResource.placement"
	placementIndexName               = "spec.placementRef.name"
)

type ProtectedApplicationViewReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

type ApplicationResult struct {
	Type                 multiclusterv1alpha1.ApplicationType
	AppRef               corev1.ObjectReference
	SubscriptionRefs     []corev1.ObjectReference
	DestinationNamespace string
}

func (r *ProtectedApplicationViewReconciler) drpcStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDRPC, oldOk := e.ObjectOld.(*ramenv1alpha1.DRPlacementControl)
			newDRPC, newOk := e.ObjectNew.(*ramenv1alpha1.DRPlacementControl)
			if !oldOk || !newOk {
				return false
			}
			if oldDRPC.Generation != newDRPC.Generation {
				return true
			}
			return r.drpcStatusChanged(oldDRPC, newDRPC)
		},
	}
}

func (r *ProtectedApplicationViewReconciler) drpcStatusChanged(old, new *ramenv1alpha1.DRPlacementControl) bool {
	if old.Status.Phase != new.Status.Phase {
		return true
	}
	if !old.Status.LastGroupSyncTime.Equal(new.Status.LastGroupSyncTime) {
		return true
	}
	if len(old.Status.Conditions) != len(new.Status.Conditions) {
		return true
	}
	return old.Status.PreferredDecision.ClusterName != new.Status.PreferredDecision.ClusterName
}

// getPrimaryClusterFromDRPC determines the actual primary cluster based on DRPC phase and spec.
// This matches the logic used by odf-console to display the correct primary cluster in the UI.
func (r *ProtectedApplicationViewReconciler) getPrimaryClusterFromDRPC(drpc *ramenv1alpha1.DRPlacementControl) string {
	if drpc.Status.Phase == "" {
		return ""
	}

	switch drpc.Status.Phase {
	case ramenv1alpha1.FailedOver:
		// After failover, primary is the failover cluster
		return drpc.Spec.FailoverCluster

	case ramenv1alpha1.Relocated:
		// After relocate, primary is back to preferred cluster
		return drpc.Spec.PreferredCluster

	default:
		// For other states (Deploying, Deployed, Initiating, etc.)
		// Use PreferredDecision as the source of truth
		return drpc.Status.PreferredDecision.ClusterName
	}
}

func (r *ProtectedApplicationViewReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	if err := mgr.GetCache().IndexField(ctx, &argov1alpha1.ApplicationSet{},
		clusterDecisionResourceIndexName,
		func(o client.Object) []string {
			appSet, ok := o.(*argov1alpha1.ApplicationSet)
			if !ok {
				return nil
			}
			var placements []string

			for _, gen := range appSet.Spec.Generators {
				if gen.ClusterDecisionResource != nil {
					for key, value := range gen.ClusterDecisionResource.LabelSelector.MatchLabels {
						if strings.Contains(key, placementLabel) {
							placements = append(placements, value)
						}
					}
				}
			}

			return placements
		}); err != nil {
		return fmt.Errorf("failed to setup ApplicationSet index: %w", err)
	}

	if err := mgr.GetCache().IndexField(ctx, &ramenv1alpha1.DRPlacementControl{},
		placementIndexName,
		func(o client.Object) []string {
			drpc, ok := o.(*ramenv1alpha1.DRPlacementControl)
			if !ok {
				return nil
			}
			return []string{drpc.Spec.PlacementRef.Name}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.ProtectedApplicationView{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&ramenv1alpha1.DRPlacementControl{},
			handler.EnqueueRequestsFromMapFunc(r.findPAVsForDRPC),
			builder.WithPredicates(r.drpcStatusChangedPredicate())).
		Watches(&placementv1beta1.Placement{},
			handler.EnqueueRequestsFromMapFunc(r.findPAVsForPlacement)).
		Watches(&placementv1beta1.PlacementDecision{},
			handler.EnqueueRequestsFromMapFunc(r.findPAVsForPlacementDecision)).
		Complete(r)
}

func (r *ProtectedApplicationViewReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("PAV", req.Name, "Namespace", req.Namespace)

	pav := &multiclusterv1alpha1.ProtectedApplicationView{}
	if err := r.Get(ctx, req.NamespacedName, pav); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !pav.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	drpc := &ramenv1alpha1.DRPlacementControl{}
	drpcKey := types.NamespacedName{
		Name:      pav.Spec.DRPCRef.Name,
		Namespace: pav.Spec.DRPCRef.Namespace,
	}
	if err := r.Get(ctx, drpcKey, drpc); err != nil {
		logger.Error("Failed to get DRPlacementControl", "error", err)
		return ctrl.Result{RequeueAfter: requeueAfterSeconds}, err
	}
	placement := &placementv1beta1.Placement{}
	placementKey := types.NamespacedName{
		Name:      drpc.Spec.PlacementRef.Name,
		Namespace: drpc.Spec.PlacementRef.Namespace,
	}
	if err := r.Get(ctx, placementKey, placement); err != nil {
		logger.Error("Failed to get Placement", "error", err)
		return ctrl.Result{RequeueAfter: requeueAfterSeconds}, err
	}

	appResult, err := r.findApplicationForDRPC(ctx, drpc, placement)
	if err != nil {
		return ctrl.Result{}, err
	}

	if pav.Labels == nil {
		pav.Labels = make(map[string]string)
	}
	pav.Labels[labelAppType] = string(appResult.Type)
	pav.Labels[labelDRPolicy] = drpc.Spec.DRPolicyRef.Name

	if err := r.Update(ctx, pav); err != nil {
		logger.Error("Failed to update PAV labels", "error", err)
		return ctrl.Result{}, err
	}

	return r.updatePAVStatus(ctx, pav, drpc, placement, appResult)
}

// updatePAVStatus updates the status of the ProtectedApplicationView based on the DRPlacementControl and Placement information
func (r *ProtectedApplicationViewReconciler) updatePAVStatus(
	ctx context.Context,
	pav *multiclusterv1alpha1.ProtectedApplicationView,
	drpc *ramenv1alpha1.DRPlacementControl,
	placement *placementv1beta1.Placement,
	appResult ApplicationResult,
) (ctrl.Result, error) {
	logger := r.Logger.With("ProtectedApplicationView", pav.Name)
	selectedClusters, err := r.getPlacementDecision(ctx, placement)
	if err != nil {
		logger.Error("Failed to get PlacementDecision", "error", err)
		selectedClusters = []string{}
	}

	drPolicy := &ramenv1alpha1.DRPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: drpc.Spec.DRPolicyRef.Name}, drPolicy); err != nil {
		logger.Error("Failed to get DRPolicy", "error", err)
		// Continue with empty drClusters - partial status is better than no status
	}

	primaryCluster := r.getPrimaryClusterFromDRPC(drpc)

	pav.Status = multiclusterv1alpha1.ProtectedApplicationViewStatus{
		ApplicationInfo: multiclusterv1alpha1.ApplicationInfo{
			Type:                 appResult.Type,
			ApplicationRef:       appResult.AppRef,
			DestinationNamespace: appResult.DestinationNamespace,
		},
		PlacementInfo: &multiclusterv1alpha1.PlacementInfo{
			PlacementRef: corev1.ObjectReference{
				APIVersion: placementv1beta1.SchemeGroupVersion.String(),
				Kind:       placementKind,
				Name:       placement.Name,
				Namespace:  placement.Namespace,
			},
			SelectedClusters: selectedClusters,
		},
		DRInfo: multiclusterv1alpha1.DRInfo{
			DRPolicyRef: corev1.ObjectReference{
				APIVersion: ramenAPIVersion,
				Kind:       drPolicyKind,
				Name:       drpc.Spec.DRPolicyRef.Name,
			},
			DRClusters:          drPolicy.Spec.DRClusters,
			PrimaryCluster:      primaryCluster,
			ProtectedNamespaces: ptr.Deref(drpc.Spec.ProtectedNamespaces, []string{}),
			Status: multiclusterv1alpha1.DRStatusInfo{
				Phase:             drpc.Status.Phase,
				LastGroupSyncTime: drpc.Status.LastGroupSyncTime,
				Conditions:        drpc.Status.Conditions,
			},
		},
		ObservedGeneration: pav.Generation,
		LastSyncTime:       ptr.To(metav1.Now()),
	}

	if appResult.Type == multiclusterv1alpha1.SubscriptionType && len(appResult.SubscriptionRefs) > 0 {
		pav.Status.ApplicationInfo.SubscriptionInfo = &multiclusterv1alpha1.SubscriptionInfo{
			SubscriptionRefs: appResult.SubscriptionRefs,
		}
	}

	if err := r.Status().Update(ctx, pav); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Updated ProtectedApplicationView status", "name", pav.Name, "type", appResult.Type)
	return ctrl.Result{}, nil
}

// findApplicationForDRPC reverse maps the DRPlacementControl to its application (ApplicationSet, Subscription, or Discovered)
func (r *ProtectedApplicationViewReconciler) findApplicationForDRPC(
	ctx context.Context,
	drpc *ramenv1alpha1.DRPlacementControl,
	placement *placementv1beta1.Placement,
) (ApplicationResult, error) {
	logger := r.Logger.With("DRPC", drpc.Name, "Namespace", drpc.Namespace)

	appSet, err := r.findApplicationSetByPlacement(ctx, drpc, placement)
	if err != nil {
		logger.Error("Failed to search for ApplicationSet", "error", err)
		return ApplicationResult{}, err
	}

	if appSet != nil {
		logger.Info("Found ApplicationSet for DRPC", "applicationSet", appSet.Name)
		return ApplicationResult{
			Type: multiclusterv1alpha1.ApplicationSetType,
			AppRef: corev1.ObjectReference{
				APIVersion: argov1alpha1.SchemeGroupVersion.String(),
				Kind:       applicationSetKind,
				Name:       appSet.Name,
				Namespace:  appSet.Namespace,
				UID:        appSet.UID,
			},
			DestinationNamespace: appSet.Spec.Template.Spec.Destination.Namespace,
		}, nil
	}

	subscriptions, err := r.findSubscriptionsByPlacement(ctx, placement)
	if err != nil {
		logger.Error("Failed to search for Subscriptions", "error", err)
		return ApplicationResult{}, err
	}

	if len(subscriptions) > 0 {
		app, err := r.findParentApplication(ctx, subscriptions)
		if err != nil {
			logger.Error("Failed to find parent Application", "error", err)
			return ApplicationResult{}, err
		}

		result := ApplicationResult{Type: multiclusterv1alpha1.SubscriptionType}

		for _, sub := range subscriptions {
			result.SubscriptionRefs = append(result.SubscriptionRefs, corev1.ObjectReference{
				APIVersion: subscriptionAPIVersion,
				Kind:       subscriptionKind,
				Name:       sub.Name,
				Namespace:  sub.Namespace,
			})
		}

		if app != nil {
			logger.Info("Found Subscription-based application for DRPC",
				"application", app.Name, "subscriptionCount", len(subscriptions))
			result.AppRef = corev1.ObjectReference{
				APIVersion: "app.k8s.io/v1beta1",
				Kind:       "Application",
				Name:       app.Name,
				Namespace:  app.Namespace,
				UID:        app.UID,
			}
		}

		return result, nil
	}

	logger.Info("No ApplicationSet or Subscription found, using Discovered type for DRPC")
	return ApplicationResult{
		Type: multiclusterv1alpha1.DiscoveredType,
		AppRef: corev1.ObjectReference{
			APIVersion: ramenAPIVersion,
			Kind:       drpcKind,
			Name:       drpc.Name,
			Namespace:  drpc.Namespace,
		},
	}, nil
}

// findApplicationSetByPlacement finds the ApplicationSet associated with the given Placement
func (r *ProtectedApplicationViewReconciler) findApplicationSetByPlacement(
	ctx context.Context,
	drpc *ramenv1alpha1.DRPlacementControl,
	placement *placementv1beta1.Placement,
) (*argov1alpha1.ApplicationSet, error) {
	logger := r.Logger.With("Placement", placement.Name, "DRPC", drpc.Name)

	appSetList := &argov1alpha1.ApplicationSetList{}
	if err := r.List(ctx, appSetList,
		client.InNamespace(placement.Namespace),
		client.MatchingFields{clusterDecisionResourceIndexName: placement.Name},
	); err != nil {
		logger.Error("Failed to list ApplicationSets", "error", err)
		return nil, err
	}

	if len(appSetList.Items) == 0 {
		return nil, nil
	}

	for i := range appSetList.Items {
		appSet := &appSetList.Items[i]
		if strings.HasPrefix(drpc.Name, appSet.Name) {
			logger.Info("Found ApplicationSet matching naming convention",
				"applicationSet", appSet.Name)
			return appSet, nil
		}
	}

	// No match found - log all names for debugging
	names := make([]string, len(appSetList.Items))
	for i, item := range appSetList.Items {
		names[i] = item.Name
	}

	logger.Warn("No ApplicationSet matches naming convention, returning first",
		"drpcName", drpc.Name,
		"selectedAppSet", appSetList.Items[0].Name,
		"allAppSets", names)

	return &appSetList.Items[0], nil
}

func (r *ProtectedApplicationViewReconciler) findPAVsForDRPC(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	drpc, ok := obj.(*ramenv1alpha1.DRPlacementControl)
	if !ok {
		return nil
	}

	// PAV has same name/namespace as DRPC
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      drpc.Name,
			Namespace: drpc.Namespace,
		},
	}}
}

// findSubscriptionsByPlacement discovers Subscriptions referencing a Placement.
// Note: Subscription API (apps.open-cluster-management.io/v1) is deprecated.
// This function exists for backward compatibility during the migration period.
// New applications should use ApplicationSet (argoproj.io/v1alpha1) instead.
func (r *ProtectedApplicationViewReconciler) findSubscriptionsByPlacement(
	ctx context.Context,
	placement *placementv1beta1.Placement,
) ([]*appsubv1.Subscription, error) {
	logger := r.Logger.With("Placement", placement.Name)

	subList := &appsubv1.SubscriptionList{}
	if err := r.List(ctx, subList, client.InNamespace(placement.Namespace)); err != nil {
		logger.Error("Failed to list Subscriptions", "error", err, "namespace", placement.Namespace)
		return nil, err
	}

	var matchingSubs []*appsubv1.Subscription
	for i := range subList.Items {
		sub := &subList.Items[i]
		if sub.Spec.Placement != nil &&
			sub.Spec.Placement.PlacementRef != nil &&
			sub.Spec.Placement.PlacementRef.Name == placement.Name &&
			sub.Spec.Placement.PlacementRef.Kind == placementKind {
			matchingSubs = append(matchingSubs, sub)
			logger.Info("Found Subscription for Placement",
				"subscription", sub.Name,
				"placement", placement.Name)
		}
	}

	return matchingSubs, nil
}

// findParentApplication finds the parent Application of the given Subscriptions.
// Returns nil, nil if no parent Application exists (valid during creation/deletion).
// Returns error only for API failures that should trigger requeue.
func (r *ProtectedApplicationViewReconciler) findParentApplication(
	ctx context.Context,
	subscriptions []*appsubv1.Subscription,
) (*appv1beta1.Application, error) {
	logger := r.Logger.With("Subscriptions", fmt.Sprintf("%d subscriptions", len(subscriptions)))

	if len(subscriptions) == 0 {
		return nil, nil
	}

	firstSub := subscriptions[0]

	appList := &appv1beta1.ApplicationList{}
	if err := r.List(ctx, appList, client.InNamespace(firstSub.Namespace)); err != nil {
		logger.Error("Failed to list Applications", "error", err, "namespace", firstSub.Namespace)
		return nil, err
	}

	for i := range appList.Items {
		app := &appList.Items[i]
		if subAnnotation, exists := app.Annotations[subscriptionAnnotation]; exists {
			subRefs := strings.Split(subAnnotation, ",")
			for _, subRef := range subRefs {
				parts := strings.Split(strings.TrimSpace(subRef), "/")
				if len(parts) == 2 && parts[0] == firstSub.Namespace && parts[1] == firstSub.Name {
					logger.Info("Found parent Application via annotation",
						"application", app.Name,
						"subscription", firstSub.Name)
					return app, nil
				}
			}
		}
		if r.subscriptionBelongsToApp(firstSub, app) {
			logger.Info("Found parent Application via selector",
				"application", app.Name,
				"subscription", firstSub.Name)
			return app, nil
		}
	}

	logger.Info("No parent Application found for subscriptions")
	return nil, nil
}

// subscriptionBelongsToApp checks if a Subscription belongs to an Application based on label selectors.
func (r *ProtectedApplicationViewReconciler) subscriptionBelongsToApp(
	sub *appsubv1.Subscription,
	app *appv1beta1.Application,
) bool {
	if app.Spec.Selector == nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(app.Spec.Selector)
	if err != nil {
		return false
	}

	return selector.Matches(labels.Set(sub.Labels))
}

// getPlacementDecision retrieves the list of selected clusters from the PlacementDecision associated with the Placement
func (r *ProtectedApplicationViewReconciler) getPlacementDecision(
	ctx context.Context,
	placement *placementv1beta1.Placement,
) ([]string, error) {
	decisionList := &placementv1beta1.PlacementDecisionList{}
	if err := r.List(ctx, decisionList,
		client.InNamespace(placement.Namespace),
		client.MatchingLabels{
			placementLabel: placement.Name,
		}); err != nil {
		return nil, err
	}

	var clusters []string
	for _, decision := range decisionList.Items {
		for _, d := range decision.Status.Decisions {
			clusters = append(clusters, d.ClusterName)
		}
	}

	return clusters, nil
}

// findPAVsForPlacement finds ProtectedApplicationViews associated with the given Placement
func (r *ProtectedApplicationViewReconciler) findPAVsForPlacement(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	placement, ok := obj.(*placementv1beta1.Placement)
	if !ok {
		return nil
	}
	drpcList := &ramenv1alpha1.DRPlacementControlList{}
	if err := r.List(ctx, drpcList,
		client.InNamespace(placement.Namespace),
		client.MatchingFields{placementIndexName: placement.Name},
	); err != nil {
		r.Logger.Error("Failed to list DRPCs by placement", "error", err)
		return nil
	}

	var requests []reconcile.Request
	for _, drpc := range drpcList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      drpc.Name,
				Namespace: drpc.Namespace,
			},
		})
	}

	return requests
}

// findPAVsForPlacementDecision finds ProtectedApplicationViews associated with the given PlacementDecision
func (r *ProtectedApplicationViewReconciler) findPAVsForPlacementDecision(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	decision, ok := obj.(*placementv1beta1.PlacementDecision)
	if !ok {
		return nil
	}
	placementName, exists := decision.Labels[placementLabel]
	if !exists {
		return nil
	}

	placement := &placementv1beta1.Placement{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      placementName,
		Namespace: decision.Namespace,
	}, placement); err != nil {
		return nil
	}

	return r.findPAVsForPlacement(ctx, placement)
}
