package utils

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretLabelType string

const (
	InternalLabel                   SecretLabelType = "INTERNAL"
	IgnoreLabel                     SecretLabelType = "IGNORE"
	ProviderLabel                   SecretLabelType = "PROVIDER"
	SecretLabelTypeKey                              = "multicluster.odf.openshift.io/secret-type"
	CreatedByLabelKey                               = "multicluster.odf.openshift.io/created-by"
	ObjectKindLabelKey                              = "multicluster.odf.openshift.io/object-kind"
	CreatedForClientID                              = "multicluster.odf.openshift.io/client-id"
	CreatorMulticlusterOrchestrator                 = "odf-multicluster-orchestrator"
	NamespaceKey                                    = "namespace"
	StorageClusterNameKey                           = "storage-cluster-name"
	SecretDataKey                                   = "secret-data"
	SecretOriginKey                                 = "secret-origin"
	MirrorPeerSecret                                = "mirrorpeersecret"
	HubRecoveryLabel                                = "cluster.open-cluster-management.io/backup"
)

type S3Token struct {
	AccessKeyID          string `json:"AWS_ACCESS_KEY_ID"`
	SecretAccessKey      string `json:"AWS_SECRET_ACCESS_KEY"`
	S3Bucket             string `json:"s3Bucket"`
	S3CompatibleEndpoint string `json:"s3CompatibleEndpoint"`
	S3ProfileName        string `json:"s3ProfileName"`
	S3Region             string `json:"s3Region"`
}

var OriginMap = map[string]string{"RookOrigin": "rook", "S3Origin": "S3"}

// IsSecretInternal returns true if the provided object has an Internal label
func IsSecretInternal(obj client.Object) bool {
	return obj.GetLabels()[SecretLabelTypeKey] == string(InternalLabel)
}

func ValidateInternalSecret(internalSecret *corev1.Secret, expectedLabel SecretLabelType) error {
	if internalSecret == nil {
		return errors.New("provided secret is 'nil'")
	}
	if expectedLabel == "" {
		return errors.New("an empty expected label provided. please provide 'IgnoreLabel' instead")
	}
	if expectedLabel != IgnoreLabel {
		if expectedLabel != SecretLabelType(internalSecret.Labels[SecretLabelTypeKey]) {
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

	// nolint:staticcheck
	if !(namespaceKeyOk && secretDataKeyOk && secretOriginKeyOk && scNameKeyOk) {
		return errors.New("expected data map keys are not present")
	}
	return nil
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

func FetchAllMirrorPeers(ctx context.Context, rc client.Client) ([]multiclusterv1alpha1.MirrorPeer, error) {
	var mirrorPeerListObj multiclusterv1alpha1.MirrorPeerList
	err := rc.List(ctx, &mirrorPeerListObj)
	if err != nil {
		return nil, err
	}
	return mirrorPeerListObj.Items, nil
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
