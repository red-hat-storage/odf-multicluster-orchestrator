/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package odf

import (
	"context"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

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
