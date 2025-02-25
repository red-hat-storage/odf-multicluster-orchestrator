package addons

import (
	"encoding/json"
	"fmt"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateBlueSecret(
	secret corev1.Secret,
	secretType utils.SecretLabelType,
	uniqueName string,
	sc string,
	managedCluster string,
	customData map[string][]byte,
	annotations map[string]string,
) (*corev1.Secret, error) {
	secretData, err := json.Marshal(secret.Data)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal secret data for secret '%s' in namespace '%s': %v",
			secret.Name, secret.Namespace, err,
		)
	}

	data := make(map[string][]byte)
	data[utils.NamespaceKey] = []byte(secret.Namespace)
	data[utils.StorageClusterNameKey] = []byte(sc)
	data[utils.SecretDataKey] = secretData

	for key, value := range customData {
		data[key] = value
	}

	nSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uniqueName,
			Namespace: managedCluster,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(secretType),
				utils.HubRecoveryLabel:   "",
			},
			Annotations: annotations,
		},
		Type: utils.SecretLabelTypeKey,
		Data: data,
	}

	return nSecret, nil
}
