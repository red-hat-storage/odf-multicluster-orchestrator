//go:build unit
// +build unit

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

import "testing"

func TestGetIbmStorageScaleInfoFromConfigMap(t *testing.T) {
	infoMap := map[string]string{
		"cluster-a_scale-cluster-a": infoEntry(t, "5.2.0"),
	}

	t.Run("found", func(t *testing.T) {
		info, err := getIbmStorageScaleInfoFromConfigMap(infoMap, "cluster-a_scale-cluster-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Version != "5.2.0" {
			t.Fatalf("expected version 5.2.0, got %q", info.Version)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, err := getIbmStorageScaleInfoFromConfigMap(infoMap, "cluster-x_scale-cluster-x")
		if err == nil {
			t.Fatal("expected error for missing key, got nil")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		badMap := map[string]string{"k": "not-json"}
		_, err := getIbmStorageScaleInfoFromConfigMap(badMap, "k")
		if err == nil {
			t.Fatal("expected error for malformed JSON, got nil")
		}
	})
}
