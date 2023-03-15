package controllers

import (
	"context"
	"fmt"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

const (
	drpName     = "test-dr-policy"
	mpName      = "mirrorpeer"
	cName1      = "cluster-1"
	cName2      = "cluster-2"
	scName      = "test-storagecluster"
	scNamespace = "test-namespace"
)

func TestDRPolicyReconcile(t *testing.T) {

	mirrorpeer := multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: mpName,
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type: multiclusterv1alpha1.Async,
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: cName1,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      scName,
						Namespace: scNamespace,
					},
				},
				{
					ClusterName: cName2,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      scName,
						Namespace: scNamespace,
					},
				},
			},
		},
	}

	drpolicy := ramenv1alpha1.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: drpName,
		},
		Spec: ramenv1alpha1.DRPolicySpec{
			SchedulingInterval: "1h",
			DRClusters:         []string{cName1, cName2},
		},
	}

	r := getFakeDRPolicyReconciler(&drpolicy, &mirrorpeer)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: drpName,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("DRPolicyReconciler Reconcile() failed. Error: %s", err)
	}

	for _, clusterName := range drpolicy.Spec.DRClusters {
		name := fmt.Sprintf("vrc-%v", utils.FnvHash(drpolicy.Name))
		var found workv1.ManifestWork
		err := r.HubClient.Get(ctx, types.NamespacedName{
			Namespace: clusterName,
			Name:      name,
		}, &found)

		if err != nil {
			t.Errorf("Failed to get ManifestWork. Error: %s", err)
		}
	}
}

func getFakeDRPolicyReconciler(drpolicy *ramenv1alpha1.DRPolicy, mp *multiclusterv1alpha1.MirrorPeer) DRPolicyReconciler {
	scheme := mgrScheme
	ns1 := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName1,
		},
	}
	ns2 := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName2,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(drpolicy, mp, ns1, ns2).Build()

	r := DRPolicyReconciler{
		HubClient: fakeClient,
		Scheme:    scheme,
	}

	return r
}
