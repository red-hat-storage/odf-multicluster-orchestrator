package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
)

const (
	CreatedByLabelKey   = "multicluster.odf.openshift.io/created-by"
	CreatedByLabelValue = "tokenexchange"
)

func getSecret(lister corev1lister.SecretLister, name, namespace string) (*corev1.Secret, error) {
	se, err := lister.Secrets(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil, err
	case err != nil:
		return nil, err
	}
	return se, nil
}

func generateBlueSecret(secret *corev1.Secret, secretType common.SecretLabelType, name string, managedCluster string, customData map[string][]byte) (nsecret corev1.Secret, err error) {
	if secret == nil {
		return nsecret, fmt.Errorf("cannot create secret on the hub, source secret nil")
	}

	secretData, err := json.Marshal(secret.Data)
	if err != nil {
		return nsecret, fmt.Errorf("cannot create secret on the hub, marshalling failed")
	}

	data := map[string][]byte{
		common.SecretDataKey: secretData,
		common.NamespaceKey:  []byte(secret.Namespace),
	}

	for key, value := range customData {
		data[key] = value
	}

	nSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.CreateUniqueSecretName(managedCluster, secret.Namespace, name),
			Namespace: managedCluster,
			Labels: map[string]string{
				common.SecretLabelTypeKey: string(secretType),
			},
		},
		Type: common.SecretLabelTypeKey,
		Data: data,
	}
	return nSecret, nil
}

func createSecret(client kubernetes.Interface, recorder events.Recorder, newSecret *corev1.Secret) error {
	_, _, err := resourceapply.ApplySecret(client.CoreV1(), recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to apply secret %q in namespace %q. Error %v", newSecret.Name, newSecret.Namespace, err)
	}

	return nil
}

func convertObjectToSecretExchangeController(obj interface{}) (*SecretExchangeController, bool) {
	s, ok := obj.(*SecretExchangeController)
	return s, ok
}

func validateGreenSecret(secret corev1.Secret) error {
	if secret.GetLabels()[common.SecretLabelTypeKey] != string(common.DestinationLabel) {
		return fmt.Errorf("secret %q in namespace %q is not a green secret. Skip syncing with the spoke cluster", secret.Name, secret.Namespace)
	}

	if secret.Data == nil {
		return fmt.Errorf("secret data not found for the secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data["namespace"]) == "" {
		return fmt.Errorf("missing storageCluster namespace info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[common.StorageClusterNameKey]) == "" {
		return fmt.Errorf("missing storageCluster name info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[common.SecretDataKey]) == "" {
		return fmt.Errorf("missing secret-data info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	return nil
}
