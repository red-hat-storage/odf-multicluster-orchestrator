package odf

import (
	"context"
	"fmt"
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/backend"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ODFBackend struct{}

func init() {
	backend.Register(&ODFBackend{})
}

func (b *ODFBackend) Name() string { return "odf" }

func (b *ODFBackend) ClusterClaimKey() string { return utils.OdfInfoClusterClaimNamespacedName }

func (b *ODFBackend) EnsureReplicationReady(ctx context.Context, c client.Client, logger *slog.Logger,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error) {

	if mirrorPeer.Spec.Type != multiclusterv1alpha1.Async {
		return true, nil
	}

	if err := createStorageClusterPeer(ctx, c, logger, mirrorPeer, clientInfoMap); err != nil {
		return false, fmt.Errorf("failed to create StorageClusterPeer: %w", err)
	}

	if err := createManifestWorkForClusterPairingConfigMap(ctx, c, logger, mirrorPeer, clientInfoMap); err != nil {
		return false, fmt.Errorf("failed to create ManifestWork for ClusterPairingConfigMap: %w", err)
	}

	done, err := isProviderModePeeringDone(ctx, c, logger, mirrorPeer, clientInfoMap)
	if err != nil {
		return false, fmt.Errorf("failed to check provider mode peering: %w", err)
	}

	return done, nil
}

func (b *ODFBackend) GetS3Profile(ctx context.Context, c client.Client, scheme *runtime.Scheme,
	peerRef multiclusterv1alpha1.PeerRef, mirrorPeer *multiclusterv1alpha1.MirrorPeer,
	clientInfoMap map[string]string, ramenNamespace string, logger *slog.Logger) (*utils.S3Profile, string, error) {

	hasStorageClientRef, err := IsStorageClientType(mirrorPeer, clientInfoMap)
	if err != nil {
		return nil, "", fmt.Errorf("failed to determine peer ref type: %w", err)
	}

	var secretName, namespace string
	if hasStorageClientRef {
		secretName, namespace, err = GetNamespacedNameForClientS3Secret(peerRef, mirrorPeer, clientInfoMap)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get namespace for s3 secret: %w", err)
		}
	} else {
		secretName = utils.GetSecretNameByPeerRef(peerRef, utils.S3ProfilePrefix)
		namespace = peerRef.ClusterName
	}

	var s3Secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &s3Secret); err != nil {
		return nil, "", err
	}

	if _, ok := s3Secret.Annotations[utils.MirrorPeerNameAnnotationKey]; !ok {
		return nil, "", fmt.Errorf("failed to find MirrorPeerName annotation on secret %q", s3Secret.Name)
	}
	if s3Secret.Annotations[utils.MirrorPeerNameAnnotationKey] != mirrorPeer.Name {
		return nil, "", fmt.Errorf("secret %q belongs to MirrorPeer %q, not %q",
			s3Secret.Name, s3Secret.Annotations[utils.MirrorPeerNameAnnotationKey], mirrorPeer.Name)
	}

	data, err := ValidateAndCreateS3Secret(ctx, c, scheme, ramenNamespace, &s3Secret, mirrorPeer, logger)
	if err != nil {
		return nil, "", err
	}

	profile := &utils.S3Profile{
		S3ProfileName:  string(data[utils.S3ProfileName]),
		S3Bucket:       string(data[utils.S3BucketName]),
		S3Region:       string(data[utils.S3Region]),
		S3Endpoint:     string(data[utils.S3Endpoint]),
		AccessKeyID:    string(data[utils.AwsAccessKeyId]),
		SecretAccessKey: string(data[utils.AwsSecretAccessKey]),
		RawData:        data,
	}

	return profile, secretName, nil
}

func (b *ODFBackend) DeleteStorageBackendResources(ctx context.Context, c client.Client,
	mirrorPeer *multiclusterv1alpha1.MirrorPeer, logger *slog.Logger) error {

	logger.Info("Starting deletion of backend resources for MirrorPeer", "MirrorPeer", mirrorPeer.Name)

	for i, peerRef := range mirrorPeer.Spec.Items {
		logger.Info("Checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName)

		peerRefUsed, err := DoesAnotherMirrorPeerPointToPeerRef(ctx, c, mirrorPeer.Spec.Items[i])
		if err != nil {
			logger.Error("Error checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName, "error", err)
			return err
		}

		if !peerRefUsed {
			logger.Info("PeerRef is not used by another MirrorPeer, proceeding to delete secrets", "PeerRef", peerRef.ClusterName)

			secretLabels := []string{}
			if mirrorPeer.Spec.ManageS3 {
				secretLabels = append(secretLabels, string(utils.InternalLabel))
			}

			secretRequirement, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.In, secretLabels)
			if err != nil {
				logger.Error("Cannot create label requirement for deleting secrets", "error", err)
				return err
			}

			secretSelector := labels.NewSelector().Add(*secretRequirement)
			deleteOpt := client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace:     mirrorPeer.Spec.Items[i].ClusterName,
					LabelSelector: secretSelector,
				},
			}

			var secret corev1.Secret
			if err := c.DeleteAllOf(ctx, &secret, &deleteOpt); err != nil {
				logger.Error("Error while deleting secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name, "PeerRef", peerRef.ClusterName, "error", err)
			}

			logger.Info("Secrets successfully deleted", "PeerRef", peerRef.ClusterName)
		} else {
			logger.Info("PeerRef is still used by another MirrorPeer, skipping deletion", "PeerRef", peerRef.ClusterName)
		}
	}

	logger.Info("Completed deletion of backend resources for MirrorPeer", "MirrorPeer", mirrorPeer.Name)
	return nil
}
