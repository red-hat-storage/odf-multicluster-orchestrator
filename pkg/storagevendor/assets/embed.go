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

package assets

import "embed"

// CRDFiles embeds the SourceManifest and AppliedManifest CRD definitions.
// These CRDs are used by storage vendors that need to sync custom resources
// between hub and managed clusters for DR configuration.
//
//go:embed multicluster.odf.openshift.io_sourcemanifests.yaml
//go:embed multicluster.odf.openshift.io_appliedmanifests.yaml
var CRDFiles embed.FS

// RBACFiles embeds RBAC resources needed for klusterlet to manage the CRDs.
// The ClusterRole grants permissions to create, update, and watch
// SourceManifest and AppliedManifest resources.
//
//go:embed klusterlet_role.yaml
//go:embed klusterlet_rolebinding.yaml
var RBACFiles embed.FS
