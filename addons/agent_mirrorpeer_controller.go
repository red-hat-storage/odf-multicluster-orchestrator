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

package addons

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	HubClient            client.Client
	Scheme               *runtime.Scheme
	SpokeClient          client.Client
	SpokeClusterName     string
	OdfOperatorNamespace string
	Logger               *slog.Logger

	TestEnvFile          string
	CurrentNamespace     string
	HubOperatorNamespace string
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Running MirrorPeer reconciler on spoke cluster")

	mirrorPeer := &multiclusterv1alpha1.MirrorPeer{}
	err := r.HubClient.Get(ctx, req.NamespacedName, mirrorPeer)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MirrorPeer not found, ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to retrieve MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	cm, err := odf.GetClientInfoConfigMap(ctx, r.HubClient, r.HubOperatorNamespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	hasStorageClientRef, err := odf.IsStorageClientType(mirrorPeer, cm.Data)
	logger.Info("MirrorPeer has client reference?", "True/False", hasStorageClientRef)

	if err != nil {
		logger.Error("Failed to check if storage client ref exists", "error", err)
		return ctrl.Result{}, err
	}

	var scr *multiclusterv1alpha1.StorageClusterRef
	if hasStorageClientRef {
		sc, err := odf.GetStorageClusterFromCurrentNamespace(ctx, r.SpokeClient, r.CurrentNamespace)
		if err != nil {
			logger.Error("Failed to fetch StorageCluster for given namespace", "Namespace", r.CurrentNamespace)
			return ctrl.Result{}, err
		}
		scr = &multiclusterv1alpha1.StorageClusterRef{
			Name:      sc.Name,
			Namespace: sc.Namespace,
		}
	} else {
		scr, err = odf.GetCurrentStorageClusterRef(mirrorPeer, r.SpokeClusterName)
		if err != nil {
			logger.Error("Failed to get current storage cluster ref", "error", err)
			return ctrl.Result{}, err
		}
	}

	agentFinalizer := r.SpokeClusterName + "." + utils.SpokeMirrorPeerFinalizer
	if len(agentFinalizer) > 63 {
		agentFinalizer = fmt.Sprintf("%s.%s", r.SpokeClusterName[0:10], utils.SpokeMirrorPeerFinalizer)
	}

	if mirrorPeer.GetDeletionTimestamp().IsZero() {
		if controllerutil.AddFinalizer(mirrorPeer, agentFinalizer) {
			logger.Info("Adding finalizer to MirrorPeer", "finalizer", agentFinalizer)
			if err := r.HubClient.Update(ctx, mirrorPeer); err != nil {
				logger.Error("Failed to add finalizer to MirrorPeer", "error", err)
				return ctrl.Result{}, err
			}
		}
	} else {
		var addonDeletionlock corev1.ConfigMap
		if err = r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: r.CurrentNamespace, Name: utils.AddonDeletionlockName}, &addonDeletionlock); err != nil {
			return ctrl.Result{}, err
		}

		// Remove finalizer if present from previous versions.
		if controllerutil.RemoveFinalizer(&addonDeletionlock, utils.ResourceDistributionFinalizer) {
			if err := r.SpokeClient.Update(ctx, &addonDeletionlock); err != nil {
				return ctrl.Result{}, err
			}
		}

		addonKey := ""
		for _, peer := range mirrorPeer.Spec.Items {
			cInfo, err := odf.GetClientInfoFromConfigMap(cm.Data, utils.GetKey(peer.ClusterName, peer.StorageClusterRef.Name))
			if err != nil {
				return ctrl.Result{}, err
			}
			if cInfo.ProviderInfo.ProviderManagedClusterName == r.SpokeClusterName {
				addonKey = cInfo.ClientID
				break
			}
		}

		if _, ok := addonDeletionlock.Data[addonKey]; ok {
			logger.Info("Dependent resources like templates for mirrorpeer are not yet deleted, requing")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		err = r.HubClient.Get(ctx, req.NamespacedName, mirrorPeer)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("MirrorPeer deleted during reconciling, skipping")
				return ctrl.Result{}, nil
			}
			logger.Error("Failed to retrieve MirrorPeer after deletion", "error", err)
			return ctrl.Result{}, err
		}
		if controllerutil.RemoveFinalizer(mirrorPeer, agentFinalizer) {
			if err := r.HubClient.Update(ctx, mirrorPeer); err != nil {
				return ctrl.Result{}, err
			}
		}

		logger.Info("MirrorPeer deletion complete")
		return ctrl.Result{}, nil
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async && hasStorageClientRef {
		// TODO(techdebt): Ideally we'd like to cleanup tokens after use and not re-generate token once clients are peered.
		// But, we currently lack the machinery to make that decision precisely. As a middleground, we will generate a token
		// and not clean it up. We will re-generate it when it expires.
		// if mirrorPeer.Status.Phase == multiclusterv1alpha1.ExchangedSecret {
		// 	logger.Info("Cleaning up stale onboarding token", "Token", string(mirrorPeer.GetUID()))
		// 	err = deleteStorageClusterPeerTokenSecret(ctx, r.HubClient, r.SpokeClusterName, string(mirrorPeer.GetUID()))
		// 	if err != nil {
		// 		return ctrl.Result{}, err
		// 	}
		// 	return ctrl.Result{}, nil
		// }
		// if mirrorPeer.Status.Phase == multiclusterv1alpha1.ExchangingSecret {
		// }
		var token corev1.Secret
		err = r.HubClient.Get(ctx, types.NamespacedName{Namespace: r.SpokeClusterName, Name: string(mirrorPeer.GetUID())}, &token)
		if err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if err == nil {
			logger.Info("Trying to unmarshal onboarding token.")
			ticketData, err := UnmarshalOnboardingToken(&token)
			if err != nil {
				logger.Error("Failed to unmarshal the onboarding ticket data")
				return ctrl.Result{}, err
			}
			logger.Info("Successfully unmarshalled onboarding ticket", "ticketData", ticketData)
			if ticketData.ExpirationDate > time.Now().Unix() {
				logger.Info("Onboarding token has not expired yet. Not renewing it.", "Token", token.Name, "ExpirationDate", ticketData.ExpirationDate)
				return ctrl.Result{}, nil
			}
			logger.Info("Onboarding token has expired. Deleting it", "Token", token.Name)
			err = deleteStorageClusterPeerTokenSecret(ctx, r.HubClient, r.SpokeClusterName, string(mirrorPeer.GetUID()))
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		logger.Info("Creating a new onboarding token", "Token", token.Name)
		err = createStorageClusterPeerTokenSecret(ctx, r.HubClient, r.Scheme, r.SpokeClusterName, r.OdfOperatorNamespace, mirrorPeer, scr)
		if err != nil {
			logger.Error("Failed to create StorageCluster peer token on the hub.", "error", err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) hasSpokeCluster(obj client.Object) bool {
	mp, ok := obj.(*multiclusterv1alpha1.MirrorPeer)
	if !ok {
		return false
	}
	if mp.Status.Phase == multiclusterv1alpha1.Failed {
		return false
	}
	for _, v := range mp.Spec.Items {
		if v.ClusterName == r.SpokeClusterName {
			return true
		}
	}
	return false
}

func (r *MirrorPeerReconciler) hasProviderSpokeCluster(obj client.Object) bool {
	mp, ok := obj.(*multiclusterv1alpha1.MirrorPeer)
	if !ok {
		return false
	}
	if mp.Status.Phase == multiclusterv1alpha1.Failed {
		return false
	}
	peerRefs, err := odf.GetPeerRefForProviderCluster(context.TODO(), r.SpokeClient, r.HubClient, mp)
	if err != nil {
		r.Logger.Error("Unable to reconcile MirrorPeer", "MirrorPeer", mp.GetName(), "Error", err)
		return false
	}
	if len(peerRefs) > 0 {
		return true
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mirrorPeerSpokeClusterPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.hasSpokeCluster(e.Object) || r.hasProviderSpokeCluster(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return r.hasSpokeCluster(e.Object) || r.hasProviderSpokeCluster(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.hasSpokeCluster(e.ObjectNew) || r.hasProviderSpokeCluster(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	r.Logger.Info("Setting up controller with manager")
	mpPredicate := predicate.And(predicate.GenerationChangedPredicate{}, mirrorPeerSpokeClusterPredicate)
	return ctrl.NewControllerManagedBy(mgr).
		Named("agent_mirrorpeer_controller").
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)
}
