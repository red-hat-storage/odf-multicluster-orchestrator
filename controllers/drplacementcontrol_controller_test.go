package controllers

import (
	"context"
	"testing"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testDRPCName     = "test-drpc"
	testNamespace    = "test-namespace"
	testDRPolicyName = "test-drpolicy"
)

func TestDRPCReconcile_CreatesPAVSpec(t *testing.T) {
	drpc := createTestDRPC()
	r := getFakeDRPCReconciler(drpc)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      testDRPCName,
			Namespace: testNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify PAV was created
	pav := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      testDRPCName,
		Namespace: testNamespace,
	}, pav)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	// Verify spec.drpcRef is set
	if pav.Spec.DRPCRef.Name != testDRPCName {
		t.Errorf("Expected drpcRef.name %s, got %s", testDRPCName, pav.Spec.DRPCRef.Name)
	}

	if pav.Spec.DRPCRef.Kind != drpcKind {
		t.Errorf("Expected drpcRef.kind %s, got %s", drpcKind, pav.Spec.DRPCRef.Kind)
	}

	// Verify owner reference
	if len(pav.OwnerReferences) == 0 {
		t.Fatal("PAV should have owner reference")
	}

	if pav.OwnerReferences[0].Name != testDRPCName {
		t.Errorf("Expected owner %s, got %s", testDRPCName, pav.OwnerReferences[0].Name)
	}

	// Status should be empty (populated by PAV controller)
	if pav.Status.ApplicationInfo.Type != "" {
		t.Errorf("DRPC controller should not populate status, got type: %s", pav.Status.ApplicationInfo.Type)
	}
}

func TestDRPCReconcile_UpdatesPAVSpec(t *testing.T) {
	drpc := createTestDRPC()

	// Create existing PAV with old UID
	existingPAV := &multiclusterv1alpha1.ProtectedApplicationView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDRPCName,
			Namespace: testNamespace,
		},
		Spec: multiclusterv1alpha1.ProtectedApplicationViewSpec{
			DRPCRef: corev1.ObjectReference{
				Name: testDRPCName,
				UID:  "old-uid",
			},
		},
	}

	r := getFakeDRPCReconciler(drpc, existingPAV)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      testDRPCName,
			Namespace: testNamespace,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify PAV spec was updated
	pav := &multiclusterv1alpha1.ProtectedApplicationView{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      testDRPCName,
		Namespace: testNamespace,
	}, pav)
	if err != nil {
		t.Fatalf("Failed to get PAV: %v", err)
	}

	if pav.Spec.DRPCRef.UID != drpc.UID {
		t.Errorf("Expected UID %s, got %s", drpc.UID, pav.Spec.DRPCRef.UID)
	}
}

func TestDRPCReconcile_NotFound(t *testing.T) {
	r := getFakeDRPCReconciler()

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: testNamespace,
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Should not error on NotFound, got: %v", err)
	}

	if result.RequeueAfter != 0 {
		t.Error("Should not requeue on NotFound")
	}
}

func createTestDRPC() *ramenv1alpha1.DRPlacementControl {
	return &ramenv1alpha1.DRPlacementControl{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDRPCName,
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
		Spec: ramenv1alpha1.DRPlacementControlSpec{
			PlacementRef: corev1.ObjectReference{
				Name: "test-placement",
				Kind: placementKind,
			},
			DRPolicyRef: corev1.ObjectReference{
				Name: testDRPolicyName,
			},
		},
	}
}

func getFakeDRPCReconciler(objects ...client.Object) *DRPlacementControlReconciler {
	scheme := mgrScheme

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&multiclusterv1alpha1.ProtectedApplicationView{}).
		Build()

	return &DRPlacementControlReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Logger: utils.GetLogger(utils.GetZapLogger(true)),
	}
}
