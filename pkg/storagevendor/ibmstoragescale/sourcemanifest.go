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

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	sourceManifestMCVNameTemplate = "ibmscale-sm-%s"

	sourceManifestResource = "sourcemanifest"
)

func getSourceManifestMCVName(sourceManifestName string) string {
	return fmt.Sprintf(sourceManifestMCVNameTemplate, sourceManifestName)
}

// readSourceManifests reads the SourceManifests listed in the cluster's info ConfigMap.
// It returns the cluster's SourceManifests by name.
func (v *IbmStorageScaleVendor) readSourceManifests(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, item multiclusterv1alpha1.PeerRef) (manifests map[string][]runtime.RawExtension, ready bool, err error) {
	logger := v.logger.With("cluster", item.ClusterName)

	info, err := getIbmStorageScaleInfoFromConfigMap(v.infoMap, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
	if err != nil {
		return nil, false, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", item.ClusterName, err)
	}

	if len(info.SourceManifests) == 0 {
		logger.Info("No sourceManifests listed for cluster yet")
		return nil, false, nil
	}

	manifests = make(map[string][]runtime.RawExtension, len(info.SourceManifests))
	ready = true
	for _, name := range info.SourceManifests {
		// Always create/update every MCV before bailing out so they are all populated in parallel.
		mcv, err := v.createOrUpdateSourceManifestMCV(ctx, mirrorPeer, item.ClusterName, info.OperatorNamespace, name)
		if err != nil {
			return nil, false, err
		}

		if len(mcv.Status.Result.Raw) == 0 {
			logger.Info("SourceManifest ManagedClusterView result not available yet", "sourceManifest", name, "mcv", mcv.Name)
			ready = false
			continue
		}

		var sm multiclusterv1alpha1.SourceManifest
		if err := json.Unmarshal(mcv.Status.Result.Raw, &sm); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal SourceManifest %q from ManagedClusterView %q: %w", name, mcv.Name, err)
		}
		manifests[name] = sm.Spec.Manifests
	}

	if !ready {
		return nil, false, nil
	}
	return manifests, true, nil
}

// createOrUpdateSourceManifestMCV creates or updates the MCV that reads the SourceManifest
// from its namespace on the managed cluster. This MCV is owned by the MirrorPeer.
func (v *IbmStorageScaleVendor) createOrUpdateSourceManifestMCV(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clusterName, sourceNamespace, name string) (*viewv1beta1.ManagedClusterView, error) {
	mcv := &viewv1beta1.ManagedClusterView{}
	mcv.Name = getSourceManifestMCVName(name)
	mcv.Namespace = clusterName

	_, err := controllerutil.CreateOrUpdate(ctx, v.client, mcv, func() error {
		mcv.Spec = viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Group:     multiclusterv1alpha1.GroupVersion.Group,
				Resource:  sourceManifestResource,
				Name:      name,
				Namespace: sourceNamespace,
			},
		}

		utils.AddLabel(mcv, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
		utils.AddLabel(mcv, utils.HubRecoveryLabel, "")

		return controllerutil.SetControllerReference(mirrorPeer, mcv, v.scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update ManagedClusterView %q in namespace %q: %w", mcv.Name, mcv.Namespace, err)
	}
	return mcv, nil
}
