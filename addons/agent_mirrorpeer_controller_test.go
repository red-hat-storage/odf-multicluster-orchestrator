//go:build unit
// +build unit

package addons

import (
	"context"
	"os"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/testutil"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	storageClusterName = "test-storagecluster"
	odfNamespace       = "test-namespace"
)

var testSchemeInstance = testutil.TestScheme

var (
	odfInfoConfigMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-info",
			Namespace: odfNamespace,
			UID:       types.UID("268e6cdb-54fc-4f10-afab-67b106880be3"),
		},
		Data: map[string]string{
			"test-namespace_test-storagecluster.config.yaml": `
version: 4.17.0-95.stable
deploymentType: internal
clients: []
storageCluster:
  namespacedName:
    namespace: test-namespace
    name: test-storagecluster
  storageProviderEndpoint: ""
  cephClusterFSID: 986532da-8dba-4d35-a8d2-12f037712b39
`,
		},
	}
	odfClientInfoConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: "openshift-operators",
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
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"deploymentType\": \"external\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"deploymentType\": \"external\"}}",
		},
	}
	mpItems = []multiclusterv1alpha1.PeerRef{
		{
			ClusterName: "cluster1",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      storageClusterName,
				Namespace: odfNamespace,
			},
		},
		{
			ClusterName: "cluster2",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      storageClusterName,
				Namespace: odfNamespace,
			},
		},
	}
	mirrorpeer1 = &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-proper-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:  "async",
			Items: mpItems,
		},
	}
	mirrorpeer2 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-invalid-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:  "async",
			Items: mpItems,
		},
	}
)

func TestMirrorPeerReconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := testSchemeInstance
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mirrorpeer1, &mirrorpeer2, odfClientInfoConfigMap).Build()
	os.Setenv("POD_NAMESPACE", odfNamespace)

	for _, pr := range mirrorpeer1.Spec.Items {
		storageCluster := ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pr.StorageClusterRef.Name,
				Namespace: pr.StorageClusterRef.Namespace,
			},
			Spec: ocsv1.StorageClusterSpec{},
		}

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster, &odfInfoConfigMap).Build()

		r := MirrorPeerReconciler{
			HubClient:            fakeHubClient,
			SpokeClient:          fakeSpokeClient,
			Scheme:               scheme,
			SpokeClusterName:     pr.ClusterName,
			Logger:               utils.GetLogger(utils.GetZapLogger(true)),
			HubOperatorNamespace: "openshift-operators",
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: mirrorpeer1.Name,
			},
		}
		_, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Errorf("MirrorPeerReconciler Reconcile() failed. Error: %s", err)
		}

	}

}
