package addons

type OBCTypeValue string

const (
	RBDProvisionerTemplate        = "%s.rbd.csi.ceph.com"
	MaintenanceModeFinalizer      = "maintenance.multicluster.odf.openshift.io"
	RBDMirrorDeploymentNamePrefix = "rook-ceph-rbd-mirror"
	RookCSIEnableKey              = "CSI_ENABLE_OMAP_GENERATOR"
	RookConfigMapName             = "rook-ceph-operator-config"
	RamenLabelTemplate            = "ramendr.openshift.io/%s"
	StorageIDKey                  = "storageid"
	CephFSProvisionerTemplate     = "%s.cephfs.csi.ceph.com"
	SpokeMirrorPeerFinalizer      = "spoke.multicluster.odf.openshift.io"
	OBCTypeAnnotationKey          = "multicluster.odf.openshift.io/obc-type"
	OBCNameAnnotationKey          = "multicluster.odf.openshift.io/obc-name"
)

var (
	CLIENT  OBCTypeValue = "client"
	CLUSTER OBCTypeValue = "cluster"
)
