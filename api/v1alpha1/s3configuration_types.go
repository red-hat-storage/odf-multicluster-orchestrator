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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// S3ConfigurationPhase represents the lifecycle phase of S3Configuration
// +kubebuilder:validation:Enum=Pending;Configuring;Ready;Failed
type S3ConfigurationPhase string

const (
	S3ConfigurationPhasePending     S3ConfigurationPhase = "Pending"
	S3ConfigurationPhaseConfiguring S3ConfigurationPhase = "Configuring"
	S3ConfigurationPhaseReady       S3ConfigurationPhase = "Ready"
	S3ConfigurationPhaseFailed      S3ConfigurationPhase = "Failed"
)

// SecretReference contains namespace + name to reference a secret
type SecretReference struct {
	// Name is the name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the secret on the hub cluster
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// InternalS3Spec defines configuration for ODF-managed internal S3
type InternalS3Spec struct {
	// ProviderCluster is the name of the cluster where the OBC will be created.
	// This cluster must be one of the clusters in spec.managedClusters.
	//
	// The OBC will ONLY be created on this cluster, and the generated S3 endpoint
	// will be shared by all clusters in spec.managedClusters.
	//
	// +kubebuilder:validation:Required
	ProviderCluster string `json:"providerCluster"`

	// StorageClassName is the name of the StorageClass to use for OBC creation.
	// This StorageClass should be provided by the ODF operator on the managed cluster.
	// Examples: "openshift-storage.noobaa.io", "ocs-storagecluster-ceph-rgw"
	//
	// +kubebuilder:validation:Required
	StorageClassName string `json:"storageClassName"`

	// Namespace is the namespace where OBC will be created on the managed cluster.
	// Typically "openshift-storage" for ODF deployments.
	//
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// OBCName is the name to use for the ObjectBucketClaim.
	// If not specified, a name will be generated: "odr-<s3configuration-cr-name>"
	//
	// +kubebuilder:validation:Optional
	OBCName string `json:"obcName,omitempty"`
}

// ExternalS3Spec defines configuration for external S3-compatible storage
type ExternalS3Spec struct {
	// SecretRef references a secret containing S3 credentials.
	// The secret must exist on the hub cluster in the specified namespace and contain:
	//   - AWS_ACCESS_KEY_ID: S3 access key
	//   - AWS_SECRET_ACCESS_KEY: S3 secret key
	//   - s3Bucket: Bucket name for DR metadata
	//   - s3Endpoint: S3 endpoint URL
	//   - s3Region: S3 region (optional, defaults to us-east-1)
	//
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// S3ConfigurationSpec defines the desired state of S3Configuration
// +kubebuilder:validation:XValidation:rule="(has(self.internalS3) && !has(self.externalS3)) || (!has(self.internalS3) && has(self.externalS3))",message="must specify exactly one of internalS3 or externalS3"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.internalS3) || has(self.internalS3)",message="cannot change from internalS3 to externalS3"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.externalS3) || has(self.externalS3)",message="cannot change from externalS3 to internalS3"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.internalS3) || !has(self.internalS3) || self.internalS3 == oldSelf.internalS3",message="internalS3 configuration is immutable"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.externalS3) || !has(self.externalS3) || self.externalS3 == oldSelf.externalS3",message="externalS3 configuration is immutable"
// +kubebuilder:validation:XValidation:rule="self.managedClusters == oldSelf.managedClusters",message="managedClusters is immutable"
type S3ConfigurationSpec struct {
	// InternalS3 specifies configuration for ODF-managed internal S3 (Noobaa/RGW).
	// When specified, the S3Configuration controller will:
	//   1. Use addon mechanism to create OBC on the specified cluster (internalS3.providerCluster)
	//   2. Wait for addon to transfer generated secret from spoke to hub
	//   3. Copy S3 secret to Ramen operator namespace on hub
	//   4. Use the secret to configure Ramen ConfigMap S3 profile
	//   5. Create DRCluster on hub for all clusters in spec.managedClusters
	//
	// This field is mutually exclusive with ExternalS3.
	// Only valid when used with ODF storage.
	//
	// +kubebuilder:validation:Optional
	InternalS3 *InternalS3Spec `json:"internalS3,omitempty"`

	// ExternalS3 specifies configuration for external S3-compatible storage.
	// When specified, the referenced secret must already exist on the hub.
	// Use this for:
	//   - Vendor-provided S3 (CNSA, Dell, Flash)
	//   - External S3 services (AWS S3, MinIO, etc.)
	//   - ODF with external S3 endpoint
	//
	// This field is mutually exclusive with InternalS3.
	//
	// +kubebuilder:validation:Optional
	ExternalS3 *ExternalS3Spec `json:"externalS3,omitempty"`

	// ManagedClusters is the list of clusters where this S3 configuration will be used.
	// A DRCluster will be created on the hub for each cluster in this list.
	//
	// For InternalS3:
	//   - OBC created on ONE cluster (specified in internalS3.providerCluster)
	//   - S3 secret copied to Ramen operator namespace on hub
	//   - DRCluster created on hub for ALL clusters in this list
	//   - All DRClusters reference the S3 endpoint from the cluster where OBC was created
	//
	// For ExternalS3:
	//   - S3 secret copied to Ramen operator namespace on hub
	//   - DRCluster created on hub for ALL clusters in this list
	//   - All DRClusters reference the same external S3 endpoint
	//
	// IMPORTANT:
	//   - Each cluster can only appear in ONE S3Configuration's managedClusters list (enforced by webhook)
	//   - DRCluster is a hub-only API, not deployed to spoke clusters
	//
	// The S3Configuration CR name will be used as the s3ProfileName in Ramen ConfigMap
	// and in DRCluster.spec.s3ProfileName.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	ManagedClusters []string `json:"managedClusters"`
}

// S3ConfigurationStatus defines the observed state of S3Configuration
type S3ConfigurationStatus struct {
	// Phase represents the current phase of S3Configuration
	// +optional
	Phase S3ConfigurationPhase `json:"phase,omitempty"`

	// Message provides additional information about the current phase
	// +optional
	Message string `json:"message,omitempty"`

	// ConfiguredClusters lists clusters where S3 has been successfully configured
	// +optional
	ConfiguredClusters []string `json:"configuredClusters,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp

// S3Configuration is the Schema for the s3configurations API.
// S3Configuration manages S3 configuration for DR metadata storage.
// One S3Configuration can be referenced by multiple DRClusters.
type S3Configuration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of S3Configuration
	// +required
	Spec S3ConfigurationSpec `json:"spec"`

	// status defines the observed state of S3Configuration
	// +optional
	Status S3ConfigurationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// S3ConfigurationList contains a list of S3Configuration
type S3ConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []S3Configuration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&S3Configuration{}, &S3ConfigurationList{})
}
