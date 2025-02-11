package utils

import (
	"context"
	"fmt"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultCephFSVSCNameTemplate   = "%s-cephfsplugin-snapclass"
	DefaultRBDVSCNameTemplate      = "%s-rbdplugin-snapclass"
	DefaultCephFSVSCDriverTemplate = "%s.cephfs.csi.ceph.com"
	DefaultRBDVSCDriverTemplate    = "%s.rbd.csi.ceph.com"
)

func GetDefaultVolumeSnapshotClasses(ctx context.Context, c client.Client, storageClusterName string) ([]*snapshotv1.VolumeSnapshotClass, error) {
	var defaultVolumeSnapshotClasses []*snapshotv1.VolumeSnapshotClass

	rbdName := fmt.Sprintf(DefaultRBDVSCNameTemplate, storageClusterName)
	cephFSName := fmt.Sprintf(DefaultCephFSVSCNameTemplate, storageClusterName)

	rbdVsc, err := GetVolumeSnapshotClass(ctx, c, rbdName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting RBD VolumeSnapshotClass: %v", err)
		}
	} else {
		defaultVolumeSnapshotClasses = append(defaultVolumeSnapshotClasses, rbdVsc)
	}

	cephFsVsc, err := GetVolumeSnapshotClass(ctx, c, cephFSName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting CephFS VolumeSnapshotClass: %v", err)
		}
	} else {
		defaultVolumeSnapshotClasses = append(defaultVolumeSnapshotClasses, cephFsVsc)
	}

	return defaultVolumeSnapshotClasses, nil
}

func GetVolumeSnapshotClassType(vsc *snapshotv1.VolumeSnapshotClass, storageClusterNamespace string) (CephType, error) {
	switch vsc.Driver {
	case fmt.Sprintf(DefaultRBDVSCDriverTemplate, storageClusterNamespace):
		return RBDType, nil

	case fmt.Sprintf(DefaultCephFSVSCDriverTemplate, storageClusterNamespace):
		return CephFSType, nil

	default:
		return "", fmt.Errorf("unknown driver type: %s", vsc.Driver)
	}
}

func GetVolumeSnapshotClass(ctx context.Context, c client.Client, volumeSnapshotClassName string) (*snapshotv1.VolumeSnapshotClass, error) {
	volumeSnapshotClass := &snapshotv1.VolumeSnapshotClass{}

	err := c.Get(ctx, client.ObjectKey{
		Name: volumeSnapshotClassName,
	}, volumeSnapshotClass)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to fetch VolumeSnapshotClass %s : %v", volumeSnapshotClassName, err)
	}

	return volumeSnapshotClass, nil
}
