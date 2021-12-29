package common

import (
	"crypto/sha512"
	"errors"
	"fmt"
	"reflect"
	"strings"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type SecretLabelType string

const (
	SourceLabel           SecretLabelType = "BLUE"
	DestinationLabel      SecretLabelType = "GREEN"
	InternalLabel         SecretLabelType = "INTERNAL"
	IgnoreLabel           SecretLabelType = "IGNORE"
	SecretLabelTypeKey                    = "multicluster.odf.openshift.io/secret-type"
	NamespaceKey                          = "namespace"
	StorageClusterNameKey                 = "storage-cluster-name"
	SecretDataKey                         = "secret-data"
	SecretOriginKey                       = "secret-origin"

	// rook
	RookOrigin = "rook"
)

var (
	SourceOrDestinationPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return IsSecretSource(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return IsSecretSource(e.Object) || IsSecretDestination(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return (IsSecretSource(e.ObjectOld) && IsSecretSource(e.ObjectNew)) ||
				(IsSecretDestination(e.ObjectOld) && IsSecretDestination(e.ObjectNew))
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}
)

func GetInternalLabel(secret *corev1.Secret) SecretLabelType {
	return SecretLabelType(secret.Labels[SecretLabelTypeKey])
}

func isObjectASecretWithProvidedLabel(obj client.Object, label, value string) bool {
	sec, ok := obj.(*corev1.Secret)
	if !ok {
		return false
	}
	lblVal, ok := sec.Labels[label]
	// if 'ok' (ie; if provided label key is present) AND values match,
	// then return true
	return ok && (lblVal == value)
}

// IsSecretSource returns true if the provided object is a secret with Source label
func IsSecretSource(obj client.Object) bool {
	return isObjectASecretWithProvidedLabel(obj, SecretLabelTypeKey, string(SourceLabel))
}

// IsSecretDestination returns true if the provided object is a secret with
// Destination label
func IsSecretDestination(obj client.Object) bool {
	return isObjectASecretWithProvidedLabel(obj, SecretLabelTypeKey, string(DestinationLabel))
}

func ValidateInternalSecret(internalSecret *corev1.Secret, expectedLabel SecretLabelType) error {
	if internalSecret == nil {
		return errors.New("provided secret is 'nil'")
	}
	if expectedLabel == "" {
		return errors.New("an empty expected label provided. please provide 'IgnoreLabel' instead")
	}
	if expectedLabel != IgnoreLabel {
		if expectedLabel != GetInternalLabel(internalSecret) {
			return errors.New("expected and secret's labels don't match")
		}
	}
	if internalSecret.Data == nil {
		return errors.New("secret's data map is 'nil'")
	}
	// check whether all the keys are present in data
	_, namespaceKeyOk := internalSecret.Data[NamespaceKey]
	_, scNameKeyOk := internalSecret.Data[StorageClusterNameKey]
	_, secretDataKeyOk := internalSecret.Data[SecretDataKey]
	if !(namespaceKeyOk && scNameKeyOk && secretDataKeyOk) {
		return errors.New("expected data map keys are not present")
	}
	return nil
}

// ValidateSourceSecret validates whether the given secret is a Source type
func ValidateSourceSecret(sourceSecret *corev1.Secret) error {
	return ValidateInternalSecret(sourceSecret, SourceLabel)
}

// ValidateDestinationSecret validates whether the given secret is a Destination type
func ValidateDestinationSecret(sourceSecret *corev1.Secret) error {
	return ValidateInternalSecret(sourceSecret, DestinationLabel)
}

// createInternalSecret a common function to create any type secret
func createInternalSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretType SecretLabelType,
	secretData []byte) *corev1.Secret {
	retSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretNameAndNamespace.Name,
			Namespace: secretNameAndNamespace.Namespace,
			Labels: map[string]string{
				SecretLabelTypeKey: string(secretType),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretDataKey:         secretData,
			NamespaceKey:          []byte(storageClusterNameAndNamespace.Namespace),
			StorageClusterNameKey: []byte(storageClusterNameAndNamespace.Name),
		},
	}
	return retSecret
}

// CreateSourceSecret creates a source secret
func CreateSourceSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretData []byte) *corev1.Secret {
	return createInternalSecret(secretNameAndNamespace,
		storageClusterNameAndNamespace,
		SourceLabel,
		secretData)
}

// CreateDestinationSecret creates a destination secret
func CreateDestinationSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretData []byte) *corev1.Secret {
	return createInternalSecret(secretNameAndNamespace,
		storageClusterNameAndNamespace,
		DestinationLabel,
		secretData)
}

// CreateUniqueName function creates a sha512 hex sum from the given parameters
func CreateUniqueName(params ...string) string {
	genStr := strings.Join(params, "-")
	return fmt.Sprintf("%x", sha512.Sum512([]byte(genStr)))
}

// CreateUniqueSecretName function creates a name of 40 chars using sha512 hex sum from the given parameters
func CreateUniqueSecretName(managedCluster, storageClusterNamespace, storageClusterName string, prefix ...string) string {
	if len(prefix) > 0 {
		return CreateUniqueName(prefix[0], managedCluster, storageClusterNamespace, storageClusterName)[0:39]
	}
	return CreateUniqueName(managedCluster, storageClusterNamespace, storageClusterName)[0:39]
}

// CreatePeerRefFromSecret function creates a 'PeerRef' object
// from the internal secret details
func CreatePeerRefFromSecret(secret *corev1.Secret) (multiclusterv1alpha1.PeerRef, error) {
	if err := ValidateInternalSecret(secret, IgnoreLabel); err != nil {
		return multiclusterv1alpha1.PeerRef{}, err
	}
	retPeerRef := multiclusterv1alpha1.PeerRef{
		ClusterName: secret.Namespace,
		StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
			Name:      string(secret.Data[StorageClusterNameKey]),
			Namespace: string(secret.Data[NamespaceKey])},
	}
	return retPeerRef, nil
}

func FindMatchingSecretWithPeerRef(peerRef multiclusterv1alpha1.PeerRef, secrets []corev1.Secret) *corev1.Secret {
	var matchingSourceSecret *corev1.Secret
	for _, eachSecret := range secrets {
		secretPeerRef, err := CreatePeerRefFromSecret(&eachSecret)
		// ignore any error and continue with the next
		if err != nil {
			continue
		}
		if reflect.DeepEqual(secretPeerRef, peerRef) {
			matchingSourceSecret = &eachSecret
			break
		}
	}
	return matchingSourceSecret
}
