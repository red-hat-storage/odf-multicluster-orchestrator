package addons

import (
	"context"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/volume-replication-operator/api/v1alpha1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	mirrorpeer = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-namespace",
					},
				},
			},
			SchedulingInterval:    "10m",
			ReplicationSecretName: "my-super-secret-credentials",
			Mode:                  "journal",
		},
	}
)

func TestMirrorPeerReconcilerReconcile(t *testing.T) {
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer).Build()
	oppositePeerRefsArray := make([][]multiclusterv1alpha1.PeerRef, 0)
	// Quick iteration to get peer refs
	for _, pr := range mirrorpeer.Spec.Items {
		peerRefs := getOppositePeerRefs(&mirrorpeer, pr.ClusterName)
		oppositePeerRefsArray = append(oppositePeerRefsArray, peerRefs)
	}

	for i, pr := range mirrorpeer.Spec.Items {
		secretNames := make([]string, 0)
		for _, ref := range oppositePeerRefsArray[i] {
			secretNames = append(secretNames, common.CreateUniqueSecretName(ref.ClusterName, ref.StorageClusterRef.Namespace, ref.StorageClusterRef.Name))
		}
		storageCluster := ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pr.StorageClusterRef.Name,
				Namespace: pr.StorageClusterRef.Namespace,
			},
			Spec: ocsv1.StorageClusterSpec{
				Mirroring: ocsv1.MirroringSpec{
					Enabled:         false,
					PeerSecretNames: secretNames,
				},
			},
		}

		rcm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      RookConfigMapName,
				Namespace: pr.StorageClusterRef.Namespace,
			},
		}

		// Need to initialize this map otherwise it panics during reconcile
		rcm.Data = make(map[string]string)
		rcm.Data[RookCSIEnableKey] = "false"
		rcm.Data[RookVolumeRepKey] = "false"

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster, &rcm).Build()
		ctx := context.TODO()

		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: "mirrorpeer",
			},
		}

		_, err := r.Reconcile(ctx, req)
		if err != nil {
			t.Errorf("MirrorPeerReconciler Reconcile() failed. Error: %s", err)
		}

		// Validate all Items for Reconcile

		var foundRcm corev1.ConfigMap
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: rcm.Name, Namespace: rcm.Namespace}, &foundRcm)
		if err != nil {
			t.Errorf("Failed to get rook config map %s Error: %s", rcm.Name, err)
		}

		if foundRcm.Data[RookCSIEnableKey] != "true" || foundRcm.Data[RookVolumeRepKey] != "true" {
			t.Errorf("Error: %s", err)
		}

		var foundSc ocsv1.StorageCluster
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: storageCluster.Name, Namespace: storageCluster.Namespace}, &foundSc)
		if err != nil {
			t.Errorf("Failed to get storagecluster %s Error: %s", storageCluster.Name, err)
		}

		if !foundSc.Spec.Mirroring.Enabled {
			t.Errorf("Mirroring not enabled; Error: %s", err)
		}

		var foundVrc replicationv1alpha1.VolumeReplicationClass
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: "odf-rbd-volumereplicationclass"}, &foundVrc)
		if err != nil {
			t.Errorf("Failed to get volumereplicationclass odf-rbd-volumereplicationclass Error: %s", err)
		}

		if foundVrc.Spec.Parameters[SchedulingIntervalKey] != mirrorpeer.Spec.SchedulingInterval ||
			foundVrc.Spec.Parameters[ReplicationSecretNameKey] != mirrorpeer.Spec.ReplicationSecretName ||
			foundVrc.Spec.Parameters[MirroringModeKey] != string(mirrorpeer.Spec.Mode) {
			t.Errorf("VolumeReplicatonClass not created or updated properly; Error: %s", err)
		}

	}

}
