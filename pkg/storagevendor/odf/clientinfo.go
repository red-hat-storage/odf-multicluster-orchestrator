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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

type InfoCephBlockPool struct {
	Name          string `json:"name"`
	MirrorEnabled bool   `json:"mirrorEnabled"`
}

type ProviderInfo struct {
	Version                       string               `json:"version"`
	DeploymentType                string               `json:"deploymentType"`
	ProviderManagedClusterName    string               `json:"providerManagedClusterName"`
	NamespacedName                types.NamespacedName `json:"namespacedName"`
	StorageProviderEndpoint       string               `json:"storageProviderEndpoint"`
	CephClusterFSID               string               `json:"cephClusterFSID"`
	StorageProviderPublicEndpoint string               `json:"storageProviderPublicEndpoint"`
	InfoCephBlockPools            []InfoCephBlockPool  `json:"infoCephBlockPools"`
}

type ClientInfo struct {
	ClusterID                string       `json:"clusterId"`
	Name                     string       `json:"name"`
	ProviderInfo             ProviderInfo `json:"providerInfo,omitempty"`
	ClientManagedClusterName string       `json:"clientManagedClusterName,omitempty"`
	ClientID                 string       `json:"clientId"`
}

// GetClientInfoFromConfigMap Helper function to extract and unmarshal ClientInfo from ConfigMap
func GetClientInfoFromConfigMap(clientInfoMap map[string]string, key string) (ClientInfo, error) {
	clientInfoJSON, ok := clientInfoMap[key]
	if !ok {
		return ClientInfo{}, fmt.Errorf("client info for %s not found in ConfigMap", key)
	}

	var clientInfo ClientInfo
	if err := json.Unmarshal([]byte(clientInfoJSON), &clientInfo); err != nil {
		return ClientInfo{}, fmt.Errorf("failed to unmarshal client info for %s: %v", key, err)
	}

	return clientInfo, nil
}
