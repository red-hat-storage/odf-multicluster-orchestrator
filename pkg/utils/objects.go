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
	"embed"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// EnsureObjects creates or updates a ManifestWork that installs Kubernetes objects
// (CRDs, RBAC, ConfigMaps, etc.) on the specified managed cluster.
// The objects are read from embedded YAML files, converted to JSON, and packaged into a ManifestWork.
func EnsureObjects(
	ctx context.Context,
	client client.Client,
	log *slog.Logger,
	namespace string,
	manifestWorkName string,
	fs embed.FS,
	paths ...string,
) error {
	objectBytes := make([][]byte, len(paths))

	for i, path := range paths {
		yamlBytes, err := fs.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", path, err)
		}
		// Convert YAML to JSON for RawExtension
		jsonBytes, err := yaml.YAMLToJSON(yamlBytes)
		if err != nil {
			return fmt.Errorf("failed to convert YAML to JSON for %q: %w", path, err)
		}
		objectBytes[i] = jsonBytes
	}

	result, err := CreateOrUpdateManifestWork(
		ctx,
		client,
		manifestWorkName,
		namespace,
		nil,
		metav1.OwnerReference{},
		objectBytes...,
	)
	if err != nil {
		return err
	}

	log.Info("ManifestWork for objects created/updated",
		"manifestWork", manifestWorkName,
		"cluster", namespace,
		"result", result,
	)

	mw, err := GetManifestWork(ctx, client, manifestWorkName, namespace)
	if err != nil {
		return err
	}

	isApplied := IsManifestWorkApplied(mw)
	if !isApplied {
		return fmt.Errorf("ManifestWork %s/%s not applied", mw.Namespace, mw.Name)
	}

	return nil
}
