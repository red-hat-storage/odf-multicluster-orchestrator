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

package storagevendor

import (
	"context"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	ctrl "sigs.k8s.io/controller-runtime"
)

// StorageVendor defines the interface for storage vendor-specific implementations
type StorageVendor interface {
	// ValidateVersion performs vendor-specific version compatibility checks
	ValidateVersion(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)

	// ConfigureStorageForDR sets up dr resources for vendor
	ConfigureStorageForDR(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)

	// CleanupResources cleans up dr resources for vendor
	CleanupResources(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error)
}
