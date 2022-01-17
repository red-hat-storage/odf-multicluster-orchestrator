package common

import (
	"fmt"
	"os"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
)

const (
	RamenHubNamespace  = "openshift-dr-system"
	BucketGenerateName = "odrbucket"
)

func GetCurrentStorageClusterRef(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) (*multiclusterv1alpha1.StorageClusterRef, error) {
	for _, v := range mp.Spec.Items {
		if v.ClusterName == spokeClusterName {
			return &v.StorageClusterRef, nil
		}
	}
	return nil, fmt.Errorf("StorageClusterRef for cluster %s under mirrorpeer %s not found", spokeClusterName, mp.Name)
}

func GetEnv(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
