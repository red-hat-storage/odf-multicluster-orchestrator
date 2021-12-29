package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RookSecretHandler struct {
	spokeClient client.Client
}

const (
	RookSecretHandlerName            = "rook"
	RookType                         = "kubernetes.io/rook"
	RookDefaultBlueSecretMatchString = "cluster-peer-token"
)

func (RookSecretHandler) GetBlueSecretFilter(obj interface{}) bool {
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

func (RookSecretHandler) GetGreenSecretFilter(obj interface{}) bool {
	metaObj, has := obj.(metav1.Object)
	if has {
		return metaObj.GetLabels()[common.SecretLabelTypeKey] == string(common.DestinationLabel)
	}
	return false
}

func (r RookSecretHandler) CreateBlueSecret(name string, namespace string, obj interface{}) error {
	s, ok := convertObjectToSecretExchangeController(obj)
	if !ok {
		return fmt.Errorf("unable to convert from object to secret exchange controller for secret %q in namespace %q in managed cluster. Error %v", name, namespace)
	}

	// fetch secret
	secret, err := getSecret(s.spokeSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch := r.GetBlueSecretFilter(secret)
	if !isMatch {
		// ignore handler which secret filter is not matched
		return nil
	}

	// fetch storage cluster name
	sc, err := r.getStorageClusterFromRookSecret(s.spokeKubeConfig, secret)
	if err != nil {
		return fmt.Errorf("failed to get the storage cluster name from the sercet %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	customData := map[string][]byte{
		common.StorageClusterNameKey: []byte(sc),
	}

	newSecret, err := generateBlueSecret(secret, common.SourceLabel, sc, s.clusterName, customData)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, s.clusterName, err)
	}

	err = createSecret(s.hubKubeClient, s.recorder, &newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", name, namespace, s.clusterName, err)
	}

	return nil
}

func (r RookSecretHandler) CreateGreenSecret(name string, namespace string, obj interface{}) error {
	s, ok := convertObjectToSecretExchangeController(obj)
	if !ok {
		return fmt.Errorf("unable to convert from object to secret exchange controller for secret %q in namespace %q in managed cluster. Error %v", name, namespace)
	}
	secret, err := getSecret(s.hubSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the sercet %q in namespace %q in hub. Error %v", name, namespace, err)
	}

	if err := validateGreenSecret(*secret); err != nil {
		return fmt.Errorf("failed to validate secret %q", secret.Name)
	}

	data := make(map[string][]byte)
	err = json.Unmarshal(secret.Data[common.SecretDataKey], &data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret data for the secret %q in namespace %q. %v", secret.Name, secret.Namespace, err)
	}

	toNamespace := string(secret.Data["namespace"])
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: toNamespace,
			Labels:    map[string]string{CreatedByLabelKey: CreatedByLabelValue},
		},
		Data: data,
	}

	// Create secrete on spoke cluster
	err = createSecret(s.spokeKubeClient, s.recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync hub secret %q in managed cluster in namespace %q. %v", newSecret.Name, toNamespace, err)
	}

	klog.Infof("successfully synced hub secret %q in managed cluster in namespace %q", newSecret.Name, toNamespace)

	storageClusterName := string(secret.Data[common.StorageClusterNameKey])
	err = r.updateStorageCluster(newSecret.Name, storageClusterName, toNamespace, s)
	if err != nil {
		return fmt.Errorf("failed to update secret name %q in the storageCluster %q in namespace %q. %v", newSecret.Name, storageClusterName, toNamespace, err)
	}

	return nil
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

func (r RookSecretHandler) updateStorageCluster(secretName, storageClusterName, storageClusterNamespace string, s *SecretExchangeController) error {
	ctx := context.TODO()
	sc := &ocsv1.StorageCluster{}
	err := r.spokeClient.Get(ctx, types.NamespacedName{Name: storageClusterName, Namespace: storageClusterNamespace}, sc)
	if err != nil {
		return fmt.Errorf("failed to get storage cluster %q in namespace %q. %v", storageClusterName, storageClusterNamespace, err)
	}

	// Update secret name
	if !contains(sc.Spec.Mirroring.PeerSecretNames, secretName) {
		sc.Spec.Mirroring.PeerSecretNames = append(sc.Spec.Mirroring.PeerSecretNames, secretName)
		err := r.spokeClient.Update(ctx, sc)
		if err != nil {
			return fmt.Errorf("failed to update storagecluster %q in the namespace %q. %v", storageClusterName, storageClusterNamespace, err)
		}
	}

	return nil
}

// contains checks if an item exists in a given list.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
