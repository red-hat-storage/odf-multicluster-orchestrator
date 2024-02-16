package addons

import (
	"context"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *S3SecretReconciler) syncBlueSecretForS3(ctx context.Context, name string, namespace string) error {
	// cofig map and secret name and bucket claim name is same in nooba
	// fetch obc secret
	var secret corev1.Secret
	err := r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	// fetch obc config map
	var configMap corev1.ConfigMap
	err = r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &configMap)
	if err != nil {
		return fmt.Errorf("failed to get the config map %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	mirrorPeers, err := utils.FetchAllMirrorPeers(context.TODO(), r.HubClient)
	if err != nil {
		return err
	}

	var storageClusterRef *v1alpha1.StorageClusterRef
	for _, mirrorPeer := range mirrorPeers {
		storageClusterRef, err = utils.GetCurrentStorageClusterRef(&mirrorPeer, r.SpokeClusterName)
		if err == nil {
			break
		}
	}

	if storageClusterRef == nil {
		klog.Error("failed to find storage cluster ref using spoke cluster name %s from mirrorpeers ", r.SpokeClusterName)
		return err
	}

	// fetch s3 endpoint
	route := &routev1.Route{}
	err = r.SpokeClient.Get(ctx, types.NamespacedName{Name: S3RouteName, Namespace: storageClusterRef.Namespace}, route)
	if err != nil {
		return fmt.Errorf("failed to get the s3 endpoint in namespace %q in managed cluster. Error %v", namespace, err)
	}

	s3Region := configMap.Data[S3BucketRegion]
	if s3Region == "" {
		s3Region = DefaultS3Region
	}
	// s3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: storageClusterRef.Namespace,
		},
		Type: utils.SecretLabelTypeKey,
		Data: map[string][]byte{
			utils.S3ProfileName:      []byte(fmt.Sprintf("%s-%s-%s", utils.S3ProfilePrefix, r.SpokeClusterName, storageClusterRef.Name)),
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

	newSecret, err := generateBlueSecret(s3Secret, utils.InternalLabel, utils.CreateUniqueSecretName(r.SpokeClusterName, storageClusterRef.Namespace, storageClusterRef.Name, utils.S3ProfilePrefix), storageClusterRef.Name, r.SpokeClusterName, customData)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, r.SpokeClusterName, err)
	}

	err = r.HubClient.Create(ctx, newSecret, &client.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", name, namespace, r.SpokeClusterName, err)
	}

	klog.Infof("successfully synced managed cluster s3 bucket secret %q from namespace %v to the hub cluster in namespace %q", name, namespace, r.SpokeClusterName)

	return nil
}
