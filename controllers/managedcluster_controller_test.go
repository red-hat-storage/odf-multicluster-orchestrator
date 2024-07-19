//go:build unit
// +build unit

package controllers

import (
	"context"
	"reflect"
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
						Name:  OdfInfoClusterClaimNamespacedName,
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
						Name:  OdfInfoClusterClaimNamespacedName,
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
						Name:  OdfInfoClusterClaimNamespacedName,
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
						Name:  OdfInfoClusterClaimNamespacedName,
						Value: "openshift-storage/odf-info",
					},
				},
			},
		}
		err := reconciler.processManagedClusterViews(context.TODO(), managedCluster)
		assert.NoError(t, err)
	})
}

func Test_getNamespacedNameForClusterInfo(t *testing.T) {
	type args struct {
		managedCluster clusterv1.ManagedCluster
	}
	tests := []struct {
		name    string
		args    args
		want    types.NamespacedName
		wantErr bool
	}{
		{
			name: "Valid Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{
							{
								Name:  OdfInfoClusterClaimNamespacedName,
								Value: "namespace/name",
							},
						},
					},
				},
			},
			want:    types.NamespacedName{Namespace: "namespace", Name: "name"},
			wantErr: false,
		},
		{
			name: "Missing Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{},
					},
				},
			},
			want:    types.NamespacedName{},
			wantErr: true,
		},
		{
			name: "Invalid Format for Namespaced Name Claim",
			args: args{
				managedCluster: clusterv1.ManagedCluster{
					Status: clusterv1.ManagedClusterStatus{
						ClusterClaims: []clusterv1.ManagedClusterClaim{
							{
								Name:  OdfInfoClusterClaimNamespacedName,
								Value: "invalidformat",
							},
						},
					},
				},
			},
			want:    types.NamespacedName{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getNamespacedNameForClusterInfo(tt.args.managedCluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNamespacedNameForClusterInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNamespacedNameForClusterInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}
