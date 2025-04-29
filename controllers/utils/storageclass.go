package utils

import (
	"context"
	"fmt"
	"strings"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CephType string

const (
	RBDType                                   CephType = "rbd"
	CephFSType                                CephType = "cephfs"
	RBDProvisionerTemplate                             = "%s.rbd.csi.ceph.com"
	CephFSProvisionerTemplate                          = "%s.cephfs.csi.ceph.com"
	DefaultVirtualizationStorageClassName              = "%s-ceph-rbd-virtualization"
	DefaultCephRBDStorageClassTemplate                 = "%s-ceph-rbd"
	DefaultRadosNamespaceStorageClassTemplate          = "%s-ceph-rbd-rados-namespace-"
	DefaultCephFSStorageClassTemplate                  = "%s-cephfs"
	DefaultSubvolumeGroup                              = "csi"
	DefaultRadosNamespace                              = ""
)

func ListStorageClasses(ctx context.Context, c client.Client) ([]storagev1.StorageClass, error) {
	var storageClasses storagev1.StorageClassList

	err := c.List(ctx, &storageClasses)

	if err != nil {
		return nil, fmt.Errorf("failed to list storageclasses: %w", err)
	}

	return storageClasses.Items, nil
}

func GetDefaultStorageClasses(ctx context.Context, c client.Client, storageClusterName string) ([]storagev1.StorageClass, error) {
	var defaultStorageClasses []storagev1.StorageClass

	rbdName := fmt.Sprintf(DefaultCephRBDStorageClassTemplate, storageClusterName)
	rbdRadosNamespace := fmt.Sprintf(DefaultRadosNamespaceStorageClassTemplate, storageClusterName)
	cephFSName := fmt.Sprintf(DefaultCephFSStorageClassTemplate, storageClusterName)
	virtualizationName := fmt.Sprintf(DefaultVirtualizationStorageClassName, storageClusterName)

	sc, err := ListStorageClasses(ctx, c)
	if err != nil {
		return nil, err
	}
	for i := range sc {
		if sc[i].Name == rbdName || sc[i].Name == cephFSName ||
			sc[i].Name == virtualizationName || strings.HasPrefix(sc[i].Name, rbdRadosNamespace) {
			defaultStorageClasses = append(defaultStorageClasses, sc[i])
		}
	}

	return defaultStorageClasses, nil
}

func GetStorageIdsForDefaultStorageClasses(ctx context.Context, c client.Client, scNamespacedName types.NamespacedName, spokeClusterName string) (map[CephType]string, error) {
	storageClasses, err := GetDefaultStorageClasses(ctx, c, scNamespacedName.Name)

	if err != nil {
		return nil, fmt.Errorf("error occured while fetching the default storageclasses. %v", err)
	}

	storageIdMap := make(map[CephType]string)
	for _, sc := range storageClasses {
		storageClassType, err := GetStorageClassType(sc, scNamespacedName.Namespace)
		if err != nil {
			return nil, err
		}
		storageId, err := CalculateStorageId(ctx, c, sc, scNamespacedName)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate the storageId of storageclass %s. error: %v", sc.Name, err)
		}
		storageIdMap[storageClassType] = storageId
	}

	return storageIdMap, err
}

func GetStorageClassType(sc storagev1.StorageClass, storageClusterNamespace string) (CephType, error) {
	switch sc.Provisioner {
	case fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespace):
		return RBDType, nil

	case fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespace):
		return CephFSType, nil

	default:
		return "", fmt.Errorf("unknown provisioner type: %s", sc.Provisioner)
	}
}

func CalculateStorageId(ctx context.Context, c client.Client, sc storagev1.StorageClass, storageClusterNamespacedName types.NamespacedName) (string, error) {
	cephCluster, err := FetchCephCluster(ctx, c, storageClusterNamespacedName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch ceph cluster: %v", err)
	}

	cephClusterFSID := cephCluster.Status.CephStatus.FSID
	if cephClusterFSID == "" {
		return "", fmt.Errorf("failed to calculate StorageID. Ceph FSID is empty")
	}

	var storageId string
	switch sc.Provisioner {
	case fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespacedName.Namespace):
		radosNamespaceName := DefaultRadosNamespace
		rbdRadosNamespace := fmt.Sprintf(DefaultRadosNamespaceStorageClassTemplate, storageClusterNamespacedName.Name)
		if strings.HasPrefix(sc.Name, rbdRadosNamespace) {
			radosNamespaceName = strings.TrimPrefix(sc.Name, rbdRadosNamespace)
		}
		storageId = CalculateMD5Hash([2]string{cephClusterFSID, radosNamespaceName})

	case fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespacedName.Namespace):
		storageId = CalculateMD5Hash([2]string{cephClusterFSID, DefaultSubvolumeGroup})

	default:
		return "", fmt.Errorf("unknown provisioner type: %s", sc.Provisioner)
	}

	return storageId, nil
}
