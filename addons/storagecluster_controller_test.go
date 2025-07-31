package addons

import (
	"context"
	"os"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStorageClusterReconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := mgrScheme
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorpeer1, odfClientInfoConfigMap).Build()
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

		var foundSC ocsv1.StorageCluster
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{
			Name:      storageClusterName,
			Namespace: odfNamespace,
		}, &foundSC)
		if err != nil {
			t.Errorf("Failed to get storagecluster in spoke cluster %q: %v", storageClusterName, err)
		}

		if foundSC.Annotations[utils.SCKeyTypeAnnotation] != utils.KeyTypeAES {
			t.Error("StorageCluster not updated with correct keyType")
		}

		delete(foundSC.Annotations, utils.SCKeyTypeAnnotation)

		if err = fakeSpokeClient.Update(ctx, &foundSC); err != nil {
			t.Error("Failed to remove keyType annotation from SC")
		}

		scR := StorageClusterReconciler{
			HubClient:            fakeHubClient,
			SpokeClient:          fakeSpokeClient,
			Scheme:               scheme,
			SpokeClusterName:     pr.ClusterName,
			Logger:               utils.GetLogger(utils.GetZapLogger(true)),
			HubOperatorNamespace: "openshift-operators",
		}

		_, err = scR.Reconcile(ctx, req)
		if err != nil {
			t.Errorf("StorageClusterReconciler Reconcile() failed. Error: %s", err)
		}

		var foundSCNew ocsv1.StorageCluster
		err = fakeSpokeClient.Get(ctx, types.NamespacedName{
			Name:      storageClusterName,
			Namespace: odfNamespace,
		}, &foundSCNew)
		if err != nil {
			t.Errorf("Failed to get storagecluster in spoke cluster %q: %v", storageClusterName, err)
		}

		if foundSCNew.Annotations[utils.SCKeyTypeAnnotation] != "aes" {
			t.Error("StorageCluster not updated with correct keyType")
		}
	}
}
