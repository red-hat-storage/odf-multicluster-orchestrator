package utils

import (
	"context"
	"testing"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestGetManagedClusterView(t *testing.T) {
	s := runtime.NewScheme()
	err := viewv1beta1.AddToScheme(s)
	assert.NoError(t, err)

	clusterView := &viewv1beta1.ManagedClusterView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcv",
			Namespace: "test-namespace",
		},
	}

	client := fake.NewClientBuilder().WithScheme(s).WithObjects(clusterView).Build()

	tests := []struct {
		name      string
		mcvName   string
		namespace string
		wantErr   bool
	}{
		{
			name:      "existing ManagedClusterView",
			mcvName:   "test-mcv",
			namespace: "test-namespace",
			wantErr:   false,
		},
		{
			name:      "non-existing ManagedClusterView",
			mcvName:   "non-existing-mcv",
			namespace: "test-namespace",
			wantErr:   true,
		},
		{
			name:      "existing ManagedClusterView in different namespace",
			mcvName:   "test-mcv",
			namespace: "different-namespace",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetManagedClusterView(client, tt.mcvName, tt.namespace)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
				assert.Equal(t, tt.mcvName, got.Name)
				assert.Equal(t, tt.namespace, got.Namespace)
			}
		})
	}
}
