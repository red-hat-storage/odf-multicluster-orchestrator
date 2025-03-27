package utils

import (
	"context"
	"fmt"

	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CephType string

const (
	RBDType                               CephType = "rbd"
	CephFSType                            CephType = "cephfs"
	RBDProvisionerTemplate                         = "%s.rbd.csi.ceph.com"
	CephFSProvisionerTemplate                      = "%s.cephfs.csi.ceph.com"
	DefaultVirtualizationStorageClassName          = "%s-ceph-rbd-virtualization"
	DefaultCephRBDStorageClassTemplate             = "%s-ceph-rbd"
	DefaultCephFSStorageClassTemplate              = "%s-cephfs"
	DefaultSubvolumeGroup                          = "csi"
	DefaultRadosNamespace                          = ""
)

func GetStorageClass(ctx context.Context, c client.Client, storageClassName string) (*storagev1.StorageClass, error) {
	storageClass := &storagev1.StorageClass{}

	err := c.Get(ctx, client.ObjectKey{
		Name: storageClassName,
	}, storageClass)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to fetch StorageClass %s : %v", storageClassName, err)
	}

	return storageClass, nil
}

func GetDefaultStorageClasses(ctx context.Context, c client.Client, storageClusterName string) ([]*storagev1.StorageClass, error) {
	var defaultStorageClasses []*storagev1.StorageClass

	rbdName := fmt.Sprintf(DefaultCephRBDStorageClassTemplate, storageClusterName)
	cephFSName := fmt.Sprintf(DefaultCephFSStorageClassTemplate, storageClusterName)
	virtualizationName := fmt.Sprintf(DefaultVirtualizationStorageClassName, storageClusterName)

	rbdSC, err := GetStorageClass(ctx, c, rbdName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting RBD StorageClass: %v", err)
		}
	} else {
		defaultStorageClasses = append(defaultStorageClasses, rbdSC)
	}

	cephFSSC, err := GetStorageClass(ctx, c, cephFSName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting CephFS StorageClass: %v", err)
		}
	} else {
		defaultStorageClasses = append(defaultStorageClasses, cephFSSC)
	}

	virtualizationSC, err := GetStorageClass(ctx, c, virtualizationName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting Virtualization StorageClass: %v", err)
		}
	} else {
		defaultStorageClasses = append(defaultStorageClasses, virtualizationSC)
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

func GetStorageClassType(sc *storagev1.StorageClass, storageClusterNamespace string) (CephType, error) {
	switch sc.Provisioner {
	case fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespace):
		return RBDType, nil

	case fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespace):
		return CephFSType, nil

	default:
		return "", fmt.Errorf("unknown provisioner type: %s", sc.Provisioner)
	}
}

func CalculateStorageId(ctx context.Context, c client.Client, sc *storagev1.StorageClass, storageClusterNamespacedName types.NamespacedName) (string, error) {
	cephCluster, err := FetchCephCluster(ctx, c, storageClusterNamespacedName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch ceph cluster: %v", err)
	}

	cephClusterFSID := cephCluster.Status.CephStatus.FSID
	var storageId string

	switch sc.Provisioner {
	case fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespacedName.Namespace):
		storageId = CalculateMD5Hash([2]string{cephClusterFSID, DefaultRadosNamespace})

	case fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespacedName.Namespace):
		storageId = CalculateMD5Hash([2]string{cephClusterFSID, DefaultSubvolumeGroup})

	default:
		return "", fmt.Errorf("unknown provisioner type: %s", sc.Provisioner)
	}

	return storageId, nil
}
