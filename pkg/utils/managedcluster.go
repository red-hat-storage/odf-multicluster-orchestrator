package utils

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterIDLabelKey                  = "clusterID"
	OdfInfoClusterClaimNamespacedName  = "odfinfo.odf.openshift.io"
	IbmScaleClusterClaimNamespacedName = "scale.spectrum.ibm.com"
)

// SupportedStorageVendorClaims lists all supported storage vendor cluster claim names
var SupportedStorageVendorClaims = []string{
	OdfInfoClusterClaimNamespacedName,
	IbmScaleClusterClaimNamespacedName,
}

// GetManagedClusterById fetches a ManagedCluster by its cluster ID label
func GetManagedClusterById(ctx context.Context, c client.Client, clusterId string) (*clusterv1.ManagedCluster, error) {
	managedClusterList := &clusterv1.ManagedClusterList{}

	labelSelector := labels.SelectorFromSet(labels.Set{
		clusterIDLabelKey: clusterId,
	})

	listOptions := &client.ListOptions{
		LabelSelector: labelSelector,
	}
	err := c.List(ctx, managedClusterList, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list managed clusters: %v", err)
	}

	if len(managedClusterList.Items) == 0 {
		return nil, fmt.Errorf("managed cluster with ID %s not found", clusterId)
	}

	// Return the first matching ManagedCluster (there should only be one)
	return &managedClusterList.Items[0], nil
}

// GetNamespacedNameFromClusterClaim extracts a namespaced name from a specific ClusterClaim
func GetNamespacedNameFromClusterClaim(managedCluster *clusterv1.ManagedCluster, claimName string) (types.NamespacedName, error) {
	clusterClaims := managedCluster.Status.ClusterClaims
	for _, claim := range clusterClaims {
		if claim.Name == claimName {
			namespacedName := strings.Split(claim.Value, "/")
			if len(namespacedName) != 2 {
				return types.NamespacedName{}, fmt.Errorf("invalid format for namespaced name claim %q: expected 'namespace/name', got '%s'", claimName, claim.Value)
			}
			return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil
		}
	}

	return types.NamespacedName{}, fmt.Errorf("cannot find ClusterClaim %q in ManagedCluster status", claimName)
}

// GetAllStorageVendorConfigMaps returns a map of cluster claim name to ConfigMap namespaced name
// for all supported storage vendors that have claims in the ManagedCluster
func GetAllStorageVendorConfigMaps(managedCluster *clusterv1.ManagedCluster) map[string]types.NamespacedName {
	result := make(map[string]types.NamespacedName)

	for _, claimName := range SupportedStorageVendorClaims {
		if nsName, err := GetNamespacedNameFromClusterClaim(managedCluster, claimName); err == nil {
			result[claimName] = nsName
		}
	}

	return result
}

// HasClusterClaim checks if a ManagedCluster has a specific cluster claim
func HasClusterClaim(mc *clusterv1.ManagedCluster, claimName string) bool {
	claims := mc.Status.ClusterClaims
	for _, claim := range claims {
		if claim.Name == claimName {
			return true
		}
	}
	return false
}

// HasRequiredODFKey checks if the ManagedCluster has the ODF cluster claim
func HasRequiredODFKey(mc *clusterv1.ManagedCluster) bool {
	return HasClusterClaim(mc, OdfInfoClusterClaimNamespacedName)
}

// HasRequiredIbmScaleKey checks if the ManagedCluster has the IBM Storage Scale cluster claim
func HasRequiredIbmScaleKey(mc *clusterv1.ManagedCluster) bool {
	return HasClusterClaim(mc, IbmScaleClusterClaimNamespacedName)
}

// HasRequiredStorageVendorKey checks if the ManagedCluster has any supported storage vendor cluster claim
func HasRequiredStorageVendorKey(mc *clusterv1.ManagedCluster) bool {
	for _, claimName := range SupportedStorageVendorClaims {
		if HasClusterClaim(mc, claimName) {
			return true
		}
	}
	return false
}
