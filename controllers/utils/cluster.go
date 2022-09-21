package utils

import (
	"context"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterType string

const (
	CONVERGED ClusterType = "Converged"
	EXTERNAL  ClusterType = "External"
	UNKNOWN   ClusterType = "Unknown"
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
