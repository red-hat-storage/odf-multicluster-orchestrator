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

package utils

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getCRDBytes reads a CRD YAML file from the given path.
// It first tries reading from the module root, then falls back to a relative path.
func getCRDBytes(crdPath string) ([]byte, error) {
	// Try reading from the expected location relative to the module root
	crdBytes, err := os.ReadFile(crdPath)
	if err != nil {
		// Fallback: try from three levels up (when running from pkg/storagevendor/*)
		crdBytes, err = os.ReadFile(filepath.Join("..", "..", "..", crdPath))
		if err != nil {
			return nil, fmt.Errorf("failed to read CRD file %q: %w", crdPath, err)
		}
	}
	return crdBytes, nil
}

// EnsureCRD creates or updates a ManifestWork that installs a CRD on the specified managed cluster.
// The ManifestWork is owned by the MirrorPeer for automatic cleanup.
func EnsureCRD(
	ctx context.Context,
	client client.Client,
	log *slog.Logger,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	clusterName string,
	crdPath string,
	crdManifestWorkName string,
) error {
	crdBytes, err := getCRDBytes(crdPath)
	if err != nil {
		return err
	}

	ownerRef := *metav1.NewControllerRef(mirrorPeer, multiclusterv1alpha1.GroupVersion.WithKind("MirrorPeer"))

	result, err := CreateOrUpdateManifestWork(
		ctx,
		client,
		crdManifestWorkName,
		clusterName,
		nil,
		ownerRef,
		crdBytes,
	)
	if err != nil {
		return fmt.Errorf("failed to create or update %s ManifestWork for cluster %s: %w",
			crdManifestWorkName, clusterName, err)
	}

	log.Info("CRD ManifestWork synced", "manifestWork", crdManifestWorkName, "cluster", clusterName, "result", result)
	return nil
}
