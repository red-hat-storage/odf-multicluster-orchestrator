package utils

import (
	"context"
	"fmt"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

// GetPeerRefForProviderCluster returns the client peer ref for the current provider cluster
func GetPeerRefForProviderCluster(ctx context.Context, spokeClient, hubClient client.Client, mp *multiclusterv1alpha1.MirrorPeer) ([]multiclusterv1alpha1.PeerRef, error) {
	var peerRefList []multiclusterv1alpha1.PeerRef
	operatorNamespace := GetEnv("POD_NAMESPACE")
	cm, err := GetODFInfoConfigMap(ctx, spokeClient, operatorNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ODF Info ConfigMap for namespace %s: %w", operatorNamespace, err)
	}
	var odfInfo ocsv1alpha1.OdfInfoData
	for key, value := range cm.Data {
		err := yaml.Unmarshal([]byte(value), &odfInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal ODF info data for key %s: %w", key, err)
		}
		for _, v := range mp.Spec.Items {
			clusterID, err := GetClusterID(ctx, hubClient, v.ClusterName)
			if err != nil {
				return nil, err
			}
			for _, client := range odfInfo.Clients {
				if client.ClusterID == clusterID {
					peerRefList = append(peerRefList, v)
				}
			}
		}
	}
	return peerRefList, nil
}

// getClusterID returns the cluster ID of the OCP-Cluster
func GetClusterID(ctx context.Context, client client.Client, clusterName string) (string, error) {
	var managedCluster clusterv1.ManagedCluster
	err := client.Get(ctx, types.NamespacedName{Name: clusterName}, &managedCluster)
	if err != nil {
		return "", err
	}
	return managedCluster.GetLabels()["clusterID"], nil
}

func getPeerRefType(ctx context.Context, c client.Client, peerRef multiclusterv1alpha1.PeerRef, isManagedCluster bool) (PeerRefType, error) {
	if isManagedCluster {
		operatorNamespace := GetEnv("POD_NAMESPACE")
		cm, err := GetODFInfoConfigMap(ctx, c, operatorNamespace)
		if err != nil {
			return PeerRefTypeUnknown, fmt.Errorf("failed to get ODF Info ConfigMap for namespace %s: %w", peerRef.StorageClusterRef.Namespace, err)
		}
		var odfInfo ocsv1alpha1.OdfInfoData
		for key, value := range cm.Data {

			err := yaml.Unmarshal([]byte(value), &odfInfo)
			if err != nil {
				return PeerRefTypeUnknown, fmt.Errorf("failed to unmarshal ODF info data for key %s: %w", key, err)
			}

			for _, client := range odfInfo.Clients {
				if client.Name == peerRef.StorageClusterRef.Name {
					return PeerRefTypeStorageClient, nil
				}
			}

		}
		return PeerRefTypeStorageCluster, nil
	} else {
		operatorNamespace := GetEnv("POD_NAMESPACE")
		cm, err := FetchClientInfoConfigMap(ctx, c, operatorNamespace)
		if err != nil {
			return PeerRefTypeUnknown, err
		}
		cInfo, err := GetClientInfoFromConfigMap(cm.Data, GetKey(peerRef.ClusterName, peerRef.StorageClusterRef.Name))
		if err != nil {
			return PeerRefTypeUnknown, err
		}
		if cInfo.ProviderInfo.DeploymentType != "external" {
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
