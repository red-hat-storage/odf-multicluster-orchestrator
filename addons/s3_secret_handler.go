package addons

import (
	"context"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ObjectBucketClaimKind     = "ObjectBucketClaim"
	S3BucketName              = "BUCKET_NAME"
	S3BucketRegion            = "BUCKET_REGION"
	S3RouteName               = "s3"
	DefaultS3EndpointProtocol = "https"
	// DefaultS3Region is used as a placeholder when region information is not provided by NooBaa
	DefaultS3Region = "noobaa"
)

func (r *S3SecretReconciler) syncBlueSecretForS3(ctx context.Context, name string, namespace string, mirrorPeerName string, obcType string) error {
	// fetch obc secret
	var secret corev1.Secret
	err := r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if err != nil {
		return fmt.Errorf("failed to retrieve the secret %q in namespace %q in managed cluster: %v", name, namespace, err)
	}

	// fetch obc config map
	var configMap corev1.ConfigMap
	err = r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &configMap)
	if err != nil {
		return fmt.Errorf("failed to retrieve the config map %q in namespace %q in managed cluster: %v", name, namespace, err)
	}

	mirrorPeer, err := utils.FetchMirrorPeerByName(ctx, r.HubClient, mirrorPeerName)
	if err != nil {
		r.Logger.Error("Failed to fetch  mirrorpeer", "MirrorPeer", mirrorPeerName)
		return err
	}

	var storagePeerRef *v1alpha1.PeerRef
	var storageClusterRef *v1alpha1.StorageClusterRef
	var s3ProfileName string

	if obcType == string(CLUSTER) {
		storagePeerRef, err = utils.GetPeerRefForSpokeCluster(mirrorPeer, r.SpokeClusterName)
		if err != nil {
			return fmt.Errorf("failed to find storage cluster ref using spoke cluster name %s from mirrorpeers: %v", r.SpokeClusterName, err)
		}
		if storagePeerRef == nil {
			r.Logger.Info("OBC references MirrorPeer which is not related to this spoke cluster.", "OBC Name", name, "OBC Namespace", namespace, "MirrorPeer", mirrorPeerName)
			return nil
		}
		r.Logger.Info("Found peerRef for spoke cluseter", "Spoke", r.SpokeClusterName, "PeerRef", storagePeerRef)
	} else {
		storagePeerRefList, err := utils.GetPeerRefForProviderCluster(ctx, r.SpokeClient, r.HubClient, mirrorPeer)
		if err != nil {
			return fmt.Errorf("failed to find client peerRef for current provider cluster %s. %w", r.SpokeClusterName, err)
		}
		if len(storagePeerRefList) > 1 {
			s3ProfileName = fmt.Sprintf("%s-%s", utils.S3ProfilePrefix, r.SpokeClusterName)
		}
		if len(storagePeerRefList) < 1 {
			r.Logger.Info("OBC references MirrorPeer which is not related to this provider.", "OBC Name", name, "OBC Namespace", namespace, "MirrorPeer", mirrorPeerName)
			return nil
		}
		storagePeerRef = &storagePeerRefList[0]
		r.Logger.Info("Found client peerRef for provider", "Provider", r.SpokeClusterName, "PeerRef", storagePeerRef)
	}

	storageClusterRef = &storagePeerRef.StorageClusterRef
	if s3ProfileName == "" {
		s3ProfileName = fmt.Sprintf("%s-%s-%s", utils.S3ProfilePrefix, storagePeerRef.ClusterName, storagePeerRef.StorageClusterRef.Name)
	}

	// fetch s3 endpoint
	route := &routev1.Route{}
	err = r.SpokeClient.Get(ctx, types.NamespacedName{Name: S3RouteName, Namespace: namespace}, route)
	if err != nil {
		return fmt.Errorf("failed to retrieve the S3 endpoint in namespace %q in managed cluster: %v", namespace, err)
	}

	s3Region := configMap.Data[S3BucketRegion]
	if s3Region == "" {
		s3Region = DefaultS3Region
	}

	// s3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: utils.SecretLabelTypeKey,
		Data: map[string][]byte{
			utils.S3ProfileName:      []byte(s3ProfileName),
			utils.S3BucketName:       []byte(configMap.Data[S3BucketName]),
			utils.S3Region:           []byte(s3Region),
			utils.S3Endpoint:         []byte(fmt.Sprintf("%s://%s", DefaultS3EndpointProtocol, route.Spec.Host)),
			utils.AwsSecretAccessKey: []byte(secret.Data[utils.AwsSecretAccessKey]),
			utils.AwsAccessKeyId:     []byte(secret.Data[utils.AwsAccessKeyId]),
		},
	}

	customData := map[string][]byte{
		utils.SecretOriginKey: []byte(utils.OriginMap["S3Origin"]),
	}

	var secretName string
	if obcType == string(CLUSTER) {
		secretName = utils.CreateUniqueSecretName(r.SpokeClusterName, storageClusterRef.Namespace, storageClusterRef.Name, utils.S3ProfilePrefix)
	} else {
		pr1 := mirrorPeer.Spec.Items[0]
		pr2 := mirrorPeer.Spec.Items[1]
		secretName = utils.CreateUniqueSecretNameForClient(r.SpokeClusterName, utils.GetKey(pr1.ClusterName, pr1.StorageClusterRef.Name), utils.GetKey(pr2.ClusterName, pr2.StorageClusterRef.Name))
	}

	annotations := map[string]string{
		OBCNameAnnotationKey:              name,
		utils.MirrorPeerNameAnnotationKey: mirrorPeerName,
		OBCTypeAnnotationKey:              obcType,
	}

	newSecret, err := generateBlueSecret(s3Secret, utils.InternalLabel, secretName, storageClusterRef.Name, r.SpokeClusterName, customData, annotations)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q in namespace %q for the hub cluster in namespace %q: %v", secret.Name, secret.Namespace, r.SpokeClusterName, err)
	}
	err = r.HubClient.Create(ctx, newSecret, &client.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Log that the secret already exists and attempt to update it
			r.Logger.Info("Secret already exists on hub cluster, attempting to update", "secret", newSecret.Name, "namespace", newSecret.Namespace)
			err = r.HubClient.Update(ctx, newSecret, &client.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update existing secret %q in namespace %q on hub cluster: %w", newSecret.Name, newSecret.Namespace, err)
			}
			r.Logger.Info("Successfully updated existing secret on hub cluster", "secret", newSecret.Name, "namespace", newSecret.Namespace)
			return nil
		}
		// If it's an error other than "already exists", log and return
		return fmt.Errorf("failed to create secret %q in namespace %q on hub cluster: %w", newSecret.Name, newSecret.Namespace, err)
	}

	r.Logger.Info("Successfully synced managed cluster s3 bucket secret to the hub cluster", "secret", name, "namespace", namespace, "hubNamespace", r.SpokeClusterName)
	return nil
}
