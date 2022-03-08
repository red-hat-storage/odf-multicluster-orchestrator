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
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var peerRef = multiclusterv1alpha1.PeerRef{
	ClusterName: "test-cluster",
	StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
		Name:      "test-storagecluster",
		Namespace: "test-namespace",
	},
}

func TestUndefinedMirrorPeerSpec(t *testing.T) {
	type args struct {
		spec multiclusterv1alpha1.MirrorPeerSpec
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "With empty MirrorPeer.Spec",
			args: args{
				spec: multiclusterv1alpha1.MirrorPeerSpec{},
			},
			wantErr: true,
		},
		{
			name: "With non-empty MirrorPeer.Spec",
			args: args{
				spec: multiclusterv1alpha1.MirrorPeerSpec{
					Items: []multiclusterv1alpha1.PeerRef{
						peerRef,
						peerRef,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := undefinedMirrorPeerSpec(tt.args.spec); (err != nil) != tt.wantErr {
				t.Errorf("undefinedMirrorPeerSpec() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUniqueSpecItems(t *testing.T) {
	type args struct {
		spec multiclusterv1alpha1.MirrorPeerSpec
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "With unique MirrorPeer.Spec.Items",
			args: args{
				spec: multiclusterv1alpha1.MirrorPeerSpec{
					Items: []multiclusterv1alpha1.PeerRef{
						peerRef,
						{
							ClusterName: "test-unique-cluster",
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-unique-storagecluster",
								Namespace: "test-unique-namespace",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Without unique MirrorPeer.Spec.Items",
			args: args{
				spec: multiclusterv1alpha1.MirrorPeerSpec{
					Items: []multiclusterv1alpha1.PeerRef{
						peerRef,
						peerRef,
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := uniqueSpecItems(tt.args.spec); (err != nil) != tt.wantErr {
				t.Errorf("uniqueSpecItems() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEmptySpecItems(t *testing.T) {
	type args struct {
		peerRef multiclusterv1alpha1.PeerRef
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "With empty MirrorPeer.Spec.Items value",
			args: args{
				peerRef: multiclusterv1alpha1.PeerRef{
					ClusterName: "",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "",
						Namespace: "",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Without empty MirrorPeer.Spec.Items value",
			args: args{
				peerRef: peerRef,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := emptySpecItems(tt.args.peerRef); (err != nil) != tt.wantErr {
				t.Errorf("emptySpecItems() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsManagedCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	err := clusterv1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}

	managedcluster := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Spec: clusterv1.ManagedClusterSpec{},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&managedcluster).Build()
	ctx := context.TODO()

	type args struct {
		ctx         context.Context
		client      client.Client
		clusterName string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "With clusterName == ManagedCluster.Name",
			args: args{
				ctx:         ctx,
				client:      fakeClient,
				clusterName: managedcluster.Name,
			},
			wantErr: false,
		},
		{
			name: "With clusterName != ManagedCluster.Name",
			args: args{
				ctx:         ctx,
				client:      fakeClient,
				clusterName: "cluster2",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := isManagedCluster(tt.args.ctx, tt.args.client, tt.args.clusterName); (err != nil) != tt.wantErr {
				t.Errorf("isManagedCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
