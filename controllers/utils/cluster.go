package utils

import (
	"context"
	"fmt"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
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

func ScaleDeployment(ctx context.Context, client client.Client, deploymentName string, namespace string, replicas int32) error {
	var deployment appsv1.Deployment
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, &deployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s not found in namespace %s", deploymentName, namespace)
		}
		return err
	}
	deployment.Spec.Replicas = &replicas
	err = client.Update(ctx, &deployment)
	if err != nil {
		return err
	}

	var updatedDeployment appsv1.Deployment
	for {
		err := client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace, Name: deploymentName,
		}, &updatedDeployment)
		if err != nil {
			return err
		}
		if updatedDeployment.Status.Replicas == replicas {
			break
		}
	}

	return nil
}

func FetchAllCephClusters(ctx context.Context, client client.Client) (*rookv1.CephClusterList, error) {
	var cephClusters rookv1.CephClusterList
	err := client.List(ctx, &cephClusters)
	if err != nil {
		return nil, fmt.Errorf("failed to list CephClusters %v", err)
	}
	return &cephClusters, nil
}
