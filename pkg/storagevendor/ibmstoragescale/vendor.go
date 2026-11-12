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

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
func NewIbmStorageScaleVendor(
	ctx context.Context,
	cl client.Client,
	scheme *runtime.Scheme,
	logger *slog.Logger,
	currentNamespace string,
) (*IbmStorageScaleVendor, error) {
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
func (v *IbmStorageScaleVendor) ConfigureStorageForDR(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
) (ctrl.Result, error) {
	// Ensure CRDs are installed on both clusters before syncing manifests.
	for _, peerRef := range mirrorPeer.Spec.Items {
		err := utils.EnsureCRDs(
			ctx,
			v.client,
			v.logger,
			peerRef.ClusterName,
			utils.ManifestCRDsMWName,
			utils.SourceManifestCRDPath,
			utils.AppliedManifestCRDPath,
		)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Read every source cluster's SourceManifests and push them to the peer.
	notReady := false
	for i, source := range mirrorPeer.Spec.Items {

		sourceInfo, err := getIbmStorageScaleInfoFromConfigMap(
			v.infoMap,
			utils.GetKey(source.ClusterName, source.StorageClusterRef.Name),
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", source.ClusterName, err)
		}

		peer := mirrorPeer.Spec.Items[1-i]
		peerInfo, err := getIbmStorageScaleInfoFromConfigMap(
			v.infoMap,
			utils.GetKey(peer.ClusterName, peer.StorageClusterRef.Name),
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to get IBM Storage Scale info for cluster %q: %w", peer.ClusterName, err)
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

// readSourceManifests reads the SourceManifests listed in the cluster's info ConfigMap.
// It returns each SourceManifest's resources, keyed by SourceManifest name.
func (v *IbmStorageScaleVendor) readSourceManifests(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	peerRef multiclusterv1alpha1.PeerRef,
	info IbmStorageScaleInfo,
) (map[string][]runtime.RawExtension, bool, error) {
	if len(info.SourceManifests) == 0 {
		v.logger.Info("No sourceManifests listed for cluster yet", "cluster", peerRef.ClusterName)
		return nil, false, nil
	}

	resourcesByManifestName := make(map[string][]runtime.RawExtension, len(info.SourceManifests))
	ready := true
	for _, name := range info.SourceManifests {
		// Always create/update every MCV before bailing out so they are all populated in parallel.
		mcv := &viewv1beta1.ManagedClusterView{}
		mcv.Name = getSourceManifestMCVName(name)
		mcv.Namespace = peerRef.ClusterName

		mcvConfig := func() error {
			mcv.Spec = viewv1beta1.ViewSpec{
				Scope: viewv1beta1.ViewScope{
					Group:     multiclusterv1alpha1.GroupVersion.Group,
					Resource:  "sourcemanifest",
					Name:      name,
					Namespace: info.OperatorNamespace,
				},
			}

			utils.AddLabel(mcv, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
			utils.AddLabel(mcv, utils.HubRecoveryLabel, "")

			return controllerutil.SetControllerReference(mirrorPeer, mcv, v.scheme)
		}

		_, err := controllerutil.CreateOrUpdate(ctx, v.client, mcv, mcvConfig)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create or update ManagedClusterView %q in namespace %q: %w",
				mcv.Name, mcv.Namespace, err)
		}

		// Validate MCV is ready
		mcvReady, err := v.validateMCVReady(mcv, "SourceManifest", name, peerRef.ClusterName)
		if err != nil {
			return nil, false, err
		}
		if !mcvReady {
			ready = false
			continue
		}

		var sourceManifest multiclusterv1alpha1.SourceManifest
		if err := json.Unmarshal(mcv.Status.Result.Raw, &sourceManifest); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal SourceManifest %q from ManagedClusterView %q: %w",
				name, mcv.Name, err)
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
			appliedManifest,
		)
		if err != nil {
			return err
		}

		v.logger.Info("ManifestWork for AppliedManifest created/updated",
			"cluster", peer.ClusterName,
			"appliedManifest", manifestName,
			"result", result,
		)

		mw, err := utils.GetManifestWork(ctx, v.client, getAppliedManifestWorkName(manifestName), peer.ClusterName)
		if err != nil {
			return err
		}

		isApplied := utils.CheckManifestWorkStatus(mw)
		if !isApplied {
			return fmt.Errorf("ManifestWork for AppliedManifest %s/%s not applied", mw.Namespace, mw.Name)
		}

	}
	return nil
}

func getAppliedManifestStatusMCVName(appliedManifestName string) string {
	return fmt.Sprintf(appliedManifestStatusMCVNameTemplate, appliedManifestName)
}

// checkAppliedManifestsStatus reads back every pushed AppliedManifest's status from the peer and
// aggregates them. ready is true only when all AppliedManifests report Applied and are fresh.
// It returns an error if any AppliedManifest reports a Failed or Degraded phase.
func (v *IbmStorageScaleVendor) checkAppliedManifestsStatus(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	peer multiclusterv1alpha1.PeerRef,
	info IbmStorageScaleInfo,
	resourcesByManifestName map[string][]runtime.RawExtension,
) (bool, error) {
	ready := true
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
func (v *IbmStorageScaleVendor) readAppliedManifestStatus(
	ctx context.Context,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	clusterName,
	operatorNamespace,
	name string,
) (multiclusterv1alpha1.AppliedManifestStatus, bool, error) {
	mcv := &viewv1beta1.ManagedClusterView{}
	mcv.Name = getAppliedManifestStatusMCVName(name)
	mcv.Namespace = clusterName

	mcvConfig := func() error {
		mcv.Spec = viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Group:     multiclusterv1alpha1.GroupVersion.Group,
				Resource:  "appliedmanifest",
				Name:      name,
				Namespace: operatorNamespace,
			},
		}

		utils.AddLabel(mcv, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
		utils.AddLabel(mcv, utils.HubRecoveryLabel, "")

		return controllerutil.SetControllerReference(mirrorPeer, mcv, v.scheme)
	}

	_, err := controllerutil.CreateOrUpdate(ctx, v.client, mcv, mcvConfig)
	if err != nil {
		return multiclusterv1alpha1.AppliedManifestStatus{}, false,
			fmt.Errorf("failed to create or update ManagedClusterView %q in namespace %q: %w",
				mcv.Name, mcv.Namespace, err)
	}

	// Validate MCV is ready
	mcvReady, err := v.validateMCVReady(mcv, "AppliedManifest", name, clusterName)
	if err != nil {
		return multiclusterv1alpha1.AppliedManifestStatus{}, false, err
	}
	if !mcvReady {
		return multiclusterv1alpha1.AppliedManifestStatus{}, false, nil
	}

	var appliedManifest multiclusterv1alpha1.AppliedManifest
	if err := json.Unmarshal(mcv.Status.Result.Raw, &appliedManifest); err != nil {
		return multiclusterv1alpha1.AppliedManifestStatus{},
			false,
			fmt.Errorf("failed to unmarshal AppliedManifest %q from ManagedClusterView %q: %w", name, mcv.Name, err)
	}

	if appliedManifest.Status.ObservedGeneration != appliedManifest.Generation {
		v.logger.Info("AppliedManifest status is stale, waiting for the operator to catch up",
			"cluster", clusterName,
			"appliedManifest", name,
			"generation", appliedManifest.Generation,
			"observedGeneration", appliedManifest.Status.ObservedGeneration,
		)
		return multiclusterv1alpha1.AppliedManifestStatus{}, false, nil
	}

	return appliedManifest.Status, true, nil
}

// validateMCVReady checks if a ManagedClusterView has valid conditions and a populated result.
// Returns (ready, error):
//   - (true, nil): MCV is ready, result can be used
//   - (false, nil): MCV not ready yet, should requeue
//   - (false, error): MCV failed or invalid, should stop
func (v *IbmStorageScaleVendor) validateMCVReady(
	mcv *viewv1beta1.ManagedClusterView,
	resourceType,
	resourceName,
	clusterName string,
) (bool, error) {
	// Check conditions
	switch len(mcv.Status.Conditions) {
	case 0:
		v.logger.Info("ManagedClusterView conditions not available yet",
			"cluster", clusterName,
			"resourceType", resourceType,
			"resourceName", resourceName,
			"mcv", mcv.Name,
		)
		return false, nil
	case 1:
		cond := mcv.Status.Conditions[0]
		if cond.Type != viewv1beta1.ConditionViewProcessing {
			return false, fmt.Errorf("invalid ManagedClusterView condition type %q for %s %q on cluster %q",
				cond.Type, resourceType, resourceName, clusterName)
		}
		if cond.Reason == viewv1beta1.ReasonGetResourceFailed {
			return false, fmt.Errorf("%s %q not found on cluster %q: %s",
				resourceType, resourceName, clusterName, cond.Message)
		}
		if cond.Status != metav1.ConditionTrue {
			v.logger.Info("ManagedClusterView not ready yet",
				"cluster", clusterName,
				"resourceType", resourceType,
				"resourceName", resourceName,
				"reason", cond.Reason,
				"message", cond.Message,
			)
			return false, nil
		}
	default:
		return false, fmt.Errorf("unexpected: ManagedClusterView has %d conditions (expected 1) for %s %q on cluster %q",
			len(mcv.Status.Conditions), resourceType, resourceName, clusterName)
	}

	// Check result is populated
	if len(mcv.Status.Result.Raw) == 0 {
		v.logger.Info("ManagedClusterView result not populated yet",
			"cluster", clusterName,
			"resourceType", resourceType,
			"resourceName", resourceName,
		)
		return false, nil
	}

	return true, nil
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
			Kind:       "AppliedManifest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: multiclusterv1alpha1.AppliedManifestSpec{Manifests: resources},
	})
}
