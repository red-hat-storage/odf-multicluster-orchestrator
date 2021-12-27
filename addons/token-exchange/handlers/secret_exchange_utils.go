package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
)

func GetSecret(lister corev1lister.SecretLister, name, namespace string) (*corev1.Secret, error) {
	se, err := lister.Secrets(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil, err
	case err != nil:
		return nil, err
	}
	return se, nil
}

func GetConfigMap(lister corev1lister.ConfigMapLister, name, namespace string) (*corev1.ConfigMap, error) {
	confgMap, err := lister.ConfigMaps(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil, err
	case err != nil:
		return nil, err
	}
	return confgMap, nil
}

func CreateBlueSecret(secret *corev1.Secret, secretType common.SecretLabelType, name string, managedCluster string, customData map[string][]byte) (nsecret corev1.Secret, err error) {
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
