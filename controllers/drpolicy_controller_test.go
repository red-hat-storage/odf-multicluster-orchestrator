package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
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
		if len(found.Spec.Workload.Manifests) < 2 {
			t.Errorf("Expected at least 2 VRC")
		}
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

	// Constitutes both blue secret and green secret present on the hub
	hubSecret1 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetSecretNameByPeerRef(mp.Spec.Items[0]),
			Namespace: cName1,
		},
		Data: map[string][]byte{
			"namespace":            []byte("openshift-storage"),
			"secret-data":          []byte(`{"cluster":"b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy","token":"ZXlKbWMybGtJam9pWXpSak56SmpNRE10WXpCbFlpMDBZMlppTFRnME16RXRNekExTmpZME16UmxZV1ZqSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkVkbGxyTldrM04xbG9TMEpCUVZZM2NFZHlVVXBrU1VvelJtZGpjVWxGVUZWS0wzYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak13TGpFd01TNHlORGs2TmpjNE9Td3hOekl1TXpBdU1UZ3pMakU1TURvMk56ZzVMREUzTWk0ek1DNHlNak11TWpFd09qWTNPRGtpTENKdVlXMWxjM0JoWTJVaU9pSnZjR1Z1YzJocFpuUXRjM1J2Y21GblpTSjk="}`),
			"secret-origin":        []byte("rook"),
			"storage-cluster-name": []byte("ocs-storagecluster"),
		},
		Type: "multicluster.odf.openshift.io/secret-type",
	}

	hubSecret2 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetSecretNameByPeerRef(mp.Spec.Items[1]),
			Namespace: cName2,
		},
		Data: map[string][]byte{
			"namespace":            []byte("openshift-storage"),
			"secret-data":          []byte(`{"cluster":"b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy","token":"ZXlKbWMybGtJam9pWXpSak56SmpNRE10WXpCbFlpMDBZMlppTFRnME16RXRNekExTmpZME16UmxZV1ZqSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkVkbGxyTldrM04xbG9TMEpCUVZZM2NFZHlVVXBrU1VvelJtZGpjVWxGVUZWS0wzYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak13TGpFd01TNHlORGs2TmpjNE9Td3hOekl1TXpBdU1UZ3pMakU1TURvMk56ZzVMREUzTWk0ek1DNHlNak11TWpFd09qWTNPRGtpTENKdVlXMWxjM0JoWTJVaU9pSnZjR1Z1YzJocFpuUXRjM1J2Y21GblpTSjk="}`),
			"secret-origin":        []byte("rook"),
			"storage-cluster-name": []byte("ocs-storagecluster"),
		},
		Type: "multicluster.odf.openshift.io/secret-type",
	}
	odfClientInfoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: os.Getenv("POD_NAMESPACE"),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: map[string]string{},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(drpolicy, mp, ns1, ns2, &hubSecret1, &hubSecret2, odfClientInfoConfigMap).Build()

	r := DRPolicyReconciler{
		HubClient: fakeClient,
		Scheme:    scheme,
		Logger:    utils.GetLogger(utils.GetZapLogger(true)),
	}

	return r
}
