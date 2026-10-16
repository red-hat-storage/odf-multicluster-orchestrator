package utils

const (
	S3Endpoint         = "s3CompatibleEndpoint"
	S3BucketName       = "s3Bucket"
	S3Region           = "s3Region"
	AwsAccessKeyId     = "AWS_ACCESS_KEY_ID"
	AwsSecretAccessKey = "AWS_SECRET_ACCESS_KEY"

	RamenHubOperatorConfigName = "ramen-hub-operator-config"
	RamenAPIVersion            = "ramendr.openshift.io/v1alpha1"
	DRPCKind                   = "DRPlacementControl"
	PlacementKind              = "Placement"

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

	AddonVersionAnnotationKey = "multicluster.odf.openshift.io/version"

	// SourceManifestCRDPath is the path to the SourceManifest CRD YAML file in the embedded FS.
	SourceManifestCRDPath = "multicluster.odf.openshift.io_sourcemanifests.yaml"

	// AppliedManifestCRDPath is the path to the AppliedManifest CRD YAML file in the embedded FS.
	AppliedManifestCRDPath = "multicluster.odf.openshift.io_appliedmanifests.yaml"

	// ManifestCRDsMWName is the name of the ManifestWork that installs
	// the SourceManifest and AppliedManifest CRDs to managed clusters.
	ManifestCRDsMWName = "df-mco-manifest-crds"

	// KlusterletRolePath is the path to the ClusterRole for klusterlet to manage manifest CRDs in the embedded FS.
	KlusterletRolePath = "klusterlet_role.yaml"

	// KlusterletRoleBindingPath is the path to the ClusterRoleBinding for klusterlet in the embedded FS.
	KlusterletRoleBindingPath = "klusterlet_rolebinding.yaml"

	// ManifestRBACMWName is the name of the ManifestWork that installs RBAC for manifest CRDs.
	ManifestRBACMWName = "df-mco-manifest-rbac"
)
