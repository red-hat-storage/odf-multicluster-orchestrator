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

import (
	"context"
	"encoding/json"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
)

func TestPushAppliedManifests(t *testing.T) {
	peer := peerRef("cluster-b", "scale-cluster-b")
	peerKey := utils.GetKey("cluster-b", "scale-cluster-b")
	mwKey := types.NamespacedName{Name: getAppliedManifestWorkName("sm-a"), Namespace: "cluster-b"}
	payload := []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsA"}}`)}}

	t.Run("creates a ManifestWork carrying the AppliedManifest on the peer", func(t *testing.T) {
		v, cl, mp := newTestVendor(t, map[string]string{peerKey: infoEntry(t, "5.2.0")})

		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, map[string][]runtime.RawExtension{"sm-a": payload}))

		mw := &workv1.ManifestWork{}
		require.NoError(t, cl.Get(context.TODO(), mwKey, mw))
		require.Len(t, mw.OwnerReferences, 1)
		assert.Equal(t, "MirrorPeer", mw.OwnerReferences[0].Kind)
		assert.Equal(t, "mirror-ibm", mw.OwnerReferences[0].Name)
		require.NotNil(t, mw.OwnerReferences[0].Controller)
		assert.True(t, *mw.OwnerReferences[0].Controller)

		require.Len(t, mw.Spec.Workload.Manifests, 1)
		am := &multiclusterv1alpha1.AppliedManifest{}
		require.NoError(t, json.Unmarshal(mw.Spec.Workload.Manifests[0].Raw, am))
		assert.Equal(t, "AppliedManifest", am.Kind)
		assert.Equal(t, "sm-a", am.Name)
		assert.Equal(t, "ibm-spectrum-scale", am.Namespace)
		require.Len(t, am.Spec.Manifests, 1)
		assert.Contains(t, string(am.Spec.Manifests[0].Raw), "fsA")
	})

	t.Run("updates the ManifestWork when the payload changes", func(t *testing.T) {
		v, cl, mp := newTestVendor(t, map[string]string{peerKey: infoEntry(t, "5.2.0")})
		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, map[string][]runtime.RawExtension{"sm-a": payload}))

		updated := []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsB"}}`)}}
		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, map[string][]runtime.RawExtension{"sm-a": updated}))

		mw := &workv1.ManifestWork{}
		require.NoError(t, cl.Get(context.TODO(), mwKey, mw))
		require.Len(t, mw.Spec.Workload.Manifests, 1)
		assert.Contains(t, string(mw.Spec.Workload.Manifests[0].Raw), "fsB")
	})

	t.Run("errors when the peer is not discovered", func(t *testing.T) {
		v, _, mp := newTestVendor(t, map[string]string{})

		err := v.syncAppliedManifests(context.TODO(), mp, peer, map[string][]runtime.RawExtension{"sm-a": payload})
		assert.Error(t, err)
	})
}
