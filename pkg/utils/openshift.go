package utils

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetOpenShiftVersion(ctx context.Context, cl client.Client) (string, error) {
	clusterVersion := &configv1.ClusterVersion{}
	clusterVersion.Name = "version"
	if err := cl.Get(ctx, client.ObjectKeyFromObject(clusterVersion), clusterVersion); err != nil {
		return "", err
	}

	for _, historyEntry := range clusterVersion.Status.History {
		if historyEntry.State == configv1.CompletedUpdate {
			return historyEntry.Version, nil
		}
	}

	return "", fmt.Errorf("no completed version found in clusterVersion status history")
}

// GetClusterID returns the cluster ID of the OCP-Cluster
func GetClusterID(ctx context.Context, client client.Client, clusterName string) (string, error) {
	var managedCluster clusterv1.ManagedCluster
	err := client.Get(ctx, types.NamespacedName{Name: clusterName}, &managedCluster)
	if err != nil {
		return "", err
	}
	return managedCluster.GetLabels()["clusterID"], nil
}
