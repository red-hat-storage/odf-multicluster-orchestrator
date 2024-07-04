/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

package controllers

import (
	"context"
	"log/slog"
	"os"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	addons "github.com/red-hat-storage/odf-multicluster-orchestrator/addons"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger
}

const mirrorPeerFinalizer = "hub.multicluster.odf.openshift.io"
const spokeClusterRoleBindingName = "spoke-clusterrole-bindings"

//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods;secrets;configmaps;events,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=*
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons;clustermanagementaddons,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=*
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=ramendr.openshift.io,resources=drclusters;drpolicies,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete;watch,resourceNames=spoke-clusterrole-bindings

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Reconciling request")
	// Fetch MirrorPeer for given Request
	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := r.Get(ctx, req.NamespacedName, &mirrorPeer)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error("Failed to get MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Validating MirrorPeer")
	// Validate MirrorPeer
	// MirrorPeer.Spec must be defined
	if err := undefinedMirrorPeerSpec(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec is undefined", "error", err)
		return ctrl.Result{Requeue: false}, err
	}
	// MirrorPeer.Spec.Items must be unique
	if err := uniqueSpecItems(mirrorPeer.Spec); err != nil {
		logger.Error("MirrorPeer spec items are not unique", "error", err)
		return ctrl.Result{Requeue: false}, err
	}
	for i := range mirrorPeer.Spec.Items {
		// MirrorPeer.Spec.Items must not have empty fields
		if err := emptySpecItems(mirrorPeer.Spec.Items[i]); err != nil {
			logger.Error("MirrorPeer spec items have empty fields", "error", err)
			return reconcile.Result{Requeue: false}, err
		}
		// MirrorPeer.Spec.Items[*].ClusterName must be a valid ManagedCluster
		if err := isManagedCluster(ctx, r.Client, mirrorPeer.Spec.Items[i].ClusterName); err != nil {
			logger.Error("Invalid ManagedCluster", "ClusterName", mirrorPeer.Spec.Items[i].ClusterName, "error", err)
			return ctrl.Result{}, err
		}
	}
	logger.Info("All validations for MirrorPeer passed")

	if mirrorPeer.GetDeletionTimestamp().IsZero() {
		if !utils.ContainsString(mirrorPeer.GetFinalizers(), mirrorPeerFinalizer) {
			logger.Info("Finalizer not found on MirrorPeer. Adding Finalizer")
			mirrorPeer.Finalizers = append(mirrorPeer.Finalizers, mirrorPeerFinalizer)
		}
	} else {
		logger.Info("Deleting MirrorPeer")
		mirrorPeer.Status.Phase = multiclusterv1alpha1.Deleting
		statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
		if statusErr != nil {
			logger.Error("Error occurred while updating the status of mirrorpeer", "Error", statusErr)
			return ctrl.Result{Requeue: true}, nil
		}
		if utils.ContainsString(mirrorPeer.GetFinalizers(), mirrorPeerFinalizer) {
			if utils.ContainsSuffix(mirrorPeer.GetFinalizers(), addons.SpokeMirrorPeerFinalizer) {
				logger.Info("Waiting for agent to delete resources")
				return reconcile.Result{Requeue: true}, err
			}
			if err := r.deleteSecrets(ctx, mirrorPeer); err != nil {
				logger.Error("Failed to delete resources", "error", err)
				return reconcile.Result{Requeue: true}, err
			}
			mirrorPeer.Finalizers = utils.RemoveString(mirrorPeer.Finalizers, mirrorPeerFinalizer)
			if err := r.Client.Update(ctx, &mirrorPeer); err != nil {
				logger.Error("Failed to remove finalizer from MirrorPeer", "error", err)
				return reconcile.Result{}, err
			}
		}
		logger.Info("MirrorPeer deleted, skipping reconcilation")
		return reconcile.Result{}, nil
	}

	mirrorPeerCopy := mirrorPeer.DeepCopy()
	if mirrorPeerCopy.Labels == nil {
		mirrorPeerCopy.Labels = make(map[string]string)
	}

	if val, ok := mirrorPeerCopy.Labels[utils.HubRecoveryLabel]; !ok || val != "resource" {
		logger.Info("Adding label to mirrorpeer for disaster recovery")
		mirrorPeerCopy.Labels[utils.HubRecoveryLabel] = "resource"
		err = r.Client.Update(ctx, mirrorPeerCopy)

		if err != nil {
			logger.Error("Failed to update mirrorpeer with disaster recovery label", "error", err)
			return checkK8sUpdateErrors(err, mirrorPeerCopy, logger)
		}
		logger.Info("Successfully added label to mirrorpeer for disaster recovery. Requeing request...")
		return ctrl.Result{Requeue: true}, nil
	}

	if mirrorPeer.Status.Phase == "" {
		if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
			mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangingSecret
		} else {
			mirrorPeer.Status.Phase = multiclusterv1alpha1.S3ProfileSyncing
		}
		statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
		if statusErr != nil {
			logger.Error("Error occurred while updating the status of mirrorpeer. Requeing request...", "Error ", statusErr)
			// Requeue, but don't throw
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if err := r.processManagedClusterAddon(ctx, mirrorPeer); err != nil {
		logger.Error("Failed to process managedclusteraddon", "error", err)
		return ctrl.Result{}, err
	}

	err = r.createClusterRoleBindingsForSpoke(ctx, mirrorPeer)
	if err != nil {
		logger.Error("Failed to create cluster role bindings for spoke", "error", err)
		return ctrl.Result{}, err
	}

	// update s3 profile when MirrorPeer changes
	if mirrorPeer.Spec.ManageS3 {
		for _, peerRef := range mirrorPeer.Spec.Items {
			var s3Secret corev1.Secret
			namespacedName := types.NamespacedName{
				Name:      utils.GetSecretNameByPeerRef(peerRef, utils.S3ProfilePrefix),
				Namespace: peerRef.ClusterName,
			}
			err = r.Client.Get(ctx, namespacedName, &s3Secret)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					logger.Info("S3 secret is not yet synchronised. retrying till it is available. Requeing request...", "Cluster", peerRef.ClusterName)
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error("Error in fetching s3 internal secret", "Cluster", peerRef.ClusterName, "error", err)
				return ctrl.Result{}, err
			}
			err = createOrUpdateSecretsFromInternalSecret(ctx, r.Client, &s3Secret, []multiclusterv1alpha1.MirrorPeer{mirrorPeer}, logger)
			if err != nil {
				logger.Error("Error in updating S3 profile", "Cluster", peerRef.ClusterName, "error", err)
				return ctrl.Result{}, err
			}
		}
	}

	if err = r.processMirrorPeerSecretChanges(ctx, r.Client, mirrorPeer); err != nil {
		logger.Error("Error processing MirrorPeer secret changes", "error", err)
		return ctrl.Result{}, err
	}

	err = r.createDRClusters(ctx, &mirrorPeer)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Secret not synchronised yet, retrying to create DRCluster", "MirrorPeer", mirrorPeer.Name)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error("Failed to create DRClusters for MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	return r.updateMirrorPeerStatus(ctx, mirrorPeer)
}

// processManagedClusterAddon creates an addon for the cluster management in all the peer refs,
// the resources gets an owner ref of the mirrorpeer to let the garbage collector handle it if the mirrorpeer gets deleted
func (r *MirrorPeerReconciler) processManagedClusterAddon(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger.With("MirrorPeer", mirrorPeer.Name)
	logger.Info("Processing ManagedClusterAddons for MirrorPeer")

	for _, item := range mirrorPeer.Spec.Items {
		logger.Info("Handling ManagedClusterAddon for cluster", "ClusterName", item.ClusterName)

		var managedClusterAddOn addonapiv1alpha1.ManagedClusterAddOn
		namespacedName := types.NamespacedName{
			Name:      setup.TokenExchangeName,
			Namespace: item.ClusterName,
		}

		err := r.Client.Get(ctx, namespacedName, &managedClusterAddOn)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("ManagedClusterAddon not found, will create a new one", "ClusterName", item.ClusterName)

				annotations := make(map[string]string)
				annotations[utils.DRModeAnnotationKey] = string(mirrorPeer.Spec.Type)
				managedClusterAddOn = addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:        setup.TokenExchangeName,
						Namespace:   item.ClusterName,
						Annotations: annotations,
					},
				}
			}
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &managedClusterAddOn, func() error {
			managedClusterAddOn.Spec.InstallNamespace = item.StorageClusterRef.Namespace
			if err := controllerutil.SetOwnerReference(&mirrorPeer, &managedClusterAddOn, r.Scheme); err != nil {
				logger.Error("Failed to set owner reference on ManagedClusterAddon", "error", err, "ClusterName", item.ClusterName)
				return err
			}
			return nil
		})

		if err != nil {
			logger.Error("Failed to reconcile ManagedClusterAddOn", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name), "error", err)
			return err
		}

		logger.Info("Successfully reconciled ManagedClusterAddOn", "ClusterName", item.ClusterName)
	}

	logger.Info("Successfully processed all ManagedClusterAddons for MirrorPeer")
	return nil
}

// deleteSecrets checks if another mirrorpeer is using a peer ref in the mirrorpeer being deleted, if not then it
// goes ahead and deletes all the secrets with blue, green and internal label.
// If two mirrorpeers are pointing to the same peer ref, but only gets deleted the orphan green secret in
// the still standing peer ref gets deleted by the mirrorpeer secret controller
func (r *MirrorPeerReconciler) deleteSecrets(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	logger.Info("Starting deletion of secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name)

	for i, peerRef := range mirrorPeer.Spec.Items {
		logger.Info("Checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName)

		peerRefUsed, err := utils.DoesAnotherMirrorPeerPointToPeerRef(ctx, r.Client, &mirrorPeer.Spec.Items[i])
		if err != nil {
			logger.Error("Error checking if PeerRef is used by another MirrorPeer", "PeerRef", peerRef.ClusterName, "error", err)
			return err
		}

		if !peerRefUsed {
			logger.Info("PeerRef is not used by another MirrorPeer, proceeding to delete secrets", "PeerRef", peerRef.ClusterName)

			secretLabels := []string{string(utils.SourceLabel), string(utils.DestinationLabel)}
			if mirrorPeer.Spec.ManageS3 {
				secretLabels = append(secretLabels, string(utils.InternalLabel))
			}

			secretRequirement, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.In, secretLabels)
			if err != nil {
				logger.Error("Cannot create label requirement for deleting secrets", "error", err)
				return err
			}

			secretSelector := labels.NewSelector().Add(*secretRequirement)
			deleteOpt := client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace:     mirrorPeer.Spec.Items[i].ClusterName,
					LabelSelector: secretSelector,
				},
			}

			var secret corev1.Secret
			if err := r.DeleteAllOf(ctx, &secret, &deleteOpt); err != nil {
				logger.Error("Error while deleting secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name, "PeerRef", peerRef.ClusterName, "error", err)
			}

			logger.Info("Secrets successfully deleted", "PeerRef", peerRef.ClusterName)
		} else {
			logger.Info("PeerRef is still used by another MirrorPeer, skipping deletion", "PeerRef", peerRef.ClusterName)
		}
	}

	logger.Info("Completed deletion of secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name)
	return nil
}

func (r *MirrorPeerReconciler) processMirrorPeerSecretChanges(ctx context.Context, rc client.Client, mirrorPeerObj multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	logger.Info("Processing mirror peer secret changes", "MirrorPeer", mirrorPeerObj.Name)

	var anyErr error

	for _, eachPeerRef := range mirrorPeerObj.Spec.Items {
		logger.Info("Fetching all source secrets", "ClusterName", eachPeerRef.ClusterName)
		sourceSecrets, err := fetchAllSourceSecrets(ctx, rc, eachPeerRef.ClusterName)
		if err != nil {
			logger.Error("Unable to get a list of source secrets", "error", err, "namespace", eachPeerRef.ClusterName)
			anyErr = err
			continue
		}

		// get the source secret associated with the PeerRef
		matchingSourceSecret := utils.FindMatchingSecretWithPeerRef(eachPeerRef, sourceSecrets)
		if matchingSourceSecret == nil {
			logger.Info("No matching source secret found for peer ref", "PeerRef", eachPeerRef.ClusterName)
			continue
		}
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, matchingSourceSecret, logger, mirrorPeerObj)
		if err != nil {
			logger.Error("Error while updating destination secrets", "source-secret", matchingSourceSecret.Name, "namespace", matchingSourceSecret.Namespace, "error", err)
			anyErr = err
		}
	}

	if anyErr == nil {
		logger.Info("Cleaning up any orphan destination secrets")
		anyErr = processDestinationSecretCleanup(ctx, rc, logger)
		if anyErr != nil {
			logger.Error("Error cleaning up orphan destination secrets", "error", anyErr)
		}
	} else {
		logger.Info("Errors encountered in updating secrets; skipping cleanup")
	}

	return anyErr
}

func (r *MirrorPeerReconciler) checkTokenExchangeStatus(ctx context.Context, mp multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger := r.Logger

	logger.Info("Checking token exchange status for MirrorPeer", "MirrorPeer", mp.Name)

	// Assuming pr1 and pr2 are defined as first two peer refs in spec
	pr1 := mp.Spec.Items[0]
	pr2 := mp.Spec.Items[1]

	// Check for source secret for pr1
	err := r.checkForSourceSecret(ctx, pr1)
	if err != nil {
		logger.Error("Failed to find valid source secret for the first peer reference", "PeerRef", pr1.ClusterName, "error", err)
		return false, err
	}

	// Check for destination secret for pr1 in pr2's cluster
	err = r.checkForDestinationSecret(ctx, pr1, pr2.ClusterName)
	if err != nil {
		logger.Error("Failed to find valid destination secret for the first peer reference", "PeerRef", pr1.ClusterName, "DestinationCluster", pr2.ClusterName, "error", err)
		return false, err
	}

	// Check for source secret for pr2
	err = r.checkForSourceSecret(ctx, pr2)
	if err != nil {
		logger.Error("Failed to find valid source secret for the second peer reference", "PeerRef", pr2.ClusterName, "error", err)
		return false, err
	}

	// Check for destination secret for pr2 in pr1's cluster
	err = r.checkForDestinationSecret(ctx, pr2, pr1.ClusterName)
	if err != nil {
		logger.Error("Failed to find valid destination secret for the second peer reference", "PeerRef", pr2.ClusterName, "DestinationCluster", pr1.ClusterName, "error", err)
		return false, err
	}

	logger.Info("Successfully validated token exchange status for all peer references", "MirrorPeer", mp.Name)
	return true, nil
}

func (r *MirrorPeerReconciler) checkForSourceSecret(ctx context.Context, peerRef multiclusterv1alpha1.PeerRef) error {
	logger := r.Logger
	prSecretName := utils.GetSecretNameByPeerRef(peerRef)
	var peerSourceSecret corev1.Secret
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      prSecretName,
		Namespace: peerRef.ClusterName, // Source Namespace for the secret
	}, &peerSourceSecret)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Source secret not found", "Secret", prSecretName, "Namespace", peerRef.ClusterName)
			return err
		}
		logger.Error("Failed to fetch source secret", "error", err, "Secret", prSecretName, "Namespace", peerRef.ClusterName)
		return err
	}

	err = utils.ValidateSourceSecret(&peerSourceSecret)
	if err != nil {
		logger.Error("Validation failed for source secret", "error", err, "Secret", prSecretName, "Namespace", peerRef.ClusterName)
		return err
	}

	logger.Info("Source secret validated successfully", "Secret", prSecretName, "Namespace", peerRef.ClusterName)
	return nil
}

func (r *MirrorPeerReconciler) checkForDestinationSecret(ctx context.Context, peerRef multiclusterv1alpha1.PeerRef, destNamespace string) error {
	logger := r.Logger
	prSecretName := utils.GetSecretNameByPeerRef(peerRef)
	var peerDestinationSecret corev1.Secret

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      prSecretName,
		Namespace: destNamespace, // Destination Namespace for the secret
	}, &peerDestinationSecret)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Destination secret not found", "Secret", prSecretName, "Namespace", destNamespace)
			return err
		}
		logger.Error("Failed to fetch destination secret", "error", err, "Secret", prSecretName, "Namespace", destNamespace)
		return err
	}

	err = utils.ValidateDestinationSecret(&peerDestinationSecret)
	if err != nil {
		logger.Error("Validation failed for destination secret", "error", err, "Secret", prSecretName, "Namespace", destNamespace)
		return err
	}

	logger.Info("Destination secret validated successfully", "Secret", prSecretName, "Namespace", destNamespace)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up controller for MirrorPeer")

	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{})

	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)

}

// CheckK8sUpdateErrors checks what type of error occurs when trying to update a k8s object
// and logs according to the object
func checkK8sUpdateErrors(err error, obj client.Object, logger *slog.Logger) (ctrl.Result, error) {
	if k8serrors.IsConflict(err) {
		logger.Info("Object is being updated by another process. Retrying", "Kind", obj.GetObjectKind().GroupVersionKind().Kind, "Name", obj.GetName())
		return ctrl.Result{Requeue: true}, nil
	} else if k8serrors.IsNotFound(err) {
		logger.Info("Object no longer exists. Ignoring since object must have been deleted", "Kind", obj.GetObjectKind().GroupVersionKind().Kind, "Name", obj.GetName())
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error("Failed to update object", "error", err, "Name", obj.GetName())
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) createDRClusters(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	currentNamespace := os.Getenv("POD_NAMESPACE")

	for _, pr := range mp.Spec.Items {
		clusterName := pr.ClusterName
		s3SecretName := utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)

		dc := ramenv1alpha1.DRCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}

		rookSecretName := utils.GetSecretNameByPeerRef(pr)

		var fsid string
		if mp.Spec.Type == multiclusterv1alpha1.Sync {
			logger.Info("Fetching rook secret ", "Secret", rookSecretName)
			hs, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: rookSecretName, Namespace: currentNamespace})
			if err != nil {
				logger.Error("Failed to fetch Rook secret", "error", err, "SecretName", rookSecretName, "Namespace", currentNamespace)
				return err
			}
			logger.Info("Unmarshalling rook secret ", "Secret Name:", rookSecretName)
			rt, err := utils.UnmarshalRookSecretExternal(hs)
			if err != nil {
				logger.Error("Failed to unmarshal Rook secret", "error", err, "SecretName", rookSecretName)
				return err
			}
			fsid = rt.FSID
		} else {
			logger.Info("Fetching rook secret ", "Secret Name:", rookSecretName)
			hs, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: rookSecretName, Namespace: clusterName})
			if err != nil {
				return err
			}
			logger.Info("Unmarshalling rook secret ", "Secret Name:", rookSecretName)
			rt, err := utils.UnmarshalHubSecret(hs)
			if err != nil {
				logger.Error("Failed to unmarshal Hub secret", "error", err, "SecretName", rookSecretName)
				return err
			}
			fsid = rt.FSID
		}

		dc.Spec.Region = ramenv1alpha1.Region(fsid)
		logger.Info("Fetching s3 secret ", "Secret Name:", s3SecretName)
		ss, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: s3SecretName, Namespace: clusterName})
		if err != nil {
			logger.Error("Failed to fetch S3 secret", "error", err, "SecretName", s3SecretName, "Namespace", clusterName)
			return err
		}

		logger.Info("Unmarshalling S3 secret", "SecretName", s3SecretName)
		st, err := utils.UnmarshalS3Secret(ss)
		if err != nil {
			logger.Error("Failed to unmarshal S3 secret", "error", err, "SecretName", s3SecretName)
			return err
		}

		logger.Info("Creating and updating DR clusters", "ClusterName", clusterName)
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &dc, func() error {
			dc.Spec.S3ProfileName = st.S3ProfileName
			return nil
		})
		if err != nil {
			logger.Error("Failed to create/update DR cluster", "error", err, "ClusterName", clusterName)
			return err
		}
	}
	return nil
}

func (r *MirrorPeerReconciler) createClusterRoleBindingsForSpoke(ctx context.Context, peer multiclusterv1alpha1.MirrorPeer) error {
	logger := r.Logger
	logger.Info("Starting to create or update ClusterRoleBindings for the spoke", "MirrorPeerName", peer.Name)

	crb := rbacv1.ClusterRoleBinding{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: spokeClusterRoleBindingName}, &crb)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			logger.Error("Failed to get ClusterRoleBinding", "error", err, "ClusterRoleBindingName", spokeClusterRoleBindingName)
			return err
		}
		logger.Info("ClusterRoleBinding not found, will be created", "ClusterRoleBindingName", spokeClusterRoleBindingName)
	}

	var subjects []rbacv1.Subject
	if crb.Subjects != nil {
		subjects = crb.Subjects
	}

	// Add users and groups
	for _, pr := range peer.Spec.Items {
		usub := getSubjectByPeerRef(pr, "User")
		gsub := getSubjectByPeerRef(pr, "Group")
		if !utils.ContainsSubject(subjects, usub) {
			subjects = append(subjects, *usub)
			logger.Info("Adding user subject to ClusterRoleBinding", "User", usub.Name)
		}

		if !utils.ContainsSubject(subjects, gsub) {
			subjects = append(subjects, *gsub)
			logger.Info("Adding group subject to ClusterRoleBinding", "Group", gsub.Name)
		}
	}

	spokeRoleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: spokeClusterRoleBindingName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &spokeRoleBinding, func() error {
		spokeRoleBinding.Subjects = subjects

		if spokeRoleBinding.CreationTimestamp.IsZero() {
			// RoleRef is immutable, so it's set only while creating new object
			spokeRoleBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "open-cluster-management:token-exchange:agent",
			}
			logger.Info("Setting RoleRef for new ClusterRoleBinding", "RoleRef", spokeRoleBinding.RoleRef.Name)
		}

		return nil
	})

	if err != nil {
		logger.Error("Failed to create or update ClusterRoleBinding", "error", err, "ClusterRoleBindingName", spokeClusterRoleBindingName)
		return err
	}

	logger.Info("Successfully created or updated ClusterRoleBinding", "ClusterRoleBindingName", spokeClusterRoleBindingName)
	return nil
}

func (r *MirrorPeerReconciler) updateMirrorPeerStatus(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	logger := r.Logger

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		tokensExchanged, err := r.checkTokenExchangeStatus(ctx, mirrorPeer)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Secrets not found; Attempting to reconcile again", "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error("Error while exchanging tokens", "error", err, "MirrorPeer", mirrorPeer.Name)
			mirrorPeer.Status.Message = err.Error()
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
			}
			return ctrl.Result{}, err
		}

		if tokensExchanged {
			logger.Info("Tokens exchanged", "MirrorPeer", mirrorPeer.Name)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangedSecret
			mirrorPeer.Status.Message = ""
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil
		}
	} else {
		// Sync mode status update, same flow as async but for s3 profile
		s3ProfileSynced, err := r.checkS3ProfileStatus(ctx, mirrorPeer)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("S3 secrets not found; Attempting to reconcile again", "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error("Error while syncing S3 Profile", "error", err, "MirrorPeer", mirrorPeer.Name)
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
			}
			return ctrl.Result{}, err
		}

		if s3ProfileSynced {
			logger.Info("S3 Profile synced to hub", "MirrorPeer", mirrorPeer.Name)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.S3ProfileSynced
			mirrorPeer.Status.Message = ""
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error("Error occurred while updating the status of mirrorpeer", "error", statusErr, "MirrorPeer", mirrorPeer.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *MirrorPeerReconciler) checkS3ProfileStatus(ctx context.Context, mp multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger := r.Logger
	logger.Info("Checking S3 profile status for each peer reference in the MirrorPeer", "MirrorPeerName", mp.Name)

	for _, pr := range mp.Spec.Items {
		clusterName := pr.ClusterName
		s3SecretName := utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)
		logger.Info("Attempting to fetch S3 secret", "SecretName", s3SecretName, "ClusterName", clusterName)

		_, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: s3SecretName, Namespace: clusterName})
		if err != nil {
			logger.Error("Failed to fetch S3 secret", "error", err, "SecretName", s3SecretName, "ClusterName", clusterName)
			return false, err
		}

		logger.Info("Successfully fetched S3 secret", "SecretName", s3SecretName, "ClusterName", clusterName)
	}

	logger.Info("Successfully verified S3 profile status for all peer references", "MirrorPeerName", mp.Name)
	return true, nil
}

func getSubjectByPeerRef(pr multiclusterv1alpha1.PeerRef, kind string) *rbacv1.Subject {
	switch kind {
	case "User":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultUser(pr.ClusterName, setup.TokenExchangeName, setup.TokenExchangeName),
			APIGroup: "rbac.authorization.k8s.io",
		}
	case "Group":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultGroups(pr.ClusterName, setup.TokenExchangeName)[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
	default:
		return nil
	}
}
