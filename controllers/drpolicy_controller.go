package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/apis/replication.storage/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	DefaultMirroringMode                         = "snapshot"
	MirroringModeKey                             = "mirroringMode"
	SchedulingIntervalKey                        = "schedulingInterval"
	ReplicationSecretNameKey                     = "replication.storage.openshift.io/replication-secret-name"
	ReplicationSecretNamespaceKey                = "replication.storage.openshift.io/replication-secret-namespace"
	ReplicationIDKey                             = "replicationid"
	RBDVolumeReplicationClassNameTemplate        = "rbd-volumereplicationclass-%v"
	RBDReplicationSecretName                     = "rook-csi-rbd-provisioner"
	RamenLabelTemplate                           = "ramendr.openshift.io/%s"
	RBDProvisionerTemplate                       = "%s.rbd.csi.ceph.com"
	RBDFlattenVolumeReplicationClassNameTemplate = "rbd-flatten-volumereplicationclass-%v"
	RBDFlattenVolumeReplicationClassLabelKey     = "replication.storage.openshift.io/flatten-mode"
	RBDFlattenVolumeReplicationClassLabelValue   = "force"
	RBDVolumeReplicationClassDefaultAnnotation   = "replication.storage.openshift.io/is-default-class"
)

type DRPolicyReconciler struct {
	HubClient client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *DRPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up DRPolicyReconciler with manager")

	dpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{})

	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPolicy{}, builder.WithPredicates(dpPredicate)).
		Complete(r)
}

func (r *DRPolicyReconciler) getMirrorPeerForClusterSet(ctx context.Context, clusterSet []string) (*multiclusterv1alpha1.MirrorPeer, error) {
	logger := r.Logger

	var mpList multiclusterv1alpha1.MirrorPeerList
	err := r.HubClient.List(ctx, &mpList)
	if err != nil {
		logger.Error("Could not list MirrorPeers on hub", "error", err)
		return nil, err
	}

	if len(mpList.Items) == 0 {
		logger.Info("No MirrorPeers found on hub yet")
		return nil, k8serrors.NewNotFound(schema.GroupResource{Group: multiclusterv1alpha1.GroupVersion.Group, Resource: "MirrorPeer"}, "MirrorPeerList")
	}

	for _, mp := range mpList.Items {
		if (mp.Spec.Items[0].ClusterName == clusterSet[0] && mp.Spec.Items[1].ClusterName == clusterSet[1]) ||
			(mp.Spec.Items[1].ClusterName == clusterSet[0] && mp.Spec.Items[0].ClusterName == clusterSet[1]) {
			logger.Info("Found MirrorPeer for DRPolicy", "MirrorPeerName", mp.Name)
			return &mp, nil
		}
	}

	logger.Info("Could not find any MirrorPeer for DRPolicy")
	return nil, k8serrors.NewNotFound(schema.GroupResource{Group: multiclusterv1alpha1.GroupVersion.Group, Resource: "MirrorPeer"}, fmt.Sprintf("ClusterSet-%s-%s", clusterSet[0], clusterSet[1]))
}

func (r *DRPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("Request", req.NamespacedName.String())
	logger.Info("Running DRPolicy reconciler on hub cluster")

	// Fetch DRPolicy for the given request
	var drpolicy ramenv1alpha1.DRPolicy
	err := r.HubClient.Get(ctx, req.NamespacedName, &drpolicy)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("DRPolicy not found. Ignoring since the object must have been deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to get DRPolicy", "error", err)
		return ctrl.Result{}, err
	}

	// Find MirrorPeer for clusterset for the storagecluster namespaces
	mirrorPeer, err := r.getMirrorPeerForClusterSet(ctx, drpolicy.Spec.DRClusters)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("MirrorPeer not found. Requeuing", "DRClusters", drpolicy.Spec.DRClusters)
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}
		logger.Error("Error occurred while trying to fetch MirrorPeer for given DRPolicy", "error", err)
		return ctrl.Result{}, err
	}

	if utils.IsStorageClientType(mirrorPeer.Spec.Items) {
		logger.Info("MirrorPeer contains StorageClient reference. Skipping creation of VolumeReplicationClasses", "MirrorPeer", mirrorPeer.Name)
		return ctrl.Result{}, nil
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		clusterFSIDs := make(map[string]string)
		logger.Info("Fetching cluster FSIDs")
		err = r.fetchClusterFSIDs(ctx, mirrorPeer, clusterFSIDs)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Cluster FSIDs not found, requeuing")
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
			logger.Error("An unknown error occurred while fetching the cluster FSIDs, retrying", "error", err)
			return ctrl.Result{}, fmt.Errorf("an unknown error occurred while fetching the cluster FSIDs, retrying: %v", err)
		}

		err = r.createOrUpdateManifestWorkForVRC(ctx, mirrorPeer, &drpolicy, clusterFSIDs)
		if err != nil {
			logger.Error("Failed to create VolumeReplicationClass via ManifestWork", "error", err)
			return ctrl.Result{}, fmt.Errorf("failed to create VolumeReplicationClass via ManifestWork: %v", err)
		}
	}

	logger.Info("Successfully reconciled DRPolicy")
	return ctrl.Result{}, nil
}

func (r *DRPolicyReconciler) createOrUpdateManifestWorkForVRC(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, dp *ramenv1alpha1.DRPolicy, clusterFSIDs map[string]string) error {
	logger := r.Logger.With("DRPolicy", dp.Name, "MirrorPeer", mp.Name)

	replicationId, err := utils.CreateUniqueReplicationId(clusterFSIDs)
	if err != nil {
		logger.Error("Failed to create unique replication ID", "error", err)
		return err
	}

	manifestWorkName := fmt.Sprintf("vrc-%v", utils.FnvHash(dp.Name)) // Two ManifestWork per DRPolicy

	for _, pr := range mp.Spec.Items {
		found := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestWorkName,
				Namespace: pr.ClusterName,
			},
		}

		err := r.HubClient.Get(ctx, types.NamespacedName{Name: found.Name, Namespace: pr.ClusterName}, found)

		switch {
		case err == nil:
			logger.Info("ManifestWork already exists, updating", "ManifestWorkName", manifestWorkName)
		case !k8serrors.IsNotFound(err):
			logger.Error("Failed to get ManifestWork", "ManifestWorkName", manifestWorkName, "error", err)
			return err
		}

		interval := dp.Spec.SchedulingInterval
		params := make(map[string]string)
		params[MirroringModeKey] = DefaultMirroringMode
		params[SchedulingIntervalKey] = interval
		params[ReplicationSecretNameKey] = RBDReplicationSecretName
		params[ReplicationSecretNamespaceKey] = pr.StorageClusterRef.Namespace
		vrcName := fmt.Sprintf(RBDVolumeReplicationClassNameTemplate, utils.FnvHash(interval))
		labels := make(map[string]string)
		labels[fmt.Sprintf(RamenLabelTemplate, ReplicationIDKey)] = replicationId
		labels[fmt.Sprintf(RamenLabelTemplate, "maintenancemodes")] = "Failover"
		vrc := replicationv1alpha1.VolumeReplicationClass{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VolumeReplicationClass",
				APIVersion: "replication.storage.openshift.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   vrcName,
				Labels: labels,
				Annotations: map[string]string{
					RBDVolumeReplicationClassDefaultAnnotation: "true",
				},
			},
			Spec: replicationv1alpha1.VolumeReplicationClassSpec{
				Parameters:  params,
				Provisioner: fmt.Sprintf(RBDProvisionerTemplate, pr.StorageClusterRef.Namespace),
			},
		}

		objJson, err := json.Marshal(vrc)
		if err != nil {
			logger.Error("Failed to marshal VolumeReplicationClass to JSON", "VolumeReplicationClass", vrcName, "error", err)
			return fmt.Errorf("failed to marshal %v to JSON, error %w", vrc, err)
		}

		manifestList := []workv1.Manifest{
			{
				RawExtension: runtime.RawExtension{
					Raw: objJson,
				},
			},
		}

		if dp.Spec.ReplicationClassSelector.MatchLabels[RBDFlattenVolumeReplicationClassLabelKey] == RBDFlattenVolumeReplicationClassLabelValue {
			vrcFlatten := vrc.DeepCopy()
			vrcFlatten.Name = fmt.Sprintf(RBDFlattenVolumeReplicationClassNameTemplate, utils.FnvHash(interval))
			vrcFlatten.Labels[RBDFlattenVolumeReplicationClassLabelKey] = RBDFlattenVolumeReplicationClassLabelValue
			vrcFlatten.Annotations = map[string]string{}
			vrcFlatten.Spec.Parameters["flattenMode"] = "force"
			vrcFlattenJson, err := json.Marshal(vrcFlatten)
			if err != nil {
				return fmt.Errorf("failed to marshal %v to JSON, error %w", vrcFlatten, err)
			}
			manifestList = append(manifestList, workv1.Manifest{
				RawExtension: runtime.RawExtension{
					Raw: vrcFlattenJson,
				},
			})
		}

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestWorkName,
				Namespace: pr.ClusterName,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       dp.Kind,
						Name:       dp.Name,
						UID:        dp.UID,
						APIVersion: dp.APIVersion,
					},
				},
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.HubClient, &mw, func() error {
			mw.Spec = workv1.ManifestWorkSpec{
				Workload: workv1.ManifestsTemplate{
					Manifests: manifestList,
				},
			}
			return nil
		})

		if err != nil {
			logger.Error("Failed to create/update ManifestWork", "ManifestWorkName", manifestWorkName, "error", err)
			return err
		}

		logger.Info("ManifestWork created/updated successfully", "ManifestWorkName", manifestWorkName, "VolumeReplicationClassName", vrcName)
	}

	return nil
}

func (r *DRPolicyReconciler) fetchClusterFSIDs(ctx context.Context, peer *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) error {
	for _, pr := range peer.Spec.Items {
		logger := r.Logger.With("MirrorPeer", peer.Name, "ClusterName", pr.ClusterName)
		rookSecretName := utils.GetSecretNameByPeerRef(pr)
		logger.Info("Fetching rook secret", "SecretName", rookSecretName)

		hs, err := utils.FetchSecretWithName(ctx, r.HubClient, types.NamespacedName{Name: rookSecretName, Namespace: pr.ClusterName})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Secret not found, will attempt to fetch again after a delay", "SecretName", rookSecretName)
				return err
			}
			logger.Error("Failed to fetch rook secret", "SecretName", rookSecretName, "error", err)
			return err
		}

		logger.Info("Unmarshalling rook secret", "SecretName", rookSecretName)
		rt, err := utils.UnmarshalHubSecret(hs)
		if err != nil {
			logger.Error("Failed to unmarshal rook secret", "SecretName", rookSecretName, "error", err)
			return err
		}

		clusterFSIDs[pr.ClusterName] = rt.FSID
		logger.Info("Successfully fetched FSID for cluster", "FSID", rt.FSID)
	}

	r.Logger.Info("Successfully fetched all cluster FSIDs", "MirrorPeer", peer.Name)
	return nil
}
