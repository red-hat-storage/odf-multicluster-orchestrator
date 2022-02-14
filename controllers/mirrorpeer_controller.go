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

	tokenexchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
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

	mirrorPeerCopy := mirrorPeer.DeepCopy()
	if mirrorPeerCopy.Labels == nil {
		mirrorPeerCopy.Labels = make(map[string]string)
	}

	if val, ok := mirrorPeerCopy.Labels[hubRecoveryLabel]; !ok || val != "resource" {
		logger.Info("Adding label to mirrorpeer for disaster recovery")
		mirrorPeerCopy.Labels[hubRecoveryLabel] = "resource"
		err = r.Client.Update(ctx, mirrorPeerCopy)

		if k8serrors.IsConflict(err) {
			logger.Info("MirrorPeer is being updated by another process. Retrying", "MirrorPeer", mirrorPeerCopy.Name)
			return ctrl.Result{Requeue: true}, nil
		} else if k8serrors.IsNotFound(err) {
			logger.Info("MirrorPeer no longer exists. Ignoring since object must have been deleted", "MirrorPeer", mirrorPeerCopy.Name)
			return ctrl.Result{}, nil
		} else if err != nil {
			logger.Info("Warning: Failed to update mirrorpeer", "MirrorPeer", mirrorPeerCopy.Name, "Error", err)
		} else {
			logger.Info("Successfully added label to mirrorpeer for disaster recovery", "MirrorPeer", mirrorPeerCopy.Name)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if mirrorPeer.Status.Phase == "" {
		mirrorPeer.Status.Phase = multiclusterv1alpha1.ExchangingSecret
		statusErr := r.Client.Status().Update(ctx, &mirrorPeer)
		if statusErr != nil {
			logger.Error(statusErr, "Error occurred while updating the status of mirrorpeer", "MirrorPeer", mirrorPeer)
			// Requeue, but don't throw
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Create or Update ManagedClusterAddon
	for i := range mirrorPeer.Spec.Items {
		managedClusterAddOn := addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tokenexchange.TokenExchangeName,
				Namespace: mirrorPeer.Spec.Items[i].ClusterName,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &managedClusterAddOn, func() error {
			managedClusterAddOn.Spec.InstallNamespace = mirrorPeer.Spec.Items[i].StorageClusterRef.Namespace
			return nil
		})
		if err != nil {
			if k8serrors.IsAlreadyExists(err) {
				// If ManagedClusterAddOn already exists no need to return anything
				// We can move on to check for next item in a loop
				logger.Info("ManagedClusterAddOn already exists", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name))
				continue
			}
			logger.Error(err, "Failed to reconcile ManagedClusterAddOn.", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name))
			return ctrl.Result{}, err
		}
	}

	// update s3 profile when MirrorPeer changes
	if mirrorPeer.Spec.ManageS3 {
		for _, peerRef := range mirrorPeer.Spec.Items {
			var s3Secret corev1.Secret
			namespacedName := types.NamespacedName{
				Name:      common.CreateUniqueSecretName(peerRef.ClusterName, peerRef.StorageClusterRef.Namespace, peerRef.StorageClusterRef.Name, common.S3ProfilePrefix),
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

	err = processMirrorPeerSecretChanges(ctx, r.Client, mirrorPeer)
	if err != nil {
		return ctrl.Result{}, err
	}
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

	return ctrl.Result{Requeue: true}, nil
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
		matchingSourceSecret := common.FindMatchingSecretWithPeerRef(eachPeerRef, sourceSecrets)
		// if no match found (ie; no source secret found); just continue
		if matchingSourceSecret == nil {
			continue
		}
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, matchingSourceSecret, mirrorPeerObj)
		if err != nil {
			logger.Error(err, "Error while updating Destination secrets", "source-secret", *matchingSourceSecret)
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
	prSecretName := common.CreateUniqueSecretName(peerRef.ClusterName, peerRef.StorageClusterRef.Namespace, peerRef.StorageClusterRef.Name)
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

	err = common.ValidateSourceSecret(&peerSourceSecret)
	if err != nil {
		return err
	}

	return nil
}
func (r *MirrorPeerReconciler) checkForDestinationSecret(ctx context.Context, peerRef multiclusterv1alpha1.PeerRef, destNamespace string) error {
	logger := log.FromContext(ctx)
	prSecretName := common.CreateUniqueSecretName(peerRef.ClusterName, peerRef.StorageClusterRef.Namespace, peerRef.StorageClusterRef.Name)
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
	err = common.ValidateDestinationSecret(&peerDestinationSecret)
	if err != nil {
		return err
	}

	return nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mpPredicate := common.ComposePredicates(predicate.GenerationChangedPredicate{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)
}
