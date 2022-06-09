/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

const (
	ExchangingSecret PhaseType = "ExchangingSecret"
	ExchangedSecret  PhaseType = "ExchangedSecret"
	S3ProfileSynced  PhaseType = "S3ProfileSynced"
	S3ProfileSyncing PhaseType = "S3ProfileSyncing"
	Deleting         PhaseType = "Deleting"
	Sync             DRType    = "sync"
	Async            DRType    = "async"
)

// StorageClusterRef holds a reference to a StorageCluster
type StorageClusterRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
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
	Type DRType `json:"type"`

	// Items is a list of PeerRef.
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:MinItems=2
	Items []PeerRef `json:"items"`

	// SchedulingIntervals is a list of intervals at which mirroring snapshots are taken
	// +kubebuilder:validation:Optional
	SchedulingIntervals []string `json:"schedulingIntervals,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	ManageS3 bool `json:"manageS3,omitempty"`
}

// MirrorPeerStatus defines the observed state of MirrorPeer
type MirrorPeerStatus struct {
	Phase   PhaseType `json:"phase,omitempty"`
	Message string    `json:"message,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// MirrorPeer is the Schema for the mirrorpeers API
type MirrorPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MirrorPeerSpec   `json:"spec,omitempty"`
	Status MirrorPeerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MirrorPeerList contains a list of MirrorPeer
type MirrorPeerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MirrorPeer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MirrorPeer{}, &MirrorPeerList{})
}
