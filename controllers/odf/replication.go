package odf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	StorageClusterPeerNameAnnotationKey = "ocs.openshift.io/storage-cluster-peer"
)

func CreateStorageClusterPeer(ctx context.Context, c client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) error {
	logger = logger.With("MirrorPeer", mirrorPeer.Name)
	items := mirrorPeer.Spec.Items
	clientInfo := make([]ClientInfo, 0)

	for _, item := range items {
		logger.Info("Fetching info for client", "ClientKey", utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		ci, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return err
		}
		logger.Info("Client Info found", "ClientInfo", ci)
		clientInfo = append(clientInfo, ci)
	}

	for i := range items {
		var storageClusterPeerName string
		var oppositeClient ClientInfo
		currentClient := clientInfo[i]
		// Provider A StorageClusterPeer contains info of Provider B endpoint and ticket, hence this
		if i == 0 {
			oppositeClient = clientInfo[1]
			storageClusterPeerName = getStorageClusterPeerName(oppositeClient.ProviderInfo.ProviderManagedClusterName)
		} else {
			oppositeClient = clientInfo[0]
			storageClusterPeerName = getStorageClusterPeerName(oppositeClient.ProviderInfo.ProviderManagedClusterName)
		}

		// Provider B's onboarding token will be used for Provider A's StorageClusterPeer
		logger.Info("Fetching onboarding ticket in with name and namespace", "Name", mirrorPeer.GetUID(), "Namespace", oppositeClient.ProviderInfo.ProviderManagedClusterName)
		onboardingToken, err := fetchOnboardingTicket(ctx, c, oppositeClient, mirrorPeer)
		if err != nil {
			return fmt.Errorf("failed to fetch onboarding token for provider %s. %w", oppositeClient.ProviderInfo.ProviderManagedClusterName, err)
		}

		apiEndpoint := oppositeClient.ProviderInfo.StorageProviderPublicEndpoint
		if apiEndpoint == "" {
			logger.Error("'StorageProviderPublicEndpoint' not found. Using 'StorageProviderEndpoint' instead. It might not be accessible externally.")
			apiEndpoint = oppositeClient.ProviderInfo.StorageProviderEndpoint
		}

		storageClusterPeer := ocsv1.StorageClusterPeer{
			TypeMeta: metav1.TypeMeta{
				Kind:       "StorageClusterPeer",
				APIVersion: ocsv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      storageClusterPeerName,
				Namespace: currentClient.ProviderInfo.NamespacedName.Namespace,
			},
			Spec: ocsv1.StorageClusterPeerSpec{
				OnboardingToken: onboardingToken,
				ApiEndpoint:     apiEndpoint,
			},
		}
		storageClusterPeerJson, err := json.Marshal(storageClusterPeer)
		if err != nil {
			logger.Error("Failed to marshal StorageClusterPeer to JSON", "StorageClusterPeer", storageClusterPeerName)
			return err
		}

		ownerRef := metav1.OwnerReference{
			APIVersion: mirrorPeer.APIVersion,
			Kind:       mirrorPeer.Kind,
			Name:       mirrorPeer.Name,
			UID:        mirrorPeer.UID,
		}

		manifestWorkName := fmt.Sprintf("storageclusterpeer-%s", currentClient.ProviderInfo.ProviderManagedClusterName)
		namespace := currentClient.ProviderInfo.ProviderManagedClusterName

		manifesConfigOption := []workv1.ManifestConfigOption{
			{
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:     ocsv1.GroupVersion.Group,
					Resource:  "storageclusterpeers",
					Name:      storageClusterPeer.Name,
					Namespace: storageClusterPeer.Namespace,
				},
				FeedbackRules: []workv1.FeedbackRule{
					{
						Type: workv1.JSONPathsType,
						JsonPaths: []workv1.JsonPath{
							{
								Name: "state",
								Path: ".status.state",
							},
						},
					},
				},
			},
		}
		operationResult, err := utils.CreateOrUpdateManifestWork(ctx, c, manifestWorkName, namespace, storageClusterPeerJson, manifesConfigOption, ownerRef)
		if err != nil {
			return err
		}

		logger.Info(fmt.Sprintf("ManifestWork was %s for StorageClusterPeer %s", operationResult, storageClusterPeerName))
	}

	return nil
}

func CreateManifestWorkForClusterPairingConfigMap(ctx context.Context, c client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) error {
	logger.Info("Starting to create ManifestWork for cluster pairing ConfigMap")

	logger.Info("Fetched client info ConfigMap successfully")
	items := mirrorPeer.Spec.Items

	ci1, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(items[0].ClusterName, items[0].StorageClusterRef.Name))
	if err != nil {
		logger.Error("Failed to get client info from ConfigMap for the first cluster")
		return err
	}

	logger.Info("Fetched client info for the first cluster", "ClientInfo", ci1)

	ci2, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(items[1].ClusterName, items[1].StorageClusterRef.Name))
	if err != nil {
		logger.Error("Failed to get client info from ConfigMap for the second cluster")
		return err
	}

	logger.Info("Fetched client info for the second cluster", "ClientInfo", ci2)
	logger.Info("Updating provider ConfigMap with client pairing", "ProviderClient1", ci1.ClientID, "PairedClient1", ci2.ClientID)
	if err := updateProviderConfigMap(logger, ctx, c, mirrorPeer, ci1, ci2); err != nil {
		return err
	}

	logger.Info("Updating provider ConfigMap with client pairing", "ProviderClient2", ci2.ClientID, "PairedClient2", ci1.ClientID)
	if err := updateProviderConfigMap(logger, ctx, c, mirrorPeer, ci2, ci1); err != nil {
		return err
	}

	logger.Info("Successfully created ManifestWork for cluster pairing ConfigMap")
	return nil
}

func updateProviderConfigMap(logger *slog.Logger, ctx context.Context, c client.Client, mirrorPeer *multiclusterv1alpha1.MirrorPeer, providerClientInfo ClientInfo, pairedClientInfo ClientInfo) error {
	providerName := providerClientInfo.ProviderInfo.ProviderManagedClusterName
	manifestWorkName := "storage-client-mapping"
	manifestWorkNamespace := providerName

	logger.Info("Fetching existing ManifestWork for provider", "Namespace", manifestWorkNamespace)
	manifestWork, err := utils.GetManifestWork(ctx, c, manifestWorkName, manifestWorkNamespace)
	var configMap *corev1.ConfigMap

	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("ManifestWork not found; creating a new ConfigMap")
			configMap = &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-client-mapping",
					Namespace: providerClientInfo.ProviderInfo.NamespacedName.Namespace,
					Annotations: map[string]string{
						StorageClusterPeerNameAnnotationKey: getStorageClusterPeerName(pairedClientInfo.ProviderInfo.ProviderManagedClusterName),
					},
				},
				Data: make(map[string]string),
			}
		} else {
			return fmt.Errorf("failed to get ManifestWork: %w", err)
		}
	} else {
		logger.Info("Found existing ManifestWork, decoding ConfigMap")
		if len(manifestWork.Spec.Workload.Manifests) == 0 {
			return fmt.Errorf("ManifestWork %s has no manifests", manifestWorkName)
		}
		objJson := manifestWork.Spec.Workload.Manifests[0].RawExtension.Raw
		configMap, err = utils.DecodeConfigMap(objJson)
		if err != nil {
			return fmt.Errorf("failed to decode ConfigMap: %w", err)
		}
	}

	logger.Info("Updating ConfigMap with paired client info", "ProviderClientID", providerClientInfo.ClientID, "PairedClientID", pairedClientInfo.ClientID)
	configMap.Data[providerClientInfo.ClientID] = pairedClientInfo.ClientID

	updatedObjJson, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal updated ConfigMap: %w", err)
	}

	ownerRef := metav1.OwnerReference{
		APIVersion: mirrorPeer.APIVersion,
		Kind:       mirrorPeer.Kind,
		Name:       mirrorPeer.Name,
		UID:        mirrorPeer.UID,
	}

	logger.Info("Creating or updating ManifestWork with updated ConfigMap")
	_, err = utils.CreateOrUpdateManifestWork(ctx, c, manifestWorkName, manifestWorkNamespace, updatedObjJson, []workv1.ManifestConfigOption{}, ownerRef)
	if err != nil {
		return fmt.Errorf("failed to update ManifestWork for provider %s: %w", providerName, err)
	}

	logger.Info("Successfully updated ManifestWork for provider", "ProviderName", providerName)
	return nil
}

func fetchOnboardingTicket(ctx context.Context, c client.Client, clientInfo ClientInfo, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (string, error) {
	secretName := string(mirrorPeer.GetUID())
	secretNamespace := clientInfo.ProviderInfo.ProviderManagedClusterName
	tokenSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, tokenSecret); err != nil {
		if k8serrors.IsNotFound(err) {
			return "", fmt.Errorf("secret %s not found in namespace %s", secretName, secretNamespace)
		}
		return "", fmt.Errorf("failed to fetch secret %s in namespace %s", secretName, secretNamespace)
	}

	tokenData, exists := tokenSecret.Data[utils.SecretDataKey]
	if !exists {
		return "", fmt.Errorf("token data not found in secret %s", secretName)
	}
	return string(tokenData), nil
}

func getStorageClusterPeerName(providerClusterName string) string {
	return fmt.Sprintf("%s-peer", providerClusterName)
}

func GetNamespacedNameForClientS3Secret(pr multiclusterv1alpha1.PeerRef, mp *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (string, string, error) {
	ci, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(pr.ClusterName, pr.StorageClusterRef.Name))
	if err != nil {
		return "", "", err
	}
	providerManagedClusterName := ci.ProviderInfo.ProviderManagedClusterName
	pr1 := mp.Spec.Items[0]
	pr2 := mp.Spec.Items[1]
	s3SecretName := utils.CreateUniqueSecretNameForClient(providerManagedClusterName, utils.GetKey(pr1.ClusterName, pr1.StorageClusterRef.Name), utils.GetKey(pr2.ClusterName, pr2.StorageClusterRef.Name))
	s3SecretNamespace := providerManagedClusterName

	return s3SecretName, s3SecretNamespace, nil
}

func IsProviderModePeeringDone(ctx context.Context, c client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error) {
	isStorageClusterPeerManifestWorkCreated, err := checkStorageClusterPeerStatus(ctx, c, logger, mirrorPeer, clientInfoMap)
	if err != nil {
		logger.Error("failed to check if StorageClusterPeer have been created")
		return false, err
	}

	logger.Info("StorageClusterPeer manifest work creation status", "isStorageClusterPeerManifestWorkCreated", isStorageClusterPeerManifestWorkCreated)
	isClientPairingConfigMapCreated, err := checkClientPairingConfigMapStatus(ctx, c, logger, mirrorPeer, clientInfoMap)
	if err != nil {
		logger.Error("failed to check if client pair config map has been created")
		return false, err
	}

	logger.Info("Client pairing ConfigMap creation status", "isClientPairingConfigMapCreated", isClientPairingConfigMapCreated)

	allChecksPassed := isStorageClusterPeerManifestWorkCreated &&
		isClientPairingConfigMapCreated

	logger.Info("Provider mode peering status", "AllChecksPassed", allChecksPassed)
	return allChecksPassed, nil
}

func checkStorageClusterPeerStatus(ctx context.Context, c client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error) {
	logger.Info("Checking if StorageClusterPeer ManifestWorks have been created and reached Peered status")

	items := mirrorPeer.Spec.Items
	clientInfos := make([]ClientInfo, 0, len(items))
	for _, item := range items {
		clientKey := utils.GetKey(item.ClusterName, item.StorageClusterRef.Name)
		ci, err := GetClientInfoFromConfigMap(clientInfoMap, clientKey)
		if err != nil {
			logger.Error("Failed to get client info from ConfigMap", "ClientKey", clientKey)
			return false, err
		}
		clientInfos = append(clientInfos, ci)
	}

	for _, currentClient := range clientInfos {
		manifestWorkName := fmt.Sprintf("storageclusterpeer-%s", currentClient.ProviderInfo.ProviderManagedClusterName)
		manifestWorkNamespace := currentClient.ProviderInfo.ProviderManagedClusterName

		manifestWork := &workv1.ManifestWork{}
		err := c.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: manifestWorkNamespace}, manifestWork)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManifestWork for StorageClusterPeer not found; it may not be created yet", "ManifestWorkName", manifestWorkName)
				return false, nil
			}
			return false, fmt.Errorf("failed to get ManifestWork for StorageClusterPeer: %w", err)
		}

		applied := false
		for _, condition := range manifestWork.Status.Conditions {
			if condition.Type == workv1.WorkApplied && condition.Status == metav1.ConditionTrue {
				applied = true
				break
			}
		}

		if !applied {
			logger.Info("StorageClusterPeer ManifestWork has not reached Applied status", "ManifestWorkName", manifestWorkName)
			return false, nil
		}
		logger.Info("StorageClusterPeer ManifestWork has reached Applied status", "ManifestWorkName", manifestWorkName)

		mwResourceStatusManifests := manifestWork.Status.ResourceStatus.Manifests
		if len(mwResourceStatusManifests) > 0 {
			if *mwResourceStatusManifests[0].StatusFeedbacks.Values[0].Value.String != string(ocsv1.StorageClusterPeerStatePeered) {
				logger.Info("StorageClusterPeer has not reached Peered status", "ManifestWorkName", manifestWorkName)
				return false, nil
			}
		} else {
			logger.Info("StorageClusterPeer ManifestWork has not been updated with resource status yet", "ManifestWorkName", manifestWorkName)
			return false, nil
		}
		logger.Info("StorageClusterPeer has reached Peered status", "ManifestWorkName", manifestWorkName)
	}

	logger.Info("All StorageClusterPeer ManifestWorks have been created and reached Peered status")
	return true, nil
}

func checkClientPairingConfigMapStatus(ctx context.Context, c client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error) {
	logger.Info("Checking if client pairing ConfigMap ManifestWorks have been created and reached Applied status")

	items := mirrorPeer.Spec.Items
	clientInfos := make([]ClientInfo, 0, len(items))
	for _, item := range items {
		clientKey := utils.GetKey(item.ClusterName, item.StorageClusterRef.Name)
		ci, err := GetClientInfoFromConfigMap(clientInfoMap, clientKey)
		if err != nil {
			logger.Error("Failed to get client info from ConfigMap", "ClientKey", clientKey)
			return false, err
		}
		clientInfos = append(clientInfos, ci)
	}

	for _, providerClient := range clientInfos {
		manifestWorkName := "storage-client-mapping"
		manifestWorkNamespace := providerClient.ProviderInfo.ProviderManagedClusterName

		manifestWork := &workv1.ManifestWork{}
		err := c.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: manifestWorkNamespace}, manifestWork)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManifestWork for client pairing ConfigMap not found; it may not be created yet",
					"ManifestWorkName", manifestWorkName, "Namespace", manifestWorkNamespace)
				return false, nil
			}
			return false, fmt.Errorf("failed to get ManifestWork for client pairing ConfigMap: %w", err)
		}

		applied := false
		for _, condition := range manifestWork.Status.Conditions {
			if condition.Type == workv1.WorkApplied && condition.Status == metav1.ConditionTrue {
				applied = true
				break
			}
		}

		if !applied {
			logger.Info("Client pairing ConfigMap ManifestWork has not reached Applied status",
				"ManifestWorkName", manifestWorkName, "Namespace", manifestWorkNamespace)
			return false, nil
		}

		logger.Info("Client pairing ConfigMap ManifestWork has reached Applied status",
			"ManifestWorkName", manifestWorkName, "Namespace", manifestWorkNamespace)
	}

	logger.Info("All client pairing ConfigMap ManifestWorks have been created and reached Applied status")
	return true, nil
}
