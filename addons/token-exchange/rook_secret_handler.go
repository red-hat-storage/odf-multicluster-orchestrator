package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rookSecretHandler struct {
	spokeClient client.Client
	hubClient   client.Client
}

const (
	RookType                                  = "kubernetes.io/rook"
	RookDefaultBlueSecretMatchConvergedString = "cluster-peer-token"
	DefaultExternalSecretName                 = "rook-ceph-mon"
)

func (rookSecretHandler) getBlueSecretFilter(obj interface{}) bool {
	blueSecretMatchString := os.Getenv("TOKEN_EXCHANGE_SOURCE_SECRET_STRING_MATCH")

	if blueSecretMatchString == "" {
		blueSecretMatchString = RookDefaultBlueSecretMatchConvergedString
	}

	if s, ok := obj.(*corev1.Secret); ok {
		if strings.Contains(s.ObjectMeta.Name, blueSecretMatchString) {
			for _, owner := range s.ObjectMeta.OwnerReferences {
				if owner.Kind == "CephCluster" {
					return true
				}
			}
		}

		if s.ObjectMeta.Name == DefaultExternalSecretName {
			for _, owner := range s.ObjectMeta.OwnerReferences {
				if owner.Kind == "StorageCluster" {
					return true
				}
			}
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
	klog.Infof("detected secret %s in %s namespace", name, namespace)

	// fetch storage cluster name
	sc, err := getStorageClusterFromRookSecret(secret, r.spokeClient)
	if err != nil {
		return fmt.Errorf("failed to get the storage cluster name from the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	clusterType, err := utils.GetClusterType(sc, namespace, r.spokeClient)
	if err != nil {
		return fmt.Errorf("failed to get the cluster type (converged, external) for the spoke cluster. Error %v", err)
	}

	klog.Infof("detected cluster type: %s", string(clusterType))
	var customData map[string][]byte
	var labelType utils.SecretLabelType
	var blueSecret *corev1.Secret

	// Accept secret with converged mode and secret name should not be rook-ceph-mon
	if clusterType == utils.CONVERGED && secret.Name != DefaultExternalSecretName {
		customData = map[string][]byte{
			utils.SecretOriginKey: []byte(utils.OriginMap["RookOrigin"]),
			utils.ClusterTypeKey:  []byte(utils.CONVERGED),
		}
		labelType = utils.SourceLabel

		blueSecret, err = generateBlueSecret(secret, labelType, utils.CreateUniqueSecretName(c.clusterName, secret.Namespace, sc), sc, c.clusterName, customData)
		if err != nil {
			return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
		}
		// Accept secret if the cluster is external and secret name is rook-ceph-mon
	} else if clusterType == utils.EXTERNAL && secret.Name == DefaultExternalSecretName {
		customData = map[string][]byte{
			utils.SecretOriginKey: []byte(utils.OriginMap["RookOrigin"]),
			utils.ClusterTypeKey:  []byte(utils.EXTERNAL),
		}
		labelType = utils.InternalLabel
		blueSecret, err = generateBlueSecretForExternal(secret, labelType, utils.CreateUniqueSecretName(c.clusterName, secret.Namespace, sc), sc, c.clusterName, customData)
		if err != nil {
			return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
		}
		// If both the parameters above don't match, then it is unknown secret which is either in external or converged mode.
	} else if clusterType == utils.CONVERGED && secret.Name == DefaultExternalSecretName {
		// detects rook-ceph-mon in converged mode cluster. Likely scenario but we do not need to process this secret.
		return nil
	} else {
		// any other case than the above
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: ClusterType is unknown, should be converged or external", secret.Name, secret.Namespace, c.clusterName)
	}
	err = createSecret(c.hubKubeClient, c.recorder, blueSecret)
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
			Labels:    map[string]string{utils.CreatedByLabelKey: setup.TokenExchangeName},
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

func getStorageClusterFromRookSecret(secret *corev1.Secret, rclient client.Client) (storageCluster string, err error) {
	for _, v := range secret.ObjectMeta.OwnerReferences {
		if v.Kind != "CephCluster" && v.Kind != "StorageCluster" {
			continue
		}

		if v.Kind == "CephCluster" {
			var found rookv1.CephCluster
			err := rclient.Get(context.TODO(), types.NamespacedName{Name: v.Name, Namespace: secret.Namespace}, &found)
			if err != nil {
				return "", fmt.Errorf("unable to fetch ceph cluster err: %v", err)
			}

			for _, owner := range found.ObjectMeta.OwnerReferences {
				if owner.Kind != "StorageCluster" {
					continue
				}
				storageCluster = owner.Name
			}

		} else if v.Kind == "StorageCluster" {
			storageCluster = v.Name
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
	if !utils.ContainsString(sc.Spec.Mirroring.PeerSecretNames, secretName) {
		sc.Spec.Mirroring.PeerSecretNames = append(sc.Spec.Mirroring.PeerSecretNames, secretName)
		err := spokeClient.Update(ctx, sc)
		if err != nil {
			return fmt.Errorf("failed to update storagecluster %q in the namespace %q. %v", storageClusterName, storageClusterNamespace, err)
		}
	}

	return nil
}
