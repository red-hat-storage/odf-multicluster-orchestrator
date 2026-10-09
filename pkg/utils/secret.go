package utils

import (
	"context"
	"errors"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretLabelType string

const (
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
	HubRecoveryLabel                                = "cluster.open-cluster-management.io/backup"
)

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
