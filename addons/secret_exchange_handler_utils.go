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
		return nsecret, fmt.Errorf("failed to marshal secret data for secret '%s' in namespace '%s': %v", secret.Name, secret.Namespace, err)
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
				utils.HubRecoveryLabel:   "",
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
		return nil, fmt.Errorf("failed to marshal data from Rook-Ceph monitor: error: %v, data source: %q", err, rookCephMon.Name)
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
				utils.HubRecoveryLabel:   "",
			},
		},
		Type: utils.SecretLabelTypeKey,
		Data: data,
	}

	return &nSecret, nil

}

func validateGreenSecret(secret corev1.Secret) error {
	// Validate the secret type label
	if secret.GetLabels()[utils.SecretLabelTypeKey] != string(utils.DestinationLabel) {
		return fmt.Errorf("validation failed: expected label '%s' with value '%s' for GreenSecret, but found '%s' or no label. Secret '%s' in namespace '%s' will be skipped for syncing",
			utils.SecretLabelTypeKey, utils.DestinationLabel, secret.GetLabels()[utils.SecretLabelTypeKey], secret.Name, secret.Namespace)
	}

	// Validate the presence of secret data
	if secret.Data == nil {
		return fmt.Errorf("validation failed: no data found in the secret '%s' in namespace '%s'. A non-empty data field is required",
			secret.Name, secret.Namespace)
	}

	// Validate specific data fields for completeness
	if namespace, ok := secret.Data["namespace"]; !ok || string(namespace) == "" {
		return fmt.Errorf("validation failed: missing or empty 'namespace' key in the data of the secret '%s' in namespace '%s'. This key is required for proper functionality",
			secret.Name, secret.Namespace)
	}

	if clusterName, ok := secret.Data[utils.StorageClusterNameKey]; !ok || string(clusterName) == "" {
		return fmt.Errorf("validation failed: missing or empty '%s' key in the data of the secret '%s' in namespace '%s'. This key is required for identifying the storage cluster",
			utils.StorageClusterNameKey, secret.Name, secret.Namespace)
	}

	if secretData, ok := secret.Data[utils.SecretDataKey]; !ok || string(secretData) == "" {
		return fmt.Errorf("validation failed: missing or empty '%s' key in the data of the secret '%s' in namespace '%s'. This key is essential for the operation",
			utils.SecretDataKey, secret.Name, secret.Namespace)
	}

	return nil
}
