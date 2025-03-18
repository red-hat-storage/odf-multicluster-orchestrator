/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	addons "github.com/red-hat-storage/odf-multicluster-orchestrator/addons"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger

	testEnvFile      string
	CurrentNamespace string
}

const (
	mirrorPeerFinalizer         = "hub.multicluster.odf.openshift.io"
	spokeClusterRoleBindingName = "spoke-clusterrole-bindings"
	AddonVersionAnnotationKey   = "multicluster.odf.openshift.io/version"
)

//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods;secrets;configmaps;events,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=*
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons;clustermanagementaddons,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=*
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=ramendr.openshift.io,resources=drclusters;drpolicies,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete;watch,resourceNames=spoke-clusterrole-bindings

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Reconciling request")
	// Fetch MirrorPeer for given Request
	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := r.Get(ctx, req.NamespacedName, &mirrorPeer)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error("Failed to get MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, r.Client, r.CurrentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("ConfigMap 'odf-client-info' not found. Requeueing.")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Validating MirrorPeer")
	// Validate MirrorPeer
	// MirrorPeer.Spec must be defined
	if err := undefinedMirrorPeerSpec(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec is undefined", "error", err)
		return ctrl.Result{Requeue: false}, err
	}
	// MirrorPeer.Spec.Items must be unique
	if err := uniqueSpecItems(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec items are not unique", "error", err)
		return ctrl.Result{Requeue: false}, err
	}
	for i := range mirrorPeer.Spec.Items {
		// MirrorPeer.Spec.Items must not have empty fields
		if err := emptySpecItems(mirrorPeer.Spec.Items[i]); err != nil {
			logger.Error("MirrorPeer spec items have empty fields", "error", err)
			return reconcile.Result{Requeue: false}, err
		}
		// MirrorPeer.Spec.Items[*].ClusterName must be a valid ManagedCluster
		if err := isManagedCluster(ctx, r.Client, mirrorPeer.Spec.Items[i].ClusterName); err != nil {
			logger.Error("Invalid ManagedCluster", "ClusterName", mirrorPeer.Spec.Items[i].ClusterName, "error", err)
			return ctrl.Result{}, err
		}
		// MirrorPeer.Spec.Items[*].StorageClusterRef must have a compatible version
		if err := isVersionCompatible(mirrorPeer.Spec.Items[i], clientInfoMap.Data); err != nil {
			logger.Error("Can not reconcile MirrorPeer", "error", err)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.IncompatibleVersion
			mirrorPeer.Status.Message = err.Error()
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer. Requeing request.", "Error ", statusErr)
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		} else {
			if mirrorPeer.Status.Phase == multiclusterv1alpha1.IncompatibleVersion {
				mirrorPeer.Status.Phase = ""
				mirrorPeer.Status.Message = ""
			}
		}
	}
	logger.Info("All validations for MirrorPeer passed")

	if mirrorPeer.GetDeletionTimestamp().IsZero() {
		if !utils.ContainsString(mirrorPeer.GetFinalizers(), mirrorPeerFinalizer) {
			logger.Info("Finalizer not found on MirrorPeer. Adding Finalizer")
			mirrorPeer.Finalizers = append(mirrorPeer.Finalizers, mirrorPeerFinalizer)
		}
	} else {
		logger.Info("Deleting MirrorPeer")
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Deleting
		statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
		if statusErr != nil {
			logger.Error("Error occurred while updating the status of mirrorpeer", "Error", statusErr)
			return ctrl.Result{Requeue: true}, nil
		}
		if utils.ContainsString(mirrorPeer.GetFinalizers(), mirrorPeerFinalizer) {
			if utils.ContainsSuffix(mirrorPeer.GetFinalizers(), addons.SpokeMirrorPeerFinalizer) {
				logger.Info("Waiting for agent to delete resources")
				return reconcile.Result{Requeue: true}, err
			}
			if err := r.deleteSecrets(ctx, mirrorPeer); err != nil {
				logger.Error("Failed to delete resources", "error", err)
				return reconcile.Result{Requeue: true}, err
			}
			mirrorPeer.Finalizers = utils.RemoveString(mirrorPeer.Finalizers, mirrorPeerFinalizer)
			if err := r.Client.Update(ctx, &mirrorPeer); err != nil {
				logger.Error("Failed to remove finalizer from MirrorPeer", "error", err)
				return reconcile.Result{}, err
			}
		}
		logger.Info("MirrorPeer deleted, skipping reconcilation")
		return reconcile.Result{}, nil
	}

	mirrorPeerCopy := mirrorPeer.DeepCopy()
	if mirrorPeerCopy.Labels == nil {
		mirrorPeerCopy.Labels = make(map[string]string)
	}

	if val, ok := mirrorPeerCopy.Labels[utils.HubRecoveryLabel]; !ok || val != "resource" {
		logger.Info("Adding label to mirrorpeer for disaster recovery")
		mirrorPeerCopy.Labels[utils.HubRecoveryLabel] = "resource"
		err = r.Client.Update(ctx, mirrorPeerCopy)

		if err != nil {
			logger.Error("Failed to update mirrorpeer with disaster recovery label", "error", err)
			return checkK8sUpdateErrors(err, mirrorPeerCopy, logger)
		}
		logger.Info("Successfully added label to mirrorpeer for disaster recovery. Requeing request...")
		return ctrl.Result{Requeue: true}, nil
	}

	if mirrorPeer.Status.Phase == "" {
		if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
			mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangingSecret
		} else {
			mirrorPeer.Status.Phase = multiclusterv1alpha1.S3ProfileSyncing
		}
		statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
		if statusErr != nil {
			logger.Error("Error occurred while updating the status of mirrorpeer. Requeing request.", "Error ", statusErr)
			// Requeue, but don't throw
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Check if the MirrorPeer contains StorageClient reference
	hasStorageClientRef, err := utils.IsStorageClientType(ctx, r.Client, mirrorPeer, false)
	if err != nil {
		logger.Error("Failed to determine if MirrorPeer contains StorageClient reference", "error", err)
		return ctrl.Result{}, err
	}

	if err := r.processManagedClusterAddon(ctx, mirrorPeer); err != nil {
		logger.Error("Failed to process managedclusteraddon", "error", err)
		return ctrl.Result{}, err
	}

	err = r.createClusterRoleBindingsForSpoke(ctx, mirrorPeer)
	if err != nil {
		logger.Error("Failed to create cluster role bindings for spoke", "error", err)
		return ctrl.Result{}, err
	}

	// update s3 profile when MirrorPeer changes
	if mirrorPeer.Spec.ManageS3 {
		for _, peerRef := range mirrorPeer.Spec.Items {
			var s3Secret corev1.Secret
			var secretName string

			var namespace string
			if hasStorageClientRef {
				s3SecretName, s3SecretNamespace, err := GetNamespacedNameForClientS3Secret(ctx, r.Client, r.CurrentNamespace, peerRef, &mirrorPeer)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to get namespace for s3 secret %w", err)
				}
				secretName = s3SecretName
				namespace = s3SecretNamespace
			} else {
				secretName = utils.GetSecretNameByPeerRef(peerRef, utils.S3ProfilePrefix)
				namespace = peerRef.ClusterName
			}
			namespacedName := types.NamespacedName{
				Name:      secretName,
				Namespace: namespace,
			}
			err = r.Client.Get(ctx, namespacedName, &s3Secret)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					logger.Info("S3 secret is not yet synchronised. retrying till it is available. Requeing request...", "Secret Name", secretName, "Namespace/Cluster", namespace)
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error("Error in fetching s3 internal secret", "Cluster", peerRef.ClusterName, "error", err)
				return ctrl.Result{}, err
			}
			err = createOrUpdateSecretsFromInternalSecret(ctx, r.Client, r.CurrentNamespace, &s3Secret, []multiclusterv1alpha1.MirrorPeer{mirrorPeer}, logger)
			if err != nil {
				logger.Error("Error in updating S3 profile", "Cluster", peerRef.ClusterName, "error", err)
				return ctrl.Result{}, err
			}
		}
	}

	err = r.createDRClusters(ctx, &mirrorPeer, hasStorageClientRef)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Secret not synchronised yet, retrying to create DRCluster", "MirrorPeer", mirrorPeer.Name)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error("Failed to create DRClusters for MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	if hasStorageClientRef {
		result, err := createStorageClusterPeer(ctx, r.Client, logger, r.CurrentNamespace, mirrorPeer)
		if err != nil {
			logger.Error("Failed to create StorageClusterPeer", "error", err)
			return result, err
		}

		result, err = createManifestWorkForClusterPairingConfigMap(ctx, r.Client, logger, r.CurrentNamespace, mirrorPeer)
		if err != nil {
			logger.Error("Failed to create ManifestWork for ClusterPairingConfigMap", "error", err)
			return result, err
		}
	}
	return r.updateMirrorPeerStatus(ctx, mirrorPeer, hasStorageClientRef)
}

func createManifestWorkForClusterPairingConfigMap(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	logger.Info("Starting to create ManifestWork for cluster pairing ConfigMap")

	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Client info ConfigMap not found; requeuing for later retry")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Fetched client info ConfigMap successfully")
	items := mirrorPeer.Spec.Items

	ci1, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(items[0].ClusterName, items[0].StorageClusterRef.Name))
	if err != nil {
		logger.Error("Failed to get client info from ConfigMap for the first cluster")
		return ctrl.Result{}, err
	}

	logger.Info("Fetched client info for the first cluster", "ClientInfo", ci1)

	ci2, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(items[1].ClusterName, items[1].StorageClusterRef.Name))
	if err != nil {
		logger.Error("Failed to get client info from ConfigMap for the second cluster")
		return ctrl.Result{}, err
	}

	logger.Info("Fetched client info for the second cluster", "ClientInfo", ci2)
	logger.Info("Updating provider ConfigMap with client pairing", "ProviderClient1", ci1.ClientID, "PairedClient1", ci2.ClientID)
	if err := updateProviderConfigMap(logger, ctx, client, mirrorPeer, ci1, ci2); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Updating provider ConfigMap with client pairing", "ProviderClient2", ci2.ClientID, "PairedClient2", ci1.ClientID)
	if err := updateProviderConfigMap(logger, ctx, client, mirrorPeer, ci2, ci1); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Successfully created ManifestWork for cluster pairing ConfigMap")
	return ctrl.Result{}, nil
}

// updateProviderConfigMap updates the ConfigMap on the provider with the new client pairing
func updateProviderConfigMap(logger *slog.Logger, ctx context.Context, client client.Client, mirrorPeer multiclusterv1alpha1.MirrorPeer, providerClientInfo utils.ClientInfo, pairedClientInfo utils.ClientInfo) error {
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
						utils.StorageClusterPeerNameAnnotationKey: getStorageClusterPeerName(pairedClientInfo.ProviderInfo.ProviderManagedClusterName),
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
	_, err = utils.CreateOrUpdateManifestWork(ctx, client, manifestWorkName, manifestWorkNamespace, updatedObjJson, ownerRef)
	if err != nil {
		return fmt.Errorf("failed to update ManifestWork for provider %s: %w", providerName, err)
	}

	logger.Info("Successfully updated ManifestWork for provider", "ProviderName", providerName)
	return nil
}

func createStorageClusterPeer(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	logger = logger.With("MirrorPeer", mirrorPeer.Name)
	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Client info config map not found. Retrying request another time...")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	items := mirrorPeer.Spec.Items
	clientInfo := make([]utils.ClientInfo, 0)

	for _, item := range items {
		logger.Info("Fetching info for client", "ClientKey", utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		ci, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Client Info found", "ClientInfo", ci)
		clientInfo = append(clientInfo, ci)
	}

	for i := range items {
		var storageClusterPeerName string
		var oppositeClient utils.ClientInfo
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
			return ctrl.Result{}, fmt.Errorf("failed to fetch onboarding token for provider %s. %w", oppositeClient.ProviderInfo.ProviderManagedClusterName, err)
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
			return ctrl.Result{}, err
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

		operationResult, err := utils.CreateOrUpdateManifestWork(ctx, client, manifestWorkName, namespace, storageClusterPeerJson, ownerRef)
		if err != nil {
			return ctrl.Result{}, err
		}

		logger.Info(fmt.Sprintf("ManifestWork was %s for StorageClusterPeer %s", operationResult, storageClusterPeerName))
	}

	return ctrl.Result{}, nil
}

func fetchOnboardingTicket(ctx context.Context, client client.Client, clientInfo utils.ClientInfo, mirrorPeer multiclusterv1alpha1.MirrorPeer) (string, error) {
	secretName := string(mirrorPeer.GetUID())
	secretNamespace := clientInfo.ProviderInfo.ProviderManagedClusterName
	tokenSecret, err := utils.FetchSecretWithName(ctx, client, types.NamespacedName{Name: secretName, Namespace: secretNamespace})
	if err != nil {
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

type ManagedClusterAddonConfig struct {
	// Namespace on the managedCluster where it will be deployed
	InstallNamespace string

	// Name of the MCA
	Name string

	// Namespace on the hub where MCA will be created, it represents the Managed cluster where the addons will be deployed
	Namespace string
}

func getConfig(ctx context.Context, c client.Client, currentNamespace string, mp multiclusterv1alpha1.MirrorPeer) ([]ManagedClusterAddonConfig, error) {
	managedClusterAddonsConfig := make([]ManagedClusterAddonConfig, 0)

	// Check if the MirrorPeer contains StorageClient reference
	hasStorageClientRef, err := utils.IsStorageClientType(ctx, c, mp, false)
	if err != nil {
		return []ManagedClusterAddonConfig{}, err
	}

	if hasStorageClientRef {
		clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, c, currentNamespace)
		if err != nil {
			return []ManagedClusterAddonConfig{}, err
		}
		for _, item := range mp.Spec.Items {
			clientName := item.StorageClusterRef.Name
			clientInfo, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(item.ClusterName, clientName))
			if err != nil {
				return []ManagedClusterAddonConfig{}, err
			}
			config := ManagedClusterAddonConfig{
				Name:             setup.TokenExchangeName,
				Namespace:        clientInfo.ProviderInfo.ProviderManagedClusterName,
				InstallNamespace: clientInfo.ProviderInfo.NamespacedName.Namespace,
			}
			managedClusterAddonsConfig = append(managedClusterAddonsConfig, config)
		}
	} else {
		for _, item := range mp.Spec.Items {
			managedClusterAddonsConfig = append(managedClusterAddonsConfig, ManagedClusterAddonConfig{
				Name:             setup.TokenExchangeName,
				Namespace:        item.ClusterName,
				InstallNamespace: item.StorageClusterRef.Namespace,
			})
		}
	}
	return managedClusterAddonsConfig, nil
}

// processManagedClusterAddon creates an addon for the cluster management in all the peer refs,
// the resources gets an owner ref of the mirrorpeer to let the garbage collector handle it if the mirrorpeer gets deleted
func (r *MirrorPeerReconciler) processManagedClusterAddon(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger.With("MirrorPeer", mirrorPeer.Name)
	logger.Info("Processing ManagedClusterAddons for MirrorPeer")

	addonConfigs, err := getConfig(ctx, r.Client, r.CurrentNamespace, mirrorPeer)
	if err != nil {
		return fmt.Errorf("failed to get managedclusteraddon config %w", err)
	}

	for _, config := range addonConfigs {
		logger.Info("Handling ManagedClusterAddon for cluster", "ClusterName", config.Namespace)

		var managedClusterAddOn addonapiv1alpha1.ManagedClusterAddOn
		namespacedName := types.NamespacedName{
			Name:      config.Name,
			Namespace: config.Namespace,
		}

		err := r.Client.Get(ctx, namespacedName, &managedClusterAddOn)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManagedClusterAddon not found, will create a new one", "ClusterName", config.Namespace)

				annotations := make(map[string]string)
				annotations[utils.DRModeAnnotationKey] = string(mirrorPeer.Spec.Type)
				annotations[AddonVersionAnnotationKey] = version.Version
				managedClusterAddOn = addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:        setup.TokenExchangeName,
						Namespace:   config.Namespace,
						Annotations: annotations,
					},
				}
			}
		}

		logger.Info("Installing agents on the namespace", "InstallNamespace", config.InstallNamespace)
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &managedClusterAddOn, func() error {
			managedClusterAddOn.Spec.InstallNamespace = config.InstallNamespace
			if err := controllerutil.SetOwnerReference(&mirrorPeer, &managedClusterAddOn, r.Scheme); err != nil {
				logger.Error("Failed to set owner reference on ManagedClusterAddon", "error", err, "ClusterName", config.Namespace)
				return err
			}
			return nil
		})

		if err != nil {
			logger.Error("Failed to reconcile ManagedClusterAddOn", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name), "error", err)
			return err
		}
	}

	// Wait for ManagedClusterAddon to be available
	for _, config := range addonConfigs {
		var managedClusterAddOn addonapiv1alpha1.ManagedClusterAddOn
		namespacedName := types.NamespacedName{
			Name:      config.Name,
			Namespace: config.Namespace,
		}

		err := r.Client.Get(ctx, namespacedName, &managedClusterAddOn)
		if err != nil {
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
	logger.Info("Successfully processed all ManagedClusterAddons for MirrorPeer")
	return nil
}

// deleteSecrets checks if another mirrorpeer is using a peer ref in the mirrorpeer being deleted, if not then it
// goes ahead and deletes all the secrets with blue, green and internal label.
// If two mirrorpeers are pointing to the same peer ref, but only gets deleted the orphan green secret in
// the still standing peer ref gets deleted by the mirrorpeer secret controller
func (r *MirrorPeerReconciler) deleteSecrets(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	logger.Info("Starting deletion of secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name)

	for i, peerRef := range mirrorPeer.Spec.Items {
		logger.Info("Checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName)

		peerRefUsed, err := utils.DoesAnotherMirrorPeerPointToPeerRef(ctx, r.Client, &mirrorPeer.Spec.Items[i])
		if err != nil {
			logger.Error("Error checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName, "error", err)
			return err
		}

		if !peerRefUsed {
			logger.Info("PeerRef is not used by another MirrorPeer, proceeding to delete secrets", "PeerRef", peerRef.ClusterName)

			secretLabels := []string{}
			if mirrorPeer.Spec.ManageS3 {
				secretLabels = append(secretLabels, string(utils.InternalLabel))
			}

			secretRequirement, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.In, secretLabels)
			if err != nil {
				logger.Error("Cannot create label requirement for deleting secrets", "error", err)
				return err
			}

			secretSelector := labels.NewSelector().Add(*secretRequirement)
			deleteOpt := client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace:     mirrorPeer.Spec.Items[i].ClusterName,
					LabelSelector: secretSelector,
				},
			}

			var secret corev1.Secret
			if err := r.DeleteAllOf(ctx, &secret, &deleteOpt); err != nil {
				logger.Error("Error while deleting secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name, "PeerRef", peerRef.ClusterName, "error", err)
			}

			logger.Info("Secrets successfully deleted", "PeerRef", peerRef.ClusterName)
		} else {
			logger.Info("PeerRef is still used by another MirrorPeer, skipping deletion", "PeerRef", peerRef.ClusterName)
		}
	}

	logger.Info("Completed deletion of secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up controller for MirrorPeer")

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// CheckK8sUpdateErrors checks what type of error occurs when trying to update a k8s object
// and logs according to the object
func checkK8sUpdateErrors(err error, obj client.Object, logger *slog.Logger) (ctrl.Result, error) {
	if k8serrors.IsConflict(err) {
		logger.Info("Object is being updated by another process. Retrying", "Kind", obj.GetObjectKind().GroupVersionKind().Kind, "Name", obj.GetName())
		return ctrl.Result{Requeue: true}, nil
	} else if k8serrors.IsNotFound(err) {
		logger.Info("Object no longer exists. Ignoring since object must have been deleted", "Kind", obj.GetObjectKind().GroupVersionKind().Kind, "Name", obj.GetName())
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error("Failed to update object", "error", err, "Name", obj.GetName())
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func GetNamespacedNameForClientS3Secret(ctx context.Context, client client.Client, currentNamespace string, pr multiclusterv1alpha1.PeerRef, mp *multiclusterv1alpha1.MirrorPeer) (string, string, error) {
	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", "", fmt.Errorf("client info ConfigMap not found; requeuing for later retry %w", err)
		}
		return "", "", err
	}
	ci, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(pr.ClusterName, pr.StorageClusterRef.Name))
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

func (r *MirrorPeerReconciler) createDRClusters(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, hasStorageClientRef bool) error {
	logger := r.Logger

	for _, pr := range mp.Spec.Items {
		clusterName := pr.ClusterName
		var s3SecretName string
		var s3SecretNamespace string
		if hasStorageClientRef {
			name, namespace, err := GetNamespacedNameForClientS3Secret(ctx, r.Client, r.CurrentNamespace, pr, mp)
			if err != nil {
				return fmt.Errorf("failed to get namespace for s3 secret %w", err)
			}
			s3SecretName = name
			s3SecretNamespace = namespace
		} else {
			s3SecretName = utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)
			s3SecretNamespace = pr.ClusterName
		}
		dc := ramenv1alpha1.DRCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}

		logger.Info("Fetching s3 secret ", "Secret Name:", s3SecretName)
		ss, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: s3SecretName, Namespace: s3SecretNamespace})
		if err != nil {
			logger.Error("Failed to fetch S3 secret", "error", err, "SecretName", s3SecretName, "Namespace", s3SecretNamespace)
			return err
		}

		logger.Info("Unmarshalling S3 secret", "SecretName", s3SecretName)
		st, err := utils.UnmarshalS3Secret(ss)
		if err != nil {
			logger.Error("Failed to unmarshal S3 secret", "error", err, "SecretName", s3SecretName)
			return err
		}

		logger.Info("Creating and updating DR clusters", "ClusterName", clusterName)
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &dc, func() error {
			dc.Spec.S3ProfileName = st.S3ProfileName
			return nil
		})
		if err != nil {
			logger.Error("Failed to create/update DR cluster", "error", err, "ClusterName", clusterName)
			return err
		}
	}
	return nil
}

func (r *MirrorPeerReconciler) createClusterRoleBindingsForSpoke(ctx context.Context, peer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	logger.Info("Starting to create or update ClusterRoleBindings for the spoke", "MirrorPeerName", peer.Name)

	crb := rbacv1.ClusterRoleBinding{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: spokeClusterRoleBindingName}, &crb)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.Error("Failed to get ClusterRoleBinding", "error", err, "ClusterRoleBindingName", spokeClusterRoleBindingName)
			return err
		}
		logger.Info("ClusterRoleBinding not found, will be created", "ClusterRoleBindingName", spokeClusterRoleBindingName)
	}

	var subjects []rbacv1.Subject
	if crb.Subjects != nil {
		subjects = crb.Subjects
	}

	// Add users and groups
	for _, pr := range peer.Spec.Items {
		usub := getSubjectByPeerRef(pr, "User")
		gsub := getSubjectByPeerRef(pr, "Group")
		if !utils.ContainsSubject(subjects, usub) {
			subjects = append(subjects, *usub)
			logger.Info("Adding user subject to ClusterRoleBinding", "User", usub.Name)
		}

		if !utils.ContainsSubject(subjects, gsub) {
			subjects = append(subjects, *gsub)
			logger.Info("Adding group subject to ClusterRoleBinding", "Group", gsub.Name)
		}
	}

	spokeRoleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: spokeClusterRoleBindingName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &spokeRoleBinding, func() error {
		spokeRoleBinding.Subjects = subjects

		if spokeRoleBinding.CreationTimestamp.IsZero() {
			// RoleRef is immutable, so it's set only while creating new object
			spokeRoleBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "open-cluster-management:token-exchange:agent",
			}
			logger.Info("Setting RoleRef for new ClusterRoleBinding", "RoleRef", spokeRoleBinding.RoleRef.Name)
		}

		return nil
	})

	if err != nil {
		logger.Error("Failed to create or update ClusterRoleBinding", "error", err, "ClusterRoleBindingName", spokeClusterRoleBindingName)
		return err
	}

	logger.Info("Successfully created or updated ClusterRoleBinding", "ClusterRoleBindingName", spokeClusterRoleBindingName)
	return nil
}

func (r *MirrorPeerReconciler) updateMirrorPeerStatus(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, hasStorageClientRef bool) (ctrl.Result, error) {
	logger := r.Logger
	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		if hasStorageClientRef {
			providerModePeeringDone, err := isProviderModePeeringDone(ctx, r.Client, r.Logger, r.CurrentNamespace, &mirrorPeer)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to check if provider mode peering is correctly done %w", err)
			}

			if providerModePeeringDone {
				logger.Info("Peering of clusters is completed", "MirrorPeer", mirrorPeer.Name)
				mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangedSecret
				mirrorPeer.Status.Message = ""
				statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
				if statusErr != nil {
					logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, nil
			} else {
				mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangingSecret
				statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
				if statusErr != nil {
					logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{Requeue: true}, nil
			}
		}
	} else {
		// Sync mode status update, same flow as async but for s3 profile
		s3ProfileSynced, err := checkS3ProfileStatus(ctx, r.Client, logger, r.CurrentNamespace, mirrorPeer, hasStorageClientRef)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("S3 secrets not found; Attempting to reconcile again", "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error("Error while syncing S3 Profile", "error", err, "MirrorPeer", mirrorPeer.Name)
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
			}
			return ctrl.Result{}, err
		}

		if s3ProfileSynced {
			logger.Info("S3 Profile synced to hub", "MirrorPeer", mirrorPeer.Name)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.S3ProfileSynced
			mirrorPeer.Status.Message = ""
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

func isProviderModePeeringDone(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (bool, error) {
	isS3SecretSynced, err := checkS3ProfileStatus(ctx, client, logger, currentNamespace, *mirrorPeer, true)
	if err != nil {
		logger.Error("failed to check if s3 secrets have been synced")
		return false, err
	}
	logger.Info("S3 secrets sync status", "isS3SecretSynced", isS3SecretSynced)

	isStorageClusterPeerManifestWorkCreated, err := checkStorageClusterPeerStatus(ctx, client, logger, currentNamespace, mirrorPeer)
	if err != nil {
		logger.Error("failed to check if StorageClusterPeer have been created")
		return false, err
	}

	logger.Info("StorageClusterPeer manifest work creation status", "isStorageClusterPeerManifestWorkCreated", isStorageClusterPeerManifestWorkCreated)
	isClientPairingConfigMapCreated, err := checkClientPairingConfigMapStatus(ctx, client, logger, currentNamespace, mirrorPeer)
	if err != nil {
		logger.Error("failed to check if client pair config map has been created")
		return false, err
	}

	logger.Info("Client pairing ConfigMap creation status", "isClientPairingConfigMapCreated", isClientPairingConfigMapCreated)

	isOnboardingTicketCreated, err := checkOnboardingTicketStatus(ctx, client, logger, currentNamespace, mirrorPeer)
	if err != nil {
		logger.Error("failed to check if onboarding tickets has been created")
		return false, err
	}

	logger.Info("Onboarding ticket creation status", "isOnboardingTicketCreated", isOnboardingTicketCreated)

	allChecksPassed := isS3SecretSynced &&
		isStorageClusterPeerManifestWorkCreated &&
		isClientPairingConfigMapCreated &&
		isOnboardingTicketCreated

	logger.Info("Provider mode peering status", "AllChecksPassed", allChecksPassed)
	return allChecksPassed, nil
}

func checkOnboardingTicketStatus(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger = logger.With("MirrorPeer", mirrorPeer.Name)
	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, fmt.Errorf("client info config map not found")
		}
		return false, fmt.Errorf("failed to fetch client info config map %w", err)
	}
	for _, item := range mirrorPeer.Spec.Items {
		logger.Info("Fetching info for client", "ClientKey", utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		ci, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, utils.GetKey(item.ClusterName, item.StorageClusterRef.Name))
		if err != nil {
			return false, fmt.Errorf("failed to fetch client info from the config map %w", err)
		}
		logger.Info("Client Info found for checking onboarding ticket status", "ClientInfo", ci)
		_, err = fetchOnboardingTicket(ctx, client, ci, *mirrorPeer)
		if err != nil {
			return false, fmt.Errorf("failed to fetch onboarding token for provider %s. %w", ci.ProviderInfo.ProviderManagedClusterName, err)
		}
	}

	return true, nil
}

func checkS3ProfileStatus(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mp multiclusterv1alpha1.MirrorPeer, hasStorageClientRef bool) (bool, error) {
	logger.Info("Checking S3 profile status for each peer reference in the MirrorPeer", "MirrorPeerName", mp.Name)

	for _, pr := range mp.Spec.Items {
		var s3SecretName string
		var s3SecretNamespace string
		if hasStorageClientRef {
			name, namespace, err := GetNamespacedNameForClientS3Secret(ctx, client, currentNamespace, pr, &mp)
			if err != nil {
				return false, err
			}
			s3SecretName = name
			s3SecretNamespace = namespace
		} else {
			s3SecretNamespace = pr.ClusterName
			s3SecretName = utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)
		}
		logger.Info("Attempting to fetch S3 secret", "SecretName", s3SecretName, "Namespace", s3SecretNamespace)

		_, err := utils.FetchSecretWithName(ctx, client, types.NamespacedName{Name: s3SecretName, Namespace: s3SecretNamespace})
		if err != nil {
			logger.Error("Failed to fetch S3 secret", "error", err, "SecretName", s3SecretName, "Namespace", s3SecretNamespace)
			return false, err
		}

		logger.Info("Successfully fetched S3 secret", "SecretName", s3SecretName, "Namespace", s3SecretNamespace)
	}

	logger.Info("Successfully verified S3 profile status for all peer references", "MirrorPeerName", mp.Name)
	return true, nil
}

func getSubjectByPeerRef(pr multiclusterv1alpha1.PeerRef, kind string) *rbacv1.Subject {
	switch kind {
	case "User":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultUser(pr.ClusterName, setup.TokenExchangeName, setup.TokenExchangeName),
			APIGroup: "rbac.authorization.k8s.io",
		}
	case "Group":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultGroups(pr.ClusterName, setup.TokenExchangeName)[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
	default:
		return nil
	}
}
