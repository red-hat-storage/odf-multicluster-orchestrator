package addons

import (
	"context"
	"fmt"
	"os"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	storagev1 "k8s.io/api/storage/v1"

	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
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
storageSystemName: test-storagecluster-storagesystem
`,
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
	mirrorpeer1 = multiclusterv1alpha1.MirrorPeer{
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

func getOppositePeerRefs(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) []multiclusterv1alpha1.PeerRef {
	peerRefs := make([]multiclusterv1alpha1.PeerRef, 0)
	for _, v := range mp.Spec.Items {
		if v.ClusterName != spokeClusterName {
			peerRefs = append(peerRefs, v)
		}
	}
	return peerRefs
}

func TestMirrorPeerReconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1, &mirrorpeer2).Build()
	os.Setenv("POD_NAMESPACE", odfNamespace)

	oppositePeerRefsArray := make([][]multiclusterv1alpha1.PeerRef, 0)
	// Quick iteration to get peer refs
	for _, pr := range mirrorpeer1.Spec.Items {
		peerRefs := getOppositePeerRefs(&mirrorpeer1, pr.ClusterName)
		oppositePeerRefsArray = append(oppositePeerRefsArray, peerRefs)
	}

	for i, pr := range mirrorpeer1.Spec.Items {
		secretNames := make([]string, 0)
		for _, ref := range oppositePeerRefsArray[i] {
			secretNames = append(secretNames, utils.GetSecretNameByPeerRef(ref))
		}
		storageCluster := ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pr.StorageClusterRef.Name,
				Namespace: pr.StorageClusterRef.Namespace,
			},
			Spec: ocsv1.StorageClusterSpec{
				Mirroring: &ocsv1.MirroringSpec{
					Enabled:         false,
					PeerSecretNames: secretNames,
				},
			},
		}

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster, &odfInfoConfigMap).Build()

		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
			Logger:           utils.GetLogger(utils.GetZapLogger(true)),
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
	}

}
