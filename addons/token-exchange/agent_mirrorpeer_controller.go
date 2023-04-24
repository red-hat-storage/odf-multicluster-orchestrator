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
	"fmt"
	"sort"
	"strconv"
	"time"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	submarinerv1alpha1 "github.com/submariner-io/submariner-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
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
}

const (
	RookCSIEnableKey          = "CSI_ENABLE_OMAP_GENERATOR"
	RookConfigMapName         = "rook-ceph-operator-config"
	RBDProvisionerTemplate    = "%s.rbd.csi.ceph.com"
	RamenLabelTemplate        = "ramendr.openshift.io/%s"
	StorageIDKey              = "storageid"
	CephFSProvisionerTemplate = "%s.cephfs.csi.ceph.com"
	SpokeMirrorPeerFinalizer  = "spoke.multicluster.odf.openshift.io"
)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("Running MirrorPeer reconciler on spoke cluster")
	// Fetch MirrorPeer for given Request
	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := r.HubClient.Get(ctx, req.NamespacedName, &mirrorPeer)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		klog.Error(err, "Failed to get MirrorPeer")
		return ctrl.Result{}, err
	}

	scr, err := utils.GetCurrentStorageClusterRef(&mirrorPeer, r.SpokeClusterName)
	if err != nil {
		klog.Error(err, "Failed to get current storage cluster ref")
		return ctrl.Result{}, err
	}

	agentFinalizer := r.SpokeClusterName + "." + SpokeMirrorPeerFinalizer

	if len(agentFinalizer) > 63 {
		agentFinalizer = fmt.Sprintf("%s.%s", r.SpokeClusterName[0:10], SpokeMirrorPeerFinalizer)
	}

	if mirrorPeer.GetDeletionTimestamp().IsZero() {
		if !utils.ContainsString(mirrorPeer.GetFinalizers(), agentFinalizer) {
			klog.Infof("Finalizer not found on MirrorPeer. Adding Finalizer %q", agentFinalizer)
			mirrorPeer.Finalizers = append(mirrorPeer.Finalizers, agentFinalizer)
			if err := r.HubClient.Update(ctx, &mirrorPeer); err != nil {
				klog.Errorf("Failed to add finalizer to MirrorPeer %q", mirrorPeer.Name)
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
				klog.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
				return ctrl.Result{}, nil
			}
			klog.Error(err, "Failed to get MirrorPeer")
			return ctrl.Result{}, err
		}
		mirrorPeer.Finalizers = utils.RemoveString(mirrorPeer.Finalizers, agentFinalizer)

		if err := r.HubClient.Update(ctx, &mirrorPeer); err != nil {
			klog.Error("failed to remove finalizer from MirrorPeer ", err)
			return ctrl.Result{}, err
		}
		klog.Info("MirrorPeer deleted, skipping reconcilation")
		return ctrl.Result{}, nil
	}

	klog.Infof("creating s3 buckets")
	err = r.createS3(ctx, req, mirrorPeer, scr.Namespace)
	if err != nil {
		klog.Error(err, "Failed to create ODR S3 resources")
		return ctrl.Result{}, err
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		clusterFSIDs := make(map[string]string)
		klog.Infof("Fetching clusterFSIDs")
		err = r.fetchClusterFSIDs(ctx, &mirrorPeer, clusterFSIDs)
		if err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("an unknown error occured while fetching the cluster fsids, retrying again: %v", err)
		}

		klog.Infof("enabling async mode dependencies")
		err = r.labelCephClusters(ctx, scr, clusterFSIDs)
		if err != nil {
			klog.Errorf("failed to label cephcluster. err=%v", err)
			return ctrl.Result{}, err
		}
		err = r.enableCSIAddons(ctx, scr.Namespace)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to start CSI Addons for rook: %v", err)
		}

		err = r.enableMirroring(ctx, scr.Name, scr.Namespace, &mirrorPeer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to enable mirroring the storagecluster %q in namespace %q in managed cluster. Error %v", scr.Name, scr.Namespace, err)
		}

		klog.Infof("labeling rbd storageclasses")
		errs := r.labelRBDStorageClasses(ctx, scr.Namespace, clusterFSIDs)
		if len(errs) > 0 {
			return ctrl.Result{}, fmt.Errorf("few failures occured while labeling RBD StorageClasses: %v", errs)
		}

		// Trying this at last to allow bootstrapping to be completed
		if mirrorPeer.Spec.OverlappingCIDR {
			klog.Infof("enabling multiclusterservice", "MirrorPeer", mirrorPeer.GetName(), "Peers", mirrorPeer.Spec.Items)
			err := r.enableMulticlusterService(ctx, scr.Name, scr.Namespace, &mirrorPeer)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to enable multiclusterservice for storagecluster %q in namespace %q: %v", scr.Name, scr.Namespace, err)
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) fetchClusterFSIDs(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) error {
	for _, pr := range mp.Spec.Items {
		clusterType, err := utils.GetClusterType(pr.StorageClusterRef.Name, pr.StorageClusterRef.Namespace, r.SpokeClient)
		if err != nil {
			return err
		}
		secretName := r.getSecretNameByType(clusterType, pr)

		klog.Info("Checking secret: ", secretName, " Mode:", clusterType)
		secret, err := utils.FetchSecretWithName(ctx, r.SpokeClient, types.NamespacedName{Name: secretName, Namespace: pr.StorageClusterRef.Namespace})
		if err != nil {
			return err
		}

		fsid, err := r.getFsidFromSecretByType(clusterType, secret)
		if err != nil {
			return err
		}

		clusterFSIDs[pr.ClusterName] = fsid
	}
	return nil
}

func (r *MirrorPeerReconciler) getFsidFromSecretByType(clusterType utils.ClusterType, secret *corev1.Secret) (string, error) {
	var fsid string
	if clusterType == utils.CONVERGED {
		token, err := utils.UnmarshalRookSecret(secret)
		if err != nil {
			klog.Error(err, "Error while unmarshalling converged mode peer secret; ", "peerSecret ", secret.Name)
			return "", err
		}
		fsid = token.FSID
	} else if clusterType == utils.EXTERNAL {
		token, err := utils.UnmarshalRookSecretExternal(secret)
		if err != nil {
			klog.Error(err, "Error while unmarshalling external mode peer secret; ", "peerSecret ", secret.Name)
			return "", err
		}
		fsid = token.FSID
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
	klog.Info(clusterFSIDs)
	// Get all StorageClasses in storageClusterNamespace
	scs := &storagev1.StorageClassList{}
	err := r.SpokeClient.List(ctx, scs)
	var errs []error
	if err != nil {
		errs = append(errs, err)
		return errs
	}
	klog.Infof("Found %d StorageClasses", len(scs.Items))
	key := r.SpokeClusterName
	for _, sc := range scs.Items {
		if _, ok := clusterFSIDs[key]; !ok {
			errs = append(errs, fmt.Errorf("no value found for key: %s, unable to update StorageClass for %s", key, key))
			continue
		}
		if sc.Provisioner == fmt.Sprintf(RBDProvisionerTemplate, storageClusterNamespace) || sc.Provisioner == fmt.Sprintf(CephFSProvisionerTemplate, storageClusterNamespace) {
			klog.Infof("Updating StorageClass %q with label storageid %q", sc.Name, clusterFSIDs[key])
			sc.Labels = make(map[string]string)
			sc.Labels[fmt.Sprintf(RamenLabelTemplate, StorageIDKey)] = clusterFSIDs[key]
			err = r.SpokeClient.Update(ctx, &sc)
			if err != nil {
				klog.Error(err, "Failed to update StorageClass: %s", sc.Name)
				errs = append(errs, err)
			}
		}
	}

	return errs
}

func (r *MirrorPeerReconciler) createS3(ctx context.Context, req ctrl.Request, mirrorPeer multiclusterv1alpha1.MirrorPeer, scNamespace string) error {
	noobaaOBC, err := r.getS3bucket(ctx, mirrorPeer, scNamespace)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Could not find ODR ObjectBucketClaim, creating")
			err = r.SpokeClient.Create(ctx, noobaaOBC)
			if err != nil {
				klog.Error(err, "Failed to create ODR ObjectBucketClaim")
				return err
			}
		} else {
			klog.Error(err, "Failed to get ODR ObjectBucketClaim")
			return err
		}
	}
	return err
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
	return noobaaOBC, err
}

// enableMulticlusterService sets the multiclusterservice flag on StorageCluster if submariner globalnet is enabled
func (r *MirrorPeerReconciler) enableMulticlusterService(ctx context.Context, storageClusterName string, namespace string, mp *multiclusterv1alpha1.MirrorPeer) error {
	klog.Infof("Enabling MCS for StorageCluster %q in %q namespace.", storageClusterName, namespace)
	var sc ocsv1.StorageCluster
	err := r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      storageClusterName,
		Namespace: namespace,
	}, &sc)
	if err != nil {
		klog.Errorf("Error fetching StorageCluster while enabling MCS. Error: %v", err)
		return err
	}

	var submariner submarinerv1alpha1.Submariner
	err = r.SpokeClient.Get(ctx, types.NamespacedName{
		Name:      "submariner",
		Namespace: "submariner-operator"},
		&submariner)
	if err != nil {
		klog.Errorf("Error fetching Submariner config while enabling MCS. Error: %v", err)
		return err
	}

	if sc.Spec.Network == nil {
		sc.Spec.Network = &rookv1.NetworkSpec{}
		klog.Infof("StorageCluster %q in %q namespace has no network config defined. Initializing it now. New NetworkSpec: %v", storageClusterName, namespace, sc.Spec.Network)
	}

	if !sc.Spec.Network.MultiClusterService.Enabled || sc.Spec.Network.MultiClusterService.ClusterID == "" {
		sc.Spec.Network.MultiClusterService.Enabled = true
		sc.Spec.Network.MultiClusterService.ClusterID = submariner.Spec.ClusterID
		klog.Infof("StorageCluster %q in %q namespace has MCS disabled. Enabling it now. New MCS spec: %v", storageClusterName, namespace, sc.Spec.Network.MultiClusterService)
		err := r.SpokeClient.Update(ctx, &sc)
		if err != nil {
			klog.Errorf("Error updating MCS config for StorageCluster %q in %q namespace. Error: %v", storageClusterName, namespace, err)
		}
		return err
	}

	klog.Infof("StorageCluster %q in %q namespace has MCS enabled already. Current MCS spec: %v", storageClusterName, namespace, sc.Spec.Network.MultiClusterService)
	return nil
}

// enableMirroring is a wrapper function around toggleMirroring to enable mirroring in a storage cluster
func (r *MirrorPeerReconciler) enableMirroring(ctx context.Context, storageClusterName string, namespace string, mp *multiclusterv1alpha1.MirrorPeer) error {
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
			return fmt.Errorf("could not find storagecluster %q in namespace %v: %v", storageClusterName, namespace, err)
		}
		return err
	}
	if enabled {
		oppPeers := getOppositePeerRefs(mp, r.SpokeClusterName)
		if hasRequiredSecret(sc.Spec.Mirroring.PeerSecretNames, oppPeers) {
			sc.Spec.Mirroring.Enabled = true
			klog.Info("Enabled mirroring on StorageCluster ", storageClusterName)
		} else {
			klog.Error(err, "StorageCluster does not have required PeerSecrets")
			return err
		}
	} else {
		sc.Spec.Mirroring.Enabled = false
	}
	return r.SpokeClient.Update(ctx, &sc)
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
		klog.Error(err, "failed to enable CSI addons")
		return err
	} else {
		klog.Info("CSI addons enabled successfully ")
	}
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
			return fmt.Errorf("could not find rook-ceph-config-map: %v", err)
		}
		return err
	}

	if rcm.Data == nil {
		rcm.Data = make(map[string]string, 0)
	}
	rcm.Data[RookCSIEnableKey] = strconv.FormatBool(enabled)

	return r.SpokeClient.Update(ctx, &rcm)
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

	mpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{}, mirrorPeerSpokeClusterPredicate)
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}, builder.WithPredicates(mpPredicate)).
		Complete(r)
}

func (r *MirrorPeerReconciler) disableMirroring(ctx context.Context, storageClusterName string, namespace string, mp *multiclusterv1alpha1.MirrorPeer) error {
	return r.toggleMirroring(ctx, storageClusterName, namespace, false, mp)
}

func (r *MirrorPeerReconciler) disableCSIAddons(ctx context.Context, namespace string) error {
	err := r.toggleCSIAddons(ctx, namespace, false)
	if err != nil {
		klog.Error(err, "Failed to disable CSI addons")
		return err
	} else {
		klog.Info("CSI addons disabled successfully ")
	}
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
			if err := r.SpokeClient.Delete(ctx, &secret); err != nil {
				if errors.IsNotFound(err) {
					klog.Info("failed to find green secret ", secretName)
					return nil
				} else {
					klog.Error(err, "failed to delete green secret ", secretName)
					return err
				}
			}
			klog.Info("Succesfully deleted ", secretName, " in namespace ", scrNamespace)
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
			klog.Info("Could not find ODR ObjectBucketClaim, skipping deletion")
			return nil
		} else {
			klog.Error(err, "Failed to get ODR ObjectBucketClaim")
			return err
		}
	}
	err = r.SpokeClient.Delete(ctx, noobaaOBC)
	if err != nil {
		klog.Error(err, "Failed to delete ODR ObjectBucketClaim")
		return err
	}
	return err
}

func (r *MirrorPeerReconciler) deleteMirrorPeer(ctx context.Context, mirrorPeer multiclusterv1alpha1.MirrorPeer, scr *multiclusterv1alpha1.StorageClusterRef) (ctrl.Result, error) {
	klog.Infof("Mirrorpeer is being deleted %q", mirrorPeer.Name)
	peerRef, err := utils.GetPeerRefForSpokeCluster(&mirrorPeer, r.SpokeClusterName)
	if err != nil {
		klog.Errorf("Failed to get current PeerRef %q %v", mirrorPeer.Name, err)
		return ctrl.Result{}, err
	}

	peerRefUsed, err := utils.DoesAnotherMirrorPeerPointToPeerRef(ctx, r.HubClient, peerRef)
	if err != nil {
		klog.Errorf("failed to check if another peer uses peer ref %v", err)
	}
	if !peerRefUsed {
		if err := r.disableMirroring(ctx, scr.Name, scr.Namespace, &mirrorPeer); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to disable mirroring for the storagecluster %q in namespace %q. Error %v", scr.Name, scr.Namespace, err)
		}
		if err := r.disableCSIAddons(ctx, scr.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to disable CSI Addons for rook: %v", err)
		}
	}

	if err := r.deleteGreenSecret(ctx, r.SpokeClusterName, scr.Namespace, &mirrorPeer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete green secrets: %v", err)
	}

	if err := r.deleteS3(ctx, mirrorPeer, scr.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete s3 buckets")
	}
	return ctrl.Result{}, nil
}

func (r *MirrorPeerReconciler) labelCephClusters(ctx context.Context, scr *multiclusterv1alpha1.StorageClusterRef, clusterFSIDs map[string]string) error {
	klog.Info("Labelling cephclusters with replication id")
	cephClusters, err := utils.FetchAllCephClusters(ctx, r.SpokeClient)
	if err != nil {
		klog.Errorf("failed to fetch all cephclusters. err=%v", err)
		return err
	}
	if cephClusters == nil || len(cephClusters.Items) == 0 {
		klog.Info("failed to find any cephclusters to label")
		return nil
	}
	var found rookv1.CephCluster
	for _, cc := range cephClusters.Items {
		for _, ref := range cc.OwnerReferences {
			if ref.Kind != "StorageCluster" {
				continue
			}

			if ref.Name == scr.Name {
				found = cc
				break
			}
		}
	}
	if found.Labels == nil {
		found.Labels = make(map[string]string)
	}
	var fsids []string

	for _, v := range clusterFSIDs {
		fsids = append(fsids, v)
	}
	// To ensure reliability of hash generation
	sort.Strings(fsids)
	replicationId := utils.CreateUniqueReplicationId(fsids)
	if found.Labels[utils.CephClusterReplicationIdLabel] != replicationId {
		klog.Infof("adding label %s/%s to cephcluster %s", utils.CephClusterReplicationIdLabel, replicationId, found.Name)
		found.Labels[utils.CephClusterReplicationIdLabel] = replicationId
		err = r.SpokeClient.Update(ctx, &found)
		if err != nil {
			return err
		}
	} else {
		klog.Infof("cephcluster %s is already labeled with replicationId=%q", found.Name, replicationId)
	}

	return nil
}
