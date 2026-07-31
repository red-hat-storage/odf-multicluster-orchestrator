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
	"fmt"
	"os"
	"testing"
	"time"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestMirrorPeerReconcilerReconcile(t *testing.T) {

	mirrorpeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
			},
		},
	}

	r := getFakeMirrorPeerReconciler(mirrorpeer)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "mirrorpeer",
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("MirrorPeerReconciler Reconcile() failed. Error: %s", err)
	}

	var mp multiclusterv1alpha1.MirrorPeer
	err = r.Get(ctx, req.NamespacedName, &mp)
	if err != nil {
		t.Errorf("Failed to get MirrorPeer. Error: %s", err)
	}

	if val, ok := mp.Labels[utils.HubRecoveryLabel]; !ok || val != "resource" {
		t.Errorf("MirrorPeer.Labels[%s] is not set correctly. Expected: %s, Actual: %s", utils.HubRecoveryLabel, "resource", val)
	}
}

func getFakeMirrorPeerReconciler(mirrorpeer *multiclusterv1alpha1.MirrorPeer) MirrorPeerReconciler {
	// Using the same scheme as manager to ensure consistency.
	// Using a different scheme for test might cause issues like
	// missing scheme in manager
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	managedcluster1 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Spec: clusterv1.ManagedClusterSpec{},
	}

	managedcluster2 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
		},
		Spec: clusterv1.ManagedClusterSpec{},
	}

	var odfClientInfoConfigMap = &corev1.ConfigMap{
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
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
			"cluster3_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"cluster4_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(mirrorpeer, &managedcluster1, &managedcluster2, odfClientInfoConfigMap).
		WithStatusSubresource(mirrorpeer).
		Build()

	r := MirrorPeerReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
	}
	return r
}

func TestProcessManagedClusterAddons(t *testing.T) {
	ctx := context.TODO()
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
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\"}}",
			"cluster3_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"cluster4_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
		},
	}
	// Create fake k8s client
	r := getFakeMirrorPeerReconciler(mirrorpeer)
	// Create fake secrets somehow
	if err := r.processManagedClusterAddon(ctx, mirrorpeer, odfClientInfoConfigMap.Data); err != nil {
		t.Error("Failed to create managed cluster addon")
	}

	clusterManagementAddOn := addonapiv1alpha1.ClusterManagementAddOn{}
	if err := r.Get(ctx, types.NamespacedName{
		Name: utils.TokenExchangeName,
	}, &clusterManagementAddOn); err != nil {
		t.Error("Failed to create ClusterManagementAddOn")
	}
	owner := clusterManagementAddOn.GetOwnerReferences()
	if owner[0].Name != mirrorpeer.Name {
		t.Error("Failed to add OwnerRefs to ClusterManagementAddOn")
	}

	for i := range mirrorpeer.Spec.Items {
		managedClusterAddon := addonapiv1alpha1.ManagedClusterAddOn{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      utils.TokenExchangeName,
			Namespace: mirrorpeer.Spec.Items[i].ClusterName,
		}, &managedClusterAddon); err != nil {
			t.Error("Failed to create ManagedClusterAddon")
		}
		owner := managedClusterAddon.GetOwnerReferences()
		if owner[0].Name != mirrorpeer.Name {
			t.Error("Failed to add OwnerRefs to ManagedClusterAddon")
		}
	}
}

func TestDeleteResources(t *testing.T) {
	ctx := context.TODO()

	mirrorpeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
			},
			ManageS3: true,
		},
	}
	r := getFakeMirrorPeerReconciler(mirrorpeer)

	if err := CreateFakeSecrets(mirrorpeer, r, ctx); err != nil {
		t.Error("Failed to create fake secrets", err)
	}

	internalSecrets, err := utils.FetchAllSecretsWithLabel(ctx, r.Client, "", utils.InternalLabel)
	if len(internalSecrets) < 2 {
		t.Error("Failed to delete Internal Secrets", err)
	}

	err = r.deleteSecrets(ctx, mirrorpeer)
	if err != nil {
		t.Error("Failed to delete resources", err)
	}
	for i := range mirrorpeer.Spec.Items {
		internalSecrets, err := utils.FetchAllSecretsWithLabel(ctx, r.Client, mirrorpeer.Spec.Items[i].ClusterName, utils.InternalLabel)
		if len(internalSecrets) > 0 {
			t.Error("Failed to delete Internal Secrets", err)
		}
	}

}

func CreateFakeSecrets(mirrorPeer *multiclusterv1alpha1.MirrorPeer, r MirrorPeerReconciler, ctx context.Context) error {
	for i := range mirrorPeer.Spec.Items {
		internalSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fake-internal-secret",
				Namespace: mirrorPeer.Spec.Items[i].ClusterName,
				Labels: map[string]string{
					utils.SecretLabelTypeKey: string(utils.InternalLabel),
				},
			},
			Type: corev1.SecretTypeOpaque,
		}
		if err := r.Create(ctx, internalSecret); err != nil {
			return err
		}
	}
	return nil
}

func makeClientInfoJSON(clientID, providerManagedCluster, namespace string) string {
	ci := ClientInfo{
		ClientID: clientID,
		ProviderInfo: ProviderInfo{
			Version:                    "4.19.0",
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

func TestRemoveClientIDFromClusterPairingConfigMap(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	logger := utils.GetLogger(utils.GetZapLogger(true))

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

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mw1, mw2).
			Build()

		err := removeClientIDFromClusterPairingConfigMap(ctx, fakeClient, logger, mirrorPeer, clientInfoMap)
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
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		err := removeClientIDFromClusterPairingConfigMap(ctx, fakeClient, logger, mirrorPeer, clientInfoMap)
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
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(emptyMW).
			Build()

		err := removeClientIDFromClusterPairingConfigMap(ctx, fakeClient, logger, mirrorPeer, clientInfoMap)
		if err != nil {
			t.Fatalf("should succeed when ManifestWork has no manifests, got: %v", err)
		}
	})

	t.Run("returns error when clientInfo is missing from configmap", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		incompleteClientInfoMap := map[string]string{
			"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		}

		err := removeClientIDFromClusterPairingConfigMap(ctx, fakeClient, logger, mirrorPeer, incompleteClientInfoMap)
		if err == nil {
			t.Fatal("expected error when client info is missing, got nil")
		}
	})
}

func TestDeleteMirrorPeer(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	logger := utils.GetLogger(utils.GetZapLogger(true))

	clientInfoMap := map[string]string{
		"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		"cluster2_test-storagecluster": makeClientInfoJSON("client-id-2", "provider2", "openshift-storage"),
	}

	newMirrorPeer := func() *multiclusterv1alpha1.MirrorPeer {
		return &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-mirrorpeer",
				Finalizers:        []string{mirrorPeerFinalizer},
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Type:     multiclusterv1alpha1.Async,
				ManageS3: true,
				Items: []multiclusterv1alpha1.PeerRef{
					{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
					{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
				},
			},
		}
	}

	t.Run("blocks deletion when DRPolicy references the MirrorPeer", func(t *testing.T) {
		mp := newMirrorPeer()
		drpolicy := &ramenv1alpha1.DRPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "test-drpolicy"},
			Spec:       ramenv1alpha1.DRPolicySpec{DRClusters: []string{"cluster1", "cluster2"}},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp, drpolicy).
			WithStatusSubresource(mp).
			Build()

		r := MirrorPeerReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			Logger:           logger,
			CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
		}

		_, err := r.deleteMirrorPeer(ctx, logger, mp, clientInfoMap)
		if err == nil {
			t.Fatal("expected error when DRPolicy references MirrorPeer, got nil")
		}
	})

	t.Run("blocks deletion when DRPolicy references clusters in reverse order", func(t *testing.T) {
		mp := newMirrorPeer()
		drpolicy := &ramenv1alpha1.DRPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "test-drpolicy"},
			Spec:       ramenv1alpha1.DRPolicySpec{DRClusters: []string{"cluster2", "cluster1"}},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp, drpolicy).
			WithStatusSubresource(mp).
			Build()

		r := MirrorPeerReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			Logger:           logger,
			CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
		}

		_, err := r.deleteMirrorPeer(ctx, logger, mp, clientInfoMap)
		if err == nil {
			t.Fatal("expected error when DRPolicy references MirrorPeer (reverse order), got nil")
		}
	})

	t.Run("requeues when spoke finalizer is present", func(t *testing.T) {
		mp := newMirrorPeer()
		mp.Finalizers = append(mp.Finalizers, fmt.Sprintf("some-prefix.%s", utils.SpokeMirrorPeerFinalizer))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp).
			WithStatusSubresource(mp).
			Build()

		r := MirrorPeerReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			Logger:           logger,
			CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
		}

		result, err := r.deleteMirrorPeer(ctx, logger, mp, clientInfoMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.RequeueAfter == 0 {
			t.Error("expected requeue when spoke finalizer is present")
		}
	})

	t.Run("removes clientID and finalizer on successful deletion", func(t *testing.T) {
		mp := newMirrorPeer()
		mw1 := makeStorageClientMappingManifestWork("provider1", "openshift-storage", map[string]string{
			"client-id-1":  "client-id-2",
			"other-client": "other-paired",
		})
		mw2 := makeStorageClientMappingManifestWork("provider2", "openshift-storage", map[string]string{
			"client-id-2": "client-id-1",
		})
		addon1 := makeManagedClusterAddOn(mp, "provider1")
		addon2 := makeManagedClusterAddOn(mp, "provider2")

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp, mw1, mw2, addon1, addon2).
			WithStatusSubresource(mp).
			Build()

		r := MirrorPeerReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			Logger:           logger,
			CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
		}

		result, err := r.deleteMirrorPeer(ctx, logger, mp, clientInfoMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.RequeueAfter != 0 {
			t.Error("expected no requeue on successful deletion")
		}

		// Verify clientID was removed from provider1 ManifestWork
		var updatedMW1 workv1.ManifestWork
		if err := fakeClient.Get(ctx, types.NamespacedName{Name: "storage-client-mapping", Namespace: "provider1"}, &updatedMW1); err != nil {
			t.Fatalf("failed to get ManifestWork: %v", err)
		}
		cm1, _ := utils.DecodeConfigMap(updatedMW1.Spec.Workload.Manifests[0].RawExtension.Raw)
		if _, exists := cm1.Data["client-id-1"]; exists {
			t.Error("client-id-1 should have been removed")
		}
		if cm1.Data["other-client"] != "other-paired" {
			t.Error("other-client entry should be preserved")
		}

		// Verify MirrorPeer was deleted (finalizer removed + DeletionTimestamp set means fake client deletes it)
		var updatedMP multiclusterv1alpha1.MirrorPeer
		err = fakeClient.Get(ctx, types.NamespacedName{Name: mp.Name}, &updatedMP)
		if err == nil {
			if controllerutil.ContainsFinalizer(&updatedMP, mirrorPeerFinalizer) {
				t.Error("hub finalizer should have been removed from MirrorPeer")
			}
		}
	})

	t.Run("proceeds when no DRPolicy exists", func(t *testing.T) {
		mp := newMirrorPeer()
		addon1 := makeManagedClusterAddOn(mp, "provider1")
		addon2 := makeManagedClusterAddOn(mp, "provider2")

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp, addon1, addon2).
			WithStatusSubresource(mp).
			Build()

		r := MirrorPeerReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			Logger:           logger,
			CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
		}

		_, err := r.deleteMirrorPeer(ctx, logger, mp, clientInfoMap)
		if err != nil {
			t.Fatalf("unexpected error when no DRPolicy exists: %v", err)
		}
	})
}

func TestDeleteMirrorPeerE2EFlow(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	logger := utils.GetLogger(utils.GetZapLogger(true))

	clientInfoMap := map[string]string{
		"cluster1_test-storagecluster": makeClientInfoJSON("client-id-1", "provider1", "openshift-storage"),
		"cluster2_test-storagecluster": makeClientInfoJSON("client-id-2", "provider2", "openshift-storage"),
	}

	t.Run("create then delete flow preserves other entries in configmap", func(t *testing.T) {
		mp := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-mirrorpeer",
				Finalizers:        []string{mirrorPeerFinalizer},
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{ClusterName: "cluster1", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
					{ClusterName: "cluster2", StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{Name: "test-storagecluster", Namespace: "test-namespace"}},
				},
			},
		}

		// Simulate pre-existing ManifestWorks with multiple client pairings
		mw1 := makeStorageClientMappingManifestWork("provider1", "openshift-storage", map[string]string{
			"client-id-1":     "client-id-2",
			"another-client1": "another-client2",
		})
		mw2 := makeStorageClientMappingManifestWork("provider2", "openshift-storage", map[string]string{
			"client-id-2":     "client-id-1",
			"another-client2": "another-client1",
		})

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(mp, mw1, mw2).
			WithStatusSubresource(mp).
			Build()

		err := removeClientIDFromClusterPairingConfigMap(ctx, fakeClient, logger, mp, clientInfoMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify provider1: client-id-1 removed, another-client1 preserved
		verifyConfigMapEntry(t, ctx, fakeClient, "provider1", "client-id-1", false)
		verifyConfigMapEntry(t, ctx, fakeClient, "provider1", "another-client1", true)

		// Verify provider2: client-id-2 removed, another-client2 preserved
		verifyConfigMapEntry(t, ctx, fakeClient, "provider2", "client-id-2", false)
		verifyConfigMapEntry(t, ctx, fakeClient, "provider2", "another-client2", true)
	})
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
