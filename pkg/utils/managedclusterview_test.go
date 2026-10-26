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

func TestGetODFManagedClusterViewName(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    string
	}{
		{
			name:        "Simple cluster name",
			clusterName: "cluster1",
			expected:    "odf-multicluster-mcv-cluster1",
		},
		{
			name:        "Cluster name with dashes",
			clusterName: "my-cluster-name",
			expected:    "odf-multicluster-mcv-my-cluster-name",
		},
		{
			name:        "Empty cluster name",
			clusterName: "",
			expected:    "odf-multicluster-mcv-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetODFManagedClusterViewName(tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetIbmScaleManagedClusterViewName(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    string
	}{
		{
			name:        "Simple cluster name",
			clusterName: "cluster1",
			expected:    "scale-multicluster-mcv-cluster1",
		},
		{
			name:        "Cluster name with dashes",
			clusterName: "scale-cluster",
			expected:    "scale-multicluster-mcv-scale-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetIbmScaleManagedClusterViewName(tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetTokenExchangeManagedClusterViewName(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expected    string
	}{
		{
			name:        "Simple cluster name",
			clusterName: "cluster1",
			expected:    "odf-multicluster-mcv-cluster1-token-exchange-agent",
		},
		{
			name:        "Cluster name with dashes",
			clusterName: "my-cluster",
			expected:    "odf-multicluster-mcv-my-cluster-token-exchange-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTokenExchangeManagedClusterViewName(tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateOrUpdateManagedClusterView(t *testing.T) {
	s := runtime.NewScheme()
	err := viewv1beta1.AddToScheme(s)
	assert.NoError(t, err)
	err = clusterv1.AddToScheme(s)
	assert.NoError(t, err)
	client := fake.NewClientBuilder().WithScheme(s).Build()

	t.Run("Success", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "ManagedCluster",
			},
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

		// Verify that owner reference was set
		assert.Len(t, mcv.OwnerReferences, 1)
		assert.Equal(t, managedCluster.Name, mcv.OwnerReferences[0].Name)
		assert.Equal(t, "ManagedCluster", mcv.OwnerReferences[0].Kind)
		assert.Equal(t, clusterv1.GroupVersion.String(), mcv.OwnerReferences[0].APIVersion)
		// Controller field may be nil with fake client, but SetOwnerReference sets it to true
		// Just verify the owner reference exists with correct Name, Kind, and APIVersion
	})

	t.Run("Update existing ManagedClusterView", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "ManagedCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster-2",
			},
		}
		err := client.Create(context.TODO(), managedCluster)
		assert.NoError(t, err)

		mcvName := GetODFManagedClusterViewName("managed-cluster-2")

		// Create the ManagedClusterView first
		mcv1, op1, err := CreateOrUpdateManagedClusterView(
			context.TODO(),
			client,
			s,
			managedCluster,
			mcvName,
			"configmap-1",
			"namespace-1",
			"ConfigMap",
		)
		assert.NoError(t, err)
		assert.NotNil(t, mcv1)
		assert.Equal(t, "configmap-1", mcv1.Spec.Scope.Name)
		assert.Equal(t, "namespace-1", mcv1.Spec.Scope.Namespace)
		// First call should create
		assert.NotEqual(t, "unchanged", string(op1))

		// Update with different values
		mcv2, op2, err := CreateOrUpdateManagedClusterView(
			context.TODO(),
			client,
			s,
			managedCluster,
			mcvName,
			"configmap-2",
			"namespace-2",
			"ConfigMap",
		)
		assert.NoError(t, err)
		assert.NotNil(t, mcv2)
		assert.Equal(t, "configmap-2", mcv2.Spec.Scope.Name)
		assert.Equal(t, "namespace-2", mcv2.Spec.Scope.Namespace)
		assert.Equal(t, mcvName, mcv2.Name)
		// Second call should update
		assert.NotEqual(t, "unchanged", string(op2))
	})
}
