package odf

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetStorageClusterFromCurrentNamespace(ctx context.Context, c client.Client, namespace string) (*ocsv1.StorageCluster, error) {
	storageClusterList := &ocsv1.StorageClusterList{}
	listOptions := []client.ListOption{
		client.InNamespace(namespace),
	}

	// List all StorageClusters in the specified namespace
	if err := c.List(ctx, storageClusterList, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list StorageClusters in namespace %s: %w", namespace, err)
	}

	// Ensure only one StorageCluster exists
	if len(storageClusterList.Items) == 0 {
		return nil, fmt.Errorf("no StorageCluster found in namespace %s", namespace)
	}

	if len(storageClusterList.Items) > 1 {
		return nil, fmt.Errorf("multiple StorageClusters found in namespace %s", namespace)
	}

	// Return the single StorageCluster
	return &storageClusterList.Items[0], nil
}
