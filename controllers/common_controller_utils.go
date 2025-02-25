package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"sigs.k8s.io/yaml"
)

// createOrUpdateDestinationSecretsFromSource updates all destination secrets
// associated with this source secret.
// If a list of MirrorPeer objects are provided, it will check
// the mapping from the provided MirrorPeers.
// If no MirrorPeers are provided, it will fetch all the MirrorPeers in the HUB.
func createOrUpdateDestinationSecretsFromSource(ctx context.Context, rc client.Client, sourceSecret *corev1.Secret, logger *slog.Logger, mirrorPeers ...multiclusterv1alpha1.MirrorPeer) error {
	logger.Info("Validating source secret", "Secret", sourceSecret.Name, "Namespace", sourceSecret.Namespace)

	err := utils.ValidateSourceSecret(sourceSecret)
	if err != nil {
		logger.Error("Updating secrets failed due to invalid source secret type", "error", err, "Secret", sourceSecret.Name, "Namespace", sourceSecret.Namespace)
		return err
	}

	if mirrorPeers == nil {
		logger.Info("Fetching all MirrorPeer objects as none were provided")
		mirrorPeers, err = utils.FetchAllMirrorPeers(ctx, rc)
		if err != nil {
			logger.Error("Unable to retrieve the list of MirrorPeer objects", "error", err)
			return err
		}
		logger.Info("Successfully retrieved the list of MirrorPeers", "Count", len(mirrorPeers))
	}

	logger.Info("Determining peers connected to the source secret", "Secret", sourceSecret.Name)
	uniqueConnectedPeers, err := PeersConnectedToSecret(sourceSecret, mirrorPeers)
	if err != nil {
		logger.Error("Error determining connected peers for the source secret", "error", err, "Secret", sourceSecret.Name, "Namespace", sourceSecret.Namespace)
		return err
	}
	logger.Info("Peers connected to the source secret identified", "ConnectedPeersCount", len(uniqueConnectedPeers))

	var anyErr error
	for _, eachConnectedPeer := range uniqueConnectedPeers {
		logger.Info("Creating or updating destination secret for peer", "PeerRef", eachConnectedPeer)
		namedPeerRef := NewNamedPeerRefWithSecretData(sourceSecret, eachConnectedPeer)
		err := namedPeerRef.CreateOrUpdateDestinationSecret(ctx, rc, logger)
		if err != nil {
			logger.Error("Failed to create or update destination secret", "error", err, "SourceSecret", sourceSecret.Name, "DestinationPeerRef", eachConnectedPeer)
			anyErr = err
		}
	}

	if anyErr != nil {
		logger.Error("One or more errors occurred while updating destination secrets", "lastError", anyErr)
	} else {
		logger.Info("All destination secrets updated successfully")
	}

	return anyErr
}

func createOrUpdateRamenS3Secret(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, ramenHubNamespace string, logger *slog.Logger) error {

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

	logger = logger.With("SecretName", expectedSecret.Name, "SecretNamespace", expectedSecret.Namespace, "RamenHubNamespace", ramenHubNamespace)

	err := rc.Get(ctx, namespacedName, &localSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Creating a new S3 secret")
			if createErr := rc.Create(ctx, &expectedSecret); createErr != nil {
				logger.Error("Failed to create the S3 secret", "error", createErr)
				return createErr
			}
			return nil
		}
		logger.Error("Failed to fetch the S3 secret", "error", err)
		return err
	}

	if !reflect.DeepEqual(expectedSecret.Data, localSecret.Data) {
		logger.Info("Updating the existing S3 secret")
		_, updateErr := controllerutil.CreateOrUpdate(ctx, rc, &localSecret, func() error {
			localSecret.Data = expectedSecret.Data
			return nil
		})
		if updateErr != nil {
			logger.Error("Failed to update the S3 secret", "error", updateErr)
			return updateErr
		}
	} else {
		logger.Info("No updates required for the S3 secret")
	}

	return nil
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

func updateRamenHubOperatorConfig(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, mirrorPeers []multiclusterv1alpha1.MirrorPeer, ramenHubNamespace string, logger *slog.Logger) error {
	logger.Info("Starting to update Ramen Hub Operator config", "SecretName", secret.Name, "Namespace", secret.Namespace)

	if mirrorPeers == nil {
		var err error
		mirrorPeers, err = utils.FetchAllMirrorPeers(ctx, rc)
		if err != nil {
			logger.Error("Failed to fetch all MirrorPeers", "error", err)
			return err
		}
	}

	if _, ok := secret.Annotations[utils.MirrorPeerNameAnnotationKey]; !ok {
		return fmt.Errorf("failed to find MirrorPeerName on secret")
	}

	mirrorPeerName := secret.Annotations[utils.MirrorPeerNameAnnotationKey]
	var foundMirrorPeer *multiclusterv1alpha1.MirrorPeer
	for _, mp := range mirrorPeers {
		if mp.Name == mirrorPeerName {
			foundMirrorPeer = &mp
			break
		}
	}

	if foundMirrorPeer == nil {
		return fmt.Errorf("MirrorPeer %q not found", mirrorPeerName)
	}

	if !foundMirrorPeer.Spec.ManageS3 {
		logger.Info("Manage S3 is disabled on MirrorPeer spec, skipping update", "MirrorPeer", mirrorPeerName)
		return nil
	}

	expectedS3Profile := rmn.S3StoreProfile{
		S3ProfileName:        string(data[utils.S3ProfileName]),
		S3Bucket:             string(data[utils.S3BucketName]),
		S3Region:             string(data[utils.S3Region]),
		S3CompatibleEndpoint: string(data[utils.S3Endpoint]),
		S3SecretRef: corev1.SecretReference{
			Name: secret.Name,
		},
	}

	currentRamenConfigMap := corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Name:      utils.RamenHubOperatorConfigName,
		Namespace: ramenHubNamespace,
	}
	err := rc.Get(ctx, namespacedName, &currentRamenConfigMap)
	if err != nil {
		logger.Error("Failed to fetch Ramen Hub Operator config map", "error", err, "ConfigMapName", namespacedName)
		return err
	}

	ramenConfigData, ok := currentRamenConfigMap.Data["ramen_manager_config.yaml"]
	if !ok {
		err = fmt.Errorf("DR hub operator config data is empty for the config %q in namespace %q", utils.RamenHubOperatorConfigName, ramenHubNamespace)
		logger.Error("DR hub operator config data is missing", "error", err)
		return err
	}

	ramenConfig := rmn.RamenConfig{}
	err = yaml.Unmarshal([]byte(ramenConfigData), &ramenConfig)
	if err != nil {
		logger.Error("Failed to unmarshal DR hub operator config data", "error", err)
		return err
	}

	isUpdated := false
	for i, currentS3Profile := range ramenConfig.S3StoreProfiles {
		if currentS3Profile.S3ProfileName == expectedS3Profile.S3ProfileName {
			if areS3ProfileFieldsEqual(expectedS3Profile, currentS3Profile) {
				logger.Info("No change detected in S3 profile, skipping update", "S3ProfileName", expectedS3Profile.S3ProfileName)
				return nil
			}
			updateS3ProfileFields(&expectedS3Profile, &ramenConfig.S3StoreProfiles[i])
			isUpdated = true
			logger.Info("S3 profile updated", "S3ProfileName", expectedS3Profile.S3ProfileName)
			break
		}
	}

	if !isUpdated {
		ramenConfig.S3StoreProfiles = append(ramenConfig.S3StoreProfiles, expectedS3Profile)
		logger.Info("New S3 profile added", "S3ProfileName", expectedS3Profile.S3ProfileName)
	}

	ramenConfigDataStr, err := yaml.Marshal(ramenConfig)
	if err != nil {
		logger.Error("Failed to marshal Ramen config", "error", err)
		return err
	}

	_, err = controllerutil.CreateOrUpdate(ctx, rc, &currentRamenConfigMap, func() error {
		currentRamenConfigMap.Data["ramen_manager_config.yaml"] = string(ramenConfigDataStr)
		return nil
	})
	if err != nil {
		logger.Error("Failed to update Ramen Hub Operator config map", "error", err)
		return err
	}

	logger.Info("Ramen Hub Operator config updated successfully", "ConfigMapName", namespacedName)
	return nil
}

func createOrUpdateSecretsFromInternalSecret(ctx context.Context, rc client.Client, secret *corev1.Secret, mirrorPeers []multiclusterv1alpha1.MirrorPeer, logger *slog.Logger) error {
	logger.Info("Validating internal secret", "SecretName", secret.Name, "Namespace", secret.Namespace)

	if err := utils.ValidateInternalSecret(secret, utils.InternalLabel); err != nil {
		logger.Error("Provided internal secret is not valid", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return err
	}

	data := make(map[string][]byte)
	if err := json.Unmarshal(secret.Data[utils.SecretDataKey], &data); err != nil {
		logger.Error("Failed to unmarshal secret data", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return err
	}

	secretOrigin := string(secret.Data[utils.SecretOriginKey])
	logger.Info("Processing secret based on origin", "Origin", secretOrigin, "SecretName", secret.Name)

	if secretOrigin == utils.OriginMap["S3Origin"] {
		if ok := utils.ValidateS3Secret(data); !ok {
			err := fmt.Errorf("invalid S3 secret format for secret name %q in namespace %q", secret.Name, secret.Namespace)
			logger.Error("Invalid S3 secret format", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
			return err
		}

		currentNamespace := os.Getenv("POD_NAMESPACE")
		if err := createOrUpdateRamenS3Secret(ctx, rc, secret, data, currentNamespace, logger); err != nil {
			logger.Error("Failed to create or update Ramen S3 secret", "error", err, "SecretName", secret.Name, "Namespace", currentNamespace)
			return err
		}
		if err := updateRamenHubOperatorConfig(ctx, rc, secret, data, mirrorPeers, currentNamespace, logger); err != nil {
			logger.Error("Failed to update Ramen Hub Operator config", "error", err, "SecretName", secret.Name, "Namespace", currentNamespace)
			return err
		}
	}

	return nil
}

// processDeletedSecrets finds out which type of secret is deleted
// and do appropriate action
func processDeletedSecrets(ctx context.Context, rc client.Client, req types.NamespacedName, logger *slog.Logger) error {
	logger.Info("Processing deleted secrets", "SecretName", req.Name, "Namespace", req.Namespace)

	// Fetch all secrets with a specific label, excluding those with the IgnoreLabel
	allSecrets, err := utils.FetchAllSecretsWithLabel(ctx, rc, "", utils.IgnoreLabel)
	if err != nil {
		logger.Error("Failed to fetch secrets", "error", err)
		return err
	}

	var sameNamedDestinationSecrets []corev1.Secret
	var sourceSecretPointer *corev1.Secret

	// Filter secrets by name and type
	for _, eachSecret := range allSecrets {
		if eachSecret.Name == req.Name {
			if eachSecret.Labels[utils.SecretLabelTypeKey] == string(utils.SourceLabel) {
				if sourceSecretPointer != nil {
					err = errors.New("multiple source secrets detected with the same name")
					logger.Error("Multiple source secrets found", "error", err, "SecretName", req.Name, "Namespace", req.Namespace)
					return err
				}
				sourceSecretPointer = eachSecret.DeepCopy()
			} else {
				sameNamedDestinationSecrets = append(sameNamedDestinationSecrets, eachSecret)
			}
		}
	}

	if sourceSecretPointer == nil {
		if len(sameNamedDestinationSecrets) == 0 {
			logger.Info("No related secrets need cleaning", "SecretName", req.Name)
			return nil
		}
		logger.Info("Source secret deletion detected, cleaning up destination secrets", "SecretName", req.Name)
		var anyErr error
		for _, eachDestSecret := range sameNamedDestinationSecrets {
			err = rc.Delete(ctx, &eachDestSecret)
			if err != nil {
				logger.Error("Failed to delete destination secret", "error", err, "SecretName", eachDestSecret.Name, "Namespace", eachDestSecret.Namespace)
				anyErr = err
			}
		}
		return anyErr
	} else {
		logger.Info("Destination secret deletion detected, attempting to restore", "SecretName", req.Name)
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, sourceSecretPointer, logger)
		if err != nil {
			logger.Error("Failed to update destination secret from source", "error", err, "SourceSecretName", sourceSecretPointer.Name, "Namespace", sourceSecretPointer.Namespace)
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
