package utils

import (
	"context"
	"fmt"
	"log/slog"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterType string

const (
	CONVERGED                     ClusterType = "Converged"
	EXTERNAL                      ClusterType = "External"
	UNKNOWN                       ClusterType = "Unknown"
	CephClusterReplicationIdLabel             = "replicationid.multicluster.openshift.io"
	CephClusterNameTemplate                   = "%s-cephcluster"
	KeyTypeAnnotation                         = "multicluster.odf.openshift.io/rbdMirroringKeyType"
	SCKeyTypeAnnotation                       = "ocs.openshift.io/rbdMirroringKeyType"
	KeyTypeAES256k                            = "aes256k"
	KeyTypeAES                                = "aes"
)

func FetchCephCluster(ctx context.Context, c client.Client, storageClusterNamespacedName types.NamespacedName) (*rookv1.CephCluster, error) {
	var cephCluster rookv1.CephCluster
	cephClusterName := fmt.Sprintf(CephClusterNameTemplate, storageClusterNamespacedName.Name)
	cephClusterNamespace := storageClusterNamespacedName.Namespace
	err := c.Get(ctx, types.NamespacedName{Namespace: cephClusterNamespace, Name: cephClusterName}, &cephCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cephcluster with name %s in namespace %s", cephClusterName, cephClusterNamespace)
	}
	return &cephCluster, nil
}

func GetStorageClusterFromCurrentNamespace(ctx context.Context, c client.Client, namespace string) (*ocsv1.StorageCluster, error) {
	storageClusterList := &ocsv1.StorageClusterList{}
	listOptions := []client.ListOption{
		client.InNamespace(namespace),
	}

	// List all StorageClusters in the specified namespace
	if err := c.List(ctx, storageClusterList, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list StorageClusters in namespace %s: %w", namespace, err)
	}

	// Ensure only one StorageCluster exists
	if len(storageClusterList.Items) == 0 {
		return nil, fmt.Errorf("no StorageCluster found in namespace %s", namespace)
	}

	if len(storageClusterList.Items) > 1 {
		return nil, fmt.Errorf("multiple StorageClusters found in namespace %s", namespace)
	}

	// Return the single StorageCluster
	return &storageClusterList.Items[0], nil
}

func SetKeyTypeOnStorageCluster(ctx context.Context, logger *slog.Logger, spokeClient, hubClient client.Client, sc *ocsv1.StorageCluster) error {
	allMirrorPeerKeyType := KeyTypeAES256k
	mpList := &multiclusterv1alpha1.MirrorPeerList{}
	if err := hubClient.List(ctx, mpList); err != nil {
		logger.Error("Failed to list mirrorpeers on hub", "error", err)
		return err
	}
	for _, mp := range mpList.Items {
		if mp.Annotations[KeyTypeAnnotation] != KeyTypeAES256k {
			allMirrorPeerKeyType = KeyTypeAES
			break
		}
	}

	// Set annotation on SC based on the KeyType of all the mirrorpeers on the hub for a storagecluster
	if AddAnnotation(sc, SCKeyTypeAnnotation, allMirrorPeerKeyType) {
		logger.Info("Adding/Updating keyType annotation in storagecluster", "sc.Name", sc.Name, "sc.Namespace", sc.Namespace, "KeyType", allMirrorPeerKeyType)
		if err := spokeClient.Update(ctx, sc); err != nil {
			return err
		}
	}

	return nil
}
