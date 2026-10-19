package utils

import (
	"context"
	"errors"
	"fmt"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ODFMCVNamePrefix        = "odf-multicluster-mcv"
	IbmScaleMCVNamePrefix   = "scale-multicluster-mcv"
	TokenExchangeDeployment = "token-exchange-agent"
)

var ErrRequeueReconcile = errors.New("requeue reconcile request")

func GetODFManagedClusterViewName(clusterName string) string {
	return fmt.Sprintf("%s-%s", ODFMCVNamePrefix, clusterName)
}

func GetIbmScaleManagedClusterViewName(clusterName string) string {
	return fmt.Sprintf("%s-%s", IbmScaleMCVNamePrefix, clusterName)
}

func GetTokenExchangeManagedClusterViewName(clusterName string) string {
	return fmt.Sprintf("%s-%s", GetODFManagedClusterViewName(clusterName), TokenExchangeDeployment)
}

// CreateOrUpdateManagedClusterView creates or updates a ManagedClusterView resource.
// It uses SetControllerReference to establish ownership with the provided ManagedCluster.
func CreateOrUpdateManagedClusterView(
	ctx context.Context,
	client ctrlClient.Client,
	scheme *runtime.Scheme,
	managedCluster *clusterv1.ManagedCluster,
	mcvName string,
	resourceToFindName string,
	resourceToFindNamespace string,
	resourceToFindType string,
) (*viewv1beta1.ManagedClusterView, controllerutil.OperationResult, error) {
	mcv := &viewv1beta1.ManagedClusterView{}
	mcv.Name = mcvName
	mcv.Namespace = managedCluster.Name

	operationResult, err := controllerutil.CreateOrUpdate(ctx, client, mcv, func() error {
		// Set the spec
		mcv.Spec = viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Name:      resourceToFindName,
				Namespace: resourceToFindNamespace,
				Resource:  resourceToFindType,
			},
		}

		AddLabel(mcv, CreatedByLabelKey, "odf-multicluster-managedcluster-controller")
		AddLabel(mcv, HubRecoveryLabel, "")

		// Set controller reference - this establishes ownership
		if err := controllerutil.SetControllerReference(managedCluster, mcv, scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, controllerutil.OperationResultNone, err
	}

	return mcv, operationResult, nil
}
