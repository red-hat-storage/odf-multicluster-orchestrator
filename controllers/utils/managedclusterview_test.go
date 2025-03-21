package utils

import (
	"context"
	"testing"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateOrUpdateManagedClusterView(t *testing.T) {
	s := runtime.NewScheme()
	err := viewv1beta1.AddToScheme(s)
	assert.NoError(t, err)
	client := fake.NewClientBuilder().WithScheme(s).Build()

	t.Run("Success", func(t *testing.T) {
		mcv, _, err := CreateOrUpdateManagedClusterView(context.TODO(), client, "example-configmap", "default", "ConfigMap", "managed-cluster-1", nil)
		assert.NoError(t, err)
		assert.NotNil(t, mcv)
		assert.Equal(t, GetManagedClusterViewName("managed-cluster-1"), mcv.Name)
		assert.Equal(t, "example-configmap", mcv.Spec.Scope.Name)
		assert.Equal(t, "default", mcv.Spec.Scope.Namespace)
		assert.Equal(t, "ConfigMap", mcv.Spec.Scope.Resource)
		assert.Equal(t, "odf-multicluster-managedcluster-controller", mcv.Labels[CreatedByLabelKey])
	})
}
