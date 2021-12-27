package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
)

type RookSecretHandler struct {
	spokeKubeConfig      *rest.Config
	spokeSecretLister    corev1lister.SecretLister
	spokeConfigMapLister corev1lister.ConfigMapLister
}

const (
	RookSecretHandlerName            = "rook"
	RookType                         = "kubernetes.io/rook"
	RookDefaultBlueSecretMatchString = "cluster-peer-token"
)

func (RookSecretHandler) GetObjectFilter(obj interface{}) bool {
	blueSecretMatchString := os.Getenv("TOKEN_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
	if blueSecretMatchString == "" {
		blueSecretMatchString = RookDefaultBlueSecretMatchString
	}
	if s, ok := obj.(*corev1.Secret); ok {
		if s.Type == RookType && strings.Contains(s.ObjectMeta.Name, blueSecretMatchString) {
			return true
		}
	}

	return false
}

func (r RookSecretHandler) GenerateBlueSecret(name string, namespace string, clusterName string, agentNamespace string) (nsecret *corev1.Secret, err error) {
	// fetch secret
	secret, err := GetSecret(r.spokeSecretLister, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch := r.GetObjectFilter(secret)
	if !isMatch {
		// ignore handler which secret filter is not matched
		return nil, nil
	}

	// fetch storage cluster name
	sc, err := r.getStorageClusterFromRookSecret(r.spokeKubeConfig, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get the storage cluster name from the sercet %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	customData := map[string][]byte{
		common.StorageClusterNameKey: []byte(sc),
	}

	newSecret, err := CreateBlueSecret(secret, common.SourceLabel, sc, clusterName, customData)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, clusterName, err)
	}

	return &newSecret, nil
}

func (RookSecretHandler) getStorageClusterFromRookSecret(spokeKubeConfig *rest.Config, secret *corev1.Secret) (storageCluster string, err error) {
	for _, v := range secret.ObjectMeta.OwnerReferences {
		if v.Kind != "CephCluster" {
			continue
		}

		rclient, err := rookclient.NewForConfig(spokeKubeConfig)
		if err != nil {
			return "", fmt.Errorf("unable to create a rook client err: %v", err)
		}

		found, err := rclient.CephV1().CephClusters(secret.Namespace).Get(context.TODO(), v.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("unable to fetch ceph cluster err: %v", err)
		}

		for _, owner := range found.ObjectMeta.OwnerReferences {
			if owner.Kind != "StorageCluster" {
				continue
			}
			storageCluster = owner.Name
		}

		if storageCluster != "" {
			break
		}
	}

	if storageCluster == "" {
		return storageCluster, fmt.Errorf("could not get storageCluster name")
	}
	return storageCluster, nil
}
