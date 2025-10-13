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
	"reflect"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func undefinedMirrorPeerSpec(spec multiclusterv1alpha1.MirrorPeerSpec) error {
	if reflect.DeepEqual(spec, multiclusterv1alpha1.MirrorPeerSpec{}) {
		return fmt.Errorf("validation: MirrorPeer.Spec must not be empty")
	}
	return nil
}

func uniqueSpecItems(spec multiclusterv1alpha1.MirrorPeerSpec) error {
	if reflect.DeepEqual(spec.Items[0], spec.Items[1]) {
		return fmt.Errorf("validation: MirrorPeer.Spec.Items fields must be unique within a MirrorPeer object")
	}
	return nil
}

func emptySpecItems(peerRef multiclusterv1alpha1.PeerRef) error {
	if peerRef.ClusterName == "" || peerRef.StorageClusterRef.Name == "" {
		return fmt.Errorf("validation: MirrorPeer.Spec.Items fields must not be empty or undefined")
	}
	return nil
}

func isManagedCluster(ctx context.Context, client client.Client, clusterName string) error {
	var mcluster clusterv1.ManagedCluster
	err := client.Get(ctx, types.NamespacedName{Name: clusterName}, &mcluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("validation: ManagedCluster %q not found : %q is not a managed cluster", clusterName, clusterName)
		}
		return fmt.Errorf("validation: unable to get ManagedCluster %q: error: %v", clusterName, err)
	}
	return nil
}

func isVersionCompatible(peerRef multiclusterv1alpha1.PeerRef, clientInfoMap map[string]string) error {
	clientInfo, err := utils.GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(peerRef.ClusterName, peerRef.StorageClusterRef.Name))
	if err != nil {
		return fmt.Errorf("validation: unable to get client info: error: %v", err)
	}
	eq, err := utils.CompareSemverMajorMinorVersions(clientInfo.ProviderInfo.Version, version.Version, utils.Eq)
	if err != nil {
		return fmt.Errorf("validation: unable to parse versions: error: %v", err)
	}
	if !eq {
		return fmt.Errorf("validation: StorageCluster version %q on ManagedCluster %q is incompatible with Multicluster Orchestrator version %q", clientInfo.ProviderInfo.Version, peerRef.ClusterName, version.Version)
	}
	return nil
}

// checkStorageClusterPeerStatus checks if the ManifestWorks for StorageClusterPeer resources
// have been created and reached the Applied status.
func checkStorageClusterPeerStatus(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger.Info("Checking if StorageClusterPeer ManifestWorks have been created and reached Peered status")

	// Fetch the client info ConfigMap
	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Client info ConfigMap not found; requeuing for later retry")
			return false, nil
		}
		return false, fmt.Errorf("failed to fetch client info ConfigMap: %w", err)
	}

	// Collect client information for each cluster in the MirrorPeer
	items := mirrorPeer.Spec.Items
	clientInfos := make([]utils.ClientInfo, 0, len(items))
	for _, item := range items {
		clientKey := utils.GetKey(item.ClusterName, item.StorageClusterRef.Name)
		ci, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, clientKey)
		if err != nil {
			logger.Error("Failed to get client info from ConfigMap", "ClientKey", clientKey)
			return false, err
		}
		clientInfos = append(clientInfos, ci)
	}

	// Check the status of the ManifestWork for each StorageClusterPeer
	for _, currentClient := range clientInfos {
		// Determine the name and namespace for the ManifestWork
		manifestWorkName := fmt.Sprintf("storageclusterpeer-%s", currentClient.ProviderInfo.ProviderManagedClusterName)
		manifestWorkNamespace := currentClient.ProviderInfo.ProviderManagedClusterName

		// Fetch the ManifestWork
		manifestWork := &workv1.ManifestWork{}
		err := client.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: manifestWorkNamespace}, manifestWork)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManifestWork for StorageClusterPeer not found; it may not be created yet", "ManifestWorkName", manifestWorkName)
				return false, nil
			}
			return false, fmt.Errorf("failed to get ManifestWork for StorageClusterPeer: %w", err)
		}

		// Check if the ManifestWork has been successfully applied
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

	// All ManifestWorks have been created and have Applied status
	logger.Info("All StorageClusterPeer ManifestWorks have been created and reached Peered status")
	return true, nil
}

// checkClientPairingConfigMapStatus checks if the ManifestWorks for client pairing ConfigMaps
// have been created and reached the Applied status.
func checkClientPairingConfigMapStatus(ctx context.Context, client client.Client, logger *slog.Logger, currentNamespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger.Info("Checking if client pairing ConfigMap ManifestWorks have been created and reached Applied status")

	// Fetch the client info ConfigMap
	clientInfoMap, err := utils.FetchClientInfoConfigMap(ctx, client, currentNamespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Client info ConfigMap not found; requeuing for later retry")
			return false, nil
		}
		return false, fmt.Errorf("failed to fetch client info ConfigMap: %w", err)
	}

	// Collect client information for each cluster in the MirrorPeer
	items := mirrorPeer.Spec.Items
	clientInfos := make([]utils.ClientInfo, 0, len(items))
	for _, item := range items {
		clientKey := utils.GetKey(item.ClusterName, item.StorageClusterRef.Name)
		ci, err := utils.GetClientInfoFromConfigMap(clientInfoMap.Data, clientKey)
		if err != nil {
			logger.Error("Failed to get client info from ConfigMap", "ClientKey", clientKey)
			return false, err
		}
		clientInfos = append(clientInfos, ci)
	}

	// Check the status of the ManifestWork for each provider's client pairing ConfigMap
	for _, providerClient := range clientInfos {
		manifestWorkName := "storage-client-mapping"
		manifestWorkNamespace := providerClient.ProviderInfo.ProviderManagedClusterName

		// Fetch the ManifestWork
		manifestWork := &workv1.ManifestWork{}
		err := client.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: manifestWorkNamespace}, manifestWork)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManifestWork for client pairing ConfigMap not found; it may not be created yet",
					"ManifestWorkName", manifestWorkName, "Namespace", manifestWorkNamespace)
				return false, nil
			}
			return false, fmt.Errorf("failed to get ManifestWork for client pairing ConfigMap: %w", err)
		}

		// Check if the ManifestWork has been successfully applied
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

	// All ConfigMap ManifestWorks have been created and have Applied status
	logger.Info("All client pairing ConfigMap ManifestWorks have been created and reached Applied status")
	return true, nil
}

// ValidateTokenExchangeAgentUpdated validates that the token-exchange-agent pods on managedclusters are updated properly
func ValidateTokenExchangeAgentUpdated(ctx context.Context, client client.Client, logger *slog.Logger, clusterName, testEnvFile string) error {
	var managedClusterView viewv1beta1.ManagedClusterView
	mcvNamespacedName := types.NamespacedName{
		Namespace: clusterName,
		Name:      utils.GetTokenExchangeManagedClusterViewName(clusterName),
	}
	if err := client.Get(ctx, mcvNamespacedName, &managedClusterView); err != nil {
		logger.Error("Failed to get ManagedClusterView", "error", err)
		return err
	}

	tokenExchangeDep := appsv1.Deployment{}
	if err := json.Unmarshal(managedClusterView.Status.Result.Raw, &tokenExchangeDep); err != nil {
		return fmt.Errorf("failed to unmarshal result data. %w", err)
	}

	tokenExchangeImage := utils.GetEnv("TOKEN_EXCHANGE_IMAGE", testEnvFile)
	tokenExchangeContainer := v1.Container{}
	for _, c := range tokenExchangeDep.Spec.Template.Spec.Containers {
		if c.Name == utils.TokenExchangeDeployment {
			tokenExchangeContainer = c
		}
	}
	if tokenExchangeContainer.Name == "" {
		return fmt.Errorf("container 'token-exchange-agent' not found in 'token-exchange-agent' deployment parsed from managedclusterview")
	}
	if tokenExchangeContainer.Image != tokenExchangeImage || tokenExchangeDep.Status.Replicas != 1 {
		logger.Error("token-exchange-agent pods are not yet updated, waiting for update to complete",
			"current token-exchange-agent image", tokenExchangeContainer.Image,
			"expected token-exchange-agent image", tokenExchangeImage,
			"replicas", tokenExchangeDep.Status.Replicas)
		return utils.ErrRequeueReconcile
	}

	return nil
}
