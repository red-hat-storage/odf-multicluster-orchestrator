package utils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretLabelType string

const (
	SourceLabel           SecretLabelType = "BLUE"
	DestinationLabel      SecretLabelType = "GREEN"
	InternalLabel         SecretLabelType = "INTERNAL"
	IgnoreLabel           SecretLabelType = "IGNORE"
	ProviderLabel         SecretLabelType = "PROVIDER"
	SecretLabelTypeKey                    = "multicluster.odf.openshift.io/secret-type"
	CreatedByLabelKey                     = "multicluster.odf.openshift.io/created-by"
	NamespaceKey                          = "namespace"
	StorageClusterNameKey                 = "storage-cluster-name"
	SecretDataKey                         = "secret-data"
	SecretOriginKey                       = "secret-origin"
	MirrorPeerSecret                      = "mirrorpeersecret"
	RookTokenKey                          = "token"
	ClusterTypeKey                        = "cluster_type"
	HubRecoveryLabel                      = "cluster.open-cluster-management.io/backup"
	StorageIDKey                          = "multicluster.odf.openshift.io/storageid"
)

type RookToken struct {
	FSID      string `json:"fsid"`
	Namespace string `json:"namespace"`
	MonHost   string `json:"mon_host"`
	ClientId  string `json:"client_id"`
	Key       string `json:"key"`
}

type RookTokenExternal struct {
	CephSecret   string `json:"ceph-secret,omitempty"`
	CephUsername string `json:"ceph-username,omitempty"`
	FSID         string `json:"fsid"`
	MonSecret    string `json:"mon-secret,omitempty"`
}

type HubToken struct {
	Token   string `json:"token"`
	Cluster string `json:"cluster"`
}
type S3Token struct {
	AccessKeyID          string `json:"AWS_ACCESS_KEY_ID"`
	SecretAccessKey      string `json:"AWS_SECRET_ACCESS_KEY"`
	S3Bucket             string `json:"s3Bucket"`
	S3CompatibleEndpoint string `json:"s3CompatibleEndpoint"`
	S3ProfileName        string `json:"s3ProfileName"`
	S3Region             string `json:"s3Region"`
}

var OriginMap = map[string]string{"RookOrigin": "rook", "S3Origin": "S3"}

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

// IsSecretInternal returns true if the provided object is a secret with
// Inernal label
func IsSecretInternal(obj client.Object) bool {
	return isObjectASecretWithProvidedLabel(obj, SecretLabelTypeKey, string(InternalLabel))
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
	_, secretOriginKeyOk := internalSecret.Data[SecretOriginKey]
	_, secretDataKeyOk := internalSecret.Data[SecretDataKey]

	if !(namespaceKeyOk && secretDataKeyOk && secretOriginKeyOk && scNameKeyOk) {
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

func HasHubRecoveryLabels(secret *corev1.Secret) bool {
	return secret.ObjectMeta.Labels[HubRecoveryLabel] == ""
}

func ValidateS3Secret(data map[string][]byte) bool {
	_, s3ProfileName := data[S3ProfileName]
	_, s3BucketNameOK := data[S3BucketName]
	_, s3EndpointOk := data[S3Endpoint]
	_, s3Region := data[S3Region]
	_, awsAccessKeyIdOk := data[AwsAccessKeyId]
	_, awsAccessKeyOk := data[AwsSecretAccessKey]
	return s3ProfileName && s3BucketNameOK && s3EndpointOk && s3Region && awsAccessKeyIdOk && awsAccessKeyOk
}

// createInternalSecret a common function to create any type secret
func createInternalSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretType SecretLabelType,
	secretData []byte, secretOrigin string) *corev1.Secret {
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
			SecretOriginKey:       []byte(secretOrigin),
		},
	}
	return retSecret
}

// CreateSourceSecret creates a source secret
func CreateSourceSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretData []byte, SecretOrigin string) *corev1.Secret {
	return createInternalSecret(secretNameAndNamespace,
		storageClusterNameAndNamespace,
		SourceLabel,
		secretData,
		SecretOrigin)
}

// CreateDestinationSecret creates a destination secret
func CreateDestinationSecret(secretNameAndNamespace types.NamespacedName,
	storageClusterNameAndNamespace types.NamespacedName,
	secretData []byte, secretOrigin string) *corev1.Secret {
	return createInternalSecret(secretNameAndNamespace,
		storageClusterNameAndNamespace,
		DestinationLabel,
		secretData,
		secretOrigin,
	)
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

// FetchAllSecretsWithLabel will get all the internal secrets in the namespace and with the provided label
// if the namespace is empty, it will fetch from all the namespaces
// if the label type is 'Ignore', it will fetch all the internal secrets (both source and destination)
func FetchAllSecretsWithLabel(ctx context.Context, rc client.Client, namespace string, secretLabelType SecretLabelType) ([]corev1.Secret, error) {
	var err error
	var sourceSecretList corev1.SecretList
	var clientListOptions []client.ListOption
	if namespace != "" {
		clientListOptions = append(clientListOptions, client.InNamespace(namespace))
	}
	if secretLabelType == "" {
		return nil, errors.New("empty 'SecretLabelType' provided. please provide 'Ignore' label type")
	}
	var listLabelOption client.ListOption
	if secretLabelType != IgnoreLabel {
		listLabelOption = client.MatchingLabels(map[string]string{SecretLabelTypeKey: string(secretLabelType)})
	} else {
		// if the 'secretLabelType' is asking to ignore, then
		// don't check the label value
		// just check whether the secret has the internal label key
		listLabelOption = client.HasLabels([]string{SecretLabelTypeKey})
	}
	clientListOptions = append(clientListOptions, listLabelOption)
	// find all the secrets with the provided internal label
	err = rc.List(ctx, &sourceSecretList, clientListOptions...)
	return sourceSecretList.Items, err
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

func FetchAllMirrorPeers(ctx context.Context, rc client.Client) ([]multiclusterv1alpha1.MirrorPeer, error) {
	var mirrorPeerListObj multiclusterv1alpha1.MirrorPeerList
	err := rc.List(ctx, &mirrorPeerListObj)
	if err != nil {
		return nil, err
	}
	return mirrorPeerListObj.Items, nil
}

func FetchMirrorPeerByName(ctx context.Context, rc client.Client, name string) (*multiclusterv1alpha1.MirrorPeer, error) {
	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := rc.Get(ctx, types.NamespacedName{Name: name}, &mirrorPeer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MirrorPeer %s: %w", name, err)
	}
	return &mirrorPeer, nil
}

func FetchSecretWithName(ctx context.Context, rc client.Client, secretName types.NamespacedName) (*corev1.Secret, error) {
	var secret corev1.Secret
	err := rc.Get(ctx, secretName, &secret)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func UnmarshalRookSecret(rookSecret *corev1.Secret) (*RookToken, error) {
	encodedData, err := base64.StdEncoding.DecodeString(string(rookSecret.Data[RookTokenKey]))
	if err != nil {
		return nil, err
	}

	var token RookToken
	err = json.Unmarshal(encodedData, &token)
	if err != nil {
		return nil, err
	}

	return &token, nil
}

func UnmarshalRookSecretExternal(rookSecret *corev1.Secret) (*RookTokenExternal, error) {
	fsid := string(rookSecret.Data["fsid"])

	cephUsername := string(rookSecret.Data["ceph-username"])

	cephSecret := string(rookSecret.Data["ceph-secret"])

	monSecret := string(rookSecret.Data["mon-secret"])

	token := RookTokenExternal{
		FSID:         string(fsid),
		CephSecret:   string(cephSecret),
		CephUsername: string(cephUsername),
		MonSecret:    string(monSecret),
	}

	return &token, nil
}

func UnmarshalHubSecret(hubSecret *corev1.Secret) (*RookToken, error) {
	var token HubToken
	err := json.Unmarshal(hubSecret.Data[SecretDataKey], &token)
	if err != nil {
		return nil, err
	}

	// Do an annoying conversion to RookToken
	// Double decoding

	tokenbyte, err := base64.StdEncoding.DecodeString(token.Token)
	if err != nil {
		return nil, err
	}

	actualtokenjsonbyte, err := base64.StdEncoding.DecodeString(string(tokenbyte))
	if err != nil {
		return nil, err
	}

	var actualtoken RookToken
	err = json.Unmarshal(actualtokenjsonbyte, &actualtoken)
	if err != nil {
		return nil, err
	}

	return &actualtoken, nil
}

func UnmarshalS3Secret(s3Secret *corev1.Secret) (*S3Token, error) {
	encodedData := s3Secret.Data[SecretDataKey]

	var token S3Token
	err := json.Unmarshal(encodedData, &token)
	if err != nil {
		return nil, err
	}

	aidbyte, err := base64.StdEncoding.DecodeString(token.AccessKeyID)
	if err != nil {
		return nil, err
	}
	token.AccessKeyID = string(aidbyte)

	skeybyte, err := base64.StdEncoding.DecodeString(token.SecretAccessKey)
	if err != nil {
		return nil, err
	}
	token.SecretAccessKey = string(skeybyte)

	s3pnbyte, err := base64.StdEncoding.DecodeString(token.S3ProfileName)
	if err != nil {
		return nil, err
	}
	token.S3ProfileName = string(s3pnbyte)

	s3cebyte, err := base64.StdEncoding.DecodeString(token.S3CompatibleEndpoint)
	if err != nil {
		return nil, err
	}
	token.S3CompatibleEndpoint = string(s3cebyte)

	s3rbyte, err := base64.StdEncoding.DecodeString(token.S3Region)
	if err != nil {
		return nil, err
	}
	token.S3Region = string(s3rbyte)

	s3bbyte, err := base64.StdEncoding.DecodeString(token.S3Bucket)
	if err != nil {
		return nil, err
	}
	token.S3Bucket = string(s3bbyte)

	return &token, nil
}

func GetSecretNameByPeerRef(pr multiclusterv1alpha1.PeerRef, prefix ...string) string {
	return CreateUniqueSecretName(pr.ClusterName, pr.StorageClusterRef.Namespace, pr.StorageClusterRef.Name, prefix...)
}
