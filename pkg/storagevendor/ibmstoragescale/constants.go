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

const (
	// IbmStorageScaleConfigMapName is the name of the ConfigMap on the hub that stores IBM Storage Scale cluster information.
	IbmStorageScaleConfigMapName = "ibm-storage-scale-info"
)

const (
	// sourceManifestResource is the MCV scope resource for reading a SourceManifest.
	sourceManifestResource = "sourcemanifest"

	// appliedManifestResource is the MCV scope resource for reading an AppliedManifest.
	appliedManifestResource = "appliedmanifest"

	// appliedManifestKind is the Kind set on the AppliedManifest carried in a ManifestWork.
	appliedManifestKind = "AppliedManifest"
)

const (
	// resourceNamePrefix is the shared prefix for every MCV and ManifestWork this package creates.
	resourceNamePrefix = "ibmscale-"

	// sourceManifestMCVNameTemplate names the MCV that reads a SourceManifest from the source cluster.
	sourceManifestMCVNameTemplate = resourceNamePrefix + "sm-%s"

	// appliedManifestWorkNameTemplate names the ManifestWork that delivers an AppliedManifest to the peer.
	appliedManifestWorkNameTemplate = resourceNamePrefix + "am-%s"

	// appliedManifestStatusMCVNameTemplate names the MCV that reads an AppliedManifest's status from the peer.
	appliedManifestStatusMCVNameTemplate = resourceNamePrefix + "am-%s-status"
)
