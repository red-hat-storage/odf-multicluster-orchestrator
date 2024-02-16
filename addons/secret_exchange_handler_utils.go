package addons

import (
	"encoding/json"
	"fmt"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateBlueSecret(secret corev1.Secret, secretType utils.SecretLabelType, uniqueName string, sc string, managedCluster string, customData map[string][]byte) (nsecret *corev1.Secret, err error) {
	secretData, err := json.Marshal(secret.Data)
	if err != nil {
		return nsecret, fmt.Errorf("cannot create secret on the hub, marshalling failed")
	}

	data := make(map[string][]byte)

	data[utils.NamespaceKey] = []byte(secret.Namespace)
	data[utils.StorageClusterNameKey] = []byte(sc)
	data[utils.SecretDataKey] = secretData

	for key, value := range customData {
		data[key] = value
	}

	nSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uniqueName,
			Namespace: managedCluster,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(secretType),
			},
		},
		Type: utils.SecretLabelTypeKey,
		Data: data,
	}
	return &nSecret, nil
}

func generateBlueSecretForExternal(rookCephMon corev1.Secret, labelType utils.SecretLabelType, name string, sc string, managedClusterName string, customData map[string][]byte) (*corev1.Secret, error) {
	secretData, err := json.Marshal(rookCephMon.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal secret data err: %v", err)
	}

	data := make(map[string][]byte)
	data[utils.NamespaceKey] = []byte(rookCephMon.Namespace)
	data[utils.StorageClusterNameKey] = []byte(sc)
	for key, value := range customData {
		data[key] = value
	}

	data[utils.SecretDataKey] = secretData

	nSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: managedClusterName,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(labelType),
			},
		},
		Type: utils.SecretLabelTypeKey,
		Data: data,
	}

	return &nSecret, nil

}

func validateGreenSecret(secret corev1.Secret) error {
	if secret.GetLabels()[utils.SecretLabelTypeKey] != string(utils.DestinationLabel) {
		return fmt.Errorf("secret %q in namespace %q is not a green secret. Skip syncing with the spoke cluster", secret.Name, secret.Namespace)
	}

	if secret.Data == nil {
		return fmt.Errorf("secret data not found for the secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data["namespace"]) == "" {
		return fmt.Errorf("missing storageCluster namespace info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[utils.StorageClusterNameKey]) == "" {
		return fmt.Errorf("missing storageCluster name info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[utils.SecretDataKey]) == "" {
		return fmt.Errorf("missing secret-data info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	return nil
}
