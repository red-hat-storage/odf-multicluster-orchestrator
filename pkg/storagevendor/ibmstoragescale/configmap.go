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

package ibmstoragescale

import (
	"context"
	"encoding/json"
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// IbmStorageScaleConfigMapName is the name of the ConfigMap on the hub that stores IBM Storage Scale cluster information
	IbmStorageScaleConfigMapName = "ibm-storage-scale-info"
)

// getIbmStorageScaleConfigMap fetches the IBM Storage Scale info ConfigMap from the hub.
func getIbmStorageScaleConfigMap(ctx context.Context, cl client.Client, namespace string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	cm.Name = IbmStorageScaleConfigMapName
	cm.Namespace = namespace
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cm), cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q in namespace %q: %w", IbmStorageScaleConfigMapName, namespace, err)
	}
	return cm, nil
}

// getScaleInfo returns the IBM Storage Scale info for the cluster referenced by ref.
func (v *IbmStorageScaleVendor) getScaleInfo(ref multiclusterv1alpha1.PeerRef) (IbmStorageScaleInfo, error) {
	info, err := getIbmStorageScaleInfoFromConfigMap(v.infoMap, utils.GetKey(ref.ClusterName, ref.StorageClusterRef.Name))
	if err != nil {
		return info, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", ref.ClusterName, err)
	}
	return info, nil
}

// getIbmStorageScaleInfoFromConfigMap returns the IbmStorageScaleInfo stored under key in the info map.
func getIbmStorageScaleInfoFromConfigMap(infoMap map[string]string, key string) (IbmStorageScaleInfo, error) {
	var info IbmStorageScaleInfo
	raw, ok := infoMap[key]
	if !ok {
		return info, fmt.Errorf("no IBM Storage Scale info found for key %q", key)
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return info, fmt.Errorf("failed to unmarshal IBM Storage Scale info for key %q: %w", key, err)
	}
	return info, nil
}
