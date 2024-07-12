package addons

import (
	"context"
	"testing"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	s3SecretName      = "test-obc"
	s3SecretNamespace = "openshift-storage"
	s3SecretData      = map[string][]byte{
		utils.AwsAccessKeyId:     []byte("test-access-key-id"),
		utils.AwsSecretAccessKey: []byte("test-secret-access-key"),
	}
	managedS3Secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s3SecretName,
			Namespace: s3SecretNamespace,
		},
		Data: s3SecretData,
		Type: corev1.SecretTypeOpaque,
	}

	configMapData = map[string]string{
		S3BucketName:   "test-bucket",
		S3BucketRegion: "us-east-1",
	}
	managedConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s3SecretName,
			Namespace: s3SecretNamespace,
		},
		Data: configMapData,
	}

	storageClusterOnManagedCluster = &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: s3SecretNamespace,
		},
	}

	route = &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      S3RouteName,
			Namespace: s3SecretNamespace,
		},
		Spec: routev1.RouteSpec{
			Host: "test-host",
		},
	}

	objectBucketClaim = &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s3SecretName,
			Namespace: s3SecretNamespace,
		},
		Status: obv1alpha1.ObjectBucketClaimStatus{
			Phase: obv1alpha1.ObjectBucketClaimStatusPhaseBound,
		},
	}

	mirrorPeerItems = []multiclusterv1alpha1.PeerRef{
		{
			ClusterName: "cluster1",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      "ocs-storagecluster",
				Namespace: "openshift-storage",
			},
		},
		{
			ClusterName: "cluster2",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      "ocs-storagecluster",
				Namespace: "openshift-storage",
			},
		},
	}
	mirrorPeer = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mirrorpeer",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type:  "async",
			Items: mirrorPeerItems,
		},
	}
)

func TestS3SecretReconciler_Reconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add corev1 scheme")
	}
	err = obv1alpha1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add obv1alpha1 scheme")
	}
	err = routev1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add routev1 scheme")
	}
	err = multiclusterv1alpha1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add multiclusterv1alpha1 scheme")
	}
	err = rookv1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add rookv1 scheme")
	}
	err = ocsv1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add ocsv1 scheme")
	}

	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mirrorPeer).Build()
	fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(managedS3Secret, managedConfigMap, route, objectBucketClaim, storageClusterOnManagedCluster).Build()

	logger := utils.GetLogger(utils.GetZapLogger(true))
	reconciler := &S3SecretReconciler{
		HubClient:        fakeHubClient,
		SpokeClient:      fakeSpokeClient,
		SpokeClusterName: "cluster1",
		Logger:           logger,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      objectBucketClaim.Name,
			Namespace: objectBucketClaim.Namespace,
		},
	}

	// Test cases
	tests := []struct {
		name    string
		req     ctrl.Request
		wantErr bool
	}{
		{
			name: "Reconcile OBC successfully",
			req:  req,

			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			_, err := reconciler.Reconcile(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.name == "Reconcile OBC successfully" {
				reconciledSecret := &corev1.Secret{}
				err = fakeHubClient.Get(ctx, types.NamespacedName{
					Name:      utils.CreateUniqueSecretName(reconciler.SpokeClusterName, storageClusterOnManagedCluster.Namespace, storageClusterOnManagedCluster.Name, utils.S3ProfilePrefix),
					Namespace: reconciler.SpokeClusterName,
				}, reconciledSecret)
				if err != nil {
					t.Errorf("Fetching the secret on hub cluster failed %v", err)
				}
				if reconciledSecret.Labels[utils.SecretLabelTypeKey] != string(utils.InternalLabel) {
					t.Errorf("expected label %s to be %s", utils.SecretLabelTypeKey, string(utils.InternalLabel))
				}
			}
		})
	}
}
