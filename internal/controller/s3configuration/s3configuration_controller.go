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
	"fmt"
	"log/slog"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// S3ConfigurationFinalizer is the finalizer for S3Configuration
	S3ConfigurationFinalizer = "multicluster.odf.openshift.io/s3configuration"

	// OBCNamePrefix is the default prefix for OBC names
	OBCNamePrefix = "odrbucket"
)

// S3ConfigurationReconciler reconciles a S3Configuration object
type S3ConfigurationReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Logger           *slog.Logger
	CurrentNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3ConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to watch secrets with our CreatedByLabelKey in managed cluster namespaces
	secretPredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return false
		}
		// Watch secrets with our label in managed cluster namespaces or Ramen namespace
		if labels := secret.GetLabels(); labels != nil {
			if createdBy, exists := labels[utils.CreatedByLabelKey]; exists {
				return createdBy == utils.S3ConfigAddonName
			}
		}
		return false
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.S3Configuration{}).
		Owns(&addonapiv1alpha1.ManagedClusterAddOn{}).
		Owns(&addonapiv1alpha1.ClusterManagementAddOn{}).
		Owns(&workv1.ManifestWork{}).
		Owns(&rmn.DRCluster{}).
		Owns(&corev1.Secret{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findS3ConfigForSecret),
			builder.WithPredicates(secretPredicate),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findS3ConfigsForRamenConfigMap),
			builder.WithPredicates(utils.NamePredicate(utils.RamenHubOperatorConfigName), utils.NamespacePredicate(r.CurrentNamespace)),
		).
		Watches(
			&rmn.DRPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.findS3ConfigsForDRPolicy),
		).
		Named("s3configuration").
		Complete(r)
}

// findS3ConfigForSecret finds S3Configurations that should be reconciled when a secret changes
func (r *S3ConfigurationReconciler) findS3ConfigForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	// Get the secret's namespace - this should be a managed cluster namespace
	clusterNamespace := secret.GetNamespace()

	// List all S3Configurations
	s3ConfigList := &multiclusterv1alpha1.S3ConfigurationList{}
	if err := r.List(ctx, s3ConfigList); err != nil {
		r.Logger.Error("Failed to list S3Configurations for secret watch", "error", err)
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, s3Config := range s3ConfigList.Items {
		// Check if this S3Config uses this cluster
		if s3Config.Spec.InternalS3 != nil && s3Config.Spec.InternalS3.ProviderCluster == clusterNamespace {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: s3Config.Name,
				},
			})
		}
	}

	return requests
}

// findS3ConfigsForRamenConfigMap finds all S3Configurations when Ramen ConfigMap changes
func (r *S3ConfigurationReconciler) findS3ConfigsForRamenConfigMap(ctx context.Context, configMap client.Object) []reconcile.Request {
	// List all S3Configurations
	s3ConfigList := &multiclusterv1alpha1.S3ConfigurationList{}
	if err := r.List(ctx, s3ConfigList); err != nil {
		r.Logger.Error("Failed to list S3Configurations for Ramen ConfigMap watch", "error", err)
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, s3Config := range s3ConfigList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: s3Config.Name,
			},
		})
	}

	return requests
}

// findS3ConfigsForDRPolicy finds S3Configurations that should be reconciled when DRPolicy changes
func (r *S3ConfigurationReconciler) findS3ConfigsForDRPolicy(ctx context.Context, drPolicy client.Object) []reconcile.Request {
	policy, ok := drPolicy.(*rmn.DRPolicy)
	if !ok {
		return []reconcile.Request{}
	}

	// List all S3Configurations
	s3ConfigList := &multiclusterv1alpha1.S3ConfigurationList{}
	if err := r.List(ctx, s3ConfigList); err != nil {
		r.Logger.Error("Failed to list S3Configurations for DRPolicy watch", "error", err)
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	// Find S3Configurations that use any of the DRPolicy's clusters
	for _, s3Config := range s3ConfigList.Items {
		for _, managedCluster := range s3Config.Spec.ManagedClusters {
			for _, drCluster := range policy.Spec.DRClusters {
				if managedCluster == drCluster {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: s3Config.Name,
						},
					})
					break
				}
			}
		}
	}

	return requests
}

// S3Configuration resource RBAC
// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=s3configurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=s3configurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=s3configurations/finalizers,verbs=update

// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks/status,verbs=get

// +kubebuilder:rbac:groups=ramendr.openshift.io,resources=drclusters,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ramendr.openshift.io,resources=drpolicies,verbs=get;list;watch

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *S3ConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("s3configuration", req.Name)
	logger.Info("Reconciling S3Configuration")

	// Fetch the S3Configuration instance
	s3Config := &multiclusterv1alpha1.S3Configuration{}
	if err := r.Get(ctx, req.NamespacedName, s3Config); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("S3Configuration resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to get S3Configuration", "error", err)
		return ctrl.Result{}, err
	}

	result, reconcileErr := r.reconcilePhases(ctx, logger, s3Config)

	statusErr := r.Client.Status().Update(ctx, s3Config)
	if statusErr != nil {
		logger.Error("Failed to update S3Config status.", "error", statusErr)
	}
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	} else if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	return result, nil
}

func (r *S3ConfigurationReconciler) reconcilePhases(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) (reconcile.Result, error) {

	if !s3Config.DeletionTimestamp.IsZero() {
		return r.deletionPhase(ctx, logger, s3Config)
	}

	s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhasePending
	// Add finalizer if not present
	if controllerutil.AddFinalizer(s3Config, S3ConfigurationFinalizer) {
		if err := r.Update(ctx, s3Config); err != nil {
			logger.Error("Failed to add finalizer", "error", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Validate that managedClusters are not used by other S3Configurations
	if err := r.validateManagedClustersUniqueness(ctx, logger, s3Config); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		s3Config.Status.Message = err.Error()
		logger.Error("Validation failed", "error", err)
		return ctrl.Result{}, err
	}

	secret := &corev1.Secret{}
	if s3Config.Spec.InternalS3 != nil {
		if res, err := r.reconcileInternalS3Type(ctx, s3Config, logger); err != nil || !res.IsZero() {
			return res, err
		}
		secret.Name = getOBCName(s3Config) // Use OBC name as secret name
		secret.Namespace = s3Config.Spec.InternalS3.ProviderCluster

		// Wait for addon to sync secret
		if err := r.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Secret not yet created by addon", "secret", client.ObjectKeyFromObject(secret))
				return ctrl.Result{}, fmt.Errorf("secret not found: %w", err)
			}
			logger.Error("Failed to get secret", "secret", client.ObjectKeyFromObject(secret), "error", err)
			return ctrl.Result{}, err
		}
	} else if s3Config.Spec.ExternalS3 != nil {
		// External S3: the user has already created the secret on the hub.
		secret.Name = s3Config.Spec.ExternalS3.SecretRef.Name
		secret.Namespace = s3Config.Spec.ExternalS3.SecretRef.Namespace
	}

	if err := r.validateSecret(ctx, logger, secret); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		return ctrl.Result{}, err
	}

	if err := r.ensureRamenSecret(ctx, logger, s3Config, secret); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		return ctrl.Result{}, err
	}

	if err := r.addS3ProfileToRamenConfig(ctx, logger, s3Config, secret); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		return ctrl.Result{}, err
	}

	if err := r.ensureDrClusters(ctx, logger, s3Config); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		return ctrl.Result{}, err
	}

	// Phase 7: Update status to Ready
	s3Config.Status.ConfiguredClusters = s3Config.Spec.ManagedClusters
	s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseReady
	s3Config.Status.Message = "S3 configured successfully"

	logger.Info("S3 configuration completed successfully")

	return ctrl.Result{}, nil
}

// deletionPhase handles cleanup when S3Configuration is being deleted
func (r *S3ConfigurationReconciler) deletionPhase(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) (ctrl.Result, error) {
	logger.Info("Handling deletion of S3Configuration")

	if controllerutil.ContainsFinalizer(s3Config, S3ConfigurationFinalizer) {
		// Check if any DRPolicy uses the managedClusters from this S3Configuration
		if err := r.validateNoDRPolicyUsesClusters(ctx, logger, s3Config); err != nil {
			logger.Error("Cannot delete S3Configuration: clusters are in use by DRPolicy", "error", err)
			s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
			s3Config.Status.Message = fmt.Sprintf("Cannot delete: %v", err)
			return ctrl.Result{}, err
		}

		// Cleanup steps:
		// 1. Remove S3 profile from Ramen ConfigMap
		if err := r.removeS3ProfileFromRamenConfig(ctx, logger, s3Config); err != nil {
			logger.Error("Failed to remove S3 profile from Ramen ConfigMap", "error", err)
			return ctrl.Result{}, err
		}

		// 2. Delete ManifestWork if it exists (this will delete OBC on spoke, triggering addon finalizer cleanup)
		//    Wait for ManifestWork deletion before removing finalizer to ensure addon has time to clean up hub secrets
		if s3Config.Spec.InternalS3 != nil {
			manifestWorkName := fmt.Sprintf("s3config-%s-obc", s3Config.Name)
			providerCluster := s3Config.Spec.InternalS3.ProviderCluster

			mw := &workv1.ManifestWork{}
			mw.Name = manifestWorkName
			mw.Namespace = providerCluster
			if err := r.Get(ctx, client.ObjectKeyFromObject(mw), mw); err == nil {
				// ManifestWork exists, delete it and wait for completion
				logger.Info("Deleting ManifestWork to trigger OBC cleanup", "manifestWork", manifestWorkName, "cluster", providerCluster)
				if err := r.Delete(ctx, mw); err != nil {
					logger.Error("Failed to delete ManifestWork", "error", err)
					return ctrl.Result{}, err
				}
				// Requeue to wait for ManifestWork deletion to complete
				// This ensures OBC is deleted and addon finalizer runs to clean up hub secret
				logger.Info("Waiting for ManifestWork deletion to complete")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			} else if !errors.IsNotFound(err) {
				logger.Error("Failed to get ManifestWork", "error", err)
				return ctrl.Result{}, err
			}
			// ManifestWork already deleted, OBC cleanup complete

			// 3. Clean up ManagedClusterAddOn if this is the last S3Config using this cluster
			if err := r.cleanupManagedClusterAddOn(ctx, logger, providerCluster, s3Config.Name); err != nil {
				logger.Error("Failed to cleanup ManagedClusterAddOn", "error", err)
				return ctrl.Result{}, err
			}
		}

		// 3. Note: DRClusters, ClusterManagementAddOn, and Ramen secrets
		//    will be deleted automatically via owner references and garbage collection.

		logger.Info("Cleanup completed, removing finalizer")
		controllerutil.RemoveFinalizer(s3Config, S3ConfigurationFinalizer)
		if err := r.Update(ctx, s3Config); err != nil {
			logger.Error("Failed to remove finalizer", "error", err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileInternalS3Type handles ODF-managed internal S3
func (r *S3ConfigurationReconciler) reconcileInternalS3Type(ctx context.Context, s3Config *multiclusterv1alpha1.S3Configuration, logger *slog.Logger) (ctrl.Result, error) {
	logger.Info("Reconciling InternalS3 configuration")

	s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseConfiguring

	// Ensure addon is deployed on the cluster where OBC will be created
	if err := r.ensureManagedClusterAddOn(ctx, logger, s3Config); err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		s3Config.Status.Message = fmt.Sprintf("Failed to deploy addon: %v", err)
		return ctrl.Result{}, err
	}

	obcCreated, err := r.ensureOBCManifestWork(ctx, logger, s3Config)
	if err != nil {
		s3Config.Status.Phase = multiclusterv1alpha1.S3ConfigurationPhaseFailed
		s3Config.Status.Message = fmt.Sprintf("Failed to create OBC ManifestWork: %v", err)
		return ctrl.Result{}, err
	}
	if !obcCreated {
		logger.Info("Waiting for OBC ManifestWork to be applied")
		return ctrl.Result{}, fmt.Errorf("waiting for OBC to be created")
	}
	return ctrl.Result{}, nil
}

// ensureManagedClusterAddOn ensures the S3Config addon is deployed on the provider cluster
func (r *S3ConfigurationReconciler) ensureManagedClusterAddOn(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) error {
	providerCluster := s3Config.Spec.InternalS3.ProviderCluster
	logger.Info("Ensuring S3Config addon", "cluster", providerCluster)

	// Step 1: Create/Update ClusterManagementAddOn (hub-level resource)
	clusterManagementAddOn := &addonapiv1alpha1.ClusterManagementAddOn{}
	clusterManagementAddOn.Name = utils.S3ConfigAddonName

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, clusterManagementAddOn, func() error {
		if err := controllerutil.SetOwnerReference(s3Config, clusterManagementAddOn, r.Scheme); err != nil {
			return err
		}
		utils.AddLabel(clusterManagementAddOn, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
		clusterManagementAddOn.Spec.AddOnMeta.DisplayName = "S3 Configuration Addon"
		clusterManagementAddOn.Spec.AddOnMeta.Description = "Syncs S3Configuration OBC secrets from spoke to hub"
		clusterManagementAddOn.Spec.InstallStrategy.Type = "Manual"

		return nil
	}); err != nil {
		logger.Error("Failed to create/update ClusterManagementAddOn", "error", err)
		return err
	}

	// Step 2: Create/Update ManagedClusterAddOn (per-cluster installation)
	managedClusterAddOn := addonapiv1alpha1.ManagedClusterAddOn{}
	managedClusterAddOn.Name = utils.S3ConfigAddonName
	managedClusterAddOn.Namespace = providerCluster

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, &managedClusterAddOn, func() error {
		if err := controllerutil.SetOwnerReference(s3Config, clusterManagementAddOn, r.Scheme); err != nil {
			return err
		}
		utils.AddLabel(&managedClusterAddOn, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)
		managedClusterAddOn.Spec.InstallNamespace = s3Config.Spec.InternalS3.Namespace
		return nil
	}); err != nil {
		logger.Error("Failed to create/update ManagedClusterAddOn", "error", err, "addon", utils.S3ConfigAddonName)
		return err
	}

	logger.Info("S3Config addon ensured", "cluster", providerCluster)
	return nil
}

// ensureOBCManifestWork creates ManifestWork to deploy OBC on the spoke cluster
func (r *S3ConfigurationReconciler) ensureOBCManifestWork(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) (bool, error) {
	obcName := getOBCName(s3Config)
	providerCluster := s3Config.Spec.InternalS3.ProviderCluster
	namespace := s3Config.Spec.InternalS3.Namespace
	storageClassName := s3Config.Spec.InternalS3.StorageClassName

	logger.Info("Ensuring OBC ManifestWork", "obcName", obcName, "providerCluster", providerCluster, "namespace", namespace)

	// Create OBC resource with proper TypeMeta for ManifestWork
	obc := &obv1alpha1.ObjectBucketClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "objectbucket.io/v1alpha1",
			Kind:       "ObjectBucketClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      obcName,
			Namespace: namespace,
		},
		Spec: obv1alpha1.ObjectBucketClaimSpec{
			BucketName:       obcName,
			StorageClassName: storageClassName,
		},
	}
	utils.AddAnnotation(obc, utils.S3ConfigurationNameAnnotationKey, s3Config.Name)
	utils.AddLabel(obc, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)

	// Marshal OBC to JSON
	obcJSON, err := json.Marshal(obc)
	if err != nil {
		return false, fmt.Errorf("failed to marshal OBC to JSON: %w", err)
	}

	// Create ManifestWork
	manifestWorkName := fmt.Sprintf("s3config-%s-obc", s3Config.Name)
	mw := &workv1.ManifestWork{}
	mw.Name = manifestWorkName
	mw.Namespace = providerCluster

	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, mw, func() error {
		if err := controllerutil.SetControllerReference(s3Config, mw, r.Scheme); err != nil {
			return err
		}
		utils.AddLabel(mw, utils.CreatedByLabelKey, utils.CreatorMulticlusterOrchestrator)

		mw.Spec.Workload.Manifests = []workv1.Manifest{
			{
				RawExtension: runtime.RawExtension{
					Raw: obcJSON,
				},
			},
		}
		return nil
	}); err != nil {
		logger.Error("Failed to create/update ManifestWork", "manifestWorkName", manifestWorkName, "error", err)
		return false, err
	}

	logger.Info("ManifestWork created/updated successfully", "manifestWorkName", manifestWorkName)

	// Check if ManifestWork is applied
	if err := r.Get(ctx, client.ObjectKeyFromObject(mw), mw); err != nil {
		return false, err
	}

	// Check ManifestWork status
	for _, condition := range mw.Status.Conditions {
		if condition.Type == "Applied" && condition.Status == metav1.ConditionTrue {
			logger.Info("ManifestWork Applied successfully")
			return true, nil
		}
	}

	logger.Info("ManifestWork not yet Applied", "status", mw.Status.Conditions)
	return false, nil
}

// waitForInternalSecret waits for the addon to sync the secret from spoke to hub
func (r *S3ConfigurationReconciler) validateSecret(ctx context.Context, logger *slog.Logger, secret *corev1.Secret) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		logger.Error("Failed to get secret", "secret", client.ObjectKeyFromObject(secret), "error", err)
		return err
	}

	// Validate secret has required S3 keys
	requiredKeys := []string{
		utils.AwsAccessKeyId,
		utils.AwsSecretAccessKey,
		utils.S3BucketName,
		utils.S3Endpoint,
		utils.S3Region,
	}

	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			logger.Error("Secret missing required key", "secret", client.ObjectKeyFromObject(secret), "missingKey", key)
			return fmt.Errorf("secret %s missing required key: %s", client.ObjectKeyFromObject(secret), key)
		}
	}

	logger.Info("Found valid secret", "secret", client.ObjectKeyFromObject(secret))
	return nil
}

// ensureRamenSecret copies the S3 credentials to Ramen namespace
func (r *S3ConfigurationReconciler) ensureRamenSecret(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration, secret *corev1.Secret) error {
	logger.Info("Copying S3 credentials to Ramen namespace", "sourceSecret", client.ObjectKeyFromObject(secret), "targetNamespace", r.CurrentNamespace)

	ramenSecret := &corev1.Secret{}
	ramenSecret.Name = s3Config.Name
	ramenSecret.Namespace = r.CurrentNamespace

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ramenSecret, func() error {
		if err := controllerutil.SetControllerReference(s3Config, ramenSecret, r.Scheme); err != nil {
			return err
		}
		utils.AddLabel(ramenSecret, utils.HubRecoveryLabel, "")
		utils.AddLabel(ramenSecret, utils.CreatedByLabelKey, utils.S3ConfigAddonName)

		ramenSecret.Data = map[string][]byte{
			utils.AwsAccessKeyId:     secret.Data[utils.AwsAccessKeyId],
			utils.AwsSecretAccessKey: secret.Data[utils.AwsSecretAccessKey],
		}
		return nil
	}); err != nil {
		logger.Error("Failed to create/update secret in Ramen namespace", "error", err, "secretName", s3Config.Name)
		return err
	}

	logger.Info("Successfully copied secret to Ramen namespace", "secretName", s3Config.Name, "namespace", r.CurrentNamespace)
	return nil
}

// addS3ProfileToRamenConfig updates the Ramen ConfigMap with S3 profile
func (r *S3ConfigurationReconciler) addS3ProfileToRamenConfig(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration, secret *corev1.Secret) error {
	return utils.UpdateRamenConfigForS3Configuration(
		ctx,
		r.Client,
		s3Config.Name,
		string(secret.Data[utils.S3BucketName]),
		string(secret.Data[utils.S3Region]),
		string(secret.Data[utils.S3Endpoint]),
		r.CurrentNamespace,
		logger,
	)
}

// ensureDrClusters creates DRCluster resources for each managed cluster
func (r *S3ConfigurationReconciler) ensureDrClusters(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) error {
	logger.Info("Creating DRClusters for managed clusters", "managedClusters", s3Config.Spec.ManagedClusters)

	for _, clusterName := range s3Config.Spec.ManagedClusters {
		drCluster := &rmn.DRCluster{}
		drCluster.Name = clusterName

		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, drCluster, func() error {
			if err := controllerutil.SetControllerReference(s3Config, drCluster, r.Scheme); err != nil {
				return err
			}
			drCluster.Spec.S3ProfileName = s3Config.Name
			return nil
		}); err != nil {
			logger.Error("Failed to create/update DRCluster", "error", err, "clusterName", clusterName)
			return err
		}

		logger.Info("DRCluster created/updated successfully", "clusterName", clusterName, "s3ProfileName", s3Config.Name)
	}

	logger.Info("All DRClusters created/updated successfully")
	return nil
}

// getOBCName returns the OBC name for this S3Configuration
func getOBCName(s3Config *multiclusterv1alpha1.S3Configuration) string {
	if s3Config.Spec.InternalS3.OBCName != "" {
		return s3Config.Spec.InternalS3.OBCName
	}
	return fmt.Sprintf("%s-%s", OBCNamePrefix, s3Config.Name)
}

// validateNoDRPolicyUsesClusters checks if any DRPolicy is using the clusters from this S3Configuration
func (r *S3ConfigurationReconciler) validateNoDRPolicyUsesClusters(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) error {
	logger.Info("Checking if any DRPolicy uses the managedClusters")

	// List all DRPolicies
	drPolicyList := &rmn.DRPolicyList{}
	if err := r.List(ctx, drPolicyList); err != nil {
		logger.Error("Failed to list DRPolicies", "error", err)
		return fmt.Errorf("failed to list DRPolicies: %w", err)
	}

	// Check if any DRPolicy uses any of this S3Configuration's clusters
	for _, drPolicy := range drPolicyList.Items {
		for _, drCluster := range drPolicy.Spec.DRClusters {
			for _, managedCluster := range s3Config.Spec.ManagedClusters {
				if drCluster == managedCluster {
					return fmt.Errorf("cannot delete S3Configuration: cluster %q is being used by DRPolicy %q", managedCluster, drPolicy.Name)
				}
			}
		}
	}

	logger.Info("No DRPolicy is using the managedClusters, safe to delete")
	return nil
}

// removeS3ProfileFromRamenConfig removes the S3 profile from Ramen ConfigMap
func (r *S3ConfigurationReconciler) removeS3ProfileFromRamenConfig(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) error {
	logger.Info("Removing S3 profile from Ramen ConfigMap", "s3ProfileName", s3Config.Name)

	return utils.RemoveS3ProfileFromRamenConfig(
		ctx,
		r.Client,
		s3Config.Name,
		r.CurrentNamespace,
		logger,
	)
}

// cleanupManagedClusterAddOn deletes MCA if no other S3Configuration uses this cluster
func (r *S3ConfigurationReconciler) cleanupManagedClusterAddOn(ctx context.Context, logger *slog.Logger, providerCluster string, currentS3ConfigName string) error {
	// List all S3Configurations
	s3ConfigList := &multiclusterv1alpha1.S3ConfigurationList{}
	if err := r.List(ctx, s3ConfigList); err != nil {
		logger.Error("Failed to list S3Configurations", "error", err)
		return err
	}

	// Check if any other S3Configuration uses this cluster
	for _, s3Config := range s3ConfigList.Items {
		// Skip the current S3Config being deleted
		if s3Config.Name == currentS3ConfigName {
			continue
		}
		// Check if another S3Config uses the same cluster
		if s3Config.Spec.InternalS3 != nil && s3Config.Spec.InternalS3.ProviderCluster == providerCluster {
			logger.Info("ManagedClusterAddOn still in use by another S3Configuration", "cluster", providerCluster, "s3config", s3Config.Name)
			return nil
		}
	}

	// No other S3Config uses this cluster, safe to delete MCA
	logger.Info("Deleting ManagedClusterAddOn as no S3Configuration uses this cluster", "cluster", providerCluster)
	mca := &addonapiv1alpha1.ManagedClusterAddOn{}
	mca.Name = utils.S3ConfigAddonName
	mca.Namespace = providerCluster

	if err := r.Delete(ctx, mca); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error("Failed to delete ManagedClusterAddOn", "error", err)
			return err
		}
		logger.Info("ManagedClusterAddOn already deleted")
	} else {
		logger.Info("Successfully deleted ManagedClusterAddOn", "cluster", providerCluster)
	}

	return nil
}

// validateManagedClustersUniqueness ensures that each managedCluster is only part of one S3Configuration
// TODO: Improve validation to allow the oldest S3Configuration (by creation timestamp) and fail newer ones
func (r *S3ConfigurationReconciler) validateManagedClustersUniqueness(ctx context.Context, logger *slog.Logger, s3Config *multiclusterv1alpha1.S3Configuration) error {
	logger.Info("Validating managedClusters uniqueness")

	// Validate no duplicate clusters within this S3Configuration
	clusterSet := make(map[string]bool)
	for _, cluster := range s3Config.Spec.ManagedClusters {
		if clusterSet[cluster] {
			return fmt.Errorf("duplicate cluster %q found in managedClusters list", cluster)
		}
		clusterSet[cluster] = true
	}

	// Get all existing S3Configurations
	s3ConfigList := &multiclusterv1alpha1.S3ConfigurationList{}
	if err := r.List(ctx, s3ConfigList); err != nil {
		logger.Error("Failed to list S3Configurations", "error", err)
		return fmt.Errorf("failed to list S3Configurations: %w", err)
	}

	// Build a map of which clusters are used by other S3Configurations
	usedClusters := make(map[string]string) // cluster -> S3Configuration name
	for _, existingConfig := range s3ConfigList.Items {
		// Skip the current S3Configuration being validated
		if existingConfig.Name == s3Config.Name {
			continue
		}

		// Add all managed clusters from existing configs
		for _, cluster := range existingConfig.Spec.ManagedClusters {
			usedClusters[cluster] = existingConfig.Name
		}
	}

	// Check if any of the new clusters are already in use
	var conflicts []string
	for _, cluster := range s3Config.Spec.ManagedClusters {
		if existingConfigName, exists := usedClusters[cluster]; exists {
			conflicts = append(conflicts, fmt.Sprintf("cluster %q is already managed by S3Configuration %q", cluster, existingConfigName))
		}
	}

	if len(conflicts) > 0 {
		return fmt.Errorf("managedCluster conflict detected: %v. Each cluster can only be part of one S3Configuration", conflicts)
	}

	logger.Info("ManagedClusters uniqueness validation passed")
	return nil
}
