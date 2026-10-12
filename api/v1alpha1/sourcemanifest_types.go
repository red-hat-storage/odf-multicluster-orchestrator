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
	"k8s.io/apimachinery/pkg/runtime"
)

// SourceManifestSpec defines the desired resources/payload to be created on the destination cluster.
type SourceManifestSpec struct {
	// Manifests is the list of Kubernetes objects to transport and apply on the destination cluster.
	// +optional
	Manifests []runtime.RawExtension `json:"manifests,omitempty"`
}

// SourceManifestStatus defines the observed state of SourceManifest.
// This resource does not use status in practice; MCO processes it via ManagedClusterView.
type SourceManifestStatus struct {
	// This status is intentionally left empty as this resource is spec-only
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SourceManifest is the CR created by a storage vendor on Cluster A which defines the resources
// that need to be created on Cluster B.
// It carries no status; MCO processes this via ManagedClusterView.
type SourceManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SourceManifestSpec `json:"spec,omitempty"`
	// Status is not used but defined for operator-sdk compatibility
	// +optional
	Status SourceManifestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SourceManifestList contains a list of SourceManifest
type SourceManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SourceManifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SourceManifest{}, &SourceManifestList{})
}
