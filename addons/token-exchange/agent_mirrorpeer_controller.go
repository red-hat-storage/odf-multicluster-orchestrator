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

	replicationv1alpha1 "github.com/csi-addons/volume-replication-operator/api/v1alpha1"
	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
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
	RookCSIEnableKey                      = "CSI_ENABLE_OMAP_GENERATOR"
	RookVolumeRepKey                      = "CSI_ENABLE_VOLUME_REPLICATION"
	MirroringModeKey                      = "mirroringMode"
	SchedulingIntervalKey                 = "schedulingInterval"
	ReplicationSecretNameKey              = "replication.storage.openshift.io/replication-secret-name"
	ReplicationSecretNamespaceKey         = "replication.storage.openshift.io/replication-secret-namespace"
	RBDProvisionerTemplate                = "%s.rbd.csi.ceph.com"
	RookConfigMapName                     = "rook-ceph-operator-config"
	RBDVolumeReplicationClassNameTemplate = "rbd-volumereplicationclass-%v"
	RBDReplicationSecretName              = "rook-csi-rbd-provisioner"
	DefaultMirroringMode                  = "snapshot"
	RamenLabelTemplate                    = "ramendr.openshift.io/%s"
	StorageIDKey                          = "storageid"
	ReplicationIDKey                      = "replicationid"
	CephFSProvisionerTemplate             = "%s.cephfs.csi.ceph.com"
	SpokeMirrorPeerFinalizer              = "spoke.mirrorpeer.multicluster.odf.openshift.io"
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

	clusterFSIDs := make(map[string]string)
	klog.Infof("Fetching clusterFSIDs")
	err = r.fetchClusterFSIDs(ctx, &mirrorPeer, clusterFSIDs)
	if err != nil {
		return ctrl.Result{Requeue: true}, fmt.Errorf("failed to get all cluster FSIDs, retrying again: %v", err)
	}

	klog.Info(clusterFSIDs)
	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		klog.Infof("enabling async mode dependencies")
		err = r.enableCSIAddons(ctx, scr.Namespace)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to start CSI Addons for rook: %v", err)
		}

		err = r.enableMirroring(ctx, scr.Name, scr.Namespace, &mirrorPeer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to enable mirroring the storagecluster %q in namespace %q in managed cluster. Error %v", scr.Name, scr.Namespace, err)
		}

		errs := r.createVolumeReplicationClass(ctx, &mirrorPeer, clusterFSIDs)
		if len(errs) > 0 {
			return ctrl.Result{}, fmt.Errorf("few failures occured while creating VolumeReplicationClasses: %v", errs)
		}
	}

	klog.Infof("labeling rbd storageclasses")
	errs := r.labelRBDStorageClasses(ctx, mirrorPeer, scr.Namespace, clusterFSIDs)
	if len(errs) > 0 {
		return ctrl.Result{}, fmt.Errorf("few failures occured while labeling RBD StorageClasses: %v", errs)
	}

	klog.Infof("creating s3 buckets")
	err = r.createS3(ctx, req, mirrorPeer, scr.Namespace)
	if err != nil {
		klog.Error(err, "Failed to create ODR S3 resources")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

	rcm.Data[RookCSIEnableKey] = strconv.FormatBool(enabled)
	rcm.Data[RookVolumeRepKey] = strconv.FormatBool(enabled)

	return r.SpokeClient.Update(ctx, &rcm)
}

func (r *MirrorPeerReconciler) fetchClusterFSIDs(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) error {
	for _, pr := range mp.Spec.Items {
		var secretName string
		if pr.ClusterName == r.SpokeClusterName {
			secretName = fmt.Sprintf("cluster-peer-token-%s-cephcluster", pr.StorageClusterRef.Name)
		} else {
			secretName = utils.GetSecretNameByPeerRef(pr)
		}
		klog.Info("Checking secret ", secretName)
		secret, err := utils.FetchSecretWithName(ctx, r.SpokeClient, types.NamespacedName{Name: secretName, Namespace: pr.StorageClusterRef.Namespace})
		if err != nil {
			klog.Error(err, "Error while fetching peer secret", "peerSecret ", secretName)
			return err
		}

		token, err := utils.UnmarshalRookSecret(secret)
		if err != nil {
			klog.Error(err, "Error while unmarshalling peer secret", "peerSecret ", secretName)
			return err
		}

		clusterFSIDs[pr.ClusterName] = token.FSID
	}
	return nil
}

func (r *MirrorPeerReconciler) createVolumeReplicationClass(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) []error {
	scr, err := utils.GetCurrentStorageClusterRef(mp, r.SpokeClusterName)
	var errs []error
	if err != nil {
		klog.Error(err, "Failed to get current storage cluster ref")
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs
	}
	var fsids []string
	for _, v := range clusterFSIDs {
		fsids = append(fsids, v)
	}

	// To ensure reliability of hash generation
	sort.Strings(fsids)

	replicationId := utils.CreateUniqueReplicationId(fsids)

	for _, interval := range mp.Spec.SchedulingIntervals {
		params := make(map[string]string)
		params[MirroringModeKey] = DefaultMirroringMode
		params[SchedulingIntervalKey] = interval
		params[ReplicationSecretNameKey] = RBDReplicationSecretName
		params[ReplicationSecretNamespaceKey] = scr.Namespace
		vrcName := fmt.Sprintf(RBDVolumeReplicationClassNameTemplate, utils.FnvHash(interval))
		klog.Infof("Creating volume replication class %q with label replicationId %q", vrcName, replicationId)
		found := &replicationv1alpha1.VolumeReplicationClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: vrcName,
			},
		}
		err = r.SpokeClient.Get(ctx, types.NamespacedName{
			Name: found.Name,
		}, found)

		switch {
		case err == nil:
			klog.Infof("VolumeReplicationClass already exists: %s", vrcName)
			continue
		case !errors.IsNotFound(err):
			klog.Error(err, "Failed to get VolumeReplicationClass: %s", vrcName)
			errs = append(errs, err)
			continue
		}
		labels := make(map[string]string)
		labels[fmt.Sprintf(RamenLabelTemplate, ReplicationIDKey)] = replicationId
		vrc := replicationv1alpha1.VolumeReplicationClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:   vrcName,
				Labels: labels,
			},
			Spec: replicationv1alpha1.VolumeReplicationClassSpec{
				Parameters:  params,
				Provisioner: fmt.Sprintf(RBDProvisionerTemplate, scr.Namespace),
			},
		}
		err = r.SpokeClient.Create(ctx, &vrc)
		if err != nil {
			klog.Error(err, "Failed to create VolumeReplicationClass: %s", vrcName)
			errs = append(errs, err)
			continue
		}
		klog.Infof("VolumeReplicationClass created: %s", vrcName)
	}
	return errs
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

func (r *MirrorPeerReconciler) labelRBDStorageClasses(ctx context.Context, mp multiclusterv1alpha1.MirrorPeer, storageClusterNamespace string, clusterFSIDs map[string]string) []error {
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

// deleteVolumeReplicationClass deletes the volume replication class present in the storage cluster resource,
// we check if another mirrorpeer is using the same scheduling interval while pointing to the same peer ref
func (r *MirrorPeerReconciler) deleteVolumeReplicationClass(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, peerRef *multiclusterv1alpha1.PeerRef) error {
	for _, interval := range mp.Spec.SchedulingIntervals {
		intervalUsed, err := utils.DoesAnotherMirrorPeerContainTheSchedulingInterval(ctx, r.HubClient, mp, interval, peerRef)
		if err != nil {
			return err
		}
		if intervalUsed {
			return nil
		}

		vrcName := fmt.Sprintf(RBDVolumeReplicationClassNameTemplate, utils.FnvHash(interval))

		vrc := &replicationv1alpha1.VolumeReplicationClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: vrcName,
			},
		}
		err = r.SpokeClient.Delete(ctx, vrc)
		if errors.IsNotFound(err) {
			klog.Error("Cannot find volume replication class for interval", interval, " ", err)
			return nil
		}

		klog.Info("Succesfully deleted interval ", interval)

	}
	return nil
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

	if err := r.deleteVolumeReplicationClass(ctx, &mirrorPeer, peerRef); err != nil {
		return ctrl.Result{}, fmt.Errorf("few failures occured while deleting VolumeReplicationClasses: %v", err)
	}
	if err := r.deleteS3(ctx, mirrorPeer, scr.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete s3 buckets")
	}
	return ctrl.Result{}, nil
}
