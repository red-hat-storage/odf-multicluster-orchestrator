package utils

const (
	S3Endpoint         = "s3CompatibleEndpoint"
	S3BucketName       = "s3Bucket"
	S3Region           = "s3Region"
	AwsAccessKeyId     = "AWS_ACCESS_KEY_ID"
	AwsSecretAccessKey = "AWS_SECRET_ACCESS_KEY"

	RamenHubOperatorConfigName = "ramen-hub-operator-config"

	S3ConfigurationNameAnnotationKey = "multicluster.odf.openshift.io/s3configuration"
	HubOperatorNamespaceKey          = "hub.multicluster.odf.openshift.io/operator-namespace"

	SpokeMirrorPeerFinalizer = "spoke.multicluster.odf.openshift.io"
	TokenExchangeName        = "tokenexchange"
	S3ConfigAddonName        = "s3config"

	// Addon shared constants
	ResourceDistributionFinalizer = "multicluster.odf.openshift.io/resource-distribution-controller"
	AddonDeletionlockName         = "token-exchange-addon-lock"

	TLSProfileName   = "ocs-tls-profile"
	TLSProfileDomain = "odf-multicluster.openshift.io"
	TLSProfileServer = "console"
)
