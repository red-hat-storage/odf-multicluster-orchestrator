package utils

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

type ProviderInfo struct {
	Version                       string               `json:"version"`
	DeploymentType                string               `json:"deploymentType"`
	ProviderManagedClusterName    string               `json:"providerManagedClusterName"`
	NamespacedName                types.NamespacedName `json:"namespacedName"`
	StorageProviderEndpoint       string               `json:"storageProviderEndpoint"`
	CephClusterFSID               string               `json:"cephClusterFSID"`
	StorageProviderPublicEndpoint string               `json:"storageProviderPublicEndpoint"`
}

type ClientInfo struct {
	ClusterID                string       `json:"clusterId"`
	Name                     string       `json:"name"`
	ProviderInfo             ProviderInfo `json:"providerInfo,omitempty"`
	ClientManagedClusterName string       `json:"clientManagedClusterName,omitempty"`
	ClientID                 string       `json:"clientId"`
}

// Helper function to extract and unmarshal ClientInfo from ConfigMap
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
