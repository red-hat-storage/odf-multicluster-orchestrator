package addons

import (
	"context"
	"fmt"
	"os"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type s3SecretHandler struct {
	spokeClient client.Client
	hubClient   client.Client
}

const (
	S3SecretHandlerName       = "s3"
	ObjectBucketClaimKind     = "ObjectBucketClaim"
	S3BucketName              = "BUCKET_NAME"
	S3BucketRegion            = "BUCKET_REGION"
	S3RouteName               = "s3"
	DefaultS3EndpointProtocol = "https"
	// DefaultS3Region is used as a placeholder when region information is not provided by NooBaa
	DefaultS3Region = "noobaa"
)

func getFilterCondition(ownerReferences []metav1.OwnerReference, name string, blueSecretMatchString string) bool {
	for _, ownerReference := range ownerReferences {
		if ownerReference.Kind != ObjectBucketClaimKind {
			continue
		}
		return strings.Contains(name, blueSecretMatchString)
	}
	return false
}

func (s3SecretHandler) getBlueSecretFilter(obj interface{}) bool {
	blueSecretMatchString := os.Getenv("S3_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
	if blueSecretMatchString == "" {
		blueSecretMatchString = utils.BucketGenerateName
	}
	if s, ok := obj.(*corev1.Secret); ok {
		return getFilterCondition(s.OwnerReferences, s.ObjectMeta.Name, blueSecretMatchString)
	} else if c, ok := obj.(*corev1.ConfigMap); ok {
		return getFilterCondition(c.OwnerReferences, c.ObjectMeta.Name, blueSecretMatchString)
	}

	return false
}

func (s3SecretHandler) getGreenSecretFilter(obj interface{}) bool {
	return false
}

func (s s3SecretHandler) syncBlueSecret(name string, namespace string, c *blueSecretTokenExchangeAgentController) error {
	// cofig map and secret name and bucket claim name is same in nooba
	// fetch obc secret
	secret, err := getSecret(c.spokeSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch := s.getBlueSecretFilter(secret)
	if !isMatch {
		// ignore handler which secret filter is not matched
		return nil
	}

	// fetch obc config map
	configMap, err := getConfigMap(c.spokeConfigMapLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the config map %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch = s.getBlueSecretFilter(configMap)
	if !isMatch {
		// ignore handler which configmap filter is not matched
		return nil
	}

	mirrorPeers, err := utils.FetchAllMirrorPeers(context.TODO(), s.hubClient)
	if err != nil {
		return err
	}

	var storageClusterRef *v1alpha1.StorageClusterRef
	for _, mirrorPeer := range mirrorPeers {
		storageClusterRef, err = utils.GetCurrentStorageClusterRef(&mirrorPeer, c.clusterName)
		if err == nil {
			break
		}
	}

	if storageClusterRef == nil {
		klog.Error("failed to find storage cluster ref using spoke cluster name %s from mirrorpeers ", c.clusterName)
		return err
	}

	// fetch s3 endpoint
	route := &routev1.Route{}
	err = s.spokeClient.Get(context.TODO(), types.NamespacedName{Name: S3RouteName, Namespace: storageClusterRef.Namespace}, route)
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
			utils.S3ProfileName:      []byte(fmt.Sprintf("%s-%s-%s", utils.S3ProfilePrefix, c.clusterName, storageClusterRef.Name)),
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

	newSecret, err := generateBlueSecret(&s3Secret, utils.InternalLabel, utils.CreateUniqueSecretName(c.clusterName, storageClusterRef.Namespace, storageClusterRef.Name, utils.S3ProfilePrefix), storageClusterRef.Name, c.clusterName, customData)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
	}

	err = createSecret(c.hubKubeClient, c.recorder, &newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", name, namespace, c.clusterName, err)
	}

	klog.Infof("successfully synced managed cluster s3 bucket secret %q from namespace %v to the hub cluster in namespace %q", name, namespace, c.clusterName)

	return nil
}

func (s3SecretHandler) syncGreenSecret(name string, namespace string, c *greenSecretTokenExchangeAgentController) error {
	return nil
}
