package addons

import (
	"context"
	"fmt"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"testing"

	replicationv1alpha1 "github.com/csi-addons/volume-replication-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type ReconcilerStorageCluster struct {
	r  MirrorPeerReconciler
	sc ocsv1.StorageCluster
}

var (
	mpItems = []multiclusterv1alpha1.PeerRef{
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
	}
	mirrorpeer1 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-proper-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:                "async",
			Items:               mpItems,
			SchedulingIntervals: []string{"10m", "5m", "30m", "1h", "5m"},
		},
	}
	// Validating webhooks in place won't allow for this to be created
	mirrorpeer2 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-with-invalid-scheduling-intervals",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:                "async",
			Items:               mpItems,
			SchedulingIntervals: []string{"1p", "2o", "3h", "4hh", "5md"},
		},
	}
)

func TestMirrorPeerReconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1, &mirrorpeer2).Build()
	oppositePeerRefsArray := make([][]multiclusterv1alpha1.PeerRef, 0)
	// Quick iteration to get peer refs
	for _, pr := range mirrorpeer1.Spec.Items {
		peerRefs := getOppositePeerRefs(&mirrorpeer1, pr.ClusterName)
		oppositePeerRefsArray = append(oppositePeerRefsArray, peerRefs)
	}

	reconcilers := make([]ReconcilerStorageCluster, 0)
	for i, pr := range mirrorpeer1.Spec.Items {
		secretNames := make([]string, 0)
		for _, ref := range oppositePeerRefsArray[i] {
			secretNames = append(secretNames, utils.CreateUniqueSecretName(ref.ClusterName, ref.StorageClusterRef.Namespace, ref.StorageClusterRef.Name))
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

		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
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

		var foundRcm corev1.ConfigMap
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: rcm.Name, Namespace: rcm.Namespace}, &foundRcm)
		if err != nil {
			t.Errorf("Failed to get rook config map %s Error: %s", rcm.Name, err)
		}

		if foundRcm.Data[RookCSIEnableKey] != "true" || foundRcm.Data[RookVolumeRepKey] != "true" {
			t.Errorf("Values for %s and %s in %s are not set correctly", RookCSIEnableKey, RookVolumeRepKey, foundRcm.Name)
		}

		var foundSc ocsv1.StorageCluster
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: storageCluster.Name, Namespace: storageCluster.Namespace}, &foundSc)
		if err != nil {
			t.Errorf("Failed to get storagecluster %s Error: %s", storageCluster.Name, err)
		}

		if !foundSc.Spec.Mirroring.Enabled {
			t.Errorf("Mirroring not enabled; Error: %s", err)
		}

		reconcilers = append(reconcilers, ReconcilerStorageCluster{
			r:  r,
			sc: foundSc,
		})
	}

	for _, reconciler := range reconcilers {
		// For each schedulingInterval in MirrorPeer, check if the corresponding VolumeReplicationClass has been created.
		for _, interval := range mirrorpeer1.Spec.SchedulingIntervals {
			vrcName := fmt.Sprintf(RBDVolumeReplicationClassNameTemplate, utils.FnvHash(interval))
			found := &replicationv1alpha1.VolumeReplicationClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: vrcName,
				},
			}
			err := reconciler.r.SpokeClient.Get(ctx, types.NamespacedName{
				Name: found.Name,
			}, found)

			if err != nil {
				t.Errorf("Failed to get VolumeReplicationClass %s Error: %s", found.Name, err)
			}

			if found.Spec.Provisioner != fmt.Sprintf(RBDProvisionerTemplate, reconciler.sc.Namespace) &&
				found.Spec.Parameters[ReplicationSecretNameKey] != RBDReplicationSecretName ||
				found.Spec.Parameters[MirroringModeKey] != DefaultMirroringMode {
				t.Errorf("VolumeReplicatonClass not created or updated properly; Please check Provisioner, Parameters and MirroringMode")
				break
			}
		}
	}

}
