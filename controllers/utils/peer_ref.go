package utils

import (
	"context"
	"fmt"
	"os"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"gopkg.in/yaml.v2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PeerRefType string

const (
	// PeerRefTypeStorageClient represents a storage client
	PeerRefTypeStorageClient PeerRefType = "StorageClient"

	// PeerRefTypeStorageCluster represents a storage cluster
	PeerRefTypeStorageCluster PeerRefType = "StorageCluster"

	// PeerRefTypeUnknown represents an unknown type
	PeerRefTypeUnknown PeerRefType = "Unknown"
)

// DoesAnotherMirrorPeerPointToPeerRef checks if another mirrorpeer is pointing to the provided peer ref
func DoesAnotherMirrorPeerPointToPeerRef(ctx context.Context, rc client.Client, peerRef *multiclusterv1alpha1.PeerRef) (bool, error) {
	mirrorPeers, err := FetchAllMirrorPeers(ctx, rc)
	if err != nil {
		return false, err
	}
	count := 0
	for i := range mirrorPeers {
		if ContainsPeerRef(mirrorPeers[i].Spec.Items, peerRef) {
			count++
		}
	}

	return count > 1, nil
}

// GetPeerRefForSpokeCluster returns the peer ref for the cluster name
func GetPeerRefForSpokeCluster(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) (*multiclusterv1alpha1.PeerRef, error) {
	for _, v := range mp.Spec.Items {
		if v.ClusterName == spokeClusterName {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("PeerRef for cluster %s under mirrorpeer %s not found", spokeClusterName, mp.Name)
}

func getPeerRefType(ctx context.Context, c client.Client, peerRef multiclusterv1alpha1.PeerRef, isManagedCluster bool) (PeerRefType, error) {
	if isManagedCluster {
		cm, err := GetODFInfoConfigMap(ctx, c, peerRef.StorageClusterRef.Namespace)
		if err != nil {
			return PeerRefTypeUnknown, fmt.Errorf("failed to get ODF Info ConfigMap for namespace %s: %w", peerRef.StorageClusterRef.Namespace, err)
		}
		var odfInfo ocsv1alpha1.OdfInfoData
		for key, value := range cm.Data {
			namespacedName := SplitKeyForNamespacedName(key)
			if namespacedName.Name == peerRef.StorageClusterRef.Name {
				err := yaml.Unmarshal([]byte(value), &odfInfo)
				if err != nil {
					return PeerRefTypeUnknown, fmt.Errorf("failed to unmarshal ODF info data for key %s: %w", key, err)
				}

				for _, client := range odfInfo.Clients {
					if client.Name == peerRef.ClusterName {
						return PeerRefTypeStorageClient, nil
					}
				}
			}
		}
		return PeerRefTypeStorageCluster, nil
	} else {
		operatorNamespace := os.Getenv("POD_NAMESPACE")
		cm, err := FetchConfigMap(ctx, c, ClientInfoConfigMapName, operatorNamespace)
		if k8serrors.IsNotFound(err) {
			return PeerRefTypeStorageCluster, nil
		}
		if err != nil {
			return PeerRefTypeUnknown, err
		}

		if _, ok := cm.Data[peerRef.ClusterName]; ok {
			return PeerRefTypeStorageClient, nil
		}
		return PeerRefTypeStorageCluster, nil
	}
}

// IsStorageClientType checks if peerRefs on MirrorPeer is of type StorageClient or StorageCluster
func IsStorageClientType(ctx context.Context, c client.Client, mirrorPeer multiclusterv1alpha1.MirrorPeer, isManagedCluster bool) (bool, error) {
	for _, v := range mirrorPeer.Spec.Items {
		peerRefType, err := getPeerRefType(ctx, c, v, isManagedCluster)
		if err != nil {
			return false, err
		}
		if peerRefType != PeerRefTypeStorageClient {
			return false, nil
		}
	}
	return true, nil
}
