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
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type peerRefType string

const (
	// peerRefTypeStorageClient represents a storage client
	peerRefTypeStorageClient peerRefType = "StorageClient"

	// peerRefTypeStorageCluster represents a storage cluster
	peerRefTypeStorageCluster peerRefType = "StorageCluster"

	// peerRefTypeUnknown represents an unknown type
	peerRefTypeUnknown peerRefType = "Unknown"
)

// GetPeerRefForProviderCluster returns the client peer ref for the current provider cluster
func GetPeerRefForProviderCluster(ctx context.Context, spokeClient, hubClient client.Client, mp *multiclusterv1alpha1.MirrorPeer) ([]multiclusterv1alpha1.PeerRef, error) {
	var peerRefList []multiclusterv1alpha1.PeerRef
	operatorNamespace := utils.GetEnv("POD_NAMESPACE")
	cm, err := GetODFInfoConfigMap(ctx, spokeClient, operatorNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get DF Info ConfigMap for namespace %s: %w", operatorNamespace, err)
	}
	var odfInfo ocsv1alpha1.OdfInfoData
	for key, value := range cm.Data {
		err := yaml.Unmarshal([]byte(value), &odfInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal DF info data for key %s: %w", key, err)
		}
		for _, v := range mp.Spec.Items {
			clusterID, err := utils.GetClusterID(ctx, hubClient, v.ClusterName)
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

func getPeerRefType(peerRef multiclusterv1alpha1.PeerRef, clientInfoMap map[string]string) (peerRefType, error) {
	cInfo, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(peerRef.ClusterName, peerRef.StorageClusterRef.Name))
	if err != nil {
		return peerRefTypeUnknown, err
	}
	if cInfo.ProviderInfo.DeploymentType != "external" {
		return peerRefTypeStorageClient, nil
	}
	return peerRefTypeStorageCluster, nil
}

// IsStorageClientType checks if peerRefs on MirrorPeer is of type StorageClient or StorageCluster
func IsStorageClientType(mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error) {
	for _, v := range mirrorPeer.Spec.Items {
		peerRefType, err := getPeerRefType(v, clientInfoMap)
		if err != nil {
			return false, err
		}
		if peerRefType != peerRefTypeStorageClient {
			return false, nil
		}
	}
	return true, nil
}
