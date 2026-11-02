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

	"k8s.io/apimachinery/pkg/runtime"
)

func getSourceManifestMCVName(sourceManifestName string) string {
	return fmt.Sprintf(sourceManifestMCVNameTemplate, sourceManifestName)
}

// readSourceManifests reads the SourceManifests listed in the cluster's info ConfigMap.
// It returns each SourceManifest's resources, keyed by SourceManifest name.
func (v *IbmStorageScaleVendor) readSourceManifests(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, item multiclusterv1alpha1.PeerRef, info IbmStorageScaleInfo) (resourcesByManifestName map[string][]runtime.RawExtension, ready bool, err error) {
	if len(info.SourceManifests) == 0 {
		v.logger.Info("No sourceManifests listed for cluster yet", "cluster", item.ClusterName)
		return nil, false, nil
	}

	resourcesByManifestName = make(map[string][]runtime.RawExtension, len(info.SourceManifests))
	ready = true
	for _, name := range info.SourceManifests {
		// Always create/update every MCV before bailing out so they are all populated in parallel.
		resources, manifestReady, err := v.readSourceManifest(ctx, mirrorPeer, item.ClusterName, info.OperatorNamespace, name)
		if err != nil {
			return nil, false, err
		}
		if !manifestReady {
			ready = false
			continue
		}
		resourcesByManifestName[name] = resources
	}

	if !ready {
		return nil, false, nil
	}
	return resourcesByManifestName, true, nil
}

// readSourceManifest reads a single SourceManifest from the managed cluster via an MCV.
// It returns the SourceManifest's resources, or ready=false if the MCV result is not available yet.
func (v *IbmStorageScaleVendor) readSourceManifest(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clusterName, sourceNamespace, name string) (resources []runtime.RawExtension, ready bool, err error) {
	sourceManifestMCVName := getSourceManifestMCVName(name)
	mcv, err := v.createOrUpdateMCV(ctx, mirrorPeer, sourceManifestMCVName, clusterName, sourceManifestResource, sourceNamespace, name)
	if err != nil {
		return nil, false, err
	}

	if len(mcv.Status.Result.Raw) == 0 {
		v.logger.Info("SourceManifest ManagedClusterView result not available yet", "cluster", clusterName, "sourceManifest", name, "mcv", mcv.Name)
		return nil, false, nil
	}

	var sourceManifest multiclusterv1alpha1.SourceManifest
	if err := json.Unmarshal(mcv.Status.Result.Raw, &sourceManifest); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal SourceManifest %q from ManagedClusterView %q: %w", name, mcv.Name, err)
	}
	return sourceManifest.Spec.Manifests, true, nil
}
