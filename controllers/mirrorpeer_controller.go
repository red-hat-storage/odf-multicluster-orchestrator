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

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	addons "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
	tokenExchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const hubRecoveryLabel = "cluster.open-cluster-management.io/backup"
const mirrorPeerFinalizer = "hub.mirrorpeer.multicluster.odf.openshift.io"
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
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=ramendr.openshift.io,resources=drclusters,verbs=get;list;create;update;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete;watch,resourceNames=spoke-clusterrole-bindings

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

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
		logger.Error(err, "Failed to get MirrorPeer")
		return ctrl.Result{}, err
	}

	logger.V(2).Info("Validating MirrorPeer", "MirrorPeer", req.NamespacedName)
	// Validate MirrorPeer
	// MirrorPeer.Spec must be defined
	if err := undefinedMirrorPeerSpec(mirrorPeer.Spec); err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	// MirrorPeer.Spec.Items must be unique
	if err := uniqueSpecItems(mirrorPeer.Spec); err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	for i := range mirrorPeer.Spec.Items {
		// MirrorPeer.Spec.Items must not have empty fields
		if err := emptySpecItems(mirrorPeer.Spec.Items[i]); err != nil {
			// return error and do not requeue since user needs to update the spec
			// when user updates the spec, new reconcile will be triggered
			return reconcile.Result{Requeue: false}, err
		}
		// MirrorPeer.Spec.Items[*].ClusterName must be a valid ManagedCluster
		if err := isManagedCluster(ctx, r.Client, mirrorPeer.Spec.Items[i].ClusterName); err != nil {
			return ctrl.Result{}, err
		}
	}
	logger.V(2).Info("All validations for MirrorPeer passed", "MirrorPeer", req.NamespacedName)

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
			logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
			return ctrl.Result{Requeue: true}, nil
		}
		if utils.ContainsString(mirrorPeer.GetFinalizers(), mirrorPeerFinalizer) {
			if utils.ContainsSuffix(mirrorPeer.GetFinalizers(), addons.SpokeMirrorPeerFinalizer) {
				logger.Info("Waiting for agent to delete resources")
				return reconcile.Result{Requeue: true}, err
			}
			if err := r.deleteSecrets(ctx, mirrorPeer); err != nil {
				logger.Error(err, "Failed to delete resources")
				return reconcile.Result{Requeue: true}, err
			}
			mirrorPeer.Finalizers = utils.RemoveString(mirrorPeer.Finalizers, mirrorPeerFinalizer)
			if err := r.Client.Update(ctx, &mirrorPeer); err != nil {
				logger.Info("Failed to remove finalizer from MirrorPeer")
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

	if val, ok := mirrorPeerCopy.Labels[hubRecoveryLabel]; !ok || val != "resource" {
		logger.Info("Adding label to mirrorpeer for disaster recovery")
		mirrorPeerCopy.Labels[hubRecoveryLabel] = "resource"
		err = r.Client.Update(ctx, mirrorPeerCopy)

		if err != nil {
			return checkK8sUpdateErrors(err, mirrorPeerCopy)
		}
		logger.Info("Successfully added label to mirrorpeer for disaster recovery", "Mirrorpeer", mirrorPeerCopy.Name)
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
			logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
			// Requeue, but don't throw
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if err := r.processManagedClusterAddon(ctx, mirrorPeer); err != nil {
		return ctrl.Result{}, err
	}

	err = r.createClusterRoleBindingsForSpoke(ctx, mirrorPeer)
	if err != nil {
		logger.Error(err, "Failed to create cluster role bindings for spoke")
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
				logger.Error(err, "error in fetching s3 internal secret", "peerref", peerRef.ClusterName, "MirrorPeer", mirrorPeer)
				return ctrl.Result{}, err
			}
			err = createOrUpdateSecretsFromInternalSecret(ctx, r.Client, &s3Secret, []multiclusterv1alpha1.MirrorPeer{mirrorPeer})
			if err != nil {
				logger.Error(err, "error in updating S3 profile", "peerref", peerRef.ClusterName, "MirrorPeer", mirrorPeer)
				return ctrl.Result{}, err
			}
		}
	}

	if err = processMirrorPeerSecretChanges(ctx, r.Client, mirrorPeer); err != nil {
		return ctrl.Result{}, err
	}

	err = r.createDRClusters(ctx, &mirrorPeer)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("resource not found, retrying to create DRCluster", "MirrorPeer", mirrorPeer)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to create DRClusters for MirrorPeer", "MirrorPeer", mirrorPeer.Name)
		return ctrl.Result{}, err
	}

	return r.updateMirrorPeerStatus(ctx, mirrorPeer)
}

// processManagedClusterAddon creates an addon for the cluster management in all the peer refs,
// the resources gets an owner ref of the mirrorpeer to let the garbage collector handle it if the mirrorpeer gets deleted
func (r *MirrorPeerReconciler) processManagedClusterAddon(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	// Create or Update ManagedClusterAddon
	for i := range mirrorPeer.Spec.Items {
		var managedClusterAddOn addonapiv1alpha1.ManagedClusterAddOn
		if err := r.Client.Get(ctx, types.NamespacedName{
			Name:      tokenExchange.TokenExchangeName,
			Namespace: mirrorPeer.Spec.Items[i].ClusterName,
		}, &managedClusterAddOn); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Cannot find managedClusterAddon, creating")
				annotations := make(map[string]string)
				annotations[utils.DRModeAnnotationKey] = string(mirrorPeer.Spec.Type)

				managedClusterAddOn = addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:        tokenExchange.TokenExchangeName,
						Namespace:   mirrorPeer.Spec.Items[i].ClusterName,
						Annotations: annotations,
					},
				}
			}
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &managedClusterAddOn, func() error {
			managedClusterAddOn.Spec.InstallNamespace = mirrorPeer.Spec.Items[i].StorageClusterRef.Namespace
			return controllerutil.SetOwnerReference(&mirrorPeer, &managedClusterAddOn, r.Scheme)
		})
		if err != nil {
			logger.Error(err, "Failed to reconcile ManagedClusterAddOn.", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name))
			return err
		}
	}
	return nil
}

// deleteSecrets checks if another mirrorpeer is using a peer ref in the mirrorpeer being deleted, if not then it
// goes ahead and deletes all the secrets with blue, green and internal label.
// If two mirrorpeers are pointing to the same peer ref, but only gets deleted the orphan green secret in
// the still standing peer ref gets deleted by the mirrorpeer secret controller
func (r *MirrorPeerReconciler) deleteSecrets(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	for i := range mirrorPeer.Spec.Items {
		peerRefUsed, err := utils.DoesAnotherMirrorPeerPointToPeerRef(ctx, r.Client, &mirrorPeer.Spec.Items[i])
		if err != nil {
			return err
		}
		if !peerRefUsed {
			secretLabels := []string{string(utils.SourceLabel), string(utils.DestinationLabel)}

			if mirrorPeer.Spec.ManageS3 {
				secretLabels = append(secretLabels, string(utils.InternalLabel))
			}

			secretRequirement, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.In, secretLabels)
			if err != nil {
				logger.Error(err, "cannot parse new requirement")
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
				logger.Error(err, "Error while deleting secrets for MirrorPeer", "MirrorPeer", mirrorPeer.Name)
			}
		}
	}
	return nil
}

func processMirrorPeerSecretChanges(ctx context.Context, rc client.Client, mirrorPeerObj multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	var anyErr error

	for _, eachPeerRef := range mirrorPeerObj.Spec.Items {
		sourceSecrets, err := fetchAllSourceSecrets(ctx, rc, eachPeerRef.ClusterName)
		if err != nil {
			logger.Error(err, "Unable to get a list of source secrets", "namespace", eachPeerRef.ClusterName)
			anyErr = err
			continue
		}
		// get the source secret associated with the PeerRef
		matchingSourceSecret := utils.FindMatchingSecretWithPeerRef(eachPeerRef, sourceSecrets)
		// if no match found (ie; no source secret found); just continue
		if matchingSourceSecret == nil {
			continue
		}
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, matchingSourceSecret, mirrorPeerObj)
		if err != nil {
			logger.Error(err, "Error while updating Destination secrets", "source-secret", matchingSourceSecret.Name, "namespace", matchingSourceSecret.Namespace)
			anyErr = err
		}
	}
	if anyErr == nil {
		// if there are no other errors,
		// cleanup any other orphan destination secrets
		anyErr = processDestinationSecretCleanup(ctx, rc)
	}
	return anyErr
}

func (r *MirrorPeerReconciler) checkTokenExchangeStatus(ctx context.Context, mp multiclusterv1alpha1.MirrorPeer) (bool, error) {
	logger := log.FromContext(ctx)

	//TODO Add support for more peer refs when applicable
	pr1 := mp.Spec.Items[0]
	pr2 := mp.Spec.Items[1]

	err := r.checkForSourceSecret(ctx, pr1)
	if err != nil {
		logger.Error(err, "Failed to find valid source secret", "PeerRef", pr1)
		return false, err
	}

	err = r.checkForDestinationSecret(ctx, pr1, pr2.ClusterName)
	if err != nil {
		logger.Error(err, "Failed to find valid destination secret", "PeerRef", pr1)
		return false, err
	}

	err = r.checkForSourceSecret(ctx, pr2)
	if err != nil {
		logger.Error(err, "Failed to find valid source secret", "PeerRef", pr2)
		return false, err
	}

	err = r.checkForDestinationSecret(ctx, pr2, pr1.ClusterName)
	if err != nil {
		logger.Error(err, "Failed to find destination secret", "PeerRef", pr2)
		return false, err
	}

	return true, nil
}

func (r *MirrorPeerReconciler) checkForSourceSecret(ctx context.Context, peerRef multiclusterv1alpha1.PeerRef) error {
	logger := log.FromContext(ctx)
	prSecretName := utils.GetSecretNameByPeerRef(peerRef)
	var peerSourceSecret corev1.Secret
	err := r.Client.Get(ctx, types.NamespacedName{
		Name: prSecretName,
		// Source Namespace for the secret
		Namespace: peerRef.ClusterName,
	}, &peerSourceSecret)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Source secret not found", "Secret", prSecretName)
			return err
		}
	}

	err = utils.ValidateSourceSecret(&peerSourceSecret)
	if err != nil {
		return err
	}

	return nil
}
func (r *MirrorPeerReconciler) checkForDestinationSecret(ctx context.Context, peerRef multiclusterv1alpha1.PeerRef, destNamespace string) error {
	logger := log.FromContext(ctx)
	prSecretName := utils.GetSecretNameByPeerRef(peerRef)
	var peerDestinationSecret corev1.Secret
	err := r.Client.Get(ctx, types.NamespacedName{
		Name: prSecretName,
		// Destination Namespace for the secret
		Namespace: destNamespace,
	}, &peerDestinationSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Destination secret not found", "Secret", prSecretName)
			return err
		}
	}
	err = utils.ValidateDestinationSecret(&peerDestinationSecret)
	if err != nil {
		return err
	}

	return nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)
}

// CheckK8sUpdateErrors checks what type of error occurs when trying to update a k8s object
// and logs according to the object
func checkK8sUpdateErrors(err error, obj client.Object) (ctrl.Result, error) {
	if k8serrors.IsConflict(err) {
		klog.Info("Object is being updated by another process. Retrying", obj.GetObjectKind(), obj.GetName())
		return ctrl.Result{Requeue: true}, nil
	} else if k8serrors.IsNotFound(err) {
		klog.Info("Object no longer exists. Ignoring since object must have been deleted", obj.GetObjectKind(), obj.GetName())
		return ctrl.Result{}, nil
	} else if err != nil {
		klog.Info("Warning: Failed to update object", obj.GetName(), "Error", err)
	}
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) createDRClusters(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	for _, pr := range mp.Spec.Items {
		clusterName := pr.ClusterName
		s3SecretName := utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)

		dc := ramenv1alpha1.DRCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		}

		if mp.Spec.Type == multiclusterv1alpha1.Async {
			rookSecretName := utils.GetSecretNameByPeerRef(pr)

			hs, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: rookSecretName, Namespace: clusterName})
			if err != nil {
				logger.Error(err, "Failed to fetch rook secret", "Secret", rookSecretName)
				return err
			}

			rt, err := utils.UnmarshalHubSecret(hs)
			if err != nil {
				logger.Error(err, "Failed to unmarshal rook secret", "Secret", rookSecretName)
				return err
			}
			dc.Spec.Region = ramenv1alpha1.Region(rt.FSID)
		}
		ss, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: s3SecretName, Namespace: clusterName})
		if err != nil {
			logger.Error(err, "Failed to fetch s3 secret", "Secret", s3SecretName)
			return err
		}

		st, err := utils.UnmarshalS3Secret(ss)
		if err != nil {
			logger.Error(err, "Failed to unmarshal s3 secret", "Secret", s3SecretName)
			return err
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &dc, func() error {
			dc.Spec.S3ProfileName = st.S3ProfileName
			return nil
		})

		if err != nil {
			return err
		}

	}
	return nil
}

func (r *MirrorPeerReconciler) createClusterRoleBindingsForSpoke(ctx context.Context, peer multiclusterv1alpha1.MirrorPeer) error {
	crb := rbacv1.ClusterRoleBinding{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: spokeClusterRoleBindingName}, &crb)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
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
		}

		if !utils.ContainsSubject(subjects, gsub) {
			subjects = append(subjects, *gsub)
		}
	}

	spokeRoleBinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: spokeClusterRoleBindingName,
		},
		Subjects: subjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "open-cluster-management:token-exchange:agent",
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &spokeRoleBinding, func() error {
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (r *MirrorPeerReconciler) updateMirrorPeerStatus(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		tokensExchanged, err := r.checkTokenExchangeStatus(ctx, mirrorPeer)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Secrets not found; Attempting to reconcile again")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Info("Error while exchanging tokens", "MirrorPeer", mirrorPeer)
			mirrorPeer.Status.Message = err.Error()
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
			}
			return ctrl.Result{}, err
		}

		if tokensExchanged {
			logger.Info("Tokens exchanged", "MirrorPeer", mirrorPeer)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangedSecret
			mirrorPeer.Status.Message = ""
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error(statusErr, "Error occured while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
				// Requeue, but don't throw
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil
		}
	} else {
		// Sync mode status update, same flow as async but for s3 profile
		s3ProfileSynced, err := r.checkS3ProfileStatus(ctx, mirrorPeer)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Secrets not found; Attempting to reconcile again")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Info("Error while syncing S3 Profile", "MirrorPeer", mirrorPeer)
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
			}
			return ctrl.Result{}, err
		}

		if s3ProfileSynced {
			logger.Info("S3Profile synced to hub", "MirrorPeer", mirrorPeer)
			mirrorPeer.Status.Phase = multiclusterv1alpha1.S3ProfileSynced
			mirrorPeer.Status.Message = ""
			statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
			if statusErr != nil {
				logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
				// Requeue, but don't throw
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *MirrorPeerReconciler) checkS3ProfileStatus(ctx context.Context, mp multiclusterv1alpha1.MirrorPeer) (bool, error) {
	// If S3Profile secret can be fetched for each peerref in the MirrorPeer then the sync is successful
	for _, pr := range mp.Spec.Items {
		clusterName := pr.ClusterName
		s3SecretName := utils.GetSecretNameByPeerRef(pr, utils.S3ProfilePrefix)
		_, err := utils.FetchSecretWithName(ctx, r.Client, types.NamespacedName{Name: s3SecretName, Namespace: clusterName})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func getSubjectByPeerRef(pr multiclusterv1alpha1.PeerRef, kind string) *rbacv1.Subject {
	switch kind {
	case "User":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultUser(pr.ClusterName, addons.TokenExchangeName, addons.TokenExchangeName),
			APIGroup: "rbac.authorization.k8s.io",
		}
	case "Group":
		return &rbacv1.Subject{
			Kind:     kind,
			Name:     agent.DefaultGroups(pr.ClusterName, addons.TokenExchangeName)[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
	default:
		return nil
	}
}
