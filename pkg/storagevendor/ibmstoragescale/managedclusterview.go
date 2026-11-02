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

package ibmstoragescale

import (
	"context"
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// createOrUpdateMCV creates or updates a ManagedClusterView in clusterName's namespace that reads
// the given resource from resourceNamespace on that managed cluster. The MCV is owned by the MirrorPeer.
func (v *IbmStorageScaleVendor) createOrUpdateMCV(ctx context.Context, mirrorPeer *multiclusterv1alpha1.MirrorPeer, mcvName, clusterName, resource, resourceNamespace, resourceName string) (*viewv1beta1.ManagedClusterView, error) {
	mcv := &viewv1beta1.ManagedClusterView{}
	mcv.Name = mcvName
	mcv.Namespace = clusterName

	_, err := controllerutil.CreateOrUpdate(ctx, v.client, mcv, func() error {
		mcv.Spec = viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Group:     multiclusterv1alpha1.GroupVersion.Group,
				Resource:  resource,
				Name:      resourceName,
				Namespace: resourceNamespace,
			},
		}

		utils.AddLabel(mcv, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
		utils.AddLabel(mcv, utils.HubRecoveryLabel, "")

		return controllerutil.SetControllerReference(mirrorPeer, mcv, v.scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update ManagedClusterView %q in namespace %q: %w", mcv.Name, mcv.Namespace, err)
	}
	return mcv, nil
}
