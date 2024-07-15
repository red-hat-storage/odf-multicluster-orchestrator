package addons

import (
	"context"
	"fmt"
	"testing"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	storagev1 "k8s.io/api/storage/v1"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

	secretData = map[string][]byte{
		"token":   []byte("eyJmc2lkIjoiMzU2NjZlNGMtZTljMC00ZmE3LWE3MWEtMmIwNTJiZjUxOTFhIiwiY2xpZW50X2lkIjoicmJkLW1pcnJvci1wZWVyIiwia2V5IjoiQVFDZVkwNWlYUmtsTVJBQU95b3I3ZTZPL3MrcTlzRnZWcVpVaHc9PSIsIm1vbl9ob3N0IjoiMTcyLjMxLjE2NS4yMjg6Njc4OSwxNzIuMzEuMTkxLjE0MDo2Nzg5LDE3Mi4zMS44LjQ0OjY3ODkiLCJuYW1lc3BhY2UiOiJvcGVuc2hpZnQtc3RvcmFnZSJ9"),
		"cluster": []byte("ocs-storagecluster-cephcluster"),
	}
	// Create secret cluster-peer-token
	clusterPeerToken = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-peer-token-test-storagecluster-cephcluster",
			Namespace: "test-namespace",
		},
		Data: secretData,
	}

	exchangedSecret1 = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "5feeb6c9ab835d7365ccec211f0756ede3a54f3",
			Namespace: "test-namespace",
		},
		Data: secretData,
	}

	exchangedSecret2 = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "c4bca1dacc9733952cc5a705761792867c4d3fb",
			Namespace: "test-namespace",
		},
		Data: secretData,
	}

	rbdStorageClass = &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rbd-storageclass",
		},
		Provisioner: fmt.Sprintf(RBDProvisionerTemplate, "test-namespace"),
	}

	cephfsStorageClass = &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cephfs-storageclass",
		},
		Provisioner: fmt.Sprintf(CephFSProvisionerTemplate, "test-namespace"),
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

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster, &rcm, &clusterPeerToken, &exchangedSecret1, &exchangedSecret2, rbdStorageClass, cephfsStorageClass).Build()

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

		// Validate all Items for Reconcile

		var foundRcm corev1.ConfigMap
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: rcm.Name, Namespace: rcm.Namespace}, &foundRcm)
		if err != nil {
			t.Errorf("Failed to get rook config map %s Error: %s", rcm.Name, err)
		}

		if foundRcm.Data[RookCSIEnableKey] != "true" {
			t.Errorf("Values for %s in %s is not set correctly", RookCSIEnableKey, foundRcm.Name)
		}

		var foundSc ocsv1.StorageCluster
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{Name: storageCluster.Name, Namespace: storageCluster.Namespace}, &foundSc)
		if err != nil {
			t.Errorf("Failed to get storagecluster %s Error: %s", storageCluster.Name, err)
		}

		if !foundSc.Spec.Mirroring.Enabled {
			t.Errorf("Mirroring not enabled; Error: %s", err)
		}

	}

}

func TestDisableMirroring(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1).Build()
	for _, pr := range mirrorpeer1.Spec.Items {
		storageCluster := ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pr.StorageClusterRef.Name,
				Namespace: pr.StorageClusterRef.Namespace,
			},
			Spec: ocsv1.StorageClusterSpec{
				Mirroring: ocsv1.MirroringSpec{
					Enabled: true,
				},
			},
		}

		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&storageCluster).Build()
		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
			Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		}
		if err := r.disableMirroring(ctx, pr.StorageClusterRef.Name, pr.StorageClusterRef.Namespace, &mirrorpeer1); err != nil {
			t.Error("failed to disable mirroring", err)
		}
		var sc ocsv1.StorageCluster
		if err := fakeSpokeClient.Get(ctx, types.NamespacedName{
			Name:      pr.StorageClusterRef.Name,
			Namespace: pr.StorageClusterRef.Namespace,
		}, &sc); err != nil {
			t.Error("failed to get storage cluster", err)
		}

		if sc.Spec.Mirroring.Enabled {
			t.Error("failed to disable mirroring")
		}
	}
}

func TestDeleteGreenSecret(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	secretData := map[string][]byte{
		utils.StorageClusterNameKey: []byte("test-storagecluster"),
		utils.NamespaceKey:          []byte("test-namespace"),
		utils.SecretOriginKey:       []byte(""),
		utils.SecretDataKey:         []byte(""),
	}
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1).Build()
	for _, pr := range mirrorpeer1.Spec.Items {
		fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
		oppositePeerRefs := getOppositePeerRefs(&mirrorpeer1, pr.ClusterName)
		for _, oppPeer := range oppositePeerRefs {
			greenSecret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.GetSecretNameByPeerRef(oppPeer),
					Namespace: pr.StorageClusterRef.Namespace,
				},
				Data: secretData,
			}
			if err := fakeSpokeClient.Create(ctx, &greenSecret); err != nil {
				t.Error("failed to create an exchanged secret", greenSecret.Name)
			}
		}

		r := MirrorPeerReconciler{
			HubClient:        fakeHubClient,
			SpokeClient:      fakeSpokeClient,
			Scheme:           scheme,
			SpokeClusterName: pr.ClusterName,
			Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		}

		if err := r.deleteGreenSecret(ctx, pr.ClusterName, pr.StorageClusterRef.Namespace, &mirrorpeer1); err != nil {
			t.Errorf("failed to delete green secret from namespace %s, err: %v", pr.StorageClusterRef.Namespace, err)
		}
		for _, oppPeer := range oppositePeerRefs {
			var greenSecret corev1.Secret
			if err := r.SpokeClient.Get(ctx, types.NamespacedName{
				Name:      utils.GetSecretNameByPeerRef(oppPeer),
				Namespace: pr.StorageClusterRef.Namespace,
			}, &greenSecret); err != nil {
				if !errors.IsNotFound(err) {
					t.Error(err, "Green Secret did not get deleted")
				}
			} else {
				t.Error("Green secret did not get deleted")
			}
		}
	}
}

func TestDeleteS3(t *testing.T) {
	bucketName := "odrbucket-b1b922184baf"
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1).Build()
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
