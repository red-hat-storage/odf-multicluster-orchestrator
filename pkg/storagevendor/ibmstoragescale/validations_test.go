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

package ibmstoragescale

import (
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"
)

func mirrorPeerWithItems(items ...multiclusterv1alpha1.PeerRef) *multiclusterv1alpha1.MirrorPeer {
	return &multiclusterv1alpha1.MirrorPeer{
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: items,
		},
	}
}

func peerRef(clusterName, storageClusterName string) multiclusterv1alpha1.PeerRef {
	return multiclusterv1alpha1.PeerRef{
		ClusterName: clusterName,
		StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
			Name: storageClusterName,
		},
	}
}

func infoEntry(t *testing.T, version string) string {
	t.Helper()
	return `{"version":"` + version + `","operatorNamespace":"ibm-spectrum-scale","cluster":{"name":"scale-cluster"},"sourceManifests":["sm-a"]}`
}

func TestArePeerVersionsCompatible(t *testing.T) {
	itemA := peerRef("cluster-a", "scale-cluster-a")
	itemB := peerRef("cluster-b", "scale-cluster-b")

	tests := []struct {
		name    string
		peer    *multiclusterv1alpha1.MirrorPeer
		infoMap map[string]string
		wantErr bool
	}{
		{
			name: "compatible versions",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.2.1"),
			},
			wantErr: false,
		},
		{
			name: "incompatible major.minor versions",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.3.0"),
			},
			wantErr: true,
		},
		{
			name: "cluster not discovered yet",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
			},
			wantErr: true,
		},
		{
			name: "empty version for a cluster",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, ""),
			},
			wantErr: true,
		},
		{
			name:    "wrong number of items",
			peer:    mirrorPeerWithItems(itemA),
			infoMap: map[string]string{},
			wantErr: true,
		},
		{
			name: "malformed info JSON",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): "not-json",
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.2.0"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := arePeerVersionsCompatible(tt.peer, tt.infoMap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("arePeerVersionsCompatible() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
