package odf

import (
	"context"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ODFInfoConfigMapName              = "odf-info"
	ClientInfoConfigMapName           = "odf-client-info"
	StorageClientMappingConfigMapName = "storage-client-mapping"
)

// GetODFInfoConfigMap fetches the odf-info ConfigMap from the given namespace. This will only work on the managed cluster
func GetODFInfoConfigMap(ctx context.Context, c client.Client, namespace string) (*corev1.ConfigMap, error) {
	return utils.FetchConfigMap(ctx, c, ODFInfoConfigMapName, namespace)
}

func GetClientInfoConfigMap(ctx context.Context, c client.Client, currentNamespace string) (*corev1.ConfigMap, error) {
	return utils.FetchConfigMap(ctx, c, ClientInfoConfigMapName, currentNamespace)
}

func GetStorageClientMapping(ctx context.Context, c client.Client, currentNamespace string) (*corev1.ConfigMap, error) {
	return utils.FetchConfigMap(ctx, c, StorageClientMappingConfigMapName, currentNamespace)
}
