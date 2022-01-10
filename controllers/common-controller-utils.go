package controllers

import (
	"context"
	"errors"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createOrUpdateDestinationSecretsFromSource updates all destination secrets
// associated with this source secret.
// If a list of MirrorPeer objects are provided, it will check
// the mapping from the provided MirrorPeers.
// If no MirrorPeers are provided, it will fetch all the MirrorPeers in the HUB.
func createOrUpdateDestinationSecretsFromSource(ctx context.Context, rc client.Client, sourceSecret *corev1.Secret, mirrorPeers ...multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	err := common.ValidateSourceSecret(sourceSecret)
	if err != nil {
		logger.Error(err, "Updating secrets failed. Invalid secret type.", "secret", sourceSecret)
		return err
	}

	if mirrorPeers == nil {
		mirrorPeers, err = common.FetchAllMirrorPeers(ctx, rc)
		if err != nil {
			logger.Error(err, "Unable to get the list of MirrorPeer objects")
			return err
		}
		logger.V(2).Info("Successfully got the list of MirrorPeers", "MirrorPeerListObj", mirrorPeers)
	}

	uniqueConnectedPeers, err := PeersConnectedToSecret(sourceSecret, mirrorPeers)
	if err != nil {
		logger.Error(err, "ConnectedPeers returned an error", "secret", sourceSecret, "mirrorpeers", mirrorPeers)
		return err
	}
	logger.V(2).Info("Listing all the Peers connected to the Source", "SourceSecret", sourceSecret, "#connected-peers", len(uniqueConnectedPeers))

	// anyErr will have the last found error
	var anyErr error
	for _, eachConnectedPeer := range uniqueConnectedPeers {
		namedPeerRef := NewNamedPeerRefWithSecretData(sourceSecret, eachConnectedPeer)
		err := namedPeerRef.CreateOrUpdateDestinationSecret(ctx, rc)
		if err != nil {
			logger.Error(err, "Unable to update the destination secret", "PeerRef", eachConnectedPeer)
			anyErr = err
		}
	}

	return anyErr
}

func processDestinationSecretUpdation(ctx context.Context, rc client.Client, destSecret *corev1.Secret) error {
	logger := log.FromContext(ctx)
	err := common.ValidateDestinationSecret(destSecret)
	if err != nil {
		logger.Error(err, "Destination secret validation failed", "secret", destSecret)
		return err
	}
	mirrorPeers, err := common.FetchAllMirrorPeers(ctx, rc)
	if err != nil {
		logger.Error(err, "Failed to get the list of MirrorPeer objects")
		return err
	}
	uniqueConnectedPeers, err := PeersConnectedToSecret(destSecret, mirrorPeers)
	if err != nil {
		logger.Error(err, "Failed to get the peers connected to the secret", "secret", destSecret)
		return err
	}
	var connectedSource *corev1.Secret
	for _, eachConnectedPeer := range uniqueConnectedPeers {
		var connectedSecret corev1.Secret
		nPeerRef := NewNamedPeerRefWithSecretData(destSecret, eachConnectedPeer)
		err := nPeerRef.GetAssociatedSecret(ctx, rc, &connectedSecret)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			logger.Error(err, "Unexpected error while finding the source secret", "peer-ref", eachConnectedPeer, "secret", destSecret)
			return err
		}
		if common.IsSecretSource(&connectedSecret) {
			connectedSource = connectedSecret.DeepCopy()
			break
		}
	}

	if connectedSource == nil {
		logger.Error(nil, "No connected source found. Removing the dangling destination secret", "secret", destSecret)
		err = rc.Delete(ctx, destSecret)
		return err
	}
	err = createOrUpdateDestinationSecretsFromSource(ctx, rc, connectedSource, mirrorPeers...)
	return err
}

func processDestinationSecretCleanup(ctx context.Context, rc client.Client) error {
	logger := log.FromContext(ctx)
	allDestinationSecrets, err := fetchAllDestinationSecrets(ctx, rc, "")
	if err != nil {
		logger.Error(err, "Unable to get all the destination secrets")
		return err
	}
	var anyError error
	for _, eachDSecret := range allDestinationSecrets {
		err = processDestinationSecretUpdation(ctx, rc, &eachDSecret)
		if err != nil {
			anyError = err
			logger.Error(err, "Failed to update destination secret", "secret", eachDSecret)
		}
	}
	return anyError
}

// processDeletedSecrets finds out which type of secret is deleted
// and do appropriate action
func processDeletedSecrets(ctx context.Context, rc client.Client, req types.NamespacedName) error {
	var err error
	logger := log.FromContext(ctx, "controller", "MirrorPeerController")
	// get all the secrets with the same name
	allSecrets, err := fetchAllInternalSecrets(ctx, rc, "", common.IgnoreLabel)
	if err != nil {
		logger.Error(err, "Unable to get the list of secrets")
		return err
	}
	// sameNamedDestinationSecrets will collect the same named destination secrets
	var sameNamedDestinationSecrets []corev1.Secret
	// sourceSecretPointer will point to the source secret
	var sourceSecretPointer *corev1.Secret
	// append all the secrets which have the same requested name
	for _, eachSecret := range allSecrets {
		if eachSecret.Name == req.Name {
			// check similarly named secret is present (or not)
			if secretType := eachSecret.Labels[common.SecretLabelTypeKey]; secretType == string(common.SourceLabel) {
				// if 'sourceSecretPointer' already points to a source secret,
				// it is an error. We should not have TWO source
				// secrets of same name.
				if sourceSecretPointer != nil {
					err = errors.New("multiple source secrets detected")
					logger.Error(err, "Cannot have more than one source secrets with the same name", "request", req, "source-secret", *sourceSecretPointer)
					return err
				}
				sourceSecretPointer = eachSecret.DeepCopy()
			} else {
				sameNamedDestinationSecrets = append(sameNamedDestinationSecrets, eachSecret)
			}
		}
	}

	logger.V(2).Info("List of secrets with requested name", "secret-name", req.Name, "secretlist", sameNamedDestinationSecrets, "#secrets", len(sameNamedDestinationSecrets))

	if sourceSecretPointer == nil {
		// if there is neither source secret nor any other similarly named secrets,
		// that means all 'req.Name'-ed secrets are cleaned up and nothing to be done
		if len(sameNamedDestinationSecrets) == 0 {
			return nil
		}
		logger.Info("A SOURCE secret deletion detected", "secret-name", req.Name)
		var anyErr error
		// if source secret is not present, remove all the destinations|GREENs
		for _, eachDestSecret := range sameNamedDestinationSecrets {
			err = rc.Delete(ctx, &eachDestSecret)
			if err != nil {
				logger.Error(err, "Deletion failed", "secret", eachDestSecret)
				anyErr = err
			}
		}
		// if any error has happened,
		// we will return the last found error
		if anyErr != nil {
			return anyErr
		}
	} else {
		logger.Info("A DESTINATION secret deletion detected", "secret-name", req.Name)
		// in this section, one of the destination is removed
		// action: use the source secret pointed by 'sourceSecretPointer'
		// and restore the missing destination secret
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, sourceSecretPointer)
		if err != nil {
			logger.Error(err, "Unable to update the destination secret", "source-secret", sourceSecretPointer)
			return err
		}
	}

	return nil
}

// peersConnectedFromPeerRefList returns all the peers associated with the source peer item
func peersConnectedFromPeerRefList(sourcePeerRef multiclusterv1alpha1.PeerRef, allPeerItems []multiclusterv1alpha1.PeerRef) []multiclusterv1alpha1.PeerRef {
	var connectedPeers []multiclusterv1alpha1.PeerRef
	var itemsContainsSource bool
	for _, eachPeerRef := range allPeerItems {
		// checks whether the provided 'sourcePeerItem' is in the list
		if reflect.DeepEqual(sourcePeerRef, eachPeerRef) {
			itemsContainsSource = true
			continue
		}
		connectedPeers = append(connectedPeers, eachPeerRef)
	}
	// when the source PeerRef is in the list,
	// then only return the 'connectedPeers'
	if itemsContainsSource {
		return connectedPeers
	}
	return nil
}

// PeersConnectedToPeerRef returns a list of PeerRefs connected to the 'sourcePeerRef'.
func PeersConnectedToPeerRef(sourcePeerRef multiclusterv1alpha1.PeerRef, mirrorPeers []multiclusterv1alpha1.MirrorPeer) []multiclusterv1alpha1.PeerRef {
	uniquePeerRefMap := make(map[string]multiclusterv1alpha1.PeerRef)
	for _, eachMirrorPeerObj := range mirrorPeers {
		connectedPeers := peersConnectedFromPeerRefList(sourcePeerRef, eachMirrorPeerObj.Spec.Items)
		if len(connectedPeers) > 0 {
			// add the PeerRef s into a map, to keep only the unique ones
			for _, eachPeerRef := range connectedPeers {
				mapKey := common.CreateUniqueName(eachPeerRef.ClusterName, eachPeerRef.StorageClusterRef.Namespace, eachPeerRef.StorageClusterRef.Name)
				uniquePeerRefMap[mapKey] = eachPeerRef
			}
		}
	}
	// extract the unique 'PeerRef' s
	var uniquePeerRefs []multiclusterv1alpha1.PeerRef
	for _, eachPeerRef := range uniquePeerRefMap {
		uniquePeerRefs = append(uniquePeerRefs, eachPeerRef)
	}
	return uniquePeerRefs
}

// PeersConnectedToSecret return unique PeerRefs associated with the secret
func PeersConnectedToSecret(secret *corev1.Secret, mirrorPeers []multiclusterv1alpha1.MirrorPeer) ([]multiclusterv1alpha1.PeerRef, error) {
	// create a PeerRef object from the given source secret
	sourcePeerRef, err := common.CreatePeerRefFromSecret(secret)
	if err != nil {
		return nil, err
	}
	return PeersConnectedToPeerRef(sourcePeerRef, mirrorPeers), nil
}

// fetchAllInternalSecrets will get all the internal secrets in the namespace and with the provided label
// if the namespace is empty, it will fetch from all the namespaces
// if the label type is 'Ignore', it will fetch all the internal secrets (both source and destination)
func fetchAllInternalSecrets(ctx context.Context, rc client.Client, namespace string, secretLabelType common.SecretLabelType) ([]corev1.Secret, error) {
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
	if secretLabelType != common.IgnoreLabel {
		listLabelOption = client.MatchingLabels(map[string]string{common.SecretLabelTypeKey: string(secretLabelType)})
	} else {
		// if the 'secretLabelType' is asking to ignore, then
		// don't check the label value
		// just check whether the secret has the internal label key
		listLabelOption = client.HasLabels([]string{common.SecretLabelTypeKey})
	}
	clientListOptions = append(clientListOptions, listLabelOption)
	// find all the secrets with the provided internal label
	err = rc.List(ctx, &sourceSecretList, clientListOptions...)
	return sourceSecretList.Items, err
}

func fetchAllSourceSecrets(ctx context.Context, rc client.Client, namespace string) ([]corev1.Secret, error) {
	return fetchAllInternalSecrets(ctx, rc, namespace, common.SourceLabel)
}

func fetchAllDestinationSecrets(ctx context.Context, rc client.Client, namespace string) ([]corev1.Secret, error) {
	return fetchAllInternalSecrets(ctx, rc, namespace, common.DestinationLabel)
}
