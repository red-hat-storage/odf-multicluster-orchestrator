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

package s3config

import (
	"context"
	"github.com/stretchr/testify/assert"
	"log/slog"
	"os"
	"testing"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcile_OBCNotFound(t *testing.T) {
	r := getFakeS3ConfigAddonReconciler(t)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent-obc",
			Namespace: "test-namespace",
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() should not return error for not found OBC. Error: %s", err)
	}
	if result.RequeueAfter != 0 {
		t.Error("Reconcile() should not requeue for not found OBC")
	}
}

func TestReconcile_OBCNotBound(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				utils.S3ConfigurationNameAnnotationKey: "test-s3config",
			},
		},
		Status: obv1alpha1.ObjectBucketClaimStatus{
			Phase: obv1alpha1.ObjectBucketClaimStatusPhasePending,
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obc)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	// First reconcile adds finalizer
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() should not return error for adding finalizer. Error: %s", err)
	}
	if !result.Requeue {
		t.Error("Reconcile() should requeue after adding finalizer")
	}

	// Second reconcile checks OBC status
	result, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() should not return error for unbound OBC. Error: %s", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("Reconcile() should requeue for unbound OBC")
	}
}

func TestReconcile_OBCMissingAnnotation(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
			// No S3Configuration annotation
		},
		Status: obv1alpha1.ObjectBucketClaimStatus{
			Phase: obv1alpha1.ObjectBucketClaimStatusPhaseBound,
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obc)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	// First reconcile adds finalizer
	_, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() should not return error for adding finalizer. Error: %s", err)
	}

	// Second reconcile should fail due to missing annotation
	_, err = r.Reconcile(ctx, req)
	if err == nil {
		t.Error("Reconcile() should return error when S3Configuration annotation is missing")
	}
}

func TestReconcile_SuccessfulSync(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				utils.S3ConfigurationNameAnnotationKey: "test-s3config",
			},
		},
		Status: obv1alpha1.ObjectBucketClaimStatus{
			Phase: obv1alpha1.ObjectBucketClaimStatusPhaseBound,
		},
	}

	obcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("test-access-key"),
			utils.AwsSecretAccessKey: []byte("test-secret-key"),
		},
	}

	obcConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			S3BucketName:   "test-bucket",
			S3BucketRegion: "us-east-1",
		},
	}

	s3Route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      S3RouteName,
			Namespace: "test-namespace",
		},
		Spec: routev1.RouteSpec{
			Host: "s3.example.com",
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obc, obcSecret, obcConfigMap, s3Route)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	// First reconcile adds finalizer
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() failed on adding finalizer. Error: %s", err)
	}
	if !result.Requeue {
		t.Error("Reconcile() should requeue after adding finalizer")
	}

	// Second reconcile syncs the secret
	result, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() failed. Error: %s", err)
	}
	if result.RequeueAfter != 0 {
		t.Error("Reconcile() should not requeue on successful sync")
	}

	// Verify secret was created on hub
	hubSecret := &corev1.Secret{}
	err = r.HubClient.Get(ctx, types.NamespacedName{
		Name:      "test-obc",
		Namespace: "cluster1",
	}, hubSecret)
	if err != nil {
		t.Errorf("Failed to get hub secret. Error: %s", err)
	}

	// Verify secret data
	if string(hubSecret.Data[utils.AwsAccessKeyId]) != "test-access-key" {
		t.Errorf("Hub secret AWS_ACCESS_KEY_ID incorrect. Got: %s", string(hubSecret.Data[utils.AwsAccessKeyId]))
	}
	if string(hubSecret.Data[utils.AwsSecretAccessKey]) != "test-secret-key" {
		t.Errorf("Hub secret AWS_SECRET_ACCESS_KEY incorrect. Got: %s", string(hubSecret.Data[utils.AwsSecretAccessKey]))
	}
	if string(hubSecret.Data[utils.S3BucketName]) != "test-bucket" {
		t.Errorf("Hub secret S3_BUCKET incorrect. Got: %s", string(hubSecret.Data[utils.S3BucketName]))
	}
	if string(hubSecret.Data[utils.S3Region]) != "us-east-1" {
		t.Errorf("Hub secret S3_REGION incorrect. Got: %s", string(hubSecret.Data[utils.S3Region]))
	}
	if string(hubSecret.Data[utils.S3Endpoint]) != "https://s3.example.com" {
		t.Errorf("Hub secret S3_ENDPOINT incorrect. Got: %s", string(hubSecret.Data[utils.S3Endpoint]))
	}

	// Verify label
	if hubSecret.Labels[utils.CreatedByLabelKey] != utils.S3ConfigAddonName {
		t.Errorf("Hub secret label incorrect. Got: %s", hubSecret.Labels[utils.CreatedByLabelKey])
	}
}

func TestSyncSecretToHub_DefaultRegion(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	obcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("test-access-key"),
			utils.AwsSecretAccessKey: []byte("test-secret-key"),
		},
	}

	obcConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			S3BucketName: "test-bucket",
			// S3BucketRegion not set, should default to "noobaa"
		},
	}

	s3Route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      S3RouteName,
			Namespace: "test-namespace",
		},
		Spec: routev1.RouteSpec{
			Host: "s3.example.com",
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obcSecret, obcConfigMap, s3Route)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.syncSecretToHub(ctx, obc, logger)
	if err != nil {
		t.Errorf("syncSecretToHub() failed. Error: %s", err)
	}

	// Verify secret was created on hub with default region
	hubSecret := &corev1.Secret{}
	err = r.HubClient.Get(ctx, types.NamespacedName{
		Name:      "test-obc",
		Namespace: "cluster1",
	}, hubSecret)
	if err != nil {
		t.Errorf("Failed to get hub secret. Error: %s", err)
	}

	if string(hubSecret.Data[utils.S3Region]) != DefaultS3Region {
		t.Errorf("Hub secret should have default region. Got: %s, Want: %s",
			string(hubSecret.Data[utils.S3Region]), DefaultS3Region)
	}
}

func TestSyncSecretToHub_UpdateExisting(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	obcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("new-access-key"),
			utils.AwsSecretAccessKey: []byte("new-secret-key"),
		},
	}

	obcConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			S3BucketName:   "test-bucket",
			S3BucketRegion: "us-west-2",
		},
	}

	s3Route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      S3RouteName,
			Namespace: "test-namespace",
		},
		Spec: routev1.RouteSpec{
			Host: "s3.example.com",
		},
	}

	// Pre-existing secret on hub
	existingHubSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "cluster1",
			Labels: map[string]string{
				utils.CreatedByLabelKey: utils.S3ConfigAddonName,
			},
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("old-access-key"),
			utils.AwsSecretAccessKey: []byte("old-secret-key"),
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obcSecret, obcConfigMap, s3Route, existingHubSecret)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.syncSecretToHub(ctx, obc, logger)
	if err != nil {
		t.Errorf("syncSecretToHub() failed. Error: %s", err)
	}

	// Verify secret was updated on hub
	hubSecret := &corev1.Secret{}
	err = r.HubClient.Get(ctx, types.NamespacedName{
		Name:      "test-obc",
		Namespace: "cluster1",
	}, hubSecret)
	if err != nil {
		t.Errorf("Failed to get hub secret. Error: %s", err)
	}

	// Verify data was updated
	if string(hubSecret.Data[utils.AwsAccessKeyId]) != "new-access-key" {
		t.Errorf("Hub secret should be updated. Got: %s, Want: new-access-key",
			string(hubSecret.Data[utils.AwsAccessKeyId]))
	}
	if string(hubSecret.Data[utils.S3Region]) != "us-west-2" {
		t.Errorf("Hub secret region should be updated. Got: %s, Want: us-west-2",
			string(hubSecret.Data[utils.S3Region]))
	}
}

func TestSyncSecretToHub_MissingSecret(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	r := getFakeS3ConfigAddonReconciler(t)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.syncSecretToHub(ctx, obc, logger)
	if err == nil {
		t.Error("syncSecretToHub() should return error when OBC secret is missing")
	}
}

func TestSyncSecretToHub_MissingConfigMap(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	obcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("test-access-key"),
			utils.AwsSecretAccessKey: []byte("test-secret-key"),
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obcSecret)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.syncSecretToHub(ctx, obc, logger)
	if err == nil {
		t.Error("syncSecretToHub() should return error when OBC ConfigMap is missing")
	}
}

func TestSyncSecretToHub_MissingRoute(t *testing.T) {
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	obcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId:     []byte("test-access-key"),
			utils.AwsSecretAccessKey: []byte("test-secret-key"),
		},
	}

	obcConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			S3BucketName: "test-bucket",
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obcSecret, obcConfigMap)
	ctx := context.TODO()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	err := r.syncSecretToHub(ctx, obc, logger)
	if err == nil {
		t.Error("syncSecretToHub() should return error when S3 Route is missing")
	}
}

func TestReconcile_OBCDeletion(t *testing.T) {
	now := metav1.Now()
	obc := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				utils.S3ConfigurationNameAnnotationKey: "test-s3config",
			},
			Finalizers:        []string{S3ConfigAddonFinalizer},
			DeletionTimestamp: &now,
		},
		Status: obv1alpha1.ObjectBucketClaimStatus{
			Phase: obv1alpha1.ObjectBucketClaimStatusPhaseBound,
		},
	}

	// Hub secret that should be deleted
	hubSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-obc",
			Namespace: "cluster1",
			Labels: map[string]string{
				utils.CreatedByLabelKey: utils.S3ConfigAddonName,
			},
		},
		Data: map[string][]byte{
			utils.AwsAccessKeyId: []byte("test-key"),
		},
	}

	r := getFakeS3ConfigAddonReconciler(t, obc, hubSecret)
	ctx := context.TODO()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-obc",
			Namespace: "test-namespace",
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("Reconcile() failed during deletion. Error: %s", err)
	}
	if result.RequeueAfter != 0 || result.Requeue {
		t.Error("Reconcile() should not requeue after successful deletion cleanup")
	}

	// Verify hub secret was deleted
	foundSecret := &corev1.Secret{}
	err = r.HubClient.Get(ctx, types.NamespacedName{
		Name:      "test-obc",
		Namespace: "cluster1",
	}, foundSecret)
	if err == nil {
		t.Error("Hub secret should have been deleted")
	}

	// OBC will be fully deleted after finalizer is removed (since DeletionTimestamp is set)
	// This is expected Kubernetes behavior - verify the OBC is gone
	updatedOBC := &obv1alpha1.ObjectBucketClaim{}
	err = r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      "test-obc",
		Namespace: "test-namespace",
	}, updatedOBC)
	if err == nil {
		t.Error("OBC should have been deleted after finalizer removal")
	}
}

// Helper function to create a fake reconciler with optional initial objects
func getFakeS3ConfigAddonReconciler(t *testing.T, initObjs ...runtime.Object) S3ConfigAddonReconciler {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = obv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = routev1.AddToScheme(scheme)
	assert.NoError(t, err)

	// Separate objects for spoke and hub clients
	spokeObjs := []runtime.Object{}
	hubObjs := []runtime.Object{}

	for _, obj := range initObjs {
		switch o := obj.(type) {
		case *obv1alpha1.ObjectBucketClaim, *corev1.ConfigMap, *routev1.Route:
			spokeObjs = append(spokeObjs, obj)
		case *corev1.Secret:
			// Secrets in cluster1 namespace go to hub, others to spoke
			if o.Namespace == "cluster1" {
				hubObjs = append(hubObjs, obj)
			} else {
				spokeObjs = append(spokeObjs, obj)
			}
		default:
			spokeObjs = append(spokeObjs, obj)
		}
	}

	spokeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(spokeObjs...).
		Build()

	hubClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(hubObjs...).
		Build()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return S3ConfigAddonReconciler{
		Scheme:           scheme,
		HubClient:        hubClient,
		SpokeClient:      spokeClient,
		SpokeClusterName: "cluster1",
		Logger:           logger,
	}
}
