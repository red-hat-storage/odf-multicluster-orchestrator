package controllers

import (
	"context"
	"os"
	"testing"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
			ReplicationClassSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					RBDFlattenVolumeReplicationClassLabelKey: RBDFlattenVolumeReplicationClassLabelValue,
				},
			},
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

}

func getFakeDRPolicyReconciler(drpolicy *ramenv1alpha1.DRPolicy, mp *multiclusterv1alpha1.MirrorPeer) DRPolicyReconciler {
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName1,
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName2,
		},
	}
	odfClientInfoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: utils.GetEnv("POD_NAMESPACE"),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: map[string]string{
			"cluster-1_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
			"cluster-2_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(drpolicy, mp, ns1, ns2, odfClientInfoConfigMap).Build()

	r := DRPolicyReconciler{
		HubClient:        fakeClient,
		Scheme:           scheme,
		Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
	}

	return r
}
