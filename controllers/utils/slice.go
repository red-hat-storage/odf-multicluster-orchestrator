package utils

import (
	rbacv1 "k8s.io/api/rbac/v1"
	"reflect"
	"strings"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
)

// ContainsString checks if a slice of strings contains the provided string
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func ContainsSuffix(slice []string, s string) bool {
	for _, item := range slice {
		if strings.HasSuffix(item, s) {
			return true
		}
	}
	return false
}

// ContainsPeerRef checks if a slice of PeerRef contains the provided PeerRef
func ContainsPeerRef(slice []multiclusterv1alpha1.PeerRef, peerRef *multiclusterv1alpha1.PeerRef) bool {
	for i := range slice {
		if reflect.DeepEqual(slice[i], *peerRef) {
			return true
		}
	}
	return false
}

// RemoveString removes a given string from a slice and returns the new slice
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func ContainsSubject(slice []rbacv1.Subject, subject *rbacv1.Subject) bool {
	for i := range slice {
		if reflect.DeepEqual(slice[i], *subject) {
			return true
		}
	}
	return false
}

// RemoveMirrorPeer removes the given mirrorPeer from the slice and returns the new slice
func RemoveMirrorPeer(slice []multiclusterv1alpha1.MirrorPeer, mirrorPeer multiclusterv1alpha1.MirrorPeer) (result []multiclusterv1alpha1.MirrorPeer) {
	for _, item := range slice {
		if item.Name == mirrorPeer.Name {
			continue
		}
		result = append(result, item)
	}
	return
}
