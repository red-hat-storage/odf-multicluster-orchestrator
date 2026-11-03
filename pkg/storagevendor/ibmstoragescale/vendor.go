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
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IbmStorageScaleVendor implements the StorageVendor interface for IBM Storage Scale (CNSA).
type IbmStorageScaleVendor struct {
	client           client.Client
	scheme           *runtime.Scheme
	logger           *slog.Logger
	currentNamespace string
	infoMap          map[string]string
}

// NewIbmStorageScaleVendor constructs the IBM Storage Scale StorageVendor. It loads the
// hub info ConfigMap; if it is not present yet (discovery not complete), this returns an
// error so the MirrorPeer reconcile requeues until it appears.
func NewIbmStorageScaleVendor(ctx context.Context, cl client.Client, scheme *runtime.Scheme, logger *slog.Logger, currentNamespace string) (*IbmStorageScaleVendor, error) {
	infoConfigMap, err := getIbmStorageScaleConfigMap(ctx, cl, currentNamespace)
	if err != nil {
		return nil, err
	}
	return &IbmStorageScaleVendor{
		client:           cl,
		scheme:           scheme,
		logger:           logger.With("storageVendor", "ibmstoragescale"),
		currentNamespace: currentNamespace,
		infoMap:          infoConfigMap.Data,
	}, nil
}

// ValidateVersion checks that both peers are discovered and their IBM Storage Scale
// versions are present and compatible with each other.
func (v *IbmStorageScaleVendor) ValidateVersion(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
) (ctrl.Result, error) {
	if len(mirrorPeer.Spec.Items) != 2 {
		return ctrl.Result{}, fmt.Errorf("expected exactly 2 items in MirrorPeer %q, got %d", mirrorPeer.Name, len(mirrorPeer.Spec.Items))
	}

	versions := make([]string, 0, 2)
	for i := range mirrorPeer.Spec.Items {
		peerRef := mirrorPeer.Spec.Items[i]
		info, err := getIbmStorageScaleInfoFromConfigMap(v.infoMap, utils.GetKey(peerRef.ClusterName, peerRef.StorageClusterRef.Name))
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", peerRef.ClusterName, err)
		}
		if info.Version == "" {
			return ctrl.Result{}, fmt.Errorf("IBM Storage Scale version is empty for cluster %q", peerRef.ClusterName)
		}
		versions = append(versions, info.Version)
	}

	eq, err := utils.CompareSemverMajorMinorVersions(versions[0], versions[1], utils.Eq)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to parse IBM Storage Scale versions %q and %q: %w", versions[0], versions[1], err)
	}
	if !eq {
		return ctrl.Result{}, fmt.Errorf("incompatible IBM Storage Scale versions across peers: %q and %q", versions[0], versions[1])
	}

	// TODO: optionally enforce a minimum supported IBM Storage Scale version here.
	return ctrl.Result{}, nil

}

// ConfigureStorageForDR sets up DR resources for IBM Storage Scale.
func (v *IbmStorageScaleVendor) ConfigureStorageForDR(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	// read every source cluster's SourceManifests and push them to the peer.
	notReady := false
	for i, source := range mirrorPeer.Spec.Items {
		sourceInfo, err := v.getScaleInfo(source)
		if err != nil {
			return ctrl.Result{}, err
		}

		peer := mirrorPeer.Spec.Items[1-i]
		peerInfo, err := v.getScaleInfo(peer)
		if err != nil {
			return ctrl.Result{}, err
		}

		resourcesByManifestName, ready, err := v.readSourceManifests(ctx, mirrorPeer, source, sourceInfo)
		if err != nil {
			v.logger.Error("Failed to read SourceManifests", "cluster", source.ClusterName, "error", err)
			return ctrl.Result{}, err
		}
		if !ready {
			notReady = true
			continue
		}

		if err := v.syncAppliedManifests(ctx, mirrorPeer, peer, peerInfo, resourcesByManifestName); err != nil {
			v.logger.Error("Failed to sync AppliedManifests", "cluster", peer.ClusterName, "error", err)
			return ctrl.Result{}, err
		}

		statusReady, err := v.checkAppliedManifestsStatus(ctx, mirrorPeer, peer, peerInfo, resourcesByManifestName)
		if err != nil {
			v.logger.Error("AppliedManifest reported a failure", "cluster", peer.ClusterName, "error", err)
			return ctrl.Result{}, err
		}
		if !statusReady {
			notReady = true
		}
	}
	if notReady {
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// CleanupResources cleans up DR resources for IBM Storage Scale.
func (v *IbmStorageScaleVendor) CleanupResources(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
) (ctrl.Result, error) {
	// Placeholder: delete the owned MCVs and ManifestWorks (owner-reference GC handles most of it).
	return ctrl.Result{}, nil
}

// getScaleInfo returns the IBM Storage Scale info for the cluster referenced by ref.
func (v *IbmStorageScaleVendor) getScaleInfo(ref multiclusterv1alpha1.PeerRef) (IbmStorageScaleInfo, error) {
	info, err := getIbmStorageScaleInfoFromConfigMap(v.infoMap, utils.GetKey(ref.ClusterName, ref.StorageClusterRef.Name))
	if err != nil {
		return info, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", ref.ClusterName, err)
	}
	return info, nil
}

// readSourceManifests reads the SourceManifests listed in the cluster's info ConfigMap.
// It returns each SourceManifest's resources, keyed by SourceManifest name.
func (v *IbmStorageScaleVendor) readSourceManifests(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	peerRef multiclusterv1alpha1.PeerRef,
	info IbmStorageScaleInfo,
) (resourcesByManifestName map[string][]runtime.RawExtension, ready bool, err error) {
	if len(info.SourceManifests) == 0 {
		v.logger.Info("No sourceManifests listed for cluster yet", "cluster", peerRef.ClusterName)
		return nil, false, nil
	}

	resourcesByManifestName = make(map[string][]runtime.RawExtension, len(info.SourceManifests))
	ready = true
	for _, name := range info.SourceManifests {
		// Always create/update every MCV before bailing out so they are all populated in parallel.
		mcv, err := v.createOrUpdateMCV(ctx,
			mirrorPeer,
			getSourceManifestMCVName(name),
			peerRef.ClusterName,
			sourceManifestResource,
			info.OperatorNamespace,
			name,
		)
		if err != nil {
			return nil, false, err
		}

		if len(mcv.Status.Result.Raw) == 0 {
			v.logger.Info("SourceManifest ManagedClusterView result not available yet", "cluster", peerRef.ClusterName, "sourceManifest", name, "mcv", mcv.Name)
			ready = false
			continue
		}

		var sourceManifest multiclusterv1alpha1.SourceManifest
		if err := json.Unmarshal(mcv.Status.Result.Raw, &sourceManifest); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal SourceManifest %q from ManagedClusterView %q: %w", name, mcv.Name, err)
		}
		resourcesByManifestName[name] = sourceManifest.Spec.Manifests
	}

	if !ready {
		return nil, false, nil
	}
	return resourcesByManifestName, true, nil
}

// syncAppliedManifests creates or updates one ManifestWork per SourceManifest on the peer
// cluster. Each ManifestWork carries an AppliedManifest with the same payload.
func (v *IbmStorageScaleVendor) syncAppliedManifests(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	peer multiclusterv1alpha1.PeerRef,
	info IbmStorageScaleInfo,
	resourcesByManifestName map[string][]runtime.RawExtension,
) error {
	for manifestName, resources := range resourcesByManifestName {
		appliedManifest, err := buildAppliedManifest(manifestName, info.OperatorNamespace, resources)
		if err != nil {
			return fmt.Errorf("failed to marshal AppliedManifest %q: %w", manifestName, err)
		}

		ownerRef := *metav1.NewControllerRef(mirrorPeer, multiclusterv1alpha1.GroupVersion.WithKind("MirrorPeer"))

		result, err := utils.CreateOrUpdateManifestWork(
			ctx,
			v.client,
			getAppliedManifestWorkName(manifestName),
			peer.ClusterName,
			nil,
			ownerRef,
			appliedManifest)
		if err != nil {
			return err
		}

		v.logger.Info("ManifestWork for AppliedManifest synced", "cluster",
			peer.ClusterName, "appliedManifest", manifestName, "result", result)
	}
	return nil
}

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

func getSourceManifestMCVName(sourceManifestName string) string {
	return fmt.Sprintf(sourceManifestMCVNameTemplate, sourceManifestName)
}

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
