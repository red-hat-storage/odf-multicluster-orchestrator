package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultExternalSecretName = "rook-ceph-mon"
)

func (r *GreenSecretReconciler) syncGreenSecretForRook(ctx context.Context, secret corev1.Secret) error {
	var err error

	data := make(map[string][]byte)
	err = json.Unmarshal(secret.Data[utils.SecretDataKey], &data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret data for the secret %q in namespace %q: %v", secret.Name, secret.Namespace, err)
	}
	for k, v := range secret.Data {
		if k != utils.SecretDataKey {
			data[k] = v
		}
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

	err = r.SpokeClient.Create(ctx, newSecret, &client.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existingSecret := &corev1.Secret{}
			err = r.SpokeClient.Get(ctx, types.NamespacedName{
				Name:      newSecret.Name,
				Namespace: newSecret.Namespace,
			}, existingSecret)
			if err != nil {
				return fmt.Errorf("failed to get existing secret %q in namespace %q: %v",
					newSecret.Name, newSecret.Namespace, err)
			}

			if !reflect.DeepEqual(existingSecret.Data, newSecret.Data) {
				existingSecret.Data = newSecret.Data

				err = r.SpokeClient.Update(ctx, existingSecret, &client.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update secret %q in namespace %q: %v",
						newSecret.Name, newSecret.Namespace, err)
				}
				r.Logger.Info("Successfully updated existing secret on spoke cluster",
					"secret", newSecret.Name,
					"namespace", newSecret.Namespace)
				return nil
			}

			r.Logger.Info("Secret already exists on spoke cluster with matching data",
				"secret", newSecret.Name,
				"namespace", newSecret.Namespace)
			return nil
		}
		return fmt.Errorf("failed to sync hub secret %q to managed cluster in namespace %q: %v",
			newSecret.Name, toNamespace, err)
	}

	r.Logger.Info("Successfully created new secret on spoke cluster",
		"secret", newSecret.Name,
		"namespace", newSecret.Namespace)

	storageClusterName := string(secret.Data[utils.StorageClusterNameKey])
	err = updateStorageCluster(newSecret.Name, storageClusterName, toNamespace, r.SpokeClient)
	if err != nil {
		return fmt.Errorf("failed to update secret %q in the storageCluster %q in namespace %q: %v", newSecret.Name, storageClusterName, toNamespace, err)
	}

	r.Logger.Info("Successfully synced hub secret to managed cluster", "secret", newSecret.Name, "namespace", toNamespace)
	return nil
}

func updateStorageCluster(secretName, storageClusterName, storageClusterNamespace string, spokeClient client.Client) error {
	ctx := context.TODO()
	sc := &ocsv1.StorageCluster{}
	err := spokeClient.Get(ctx, types.NamespacedName{Name: storageClusterName, Namespace: storageClusterNamespace}, sc)
	if err != nil {
		return fmt.Errorf("failed to get storage cluster %q in namespace %q. %v", storageClusterName, storageClusterNamespace, err)
	}

	// Update secret name
	if sc.Spec.Mirroring == nil {
		sc.Spec.Mirroring = &ocsv1.MirroringSpec{}
	}
	if !utils.ContainsString(sc.Spec.Mirroring.PeerSecretNames, secretName) {
		sc.Spec.Mirroring.PeerSecretNames = append(sc.Spec.Mirroring.PeerSecretNames, secretName)
		err := spokeClient.Update(ctx, sc)
		if err != nil {
			return fmt.Errorf("failed to update storagecluster %q in the namespace %q. %v", storageClusterName, storageClusterNamespace, err)
		}
	}

	return nil
}
