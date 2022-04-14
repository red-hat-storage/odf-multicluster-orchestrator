package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"os"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rookSecretHandler struct {
	spokeClient client.Client
	hubClient   client.Client
	rookClient  rookclient.Interface
}

const (
	RookType                         = "kubernetes.io/rook"
	RookDefaultBlueSecretMatchString = "cluster-peer-token"
)

func (rookSecretHandler) getBlueSecretFilter(obj interface{}) bool {
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

func (rookSecretHandler) getGreenSecretFilter(obj interface{}) bool {
	metaObj, has := obj.(metav1.Object)
	if has {
		return metaObj.GetLabels()[utils.SecretLabelTypeKey] == string(utils.DestinationLabel)
	}
	return false
}

func (r rookSecretHandler) syncBlueSecret(name string, namespace string, c *blueSecretTokenExchangeAgentController) error {
	// fetch secret
	secret, err := getSecret(c.spokeSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}
	isMatch := r.getBlueSecretFilter(secret)
	if !isMatch {
		// ignore handler which secret filter is not matched
		return nil
	}

	// fetch storage cluster name
	sc, err := getStorageClusterFromRookSecret(secret, r.rookClient)
	if err != nil {
		return fmt.Errorf("failed to get the storage cluster name from the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	customData := map[string][]byte{
		utils.SecretOriginKey: []byte(utils.OriginMap["RookOrigin"]),
	}

	newSecret, err := generateBlueSecret(secret, utils.SourceLabel, utils.CreateUniqueSecretName(c.clusterName, secret.Namespace, sc), sc, c.clusterName, customData)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
	}

	err = createSecret(c.hubKubeClient, c.recorder, &newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", name, namespace, c.clusterName, err)
	}

	klog.Infof("successfully synced managed cluster secret %q from namespace %v to the hub cluster in namespace %q", name, namespace, c.clusterName)

	return nil
}

func (r rookSecretHandler) syncGreenSecret(name string, namespace string, c *greenSecretTokenExchangeAgentController) error {
	secret, err := getSecret(c.hubSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in hub. Error %v", name, namespace, err)
	}

	if err := validateGreenSecret(*secret); err != nil {
		return fmt.Errorf("failed to validate secret %q", secret.Name)
	}

	data := make(map[string][]byte)
	err = json.Unmarshal(secret.Data[utils.SecretDataKey], &data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret data for the secret %q in namespace %q. %v", secret.Name, secret.Namespace, err)
	}

	toNamespace := string(secret.Data["namespace"])
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: toNamespace,
			Labels:    map[string]string{utils.CreatedByLabelKey: TokenExchangeName},
		},
		Data: data,
	}

	// Create secrete on spoke cluster
	err = createSecret(c.spokeKubeClient, c.recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync hub secret %q in managed cluster in namespace %q. %v", newSecret.Name, toNamespace, err)
	}

	klog.Infof("successfully synced hub secret %q in managed cluster in namespace %q", newSecret.Name, toNamespace)

	storageClusterName := string(secret.Data[utils.StorageClusterNameKey])
	err = updateStorageCluster(newSecret.Name, storageClusterName, toNamespace, r.spokeClient)
	if err != nil {
		return fmt.Errorf("failed to update secret name %q in the storageCluster %q in namespace %q. %v", newSecret.Name, storageClusterName, toNamespace, err)
	}

	return nil
}

func getStorageClusterFromRookSecret(secret *corev1.Secret, rclient rookclient.Interface) (storageCluster string, err error) {
	for _, v := range secret.ObjectMeta.OwnerReferences {
		if v.Kind != "CephCluster" {
			continue
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

func updateStorageCluster(secretName, storageClusterName, storageClusterNamespace string, spokeClient client.Client) error {
	ctx := context.TODO()
	sc := &ocsv1.StorageCluster{}
	err := spokeClient.Get(ctx, types.NamespacedName{Name: storageClusterName, Namespace: storageClusterNamespace}, sc)
	if err != nil {
		return fmt.Errorf("failed to get storage cluster %q in namespace %q. %v", storageClusterName, storageClusterNamespace, err)
	}

	// Update secret name
	if !contains(sc.Spec.Mirroring.PeerSecretNames, secretName) {
		sc.Spec.Mirroring.PeerSecretNames = append(sc.Spec.Mirroring.PeerSecretNames, secretName)
		err := spokeClient.Update(ctx, sc)
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
