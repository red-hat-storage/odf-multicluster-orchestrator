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

package addons

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MirrorPeer", req.NamespacedName.String())
	logger.Info("Running MirrorPeer reconciler on spoke cluster")

	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := r.HubClient.Get(ctx, req.NamespacedName, &mirrorPeer)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MirrorPeer not found, ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to retrieve MirrorPeer", "error", err)
		return ctrl.Result{}, err
	}

	var scr *multiclusterv1alpha1.StorageClusterRef
	currentNamespace := os.Getenv("POD_NAMESPACE")
	sc, err := utils.GetStorageClusterFromCurrentNamespace(ctx, r.SpokeClient, currentNamespace)
	if err != nil {
		logger.Error("Failed to fetch StorageCluster for given namespace", "Namespace", currentNamespace)
		return ctrl.Result{}, err
	}
	scr = &multiclusterv1alpha1.StorageClusterRef{
		Name:      sc.Name,
		Namespace: sc.Namespace,
	}

	agentFinalizer := r.SpokeClusterName + "." + SpokeMirrorPeerFinalizer
	if len(agentFinalizer) > 63 {
		agentFinalizer = fmt.Sprintf("%s.%s", r.SpokeClusterName[0:10], SpokeMirrorPeerFinalizer)
	}

	if mirrorPeer.GetDeletionTimestamp().IsZero() {
		if !utils.ContainsString(mirrorPeer.GetFinalizers(), agentFinalizer) {
			logger.Info("Adding finalizer to MirrorPeer", "finalizer", agentFinalizer)
			mirrorPeer.Finalizers = append(mirrorPeer.Finalizers, agentFinalizer)
			if err := r.HubClient.Update(ctx, &mirrorPeer); err != nil {
				logger.Error("Failed to add finalizer to MirrorPeer", "error", err)
				return ctrl.Result{}, err
			}
		}
	} else {
		// TODO Write complete deletion for Provider mode mirrorpeer

		err = r.HubClient.Get(ctx, req.NamespacedName, &mirrorPeer)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("MirrorPeer deleted during reconciling, skipping")
				return ctrl.Result{}, nil
			}
			logger.Error("Failed to retrieve MirrorPeer after deletion", "error", err)
			return ctrl.Result{}, err
		}
		mirrorPeer.Finalizers = utils.RemoveString(mirrorPeer.Finalizers, agentFinalizer)

		if err := r.HubClient.Update(ctx, &mirrorPeer); err != nil {
			logger.Error("Failed to remove finalizer from MirrorPeer", "error", err)
			return ctrl.Result{}, err
		}
		logger.Info("MirrorPeer deletion complete")
		return ctrl.Result{}, nil
	}

	logger.Info("Creating S3 buckets")
	err = r.createS3(ctx, mirrorPeer, scr.Namespace)
	if err != nil {
		logger.Error("Failed to create ODR S3 resources", "error", err)
		return ctrl.Result{}, err
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		if mirrorPeer.Status.Phase == multiclusterv1alpha1.ExchangedSecret {
			logger.Info("Cleaning up stale onboarding token", "Token", string(mirrorPeer.GetUID()))
			err = deleteStorageClusterPeerTokenSecret(ctx, r.HubClient, r.SpokeClusterName, string(mirrorPeer.GetUID()))
			if err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		if mirrorPeer.Status.Phase == multiclusterv1alpha1.ExchangingSecret {
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
	}

	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) createS3(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string) error {
	bucketNamespace := utils.GetEnv("ODR_NAMESPACE", scNamespace)
	bucketName := utils.GenerateBucketName(mirrorPeer)
	annotations := map[string]string{
		utils.MirrorPeerNameAnnotationKey: mirrorPeer.Name,
	}

	operationResult, err := utils.CreateOrUpdateObjectBucketClaim(ctx, r.SpokeClient, bucketName, bucketNamespace, annotations)
	if err != nil {
		return err
	}
	r.Logger.Info(fmt.Sprintf("ObjectBucketClaim %s was %s in namespace %s", bucketName, operationResult, bucketNamespace))

	return nil
}

func (r *MirrorPeerReconciler) hasSpokeCluster(obj client.Object) bool {
	mp, ok := obj.(*multiclusterv1alpha1.MirrorPeer)
	if !ok {
		return false
	}
	for _, v := range mp.Spec.Items {
		if v.ClusterName == r.SpokeClusterName {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mirrorPeerSpokeClusterPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.hasSpokeCluster(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return r.hasSpokeCluster(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.hasSpokeCluster(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	r.Logger.Info("Setting up controller with manager")
	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{}, mirrorPeerSpokeClusterPredicate)
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)
}
