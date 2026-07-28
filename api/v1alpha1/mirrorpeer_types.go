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

type PhaseType string
type DRType string
type PhaseMessage string

// PhaseType for a mirrorpeer
const (
	Initializing PhaseType = "Initializing"
	Configuring  PhaseType = "Configuring"
	Ready        PhaseType = "Ready"
	Failed       PhaseType = "Failed"
	Deleting     PhaseType = "Deleting"
)

// DRType for a mirrorpeer
const (
	Sync  DRType = "sync"
	Async DRType = "async"
)

// PhaseMessage for a mirrorpeer
const (
	ValidationFailed                 PhaseMessage = "Validation failed"
	DeletionFailed                   PhaseMessage = "Deletion failed"
	DeletionInProgress               PhaseMessage = "Deletion is in progress"
	ConfigurationFailed              PhaseMessage = "Configuration failed"
	ManagedClusterAddOnFailed        PhaseMessage = "ManagedClusterAddon configuration failed"
	PeeringInProgress                PhaseMessage = "Peering is in progress"
	PeeringFailed                    PhaseMessage = "Peering of managedclusters failed"
	S3ConfigurationInProgress        PhaseMessage = "S3 configuration is in progress"
	S3ConfigurationFailed            PhaseMessage = "S3 configuration failed"
	DRClusterConfigurationInProgress PhaseMessage = "DRCluster configuration is in progress"
	DRClusterConfigurationFailed     PhaseMessage = "DRCluster configuration failed"
	MirrorPeerReady                  PhaseMessage = "Setup is completed"
)

// Condition type for a mirrorpeer
const (
	ConditionValidated  = "Validated"
	ConditionConfigured = "Configured"
	ConditionDeleted    = "Deleted"
)

// Condition messages for a mirrorpeer
const (
	MessageConfigurationInProgress          = "Configuration is in progress"
	MessagePeeringInProgress                = "Peering is in progress"
	MessageS3ConfigurationInProgress        = "S3 configuration is in progress"
	MessageDRClusterConfigurationInProgress = "DRCluster configuration is in progress"
	MessageDeletionInProgress               = "MirrorPeer is being deleted"
	MessageMirrorPeerValid                  = "MirrorPeer is valid"
	MessageConfigurationDone                = "MirrorPeer configuration done"
)

// Condition reason for a mirrorpeer
const (
	ReasonValidationFailed                 = "ValidationFailed"
	ReasonValidated                        = "MirrorPeerValid"
	ReasonConfigurationDone                = "Configured"
	ReasonConfigurationFailed              = "ConfigurationFailed"
	ReasonConfigurationInProgress          = "ConfigurationInProgress"
	ReasonManagedClusterAddonFailed        = "ManagedClusterAddonFailed"
	ReasonPeeringInProgress                = "PeeringInProgress"
	ReasonPeeringFailed                    = "PeeringFailed"
	ReasonS3ConfiguringInProgress          = "S3ConfigurationInProgress"
	ReasonS3ConfigurationFailed            = "S3ConfigurationFailed"
	ReasonDRClusterConfigurationInProgress = "DRClusterConfigurationInProgress"
	ReasonDRClusterConfigurationFailed     = "DRClusterConfigurationFailed"
	ReasonDeletionFailed                   = "DeletionFailed"
	ReasonDeletionInProgress               = "DeletionInProgress"
	ReasonMirrorPeerReady                  = "MirrorPeerReady"
)

// StorageClusterRef holds a reference to a StorageCluster
type StorageClusterRef struct {
	Name string `json:"name"`

	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

// PeerRef holds a reference to a mirror peer
type PeerRef struct {
	// ClusterName is the name of ManagedCluster.
	// ManagedCluster matching this name is considered
	// a peer cluster.
	ClusterName string `json:"clusterName"`
	// StorageClusterRef holds a reference to StorageCluster object
	StorageClusterRef StorageClusterRef `json:"storageClusterRef"`
}

// MirrorPeerSpec defines the desired state of MirrorPeer
type MirrorPeerSpec struct {
	// Type represents the mode of DR operation (sync or async)
	// +kubebuilder:default=async
	// +kubebuilder:validation:Enum=async;sync
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.type is immutable."
	Type DRType `json:"type"`

	// Items is a list of PeerRef.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(e, (size(oldSelf.filter(x, (x.clusterName == e.clusterName) && (x.storageClusterRef.name == e.storageClusterRef.name) )) == 1))",message="items.clusterName and items.storageClusterRef.name fields are immutable."
	// +listType=map
	// +listMapKey=clusterName
	Items []PeerRef `json:"items"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	ManageS3 bool `json:"manageS3,omitempty"`
}

// MirrorPeerStatus defines the observed state of MirrorPeer
type MirrorPeerStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	Phase      PhaseType          `json:"phase,omitempty"`
	Message    PhaseMessage       `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=.spec.type
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp

// MirrorPeer is the Schema for the mirrorpeers API
type MirrorPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MirrorPeerSpec   `json:"spec,omitempty"`
	Status MirrorPeerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MirrorPeerList contains a list of MirrorPeer
type MirrorPeerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MirrorPeer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MirrorPeer{}, &MirrorPeerList{})
}
