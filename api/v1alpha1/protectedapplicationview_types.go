package v1alpha1

import (
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ApplicationType string

const (
	// ApplicationSetType represents an ArgoCD ApplicationSet application
	ApplicationSetType ApplicationType = "ApplicationSet"

	// SubscriptionType represents an OpenShift Subscription application
	SubscriptionType ApplicationType = "Subscription"

	// DiscoveredType represents a discovered application (neither ApplicationSet nor Subscription)
	DiscoveredType ApplicationType = "Discovered"
)

// +kubebuilder:validation:XValidation:rule="self.drpcRef == oldSelf.drpcRef",message="drpcRef is immutable"
type ProtectedApplicationViewSpec struct {
	DRPCRef corev1.ObjectReference `json:"drpcRef"`
}

// PlacementInfo contains information about application placement
type PlacementInfo struct {
	// PlacementRef references the placement resource used for this application
	// +kubebuilder:validation:Required
	PlacementRef corev1.ObjectReference `json:"placementRef"`

	// SelectedClusters is the list of clusters where application is placed
	// +optional
	SelectedClusters []string `json:"selectedClusters,omitempty"`
}

// DRInfo contains disaster recovery configuration information
type DRInfo struct {
	// DRPolicyRef references the DRPolicy used for protection
	// +kubebuilder:validation:Required
	DRPolicyRef corev1.ObjectReference `json:"drpolicyRef"`

	// DRClusters is the list of clusters in the DR relationship
	// +optional
	DRClusters []string `json:"drClusters,omitempty"`

	// PrimaryCluster is the current primary/active cluster
	// +optional
	PrimaryCluster string `json:"primaryCluster,omitempty"`

	// ProtectedNamespaces contains list of namespaces protected by this DRPC
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
	Phase ramenv1alpha1.DRState `json:"phase,omitempty"`

	// LastGroupSyncTime is the last time the DR group was synchronized
	// +optional
	LastGroupSyncTime *metav1.Time `json:"lastGroupSyncTime,omitempty"`

	// Conditions represent the latest available observations of the DR state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ApplicationInfo contains information about the protected application like type and references
type ApplicationInfo struct {
	// Type of application (ApplicationSet, Subscription, or Discovered)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ApplicationSet;Subscription;Discovered
	Type ApplicationType `json:"type"`

	// ApplicationRef references the source application resource, ApplicationSet will refer to ArgoCD ApplicationSet, Subscription will refer to K8s Application
	// +optional
	ApplicationRef corev1.ObjectReference `json:"applicationRef"`

	// SubscriptionInfo contains Subscription-specific information which belong to this application
	// +optional
	SubscriptionInfo *SubscriptionInfo `json:"subscriptionInfo,omitempty"`
}

// SubscriptionInfo contains Subscription-specific information
type SubscriptionInfo struct {
	// SubscriptionRefs lists Subscription resources in this DRPC group
	// +optional
	SubscriptionRefs []corev1.ObjectReference `json:"subscriptionRefs,omitempty"`
}

// ProtectedApplicationViewStatus defines the observed state of ProtectedApplicationView
type ProtectedApplicationViewStatus struct {
	ApplicationInfo ApplicationInfo `json:"applicationInfo,omitempty"`

	// Placements contains placement information for this application
	// +optional
	PlacementInfo *PlacementInfo `json:"placementInfo,omitempty"`

	// DRInfo contains disaster recovery configuration
	// +kubebuilder:validation:Required
	DRInfo DRInfo `json:"drInfo"`

	// ObservedGeneration is the generation most recently observed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastSyncTime is the last time the view was synchronized
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest available observations of the application state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pav
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.status.applicationInfo.type`
// +kubebuilder:printcolumn:name="App",type=string,JSONPath=`.status.applicationInfo.applicationRef.name`
// +kubebuilder:printcolumn:name="DRPolicy",type=string,JSONPath=`.status.drInfo.drPolicyRef.name`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.drInfo.status.phase`
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
