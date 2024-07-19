package utils

import (
	"context"
	"fmt"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const MCVLabelKey = "multicluster.odf.openshift.io/cluster"
const MCVNameTemplate = "odf-multicluster-mcv-%s"

func GetManagedClusterViewName(clusterName string) string {
	return fmt.Sprintf(MCVNameTemplate, clusterName)
}

func CreateOrUpdateManagedClusterView(ctx context.Context, client ctrlClient.Client, resourceToFindName string, resourceToFindNamespace string, resourceToFindType string, clusterName string, ownerRef *metav1.OwnerReference) (*viewv1beta1.ManagedClusterView, controllerutil.OperationResult, error) {
	mcv := &viewv1beta1.ManagedClusterView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetManagedClusterViewName(clusterName),
			Namespace: clusterName,
		},
	}

	operationResult, err := controllerutil.CreateOrUpdate(ctx, client, mcv, func() error {
		mcv.Spec = viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Name:      resourceToFindName,
				Namespace: resourceToFindNamespace,
				Resource:  resourceToFindType,
			},
		}

		if mcv.Labels == nil {
			mcv.Labels = make(map[string]string)
		}

		mcv.Labels[CreatedByLabelKey] = "odf-multicluster-managedcluster-controller"

		if ownerRef != nil {
			mcv.OwnerReferences = []metav1.OwnerReference{*ownerRef}
		}

		return nil
	})

	if err != nil {
		return nil, controllerutil.OperationResultNone, err
	}

	return mcv, operationResult, nil
}

func GetManagedClusterView(client ctrlClient.Client, name, namespace string) (*viewv1beta1.ManagedClusterView, error) {
	mcv := &viewv1beta1.ManagedClusterView{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, mcv)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("ManagedClusterView %s not found in namespace %s", name, namespace)
		}
		return nil, fmt.Errorf("failed to get ManagedClusterView %s in namespace %s. %w", name, namespace, err)
	}
	return mcv, nil
}
