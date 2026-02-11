package utils

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CephClusterNameTemplate = "%s-cephcluster"
)

func FetchCephCluster(ctx context.Context, c client.Client, storageClusterNamespacedName types.NamespacedName) (*rookv1.CephCluster, error) {
	var cephCluster rookv1.CephCluster
	cephClusterName := fmt.Sprintf(CephClusterNameTemplate, storageClusterNamespacedName.Name)
	cephClusterNamespace := storageClusterNamespacedName.Namespace
	err := c.Get(ctx, types.NamespacedName{Namespace: cephClusterNamespace, Name: cephClusterName}, &cephCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cephcluster with name %s in namespace %s", cephClusterName, cephClusterNamespace)
	}
	return &cephCluster, nil
}

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
