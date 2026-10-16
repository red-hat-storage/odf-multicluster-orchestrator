//go:build unit
// +build unit

package managedclusterview

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	"github.com/google/uuid"
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

func TestCreateOrUpdateODFConfigMap(t *testing.T) {

	s := scheme.Scheme
	_ = viewv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)

	c := fake.NewClientBuilder().WithScheme(s).Build()
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterViewReconciler{
		Client:           c,
		Logger:           logger,
		Scheme:           s,
		CurrentNamespace: "openshift-operators",
	}

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
		mc1 := createManagedCluster("cluster1", "cluster1")
		mc2 := createManagedCluster("cluster2", "cluster2")
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
  annotations:
        ocs.openshift.io/api-server-exported-address: ""
  infoCephBlockPools:
    - name: "pool1"
      mirrorEnabled: true
    - name: "pool2"
      mirrorEnabled: false
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc1, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("test-view", "cluster1", data, ownerRefs)

		ctx := context.TODO()
		err = c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = reconciler.createOrUpdateODFConfigMap(ctx, *mcv)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: odf.ClientInfoConfigMapName, Namespace: utils.GetEnv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		expectedData := map[string]string{
			"cluster1_client1": `{"clusterId":"cluster1","name":"client1","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","providerManagedClusterName":"cluster1","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"7a3d6b81-a55d-44fe-84d0-46c67cd395ca","storageProviderPublicEndpoint":"","infoCephBlockPools":[{"name":"pool1","mirrorEnabled":true},{"name":"pool2","mirrorEnabled":false}]},"clientManagedClusterName":"cluster1","clientId":"client1"}`,
		}

		assert.Equal(t, expectedData, cm.Data)
		assert.Equal(t, 1, len(cm.OwnerReferences))
		assert.Equal(t, mc1.Name, cm.OwnerReferences[0].Name)
		assert.Equal(t, "ManagedCluster", cm.OwnerReferences[0].Kind)
	})

	t.Run("Update ConfigMap with MCV in cluster2", func(t *testing.T) {
		mc2 := createManagedCluster("cluster2", "cluster2")
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
  annotations:
        ocs.openshift.io/api-server-exported-address: ""
  infoCephBlockPools:
    - name: "pool1"
      mirrorEnabled: true
    - name: "pool2"
      mirrorEnabled: false
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc2, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("new-view", "cluster2", data, ownerRefs)

		err := c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = reconciler.createOrUpdateODFConfigMap(ctx, *mcv)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: odf.ClientInfoConfigMapName, Namespace: utils.GetEnv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		expectedData := map[string]string{
			"cluster1_client1": `{"clusterId":"cluster1","name":"client1","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","providerManagedClusterName":"cluster1","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"7a3d6b81-a55d-44fe-84d0-46c67cd395ca","storageProviderPublicEndpoint":"","infoCephBlockPools":[{"name":"pool1","mirrorEnabled":true},{"name":"pool2","mirrorEnabled":false}]},"clientManagedClusterName":"cluster1","clientId":"client1"}`,
			"cluster2_client2": `{"clusterId":"cluster2","name":"client2","providerInfo":{"version":"4.Y.Z","deploymentType":"internal","providerManagedClusterName":"cluster2","namespacedName":{"Namespace":"openshift-storage","Name":"ocs-storagecluster"},"storageProviderEndpoint":"","cephClusterFSID":"8b3d6b81-b55d-55fe-94d0-56c67cd495ca","storageProviderPublicEndpoint":"","infoCephBlockPools":[{"name":"pool1","mirrorEnabled":true},{"name":"pool2","mirrorEnabled":false}]},"clientManagedClusterName":"cluster2","clientId":"client2"}`,
		}

		assert.Equal(t, expectedData, cm.Data)
		// Should have 2 owner references: 2 ManagedClusters (one from each cluster)
		assert.Equal(t, 2, len(cm.OwnerReferences))
	})
}

func TestCreateOrUpdateFAConfigMap(t *testing.T) {

	s := scheme.Scheme
	_ = viewv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)

	c := fake.NewClientBuilder().WithScheme(s).Build()
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterViewReconciler{
		Client:           c,
		Logger:           logger,
		Scheme:           s,
		CurrentNamespace: "openshift-operators",
	}

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

	t.Run("Create FA ConfigMap with MCV in fa-cluster1", func(t *testing.T) {
		mc1 := createManagedCluster("fa-cluster1", "fa-cluster1")
		err := c.Create(context.TODO(), mc1)
		assert.NoError(t, err)

		data := map[string]string{
			"cnsa-cluster.config.yaml": `version: "1.0.0"
operator-namespace: ibm-spectrum-scale
cluster:
  name: "cnsa-cluster"
sourceManifests:
  - "source-manifest-1"
  - "source-manifest-2"
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc1, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("fa-test-view", "fa-cluster1", data, ownerRefs)

		ctx := context.TODO()
		err = c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = reconciler.createOrUpdateFAConfigMap(ctx, *mcv)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: "ibm-storage-scale-info", Namespace: utils.GetEnv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		// Verify the key format: managedclustername_faclustername (using underscore separator)
		expectedKey := "fa-cluster1_cnsa-cluster"
		t.Logf("ConfigMap Data keys: %v", cm.Data)
		assert.Contains(t, cm.Data, expectedKey)

		// Verify the FA info is correctly marshaled
		var faInfo map[string]interface{}
		err = json.Unmarshal([]byte(cm.Data[expectedKey]), &faInfo)
		assert.NoError(t, err)
		assert.Equal(t, "1.0.0", faInfo["version"])
		assert.Equal(t, "ibm-spectrum-scale", faInfo["operatorNamespace"])

		if faInfo["cluster"] != nil {
			cluster := faInfo["cluster"].(map[string]interface{})
			assert.Equal(t, "cnsa-cluster", cluster["name"])
		} else {
			t.Fatal("cluster field is nil")
		}

		sourceManifests := faInfo["sourceManifests"].([]interface{})
		assert.Equal(t, 2, len(sourceManifests))
		assert.Equal(t, "source-manifest-1", sourceManifests[0])
		assert.Equal(t, "source-manifest-2", sourceManifests[1])

		// Verify owner reference
		assert.Equal(t, 1, len(cm.OwnerReferences))
		assert.Equal(t, mc1.Name, cm.OwnerReferences[0].Name)
		assert.Equal(t, "ManagedCluster", cm.OwnerReferences[0].Kind)
	})

	t.Run("Update FA ConfigMap with MCV in fa-cluster2", func(t *testing.T) {
		mc2 := createManagedCluster("fa-cluster2", "fa-cluster2")
		err := c.Create(context.TODO(), mc2)
		assert.NoError(t, err)

		ctx := context.TODO()
		data := map[string]string{
			"production-cluster.config.yaml": `version: "2.0.0"
operator-namespace: ibm-spectrum-scale
cluster:
  name: "production-cluster"
sourceManifests:
  - "prod-manifest-1"
`,
		}
		ownerRefs := []metav1.OwnerReference{
			*metav1.NewControllerRef(mc2, clusterv1.SchemeGroupVersion.WithKind("ManagedCluster")),
		}
		mcv := createManagedClusterView("fa-new-view", "fa-cluster2", data, ownerRefs)

		err = c.Create(ctx, mcv)
		assert.NoError(t, err)

		err = reconciler.createOrUpdateFAConfigMap(ctx, *mcv)
		assert.NoError(t, err)

		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, types.NamespacedName{Name: "ibm-storage-scale-info", Namespace: utils.GetEnv("POD_NAMESPACE")}, cm)
		assert.NoError(t, err)
		assert.NotNil(t, cm)

		// Should have both clusters now
		assert.Equal(t, 2, len(cm.Data))
		assert.Contains(t, cm.Data, "fa-cluster1_cnsa-cluster")
		assert.Contains(t, cm.Data, "fa-cluster2_production-cluster")

		// Should have 2 owner references: 2 ManagedClusters (one from each cluster)
		assert.Equal(t, 2, len(cm.OwnerReferences))
	})
}
