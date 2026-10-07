//go:build unit
// +build unit

package ramen

import (
	"context"
	"os"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	drpName     = "test-dr-policy"
	mpName      = "mirrorpeer"
	cName1      = "cluster-1"
	cName2      = "cluster-2"
	scName      = "test-storagecluster"
	scNamespace = "test-namespace"
)

func TestDRPolicyReconcile(t *testing.T) {

	mirrorpeer := multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: mpName,
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type: multiclusterv1alpha1.Async,
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: cName1,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      scName,
						Namespace: scNamespace,
					},
				},
				{
					ClusterName: cName2,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      scName,
						Namespace: scNamespace,
					},
				},
			},
		},
		Status: multiclusterv1alpha1.MirrorPeerStatus{
			Phase: multiclusterv1alpha1.Ready,
		},
	}

	drpolicy := ramenv1alpha1.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: drpName,
		},
		Spec: ramenv1alpha1.DRPolicySpec{
			SchedulingInterval: "1h",
			DRClusters:         []string{cName1, cName2},
			ReplicationClassSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					RBDFlattenVolumeReplicationClassLabelKey: RBDFlattenVolumeReplicationClassLabelValue,
				},
			},
		},
	}

	r := getFakeDRPolicyReconciler(&drpolicy, &mirrorpeer)

	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: drpName,
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("DRPolicyReconciler Reconcile() failed. Error: %s", err)
	}

}

func getFakeDRPolicyReconciler(drpolicy *ramenv1alpha1.DRPolicy, mp *multiclusterv1alpha1.MirrorPeer) DRPolicyReconciler {
	scheme := mgrScheme
	os.Setenv("POD_NAMESPACE", "openshift-operators")
	os.Setenv("TOKEN_EXCHANGE_IMAGE", "quay.io/ocs-dev/mco:test")
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName1,
		},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cName2,
		},
	}
	odfClientInfoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: utils.GetEnv("POD_NAMESPACE"),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: map[string]string{
			"cluster-1_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"providerManagedClusterName\":\"cluster-1\"}}",
			"cluster-2_test-storagecluster": "{\"providerInfo\":{\"version\":\"5.0.0\", \"providerManagedClusterName\":\"cluster-2\"}}",
		},
	}

	replicas := int32(1)
	tokenExchangeDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "token-exchange-agent",
			Namespace: "openshift-storage",
			Labels: map[string]string{
				"app": "token-exchange-agent",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "token-exchange-agent",
							Image: "quay.io/ocs-dev/mco:test",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			ReadyReplicas:     1,
			Replicas:          1,
			UpdatedReplicas:   1,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentAvailable,
					Status:  corev1.ConditionTrue,
					Reason:  "MinimumReplicasAvailable",
					Message: "Deployment has minimum availability.",
				},
			},
		},
	}

	tokenExchangeMCV1 := &viewv1beta1.ManagedClusterView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-multicluster-mcv-cluster-1-token-exchange-agent",
			Namespace: cName1,
			Labels: map[string]string{
				"cluster.open-cluster-management.io/backup": "",
				"multicluster.odf.openshift.io/created-by":  "odf-multicluster-managedcluster-controller",
			},
		},
		Spec: viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Name:      "token-exchange-agent",
				Namespace: "openshift-storage",
				Resource:  "Deployment",
			},
		},
		Status: viewv1beta1.ViewStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Processing",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "GetResourceProcessing",
					Message:            "Watching resources successfully",
				},
			},
			Result: runtime.RawExtension{
				Object: tokenExchangeDeployment,
			},
		},
	}

	tokenExchangeMCV2 := &viewv1beta1.ManagedClusterView{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-multicluster-mcv-cluster-2-token-exchange-agent",
			Namespace: cName2,
			Labels: map[string]string{
				"cluster.open-cluster-management.io/backup": "",
				"multicluster.odf.openshift.io/created-by":  "odf-multicluster-managedcluster-controller",
			},
		},
		Spec: viewv1beta1.ViewSpec{
			Scope: viewv1beta1.ViewScope{
				Name:      "token-exchange-agent",
				Namespace: "openshift-storage",
				Resource:  "Deployment",
			},
		},
		Status: viewv1beta1.ViewStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Processing",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "GetResourceProcessing",
					Message:            "Watching resources successfully",
				},
			},
			Result: runtime.RawExtension{
				Object: tokenExchangeDeployment,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(drpolicy, mp, ns1, ns2, odfClientInfoConfigMap, tokenExchangeMCV1, tokenExchangeMCV2).Build()

	r := DRPolicyReconciler{
		HubClient:        fakeClient,
		Scheme:           scheme,
		Logger:           utils.GetLogger(utils.GetZapLogger(true)),
		CurrentNamespace: utils.GetEnv("POD_NAMESPACE"),
	}

	return r
}
