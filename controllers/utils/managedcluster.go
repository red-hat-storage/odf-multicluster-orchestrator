package utils

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const clusterIDLabelKey = "clusterID"

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
