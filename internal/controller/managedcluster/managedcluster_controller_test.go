//go:build unit
// +build unit

package managedcluster

import (
	"context"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var mgrScheme = runtime.NewScheme()

func TestManagedClusterReconcile(t *testing.T) {
	_ = clusterv1.AddToScheme(mgrScheme)
	_ = viewv1beta1.AddToScheme(mgrScheme)
	_ = multiclusterv1alpha1.AddToScheme(mgrScheme)

	client := fake.NewClientBuilder().WithScheme(mgrScheme).Build()
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterReconciler{
		Client: client,
		Scheme: mgrScheme,
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
		managedCluster := &clusterv1.ManagedCluster{
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

		_ = client.Create(context.TODO(), managedCluster)

		res, err := reconciler.Reconcile(context.TODO(), req)
		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)
	})
}

func TestProcessManagedClusterViews(t *testing.T) {
	_ = clusterv1.AddToScheme(mgrScheme)
	_ = viewv1beta1.AddToScheme(mgrScheme)
	_ = multiclusterv1alpha1.AddToScheme(mgrScheme)

	client := fake.NewClientBuilder().WithScheme(mgrScheme).Build()
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterReconciler{
		Client: client,
		Scheme: mgrScheme,
		Logger: logger,
	}

	t.Run("ManagedClusterView does not exist. It should create it", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
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
		err = client.Get(context.TODO(), types.NamespacedName{Name: utils.GetODFManagedClusterViewName("test-cluster"), Namespace: "test-cluster"}, createdMCV)
		assert.NoError(t, err)
		assert.Equal(t, utils.GetODFManagedClusterViewName("test-cluster"), createdMCV.Name)
		assert.Equal(t, "test-cluster", createdMCV.Namespace)
		assert.Equal(t, "odf-info", createdMCV.Spec.Scope.Name)
		assert.Equal(t, "openshift-storage", createdMCV.Spec.Scope.Namespace)
	})

	t.Run("ManagedClusterView exists but spec does not match. Desired spec should be reached", func(t *testing.T) {
		existingMCV := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GetODFManagedClusterViewName("test-cluster"),
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

		managedCluster := &clusterv1.ManagedCluster{
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
		_ = client.Get(context.TODO(), types.NamespacedName{Name: utils.GetODFManagedClusterViewName("test-cluster"), Namespace: "test-cluster"}, updatedMCV)
		assert.Equal(t, "odf-info", updatedMCV.Spec.Scope.Name)
		assert.Equal(t, "openshift-storage", updatedMCV.Spec.Scope.Namespace)
	})

	t.Run("ManagedClusterView exists and spec matches. Nothing should change. It should be error free", func(t *testing.T) {
		existingMCV := &viewv1beta1.ManagedClusterView{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GetODFManagedClusterViewName("test-cluster"),
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

		managedCluster := &clusterv1.ManagedCluster{
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

func TestScaleClusterClaimSupport(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clusterv1.AddToScheme(scheme)
	_ = viewv1beta1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := utils.GetLogger(utils.GetZapLogger(true))

	reconciler := &ManagedClusterReconciler{
		Client: client,
		Scheme: scheme,
		Logger: logger,
	}

	t.Run("ManagedCluster with Scale ClusterClaim", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "scale-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.IbmScaleClusterClaimNamespacedName,
						Value: "ibm-spectrum-scale/ibm-spectrum-scale-replicationinfo",
					},
				},
			},
		}

		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)

		// Verify Scale ManagedClusterView was created
		createdScaleMCV := &viewv1beta1.ManagedClusterView{}
		err = client.Get(context.TODO(), types.NamespacedName{
			Name:      utils.GetIbmScaleManagedClusterViewName("scale-cluster"),
			Namespace: "scale-cluster",
		}, createdScaleMCV)
		assert.NoError(t, err)
		assert.Equal(t, "ibm-spectrum-scale-replicationinfo", createdScaleMCV.Spec.Scope.Name)
		assert.Equal(t, "ibm-spectrum-scale", createdScaleMCV.Spec.Scope.Namespace)
		assert.Equal(t, "ConfigMap", createdScaleMCV.Spec.Scope.Resource)

		// Verify token-exchange ManagedClusterView was NOT created for scale
		tokenExchangeMCV := &viewv1beta1.ManagedClusterView{}
		err = client.Get(context.TODO(), types.NamespacedName{
			Name:      utils.GetTokenExchangeManagedClusterViewName("scale-cluster"),
			Namespace: "scale-cluster",
		}, tokenExchangeMCV)
		assert.Error(t, err) // Should not exist
	})

	t.Run("ManagedCluster with both ODF and Scale ClusterClaims", func(t *testing.T) {
		managedCluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "multi-vendor-cluster",
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  utils.OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
					{
						Name:  utils.IbmScaleClusterClaimNamespacedName,
						Value: "ibm-spectrum-scale/ibm-spectrum-scale-replicationinfo",
					},
				},
			},
		}

		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)

		// Verify ODF ManagedClusterView was created
		odfMCV := &viewv1beta1.ManagedClusterView{}
		err = client.Get(context.TODO(), types.NamespacedName{
			Name:      utils.GetODFManagedClusterViewName("multi-vendor-cluster"),
			Namespace: "multi-vendor-cluster",
		}, odfMCV)
		assert.NoError(t, err)
		assert.Equal(t, "odf-info", odfMCV.Spec.Scope.Name)
		assert.Equal(t, "openshift-storage", odfMCV.Spec.Scope.Namespace)

		// Verify Scale ManagedClusterView was created
		scaleMCV := &viewv1beta1.ManagedClusterView{}
		err = client.Get(context.TODO(), types.NamespacedName{
			Name:      utils.GetIbmScaleManagedClusterViewName("multi-vendor-cluster"),
			Namespace: "multi-vendor-cluster",
		}, scaleMCV)
		assert.NoError(t, err)
		assert.Equal(t, "ibm-spectrum-scale-replicationinfo", scaleMCV.Spec.Scope.Name)
		assert.Equal(t, "ibm-spectrum-scale", scaleMCV.Spec.Scope.Namespace)
	})
}
