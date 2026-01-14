package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	plrv1alpha1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	appv1beta1 "sigs.k8s.io/application/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	pavTestDRPCName      = "test-drpc"
	pavTestNamespace     = "test-namespace"
	pavTestPlacementName = "test-placement"
	pavTestAppSetName    = "test-appset"
	pavTestAppName       = "test-app"
	pavTestSubName       = "test-subscription"
	pavTestDRPolicyName  = "test-drpolicy"
	pavTestCluster1      = "cluster-1"
	pavTestCluster2      = "cluster-2"
)

func TestPAVReconcile_ApplicationSet(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	placement := createTestPlacementForPAV()
	appSet := createTestApplicationSetForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, appSet, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify PAV status was populated
	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.ApplicationInfo.Type != multiclusterv1alpha1.ApplicationSetType {
		t.Errorf("Expected ApplicationSetType, got %s", updatedPAV.Status.ApplicationInfo.Type)
	}

	if updatedPAV.Status.ApplicationInfo.ApplicationRef.Name != pavTestAppSetName {
		t.Errorf("Expected appRef %s, got %s", pavTestAppSetName, updatedPAV.Status.ApplicationInfo.ApplicationRef.Name)
	}
}

func TestPAVReconcile_Subscription(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	placement := createTestPlacementForPAV()
	subscription := createTestSubscriptionForPAV()
	application := createTestApplicationForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, subscription, application, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.ApplicationInfo.Type != multiclusterv1alpha1.SubscriptionType {
		t.Errorf("Expected SubscriptionType, got %s", updatedPAV.Status.ApplicationInfo.Type)
	}

	if updatedPAV.Status.ApplicationInfo.ApplicationRef.Name != pavTestAppName {
		t.Errorf("Expected appRef %s, got %s", pavTestAppName, updatedPAV.Status.ApplicationInfo.ApplicationRef.Name)
	}

	if updatedPAV.Status.ApplicationInfo.SubscriptionInfo == nil {
		t.Fatal("SubscriptionInfo should not be nil")
	}

	if len(updatedPAV.Status.ApplicationInfo.SubscriptionInfo.SubscriptionRefs) != 1 {
		t.Errorf("Expected 1 subscription ref, got %d", len(updatedPAV.Status.ApplicationInfo.SubscriptionInfo.SubscriptionRefs))
	}
}

func TestPAVReconcile_Discovered(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	placement := createTestPlacementForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.ApplicationInfo.Type != multiclusterv1alpha1.DiscoveredType {
		t.Errorf("Expected DiscoveredType, got %s", updatedPAV.Status.ApplicationInfo.Type)
	}

	if updatedPAV.Status.ApplicationInfo.ApplicationRef.Name != pavTestDRPCName {
		t.Errorf("Expected appRef %s, got %s", pavTestDRPCName, updatedPAV.Status.ApplicationInfo.ApplicationRef.Name)
	}
}

func TestPAVReconcile_SubscriptionNoParentApp(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	placement := createTestPlacementForPAV()
	subscription := createTestSubscriptionForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, subscription, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.ApplicationInfo.Type != multiclusterv1alpha1.SubscriptionType {
		t.Errorf("Expected SubscriptionType, got %s", updatedPAV.Status.ApplicationInfo.Type)
	}

	if updatedPAV.Status.ApplicationInfo.ApplicationRef.Name != "" {
		t.Errorf("Expected empty appRef, got %s", updatedPAV.Status.ApplicationInfo.ApplicationRef.Name)
	}

	if len(updatedPAV.Status.ApplicationInfo.SubscriptionInfo.SubscriptionRefs) != 1 {
		t.Errorf("Expected 1 subscription ref, got %d", len(updatedPAV.Status.ApplicationInfo.SubscriptionInfo.SubscriptionRefs))
	}
}

func TestPAVReconcile_StatusUpdate(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	drpc.Spec.PreferredCluster = pavTestCluster1
	drpc.Status.Phase = ramenv1alpha1.Relocated
	drpc.Status.PreferredDecision = ramenv1alpha1.PlacementDecision{
		ClusterName: pavTestCluster1,
	}

	placement := createTestPlacementForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.DRInfo.Status.Phase != ramenv1alpha1.Relocated {
		t.Errorf("Expected phase %s, got %s", ramenv1alpha1.Relocated, updatedPAV.Status.DRInfo.Status.Phase)
	}

	if updatedPAV.Status.DRInfo.PrimaryCluster != pavTestCluster1 {
		t.Errorf("Expected primary cluster %s, got %s", pavTestCluster1, updatedPAV.Status.DRInfo.PrimaryCluster)
	}

	if updatedPAV.Status.PlacementInfo == nil {
		t.Fatal("PlacementInfo should not be nil")
	}

	if len(updatedPAV.Status.PlacementInfo.SelectedClusters) != 2 {
		t.Errorf("Expected 2 clusters, got %d", len(updatedPAV.Status.PlacementInfo.SelectedClusters))
	}

	if updatedPAV.Status.LastSyncTime == nil {
		t.Error("LastSyncTime should be set")
	}
}

func TestFindApplicationSetByPlacement_NamingConvention(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	drpc.Name = "myapp-placement-drpc"
	pav.Name = drpc.Name
	pav.Spec.DRPCRef.Name = drpc.Name

	placement := createTestPlacementForPAV()
	placement.Name = "shared-placement"

	appSet1 := createTestApplicationSetForPAV()
	appSet1.Name = "myapp"
	appSet1.Spec.Generators[0].ClusterDecisionResource.LabelSelector.MatchLabels = map[string]string{
		"cluster.open-cluster-management.io/placement": "shared-placement",
	}

	appSet2 := createTestApplicationSetForPAV()
	appSet2.Name = "otherapp"
	appSet2.Spec.Generators[0].ClusterDecisionResource.LabelSelector.MatchLabels = map[string]string{
		"cluster.open-cluster-management.io/placement": "shared-placement",
	}

	r := getFakePAVReconciler(pav, drpc, placement, appSet1, appSet2)

	result, err := r.findApplicationSetByPlacement(context.TODO(), drpc, placement)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil || result.Name != "myapp" {
		t.Errorf("Expected 'myapp', got %v", result)
	}
}

func TestFindPAVsForPlacement_WithIndex(t *testing.T) {
	placement := createTestPlacementForPAV()

	drpc1 := createTestDRPCForPAV()
	drpc1.Name = "drpc-1"

	drpc2 := createTestDRPCForPAV()
	drpc2.Name = "drpc-2"
	drpc2.Spec.PlacementRef.Name = "other-placement"

	drpc3 := createTestDRPCForPAV()
	drpc3.Name = "drpc-3"

	pav1 := createTestPAV()
	pav1.Name = "drpc-1"
	pav1.Spec.DRPCRef.Name = "drpc-1"

	pav2 := createTestPAV()
	pav2.Name = "drpc-2"
	pav2.Spec.DRPCRef.Name = "drpc-2"

	pav3 := createTestPAV()
	pav3.Name = "drpc-3"
	pav3.Spec.DRPCRef.Name = "drpc-3"

	r := getFakePAVReconciler(placement, drpc1, drpc2, drpc3, pav1, pav2, pav3)

	requests := r.findPAVsForPlacement(context.TODO(), placement)

	// Should only return drpc-1 and drpc-3 (both reference test-placement)
	if len(requests) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(requests))
	}

	// Verify the correct DRPCs were returned
	foundDRPC1 := false
	foundDRPC3 := false
	for _, req := range requests {
		if req.Name == "drpc-1" {
			foundDRPC1 = true
		}
		if req.Name == "drpc-3" {
			foundDRPC3 = true
		}
		if req.Name == "drpc-2" {
			t.Error("drpc-2 should not be returned (different placement)")
		}
	}

	if !foundDRPC1 {
		t.Error("Expected to find drpc-1")
	}
	if !foundDRPC3 {
		t.Error("Expected to find drpc-3")
	}
}

func TestFindPAVsForPlacement_NoMatches(t *testing.T) {
	placement := createTestPlacementForPAV()

	drpc := createTestDRPCForPAV()
	drpc.Spec.PlacementRef.Name = "other-placement"

	pav := createTestPAV()

	r := getFakePAVReconciler(placement, drpc, pav)

	requests := r.findPAVsForPlacement(context.TODO(), placement)

	if len(requests) != 0 {
		t.Errorf("Expected 0 requests, got %d", len(requests))
	}
}

func TestFindPAVsForPlacement_MultipleNamespaces(t *testing.T) {
	placement := createTestPlacementForPAV()

	drpc1 := createTestDRPCForPAV()
	drpc1.Name = "drpc-1"
	drpc1.Namespace = pavTestNamespace

	drpc2 := createTestDRPCForPAV()
	drpc2.Name = "drpc-2"
	drpc2.Namespace = "other-namespace"

	pav1 := createTestPAV()
	pav1.Name = "drpc-1"
	pav1.Namespace = pavTestNamespace

	pav2 := createTestPAV()
	pav2.Name = "drpc-2"
	pav2.Namespace = "other-namespace"

	r := getFakePAVReconciler(placement, drpc1, drpc2, pav1, pav2)

	requests := r.findPAVsForPlacement(context.TODO(), placement)

	// Should only return drpc-1 (same namespace as placement)
	if len(requests) != 1 {
		t.Errorf("Expected 1 request, got %d", len(requests))
	}

	if len(requests) > 0 && requests[0].Name != "drpc-1" {
		t.Errorf("Expected drpc-1, got %s", requests[0].Name)
	}
}

func createTestPAV() *multiclusterv1alpha1.ProtectedApplicationView {
	return &multiclusterv1alpha1.ProtectedApplicationView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
		Spec: multiclusterv1alpha1.ProtectedApplicationViewSpec{
			DRPCRef: corev1.ObjectReference{
				Name:      pavTestDRPCName,
				Namespace: pavTestNamespace,
			},
		},
	}
}

func createTestDRPCForPAV() *ramenv1alpha1.DRPlacementControl {
	return &ramenv1alpha1.DRPlacementControl{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
		Spec: ramenv1alpha1.DRPlacementControlSpec{
			PlacementRef: corev1.ObjectReference{
				Name:      pavTestPlacementName,
				Namespace: pavTestNamespace,
				Kind:      placementKind,
			},
			DRPolicyRef: corev1.ObjectReference{
				Name: pavTestDRPolicyName,
			},
			ProtectedNamespaces: &[]string{"app-namespace"},
		},
		Status: ramenv1alpha1.DRPlacementControlStatus{
			Phase: ramenv1alpha1.Deployed,
		},
	}
}

func createTestPlacementForPAV() *placementv1beta1.Placement {
	return &placementv1beta1.Placement{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestPlacementName,
			Namespace: pavTestNamespace,
		},
		Spec: placementv1beta1.PlacementSpec{
			ClusterSets: []string{"clusterset1"},
		},
	}
}

func createTestApplicationSetForPAV() *argov1alpha1.ApplicationSet {
	return &argov1alpha1.ApplicationSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestAppSetName,
			Namespace: pavTestNamespace,
		},
		Spec: argov1alpha1.ApplicationSetSpec{
			Generators: []argov1alpha1.ApplicationSetGenerator{
				{
					ClusterDecisionResource: &argov1alpha1.DuckTypeGenerator{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"cluster.open-cluster-management.io/placement": pavTestPlacementName,
							},
						},
					},
				},
			},
		},
	}
}

func createTestSubscriptionForPAV() *appsubv1.Subscription {
	return &appsubv1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestSubName,
			Namespace: pavTestNamespace,
			Labels: map[string]string{
				"app": pavTestAppName,
			},
		},
		Spec: appsubv1.SubscriptionSpec{
			Channel: "channel/repo",
			Placement: &plrv1alpha1.Placement{
				PlacementRef: &corev1.ObjectReference{
					Name: pavTestPlacementName,
					Kind: placementKind,
				},
			},
		},
	}
}

func createTestApplicationForPAV() *appv1beta1.Application {
	return &appv1beta1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestAppName,
			Namespace: pavTestNamespace,
			Annotations: map[string]string{
				subscriptionAnnotation: pavTestNamespace + "/" + pavTestSubName,
			},
		},
		Spec: appv1beta1.ApplicationSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": pavTestAppName,
				},
			},
		},
	}
}

func createTestDRPolicyForPAV() *ramenv1alpha1.DRPolicy {
	return &ramenv1alpha1.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: pavTestDRPolicyName,
		},
		Spec: ramenv1alpha1.DRPolicySpec{
			DRClusters:         []string{pavTestCluster1, pavTestCluster2},
			SchedulingInterval: "1h",
		},
	}
}

func createTestPlacementDecisionForPAV() *placementv1beta1.PlacementDecision {
	return &placementv1beta1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pavTestPlacementName + "-decision",
			Namespace: pavTestNamespace,
			Labels: map[string]string{
				placementLabel: pavTestPlacementName,
			},
		},
		Status: placementv1beta1.PlacementDecisionStatus{
			Decisions: []placementv1beta1.ClusterDecision{
				{ClusterName: pavTestCluster1},
				{ClusterName: pavTestCluster2},
			},
		},
	}
}

func getFakePAVReconciler(objects ...client.Object) *ProtectedApplicationViewReconciler {
	scheme := mgrScheme

	if err := appv1beta1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("Failed to add Application scheme: %v", err))
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&multiclusterv1alpha1.ProtectedApplicationView{}).
		WithIndex(
			&argov1alpha1.ApplicationSet{},
			clusterDecisionResourceIndexName,
			func(o client.Object) []string {
				appSet, ok := o.(*argov1alpha1.ApplicationSet)
				if !ok {
					return nil
				}
				var placements []string

				for _, gen := range appSet.Spec.Generators {
					if gen.ClusterDecisionResource != nil &&
						gen.ClusterDecisionResource.LabelSelector.MatchLabels != nil {
						for key, value := range gen.ClusterDecisionResource.LabelSelector.MatchLabels {

							if strings.Contains(key, placementLabel) {
								placements = append(placements, value)
							}
						}
					}
				}

				return placements
			},
		).
		WithIndex(
			&ramenv1alpha1.DRPlacementControl{},
			placementIndexName,
			func(o client.Object) []string {
				drpc, ok := o.(*ramenv1alpha1.DRPlacementControl)
				if !ok {
					return nil
				}
				return []string{drpc.Spec.PlacementRef.Name}
			},
		).
		Build()

	return &ProtectedApplicationViewReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Logger: utils.GetLogger(utils.GetZapLogger(true)),
	}
}

func TestPAVReconcile_FailedOverPrimaryCluster(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	drpc.Spec.PreferredCluster = pavTestCluster1
	drpc.Spec.FailoverCluster = pavTestCluster2
	drpc.Status.Phase = ramenv1alpha1.FailedOver
	drpc.Status.PreferredDecision = ramenv1alpha1.PlacementDecision{
		ClusterName: pavTestCluster1,
	}

	placement := createTestPlacementForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.DRInfo.Status.Phase != ramenv1alpha1.FailedOver {
		t.Errorf("Expected phase %s, got %s", ramenv1alpha1.FailedOver, updatedPAV.Status.DRInfo.Status.Phase)
	}

	// Verify primaryCluster is failoverCluster, not PreferredDecision
	if updatedPAV.Status.DRInfo.PrimaryCluster != pavTestCluster2 {
		t.Errorf("Expected primary cluster %s (failoverCluster), got %s",
			pavTestCluster2, updatedPAV.Status.DRInfo.PrimaryCluster)
	}
}

func TestPAVReconcile_RelocatedPrimaryCluster(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	drpc.Spec.PreferredCluster = pavTestCluster1
	drpc.Spec.FailoverCluster = pavTestCluster2
	drpc.Status.Phase = ramenv1alpha1.Relocated
	drpc.Status.PreferredDecision = ramenv1alpha1.PlacementDecision{
		ClusterName: pavTestCluster2, // Still points to failover cluster
	}

	placement := createTestPlacementForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.DRInfo.Status.Phase != ramenv1alpha1.Relocated {
		t.Errorf("Expected phase %s, got %s", ramenv1alpha1.Relocated, updatedPAV.Status.DRInfo.Status.Phase)
	}

	// Verify primaryCluster is preferredCluster
	if updatedPAV.Status.DRInfo.PrimaryCluster != pavTestCluster1 {
		t.Errorf("Expected primary cluster %s (preferredCluster), got %s",
			pavTestCluster1, updatedPAV.Status.DRInfo.PrimaryCluster)
	}
}

func TestPAVReconcile_DeployedPrimaryCluster(t *testing.T) {
	pav := createTestPAV()
	drpc := createTestDRPCForPAV()
	drpc.Spec.PreferredCluster = pavTestCluster1
	drpc.Spec.FailoverCluster = pavTestCluster2
	drpc.Status.Phase = ramenv1alpha1.Deployed
	drpc.Status.PreferredDecision = ramenv1alpha1.PlacementDecision{
		ClusterName: pavTestCluster1,
	}

	placement := createTestPlacementForPAV()
	drPolicy := createTestDRPolicyForPAV()
	placementDecision := createTestPlacementDecisionForPAV()

	r := getFakePAVReconciler(pav, drpc, placement, drPolicy, placementDecision)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      pavTestDRPCName,
			Namespace: pavTestNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedPAV := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      pavTestDRPCName,
		Namespace: pavTestNamespace,
	}, updatedPAV)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if updatedPAV.Status.DRInfo.Status.Phase != ramenv1alpha1.Deployed {
		t.Errorf("Expected phase %s, got %s", ramenv1alpha1.Deployed, updatedPAV.Status.DRInfo.Status.Phase)
	}

	// Verify primaryCluster uses PreferredDecision for Deployed state
	if updatedPAV.Status.DRInfo.PrimaryCluster != pavTestCluster1 {
		t.Errorf("Expected primary cluster %s (preferredDecision), got %s",
			pavTestCluster1, updatedPAV.Status.DRInfo.PrimaryCluster)
	}
}
