package utils

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
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
