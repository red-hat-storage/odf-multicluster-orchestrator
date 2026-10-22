/*
Copyright 2020 Red Hat OpenShift Container Storage.

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

// Copied from github.com/red-hat-storage/ocs-operator/api/v1alpha1/odfinfoconfig.go
// at commit 9cb4c22acf4bbd864afed224edfc393737e0c815 so MCO can use the new
// OdfInfoData.TLSInfo field without importing the OCS API revision, which requires
// Kubernetes v0.37.1. MCO uses Argo CD's ApplicationSet API in
// internal/controller/protectedapplicationview and library-go asset templating in
// addons/s3config and addons/tokenexchange. MCO cannot yet move past Kubernetes
// v0.34 because Argo CD's gitops-engine imports autoscaling/v2beta1 and
// autoscaling/v2beta2, while the library-go/OpenShift API chain imports
// scheduling/v1alpha1. These APIs are unavailable in newer k8s.io/api modules, so
// those dependency chains need coordinated upgrades first.

package v1alpha1

import "k8s.io/apimachinery/pkg/types"

const ApiServerExportedAddressAnnotationName = "ocs.openshift.io/api-server-exported-address"

// ConnectedClient describes the connected clients of the storage cluster key
// +kubebuilder:object:generate=false
type ConnectedClient struct {
	Name      string `yaml:"name"`
	ClusterID string `yaml:"clusterId"`
	ClientID  string `yaml:"clientId"`
}

// +kubebuilder:object:generate=false
type InfoCephBlockPool struct {
	Name          string `yaml:"name"`
	MirrorEnabled bool   `yaml:"mirrorEnabled"`
}

// +kubebuilder:object:generate=false
type TLSInfo struct {
	ServerSAN       string `yaml:"serverSAN"`
	ServerCMContent string `yaml:"serverCmContent"`
	ClientSAN       string `yaml:"clientSAN"`
	ClientCMContent string `yaml:"clientCmContent"`
	ClientCertName  string `yaml:"clientCertName"`
}

// InfoStorageCluster describes information regarding a storage cluster key
// +kubebuilder:object:generate=false
type InfoStorageCluster struct {
	NamespacedName          types.NamespacedName `yaml:"namespacedName"`
	StorageProviderEndpoint string               `yaml:"storageProviderEndpoint"`
	CephClusterFSID         string               `yaml:"cephClusterFSID"`
	StorageClusterUID       string               `yaml:"storageClusterUID"`
	Annotations             map[string]string    `yaml:"annotations"`
	InfoCephBlockPools      []InfoCephBlockPool  `yaml:"infoCephBlockPools"`
}

// OdfInfoData describes odf-info CM's data
// +kubebuilder:object:generate=false
type OdfInfoData struct {
	Version        string             `yaml:"version"`
	DeploymentType string             `yaml:"deploymentType"`
	Clients        []ConnectedClient  `yaml:"clients"`
	StorageCluster InfoStorageCluster `yaml:"storageCluster"`
	TLSInfo        TLSInfo            `yaml:"tlsInfo"`
}
