package addons

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	storagev1 "k8s.io/api/storage/v1"

	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	storageClusterName = "test-storagecluster"
	odfNamespace       = "test-namespace"

	odfInfoConfigMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-info",
			Namespace: odfNamespace, // Use a generic namespace
			UID:       types.UID("268e6cdb-54fc-4f10-afab-67b106880be3"),
		},
		Data: map[string]string{
			"test-namespace_test-storagecluster.config.yaml": `
version: 4.17.0-95.stable
deploymentType: internal
clients: []
storageCluster:
  namespacedName:
    namespace: test-namespace
    name: test-storagecluster
  storageProviderEndpoint: ""
  cephClusterFSID: 986532da-8dba-4d35-a8d2-12f037712b39
`,
		},
	}
	odfClientInfoConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: "openshift-operators",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: map[string]string{
			"cluster1_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"cluster2_test-storagecluster": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
		},
	}
	mpItems = []multiclusterv1alpha1.PeerRef{
		{
			ClusterName: "cluster1",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      storageClusterName,
				Namespace: odfNamespace,
			},
		},
		{
			ClusterName: "cluster2",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      storageClusterName,
				Namespace: odfNamespace,
			},
		},
	}
	mirrorpeer1 = &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-proper-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:  "async",
			Items: mpItems,
		},
	}
	// Validating webhooks in place won't allow for this to be created
	mirrorpeer2 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-invalid-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:  "async",
			Items: mpItems,
		},
	}

	rbdStorageClass        = GetTestCephRBDStorageClass()
	rbdVolumeSnapshotClass = GetTestRBDVolumeSnapshotClass()
	rbdVirtStorageClass    = GetTestCephRBDVirtStorageClass()

	cephfsStorageClass        = GetTestCephFSStorageClass()
	cephfsVolumeSnapshotClass = GetTestCephFSVolumeSnapshotClass()

	cephblockpool = GetTestCephBlockPool()
)

func GetTestCephRBDStorageClass() *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-ceph-rbd", storageClusterName),
		},
		Provisioner:          fmt.Sprintf(RBDProvisionerTemplate, odfNamespace),
		ReclaimPolicy:        &[]corev1.PersistentVolumeReclaimPolicy{corev1.PersistentVolumeReclaimDelete}[0],
		AllowVolumeExpansion: &[]bool{true}[0],
		VolumeBindingMode:    &[]storagev1.VolumeBindingMode{storagev1.VolumeBindingImmediate}[0],
		Parameters: map[string]string{
			"clusterID": odfNamespace,
			"pool":      fmt.Sprintf("%s-cephblockpool", storageClusterName),
		},
	}
}

func GetTestCephRBDVirtStorageClass() *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-ceph-rbd-virtualization", storageClusterName),
		},
		Provisioner:          fmt.Sprintf(RBDProvisionerTemplate, odfNamespace),
		ReclaimPolicy:        &[]corev1.PersistentVolumeReclaimPolicy{corev1.PersistentVolumeReclaimDelete}[0],
		AllowVolumeExpansion: &[]bool{true}[0],
		VolumeBindingMode:    &[]storagev1.VolumeBindingMode{storagev1.VolumeBindingImmediate}[0],
		Parameters: map[string]string{
			"clusterID": odfNamespace,
			"pool":      fmt.Sprintf("%s-cephblockpool", storageClusterName),
		},
	}
}

func GetTestCephFSStorageClass() *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-cephfs", storageClusterName),
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Filesystem volumes",
			},
		},
		Provisioner:          fmt.Sprintf(CephFSProvisionerTemplate, odfNamespace),
		ReclaimPolicy:        &[]corev1.PersistentVolumeReclaimPolicy{corev1.PersistentVolumeReclaimDelete}[0],
		AllowVolumeExpansion: &[]bool{true}[0],
		VolumeBindingMode:    &[]storagev1.VolumeBindingMode{storagev1.VolumeBindingImmediate}[0],
		Parameters: map[string]string{
			"clusterID": odfNamespace,
			"fsName":    fmt.Sprintf("%s-cephfilesystem", odfNamespace),
		},
	}
}

func GetTestCephFSVolumeSnapshotClass() *snapv1.VolumeSnapshotClass {
	return &snapv1.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-cephfsplugin-snapclass", storageClusterName),
		},
		Driver:         fmt.Sprintf(CephFSProvisionerTemplate, odfNamespace),
		DeletionPolicy: "Delete",
		Parameters: map[string]string{
			"clusterID": odfNamespace,
		},
	}
}

func GetTestRBDVolumeSnapshotClass() *snapv1.VolumeSnapshotClass {
	return &snapv1.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-rbdplugin-snapclass", storageClusterName),
		},
		Driver:         fmt.Sprintf(RBDProvisionerTemplate, odfNamespace),
		DeletionPolicy: "Delete",
		Parameters: map[string]string{
			"clusterID": odfNamespace,
			"csi.storage.k8s.io/snapshotter-secret-name":      "rook-csi-rbd-provisioner",
			"csi.storage.k8s.io/snapshotter-secret-namespace": odfNamespace,
		},
	}
}

func GetTestCephCluster() *rookv1.CephCluster {
	return &rookv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cephcluster", storageClusterName),
			Namespace: odfNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "ocs.openshift.io/v1",
					Kind:               "StorageCluster",
					Name:               storageClusterName,
					UID:                "5b7a4eeb-6d50-4b35-a570-1cb830c8cb92",
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Status: rookv1.ClusterStatus{
			State: rookv1.ClusterStateCreated,
			Phase: "Ready",
			CephStatus: &rookv1.CephStatus{
				Health: "HEALTH_OK",
				FSID:   "7a877890-d161-4e7a-84d2-8425d556c701",
			},
		},
	}
}

func GetTestCephBlockPool() *rookv1.CephBlockPool {
	return &rookv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cephblockpool", storageClusterName),
			Namespace: odfNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "ocs.openshift.io/v1",
					Kind:               "StorageCluster",
					Name:               storageClusterName,
					UID:                "5b7a4eeb-6d50-4b35-a570-1cb830c8cb92",
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Status: &rookv1.CephBlockPoolStatus{
			Phase:  "Ready",
			PoolID: 1,
		},
	}
}

func TestMirrorPeerReconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mirrorpeer1, &mirrorpeer2, odfClientInfoConfigMap).Build()
	os.Setenv("POD_NAMESPACE", odfNamespace)

	for _, pr := range mirrorpeer1.Spec.Items {
		storageCluster := ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pr.StorageClusterRef.Name,
				Namespace: pr.StorageClusterRef.Namespace,
			},
			Spec: ocsv1.StorageClusterSpec{},
		}
		cephCluster := GetTestCephCluster()

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster, cephCluster, rbdVirtStorageClass, cephblockpool, rbdStorageClass, cephfsStorageClass, &odfInfoConfigMap, rbdVolumeSnapshotClass, cephfsVolumeSnapshotClass).Build()

		r := MirrorPeerReconciler{
			HubClient:            fakeHubClient,
			SpokeClient:          fakeSpokeClient,
			Scheme:               scheme,
			SpokeClusterName:     pr.ClusterName,
			Logger:               utils.GetLogger(utils.GetZapLogger(true)),
			HubOperatorNamespace: "openshift-operators",
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: mirrorpeer1.Name,
			},
		}
		_, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Errorf("MirrorPeerReconciler Reconcile() failed. Error: %s", err)
		}

		// Validate all Items for Reconcile

		var foundCephfsSC storagev1.StorageClass
		cephfsScName := fmt.Sprintf(utils.DefaultCephFSStorageClassTemplate, "test-storagecluster")
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: cephfsScName}, &foundCephfsSC)
		if err != nil {
			t.Errorf("Failed to get CephFS StorageClass %q: %v", cephfsScName, err)
		}

		expectedCephFSStorageID := "53bcee28df765abb22cdba2d3d16b63c"
		if foundCephfsSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] != expectedCephFSStorageID {
			t.Errorf("CephFS StorageClass %q has incorrect StorageID - expected: %q, got: %q",
				foundCephfsSC.Name,
				expectedCephFSStorageID,
				foundCephfsSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)])
		}

		cephRbdScName := fmt.Sprintf(utils.DefaultCephRBDStorageClassTemplate, "test-storagecluster")
		var foundCephRBDSC storagev1.StorageClass
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: cephRbdScName}, &foundCephRBDSC)
		if err != nil {
			t.Errorf("Failed to get CephRBD StorageClass %q: %v", cephRbdScName, err)
		}

		expectedRBDStorageID := "d9b40172f24bf4752da07dfc1ad9c982"
		if foundCephRBDSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] != expectedRBDStorageID {
			t.Errorf("CephRBD StorageClass %q has incorrect StorageID - expected: %q, got: %q",
				cephRbdScName,
				expectedRBDStorageID,
				foundCephRBDSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)])
		}

		var foundCephRBDVirtSC storagev1.StorageClass
		cephRbdVirtScName := fmt.Sprintf(utils.DefaultVirtualizationStorageClassName, "test-storagecluster")
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: cephRbdVirtScName}, &foundCephRBDVirtSC)
		if err != nil {
			t.Errorf("Failed to get CephRBD Virtualization StorageClass %q: %v", cephRbdVirtScName, err)
		}

		if foundCephRBDVirtSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] != expectedRBDStorageID {
			t.Errorf("CephRBD Virtualization StorageClass %q has incorrect StorageID - expected: %q, got: %q",
				cephRbdVirtScName,
				expectedRBDStorageID,
				foundCephRBDVirtSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)])
		}

		var foundCephFSVSC snapv1.VolumeSnapshotClass
		cephfsVscName := fmt.Sprintf(utils.DefaultCephFSVSCNameTemplate, "test-storagecluster")
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: cephfsVscName}, &foundCephFSVSC)
		if err != nil {
			t.Errorf("Failed to get CephFS VolumeSnapshotClass %q: %v", cephfsVscName, err)
		}

		cephFSStorageID := foundCephfsSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)]
		foundCephFSVSCStorageID := foundCephFSVSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)]
		if foundCephFSVSCStorageID != cephFSStorageID {
			t.Errorf("CephFS VolumeSnapshotClass %q has mismatched StorageID with StorageClass %q - expected: %q, got: %q",
				foundCephFSVSC.Name,
				foundCephfsSC.Name,
				cephFSStorageID,
				foundCephFSVSCStorageID)
		}

		var foundRBDVSC snapv1.VolumeSnapshotClass
		rbdVscName := fmt.Sprintf(utils.DefaultRBDVSCNameTemplate, "test-storagecluster")
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: rbdVscName}, &foundRBDVSC)
		if err != nil {
			t.Errorf("Failed to get RBD VolumeSnapshotClass %q: %v", rbdVscName, err)
		}

		rbdStorageID := foundCephRBDSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)]
		foundRBDVSCStorageID := foundRBDVSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)]
		if foundRBDVSCStorageID != rbdStorageID {
			t.Errorf("RBD VolumeSnapshotClass %q has mismatched StorageID with StorageClass %q - expected: %q, got: %q",
				foundRBDVSC.Name,
				foundCephRBDSC.Name,
				rbdStorageID,
				foundRBDVSCStorageID)
		}

		rbdVirtStorageID := foundCephRBDVirtSC.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)]
		if foundRBDVSCStorageID != rbdVirtStorageID {
			t.Errorf("RBD VolumeSnapshotClass %q has mismatched StorageID with Virtualization StorageClass %q - expected: %q, got: %q",
				foundRBDVSC.Name,
				foundCephRBDVirtSC.Name,
				rbdVirtStorageID,
				foundRBDVSCStorageID)
		}
	}

}

func TestDeleteS3(t *testing.T) {
	bucketName := utils.GenerateBucketName(mirrorPeer)
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mirrorpeer1).Build()
	for _, pr := range mirrorpeer1.Spec.Items {
		obc := &v1alpha1.ObjectBucketClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bucketName,
				Namespace: pr.StorageClusterRef.Namespace,
			},
		}
		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obc).Build()
		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
			Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		}
		if err := r.deleteS3(ctx, mirrorpeer1, pr.StorageClusterRef.Namespace); err != nil {
			t.Errorf("failed to delete s3 bucket")
		}
		if err := fakeSpokeClient.Get(ctx, types.NamespacedName{
			Namespace: pr.StorageClusterRef.Namespace,
			Name:      bucketName}, obc); err != nil {
			if !errors.IsNotFound(err) {
				t.Error(err, "S3 bucket did not get deleted")
			}
		} else {
			t.Error("S3 bucket did not get deleted")
		}
	}
}
