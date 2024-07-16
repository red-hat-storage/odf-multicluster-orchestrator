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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	HubClient        client.Client
	Scheme           *runtime.Scheme
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger
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

	scr, err := utils.GetCurrentStorageClusterRef(&mirrorPeer, r.SpokeClusterName)
	if err != nil {
		logger.Error("Failed to get current storage cluster ref", "error", err)
		return ctrl.Result{}, err
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
		result, err := r.deleteMirrorPeer(ctx, mirrorPeer, scr)
		if err != nil {
			return result, err
		}
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
		clusterFSIDs := make(map[string]string)
		logger.Info("Fetching clusterFSIDs")
		err = r.fetchClusterFSIDs(ctx, &mirrorPeer, clusterFSIDs)
		if err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("an unknown error occurred while fetching the cluster fsids, retrying again: %v", err)
		}

		logger.Info("Enabling async mode dependencies")
		err = r.labelCephClusters(ctx, scr, clusterFSIDs)
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

		logger.Info("Labeling RBD storageclasses")
		errs := r.labelRBDStorageClasses(ctx, scr.Namespace, clusterFSIDs)
		if len(errs) > 0 {
			return ctrl.Result{}, fmt.Errorf("few failures occurred while labeling RBD StorageClasses: %v", errs)
		}
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
				// TODO: Replace it with exported type from ocs-operator
				type OnboardingTicket struct {
					ID                string `json:"id"`
					ExpirationDate    int64  `json:"expirationDate,string"`
					StorageQuotaInGiB uint   `json:"storageQuotaInGiB,omitempty"`
				}
				var ticketData OnboardingTicket
				err = json.Unmarshal(token.Data["storagecluster-peer-token"], &ticketData)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to unmarshal onboarding ticket message. %w", err)
				}
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
			err = createStorageClusterPeerTokenSecret(ctx, r.HubClient, r.Scheme, r.SpokeClusterName, "openshift-storage", mirrorPeer, scr) //TODO: get odfOperatorNamespace from addon flags
			if err != nil {
				logger.Error("Failed to create StorageCluster peer token on the hub.", "error", err)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) fetchClusterFSIDs(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) error {
	for _, pr := range mp.Spec.Items {
		clusterType, err := utils.GetClusterType(pr.StorageClusterRef.Name, pr.StorageClusterRef.Namespace, r.SpokeClient)
		if err != nil {
			r.Logger.Error("Failed to get cluster type", "clusterName", pr.StorageClusterRef.Name, "namespace", pr.StorageClusterRef.Namespace, "error", err)
			return err
		}
		secretName := r.getSecretNameByType(clusterType, pr)

		r.Logger.Info("Checking secret", "secretName", secretName, "mode", clusterType, "clusterName", pr.StorageClusterRef.Name, "namespace", pr.StorageClusterRef.Namespace)
		secret, err := utils.FetchSecretWithName(ctx, r.SpokeClient, types.NamespacedName{Name: secretName, Namespace: pr.StorageClusterRef.Namespace})
		if err != nil {
			r.Logger.Error("Failed to fetch secret", "secretName", secretName, "namespace", pr.StorageClusterRef.Namespace, "error", err)
			return err
		}

		fsid, err := r.getFsidFromSecretByType(clusterType, secret)
		if err != nil {
			r.Logger.Error("Failed to extract FSID from secret", "secretName", secretName, "namespace", pr.StorageClusterRef.Namespace, "error", err)
			return err
		}

		clusterFSIDs[pr.ClusterName] = fsid
		r.Logger.Info("FSID fetched for cluster", "clusterName", pr.ClusterName, "FSID", fsid)
	}
	return nil
}

func (r *MirrorPeerReconciler) getFsidFromSecretByType(clusterType utils.ClusterType, secret *corev1.Secret) (string, error) {
	var fsid string
	if clusterType == utils.CONVERGED {
		token, err := utils.UnmarshalRookSecret(secret)
		if err != nil {
			r.Logger.Error("Failed to unmarshal converged mode peer secret", "peerSecret", secret.Name, "error", err)
			return "", err
		}
		fsid = token.FSID
		r.Logger.Info("FSID retrieved for converged mode", "FSID", fsid, "secret", secret.Name)
	} else if clusterType == utils.EXTERNAL {
		token, err := utils.UnmarshalRookSecretExternal(secret)
		if err != nil {
			r.Logger.Error("Failed to unmarshal external mode peer secret", "peerSecret", secret.Name, "error", err)
			return "", err
		}
		fsid = token.FSID
		r.Logger.Info("FSID retrieved for external mode", "FSID", fsid, "secret", secret.Name)
	}
	return fsid, nil
}

func (r *MirrorPeerReconciler) getSecretNameByType(clusterType utils.ClusterType, pr multiclusterv1alpha1.PeerRef) string {
	var secretName string
	if clusterType == utils.CONVERGED {
		if pr.ClusterName == r.SpokeClusterName {
			secretName = fmt.Sprintf("cluster-peer-token-%s-cephcluster", pr.StorageClusterRef.Name)
		} else {
			secretName = utils.GetSecretNameByPeerRef(pr)
		}
	} else if clusterType == utils.EXTERNAL {
		secretName = DefaultExternalSecretName
	}
	return secretName
}

func (r *MirrorPeerReconciler) labelRBDStorageClasses(ctx context.Context, storageClusterNamespace string, clusterFSIDs map[string]string) []error {
	r.Logger.Info("Fetching cluster FSIDs", "clusterFSIDs", clusterFSIDs)
	// Get all StorageClasses in storageClusterNamespace
	scs := &storagev1.StorageClassList{}
	err := r.SpokeClient.List(ctx, scs)
	var errs []error
	if err != nil {
		errs = append(errs, err)
		r.Logger.Error("Failed to list StorageClasses", "namespace", storageClusterNamespace, "error", err)
		return errs
	}
	r.Logger.Info("Found StorageClasses", "count", len(scs.Items), "namespace", storageClusterNamespace)

	key := r.SpokeClusterName
	for _, sc := range scs.Items {
		if fsid, ok := clusterFSIDs[key]; !ok {
			errMsg := fmt.Errorf("no FSID found for key: %s, unable to update StorageClass", key)
			errs = append(errs, errMsg)
			r.Logger.Error("Missing FSID for StorageClass update", "key", key, "StorageClass", sc.Name)
			continue
		} else {
			if sc.Provisioner == fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespace) || sc.Provisioner == fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespace) {
				r.Logger.Info("Updating StorageClass with FSID", "StorageClass", sc.Name, "FSID", fsid)
				if sc.Labels == nil {
					sc.Labels = make(map[string]string)
				}
				sc.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] = fsid
				if err = r.SpokeClient.Update(ctx, &sc); err != nil {
					errs = append(errs, err)
					r.Logger.Error("Failed to update StorageClass with FSID", "StorageClass", sc.Name, "FSID", fsid, "error", err)
				}
			}
		}
	}

	return errs
}

func (r *MirrorPeerReconciler) createS3(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string) error {
	noobaaOBC, err := r.getS3bucket(ctx, mirrorPeer, scNamespace)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("ODR ObjectBucketClaim not found, creating new one", "MirrorPeer", mirrorPeer.Name, "namespace", scNamespace)
			err = r.SpokeClient.Create(ctx, noobaaOBC)
			if err != nil {
				r.Logger.Error("Failed to create ODR ObjectBucketClaim", "error", err, "MirrorPeer", mirrorPeer.Name, "namespace", scNamespace)
				return err
			}
		} else {
			r.Logger.Error("Failed to retrieve ODR ObjectBucketClaim", "error", err, "MirrorPeer", mirrorPeer.Name, "namespace", scNamespace)
			return err
		}
	} else {
		r.Logger.Info("ODR ObjectBucketClaim already exists, no action needed", "MirrorPeer", mirrorPeer.Name, "namespace", scNamespace)
	}
	return nil
}

func (r *MirrorPeerReconciler) getS3bucket(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string) (*obv1alpha1.ObjectBucketClaim, error) {
	var peerAccumulator string
	for _, peer := range mirrorPeer.Spec.Items {
		peerAccumulator += peer.ClusterName
	}
	checksum := sha1.Sum([]byte(peerAccumulator))

	bucketGenerateName := utils.BucketGenerateName
	// truncate to bucketGenerateName + "-" + first 12 (out of 20) byte representations of sha1 checksum
	bucket := fmt.Sprintf("%s-%s", bucketGenerateName, hex.EncodeToString(checksum[:]))[0 : len(bucketGenerateName)+1+12]
	namespace := utils.GetEnv("ODR_NAMESPACE", scNamespace)

	noobaaOBC := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket,
			Namespace: namespace,
		},
		Spec: obv1alpha1.ObjectBucketClaimSpec{
			BucketName:       bucket,
			StorageClassName: namespace + ".noobaa.io",
		},
	}

	err := r.SpokeClient.Get(ctx, types.NamespacedName{Name: bucket, Namespace: namespace}, noobaaOBC)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("ObjectBucketClaim not found, will be created", "bucket", bucket, "namespace", namespace)
		} else {
			r.Logger.Error("Failed to get ObjectBucketClaim", "error", err, "bucket", bucket, "namespace", namespace)
		}
	} else {
		r.Logger.Info("ObjectBucketClaim retrieved successfully", "bucket", bucket, "namespace", namespace)
	}
	return noobaaOBC, err
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
		oppPeers := getOppositePeerRefs(mp, r.SpokeClusterName)
		if hasRequiredSecret(sc.Spec.Mirroring.PeerSecretNames, oppPeers) {
			sc.Spec.Mirroring.Enabled = true
			r.Logger.Info("Mirroring enabled on StorageCluster", "storageClusterName", storageClusterName)
		} else {
			return fmt.Errorf("StorageCluster %q does not have required PeerSecrets", storageClusterName)
		}
	} else {
		sc.Spec.Mirroring.Enabled = false
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

	r.Logger.Info("Setting up controller with manager")
	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{}, mirrorPeerSpokeClusterPredicate)
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
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
	noobaaOBC, err := r.getS3bucket(ctx, mirrorPeer, scNamespace)
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

func (r *MirrorPeerReconciler) labelCephClusters(ctx context.Context, scr *multiclusterv1alpha1.StorageClusterRef, clusterFSIDs map[string]string) error {
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

	replicationId, err := utils.CreateUniqueReplicationId(clusterFSIDs)
	if err != nil {
		r.Logger.Error("Failed to create a unique replication ID", "error", err)
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
