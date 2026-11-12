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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnsureCRDs creates or updates a ManifestWork that installs CRDs on the specified managed cluster.
func EnsureCRDs(
	ctx context.Context,
	client client.Client,
	log *slog.Logger,
	namespace string,
	manifestWorkName string,
	crdPaths ...string,
) error {
	crdBytes := make([][]byte, len(crdPaths))

	for i, path := range crdPaths {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read CRD file %q: %w", path, err)
		}
		crdBytes[i] = bytes
	}

	result, err := CreateOrUpdateManifestWork(
		ctx,
		client,
		manifestWorkName,
		namespace,
		nil,
		metav1.OwnerReference{},
		crdBytes...,
	)
	if err != nil {
		return err
	}

	log.Info("ManifestWork for CRD created/updated",
		"manifestWork", manifestWorkName,
		"cluster", namespace,
		"result", result,
	)

	mw, err := GetManifestWork(ctx, client, manifestWorkName, namespace)
	if err != nil {
		return err
	}

	isApplied := CheckManifestWorkStatus(mw)
	if !isApplied {
		return fmt.Errorf("CRD ManifestWork %s/%s not applied", mw.Namespace, mw.Name)
	}

	return nil
}
