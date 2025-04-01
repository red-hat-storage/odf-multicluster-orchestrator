package utils

import (
	"context"
	"os"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetPeerRefForProviderCluster(t *testing.T) {
	err := os.Setenv("POD_NAMESPACE", "openshift-storage")
	assert.NoError(t, err)

	mgrScheme := runtime.NewScheme()
	assert.NoError(t, clientgoscheme.AddToScheme(mgrScheme))
	assert.NoError(t, multiclusterv1alpha1.AddToScheme(mgrScheme))
	assert.NoError(t, clusterv1.AddToScheme(mgrScheme))

	mirrorpeer := &multiclusterv1alpha1.MirrorPeer{
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "ocs-storagecluster",
						Namespace: "openshift-storage",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "ocs-storagecluster",
						Namespace: "openshift-storage",
					},
				},
			},
		},
	}

	obj := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ODFInfoConfigMapName,
				Namespace: "openshift-storage",
			},
			Data: map[string]string{
				"openshift-storage_ocs-storagecluster.config.yaml": `
version: 4.18.0-126.stable
deploymentType: internal
clients:
- name: ocs-storagecluster
  clusterId: fb64afb3-d933-42a9-a25b-e119652a8db6
  clientId: 85331431-11c3-47ef-bd83-f5ff6d49be8c
- name: ocs-storagecluster
  clusterId: 4524a27b-03ab-4f35-9441-b51e1267a10b
  clientId: 85331431-11c3-47ef-bd83-f5ff6d49be8c
storageCluster:
  namespacedName:
    namespace: openshift-storage
    name: ocs-storagecluster
  storageProviderEndpoint: 172.30.63.56:50051
  cephClusterFSID: 747a4fb7-e130-4f25-bb4d-c9ceb8ef5764
  storageClusterUID: 4524a27b-03ab-4f35-9441-b51e1267a10b
  annotations:
    ocs.openshift.io/api-server-exported-address: baremetal-1.ocs-provider-server.openshift-storage.svc.clusterset.local:50051
    ocs.openshift.io/deployment-mode: provider
    uninstall.ocs.openshift.io/cleanup-policy: delete
    uninstall.ocs.openshift.io/mode: graceful
`,
			},
		},
		mirrorpeer,
		&clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
				Labels: map[string]string{
					"clusterID": "fb64afb3-d933-42a9-a25b-e119652a8db6",
				},
			},
		},
		&clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster2",
				Labels: map[string]string{
					"clusterID": "4524a27b-03ab-4f35-9441-b51e1267a10b",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(mgrScheme).WithRuntimeObjects(obj...).Build()
	refList, err := GetPeerRefForProviderCluster(context.TODO(), c, c, mirrorpeer)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(refList))
	assert.Equal(t, "cluster1", refList[0].ClusterName)
	assert.NotEqual(t, "cluster2", refList[0].ClusterName)
	assert.Equal(t, "cluster2", refList[1].ClusterName)
	assert.NotEqual(t, "cluster1", refList[1].ClusterName)
	err = os.Unsetenv("POD_NAMESPACE")
	assert.NoError(t, err)
}
