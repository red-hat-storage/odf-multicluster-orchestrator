package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	StorageIDKey                                 = "storageid"
)

type DRPolicyReconciler struct {
	HubClient client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger

	testEnvFile      string
	currentNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *DRPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up DRPolicyReconciler with manager")

	dpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{})

	tokenToDRPolicyMapFunc := func(ctx context.Context, obj client.Object) []ctrl.Request {
		r.Logger.Debug("Mapping secret to DRPolicy", "SecretName", obj.GetName(), "SecretNamespace", obj.GetNamespace())
		reqs := []ctrl.Request{}
		var mirrorPeerList multiclusterv1alpha1.MirrorPeerList
		err := r.HubClient.List(ctx, &mirrorPeerList)
		if err != nil {
			r.Logger.Error("Unable to reconcile DRPolicy based on token changes. Failed to list MirrorPeers.", "error", err)
			return reqs
		}
		r.Logger.Debug("Fetched MirrorPeers", "count", len(mirrorPeerList.Items))
		for _, mirrorpeer := range mirrorPeerList.Items {
			for _, peerRef := range mirrorpeer.Spec.Items {
				name := utils.GetSecretNameByPeerRef(peerRef)
				r.Logger.Debug("GetSecretNameByPeerRef()", "MirrorPeer", mirrorpeer.GetName(), "peerRef", peerRef, "SecretName", name)
				if name == obj.GetName() {
					r.Logger.Debug("GetSecretNameByPeerRef() == obj.GetName()", "MirrorPeer", mirrorpeer.GetName(), "peerRef", peerRef, "SecretName", name, "ObjectName", obj.GetName())
					var drpolicyList ramenv1alpha1.DRPolicyList
					err := r.HubClient.List(ctx, &drpolicyList)
					if err != nil {
						r.Logger.Error("Unable to reconcile DRPolicy based on token changes. Failed to list DRPolicies", "error", err)
						return reqs
					}
					r.Logger.Debug("Fetched DRPolicies", "count", len(drpolicyList.Items))
					for _, drpolicy := range drpolicyList.Items {
						for i := range drpolicy.Spec.DRClusters {
							if drpolicy.Spec.DRClusters[i] == peerRef.ClusterName {
								r.Logger.Debug("drpolicy.Spec.DRClusters == peerRef.ClusterName", "DRPolicy", drpolicy.GetName(), "DRCluster", drpolicy.Spec.DRClusters[i], "peerRef.ClusterName", peerRef.ClusterName)
								reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: drpolicy.Name}})
							}
						}
					}
					break
				}
				r.Logger.Debug("GetSecretNameByPeerRef() != obj.GetName()", "MirrorPeer", mirrorpeer.GetName(), "peerRef", peerRef, "SecretName", name, "ObjectName", obj.GetName())
			}
		}
		r.Logger.Info("DRPolicy reconcile requests generated based on token change.", "RequestCount", len(reqs), "Requests", reqs, "TokenName", obj.GetName())
		return reqs
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPolicy{}, builder.WithPredicates(dpPredicate)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(tokenToDRPolicyMapFunc), builder.WithPredicates(utils.SourcePredicate)).
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

	// Check if the MirrorPeer contains StorageClient reference
	hasStorageClientRef, err := utils.IsStorageClientType(ctx, r.HubClient, *mirrorPeer, false)
	if err != nil {
		logger.Error("Failed to determine if MirrorPeer contains StorageClient reference", "error", err)
		return ctrl.Result{}, err
	}

	if hasStorageClientRef {
		logger.Info("MirrorPeer contains StorageClient reference. Skipping creation of VolumeReplicationClasses", "MirrorPeer", mirrorPeer.Name)
		return ctrl.Result{}, nil
	}

	logger.Info("Successfully reconciled DRPolicy")
	return ctrl.Result{}, nil
}
