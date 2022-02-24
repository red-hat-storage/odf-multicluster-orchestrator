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
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMirrorPeerReconcilerReconcile(t *testing.T) {
	// Using the same scheme as manager to ensure consistency.
	// Using a different scheme for test might cause issues like
	// missing scheme in manager
	scheme := mgrScheme

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

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer, &managedcluster1, &managedcluster2).Build()
	ctx := context.TODO()

	r := MirrorPeerReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

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

	if val, ok := mp.Labels[hubRecoveryLabel]; !ok || val != "resource" {
		t.Errorf("MirrorPeer.Labels[%s] is not set correctly. Expected: %s, Actual: %s", hubRecoveryLabel, "resource", val)
	}
}
