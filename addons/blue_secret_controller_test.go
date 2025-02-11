package addons

import (
	"context"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Define the test secret and related data
var (
	spokeClusterName = "test-cluster"
	sData            = map[string][]byte{
		"token":   []byte("ZXlKbWMybGtJam9pTjJFelpEWmlPREV0WVRVMVpDMDBOR1psTFRnMFpEQXRORFpqTmpkalpETTVOV05oSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkJXRmRaT1cxcU56STJTMmhCUVdWWFMyWXdaMkpSWjBkQlpVbDNUR3RJVmtaaU5HYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak14TGpFek1TNHhPRE02TXpNd01Dd3hOekl1TXpFdU1UWTNMakUwTXpvek16QXdMREUzTWk0ek1TNDFPUzR4TlRFNk16TXdNQ0lzSW01aGJXVnpjR0ZqWlNJNkltOXdaVzV6YUdsbWRDMXpkRzl5WVdkbEluMD0="),
		"cluster": []byte("b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy"),
	}

	managedClusterSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-peer-token-test-storagecluster-cephcluster",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"app": "rook",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "ceph.rook.io/v1",
					Kind:       "CephCluster",
					Name:       "test-storagecluster-cephcluster",
					UID:        "390fa888-59c0-4212-8e3b-e426e52c800e",
				},
			},
		},
		Data: sData,
		Type: "kubernetes.io/rook",
	}

	storageCluster = &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: "test-namespace",
			UID:       "12345678-1234-1234-1234-123456789012",
		},
	}

	cephCluster      = GetTestCephCluster()
	hubClusterSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.CreateUniqueSecretName(spokeClusterName, managedClusterSecret.Namespace, storageCluster.Name),
			Namespace: spokeClusterName,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(utils.SourceLabel),
			},
		},
		Data: sData,
		Type: "kubernetes.io/rook",
	}
)

func TestBlueSecretReconciler_Reconcile(t *testing.T) {
	ctx := context.TODO()
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add corev1 scheme")
	}
	err = ocsv1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add ocsv1 scheme")
	}
	err = rookv1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add rookv1 scheme")
	}

	// Create fake clients
	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects().Build()
	fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(managedClusterSecret, storageCluster, cephCluster).Build()

	logger := utils.GetLogger(utils.GetZapLogger(true))
	reconciler := &BlueSecretReconciler{
		HubClient:        fakeHubClient,
		SpokeClient:      fakeSpokeClient,
		SpokeClusterName: "test-cluster",
		Logger:           logger,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      managedClusterSecret.Name,
			Namespace: managedClusterSecret.Namespace,
		},
	}

	// Test cases
	tests := []struct {
		name    string
		req     ctrl.Request
		wantErr bool
	}{
		{
			name:    "Reconcile BlueSecret successfully",
			req:     req,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reconciler.Reconcile(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			reconciledSecret := &corev1.Secret{}
			err = fakeHubClient.Get(ctx, types.NamespacedName{
				Name:      hubClusterSecret.Name,
				Namespace: hubClusterSecret.Namespace,
			}, reconciledSecret)
			if err != nil {
				t.Errorf("Fetching the secret on hub failed %v", err)
			}
			if reconciledSecret.Labels[utils.SecretLabelTypeKey] != string(utils.SourceLabel) {
				t.Errorf("expected label %s to be %s", utils.SecretLabelTypeKey, string(utils.SourceLabel))
			}

		})
	}
}
