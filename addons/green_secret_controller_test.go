package addons

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	greenSecretName      = "d8433b8cb5b6d99c4d785ebd6082efd19cad50c"
	greenSecretNamespace = "local-cluster"
	greenSecretData      = map[string][]byte{
		"namespace":              []byte("openshift-storage"),
		"secret-origin":          []byte("rook"),
		"storage-cluster-name":   []byte("ocs-storagecluster"),
		utils.SecretStorageIDKey: []byte("{\"cephfs\":\"f9708852fe4cf1f4d5de7e525f1b0aba\",\"rbd\":\"dcd70114947d0bb1f6b96f0dd6a9aaca\"}"),
	}

	secretDataContent = map[string]string{
		"cluster": "b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy",
		"token":   "ZXlKbWMybGtJam9pTjJFelpEWmlPREV0WVRVMVpDMDBOR1psTFRnMFpEQXRORFpqTmpkalpETTVOV05oSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkJXRmRaT1cxcU56STJTMmhCUVdWWFMyWXdaMkpSWjBkQlpVbDNUR3RJVmtaaU5HYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak14TGpFek1TNHhPRE02TXpNd01Dd3hOekl1TXpFdU1UWTNMakUwTXpvek16QXdMREUzTWk0ek1TNDFPUzR4TlRFNk16TXdNQ0lzSW01aGJXVnpjR0ZqWlNJNkltOXdaVzV6YUdsbWRDMXpkRzl5WVdkbEluMD0=",
	}

	storageClusterToUpdate = &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: "openshift-storage",
		},
		Spec: ocsv1.StorageClusterSpec{
			Mirroring: &ocsv1.MirroringSpec{
				PeerSecretNames: []string{},
			},
		},
	}

	syncedGreenSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      greenSecretName,
			Namespace: "openshift-storage",
			Labels: map[string]string{
				utils.CreatedByLabelKey: setup.TokenExchangeName,
			},
		},
		Data: map[string][]byte{},
	}
)

func TestGreenSecretReconciler_Reconcile(t *testing.T) {

	encodedSecretData, err := json.Marshal(secretDataContent)
	if err != nil {
		t.Error("failed marshal secret data")
	}
	greenSecretOnHub := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      greenSecretName,
			Namespace: greenSecretNamespace,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(utils.DestinationLabel),
			},
		},
		Data: map[string][]byte{
			"namespace":              greenSecretData["namespace"],
			"secret-data":            encodedSecretData,
			"secret-origin":          greenSecretData["secret-origin"],
			"storage-cluster-name":   greenSecretData["storage-cluster-name"],
			utils.SecretStorageIDKey: greenSecretData[utils.SecretStorageIDKey],
		},
		Type: corev1.SecretTypeOpaque,
	}

	ctx := context.TODO()
	scheme := runtime.NewScheme()
	err = corev1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add corev1 scheme")
	}
	err = ocsv1.AddToScheme(scheme)
	if err != nil {
		t.Error("failed to add ocsv1 scheme")
	}

	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(greenSecretOnHub).Build()
	fakeSpokeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(storageClusterToUpdate).Build()

	logger := utils.GetLogger(utils.GetZapLogger(true))
	reconciler := &GreenSecretReconciler{
		HubClient:   fakeHubClient,
		SpokeClient: fakeSpokeClient,
		Logger:      logger,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      greenSecretOnHub.Name,
			Namespace: greenSecretOnHub.Namespace,
		},
	}

	// Test cases
	tests := []struct {
		name string
		req  ctrl.Request

		wantErr bool
	}{
		{
			name: "Reconcile GreenSecret successfully",
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

			if tt.name == "Reconcile GreenSecret successfully" {
				reconciledSecret := &corev1.Secret{}
				err = fakeSpokeClient.Get(ctx, types.NamespacedName{
					Name:      syncedGreenSecret.Name,
					Namespace: syncedGreenSecret.Namespace,
				}, reconciledSecret)
				if err != nil {
					t.Errorf("Fetching the secret on spoke cluster failed %v", err)
				}
				if reconciledSecret.Labels[utils.CreatedByLabelKey] != setup.TokenExchangeName {
					t.Errorf("expected label %s to be %s", utils.CreatedByLabelKey, setup.TokenExchangeName)
				}

				expectedStorageIDsData := greenSecretData[utils.SecretStorageIDKey]
				reconciledStorageIDsData, exists := reconciledSecret.Data[utils.SecretStorageIDKey]
				if !exists {
					t.Errorf("StorageIDs key %q not found in reconciled secret", utils.SecretStorageIDKey)
				}

				var expectedStorageIDs, reconciledStorageIDs map[string]string
				if err := json.Unmarshal(expectedStorageIDsData, &expectedStorageIDs); err != nil {
					t.Errorf("Failed to unmarshal expected StorageIDs: %v", err)
				}
				if err := json.Unmarshal(reconciledStorageIDsData, &reconciledStorageIDs); err != nil {
					t.Errorf("Failed to unmarshal reconciled StorageIDs: %v", err)
				}

				if !reflect.DeepEqual(expectedStorageIDs, reconciledStorageIDs) {
					t.Errorf("StorageIDs mismatch - expected: %+v, got: %+v",
						expectedStorageIDs, reconciledStorageIDs)
				}

				// Verify storage cluster update (existing code)
				updatedStorageCluster := &ocsv1.StorageCluster{}
				err = fakeSpokeClient.Get(ctx, types.NamespacedName{
					Name:      storageClusterToUpdate.Name,
					Namespace: storageClusterToUpdate.Namespace,
				}, updatedStorageCluster)
				if err != nil {
					t.Errorf("Fetching the storage cluster failed %v", err)
				}
				if !utils.ContainsString(updatedStorageCluster.Spec.Mirroring.PeerSecretNames, syncedGreenSecret.Name) {
					t.Errorf("expected storage cluster to be updated with secret name %s", syncedGreenSecret.Name)
				}
			}
		})
	}
}
