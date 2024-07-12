package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RookType                                  = "kubernetes.io/rook"
	RookDefaultBlueSecretMatchConvergedString = "cluster-peer-token"
	DefaultExternalSecretName                 = "rook-ceph-mon"
)

func getBlueSecretFilterForRook(obj interface{}) bool {
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

func (r *BlueSecretReconciler) syncBlueSecretForRook(ctx context.Context, secret corev1.Secret) error {
	logger := r.Logger.With("secret", secret.Name, "namespace", secret.Namespace)
	// fetch storage cluster name
	storageClusterName, err := getStorageClusterFromRookSecret(ctx, r.SpokeClient, secret)
	if err != nil {
		return fmt.Errorf("failed to get the storage cluster name from the secret %q in namespace %q in managed cluster. Error %v", secret.Name, secret.Namespace, err)
	}

	clusterType, err := utils.GetClusterType(storageClusterName, secret.Namespace, r.SpokeClient)
	if err != nil {
		return fmt.Errorf("failed to get the cluster type (converged, external) for the spoke cluster. Error %v", err)
	}

	logger.Info("Detected cluster type", "clusterType", string(clusterType))
	var customData map[string][]byte
	var labelType utils.SecretLabelType
	var blueSecret *corev1.Secret

	if clusterType == utils.CONVERGED && secret.Name != DefaultExternalSecretName {
		customData = map[string][]byte{
			utils.SecretOriginKey: []byte(utils.OriginMap["RookOrigin"]),
			utils.ClusterTypeKey:  []byte(utils.CONVERGED),
		}
		labelType = utils.SourceLabel

		blueSecret, err = generateBlueSecret(secret, labelType, utils.CreateUniqueSecretName(r.SpokeClusterName, secret.Namespace, storageClusterName), storageClusterName, r.SpokeClusterName, customData)
		if err != nil {
			return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, r.SpokeClusterName, err)
		}
	} else if clusterType == utils.EXTERNAL && secret.Name == DefaultExternalSecretName {
		customData = map[string][]byte{
			utils.SecretOriginKey: []byte(utils.OriginMap["RookOrigin"]),
			utils.ClusterTypeKey:  []byte(utils.EXTERNAL),
		}
		labelType = utils.InternalLabel

		blueSecret, err = generateBlueSecretForExternal(secret, labelType, utils.CreateUniqueSecretName(r.SpokeClusterName, secret.Namespace, storageClusterName), storageClusterName, r.SpokeClusterName, customData)
		if err != nil {
			return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, r.SpokeClusterName, err)
		}
	} else if clusterType == utils.CONVERGED && secret.Name == DefaultExternalSecretName {
		logger.Info("Skip external secret in converged mode")
		return nil
	} else {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: ClusterType is unknown, should be converged or external", secret.Name, secret.Namespace, r.SpokeClusterName)
	}

	// Create secret on hub cluster
	err = r.HubClient.Create(ctx, blueSecret, &client.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Secret already exists on hub cluster, not creating again")
			return nil
		}
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, r.SpokeClusterName, err)
	}

	logger.Info("Successfully synced managed cluster secret to hub cluster", "hubNamespace", r.SpokeClusterName)
	return nil
}

func (r *GreenSecretReconciler) syncGreenSecretForRook(ctx context.Context, secret corev1.Secret) error {
	var err error

	data := make(map[string][]byte)
	err = json.Unmarshal(secret.Data[utils.SecretDataKey], &data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret data for the secret %q in namespace %q: %v", secret.Name, secret.Namespace, err)
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

	// Create secret on spoke cluster
	err = r.SpokeClient.Create(ctx, newSecret, &client.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			r.Logger.Info("Secret already exists on spoke cluster, not creating again", "secret", newSecret.Name, "namespace", newSecret.Namespace)
			return nil
		}
		return fmt.Errorf("failed to sync hub secret %q to managed cluster in namespace %q: %v", newSecret.Name, toNamespace, err)
	}

	storageClusterName := string(secret.Data[utils.StorageClusterNameKey])
	err = updateStorageCluster(newSecret.Name, storageClusterName, toNamespace, r.SpokeClient)
	if err != nil {
		return fmt.Errorf("failed to update secret %q in the storageCluster %q in namespace %q: %v", newSecret.Name, storageClusterName, toNamespace, err)
	}

	r.Logger.Info("Successfully synced hub secret to managed cluster", "secret", newSecret.Name, "namespace", toNamespace)
	return nil
}

func getStorageClusterFromRookSecret(ctx context.Context, client client.Client, secret corev1.Secret) (storageCluster string, err error) {
	for _, v := range secret.ObjectMeta.OwnerReferences {
		if v.Kind != "CephCluster" && v.Kind != "StorageCluster" {
			continue
		}

		if v.Kind == "CephCluster" {
			var cephCluster rookv1.CephCluster
			err := client.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: v.Name}, &cephCluster)
			if err != nil {
				return "", fmt.Errorf("failed to fetch CephCluster '%s' referenced by secret '%s' in namespace '%s': %v", v.Name, secret.Name, secret.Namespace, err)
			}

			for _, owner := range cephCluster.ObjectMeta.OwnerReferences {
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
		return "", fmt.Errorf("could not determine storageCluster name from secret '%s' in namespace '%s'", secret.Name, secret.Namespace)
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
