package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MCGSecretHandler struct {
	spokeKubeConfig      *rest.Config
	spokeSecretLister    corev1lister.SecretLister
	spokeConfigMapLister corev1lister.ConfigMapLister
}

const (
	MCGSecretHandlerName            = "mcg"
	MCGDefaultBlueSecretMatchString = "odrbucket"
	MCGObjectBucketClaimKind        = "ObjectBucketClaim"
	S3BucketName                    = "BUCKET_HOST"
	S3RouteName                     = "s3"
	DefaultS3EndpointProtocol       = "https"
)

func getFilterCondition(ownerReferences []metav1.OwnerReference, name string, blueSecretMatchString string) bool {
	for _, ownerReference := range ownerReferences {
		if ownerReference.Kind != MCGObjectBucketClaimKind {
			continue
		}
		return strings.Contains(name, blueSecretMatchString)
	}
	return false
}

func (MCGSecretHandler) GetObjectFilter(obj interface{}) bool {
	var isMatching bool = false
	blueSecretMatchString := os.Getenv("MCG_S3_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
	if blueSecretMatchString == "" {
		blueSecretMatchString = MCGDefaultBlueSecretMatchString
	}

	if s, ok := obj.(*corev1.Secret); ok {
		isMatching = getFilterCondition(s.OwnerReferences, s.ObjectMeta.Name, blueSecretMatchString)
	}

	if s, ok := obj.(*corev1.ConfigMap); ok {
		isMatching = getFilterCondition(s.OwnerReferences, s.ObjectMeta.Name, blueSecretMatchString)
	}

	return isMatching
}

func (m MCGSecretHandler) GenerateBlueSecret(name string, namespace string, clusterName string, agentNamespace string) (nsecret *corev1.Secret, err error) {
	// cofig map and secret name and bucket claim name is same in nooba
	// fetch obc secret
	secret, err := GetSecret(m.spokeSecretLister, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch := m.GetObjectFilter(secret)
	if !isMatch {
		// ignore handler which secret filter is not matched
		return nil, nil
	}

	// fetch obc config map
	configMap, err := GetConfigMap(m.spokeConfigMapLister, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get the config map %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch = m.GetObjectFilter(configMap)
	if !isMatch {
		// ignore handler which configmap filter is not matched
		return nil, nil
	}

	// fetch s3 endpoint
	scheme := runtime.NewScheme()
	if err := routev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add routev1 scheme to runtime scheme: %v", err)
	}
	cl, err := client.New(m.spokeKubeConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	route := &routev1.Route{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: S3RouteName, Namespace: agentNamespace}, route)
	if err != nil {
		return nil, fmt.Errorf("failed to get the s3 endpoint in namespace %q in managed cluster. Error %v", namespace, err)
	}

	// s3 secret
	mcgSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: common.SecretLabelTypeKey,
		Data: map[string][]byte{
			common.S3ProfileName:      []byte(fmt.Sprintf("%s-%s-%s", common.S3SecretPrefix, clusterName, namespace)),
			common.S3BucketName:       []byte(configMap.Data[S3BucketName]),
			common.AwsSecretAccessKey: []byte(secret.Data[common.AwsSecretAccessKey]),
			common.AwsAccessKeyId:     []byte(secret.Data[common.AwsAccessKeyId]),
			common.S3Endpoint:         []byte(fmt.Sprintf("%s://%s", DefaultS3EndpointProtocol, route.Spec.Host)),
		},
	}

	newSecret, err := CreateBlueSecret(&mcgSecret, common.IgnoreLabel, name, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, clusterName, err)
	}

	return &newSecret, nil
}
