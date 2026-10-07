/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package s3configuration

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"log/slog"
	"os"
	"strings"
	"testing"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	rmn "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestGetOBCName(t *testing.T) {
	tests := []struct {
		name     string
		s3Config *multiclusterv1alpha1.S3Configuration
		expected string
	}{
		{
			name: "With custom OBC name",
			s3Config: &multiclusterv1alpha1.S3Configuration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-s3config",
				},
				Spec: multiclusterv1alpha1.S3ConfigurationSpec{
					InternalS3: &multiclusterv1alpha1.InternalS3Spec{
						OBCName: "custom-obc-name",
					},
				},
			},
			expected: "custom-obc-name",
		},
		{
			name: "Without custom OBC name",
			s3Config: &multiclusterv1alpha1.S3Configuration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-s3config",
				},
				Spec: multiclusterv1alpha1.S3ConfigurationSpec{
					InternalS3: &multiclusterv1alpha1.InternalS3Spec{},
				},
			},
			expected: "odrbucket-test-s3config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getOBCName(tt.s3Config)
			if result != tt.expected {
				t.Errorf("getOBCName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestReconcile_ResourceNotFound(t *testing.T) {
	r := getFakeS3ConfigurationReconciler(t)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "non-existent",
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() should not return error for not found resource. Error: %s", err)
	}
	if result.RequeueAfter != 0 {
		t.Error("Reconcile() should not requeue for not found resource")
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "test-s3config",
		},
	}

	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() failed. Error: %s", err)
	}

	// Verify finalizer was added
	var updatedS3Config multiclusterv1alpha1.S3Configuration
	err = r.Get(ctx, req.NamespacedName, &updatedS3Config)
	if err != nil {
		t.Errorf("Failed to get S3Configuration. Error: %s", err)
	}

	if !controllerutil.ContainsFinalizer(&updatedS3Config, S3ConfigurationFinalizer) {
		t.Errorf("Finalizer was not added to S3Configuration")
	}
}

func TestReconcile_Deletion(t *testing.T) {
	now := metav1.Now()
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-s3config",
			Finalizers:        []string{S3ConfigurationFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name: "test-s3config",
		},
	}

	// Reconcile deletion - the fake client will delete the object after finalizer removal
	// which causes status update to fail with NotFound error
	_, err := r.Reconcile(ctx, req)

	// The error is expected because after finalizer removal, the object is deleted
	// and status update fails with NotFound
	if err != nil && !errors.IsNotFound(err) {
		t.Errorf("Reconcile() failed with unexpected error. Error: %s", err)
	}
}

func TestEnsureManagedClusterAddOn(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.ensureManagedClusterAddOn(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("ensureManagedClusterAddOn() failed. Error: %s", err)
	}

	// Verify ClusterManagementAddOn was created
	clusterMgmtAddon := &addonapiv1alpha1.ClusterManagementAddOn{}
	err = r.Get(ctx, types.NamespacedName{Name: utils.S3ConfigAddonName}, clusterMgmtAddon)
	if err != nil {
		t.Errorf("Failed to get ClusterManagementAddOn. Error: %s", err)
	}

	if clusterMgmtAddon.Spec.AddOnMeta.DisplayName != "S3 Configuration Addon" {
		t.Errorf("ClusterManagementAddOn DisplayName incorrect. Got: %s", clusterMgmtAddon.Spec.AddOnMeta.DisplayName)
	}

	// Verify ManagedClusterAddOn was created
	managedClusterAddon := &addonapiv1alpha1.ManagedClusterAddOn{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      utils.S3ConfigAddonName,
		Namespace: "cluster1",
	}, managedClusterAddon)
	if err != nil {
		t.Errorf("Failed to get ManagedClusterAddOn. Error: %s", err)
	}

	if managedClusterAddon.Spec.InstallNamespace != "test-namespace" {
		t.Errorf("ManagedClusterAddOn InstallNamespace incorrect. Got: %s", managedClusterAddon.Spec.InstallNamespace)
	}
}

func TestEnsureOBCManifestWork(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
				OBCName:          "custom-obc",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	created, err := r.ensureOBCManifestWork(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("ensureOBCManifestWork() failed. Error: %s", err)
	}

	// Should not be created yet (no Applied condition)
	if created {
		t.Error("ensureOBCManifestWork() should return false when ManifestWork not yet Applied")
	}

	// Verify ManifestWork was created
	mw := &workv1.ManifestWork{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      "s3config-test-s3config-obc",
		Namespace: "cluster1",
	}, mw)
	if err != nil {
		t.Errorf("Failed to get ManifestWork. Error: %s", err)
	}

	if len(mw.Spec.Workload.Manifests) != 1 {
		t.Errorf("ManifestWork should contain 1 manifest, got: %d", len(mw.Spec.Workload.Manifests))
	}

	// Verify the OBC is correctly configured
	if len(mw.Spec.Workload.Manifests) > 0 {
		manifest := mw.Spec.Workload.Manifests[0]
		var obc obv1alpha1.ObjectBucketClaim
		if err := json.Unmarshal(manifest.Raw, &obc); err != nil {
			t.Errorf("Failed to unmarshal OBC from manifest: %v", err)
		} else {
			if obc.Name != "custom-obc" {
				t.Errorf("OBC name incorrect. Got: %s, Want: custom-obc", obc.Name)
			}
			if obc.Namespace != "test-namespace" {
				t.Errorf("OBC namespace incorrect. Got: %s, Want: test-namespace", obc.Namespace)
			}
			if obc.Spec.StorageClassName != "test-storage-class" {
				t.Errorf("OBC StorageClassName incorrect. Got: %s, Want: test-storage-class", obc.Spec.StorageClassName)
			}
		}
	}
}

func TestEnsureOBCManifestWork_Applied(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	// Create ManifestWork with Applied condition
	mw := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3config-test-s3config-obc",
			Namespace: "cluster1",
		},
		Status: workv1.ManifestWorkStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Applied",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config, mw)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	created, err := r.ensureOBCManifestWork(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("ensureOBCManifestWork() failed. Error: %s", err)
	}

	if !created {
		t.Error("ensureOBCManifestWork() should return true when ManifestWork is Applied")
	}
}

func TestValidateSecret(t *testing.T) {
	tests := []struct {
		name      string
		secret    *corev1.Secret
		expectErr bool
	}{
		{
			name: "Valid secret with all required keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					utils.AwsAccessKeyId:     []byte("access-key"),
					utils.AwsSecretAccessKey: []byte("secret-key"),
					utils.S3BucketName:       []byte("test-bucket"),
					utils.S3Endpoint:         []byte("http://test-endpoint"),
					utils.S3Region:           []byte("us-east-1"),
				},
			},
			expectErr: false,
		},
		{
			name: "Missing AWS_ACCESS_KEY_ID",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					utils.AwsSecretAccessKey: []byte("secret-key"),
					utils.S3BucketName:       []byte("test-bucket"),
					utils.S3Endpoint:         []byte("http://test-endpoint"),
					utils.S3Region:           []byte("us-east-1"),
				},
			},
			expectErr: true,
		},
		{
			name: "Missing S3_BUCKET",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					utils.AwsAccessKeyId:     []byte("access-key"),
					utils.AwsSecretAccessKey: []byte("secret-key"),
					utils.S3Endpoint:         []byte("http://test-endpoint"),
					utils.S3Region:           []byte("us-east-1"),
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := getFakeS3ConfigurationReconciler(t, tt.secret)
			ctx := context.TODO()
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

			err := r.validateSecret(ctx, logger, tt.secret)
			if (err != nil) != tt.expectErr {
				t.Errorf("validateSecret() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestEnsureRamenSecret(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-secret",
			Namespace: "cluster1",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("access-key"),
			utils.AwsSecretAccessKey: []byte("secret-key"),
			utils.S3BucketName:       []byte("test-bucket"),
			utils.S3Endpoint:         []byte("http://test-endpoint"),
			utils.S3Region:           []byte("us-east-1"),
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config, sourceSecret)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.ensureRamenSecret(ctx, logger, s3Config, sourceSecret)
	if err != nil {
		t.Errorf("ensureRamenSecret() failed. Error: %s", err)
	}

	// Verify Ramen secret was created
	ramenSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      "test-s3config",
		Namespace: r.CurrentNamespace,
	}, ramenSecret)
	if err != nil {
		t.Errorf("Failed to get Ramen secret. Error: %s", err)
	}

	// Verify secret data
	if string(ramenSecret.Data[utils.AwsAccessKeyId]) != "access-key" {
		t.Errorf("Ramen secret AWS_ACCESS_KEY_ID incorrect. Got: %s", string(ramenSecret.Data[utils.AwsAccessKeyId]))
	}
	if string(ramenSecret.Data[utils.AwsSecretAccessKey]) != "secret-key" {
		t.Errorf("Ramen secret AWS_SECRET_ACCESS_KEY incorrect. Got: %s", string(ramenSecret.Data[utils.AwsSecretAccessKey]))
	}

	// Verify labels and annotations
	if ramenSecret.Labels[utils.CreatedByLabelKey] != utils.S3ConfigAddonName {
		t.Errorf("Ramen secret label incorrect. Got: %s", ramenSecret.Labels[utils.CreatedByLabelKey])
	}
}

func TestEnsureDRClusters(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.ensureDrClusters(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("ensureDrClusters() failed. Error: %s", err)
	}

	// Verify DRClusters were created for both clusters
	for _, clusterName := range s3Config.Spec.ManagedClusters {
		drCluster := &rmn.DRCluster{}
		err = r.Get(ctx, types.NamespacedName{Name: clusterName}, drCluster)
		if err != nil {
			t.Errorf("Failed to get DRCluster for cluster %s. Error: %s", clusterName, err)
		}

		if drCluster.Spec.S3ProfileName != s3Config.Name {
			t.Errorf("DRCluster S3ProfileName incorrect. Got: %s, Want: %s", drCluster.Spec.S3ProfileName, s3Config.Name)
		}
	}
}

func TestReconcilePhases_StatusProgression(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
			UID:  "test-uid",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// First reconcile should set status to Pending and add finalizer
	result, err := r.reconcilePhases(ctx, logger, s3Config)
	// This will error because we don't have all required resources, but that's expected
	if err == nil {
		t.Log("Expected error due to missing resources, but continuing test")
	}

	// Should requeue after adding finalizer
	if result.RequeueAfter == 0 {
		t.Error("Expected RequeueAfter to be set after adding finalizer")
	}

	// Verify finalizer was added
	if !controllerutil.ContainsFinalizer(s3Config, S3ConfigurationFinalizer) {
		t.Error("Finalizer should be added during first reconcile")
	}
}

func TestValidateManagedClustersUniqueness_NoDuplicates(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateManagedClustersUniqueness(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("validateManagedClustersUniqueness() should not return error when no conflicts. Error: %s", err)
	}
}

func TestValidateManagedClustersUniqueness_DuplicateWithinSameConfig(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2", "cluster1"}, // duplicate cluster1
		},
	}

	r := getFakeS3ConfigurationReconciler(t)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateManagedClustersUniqueness(ctx, logger, s3Config)
	if err == nil {
		t.Error("validateManagedClustersUniqueness() should return error for duplicate clusters within same config")
	}
}

func TestValidateManagedClustersUniqueness_ConflictWithOtherS3Config(t *testing.T) {
	// Existing S3Configuration
	existingS3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "existing-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	// New S3Configuration trying to use cluster2 (already used by existingS3Config)
	newS3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "new-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster2",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster2", "cluster3"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, existingS3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateManagedClustersUniqueness(ctx, logger, newS3Config)
	if err == nil {
		t.Error("validateManagedClustersUniqueness() should return error when cluster is used by another S3Configuration")
	}
	if err != nil && !strings.Contains(err.Error(), "cluster2") {
		t.Errorf("Error message should mention conflicting cluster. Got: %s", err.Error())
	}
}

func TestValidateManagedClustersUniqueness_NoConflictWithSelf(t *testing.T) {
	// Existing S3Configuration
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			InternalS3: &multiclusterv1alpha1.InternalS3Spec{
				ManagedCluster:   "cluster1",
				StorageClassName: "test-storage-class",
				Namespace:        "test-namespace",
			},
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Validating the same S3Config should not report conflict with itself
	err := r.validateManagedClustersUniqueness(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("validateManagedClustersUniqueness() should not report conflict with itself. Error: %s", err)
	}
}

func TestValidateNoDRPolicyUsesClusters_NoPolicies(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateNoDRPolicyUsesClusters(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("validateNoDRPolicyUsesClusters() should not error when no DRPolicies exist. Error: %s", err)
	}
}

func TestValidateNoDRPolicyUsesClusters_PolicyUsesCluster(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	drPolicy := &rmn.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-drpolicy",
		},
		Spec: rmn.DRPolicySpec{
			DRClusters: []string{"cluster1", "cluster3"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config, drPolicy)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateNoDRPolicyUsesClusters(ctx, logger, s3Config)
	if err == nil {
		t.Error("validateNoDRPolicyUsesClusters() should return error when DRPolicy uses cluster")
	}
	if err != nil && !strings.Contains(err.Error(), "cluster1") {
		t.Errorf("Error should mention the conflicting cluster. Got: %s", err.Error())
	}
}

func TestValidateNoDRPolicyUsesClusters_PolicyUsesNoOverlappingClusters(t *testing.T) {
	s3Config := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-s3config",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	drPolicy := &rmn.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-drpolicy",
		},
		Spec: rmn.DRPolicySpec{
			DRClusters: []string{"cluster3", "cluster4"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config, drPolicy)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.validateNoDRPolicyUsesClusters(ctx, logger, s3Config)
	if err != nil {
		t.Errorf("validateNoDRPolicyUsesClusters() should not error when DRPolicy uses different clusters. Error: %s", err)
	}
}

func TestFindS3ConfigsForDRPolicy(t *testing.T) {
	s3Config1 := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "s3config1",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			ManagedClusters: []string{"cluster1", "cluster2"},
		},
	}

	s3Config2 := &multiclusterv1alpha1.S3Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "s3config2",
		},
		Spec: multiclusterv1alpha1.S3ConfigurationSpec{
			ManagedClusters: []string{"cluster3", "cluster4"},
		},
	}

	drPolicy := &rmn.DRPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-drpolicy",
		},
		Spec: rmn.DRPolicySpec{
			DRClusters: []string{"cluster1", "cluster3"},
		},
	}

	r := getFakeS3ConfigurationReconciler(t, s3Config1, s3Config2, drPolicy)
	ctx := context.TODO()

	requests := r.findS3ConfigsForDRPolicy(ctx, drPolicy)

	// Should return both s3config1 and s3config2 since they both have overlapping clusters
	if len(requests) != 2 {
		t.Errorf("Expected 2 reconcile requests, got %d", len(requests))
	}
}

// Helper function to create a fake reconciler with optional initial objects
func getFakeS3ConfigurationReconciler(t *testing.T, initObjs ...runtime.Object) S3ConfigurationReconciler {
	scheme := runtime.NewScheme()
	err := multiclusterv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = addonapiv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = workv1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = rmn.AddToScheme(scheme)
	assert.NoError(t, err)
	err = obv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(initObjs...).
		WithStatusSubresource(&multiclusterv1alpha1.S3Configuration{}).
		Build()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return S3ConfigurationReconciler{
		Client:           client,
		Scheme:           scheme,
		Logger:           logger,
		CurrentNamespace: "openshift-operators",
	}
}
