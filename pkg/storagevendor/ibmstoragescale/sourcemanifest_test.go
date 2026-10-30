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
	"log/slog"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/testutil"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const sourceManifestResultJSON = `{"apiVersion":"multicluster.odf.openshift.io/v1alpha1","kind":"SourceManifest","metadata":{"name":"sm-a","namespace":"ibm-spectrum-scale"},"spec":{"manifests":[{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsA"}}]}}`

func newTestVendor(t *testing.T, infoMap map[string]string, objs ...client.Object) (*IbmStorageScaleVendor, client.Client, *multiclusterv1alpha1.MirrorPeer) {
	t.Helper()
	mp := &multiclusterv1alpha1.MirrorPeer{ObjectMeta: metav1.ObjectMeta{Name: "mirror-ibm", UID: "mp-uid"}}
	cl := fake.NewClientBuilder().WithScheme(testutil.TestScheme).WithObjects(objs...).Build()
	return &IbmStorageScaleVendor{
		client:           cl,
		scheme:           testutil.TestScheme,
		logger:           slog.Default(),
		currentNamespace: "openshift-operators",
		infoMap:          infoMap,
	}, cl, mp
}

func TestReadSourceManifests(t *testing.T) {
	item := peerRef("cluster-a", "scale-cluster-a")
	key := utils.GetKey("cluster-a", "scale-cluster-a")
	mcvKey := types.NamespacedName{Name: getSourceManifestMCVName("sm-a"), Namespace: "cluster-a"}

	t.Run("creates the MCV and requeues while the result is empty", func(t *testing.T) {
		v, cl, mp := newTestVendor(t, map[string]string{key: infoEntry(t, "5.2.0")})

		payloads, ready, err := v.readSourceManifests(context.TODO(), mp, item)
		require.NoError(t, err)
		assert.False(t, ready)
		assert.Nil(t, payloads)

		mcv := &viewv1beta1.ManagedClusterView{}
		require.NoError(t, cl.Get(context.TODO(), mcvKey, mcv))
		assert.Equal(t, "sourcemanifest", mcv.Spec.Scope.Resource)
		assert.Equal(t, multiclusterv1alpha1.GroupVersion.Group, mcv.Spec.Scope.Group)
		assert.Equal(t, "sm-a", mcv.Spec.Scope.Name)
		assert.Equal(t, "ibm-spectrum-scale", mcv.Spec.Scope.Namespace)
		require.Len(t, mcv.OwnerReferences, 1)
		assert.Equal(t, "MirrorPeer", mcv.OwnerReferences[0].Kind)
		assert.Equal(t, "mirror-ibm", mcv.OwnerReferences[0].Name)
	})

	t.Run("returns the payload once the MCV result is populated", func(t *testing.T) {
		existing := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{Name: mcvKey.Name, Namespace: mcvKey.Namespace},
			Status: viewv1beta1.ViewStatus{
				Result: runtime.RawExtension{Raw: []byte(sourceManifestResultJSON)},
			},
		}
		v, _, mp := newTestVendor(t, map[string]string{key: infoEntry(t, "5.2.0")}, existing)

		payloads, ready, err := v.readSourceManifests(context.TODO(), mp, item)
		require.NoError(t, err)
		assert.True(t, ready)
		require.Len(t, payloads["sm-a"], 1)
		assert.Contains(t, string(payloads["sm-a"][0].Raw), "fsA")
	})

	t.Run("not ready when no sourceManifests are listed", func(t *testing.T) {
		info := `{"version":"5.2.0","operatorNamespace":"ibm-spectrum-scale","cluster":{"name":"scale-cluster-a"},"sourceManifests":[]}`
		v, _, mp := newTestVendor(t, map[string]string{key: info})

		_, ready, err := v.readSourceManifests(context.TODO(), mp, item)
		require.NoError(t, err)
		assert.False(t, ready)
	})

	t.Run("errors when the cluster is not discovered", func(t *testing.T) {
		v, _, mp := newTestVendor(t, map[string]string{})

		_, _, err := v.readSourceManifests(context.TODO(), mp, item)
		assert.Error(t, err)
	})

	t.Run("errors on an invalid MCV result", func(t *testing.T) {
		existing := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{Name: mcvKey.Name, Namespace: mcvKey.Namespace},
			Status:     viewv1beta1.ViewStatus{Result: runtime.RawExtension{Raw: []byte("not-json")}},
		}
		v, _, mp := newTestVendor(t, map[string]string{key: infoEntry(t, "5.2.0")}, existing)

		_, _, err := v.readSourceManifests(context.TODO(), mp, item)
		assert.Error(t, err)
	})
}
