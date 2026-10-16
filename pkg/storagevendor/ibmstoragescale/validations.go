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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getIbmStorageScaleConfigMap fetches the IBM Storage Scale info ConfigMap from the hub.
func getIbmStorageScaleConfigMap(ctx context.Context, cl client.Client, namespace string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: IbmStorageScaleConfigMapName, Namespace: namespace}
	if err := cl.Get(ctx, key, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q in namespace %q: %w", IbmStorageScaleConfigMapName, namespace, err)
	}
	return cm, nil
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

// arePeerVersionsCompatible verifies both peers are discovered and their IBM Storage
// Scale versions are present and compatible (equal at major.minor) with each other.
func arePeerVersionsCompatible(mirrorPeer *multiclusterv1alpha1.MirrorPeer, infoMap map[string]string) error {
	if len(mirrorPeer.Spec.Items) != 2 {
		return fmt.Errorf("expected exactly 2 items in MirrorPeer %q, got %d", mirrorPeer.Name, len(mirrorPeer.Spec.Items))
	}

	versions := make([]string, 0, 2)
	for i := range mirrorPeer.Spec.Items {
		item := mirrorPeer.Spec.Items[i]
		info, err := getIbmStorageScaleInfoFromConfigMap(infoMap, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", item.ClusterName, err)
		}
		if info.Version == "" {
			return fmt.Errorf("IBM Storage Scale version is empty for cluster %q", item.ClusterName)
		}
		versions = append(versions, info.Version)
	}

	eq, err := utils.CompareSemverMajorMinorVersions(versions[0], versions[1], utils.Eq)
	if err != nil {
		return fmt.Errorf("unable to parse IBM Storage Scale versions %q and %q: %w", versions[0], versions[1], err)
	}
	if !eq {
		return fmt.Errorf("incompatible IBM Storage Scale versions across peers: %q and %q", versions[0], versions[1])
	}

	// TODO: optionally enforce a minimum supported IBM Storage Scale version here.
	return nil
}
