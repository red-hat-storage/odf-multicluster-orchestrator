/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package odf

import (
	"context"
	"encoding/json"
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"log/slog"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

const (
	StorageClusterPeerNameAnnotationKey = "ocs.openshift.io/storage-cluster-peer"
	ManagedClusterAddOn                 = "ManagedClusterAddOn"
	AddonApiVersion                     = "addon.open-cluster-management.io/v1alpha1"
)

type ODFVendor struct {
	client           client.Client
	scheme           *runtime.Scheme
	logger           *slog.Logger
	currentNamespace string
	infoMap          map[string]string
}

type ManagedClusterAddonConfig struct {
	// Namespace on the managedCluster where it will be deployed
	InstallNamespace string

	// Name of the MCA
	Name string

	// Namespace on the hub where MCA will be created, it represents the Managed cluster where the addons will be deployed
	Namespace string
}

func NewODFVendor(ctx context.Context, cl client.Client, scheme *runtime.Scheme, logger *slog.Logger, currentNamespace string) (*ODFVendor, error) {
	clientInfoConfigMap, err := GetClientInfoConfigMap(ctx, cl, currentNamespace)
	if err != nil {
		return nil, err
	}
	return &ODFVendor{
		client:           cl,
		scheme:           scheme,
		logger:           logger.With("storageVendor", "odf"),
		currentNamespace: currentNamespace,
		infoMap:          clientInfoConfigMap.Data,
	}, nil
}

func (odf *ODFVendor) ValidateVersion(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	for i := range mirrorPeer.Spec.Items {
		if err := isVersionCompatible(mirrorPeer.Spec.Items[i], odf.infoMap); err != nil {
			odf.logger.Error("ODF version incompatibility detected", "error", err)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (odf *ODFVendor) ConfigureStorageForDR(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	if err := odf.processManagedClusterAddon(ctx, mirrorPeer); err != nil {
		odf.logger.Error("Failed to process managedclusteraddon", "error", err)
		mirrorPeer.Status.Message = multiclusterv1alpha1.ManagedClusterAddOnFailed
		utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonManagedClusterAddonFailed)
		return ctrl.Result{}, err
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		if err := createStorageClusterPeer(ctx, odf.client, odf.logger, mirrorPeer, odf.infoMap); err != nil {
			odf.logger.Error("Failed to create StorageClusterPeer", "error", err)
			mirrorPeer.Status.Message = multiclusterv1alpha1.PeeringFailed
			utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonPeeringFailed)
			return ctrl.Result{}, err
		}

		if err := createManifestWorkForClusterPairingConfigMap(ctx, odf.client, odf.logger, mirrorPeer, odf.infoMap); err != nil {
			odf.logger.Error("Failed to create ManifestWork for ClusterPairingConfigMap", "error", err)
			mirrorPeer.Status.Message = multiclusterv1alpha1.PeeringFailed
			utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonPeeringFailed)
			return ctrl.Result{}, err
		}

		providerModePeeringDone, err := isProviderModePeeringDone(ctx, odf.client, odf.logger, mirrorPeer, odf.infoMap)
		if err != nil {
			mirrorPeer.Status.Message = multiclusterv1alpha1.PeeringFailed
			utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, err.Error(), multiclusterv1alpha1.ReasonPeeringFailed)
			return ctrl.Result{}, fmt.Errorf("failed to check if provider mode peering is correctly done %w", err)
		}

		if !providerModePeeringDone {
			mirrorPeer.Status.Message = multiclusterv1alpha1.PeeringInProgress
			utils.SetConfiguredFalseCondition(&mirrorPeer.Status.Conditions, mirrorPeer.Generation, multiclusterv1alpha1.MessagePeeringInProgress, multiclusterv1alpha1.ReasonPeeringInProgress)
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}
	return ctrl.Result{}, nil
}

func (odf *ODFVendor) CleanupResources(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		if err := odf.removeClientIDFromClusterPairingConfigMap(ctx, mirrorPeer); err != nil {
			odf.logger.Error("Failed to remove clientID from cluster pairing ConfigMap", "error", err)
			return reconcile.Result{}, err
		}
	}

	if err := odf.deleteManagedClusterAddon(ctx, mirrorPeer); err != nil {
		odf.logger.Error("Failed to delete ManagedClusterAddon", "error", err)
		return reconcile.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (odf *ODFVendor) getConfig(mp *multiclusterv1alpha1.MirrorPeer) ([]ManagedClusterAddonConfig, error) {
	managedClusterAddonsConfig := make([]ManagedClusterAddonConfig, 0)

	// Check if the MirrorPeer contains StorageClient reference
	hasStorageClientRef, err := IsStorageClientType(mp, odf.infoMap)
	if err != nil {
		return []ManagedClusterAddonConfig{}, err
	}

	if hasStorageClientRef {
		for _, item := range mp.Spec.Items {
			clientName := item.StorageClusterRef.Name
			clientInfo, err := GetClientInfoFromConfigMap(odf.infoMap, utils.GetKey(item.ClusterName, clientName))
			if err != nil {
				return []ManagedClusterAddonConfig{}, err
			}
			config := ManagedClusterAddonConfig{
				Name:             utils.TokenExchangeName,
				Namespace:        clientInfo.ProviderInfo.ProviderManagedClusterName,
				InstallNamespace: clientInfo.ProviderInfo.NamespacedName.Namespace,
			}
			managedClusterAddonsConfig = append(managedClusterAddonsConfig, config)
		}
	} else {
		for _, item := range mp.Spec.Items {
			managedClusterAddonsConfig = append(managedClusterAddonsConfig, ManagedClusterAddonConfig{
				Name:             utils.TokenExchangeName,
				Namespace:        item.ClusterName,
				InstallNamespace: item.StorageClusterRef.Namespace,
			})
		}
	}
	return managedClusterAddonsConfig, nil
}

// processManagedClusterAddon creates an addon for the cluster management in all the peer refs,
// the resources gets an owner ref of the mirrorpeer to let the garbage collector handle it if the mirrorpeer gets deleted
func (odf *ODFVendor) processManagedClusterAddon(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	odf.logger.Info("Processing ManagedClusterAddons for MirrorPeer")

	clusterManagementAddOn := &addonapiv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.TokenExchangeName,
		},
		Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
			AddOnMeta: addonapiv1alpha1.AddOnMeta{
				DisplayName: utils.TokenExchangeName,
				Description: utils.TokenExchangeName,
			},
			InstallStrategy: addonapiv1alpha1.InstallStrategy{
				Type: "Manual",
			},
		},
	}

	odf.logger.Info("Creating ClusterManagementAddOn for ManagedClusterAddons", "Name", clusterManagementAddOn.Name)
	if _, err := controllerutil.CreateOrUpdate(ctx, odf.client, clusterManagementAddOn, func() error {
		utils.AddAnnotation(clusterManagementAddOn, utils.AddonVersionAnnotationKey, version.Version)
		utils.AddAnnotation(clusterManagementAddOn, utils.HubOperatorNamespaceKey, odf.currentNamespace)
		utils.AddLabel(clusterManagementAddOn, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)

		return controllerutil.SetOwnerReference(mirrorPeer, clusterManagementAddOn, odf.scheme)
	}); err != nil {
		odf.logger.Error("Failed to reconcile ClusterManagementAddOn", "ClusterManagementAddOn", clusterManagementAddOn.Name, "error", err)
		return err
	}

	addonConfigs, err := odf.getConfig(mirrorPeer)
	if err != nil {
		return fmt.Errorf("failed to get managedclusteraddon config %w", err)
	}

	for _, config := range addonConfigs {
		odf.logger.Info("Handling ManagedClusterAddon for cluster", "ClusterName", config.Namespace)

		managedClusterAddOn := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.TokenExchangeName,
				Namespace: config.Namespace,
			},
		}

		odf.logger.Info("Installing agents on the namespace", "InstallNamespace", config.InstallNamespace)
		if _, err = controllerutil.CreateOrUpdate(ctx, odf.client, managedClusterAddOn, func() error {
			utils.AddAnnotation(managedClusterAddOn, utils.AddonVersionAnnotationKey, version.Version)
			utils.AddAnnotation(managedClusterAddOn, utils.HubOperatorNamespaceKey, odf.currentNamespace)

			utils.AddLabel(managedClusterAddOn, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
			managedClusterAddOn.Spec.InstallNamespace = config.InstallNamespace
			return controllerutil.SetOwnerReference(mirrorPeer, managedClusterAddOn, odf.scheme)
		}); err != nil {
			odf.logger.Error("Failed to reconcile ManagedClusterAddOn", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name), "error", err)
			return err
		}
	}

	// Wait for ManagedClusterAddon to be available
	for _, config := range addonConfigs {
		managedClusterAddOn := &addonapiv1alpha1.ManagedClusterAddOn{}
		managedClusterAddOn.Name = config.Name
		managedClusterAddOn.Namespace = config.Namespace

		if err := odf.client.Get(ctx, client.ObjectKeyFromObject(managedClusterAddOn), managedClusterAddOn); err != nil {
			return err
		}

		for _, condition := range managedClusterAddOn.Status.Conditions {
			if condition.Type == addonapiv1alpha1.ManagedClusterAddOnConditionAvailable {
				if condition.Status != metav1.ConditionTrue {
					return fmt.Errorf("failed while waiting for ManagedClusterAddon %q to be available: not available yet", managedClusterAddOn.Name)
				}
				break
			}
		}
	}
	odf.logger.Info("Successfully processed all ManagedClusterAddons for MirrorPeer")
	return nil
}

func (odf *ODFVendor) removeClientIDFromClusterPairingConfigMap(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	for _, item := range mirrorPeer.Spec.Items {
		ci, err := GetClientInfoFromConfigMap(odf.infoMap, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return err
		}

		providerName := ci.ProviderInfo.ProviderManagedClusterName
		manifestWorkName := "storage-client-mapping"

		manifestWork, err := utils.GetManifestWork(ctx, odf.client, manifestWorkName, providerName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				odf.logger.Info("ManifestWork not found, skipping clientID removal", "Provider", providerName)
				continue
			}
			return fmt.Errorf("failed to get ManifestWork for provider %s: %w", providerName, err)
		}

		if len(manifestWork.Spec.Workload.Manifests) == 0 {
			continue
		}

		configMap, err := utils.DecodeConfigMap(manifestWork.Spec.Workload.Manifests[0].RawExtension.Raw)
		if err != nil {
			return fmt.Errorf("failed to decode ConfigMap from ManifestWork for provider %s: %w", providerName, err)
		}

		delete(configMap.Data, ci.ClientID)
		odf.logger.Info("Removed clientID from cluster pairing ConfigMap", "ClientID", ci.ClientID, "Provider", providerName)

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

		_, err = utils.CreateOrUpdateManifestWork(ctx, odf.client, manifestWorkName, providerName, updatedObjJson, []workv1.ManifestConfigOption{}, ownerRef)
		if err != nil {
			return fmt.Errorf("failed to update ManifestWork for provider %s: %w", providerName, err)
		}
	}

	return nil
}

func (odf *ODFVendor) deleteManagedClusterAddon(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	addonConfigs, err := odf.getConfig(mirrorPeer)
	if err != nil {
		return fmt.Errorf("failed to get managedclusteraddon config %w", err)
	}

	for _, config := range addonConfigs {
		managedClusterAddOn := &addonapiv1alpha1.ManagedClusterAddOn{}
		managedClusterAddOn.Name = config.Name
		managedClusterAddOn.Namespace = config.Namespace

		if err := odf.client.Get(ctx, client.ObjectKeyFromObject(managedClusterAddOn), managedClusterAddOn); err != nil {
			return err
		}

		refs := []metav1.OwnerReference{
			{
				APIVersion:         AddonApiVersion,
				Kind:               ManagedClusterAddOn,
				Name:               mirrorPeer.GetName(),
				UID:                mirrorPeer.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		}

		found, err := controllerutil.HasOwnerReference(refs, managedClusterAddOn, odf.scheme)
		if err != nil {
			return err
		}

		if found {
			err = controllerutil.RemoveOwnerReference(mirrorPeer, managedClusterAddOn, odf.scheme)
			if err != nil {
				return err
			}
		}

		if err = odf.client.Get(ctx, client.ObjectKeyFromObject(managedClusterAddOn), managedClusterAddOn); err != nil {
			return err
		}

		if len(managedClusterAddOn.GetOwnerReferences()) == 0 {
			if err = odf.client.Delete(ctx, managedClusterAddOn); err != nil {
				return err
			}
		}
	}

	return nil
}

func createStorageClusterPeer(ctx context.Context, client client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) error {
	logger = logger.With("MirrorPeer", mirrorPeer.Name)
	items := mirrorPeer.Spec.Items
	clientInfo := make([]ClientInfo, 0)

	for _, item := range items {
		logger.Info("Fetching info for client", "ClientKey", utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		ci, err := GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return err
		}
		logger.Info("client Info found", "ClientInfo", ci)
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
		onboardingToken, err := fetchOnboardingTicket(ctx, client, oppositeClient, mirrorPeer)
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
				Name: storageClusterPeerName,
				// This provider A namespace on which the Storage object exists
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

		// ManifestWork created for Provider A will be called storageclusterpeer-{ProviderA} since that is where Manifests will be applied
		// Provider names are unique hence only 1 ManifestWork per ProviderCluster
		manifestWorkName := fmt.Sprintf("storageclusterpeer-%s", currentClient.ProviderInfo.ProviderManagedClusterName)

		// The namespace of Provider A is where this ManifestWork will be created on the hub
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
		operationResult, err := utils.CreateOrUpdateManifestWork(ctx, client, manifestWorkName, namespace, storageClusterPeerJson, manifesConfigOption, ownerRef)
		if err != nil {
			return err
		}

		logger.Info(fmt.Sprintf("ManifestWork was %s for StorageClusterPeer %s", operationResult, storageClusterPeerName))
	}

	return nil
}

func fetchOnboardingTicket(ctx context.Context, client client.Client, clientInfo ClientInfo, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (string, error) {
	secretName := string(mirrorPeer.GetUID())
	secretNamespace := clientInfo.ProviderInfo.ProviderManagedClusterName
	tokenSecret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, tokenSecret); err != nil {
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
	// Provider A will have SCP named {ProviderB}-peer
	return fmt.Sprintf("%s-peer", providerClusterName)
}

func createManifestWorkForClusterPairingConfigMap(ctx context.Context, client client.Client, logger *slog.Logger, mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) error {
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
	if err := updateProviderConfigMap(logger, ctx, client, mirrorPeer, ci1, ci2); err != nil {
		return err
	}

	logger.Info("Updating provider ConfigMap with client pairing", "ProviderClient2", ci2.ClientID, "PairedClient2", ci1.ClientID)
	if err := updateProviderConfigMap(logger, ctx, client, mirrorPeer, ci2, ci1); err != nil {
		return err
	}

	logger.Info("Successfully created ManifestWork for cluster pairing ConfigMap")
	return nil
}

// updateProviderConfigMap updates the ConfigMap on the provider with the new client pairing
func updateProviderConfigMap(logger *slog.Logger, ctx context.Context, client client.Client, mirrorPeer *multiclusterv1alpha1.MirrorPeer, providerClientInfo ClientInfo, pairedClientInfo ClientInfo) error {
	providerName := providerClientInfo.ProviderInfo.ProviderManagedClusterName
	manifestWorkName := "storage-client-mapping"
	manifestWorkNamespace := providerName

	logger.Info("Fetching existing ManifestWork for provider", "Namespace", manifestWorkNamespace)
	manifestWork, err := utils.GetManifestWork(ctx, client, manifestWorkName, manifestWorkNamespace)
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
	_, err = utils.CreateOrUpdateManifestWork(ctx, client, manifestWorkName, manifestWorkNamespace, updatedObjJson, []workv1.ManifestConfigOption{}, ownerRef)
	if err != nil {
		return fmt.Errorf("failed to update ManifestWork for provider %s: %w", providerName, err)
	}

	logger.Info("Successfully updated ManifestWork for provider", "ProviderName", providerName)
	return nil
}
