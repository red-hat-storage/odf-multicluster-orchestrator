package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProtectedApplicationViewSpec defines the desired state of ProtectedApplicationView
type ProtectedApplicationViewSpec struct {
	// Type of application (ApplicationSet, Subscription, or Discovered)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ApplicationSet;Subscription;Discovered
	Type string `json:"type"`

	// ApplicationRef references the source application resource
	// +kubebuilder:validation:Required
	ApplicationRef corev1.ObjectReference `json:"applicationRef"`

	// Placements contains placement information for this application
	// +optional
	// +kubebuilder:validation:MaxItems=10
	Placements []PlacementInfo `json:"placements,omitempty"`

	// DRInfo contains disaster recovery configuration
	// +kubebuilder:validation:Required
	DRInfo DRInfo `json:"drInfo"`

	// ApplicationSetInfo contains ApplicationSet-specific information
	// +optional
	ApplicationSetInfo *ApplicationSetInfo `json:"applicationSetInfo,omitempty"`

	// SubscriptionInfo contains Subscription-specific information
	// +optional
	SubscriptionInfo *SubscriptionInfo `json:"subscriptionInfo,omitempty"`
}

// PlacementInfo contains information about application placement
type PlacementInfo struct {
	// Name of the Placement resource
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Placement resource
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// Kind of placement (Placement or PlacementRule)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Placement;PlacementRule
	Kind string `json:"kind"`

	// SelectedClusters is the list of clusters where application is placed
	// +optional
	SelectedClusters []string `json:"selectedClusters,omitempty"`

	// NumberOfClusters is the count of selected clusters
	// +kubebuilder:validation:Minimum=0
	NumberOfClusters int `json:"numberOfClusters"`
}

// DRInfo contains disaster recovery configuration information
type DRInfo struct {
	// DRPCRef references the DRPlacementControl protecting this application
	// +kubebuilder:validation:Required
	DRPCRef corev1.ObjectReference `json:"drpcRef"`

	// DRPolicyRef references the DRPolicy used for protection
	// +kubebuilder:validation:Required
	DRPolicyRef corev1.ObjectReference `json:"drPolicyRef"`

	// DRClusters is the list of clusters in the DR relationship
	// +optional
	DRClusters []string `json:"drClusters,omitempty"`

	// PrimaryCluster is the current primary/active cluster
	// +optional
	PrimaryCluster string `json:"primaryCluster,omitempty"`

	// ProtectedNamespaces lists namespaces protected by this DRPC
	// +optional
	ProtectedNamespaces []string `json:"protectedNamespaces,omitempty"`

	// Status contains current DR status information
	// +optional
	Status DRStatusInfo `json:"status,omitempty"`
}

// DRStatusInfo contains disaster recovery status
type DRStatusInfo struct {
	// Phase represents current DR phase (Relocated, FailingOver, etc.)
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the DR state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ApplicationSetInfo contains ApplicationSet-specific information
type ApplicationSetInfo struct {
	// GitRepoURLs contains Git repository URLs used by the ApplicationSet
	// +optional
	GitRepoURLs []string `json:"gitRepoURLs,omitempty"`

	// TargetRevision is the Git revision being deployed
	// +optional
	TargetRevision string `json:"targetRevision,omitempty"`
}

// SubscriptionInfo contains Subscription-specific information
type SubscriptionInfo struct {
	// SubscriptionRefs lists Subscription resources in this DRPC group
	// +optional
	SubscriptionRefs []corev1.ObjectReference `json:"subscriptionRefs,omitempty"`
}

// ProtectedApplicationViewStatus defines the observed state of ProtectedApplicationView
type ProtectedApplicationViewStatus struct {
	// ObservedGeneration is the generation most recently observed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastSyncTime is the last time the view was synchronized
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest available observations of the view state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pav
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="App",type=string,JSONPath=`.spec.applicationRef.name`
// +kubebuilder:printcolumn:name="DRPolicy",type=string,JSONPath=`.spec.drInfo.drPolicyRef.name`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.spec.drInfo.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProtectedApplicationView represents an aggregated view of a protected application
type ProtectedApplicationView struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProtectedApplicationViewSpec   `json:"spec,omitempty"`
	Status ProtectedApplicationViewStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProtectedApplicationViewList contains a list of ProtectedApplicationView
type ProtectedApplicationViewList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProtectedApplicationView `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProtectedApplicationView{}, &ProtectedApplicationViewList{})
}
