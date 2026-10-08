package utils

const (
	S3ProfilePrefix    = "s3profile"
	S3Endpoint         = "s3CompatibleEndpoint"
	S3BucketName       = "s3Bucket"
	S3ProfileName      = "s3ProfileName"
	S3Region           = "s3Region"
	AwsAccessKeyId     = "AWS_ACCESS_KEY_ID"
	AwsSecretAccessKey = "AWS_SECRET_ACCESS_KEY"

	RamenHubOperatorConfigName = "ramen-hub-operator-config"

	MirrorPeerNameAnnotationKey      = "multicluster.odf.openshift.io/mirrorpeer"
	S3ConfigurationNameAnnotationKey = "multicluster.odf.openshift.io/s3configuration"
	HubOperatorNamespaceKey          = "hub.multicluster.odf.openshift.io/operator-namespace"

	SpokeMirrorPeerFinalizer = "spoke.multicluster.odf.openshift.io"
	TokenExchangeName        = "tokenexchange"
	S3ConfigAddonName        = "s3config"

	// Addon shared constants
	RBDProvisionerTemplate        = "%s.rbd.csi.ceph.com"
	CephFSProvisionerTemplate     = "%s.cephfs.csi.ceph.com"
	ResourceDistributionFinalizer = "multicluster.odf.openshift.io/resource-distribution-controller"
	OBCTypeAnnotationKey          = "multicluster.odf.openshift.io/obc-type"
	OBCNameAnnotationKey          = "multicluster.odf.openshift.io/obc-name"
	AddonDeletionlockName         = "token-exchange-addon-lock"

	TLSProfileName   = "ocs-tls-profile"
	TLSProfileDomain = "odf-multicluster.openshift.io"
	TLSProfileServer = "console"
)

type OBCTypeValue string

var (
	OBCTypeClient  OBCTypeValue = "client"
	OBCTypeCluster OBCTypeValue = "cluster"
)
