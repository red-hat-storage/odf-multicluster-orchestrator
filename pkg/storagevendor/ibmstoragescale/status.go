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

func getAppliedManifestStatusMCVName(appliedManifestName string) string {
	return fmt.Sprintf(appliedManifestStatusMCVNameTemplate, appliedManifestName)
}

// checkAppliedManifestsStatus reads back every pushed AppliedManifest's status from the peer and
// aggregates them. ready is true only when all AppliedManifests report Applied and are fresh.
// It returns an error if any AppliedManifest reports a Failed or Degraded phase.
func (v *IbmStorageScaleVendor) checkAppliedManifestsStatus(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, peer multiclusterv1alpha1.PeerRef, info IbmStorageScaleInfo, resourcesByManifestName map[string][]runtime.RawExtension) (ready bool, err error) {
	ready = true
	for name := range resourcesByManifestName {
		status, manifestReady, err := v.readAppliedManifestStatus(ctx, mirrorPeer, peer.ClusterName, info.OperatorNamespace, name)
		if err != nil {
			return false, err
		}
		if !manifestReady {
			ready = false
			continue
		}

		switch status.Phase {
		case multiclusterv1alpha1.AppliedManifestPhaseApplied:
			// applied and fresh; nothing to wait for
		case multiclusterv1alpha1.AppliedManifestPhaseFailed, multiclusterv1alpha1.AppliedManifestPhaseDegraded:
			return false, fmt.Errorf("AppliedManifest %q on cluster %q is in phase %q", name, peer.ClusterName, status.Phase)
		default:
			// Pending / Applying / Terminating / empty — status not Applied yet; keep waiting.
			ready = false
		}
	}
	return ready, nil
}

// readAppliedManifestStatus reads an AppliedManifest's status back from the peer cluster via an MCV.
// It returns the status, or ready=false if the MCV result is not available yet or the reported
// status is stale (observedGeneration behind the AppliedManifest's generation).
func (v *IbmStorageScaleVendor) readAppliedManifestStatus(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clusterName, operatorNamespace, name string) (status multiclusterv1alpha1.AppliedManifestStatus, ready bool, err error) {
	appliedManifestStatusMCVName := getAppliedManifestStatusMCVName(name)
	mcv, err := v.createOrUpdateMCV(ctx, mirrorPeer, appliedManifestStatusMCVName, clusterName, appliedManifestResource, operatorNamespace, name)
	if err != nil {
		return status, false, err
	}

	if len(mcv.Status.Result.Raw) == 0 {
		v.logger.Info("AppliedManifest status ManagedClusterView result not available yet", "cluster", clusterName, "appliedManifest", name, "mcv", mcv.Name)
		return status, false, nil
	}

	var appliedManifest multiclusterv1alpha1.AppliedManifest
	if err := json.Unmarshal(mcv.Status.Result.Raw, &appliedManifest); err != nil {
		return status, false, fmt.Errorf("failed to unmarshal AppliedManifest %q from ManagedClusterView %q: %w", name, mcv.Name, err)
	}

	if appliedManifest.Status.ObservedGeneration != appliedManifest.Generation {
		v.logger.Info("AppliedManifest status is stale, waiting for the operator to catch up", "cluster", clusterName, "appliedManifest", name, "generation", appliedManifest.Generation, "observedGeneration", appliedManifest.Status.ObservedGeneration)
		return status, false, nil
	}

	return appliedManifest.Status, true, nil
}
