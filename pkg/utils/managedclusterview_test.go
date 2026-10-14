package utils

import (
	"context"
	"testing"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateOrUpdateManagedClusterView(t *testing.T) {
	s := runtime.NewScheme()
	err := viewv1beta1.AddToScheme(s)
	assert.NoError(t, err)
	err = clusterv1.AddToScheme(s)
	assert.NoError(t, err)
	client := fake.NewClientBuilder().WithScheme(s).Build()

	t.Run("Success", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster-1",
			},
		}
		err := client.Create(context.TODO(), managedCluster)
		assert.NoError(t, err)

		mcvName := GetODFManagedClusterViewName("managed-cluster-1")
		mcv, _, err := CreateOrUpdateManagedClusterView(
			context.TODO(),
			client,
			s,
			managedCluster,
			mcvName,
			"example-configmap",
			"default",
			"ConfigMap",
		)
		assert.NoError(t, err)
		assert.NotNil(t, mcv)
		assert.Equal(t, GetODFManagedClusterViewName("managed-cluster-1"), mcv.Name)
		assert.Equal(t, "example-configmap", mcv.Spec.Scope.Name)
		assert.Equal(t, "default", mcv.Spec.Scope.Namespace)
		assert.Equal(t, "ConfigMap", mcv.Spec.Scope.Resource)
		assert.Equal(t, "odf-multicluster-managedcluster-controller", mcv.Labels[CreatedByLabelKey])

		// Verify that controller reference was set
		assert.Len(t, mcv.OwnerReferences, 1)
		assert.Equal(t, managedCluster.Name, mcv.OwnerReferences[0].Name)
		assert.Equal(t, "ManagedCluster", mcv.OwnerReferences[0].Kind)
		assert.True(t, *mcv.OwnerReferences[0].Controller)
	})
}
