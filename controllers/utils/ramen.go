package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	rmn "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

func createOrUpdateRamenS3Secret(ctx context.Context, rc client.Client, scheme *runtime.Scheme, name string, data map[string][]byte, ramenHubNamespace string, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ramenHubNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, rc, &secret, func() error {
		secret.Labels = map[string]string{
			CreatedByLabelKey: MirrorPeerSecret,
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			AwsAccessKeyId:     data[AwsAccessKeyId],
			AwsSecretAccessKey: data[AwsSecretAccessKey],
		}
		return controllerutil.SetControllerReference(&mirrorPeer, &secret, scheme)
	})

	return err
}

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

func updateRamenHubOperatorConfig(ctx context.Context, rc client.Client, secret *corev1.Secret, data map[string][]byte, mirrorPeer multiclusterv1alpha1.MirrorPeer, ramenHubNamespace string, logger *slog.Logger) error {
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

	ramenConfig := rmn.RamenConfig{}
	err = yaml.Unmarshal([]byte(ramenConfigData), &ramenConfig)
	if err != nil {
		logger.Error("Failed to unmarshal DR hub operator config data", "error", err)
		return err
	}

	isUpdated := false
	for i, currentS3Profile := range ramenConfig.S3StoreProfiles {
		if currentS3Profile.S3ProfileName == expectedS3Profile.S3ProfileName {
			// Merge any existing veleroNamespaceSecretKeyRef and caCertificates to the S3StoreProfile.
			mergeCustomS3ProfileFields(&currentS3Profile, &expectedS3Profile)
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

func CreateOrUpdateSecretsFromInternalSecret(ctx context.Context, rc client.Client, scheme *runtime.Scheme, currentNamespace string, secret *corev1.Secret, mirrorPeer multiclusterv1alpha1.MirrorPeer, logger *slog.Logger) error {
	logger.Info("Validating internal secret", "SecretName", secret.Name, "Namespace", secret.Namespace)

	if err := ValidateInternalSecret(secret, InternalLabel); err != nil {
		logger.Error("Provided internal secret is not valid", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return err
	}

	data := make(map[string][]byte)
	if err := json.Unmarshal(secret.Data[SecretDataKey], &data); err != nil {
		logger.Error("Failed to unmarshal secret data", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return err
	}

	secretOrigin := string(secret.Data[SecretOriginKey])
	logger.Info("Processing secret based on origin", "Origin", secretOrigin, "SecretName", secret.Name)

	if secretOrigin == OriginMap["S3Origin"] {
		if ok := ValidateS3Secret(data); !ok {
			err := fmt.Errorf("invalid S3 secret format for secret name %q in namespace %q", secret.Name, secret.Namespace)
			logger.Error("Invalid S3 secret format", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
			return err
		}

		if err := createOrUpdateRamenS3Secret(ctx, rc, scheme, secret.Name, data, currentNamespace, mirrorPeer); err != nil {
			logger.Error("Failed to create or update Ramen S3 secret", "error", err, "SecretName", secret.Name, "Namespace", currentNamespace)
			return err
		}
		if err := updateRamenHubOperatorConfig(ctx, rc, secret, data, mirrorPeer, currentNamespace, logger); err != nil {
			logger.Error("Failed to update Ramen Hub Operator config", "error", err, "SecretName", secret.Name, "Namespace", currentNamespace)
			return err
		}
	}

	return nil
}
