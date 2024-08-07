//go:build unit
// +build unit

package controllers

import (
	"context"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestManagedClusterReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clusterv1.AddToScheme(scheme)
	_ = viewv1beta1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterReconciler{
		Client: client,
		Logger: logger,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cluster"},
	}

	t.Run("ManagedCluster not found", func(t *testing.T) {
		res, err := reconciler.Reconcile(context.TODO(), req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)
	})

	t.Run("ManagedCluster found", func(t *testing.T) {
		managedCluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
				},
			},
		}

		_ = client.Create(context.TODO(), &managedCluster)

		res, err := reconciler.Reconcile(context.TODO(), req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)
	})
}

func TestProcessManagedClusterViews(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clusterv1.AddToScheme(scheme)
	_ = viewv1beta1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterReconciler{
		Client: client,
		Logger: logger,
	}

	t.Run("ManagedClusterView does not exist. It should create it", func(t *testing.T) {
		managedCluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
				},
			},
		}
		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)

		createdMCV := &viewv1beta1.ManagedClusterView{}
		err = client.Get(context.TODO(), types.NamespacedName{Name: utils.GetManagedClusterViewName("test-cluster"), Namespace: "test-cluster"}, createdMCV)
		assert.NoError(t, err)
		assert.Equal(t, utils.GetManagedClusterViewName("test-cluster"), createdMCV.Name)
		assert.Equal(t, "test-cluster", createdMCV.Namespace)
		assert.Equal(t, "odf-info", createdMCV.Spec.Scope.Name)
		assert.Equal(t, "openshift-storage", createdMCV.Spec.Scope.Namespace)
	})

	t.Run("ManagedClusterView exists but spec does not match. Desired spec should be reached", func(t *testing.T) {
		existingMCV := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GetManagedClusterViewName("test-cluster"),
				Namespace: "test-cluster",
			},
			Spec: viewv1beta1.ViewSpec{
				Scope: viewv1beta1.ViewScope{
					Name:      "old-configmap",
					Namespace: "old-namespace",
					Resource:  "ConfigMap",
				},
			},
		}
		_ = client.Create(context.TODO(), existingMCV)

		managedCluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
				},
			},
		}

		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)

		updatedMCV := &viewv1beta1.ManagedClusterView{}
		_ = client.Get(context.TODO(), types.NamespacedName{Name: utils.GetManagedClusterViewName("test-cluster"), Namespace: "test-cluster"}, updatedMCV)
		assert.Equal(t, "odf-info", updatedMCV.Spec.Scope.Name)
		assert.Equal(t, "openshift-storage", updatedMCV.Spec.Scope.Namespace)
	})

	t.Run("ManagedClusterView exists and spec matches. Nothing should change. It should be error free", func(t *testing.T) {
		existingMCV := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GetManagedClusterViewName("test-cluster"),
				Namespace: "test-cluster",
			},
			Spec: viewv1beta1.ViewSpec{
				Scope: viewv1beta1.ViewScope{
					Name:      "odf-info",
					Namespace: "openshift-storage",
					Resource:  "ConfigMap",
				},
			},
		}
		_ = client.Create(context.TODO(), existingMCV)

		managedCluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
				},
			},
		}
		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)
	})
}
