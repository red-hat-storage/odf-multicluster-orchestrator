//go:build unit
// +build unit

package controllers

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateOrUpdateConfigMap(t *testing.T) {

	s := scheme.Scheme
	_ = viewv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)

	c := fake.NewClientBuilder().WithScheme(s).Build()
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	logger := utils.GetLogger(utils.GetZapLogger(true))

	createManagedClusterView := func(name, namespace string, data map[string]string, ownerRefs []metav1.OwnerReference) *viewv1beta1.ManagedClusterView {
		raw, _ := json.Marshal(map[string]interface{}{
			"data": data,
		})
		return &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Namespace:       namespace,
				UID:             types.UID(uuid.New().String()),
				OwnerReferences: ownerRefs,
			},
			Status: viewv1beta1.ViewStatus{
				Result: runtime.RawExtension{Raw: raw},
			},
		}
	}

	createManagedCluster := func(name, clusterID string) *clusterv1.ManagedCluster {
		return &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				UID:  types.UID(uuid.New().String()),
				Labels: map[string]string{
					"clusterID": clusterID,
				},
			},
		}
	}

	t.Run("Create ConfigMap with MCV in cluster1", func(t *testing.T) {
		mc1 := createManagedCluster("cluster1-name", "cluster1")
		mc2 := createManagedCluster("cluster2-name", "cluster2")
		err := c.Create(context.TODO(), mc1)
		assert.NoError(t, err)
		err = c.Create(context.TODO(), mc2)
		assert.NoError(t, err)

		data := map[string]string{
			"openshift-storage_ocs-storagecluster.config.yaml": `
version: "4.Y.Z"
deploymentType: "internal"
clients:
  - name: "client1"
    clusterId: "cluster1"
    clientId: "client1"
storageCluster:
  namespacedName:
    name: "ocs-storagecluster"
    namespace: "openshift-storage"
  storageProviderEndpoint: ""
  cephClusterFSID: "7a3d6b81-a55d-44fe-84d0-46c67cd395ca"
storageSystemName: "ocs-storagecluster-storagesystem"
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc1, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("test-view", "cluster1", data, ownerRefs)

		ctx := context.TODO()
		err = c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = createOrUpdateConfigMap(ctx, c, *mcv, logger)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: ClientInfoConfigMapName, Namespace: os.Getenv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		expectedData := map[string]string{
			"cluster1-name/client1": `{"clusterId":"cluster1","name":"client1","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","storageSystemName":"ocs-storagecluster-storagesystem","providerManagedClusterName":"cluster1","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"7a3d6b81-a55d-44fe-84d0-46c67cd395ca"},"clientManagedClusterName":"cluster1-name","clientId":"client1"}`,
		}

		assert.Equal(t, expectedData, cm.Data)
		assert.Equal(t, 1, len(cm.OwnerReferences))
		assert.Equal(t, mc1.Name, cm.OwnerReferences[0].Name)
		assert.Equal(t, "ManagedCluster", cm.OwnerReferences[0].Kind)
		assert.Equal(t, clusterv1.GroupVersion.String(), cm.OwnerReferences[0].APIVersion)
	})

	t.Run("Update ConfigMap with MCV in cluster2", func(t *testing.T) {
		mc2 := createManagedCluster("cluster2-name", "cluster2")
		ctx := context.TODO()
		data := map[string]string{
			"openshift-storage_ocs-storagecluster.config.yaml": `
version: "4.Y.Z"
deploymentType: "internal"
clients:
  - name: "client2"
    clusterId: "cluster2"
    clientId: "client2"
storageCluster:
  namespacedName:
    name: "ocs-storagecluster"
    namespace: "openshift-storage"
  storageProviderEndpoint: ""
  cephClusterFSID: "8b3d6b81-b55d-55fe-94d0-56c67cd495ca"
storageSystemName: "ocs-storagecluster-storagesystem"
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc2, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("new-view", "cluster2", data, ownerRefs)

		err := c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = createOrUpdateConfigMap(ctx, c, *mcv, logger)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: ClientInfoConfigMapName, Namespace: os.Getenv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		expectedData := map[string]string{
			"cluster1-name/client1": `{"clusterId":"cluster1","name":"client1","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","storageSystemName":"ocs-storagecluster-storagesystem","providerManagedClusterName":"cluster1","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"7a3d6b81-a55d-44fe-84d0-46c67cd395ca"},"clientManagedClusterName":"cluster1-name","clientId":"client1"}`,
			"cluster2-name/client2": `{"clusterId":"cluster2","name":"client2","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","storageSystemName":"ocs-storagecluster-storagesystem","providerManagedClusterName":"cluster2","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"8b3d6b81-b55d-55fe-94d0-56c67cd495ca"},"clientManagedClusterName":"cluster2-name","clientId":"client2"}`,
		}

		assert.Equal(t, expectedData, cm.Data)
		assert.Equal(t, 2, len(cm.OwnerReferences))
	})
}
