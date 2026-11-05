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
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const sourceManifestResultJSON = `{"apiVersion":"multicluster.odf.openshift.io/v1alpha1","kind":"SourceManifest","metadata":{"name":"sm-a","namespace":"ibm-spectrum-scale"},"spec":{"manifests":[{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsA"}}]}}`

func mirrorPeerWithItems(items ...multiclusterv1alpha1.PeerRef) *multiclusterv1alpha1.MirrorPeer {
	return &multiclusterv1alpha1.MirrorPeer{
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: items,
		},
	}
}

func peerRef(clusterName, storageClusterName string) multiclusterv1alpha1.PeerRef {
	return multiclusterv1alpha1.PeerRef{
		ClusterName: clusterName,
		StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
			Name: storageClusterName,
		},
	}
}

func infoEntry(t *testing.T, version string) string {
	t.Helper()
	return `{"version":"` + version + `","operatorNamespace":"ibm-spectrum-scale","cluster":{"name":"scale-cluster"},"sourceManifests":["sm-a"]}`
}

func TestArePeerVersionsCompatible(t *testing.T) {
	itemA := peerRef("cluster-a", "scale-cluster-a")
	itemB := peerRef("cluster-b", "scale-cluster-b")

	tests := []struct {
		name    string
		peer    *multiclusterv1alpha1.MirrorPeer
		infoMap map[string]string
		wantErr bool
	}{
		{
			name: "compatible versions",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.2.1"),
			},
			wantErr: false,
		},
		{
			name: "incompatible major.minor versions",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.3.0"),
			},
			wantErr: true,
		},
		{
			name: "cluster not discovered yet",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
			},
			wantErr: true,
		},
		{
			name: "empty version for a cluster",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): infoEntry(t, "5.2.0"),
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, ""),
			},
			wantErr: true,
		},
		{
			name:    "wrong number of items",
			peer:    mirrorPeerWithItems(itemA),
			infoMap: map[string]string{},
			wantErr: true,
		},
		{
			name: "malformed info JSON",
			peer: mirrorPeerWithItems(itemA, itemB),
			infoMap: map[string]string{
				utils.GetKey("cluster-a", "scale-cluster-a"): "not-json",
				utils.GetKey("cluster-b", "scale-cluster-b"): infoEntry(t, "5.2.0"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _, _ := newTestVendor(t, tt.infoMap)
			_, err := v.ValidateVersion(context.TODO(), tt.peer)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

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
	mcvKey := types.NamespacedName{Name: getSourceManifestMCVName("sm-a"), Namespace: "cluster-a"}
	info := IbmStorageScaleInfo{OperatorNamespace: "ibm-spectrum-scale", SourceManifests: []string{"sm-a"}}

	t.Run("creates the MCV and requeues while the result is empty", func(t *testing.T) {
		v, cl, mp := newTestVendor(t, nil)

		payloads, ready, err := v.readSourceManifests(context.TODO(), mp, item, info)
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
		v, _, mp := newTestVendor(t, nil, existing)

		payloads, ready, err := v.readSourceManifests(context.TODO(), mp, item, info)
		require.NoError(t, err)
		assert.True(t, ready)
		require.Len(t, payloads["sm-a"], 1)
		assert.Contains(t, string(payloads["sm-a"][0].Raw), "fsA")
	})

	t.Run("not ready when no sourceManifests are listed", func(t *testing.T) {
		emptyInfo := IbmStorageScaleInfo{OperatorNamespace: "ibm-spectrum-scale"}
		v, _, mp := newTestVendor(t, nil)

		_, ready, err := v.readSourceManifests(context.TODO(), mp, item, emptyInfo)
		require.NoError(t, err)
		assert.False(t, ready)
	})

	t.Run("errors on an invalid MCV result", func(t *testing.T) {
		existing := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{Name: mcvKey.Name, Namespace: mcvKey.Namespace},
			Status:     viewv1beta1.ViewStatus{Result: runtime.RawExtension{Raw: []byte("not-json")}},
		}
		v, _, mp := newTestVendor(t, nil, existing)

		_, _, err := v.readSourceManifests(context.TODO(), mp, item, info)
		assert.Error(t, err)
	})
}

func TestSyncAppliedManifests(t *testing.T) {
	peer := peerRef("cluster-b", "scale-cluster-b")
	mwKey := types.NamespacedName{Name: getAppliedManifestWorkName("sm-a"), Namespace: "cluster-b"}
	payload := []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsA"}}`)}}
	info := IbmStorageScaleInfo{OperatorNamespace: "ibm-spectrum-scale"}

	t.Run("creates a ManifestWork carrying the AppliedManifest on the peer", func(t *testing.T) {
		v, cl, mp := newTestVendor(t, nil)

		// Pre-create ManifestWork with status to simulate it being applied
		controller := true
		existingMW := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mwKey.Name,
				Namespace: mwKey.Namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "multicluster.odf.openshift.io/v1alpha1",
					Kind:       "MirrorPeer",
					Name:       "mirror-ibm",
					UID:        mp.UID,
					Controller: &controller,
				}},
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{
					{Type: workv1.WorkApplied, Status: metav1.ConditionTrue},
					{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue},
				},
			},
		}
		require.NoError(t, cl.Create(context.TODO(), existingMW))

		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, info, map[string][]runtime.RawExtension{"sm-a": payload}))

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
		v, cl, mp := newTestVendor(t, nil)

		// Pre-create ManifestWork with status
		controller := true
		existingMW := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mwKey.Name,
				Namespace: mwKey.Namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "multicluster.odf.openshift.io/v1alpha1",
					Kind:       "MirrorPeer",
					Name:       "mirror-ibm",
					UID:        mp.UID,
					Controller: &controller,
				}},
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{
					{Type: workv1.WorkApplied, Status: metav1.ConditionTrue},
					{Type: workv1.WorkAvailable, Status: metav1.ConditionTrue},
				},
			},
		}
		require.NoError(t, cl.Create(context.TODO(), existingMW))
		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, info, map[string][]runtime.RawExtension{"sm-a": payload}))

		updated := []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"scale.spectrum.ibm.com/v1beta1","kind":"FileSystem","metadata":{"name":"fsB"}}`)}}
		require.NoError(t, v.syncAppliedManifests(context.TODO(), mp, peer, info, map[string][]runtime.RawExtension{"sm-a": updated}))

		mw := &workv1.ManifestWork{}
		require.NoError(t, cl.Get(context.TODO(), mwKey, mw))
		require.Len(t, mw.Spec.Workload.Manifests, 1)
		assert.Contains(t, string(mw.Spec.Workload.Manifests[0].Raw), "fsB")
	})
}

func appliedManifestMCVResult(t *testing.T, phase multiclusterv1alpha1.AppliedManifestPhase, generation, observedGeneration int64) *viewv1beta1.ManagedClusterView {
	t.Helper()
	am := multiclusterv1alpha1.AppliedManifest{}
	am.Name = "sm-a"
	am.Namespace = "ibm-spectrum-scale"
	am.Generation = generation
	am.Status.Phase = phase
	am.Status.ObservedGeneration = observedGeneration
	raw, err := json.Marshal(am)
	require.NoError(t, err)
	return &viewv1beta1.ManagedClusterView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getAppliedManifestStatusMCVName("sm-a"),
			Namespace: "cluster-b",
		},
		Status: viewv1beta1.ViewStatus{Result: runtime.RawExtension{Raw: raw}},
	}
}

func TestCheckAppliedManifestsStatus(t *testing.T) {
	peer := peerRef("cluster-b", "scale-cluster-b")
	info := IbmStorageScaleInfo{OperatorNamespace: "ibm-spectrum-scale"}
	resourcesByManifestName := map[string][]runtime.RawExtension{"sm-a": nil}

	tests := []struct {
		name               string
		phase              multiclusterv1alpha1.AppliedManifestPhase
		generation         int64
		observedGeneration int64
		wantReady          bool
		wantErr            bool
	}{
		{
			name:               "Applied and fresh — ready",
			phase:              multiclusterv1alpha1.AppliedManifestPhaseApplied,
			generation:         1,
			observedGeneration: 1,
			wantReady:          true,
		},
		{
			name:               "Failed — error",
			phase:              multiclusterv1alpha1.AppliedManifestPhaseFailed,
			generation:         1,
			observedGeneration: 1,
			wantErr:            true,
		},
		{
			name:               "Degraded — error",
			phase:              multiclusterv1alpha1.AppliedManifestPhaseDegraded,
			generation:         1,
			observedGeneration: 1,
			wantErr:            true,
		},
		{
			name:               "Pending — not ready",
			phase:              multiclusterv1alpha1.AppliedManifestPhasePending,
			generation:         1,
			observedGeneration: 1,
			wantReady:          false,
		},
		{
			name:               "Applying — not ready",
			phase:              multiclusterv1alpha1.AppliedManifestPhaseApplying,
			generation:         1,
			observedGeneration: 1,
			wantReady:          false,
		},
		{
			name:               "stale observedGeneration — not ready",
			phase:              multiclusterv1alpha1.AppliedManifestPhaseApplied,
			generation:         2,
			observedGeneration: 1,
			wantReady:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcv := appliedManifestMCVResult(t, tt.phase, tt.generation, tt.observedGeneration)
			v, _, mp := newTestVendor(t, nil, mcv)

			ready, err := v.checkAppliedManifestsStatus(context.TODO(), mp, peer, info, resourcesByManifestName)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantReady, ready)
		})
	}
}
