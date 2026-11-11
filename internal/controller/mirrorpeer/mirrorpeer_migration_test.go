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

package mirrorpeer

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	rmn "github.com/ramendr/ramen/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func migrationScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	assert.NoError(t, multiclusterv1alpha1.AddToScheme(scheme))
	assert.NoError(t, corev1.AddToScheme(scheme))
	assert.NoError(t, addonapiv1alpha1.AddToScheme(scheme))
	assert.NoError(t, workv1.AddToScheme(scheme))
	assert.NoError(t, rmn.AddToScheme(scheme))
	assert.NoError(t, obv1alpha1.AddToScheme(scheme))
	return scheme
}

func legacyMirrorPeer(storageVendor multiclusterv1alpha1.VendorType) *multiclusterv1alpha1.MirrorPeer {
	return &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "mirrorpeer-sample"},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			StorageVendor: storageVendor,
			ManageS3:      true,
			Type:          multiclusterv1alpha1.Async,
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "local-cluster",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "ocs-storagecluster",
						Namespace: "openshift-storage",
					},
				},
				{
					ClusterName: "dr1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "ocs-storagecluster",
						Namespace: "openshift-storage",
					},
				},
			},
		},
	}
}

func legacyDRCluster(name, s3ProfileName string) *rmn.DRCluster {
	return &rmn.DRCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "MirrorPeer", Name: "mirrorpeer-sample", APIVersion: "multicluster.odf.openshift.io/v1alpha1", UID: "uid-1"},
			},
		},
		Spec: rmn.DRClusterSpec{S3ProfileName: s3ProfileName},
	}
}

// clientInfoConfigMap builds the hub-side `odf-client-info` ConfigMap the ODF storage vendor reads.
func clientInfoConfigMap(t *testing.T, clusters ...string) *corev1.ConfigMap {
	data := map[string]string{}
	for _, cluster := range clusters {
		info := odf.ClientInfo{
			ClusterID: cluster,
			Name:      "ocs-storagecluster",
			ProviderInfo: odf.ProviderInfo{
				Version:                    "5.1.0",
				DeploymentType:             "external",
				ProviderManagedClusterName: cluster,
				NamespacedName: types.NamespacedName{
					Name:      "ocs-storagecluster",
					Namespace: "openshift-storage",
				},
			},
		}
		raw, err := json.Marshal(info)
		assert.NoError(t, err)
		data[utils.GetKey(cluster, "ocs-storagecluster")] = string(raw)
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odf.ClientInfoConfigMapName,
			Namespace: "openshift-operators",
		},
		Data: data,
	}
}

func fakeMirrorPeerReconciler(t *testing.T, objs ...runtime.Object) MirrorPeerReconciler {
	scheme := migrationScheme(t)
	return MirrorPeerReconciler{
		Client:           fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build(),
		Scheme:           scheme,
		Logger:           slog.New(slog.NewTextHandler(os.Stdout, nil)),
		CurrentNamespace: "openshift-operators",
	}
}

// Regression guard: these cases fail against the old storageVendor=="" check.
func TestNeedsS3Migration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manageS3 bool
		vendor   multiclusterv1alpha1.VendorType
		want     bool
	}{
		{
			// The case the old storageVendor=="" check missed.
			name:     "legacy peer whose vendor was defaulted by the CRD",
			manageS3: true,
			vendor:   multiclusterv1alpha1.ODF,
			want:     true,
		},
		{
			name:     "legacy peer read before defaulting",
			manageS3: true,
			vendor:   "",
			want:     true,
		},
		{
			name:     "already migrated peer",
			manageS3: false,
			vendor:   multiclusterv1alpha1.ODF,
			want:     false,
		},
		{
			name:     "peer created after S3Configuration existed",
			manageS3: false,
			vendor:   "",
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mirrorPeer := legacyMirrorPeer(tc.vendor)
			mirrorPeer.Spec.ManageS3 = tc.manageS3
			assert.Equal(t, tc.want, needsS3Migration(mirrorPeer))
		})
	}
}

// The migration must still run for a legacy MirrorPeer whose storageVendor was defaulted by the CRD.
func TestCreateS3Configuration_RunsWhenStorageVendorIsDefaulted(t *testing.T) {
	mirrorPeer := legacyMirrorPeer(multiclusterv1alpha1.ODF) // as read back after CRD defaulting
	reconciler := fakeMirrorPeerReconciler(t,
		mirrorPeer,
		clientInfoConfigMap(t, "local-cluster", "dr1"),
		legacyDRCluster("local-cluster", "s3profile-local-cluster-ocs-storagecluster"),
		legacyDRCluster("dr1", "s3profile-dr1-ocs-storagecluster"),
	)

	err := reconciler.createS3Configuration(context.TODO(), reconciler.Logger, mirrorPeer)
	assert.NoError(t, err)

	s3Configs := &multiclusterv1alpha1.S3ConfigurationList{}
	assert.NoError(t, reconciler.List(context.TODO(), s3Configs))

	// Async: one S3Configuration per distinct s3ProfileName.
	assert.Len(t, s3Configs.Items, 2, "legacy MirrorPeer should be migrated to S3Configurations")

	for _, s3Config := range s3Configs.Items {
		assert.NotNil(t, s3Config.Spec.InternalS3, "migrated config should be internal S3")
		assert.Len(t, s3Config.Spec.ManagedClusters, 1)
	}

	// The MirrorPeer must stop owning the DRClusters; the S3Configuration does now.
	for _, name := range []string{"local-cluster", "dr1"} {
		drCluster := &rmn.DRCluster{}
		assert.NoError(t, reconciler.Get(context.TODO(), client.ObjectKey{Name: name}, drCluster))
		for _, owner := range drCluster.GetOwnerReferences() {
			assert.NotEqual(t, "MirrorPeer", owner.Kind,
				"MirrorPeer ownerRef should be removed from DRCluster %s", name)
		}
	}
}

// Re-running the migration must not duplicate or error, since it is driven
// from a reconcile loop.
func TestCreateS3Configuration_IsIdempotent(t *testing.T) {
	mirrorPeer := legacyMirrorPeer(multiclusterv1alpha1.ODF)
	reconciler := fakeMirrorPeerReconciler(t,
		mirrorPeer,
		clientInfoConfigMap(t, "local-cluster", "dr1"),
		legacyDRCluster("local-cluster", "s3profile-local-cluster-ocs-storagecluster"),
		legacyDRCluster("dr1", "s3profile-dr1-ocs-storagecluster"),
	)

	assert.NoError(t, reconciler.createS3Configuration(context.TODO(), reconciler.Logger, mirrorPeer))
	assert.NoError(t, reconciler.createS3Configuration(context.TODO(), reconciler.Logger, mirrorPeer))

	s3Configs := &multiclusterv1alpha1.S3ConfigurationList{}
	assert.NoError(t, reconciler.List(context.TODO(), s3Configs))
	assert.Len(t, s3Configs.Items, 2, "re-running the migration should not duplicate S3Configurations")
}
