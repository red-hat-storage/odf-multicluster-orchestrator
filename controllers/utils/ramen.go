package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	rmn "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

func updateS3ProfileFields(expected *rmn.S3StoreProfile, found *rmn.S3StoreProfile) {
	found.S3ProfileName = expected.S3ProfileName
	found.S3Bucket = expected.S3Bucket
	found.S3Region = expected.S3Region
	found.S3CompatibleEndpoint = expected.S3CompatibleEndpoint
	found.S3SecretRef.Name = expected.S3SecretRef.Name
	found.VeleroNamespaceSecretKeyRef = expected.VeleroNamespaceSecretKeyRef
	found.CACertificates = expected.CACertificates
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

func mergeCustomS3ProfileFields(current *rmn.S3StoreProfile, expected *rmn.S3StoreProfile) {
	if current.VeleroNamespaceSecretKeyRef != nil {
		expected.VeleroNamespaceSecretKeyRef = current.VeleroNamespaceSecretKeyRef
	}

	if current.CACertificates != nil {
		expected.CACertificates = current.CACertificates
	}
}

func UpdateRamenHubOperatorConfig(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, mirrorPeer *multiclusterv1alpha1.MirrorPeer, ramenHubNamespace string, logger *slog.Logger) error {
	logger.Info("Starting to update Ramen Hub Operator config", "SecretName", secret.Name, "Namespace", secret.Namespace)

	if _, ok := secret.Annotations[MirrorPeerNameAnnotationKey]; !ok {
		return fmt.Errorf("failed to find MirrorPeerName on secret")
	}

	mirrorPeerName := secret.Annotations[MirrorPeerNameAnnotationKey]
	if mirrorPeer.Name != mirrorPeerName {
		return fmt.Errorf("MirrorPeer %q not found", mirrorPeerName)
	}

	if !mirrorPeer.Spec.ManageS3 {
		logger.Info("Manage S3 is disabled on MirrorPeer spec, skipping update", "MirrorPeer", mirrorPeerName)
		return nil
	}

	expectedS3Profile := rmn.S3StoreProfile{
		S3ProfileName:        string(data[S3ProfileName]),
		S3Bucket:             string(data[S3BucketName]),
		S3Region:             string(data[S3Region]),
		S3CompatibleEndpoint: string(data[S3Endpoint]),
		S3SecretRef: corev1.SecretReference{
			Name: secret.Name,
		},
	}

	currentRamenConfigMap := corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Name:      RamenHubOperatorConfigName,
		Namespace: ramenHubNamespace,
	}
	err := rc.Get(ctx, namespacedName, &currentRamenConfigMap)
	if err != nil {
		logger.Error("Failed to fetch Ramen Hub Operator config map", "error", err, "ConfigMapName", namespacedName)
		return err
	}

	ramenConfigData, ok := currentRamenConfigMap.Data["ramen_manager_config.yaml"]
	if !ok {
		err = fmt.Errorf("DR hub operator config data is empty for the config %q in namespace %q", RamenHubOperatorConfigName, ramenHubNamespace)
		logger.Error("DR hub operator config data is missing", "error", err)
		return err
	}

	// Unmarshal into an unstructured map so that fields unknown to our
	// version of the RamenConfig struct are preserved across the
	// read-modify-write cycle.
	rawConfig := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(ramenConfigData), &rawConfig); err != nil {
		logger.Error("Failed to unmarshal DR hub operator config data", "error", err)
		return err
	}

	existingProfiles, err := extractS3Profiles(rawConfig)
	if err != nil {
		logger.Error("Failed to extract S3 profiles from config", "error", err)
		return err
	}

	isUpdated := false
	for i, currentS3Profile := range existingProfiles {
		if currentS3Profile.S3ProfileName == expectedS3Profile.S3ProfileName {
			mergeCustomS3ProfileFields(&currentS3Profile, &expectedS3Profile)
			if areS3ProfileFieldsEqual(expectedS3Profile, currentS3Profile) {
				logger.Info("No change detected in S3 profile, skipping update", "S3ProfileName", expectedS3Profile.S3ProfileName)
				return nil
			}
			updateS3ProfileFields(&expectedS3Profile, &existingProfiles[i])
			isUpdated = true
			logger.Info("S3 profile updated", "S3ProfileName", expectedS3Profile.S3ProfileName)
			break
		}
	}

	if !isUpdated {
		existingProfiles = append(existingProfiles, expectedS3Profile)
		logger.Info("New S3 profile added", "S3ProfileName", expectedS3Profile.S3ProfileName)
	}

	if err := setS3Profiles(rawConfig, existingProfiles); err != nil {
		logger.Error("Failed to set S3 profiles in config", "error", err)
		return err
	}

	ramenConfigDataStr, err := yaml.Marshal(rawConfig)
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

// extractS3Profiles reads the s3StoreProfiles key from an unstructured config
// map and returns typed S3StoreProfile values.
func extractS3Profiles(rawConfig map[string]interface{}) ([]rmn.S3StoreProfile, error) {
	raw, ok := rawConfig["s3StoreProfiles"]
	if !ok || raw == nil {
		return nil, nil
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal s3StoreProfiles to JSON: %w", err)
	}

	var profiles []rmn.S3StoreProfile
	if err := json.Unmarshal(jsonBytes, &profiles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal s3StoreProfiles from JSON: %w", err)
	}

	return profiles, nil
}

// setS3Profiles writes the given S3StoreProfile slice back into the
// unstructured config map under the s3StoreProfiles key.
func setS3Profiles(rawConfig map[string]interface{}, profiles []rmn.S3StoreProfile) error {
	jsonBytes, err := json.Marshal(profiles)
	if err != nil {
		return fmt.Errorf("failed to marshal s3StoreProfiles to JSON: %w", err)
	}

	var raw interface{}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return fmt.Errorf("failed to unmarshal s3StoreProfiles from JSON: %w", err)
	}

	rawConfig["s3StoreProfiles"] = raw
	return nil
}
