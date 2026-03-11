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
	"strconv"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

	hasStorageClientRef, err := utils.IsStorageClientType(ctx, r.SpokeClient, mirrorPeer, true)
	logger.Info("MirrorPeer has client reference?", "True/False", hasStorageClientRef)

	if err != nil {
		logger.Error("Failed to check if storage client ref exists", "error", err)
		return ctrl.Result{}, err
	}

	var scr *multiclusterv1alpha1.StorageClusterRef
	if hasStorageClientRef {
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
	} else {
		scr, err = utils.GetCurrentStorageClusterRef(&mirrorPeer, r.SpokeClusterName)
		if err != nil {
			logger.Error("Failed to get current storage cluster ref", "error", err)
			return ctrl.Result{}, err
		}
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
		if !hasStorageClientRef {
			result, err := r.deleteMirrorPeer(ctx, mirrorPeer, scr)
			if err != nil {
				return result, err
			}
		} // TODO Write complete deletion for Provider mode mirrorpeer

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
	err = r.createS3(ctx, mirrorPeer, scr.Namespace, hasStorageClientRef)
	if err != nil {
		logger.Error("Failed to create ODR S3 resources", "error", err)
		return ctrl.Result{}, err
	}

	if !hasStorageClientRef {
		logger.Info("Fetching StorageIds")
		clusterStorageIds, err := r.fetchClusterStorageIds(ctx, &mirrorPeer, types.NamespacedName{Namespace: scr.Namespace, Name: scr.Name})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to fetch cluster storage IDs: %v", err)
		}

		logger.Info("Labeling the default StorageClasses")
		storageIds := make(map[utils.CephType]string)
		for k, v := range clusterStorageIds[r.SpokeClusterName] {
			storageIds[utils.CephType(k)] = v
		}

		err = labelDefaultStorageClasses(ctx, logger, r.SpokeClient, scr.Name, scr.Namespace, storageIds)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("an unknown error has occurred while labelling default StorageClasses: %v", err)
		}

		logger.Info("Labeled the default StorageClasses successfully")
		logger.Info("Labeling the default VolumeSnapshotClasses")

		err = labelDefaultVolumeSnapshotClasses(ctx, logger, r.SpokeClient, scr.Name, scr.Namespace, storageIds)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("an unknown error has occurred while labelling default VolumeSnapshotClasses: %v", err)
		}

		logger.Info("Labeled the default VolumeSnapshotClasses successfully")

		if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
			logger.Info("Enabling async mode dependencies")
			storageId1 := clusterStorageIds[mirrorPeer.Spec.Items[0].ClusterName]["rbd"]
			storageId2 := clusterStorageIds[mirrorPeer.Spec.Items[1].ClusterName]["rbd"]
			err = r.labelCephClusters(ctx, scr, storageId1, storageId2)
			if err != nil {
				logger.Error("Failed to label cephcluster", "error", err)
				return ctrl.Result{}, err
			}
			err = r.enableCSIAddons(ctx, scr.Namespace)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to start CSI Addons for rook: %v", err)
			}

			err = r.enableMirroring(ctx, scr.Name, scr.Namespace, &mirrorPeer)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to enable mirroring the storagecluster %q in namespace %q in managed cluster: %v", scr.Name, scr.Namespace, err)
			}

		}
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
		// }
	}

	return ctrl.Result{}, nil
}

func labelDefaultStorageClasses(ctx context.Context, logger *slog.Logger, client client.Client, storageClusterName string, storageClusterNamespace string, storageIdsMap map[utils.CephType]string) error {
	storageClasses, err := utils.GetDefaultStorageClasses(ctx, client, storageClusterName)
	if err != nil {
		return err
	}

	for _, sc := range storageClasses {
		storageClassType, err := utils.GetStorageClassType(sc, storageClusterNamespace)
		if err != nil {
			return err
		}

		if sc.Labels == nil {
			sc.Labels = make(map[string]string)
		}

		logger.Info("Labelling StorageClass", "StorageClass", sc.Name)
		sc.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] = storageIdsMap[storageClassType]

		if err := client.Update(ctx, sc); err != nil {
			return fmt.Errorf("failed to update StorageClass %s: %v", sc.Name, err)
		}
	}

	return nil
}

func labelDefaultVolumeSnapshotClasses(ctx context.Context, logger *slog.Logger, client client.Client, storageClusterName string, storageClusterNamespace string, storageIdsMap map[utils.CephType]string) error {
	volumesnapshotclasses, err := utils.GetDefaultVolumeSnapshotClasses(ctx, client, storageClusterName)
	logger.Info("Found VSCs", "Count", len(volumesnapshotclasses))
	if err != nil {
		return err
	}

	for _, vsc := range volumesnapshotclasses {
		volumeSnapshotClassType, err := utils.GetVolumeSnapshotClassType(vsc, storageClusterNamespace)
		if err != nil {
			return err
		}

		if vsc.Labels == nil {
			vsc.Labels = make(map[string]string)
		}

		logger.Info("Labelling VolumeSnapshotClass", "VolumeSnapshotClass", vsc.Name)
		vsc.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] = storageIdsMap[volumeSnapshotClassType]

		if err := client.Update(ctx, vsc); err != nil {
			return fmt.Errorf("failed to update VolumeSnapshotClass %s: %v", vsc.Name, err)
		}
	}

	return nil
}

func (r *MirrorPeerReconciler) fetchClusterStorageIds(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, storageClusterNamespacedName types.NamespacedName) (map[string]map[string]string, error) {
	// Initialize map to store cluster name -> storage IDs mapping
	clusterStorageIds := make(map[string]map[string]string)

	for _, pr := range mp.Spec.Items {
		logger := r.Logger.With("clusterName", pr.ClusterName,
			"namespace", pr.StorageClusterRef.Namespace)

		// Get cluster type (converged/external)
		clusterType, err := utils.GetClusterType(pr.StorageClusterRef.Name,
			pr.StorageClusterRef.Namespace, r.SpokeClient)
		if err != nil {
			logger.Error("Failed to get cluster type", "error", err)
			return nil, fmt.Errorf("failed to get cluster type for %s: %v", pr.ClusterName, err)
		}

		var currentPeerStorageIds = make(map[string]string)

		// Handle local cluster vs remote cluster
		if r.SpokeClusterName == pr.ClusterName {
			// For local cluster, get storage IDs from storage classes
			storageIds, err := utils.GetStorageIdsForDefaultStorageClasses(ctx,
				r.SpokeClient,
				storageClusterNamespacedName,
				r.SpokeClusterName)
			if err != nil {
				logger.Error("Failed to get storage IDs from storage classes", "error", err)
				return nil, fmt.Errorf("failed to get storage IDs for local cluster %s: %v",
					pr.ClusterName, err)
			}

			for k, v := range storageIds {
				currentPeerStorageIds[string(k)] = v
			}
		} else if clusterType != utils.EXTERNAL {
			// For remote cluster, get storage IDs from secret
			secretName := r.getSecretNameByType(clusterType, pr)
			logger.Info("Checking secret",
				"secretName", secretName,
				"mode", clusterType)

			secret, err := utils.FetchSecretWithName(ctx, r.SpokeClient,
				types.NamespacedName{
					Name:      secretName,
					Namespace: pr.StorageClusterRef.Namespace,
				})
			if err != nil {
				logger.Error("Failed to fetch secret", "error", err)
				return nil, fmt.Errorf("failed to fetch secret %s: %v", secretName, err)
			}

			currentPeerStorageIds, err = utils.GetStorageIdsFromGreenSecret(secret)
			if err != nil {
				logger.Error("Failed to extract storage IDs from secret", "error", err)
				return nil, fmt.Errorf("failed to get storage IDs from secret %s: %v",
					secretName, err)
			}
		}

		clusterStorageIds[pr.ClusterName] = currentPeerStorageIds
		logger.Info("Storage IDs fetched for cluster",
			"storageIDs", currentPeerStorageIds)
	}

	r.Logger.Info("Successfully fetched all cluster storage IDs",
		"mirrorPeer", mp.Name,
		"clusterCount", len(clusterStorageIds))
	return clusterStorageIds, nil
}

func (r *MirrorPeerReconciler) getSecretNameByType(clusterType utils.ClusterType, pr multiclusterv1alpha1.PeerRef) string {
	var secretName string
	if clusterType == utils.CONVERGED {
		secretName = utils.GetSecretNameByPeerRef(pr)
	} else if clusterType == utils.EXTERNAL {
		secretName = DefaultExternalSecretName
	}
	return secretName
}

func (r *MirrorPeerReconciler) createS3(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string, hasStorageClientRef bool) error {
	bucketNamespace := utils.GetEnv("ODR_NAMESPACE", scNamespace)
	bucketName := utils.GenerateBucketName(mirrorPeer)
	annotations := map[string]string{
		utils.MirrorPeerNameAnnotationKey: mirrorPeer.Name,
	}
	if hasStorageClientRef {
		annotations[OBCTypeAnnotationKey] = string(CLIENT)

	} else {
		annotations[OBCTypeAnnotationKey] = string(CLUSTER)
	}

	operationResult, err := utils.CreateOrUpdateObjectBucketClaim(ctx, r.SpokeClient, bucketName, bucketNamespace, annotations)
	if err != nil {
		return err
	}
	r.Logger.Info(fmt.Sprintf("ObjectBucketClaim %s was %s in namespace %s", bucketName, operationResult, bucketNamespace))

	return nil
}

// enableMirroring is a wrapper function around toggleMirroring to enable mirroring in a storage cluster
func (r *MirrorPeerReconciler) enableMirroring(ctx context.Context, storageClusterName string, namespace string, mp *multiclusterv1alpha1.MirrorPeer) error {
	r.Logger.Info("Enabling mirroring on StorageCluster", "storageClusterName", storageClusterName, "namespace", namespace)
	return r.toggleMirroring(ctx, storageClusterName, namespace, true, mp)
}

// toggleMirroring changes the state of mirroring in the storage cluster
func (r *MirrorPeerReconciler) toggleMirroring(ctx context.Context, storageClusterName string, namespace string, enabled bool, mp *multiclusterv1alpha1.MirrorPeer) error {
	var sc ocsv1.StorageCluster
	err := r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      storageClusterName,
		Namespace: namespace,
	}, &sc)

	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("could not find StorageCluster %q in namespace %v: %v", storageClusterName, namespace, err)
		}
		r.Logger.Error("Failed to retrieve StorageCluster", "storageClusterName", storageClusterName, "namespace", namespace, "error", err)
		return err
	}

	// Determine if mirroring should be enabled or disabled
	if enabled {
		if sc.Spec.Mirroring == nil {
			sc.Spec.Mirroring = &ocsv1.MirroringSpec{}
		}
		oppPeers := getOppositePeerRefs(mp, r.SpokeClusterName)
		if hasRequiredSecret(sc.Spec.Mirroring.PeerSecretNames, oppPeers) {
			sc.Spec.Mirroring.Enabled = true
			r.Logger.Info("Mirroring enabled on StorageCluster", "storageClusterName", storageClusterName)
		} else {
			return fmt.Errorf("StorageCluster %q does not have required PeerSecrets", storageClusterName)
		}
	} else {
		sc.Spec.Mirroring = nil
		r.Logger.Info("Mirroring disabled on StorageCluster", "storageClusterName", storageClusterName)
	}

	err = r.SpokeClient.Update(ctx, &sc)
	if err != nil {
		r.Logger.Error("Failed to update StorageCluster mirroring settings", "storageClusterName", storageClusterName, "enabled", sc.Spec.Mirroring.Enabled, "error", err)
		return err
	}

	return nil
}

func getOppositePeerRefs(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) []multiclusterv1alpha1.PeerRef {
	peerRefs := make([]multiclusterv1alpha1.PeerRef, 0)
	for _, v := range mp.Spec.Items {
		if v.ClusterName != spokeClusterName {
			peerRefs = append(peerRefs, v)
		}
	}
	return peerRefs
}
func hasRequiredSecret(peerSecrets []string, oppositePeerRef []multiclusterv1alpha1.PeerRef) bool {
	for _, pr := range oppositePeerRef {
		sec := utils.GetSecretNameByPeerRef(pr)
		if !utils.ContainsString(peerSecrets, sec) {
			return false
		}
	}
	return true
}
func (r *MirrorPeerReconciler) enableCSIAddons(ctx context.Context, namespace string) error {
	err := r.toggleCSIAddons(ctx, namespace, true)
	if err != nil {
		r.Logger.Error("Failed to enable CSI addons", "namespace", namespace, "error", err)
		return err
	}
	r.Logger.Info("CSI addons enabled successfully", "namespace", namespace)
	return nil
}

func (r *MirrorPeerReconciler) toggleCSIAddons(ctx context.Context, namespace string, enabled bool) error {
	var rcm corev1.ConfigMap
	err := r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      RookConfigMapName,
		Namespace: namespace,
	}, &rcm)

	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("could not find rook-ceph-config-map in namespace %q: %v", namespace, err)
		}
		r.Logger.Error("Failed to retrieve rook-ceph-config-map", "namespace", namespace, "error", err)
		return err
	}

	if rcm.Data == nil {
		rcm.Data = make(map[string]string)
	}
	rcm.Data[RookCSIEnableKey] = strconv.FormatBool(enabled)
	err = r.SpokeClient.Update(ctx, &rcm)
	if err != nil {
		r.Logger.Error("Failed to update rook-ceph-config-map with CSI addon settings", "namespace", namespace, "enabled", enabled, "error", err)
		return err
	}

	r.Logger.Info("Rook-Ceph ConfigMap updated with new CSI addons setting", "namespace", namespace, "CSIEnabled", enabled)
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

	tokenToMirrorPeerMapFunc := func(ctx context.Context, obj client.Object) []ctrl.Request {
		reqs := []ctrl.Request{}
		var mirrorPeerList multiclusterv1alpha1.MirrorPeerList
		err := r.HubClient.List(ctx, &mirrorPeerList)
		if err != nil {
			r.Logger.Error("Unable to reconcile MirrorPeer based on token changes.", "error", err)
			return reqs
		}
		for _, mirrorpeer := range mirrorPeerList.Items {
			for _, peerRef := range mirrorpeer.Spec.Items {
				name := utils.GetSecretNameByPeerRef(peerRef)
				if name == obj.GetName() {
					reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: mirrorpeer.Name}})
					break
				}
			}
		}
		return reqs
	}

	r.Logger.Info("Setting up controller with manager")
	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{}, mirrorPeerSpokeClusterPredicate)
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(tokenToMirrorPeerMapFunc), builder.WithPredicates(utils.SourceOrDestinationPredicate)).
		Complete(r)
}

func (r *MirrorPeerReconciler) disableMirroring(ctx context.Context, storageClusterName string, namespace string, mp *multiclusterv1alpha1.MirrorPeer) error {
	r.Logger.Info("Disabling mirroring on StorageCluster", "storageClusterName", storageClusterName, "namespace", namespace)
	return r.toggleMirroring(ctx, storageClusterName, namespace, false, mp)
}

func (r *MirrorPeerReconciler) disableCSIAddons(ctx context.Context, namespace string) error {
	err := r.toggleCSIAddons(ctx, namespace, false)
	if err != nil {
		r.Logger.Error("Failed to disable CSI addons", "namespace", namespace, "error", err)
		return err
	}
	r.Logger.Info("CSI addons disabled successfully", "namespace", namespace)
	return nil
}

// deleteGreenSecret deletes the exchanged secret present in the namespace of the storage cluster
func (r *MirrorPeerReconciler) deleteGreenSecret(ctx context.Context, spokeClusterName string, scrNamespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	for _, peerRef := range mirrorPeer.Spec.Items {
		if peerRef.ClusterName != spokeClusterName {
			secretName := utils.GetSecretNameByPeerRef(peerRef)
			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: scrNamespace,
				},
			}
			err := r.SpokeClient.Delete(ctx, &secret)
			if err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Green secret not found, no action needed", "secretName", secretName, "namespace", scrNamespace)
					return nil
				}
				r.Logger.Error("Failed to delete green secret", "secretName", secretName, "namespace", scrNamespace, "error", err)
				return err
			}
			r.Logger.Info("Successfully deleted green secret", "secretName", secretName, "namespace", scrNamespace)
		}
	}
	return nil
}

// deleteS3 deletes the S3 bucket in the storage cluster namespace, each new mirrorpeer generates
// a new bucket, so we do not need to check if the bucket is being used by another mirrorpeer
func (r *MirrorPeerReconciler) deleteS3(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string) error {
	bucketName := utils.GenerateBucketName(mirrorPeer)
	bucketNamespace := utils.GetEnv("ODR_NAMESPACE", scNamespace)
	noobaaOBC, err := utils.GetObjectBucketClaim(ctx, r.SpokeClient, bucketName, bucketNamespace)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("ODR ObjectBucketClaim not found, skipping deletion", "namespace", scNamespace, "MirrorPeer", mirrorPeer.Name)
			return nil
		} else {
			r.Logger.Error("Failed to retrieve ODR ObjectBucketClaim", "namespace", scNamespace, "MirrorPeer", mirrorPeer.Name, "error", err)
			return err
		}
	}
	err = r.SpokeClient.Delete(ctx, noobaaOBC)
	if err != nil {
		r.Logger.Error("Failed to delete ODR ObjectBucketClaim", "ObjectBucketClaim", noobaaOBC.Name, "namespace", scNamespace, "error", err)
		return err
	}
	r.Logger.Info("Successfully deleted ODR ObjectBucketClaim", "ObjectBucketClaim", noobaaOBC.Name, "namespace", scNamespace)
	return nil
}

func (r *MirrorPeerReconciler) deleteMirrorPeer(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scr *multiclusterv1alpha1.StorageClusterRef) (ctrl.Result, error) {
	r.Logger.Info("MirrorPeer is being deleted", "MirrorPeer", mirrorPeer.Name)

	peerRef, err := utils.GetPeerRefForSpokeCluster(&mirrorPeer, r.SpokeClusterName)
	if err != nil {
		r.Logger.Error("Failed to get current PeerRef", "MirrorPeer", mirrorPeer.Name, "error", err)
		return ctrl.Result{}, err
	}

	peerRefUsed, err := utils.DoesAnotherMirrorPeerPointToPeerRef(ctx, r.HubClient, peerRef)
	if err != nil {
		r.Logger.Error("Failed to check if another MirrorPeer uses PeerRef", "error", err)
		return ctrl.Result{}, err
	}

	if !peerRefUsed {
		if err := r.disableMirroring(ctx, scr.Name, scr.Namespace, &mirrorPeer); err != nil {
			r.Logger.Error("Failed to disable mirroring for the StorageCluster", "StorageCluster", scr.Name, "namespace", scr.Namespace, "error", err)
			return ctrl.Result{}, fmt.Errorf("failed to disable mirroring for the StorageCluster %q in namespace %q. Error %v", scr.Name, scr.Namespace, err)
		}
		if err := r.disableCSIAddons(ctx, scr.Namespace); err != nil {
			r.Logger.Error("Failed to disable CSI Addons for Rook", "namespace", scr.Namespace, "error", err)
			return ctrl.Result{}, fmt.Errorf("failed to disable CSI Addons for rook: %v", err)
		}
	}

	if err := r.deleteGreenSecret(ctx, r.SpokeClusterName, scr.Namespace, &mirrorPeer); err != nil {
		r.Logger.Error("Failed to delete green secrets", "namespace", scr.Namespace, "error", err)
		return ctrl.Result{}, fmt.Errorf("failed to delete green secrets: %v", err)
	}

	if err := r.deleteS3(ctx, mirrorPeer, scr.Namespace); err != nil {
		r.Logger.Error("Failed to delete S3 buckets", "namespace", scr.Namespace, "error", err)
		return ctrl.Result{}, fmt.Errorf("failed to delete S3 buckets")
	}

	r.Logger.Info("Successfully completed the deletion of MirrorPeer resources", "MirrorPeer", mirrorPeer.Name)
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) labelCephClusters(ctx context.Context, scr *multiclusterv1alpha1.StorageClusterRef, storageId1, storageId2 string) error {
	r.Logger.Info("Labelling CephClusters with replication ID")
	cephClusters, err := utils.FetchAllCephClusters(ctx, r.SpokeClient)
	if err != nil {
		r.Logger.Error("Failed to fetch all CephClusters", "error", err)
		return err
	}
	if cephClusters == nil || len(cephClusters.Items) == 0 {
		r.Logger.Info("No CephClusters found to label")
		return nil
	}

	var found rookv1.CephCluster
	foundFlag := false
	for _, cc := range cephClusters.Items {
		for _, ref := range cc.OwnerReferences {
			if ref.Kind == "StorageCluster" && ref.Name == scr.Name {
				found = cc
				foundFlag = true
				break
			}
		}
		if foundFlag {
			break
		}
	}
	if !foundFlag {
		r.Logger.Info("No CephCluster matched the StorageCluster reference", "StorageClusterRef", scr.Name)
		return nil
	}

	if found.Labels == nil {
		found.Labels = make(map[string]string)
	}

	replicationId, err := utils.CreateUniqueReplicationId(storageId1, storageId2)
	if err != nil {
		r.Logger.Error("Failed to create unique replication ID", "error", err)
		return err
	}

	if found.Labels[utils.CephClusterReplicationIdLabel] != replicationId {
		r.Logger.Info("Adding label to CephCluster", "label", utils.CephClusterReplicationIdLabel, "replicationId", replicationId, "CephCluster", found.Name)
		found.Labels[utils.CephClusterReplicationIdLabel] = replicationId
		err = r.SpokeClient.Update(ctx, &found)
		if err != nil {
			r.Logger.Error("Failed to update CephCluster with new label", "CephCluster", found.Name, "label", utils.CephClusterReplicationIdLabel, "error", err)
			return err
		}
	} else {
		r.Logger.Info("CephCluster already labeled with replication ID", "CephCluster", found.Name, "replicationId", replicationId)
	}

	return nil
}
