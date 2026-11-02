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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func getAppliedManifestWorkName(sourceManifestName string) string {
	return fmt.Sprintf(appliedManifestWorkNameTemplate, sourceManifestName)
}

// buildAppliedManifest returns the JSON of an AppliedManifest carrying the given resources.
func buildAppliedManifest(name, namespace string, resources []runtime.RawExtension) ([]byte, error) {
	return json.Marshal(&multiclusterv1alpha1.AppliedManifest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: multiclusterv1alpha1.GroupVersion.String(),
			Kind:       appliedManifestKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: multiclusterv1alpha1.AppliedManifestSpec{Manifests: resources},
	})
}

// syncAppliedManifests creates or updates one ManifestWork per SourceManifest on the peer
// cluster. Each ManifestWork carries an AppliedManifest with the same payload.
func (v *IbmStorageScaleVendor) syncAppliedManifests(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, peer multiclusterv1alpha1.PeerRef, info IbmStorageScaleInfo, resourcesByManifestName map[string][]runtime.RawExtension) error {
	for manifestName, resources := range resourcesByManifestName {
		if err := v.syncAppliedManifest(ctx, mirrorPeer,
			peer.ClusterName, info.OperatorNamespace, manifestName, resources); err != nil {
			return err
		}
	}
	return nil
}

// syncAppliedManifest syncs a single AppliedManifest to the peer cluster as a ManifestWork.
func (v *IbmStorageScaleVendor) syncAppliedManifest(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	clusterName, operatorNamespace, name string, resources []runtime.RawExtension) error {
	appliedManifest, err := buildAppliedManifest(name, operatorNamespace, resources)
	if err != nil {
		return fmt.Errorf("failed to marshal AppliedManifest %q: %w", name, err)
	}

	ownerRef := *metav1.NewControllerRef(mirrorPeer, multiclusterv1alpha1.GroupVersion.WithKind("MirrorPeer"))
	result, err := utils.CreateOrUpdateManifestWork(ctx, v.client, getAppliedManifestWorkName(name),
		clusterName, nil, ownerRef, appliedManifest)
	if err != nil {
		return err
	}

	v.logger.Info("ManifestWork for AppliedManifest synced", "cluster", clusterName, "appliedManifest", name, "result", result)
	return nil
}
