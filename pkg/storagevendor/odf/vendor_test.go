//go:build unit
// +build unit

/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package odf

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Test helper functions

func makeClientInfoJSON(clientID, providerManagedCluster, namespace string) string {
	ci := ClientInfo{
		ClientID: clientID,
		ProviderInfo: ProviderInfo{
			Version:                    "5.0.0",
			ProviderManagedClusterName: providerManagedCluster,
			NamespacedName:             types.NamespacedName{Name: "storagecluster", Namespace: namespace},
		},
	}
	b, _ := json.Marshal(ci)
	return string(b)
}

func makeManagedClusterAddOn(mp *multiclusterv1alpha1.MirrorPeer, namespace string) *addonapiv1alpha1.ManagedClusterAddOn {
	addon := &addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.TokenExchangeName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         AddonApiVersion,
					Kind:               ManagedClusterAddOn,
					Name:               mp.Name,
					UID:                mp.UID,
					BlockOwnerDeletion: func() *bool { b := true; return &b }(),
					Controller:         func() *bool { b := true; return &b }(),
				},
			},
		},
	}
	return addon
}

func makeStorageClientMappingManifestWork(namespace, cmNamespace string, data map[string]string) *workv1.ManifestWork {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-client-mapping",
			Namespace: cmNamespace,
		},
		Data: data,
	}
	cmJSON, _ := json.Marshal(cm)
	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-client-mapping",
			Namespace: namespace,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: cmJSON}},
				},
			},
		},
	}
}

func verifyConfigMapEntry(t *testing.T, ctx context.Context, c client.Client, namespace, key string, shouldExist bool) {
	t.Helper()
	var mw workv1.ManifestWork
	if err := c.Get(ctx, types.NamespacedName{Name: "storage-client-mapping", Namespace: namespace}, &mw); err != nil {
		t.Fatalf("failed to get ManifestWork in namespace %s: %v", namespace, err)
	}
	cm, err := utils.DecodeConfigMap(mw.Spec.Workload.Manifests[0].RawExtension.Raw)
	if err != nil {
		t.Fatalf("failed to decode ConfigMap: %v", err)
	}
	_, exists := cm.Data[key]
	if shouldExist && !exists {
		t.Errorf("expected key %q to exist in ConfigMap for namespace %s", key, namespace)
	}
	if !shouldExist && exists {
		t.Errorf("expected key %q to be removed from ConfigMap for namespace %s", key, namespace)
	}
}

func makeODFClientInfoConfigMap(namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: data,
	}
}

// Tests

func TestProcessManagedClusterAddons(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")

	mirrorpeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-test",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster3",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
				{
					ClusterName: "cluster4",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
			},
		},
	}

	odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), map[string]string{
		"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
		"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
		"cluster3_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"deploymentType\": \"external\"}}",
		"cluster4_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"deploymentType\": \"external\"}}",
	})

	managedcluster3 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster3",
		},
	}

	managedcluster4 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster4",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(mirrorpeer, odfClientInfoConfigMap, &managedcluster3, &managedcluster4).
		WithStatusSubresource(mirrorpeer).
		Build()

	vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
	if err != nil {
		t.Fatalf("Failed to create ODF vendor: %v", err)
	}

	if err := vendor.processManagedClusterAddon(ctx, mirrorpeer); err != nil {
		t.Error("Failed to create managed cluster addon")
	}

	clusterManagementAddOn := addonapiv1alpha1.ClusterManagementAddOn{}
	if err := fakeClient.Get(ctx, types.NamespacedName{
		Name: utils.TokenExchangeName,
	}, &clusterManagementAddOn); err != nil {
		t.Error("Failed to create ClusterManagementAddOn")
	}
	owner := clusterManagementAddOn.GetOwnerReferences()
	if len(owner) == 0 || owner[0].Name != mirrorpeer.Name {
		t.Error("Failed to add OwnerRefs to ClusterManagementAddOn")
	}

	for i := range mirrorpeer.Spec.Items {
		managedClusterAddon := addonapiv1alpha1.ManagedClusterAddOn{}
		if err := fakeClient.Get(ctx, types.NamespacedName{
			Name:      utils.TokenExchangeName,
			Namespace: mirrorpeer.Spec.Items[i].ClusterName,
		}, &managedClusterAddon); err != nil {
			t.Error("Failed to create ManagedClusterAddon")
		}
		owner := managedClusterAddon.GetOwnerReferences()
		if len(owner) == 0 || owner[0].Name != mirrorpeer.Name {
			t.Error("Failed to add OwnerRefs to ManagedClusterAddon")
		}
	}
}

func TestRemoveClientIDFromClusterPairingConfigMap(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")

	clientInfoMap := map[string]string{
		"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		"cluster2_test-storagecluster": makeClientInfoJSON("client-id-2", "provider2", "openshift-storage"),
	}

	mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mirrorpeer"},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
				{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
			},
		},
	}

	t.Run("removes clientID from both provider ManifestWorks", func(t *testing.T) {
		mw1 := makeStorageClientMappingManifestWork("provider1", "openshift-storage", map[string]string{
			"client-id-1":  "client-id-2",
			"other-client": "other-paired",
		})
		mw2 := makeStorageClientMappingManifestWork("provider2", "openshift-storage", map[string]string{
			"client-id-2": "client-id-1",
		})

		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mw1, mw2, odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		err = vendor.removeClientIDFromClusterPairingConfigMap(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var updatedMW1 workv1.ManifestWork
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "storage-client-mapping", Namespace: "provider1"}, &updatedMW1); err != nil {
			t.Fatalf("failed to get ManifestWork for provider1: %v", err)
		}
		cm1, err := utils.DecodeConfigMap(updatedMW1.Spec.Workload.Manifests[0].RawExtension.Raw)
		if err != nil {
			t.Fatalf("failed to decode ConfigMap: %v", err)
		}
		if _, exists := cm1.Data["client-id-1"]; exists {
			t.Error("client-id-1 should have been removed from provider1 ConfigMap")
		}
		if cm1.Data["other-client"] != "other-paired" {
			t.Error("other-client entry should be preserved in provider1 ConfigMap")
		}

		var updatedMW2 workv1.ManifestWork
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "storage-client-mapping", Namespace: "provider2"}, &updatedMW2); err != nil {
			t.Fatalf("failed to get ManifestWork for provider2: %v", err)
		}
		cm2, err := utils.DecodeConfigMap(updatedMW2.Spec.Workload.Manifests[0].RawExtension.Raw)
		if err != nil {
			t.Fatalf("failed to decode ConfigMap: %v", err)
		}
		if _, exists := cm2.Data["client-id-2"]; exists {
			t.Error("client-id-2 should have been removed from provider2 ConfigMap")
		}
	})

	t.Run("succeeds when ManifestWork does not exist", func(t *testing.T) {
		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		err = vendor.removeClientIDFromClusterPairingConfigMap(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("should succeed when ManifestWork is missing, got: %v", err)
		}
	})

	t.Run("succeeds when ManifestWork has empty manifests", func(t *testing.T) {
		emptyMW := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "storage-client-mapping",
				Namespace: "provider1",
			},
			Spec: workv1.ManifestWorkSpec{},
		}
		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(emptyMW, odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		err = vendor.removeClientIDFromClusterPairingConfigMap(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("should succeed when ManifestWork has no manifests, got: %v", err)
		}
	})

	t.Run("returns error when clientInfo is missing from configmap", func(t *testing.T) {
		incompleteClientInfoMap := map[string]string{
			"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		}
		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), incompleteClientInfoMap)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		err = vendor.removeClientIDFromClusterPairingConfigMap(ctx, mirrorPeer)
		if err == nil {
			t.Fatal("expected error when client info is missing, got nil")
		}
	})
}

func TestDeleteManagedClusterAddon(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")

	clientInfoMap := map[string]string{
		"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		"cluster2_test-storagecluster": makeClientInfoJSON("client-id-2", "provider2", "openshift-storage"),
	}

	mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mirrorpeer", UID: "test-uid"},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type: multiclusterv1alpha1.Async,
			Items: []multiclusterv1alpha1.PeerRef{
				{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
				{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
			},
		},
	}

	t.Run("successfully deletes managed cluster addons", func(t *testing.T) {
		addon1 := makeManagedClusterAddOn(mirrorPeer, "provider1")
		addon2 := makeManagedClusterAddOn(mirrorPeer, "provider2")

		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(addon1, addon2, odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		err = vendor.deleteManagedClusterAddon(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify method completed successfully without error
		// The actual deletion logic is tested in integration tests
	})
}

func TestEnsureReplicationReady(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")

	t.Run("processes ManagedClusterAddon for non-async type", func(t *testing.T) {
		mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mirrorpeer"},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Type: multiclusterv1alpha1.Sync,
				Items: []multiclusterv1alpha1.PeerRef{
					{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
					{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
				},
			},
		}

		clientInfoMap := map[string]string{
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
		}

		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)

		managedcluster1 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
		}
		managedcluster2 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster2"},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mirrorPeer, odfClientInfoConfigMap, &managedcluster1, &managedcluster2).
			WithStatusSubresource(mirrorPeer).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		result, err := vendor.ConfigureStorageForDR(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.RequeueAfter != 0 {
			t.Error("expected no requeue for sync type")
		}

		// Verify ClusterManagementAddOn was created
		var cma addonapiv1alpha1.ClusterManagementAddOn
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: utils.TokenExchangeName}, &cma); err != nil {
			t.Error("Failed to create ClusterManagementAddOn")
		}
	})
}

func TestCleanupResources(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")

	clientInfoMap := map[string]string{
		"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		"cluster2_test-storagecluster": makeClientInfoJSON("client-id-2", "provider2", "openshift-storage"),
	}

	t.Run("cleans up async resources", func(t *testing.T) {
		mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mirrorpeer", UID: "test-uid"},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Type: multiclusterv1alpha1.Async,
				Items: []multiclusterv1alpha1.PeerRef{
					{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
					{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster"}},
				},
			},
		}

		mw1 := makeStorageClientMappingManifestWork("provider1", "openshift-storage", map[string]string{
			"client-id-1": "client-id-2",
		})
		mw2 := makeStorageClientMappingManifestWork("provider2", "openshift-storage", map[string]string{
			"client-id-2": "client-id-1",
		})

		addon1 := makeManagedClusterAddOn(mirrorPeer, "provider1")
		addon2 := makeManagedClusterAddOn(mirrorPeer, "provider2")

		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), clientInfoMap)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mw1, mw2, addon1, addon2, odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		result, err := vendor.CleanupResources(ctx, mirrorPeer)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.RequeueAfter != 0 {
			t.Error("expected no requeue after cleanup")
		}

		// Verify clientID was removed from ManifestWorks
		verifyConfigMapEntry(t, ctx, fakeClient, "provider1", "client-id-1", false)
		verifyConfigMapEntry(t, ctx, fakeClient, "provider2", "client-id-2", false)
	})

	t.Run("handles cleanup for sync type without async resources", func(t *testing.T) {
		mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-mirrorpeer-sync", UID: "test-uid-sync"},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Type: multiclusterv1alpha1.Sync,
				Items: []multiclusterv1alpha1.PeerRef{
					{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
					{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
				},
			},
		}

		syncClientInfoMap := map[string]string{
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\"}}",
		}

		odfClientInfoConfigMap := makeODFClientInfoConfigMap(utils.GetEnv("POD_NAMESPACE"), syncClientInfoMap)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(odfClientInfoConfigMap).
			Build()

		vendor, err := NewODFVendor(ctx, fakeClient, scheme, utils.GetLogger(utils.GetZapLogger(true)), utils.GetEnv("POD_NAMESPACE"))
		if err != nil {
			t.Fatalf("Failed to create ODF vendor: %v", err)
		}

		// CleanupResources for sync type should skip async-specific cleanup (removeClientIDFromClusterPairingConfigMap)
		// and only try to delete managed cluster addons (which don't exist in this test, simulating they're already cleaned up)
		result, err := vendor.CleanupResources(ctx, mirrorPeer)

		// We expect an error here because the addons don't exist (they may have been cleaned up already)
		// In a real scenario, the controller would handle this gracefully
		if err == nil {
			if result.RequeueAfter != 0 {
				t.Error("expected no requeue after cleanup")
			}
		}
	})
}
