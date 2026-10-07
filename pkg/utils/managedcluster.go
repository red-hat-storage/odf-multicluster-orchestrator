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
	clusterIDLabelKey                 = "clusterID"
	OdfInfoClusterClaimNamespacedName = "odfinfo.odf.openshift.io"
)

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

func GetNamespacedNameForClusterInfo(managedCluster clusterv1.ManagedCluster) (types.NamespacedName, error) {
	clusterClaims := managedCluster.Status.ClusterClaims
	for _, claim := range clusterClaims {
		if claim.Name == OdfInfoClusterClaimNamespacedName {
			namespacedName := strings.Split(claim.Value, "/")
			if len(namespacedName) != 2 {
				return types.NamespacedName{}, fmt.Errorf("invalid format for namespaced name claim: expected 'namespace/name', got '%s'", claim.Value)
			}
			return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil
		}
	}

	return types.NamespacedName{}, fmt.Errorf("cannot find ClusterClaim %q in ManagedCluster status", OdfInfoClusterClaimNamespacedName)
}

func HasRequiredODFKey(mc *clusterv1.ManagedCluster) bool {
	claims := mc.Status.ClusterClaims
	for _, claim := range claims {
		if claim.Name == OdfInfoClusterClaimNamespacedName {
			return true
		}
	}
	return false

}
