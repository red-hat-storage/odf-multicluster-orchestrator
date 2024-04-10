package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"

	rmn "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// createOrUpdateDestinationSecretsFromSource updates all destination secrets
// associated with this source secret.
// If a list of MirrorPeer objects are provided, it will check
// the mapping from the provided MirrorPeers.
// If no MirrorPeers are provided, it will fetch all the MirrorPeers in the HUB.
func createOrUpdateDestinationSecretsFromSource(ctx context.Context, rc client.Client, sourceSecret *corev1.Secret, mirrorPeers ...multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	err := utils.ValidateSourceSecret(sourceSecret)
	if err != nil {
		logger.Error(err, "Updating secrets failed. Invalid secret type.", "secret", sourceSecret.Name, "namespace", sourceSecret.Namespace)
		return err
	}

	if mirrorPeers == nil {
		mirrorPeers, err = utils.FetchAllMirrorPeers(ctx, rc)
		if err != nil {
			logger.Error(err, "Unable to get the list of MirrorPeer objects")
			return err
		}
		logger.V(2).Info("Successfully got the list of MirrorPeers", "MirrorPeerListObj", mirrorPeers)
	}

	uniqueConnectedPeers, err := PeersConnectedToSecret(sourceSecret, mirrorPeers)
	if err != nil {
		logger.Error(err, "ConnectedPeers returned an error", "secret", sourceSecret.Name, "namespace", sourceSecret.Namespace, "mirrorpeers", mirrorPeers)
		return err
	}
	logger.V(2).Info("Listing all the Peers connected to the Source", "SourceSecret", sourceSecret.Name, "namespace", sourceSecret.Namespace, "connected-peers-length", len(uniqueConnectedPeers))

	// anyErr will have the last found error
	var anyErr error
	for _, eachConnectedPeer := range uniqueConnectedPeers {
		namedPeerRef := NewNamedPeerRefWithSecretData(sourceSecret, eachConnectedPeer)
		err := namedPeerRef.CreateOrUpdateDestinationSecret(ctx, rc)
		if err != nil {
			logger.Error(err, "Unable to update the destination secret", "secret", sourceSecret.Name, "namespace", sourceSecret.Namespace, "PeerRef", eachConnectedPeer)
			anyErr = err
		}
	}

	return anyErr
}

func processDestinationSecretUpdation(ctx context.Context, rc client.Client, destSecret *corev1.Secret) error {
	logger := log.FromContext(ctx)
	err := utils.ValidateDestinationSecret(destSecret)
	if err != nil {
		logger.Error(err, "Destination secret validation failed", "secret", destSecret.Name, "namespace", destSecret.Namespace)
		return err
	}
	mirrorPeers, err := utils.FetchAllMirrorPeers(ctx, rc)
	if err != nil {
		logger.Error(err, "Failed to get the list of MirrorPeer objects")
		return err
	}
	uniqueConnectedPeers, err := PeersConnectedToSecret(destSecret, mirrorPeers)
	if err != nil {
		logger.Error(err, "Failed to get the peers connected to the secret", "secret", destSecret.Name, "namespace", destSecret.Namespace)
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
			logger.Error(err, "Unexpected error while finding the source secret", "peer-ref", eachConnectedPeer, "secret", destSecret.Name, "namespace", destSecret.Namespace)
			return err
		}
		if utils.IsSecretSource(&connectedSecret) {
			connectedSource = connectedSecret.DeepCopy()
			break
		}
	}

	if connectedSource == nil {
		logger.Error(nil, "No connected source found. Removing the dangling destination secret", "secret", destSecret.Name, "namespace", destSecret.Namespace)
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
			logger.Error(err, "Failed to update destination secret", "secret", eachDSecret.Name, "namespace", eachDSecret.Namespace)
		}
	}
	return anyError
}

func createOrUpdateRamenS3Secret(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, ramenHubNamespace string) error {
	logger := log.FromContext(ctx)

	// convert aws s3 secret from s3 origin secret into ramen secret
	expectedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: ramenHubNamespace,
			Labels: map[string]string{
				utils.CreatedByLabelKey: utils.MirrorPeerSecret,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     data[utils.AwsAccessKeyId],
			utils.AwsSecretAccessKey: data[utils.AwsSecretAccessKey],
		},
	}

	localSecret := corev1.Secret{}
	namespacedName := types.NamespacedName{
		Name:      secret.Name,
		Namespace: ramenHubNamespace,
	}
	err := rc.Get(ctx, namespacedName, &localSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// creating new s3 secret on ramen openshift-dr-system namespace
			logger.Info("Creating a s3 secret", "secret", expectedSecret.Name, "namespace", expectedSecret.Namespace)
			return rc.Create(ctx, &expectedSecret)
		}
		logger.Error(err, "unable to fetch the s3 secret", "secret", secret.Name, "namespace", ramenHubNamespace)
		return err
	}

	if !reflect.DeepEqual(expectedSecret.Data, localSecret.Data) {
		// updating existing s3 secret on ramen openshift-dr-system namespace
		logger.Info("Updating the s3 secret", "secret", expectedSecret.Name, "namespace", ramenHubNamespace)
		_, err := controllerutil.CreateOrUpdate(ctx, rc, &localSecret, func() error {
			localSecret.Data = expectedSecret.Data
			return nil
		})
		return err
	}

	// no changes
	return nil
}

func createOrUpdateExternalSecret(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, namespace string) error {
	logger := log.FromContext(ctx)

	expectedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: namespace,
			Labels: map[string]string{
				utils.CreatedByLabelKey: utils.MirrorPeerSecret,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			// May add more parameters for external mode
			"fsid": data["fsid"],
		},
	}

	localSecret := corev1.Secret{}
	namespacedName := types.NamespacedName{
		Name:      secret.Name,
		Namespace: namespace,
	}
	err := rc.Get(ctx, namespacedName, &localSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Creating a external; secret", "secret", expectedSecret.Name, "namespace", expectedSecret.Namespace)
			return rc.Create(ctx, &expectedSecret)
		}
		logger.Error(err, "unable to fetch the external secret", "secret", secret.Name, "namespace", namespace)
		return err
	}

	if !reflect.DeepEqual(expectedSecret.Data, localSecret.Data) {
		logger.Info("Updating the external secret", "secret", expectedSecret.Name, "namespace", namespace)
		_, err := controllerutil.CreateOrUpdate(ctx, rc, &localSecret, func() error {
			localSecret.Data = expectedSecret.Data
			return nil
		})
		return err
	}

	return nil
}

func isS3ProfileManagedPeerRef(clusterPeerRef multiclusterv1alpha1.PeerRef, mirrorPeers []multiclusterv1alpha1.MirrorPeer) bool {
	for _, mirrorpeer := range mirrorPeers {
		for _, peerRef := range mirrorpeer.Spec.Items {
			if reflect.DeepEqual(clusterPeerRef, peerRef) && (mirrorpeer.Spec.ManageS3) {
				// found mirror peer with ManageS3 spec enabled
				return true
			}
		}
	}
	return false
}

func updateS3ProfileFields(expected *rmn.S3StoreProfile, found *rmn.S3StoreProfile) {
	found.S3ProfileName = expected.S3ProfileName
	found.S3Bucket = expected.S3Bucket
	found.S3Region = expected.S3Region
	found.S3CompatibleEndpoint = expected.S3CompatibleEndpoint
	found.S3SecretRef.Name = expected.S3SecretRef.Name
}

func areS3ProfileFieldsEqual(expected rmn.S3StoreProfile, found rmn.S3StoreProfile) bool {
	if expected.S3ProfileName != found.S3ProfileName {
		return false
	}

	if expected.S3Bucket != found.S3Bucket {
		return false
	}

	if expected.S3Region != found.S3Region {
		return false
	}

	if expected.S3CompatibleEndpoint != found.S3CompatibleEndpoint {
		return false
	}

	if expected.S3SecretRef.Name != found.S3SecretRef.Name {
		return false
	}

	return true
}

func updateRamenHubOperatorConfig(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, mirrorPeers []multiclusterv1alpha1.MirrorPeer, ramenHubNamespace string) error {
	logger := log.FromContext(ctx)

	clusterPeerRef, err := utils.CreatePeerRefFromSecret(secret)
	if err != nil {
		logger.Error(err, "unable to create peerref", "secret", secret.Name, "namespace", secret.Namespace)
		return err
	}
	if mirrorPeers == nil {
		mirrorPeers, err = utils.FetchAllMirrorPeers(ctx, rc)
	}
	if err != nil {
		logger.Error(err, "unable to get the list of MirrorPeer objects")
		return err
	}

	// filter mirror peer using clusterPeerRef
	if !isS3ProfileManagedPeerRef(clusterPeerRef, mirrorPeers) {
		// ManageS3 is disabled on MirrorPeer spec, skip ramen hub operator config update
		return nil
	}

	// converting s3 bucket config into ramen s3 profile
	expectedS3Profile := rmn.S3StoreProfile{
		S3ProfileName:        string(data[utils.S3ProfileName]),
		S3Bucket:             string(data[utils.S3BucketName]),
		S3Region:             string(data[utils.S3Region]),
		S3CompatibleEndpoint: string(data[utils.S3Endpoint]),
		// referenceing ramen secret
		S3SecretRef: corev1.SecretReference{
			Name: secret.Name,
		},
	}

	// fetch ramen hub operator configmap
	currentRamenConfigMap := corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Name:      utils.RamenHubOperatorConfigName,
		Namespace: ramenHubNamespace,
	}
	err = rc.Get(ctx, namespacedName, &currentRamenConfigMap)
	if err != nil {
		logger.Error(err, "unable to fetch DR hub operator config", "config", utils.RamenHubOperatorConfigName, "namespace", ramenHubNamespace)
		return err
	}

	// extract ramen manager config str from configmap
	ramenConfigData, ok := currentRamenConfigMap.Data["ramen_manager_config.yaml"]
	if !ok {
		return fmt.Errorf("DR hub operator config data is empty for the config %q in namespace %q", utils.RamenHubOperatorConfigName, ramenHubNamespace)
	}

	// converting ramen manager config str into RamenConfig
	ramenConfig := rmn.RamenConfig{}
	err = yaml.Unmarshal([]byte(ramenConfigData), &ramenConfig)
	if err != nil {
		logger.Error(err, "failed to unmarshal DR hub operator config data", "config", utils.RamenHubOperatorConfigName, "namespace", ramenHubNamespace)
		return err
	}

	isUpdated := false
	for i, currentS3Profile := range ramenConfig.S3StoreProfiles {
		if currentS3Profile.S3ProfileName == expectedS3Profile.S3ProfileName {

			if areS3ProfileFieldsEqual(expectedS3Profile, currentS3Profile) {
				// no change detected on already exiting s3 profile in RamenConfig
				return nil
			}
			// changes deducted on existing s3 profile
			updateS3ProfileFields(&expectedS3Profile, &ramenConfig.S3StoreProfiles[i])
			isUpdated = true
			break
		}
	}

	if !isUpdated {
		// new s3 profile is deducted
		ramenConfig.S3StoreProfiles = append(ramenConfig.S3StoreProfiles, expectedS3Profile)
	}

	// converting RamenConfig into ramen manager config str
	ramenConfigDataStr, err := yaml.Marshal(ramenConfig)
	if err != nil {
		logger.Error(err, "failed to marshal DR hub operator config data", "config", utils.RamenHubOperatorConfigName, "namespace", ramenHubNamespace)
		return err
	}

	// update ramen hub operator configmap
	logger.Info("Updating DR hub operator config with S3 profile", utils.RamenHubOperatorConfigName, expectedS3Profile.S3ProfileName)
	_, err = controllerutil.CreateOrUpdate(ctx, rc, &currentRamenConfigMap, func() error {
		// attach ramen manager config str into configmap
		currentRamenConfigMap.Data["ramen_manager_config.yaml"] = string(ramenConfigDataStr)
		return nil
	})

	return err
}

func createOrUpdateSecretsFromInternalSecret(ctx context.Context, rc client.Client, secret *corev1.Secret, mirrorPeers []multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)

	if err := utils.ValidateInternalSecret(secret, utils.InternalLabel); err != nil {
		logger.Error(err, "Provided internal secret is not valid", "secret", secret.Name, "namespace", secret.Namespace)
		return err
	}

	data := make(map[string][]byte)
	err := json.Unmarshal(secret.Data[utils.SecretDataKey], &data)
	if err != nil {
		logger.Error(err, "failed to unmarshal secret data", "secret", secret.Name, "namespace", secret.Namespace)
		return err
	}

	if string(secret.Data[utils.SecretOriginKey]) == utils.OriginMap["S3Origin"] {
		if ok := utils.ValidateS3Secret(data); !ok {
			return fmt.Errorf("invalid S3 secret format for secret name %q in namesapce %q", secret.Name, secret.Namespace)
		}

		currentNamespace := os.Getenv("POD_NAMESPACE")
		// S3 origin secret has two part 1. s3 bucket secret 2. s3 bucket config
		// create ramen s3 secret using s3 bucket secret
		err := createOrUpdateRamenS3Secret(ctx, rc, secret, data, currentNamespace)
		if err != nil {
			return err
		}
		// update ramen hub operator config using s3 bucket config and ramen s3 secret reference
		err = updateRamenHubOperatorConfig(ctx, rc, secret, data, mirrorPeers, currentNamespace)
		if err != nil {
			return err
		}
	}
	if string(secret.Data[utils.SecretOriginKey]) == utils.OriginMap["RookOrigin"] {
		currentNamespace := os.Getenv("POD_NAMESPACE")
		err := createOrUpdateExternalSecret(ctx, rc, secret, data, currentNamespace)
		if err != nil {
			return err
		}
	}

	return nil
}

// processDeletedSecrets finds out which type of secret is deleted
// and do appropriate action
func processDeletedSecrets(ctx context.Context, rc client.Client, req types.NamespacedName) error {
	var err error
	logger := log.FromContext(ctx, "controller", "MirrorPeerController")
	// get all the secrets with the same name
	allSecrets, err := utils.FetchAllSecretsWithLabel(ctx, rc, "", utils.IgnoreLabel)
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
			if secretType := eachSecret.Labels[utils.SecretLabelTypeKey]; secretType == string(utils.SourceLabel) {
				// if 'sourceSecretPointer' already points to a source secret,
				// it is an error. We should not have TWO source
				// secrets of same name.
				if sourceSecretPointer != nil {
					err = errors.New("multiple source secrets detected")
					logger.Error(err, "Cannot have more than one source secrets with the same name", "request", req, "source-secret", sourceSecretPointer.Name, "namespace", sourceSecretPointer.Namespace)
					return err
				}
				sourceSecretPointer = eachSecret.DeepCopy()
			} else {
				sameNamedDestinationSecrets = append(sameNamedDestinationSecrets, eachSecret)
			}
		}
	}

	logger.V(2).Info("List of secrets with requested name", "secret-name", req.Name, "secret-length", len(sameNamedDestinationSecrets))

	if sourceSecretPointer == nil {
		// if there is neither source secret nor any other similarly named secrets,
		// that means all 'req.Name'-ed secrets are cleaned up and nothing to be done
		if len(sameNamedDestinationSecrets) == 0 {
			return nil
		}
		logger.Info("A SOURCE secret deletion detected", "secret-name", req.Name, "namespace", req.Namespace)
		var anyErr error
		// if source secret is not present, remove all the destinations|GREENs
		for _, eachDestSecret := range sameNamedDestinationSecrets {
			err = rc.Delete(ctx, &eachDestSecret)
			if err != nil {
				logger.Error(err, "Deletion failed", "secret", eachDestSecret.Name, "namespace", eachDestSecret.Namespace)
				anyErr = err
			}
		}
		// if any error has happened,
		// we will return the last found error
		if anyErr != nil {
			return anyErr
		}
	} else {
		logger.Info("A DESTINATION secret deletion detected", "secret-name", req.Name, "namespace", req.Namespace)
		// in this section, one of the destination is removed
		// action: use the source secret pointed by 'sourceSecretPointer'
		// and restore the missing destination secret
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, sourceSecretPointer)
		if err != nil {
			logger.Error(err, "Unable to update the destination secret", "source-secret", sourceSecretPointer.Name, "namespace", sourceSecretPointer.Namespace)
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
				mapKey := utils.GetSecretNameByPeerRef(eachPeerRef)
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
	sourcePeerRef, err := utils.CreatePeerRefFromSecret(secret)
	if err != nil {
		return nil, err
	}
	return PeersConnectedToPeerRef(sourcePeerRef, mirrorPeers), nil
}

func fetchAllSourceSecrets(ctx context.Context, rc client.Client, namespace string) ([]corev1.Secret, error) {
	return utils.FetchAllSecretsWithLabel(ctx, rc, namespace, utils.SourceLabel)
}

func fetchAllDestinationSecrets(ctx context.Context, rc client.Client, namespace string) ([]corev1.Secret, error) {
	return utils.FetchAllSecretsWithLabel(ctx, rc, namespace, utils.DestinationLabel)
}
