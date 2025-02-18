//go:build unit
// +build unit

/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

package controllers

import (
	"context"
	"os"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMirrorPeerReconcilerReconcile(t *testing.T) {

	mirrorpeer := multiclusterv1alpha1.MirrorPeer{
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

func getFakeMirrorPeerReconciler(mirrorpeer multiclusterv1alpha1.MirrorPeer) MirrorPeerReconciler {
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
		Data: map[string]string{
			"cluster1_test-storagecluster": `
{
    "clusterId": "cluster1",
    "name": "cluster1",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "provider-1",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluster1",
    "clientId": "client-1"
}
`,
			"cluster2_test-storagecluster": `
{
    "clusterId": "cluster2",
    "name": "cluster2",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "provider-2",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluter2",
    "clientId": "client-2"
}
`,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer, &managedcluster1, &managedcluster2, odfClientInfoConfigMap).Build()

	r := MirrorPeerReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Logger: utils.GetLogger(utils.GetZapLogger(true)),
	}
	return r
}

func TestProcessManagedClusterAddons(t *testing.T) {
	ctx := context.TODO()
	mirrorpeer := multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-test",
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
	// Create fake k8s client
	r := getFakeMirrorPeerReconciler(mirrorpeer)
	// Create fake secrets somehow
	if err := r.processManagedClusterAddon(ctx, mirrorpeer); err != nil {
		t.Error("Failed to create managed cluster addon.", err)
	}

	managedClusterAddon1 := addonapiv1alpha1.ManagedClusterAddOn{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      setup.TokenExchangeName,
		Namespace: "provider-1",
	}, &managedClusterAddon1); err != nil {
		t.Error("Failed to get ManagedClusterAddon.", err)
	}
	owner1 := managedClusterAddon1.GetOwnerReferences()
	if owner1[0].Name != mirrorpeer.Name {
		t.Error("Failed to add OwnerRefs to ManagedClusterAddon.")
	}

	managedClusterAddon2 := addonapiv1alpha1.ManagedClusterAddOn{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      setup.TokenExchangeName,
		Namespace: "provider-1",
	}, &managedClusterAddon2); err != nil {
		t.Error("Failed to get ManagedClusterAddon.", err)
	}
	owner2 := managedClusterAddon2.GetOwnerReferences()
	if owner2[0].Name != mirrorpeer.Name {
		t.Error("Failed to add OwnerRefs to ManagedClusterAddon.")
	}
}
