package utils

import (
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestGenerateBucketName(t *testing.T) {
	mirrorPeer := &multiclusterv1alpha1.MirrorPeer{
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
	bucket := GenerateBucketName(mirrorPeer)
	assert.Equal(t, "odrbucket-b1b922184baf", bucket)

	mirrorPeer = &multiclusterv1alpha1.MirrorPeer{
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name: "ocs-storagecluster",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name: "ocs-storagecluster",
					},
				},
			},
		},
	}
	bucket = GenerateBucketName(mirrorPeer)
	assert.Equal(t, "odrbucket-b1b922184baf", bucket)

	mirrorPeer = &multiclusterv1alpha1.MirrorPeer{
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster3",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name: "ocs-storagecluster",
					},
				},
				{
					ClusterName: "cluster4",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name: "ocs-storagecluster",
					},
				},
			},
		},
	}
	bucket = GenerateBucketName(mirrorPeer)
	assert.Equal(t, "odrbucket-db0233c5db46", bucket)
}
