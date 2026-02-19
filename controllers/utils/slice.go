package utils

import (
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"reflect"
)

// ContainsPeerRef checks if a slice of PeerRef contains the provided PeerRef
func ContainsPeerRef(slice []multiclusterv1alpha1.PeerRef, peerRef multiclusterv1alpha1.PeerRef) bool {
	for i := range slice {
		if reflect.DeepEqual(slice[i], peerRef) {
			return true
		}
	}
	return false
}
