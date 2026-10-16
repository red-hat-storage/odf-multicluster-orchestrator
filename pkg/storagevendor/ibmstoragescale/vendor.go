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
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

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
func (v *IbmStorageScaleVendor) ValidateVersion(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	if err := arePeerVersionsCompatible(mirrorPeer, v.infoMap); err != nil {
		v.logger.Error("IBM Storage Scale version validation failed", "error", err)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ConfigureStorageForDR sets up DR resources for IBM Storage Scale.
func (v *IbmStorageScaleVendor) ConfigureStorageForDR(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	// Placeholder: for each PeerRef — read sourceManifests from the local configmap, create the
	// SourceManifest MCV, create the ManifestWork (AppliedManifest) on the peer, create the
	// status MCV, and reflect status onto the MirrorPeer.
	return ctrl.Result{}, nil
}

// CleanupResources cleans up DR resources for IBM Storage Scale.
func (v *IbmStorageScaleVendor) CleanupResources(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	// Placeholder: delete the owned MCVs and ManifestWorks (owner-reference GC handles most of it).
	return ctrl.Result{}, nil
}
