package utils

import (
	"context"
	"fmt"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterType string

const (
	CONVERGED                     ClusterType = "Converged"
	EXTERNAL                      ClusterType = "External"
	UNKNOWN                       ClusterType = "Unknown"
	CephClusterReplicationIdLabel             = "replicationid.multicluster.openshift.io"
	CephClusterNameTemplate                   = "%s-cephcluster"
)

func GetClusterType(storageClusterName string, namespace string, client client.Client) (ClusterType, error) {
	var storageCluster ocsv1.StorageCluster
	err := client.Get(context.TODO(), types.NamespacedName{Name: storageClusterName, Namespace: namespace}, &storageCluster)
	if err != nil {
		return UNKNOWN, err
	}
	if storageCluster.Spec.ExternalStorage.Enable {
		return EXTERNAL, nil
	}
	return CONVERGED, nil
}

func ScaleDeployment(ctx context.Context, client client.Client, deploymentName string, namespace string, replicas int32) (bool, error) {
	var deployment appsv1.Deployment
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, &deployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return true, fmt.Errorf("deployment %s not found in namespace %s", deploymentName, namespace)
		}
		return true, err
	}
	if *deployment.Spec.Replicas != replicas {
		deployment.Spec.Replicas = &replicas
		err = client.Update(ctx, &deployment)
		return true, err
	}
	if deployment.Status.Replicas != replicas {
		return true, nil
	}
	return false, nil
}

func FetchAllCephClusters(ctx context.Context, client client.Client) (*rookv1.CephClusterList, error) {
	var cephClusters rookv1.CephClusterList
	err := client.List(ctx, &cephClusters)
	if err != nil {
		return nil, fmt.Errorf("failed to list CephClusters %v", err)
	}
	return &cephClusters, nil
}

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

// fetchPoolIdForCephBlockPool fetches the pool ID from the CephBlockPool resource
func fetchPoolIdForCephBlockPool(ctx context.Context, c client.Client, sc *storagev1.StorageClass, namespace string) (string, error) {
	if sc.Parameters == nil {
		return "", fmt.Errorf("StorageClass parameters are nil")
	}

	poolName := sc.Parameters["pool"]
	if poolName == "" {
		return "", fmt.Errorf("pool parameter not found in StorageClass")
	}

	blockPool := &rookv1.CephBlockPool{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      poolName,
		Namespace: namespace,
	}, blockPool)
	if err != nil {
		return "", fmt.Errorf("failed to get CephBlockPool %s: %v", poolName, err)
	}

	return strconv.FormatUint(uint64(blockPool.Status.PoolID), 10), nil
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
