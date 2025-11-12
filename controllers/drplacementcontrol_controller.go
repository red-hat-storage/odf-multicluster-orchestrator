package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type DRPCReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

func (r *DRPCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	drpc := &ramenv1alpha1.DRPlacementControl{}
	if err := r.Get(ctx, req.NamespacedName, drpc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	placement, err := r.getPlacement(ctx, drpc)
	if err != nil {
		r.Logger.Error("Failed to get Placement", "error", err, "drpc", req.NamespacedName)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	appType, appRef, err := r.findApplicationForDRPC(ctx, placement)
	if err != nil {
		r.Logger.Info("No application found, skipping view creation", "drpc", req.NamespacedName)
		return r.cleanupView(drpc)
	}

	pav := r.buildPAV(ctx, drpc, placement, appType, appRef)

	if err := controllerutil.SetControllerReference(drpc, pav, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existing := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name: pav.Name, Namespace: pav.Namespace,
	}, existing)

	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, pav); err != nil {
			return ctrl.Result{}, err
		}
		r.Logger.Info("Created PAV", "name", pav.Name, "type", appType)
	} else if err == nil {
		existing.Spec = pav.Spec
		if err := r.Update(ctx, existing); err != nil {
			return ctrl.Result{}, err
		}
		r.Logger.Info("Updated PAV", "name", pav.Name, "type", appType)
	} else {
		return ctrl.Result{}, err
	}

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

func (r *DRPCReconciler) getPlacement(
	ctx context.Context,
	drpc *ramenv1alpha1.DRPlacementControl,
) (*placementv1beta1.Placement, error) {
	placement := &placementv1beta1.Placement{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      drpc.Spec.PlacementRef.Name,
		Namespace: drpc.Namespace,
	}, placement)
	return placement, err
}

func (r *DRPCReconciler) findApplicationForDRPC(
	ctx context.Context,
	placement *placementv1beta1.Placement,
) (string, corev1.ObjectReference, error) {
	// Try ApplicationSet
	appSet, err := r.findApplicationSetByPlacement(ctx, placement)
	if err == nil && appSet != nil {
		return "ApplicationSet", corev1.ObjectReference{
			APIVersion: appSet.APIVersion,
			Kind:       appSet.Kind,
			Name:       appSet.Name,
			Namespace:  appSet.Namespace,
			UID:        appSet.UID,
		}, nil
	}

	// TODO: Reverse map subscription

	// Not found - return error to skip view creation
	return "", corev1.ObjectReference{}, fmt.Errorf("no application found for placement %s/%s", placement.Namespace, placement.Name)
}

func (r *DRPCReconciler) findApplicationSetByPlacement(
	ctx context.Context,
	placement *placementv1beta1.Placement,
) (*argov1alpha1.ApplicationSet, error) {
	appSetList := &argov1alpha1.ApplicationSetList{}
	if err := r.List(ctx, appSetList, client.InNamespace(placement.Namespace)); err != nil {
		r.Logger.Error("Failed to list ApplicationSets", "error", err, "namespace", placement.Namespace)
		return nil, err
	}

	for i := range appSetList.Items {
		appSet := &appSetList.Items[i]
		if r.extractPlacementFromApplicationSet(appSet) == placement.Name {
			r.Logger.Info("Found ApplicationSet for Placement",
				"applicationSet", appSet.Name,
				"placement", placement.Name)
			return appSet, nil
		}
	}

	return nil, nil
}

func (r *DRPCReconciler) getPlacementDecision(
	ctx context.Context,
	placement *placementv1beta1.Placement,
) ([]string, error) {
	decisionList := &placementv1beta1.PlacementDecisionList{}
	if err := r.List(ctx, decisionList,
		client.InNamespace(placement.Namespace),
		client.MatchingLabels{
			"cluster.open-cluster-management.io/placement": placement.Name,
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

func (r *DRPCReconciler) getDRPolicy(
	ctx context.Context,
	policyName string,
) (*ramenv1alpha1.DRPolicy, error) {
	policy := &ramenv1alpha1.DRPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: policyName}, policy)
	return policy, err
}

func (r *DRPCReconciler) buildPAV(
	ctx context.Context,
	drpc *ramenv1alpha1.DRPlacementControl,
	placement *placementv1beta1.Placement,
	appType string,
	appRef corev1.ObjectReference,
) *multiclusterv1alpha1.ProtectedApplicationView {
	// Get placement decisions
	selectedClusters, err := r.getPlacementDecision(ctx, placement)
	if err != nil {
		r.Logger.Error("Failed to get PlacementDecision", "error", err)
	}

	drPolicy, err := r.getDRPolicy(ctx, drpc.Spec.DRPolicyRef.Name)
	if err != nil {
		r.Logger.Error("Failed to get DRPolicy", "error", err)
	}

	var drClusters []string
	if drPolicy != nil {
		drClusters = drPolicy.Spec.DRClusters
	}

	// Extract primary cluster
	primaryCluster := ""
	if drpc.Status.PreferredDecision.ClusterName != "" {
		primaryCluster = drpc.Status.PreferredDecision.ClusterName
	}

	pav := &multiclusterv1alpha1.ProtectedApplicationView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", strings.ToLower(appType), drpc.Name),
			Namespace: drpc.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/type":         appType,
				"ramendr.openshift.io/dr-policy": drpc.Spec.DRPolicyRef.Name,
			},
		},
		Spec: multiclusterv1alpha1.ProtectedApplicationViewSpec{
			Type:           appType,
			ApplicationRef: appRef,
			Placements: []multiclusterv1alpha1.PlacementInfo{{
				Name:             placement.Name,
				Namespace:        placement.Namespace,
				Kind:             placement.Kind,
				SelectedClusters: selectedClusters,
				NumberOfClusters: len(selectedClusters),
			}},
			DRInfo: multiclusterv1alpha1.DRInfo{
				DRPCRef: corev1.ObjectReference{
					Name:      drpc.Name,
					Namespace: drpc.Namespace,
				},
				DRPolicyRef: corev1.ObjectReference{
					Name: drpc.Spec.DRPolicyRef.Name,
				},
				DRClusters:          drClusters,
				PrimaryCluster:      primaryCluster,
				ProtectedNamespaces: getProtectedNamespaces(drpc),
				Status: multiclusterv1alpha1.DRStatusInfo{
					Phase:      string(drpc.Status.Phase),
					Conditions: drpc.Status.Conditions,
				},
			},
		},
	}

	// Add type-specific info for ApplicationSet
	if appType == "ApplicationSet" {
		pav.Spec.ApplicationSetInfo = r.extractApplicationSetInfo(ctx, appRef)
	}

	return pav
}

func getProtectedNamespaces(drpc *ramenv1alpha1.DRPlacementControl) []string {
	if drpc.Spec.ProtectedNamespaces == nil {
		return []string{}
	}
	return *drpc.Spec.ProtectedNamespaces
}

func (r *DRPCReconciler) extractApplicationSetInfo(
	ctx context.Context,
	appRef corev1.ObjectReference,
) *multiclusterv1alpha1.ApplicationSetInfo {
	appSet := &argov1alpha1.ApplicationSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name: appRef.Name, Namespace: appRef.Namespace,
	}, appSet); err != nil {
		return nil
	}

	var gitRepoURLs []string
	targetRevision := ""

	if appSet.Spec.Template.Spec.Source != nil {
		gitRepoURLs = append(gitRepoURLs, appSet.Spec.Template.Spec.Source.RepoURL)
		targetRevision = appSet.Spec.Template.Spec.Source.TargetRevision
	}

	return &multiclusterv1alpha1.ApplicationSetInfo{
		GitRepoURLs:    gitRepoURLs,
		TargetRevision: targetRevision,
	}
}

func (r *DRPCReconciler) cleanupView(
	drpc *ramenv1alpha1.DRPlacementControl,
) (ctrl.Result, error) {
	// View auto-deleted via owner reference
	r.Logger.Info("Skipping view creation for orphaned DRPC",
		"drpc", drpc.Name,
		"namespace", drpc.Namespace)
	return ctrl.Result{}, nil
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
