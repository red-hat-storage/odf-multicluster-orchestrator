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

// AppliedManifestSpec defines the desired payload to be created and reconciled on the destination cluster.
type AppliedManifestSpec struct {
	// Manifests contains the raw Kubernetes objects to be applied on this cluster.
	// +optional
	Manifests []runtime.RawExtension `json:"manifests,omitempty"`
}

// AppliedManifestPhase defines the high-level operational lifecycle of the AppliedManifest resource.
// +kubebuilder:validation:Enum=Pending;Applying;Applied;Failed;Degraded;Terminating
type AppliedManifestPhase string

const (
	// AppliedManifestPhasePending indicates the payload has arrived but processing has not started.
	AppliedManifestPhasePending AppliedManifestPhase = "Pending"

	// AppliedManifestPhaseApplying indicates the target manifests are currently being created or updated.
	AppliedManifestPhaseApplying AppliedManifestPhase = "Applying"

	// AppliedManifestPhaseApplied indicates all contained objects were successfully applied.
	AppliedManifestPhaseApplied AppliedManifestPhase = "Applied"

	// AppliedManifestPhaseFailed indicates one or more objects failed to apply.
	AppliedManifestPhaseFailed AppliedManifestPhase = "Failed"

	// AppliedManifestPhaseDegraded indicates objects were applied, but underlying resources are unhealthy.
	AppliedManifestPhaseDegraded AppliedManifestPhase = "Degraded"

	// AppliedManifestPhaseTerminating indicates cleanup or garbage collection is in progress.
	AppliedManifestPhaseTerminating AppliedManifestPhase = "Terminating"
)

// AppliedManifestStatus defines the observed state of AppliedManifest.
type AppliedManifestStatus struct {
	// Phase represents the current high-level state of the AppliedManifest CR lifecycle.
	// +optional
	Phase AppliedManifestPhase `json:"phase,omitempty"`

	// ObservedGeneration represents the .metadata.generation that was last processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions define the current health and progress of the resources created.
	// Standard Kubernetes conditions [Applied].
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AppliedManifest is the CR created on the destination cluster by MCO and reconciled by the 3rd-party storage vendor.
// The status of this CR is watched by MCO, and reflected on mirrorpeer status.
type AppliedManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppliedManifestSpec   `json:"spec,omitempty"`
	Status AppliedManifestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppliedManifestList contains a list of AppliedManifest resources.
type AppliedManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppliedManifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppliedManifest{}, &AppliedManifestList{})
}
