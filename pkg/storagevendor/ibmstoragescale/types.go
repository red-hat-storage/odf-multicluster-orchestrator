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

// IbmStorageScaleInfo represents the replication information from FA ConfigMap
type IbmStorageScaleInfo struct {
	Version           string   `yaml:"version" json:"version"`
	OperatorNamespace string   `yaml:"operator-namespace" json:"operatorNamespace"`
	Cluster           Cluster  `yaml:"cluster" json:"cluster"`
	SourceManifests   []string `yaml:"sourceManifests" json:"sourceManifests"`
}

type Cluster struct {
	Name string `yaml:"name" json:"name"`
}
