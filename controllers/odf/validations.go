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
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
