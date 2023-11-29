package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/apis/replication.storage/v1alpha1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	DefaultMirroringMode                  = "snapshot"
	MirroringModeKey                      = "mirroringMode"
	SchedulingIntervalKey                 = "schedulingInterval"
	ReplicationSecretNameKey              = "replication.storage.openshift.io/replication-secret-name"
	ReplicationSecretNamespaceKey         = "replication.storage.openshift.io/replication-secret-namespace"
	ReplicationIDKey                      = "replicationid"
	RBDVolumeReplicationClassNameTemplate = "rbd-volumereplicationclass-%v"
	RBDReplicationSecretName              = "rook-csi-rbd-provisioner"
	RamenLabelTemplate                    = "ramendr.openshift.io/%s"
	RBDProvisionerTemplate                = "%s.rbd.csi.ceph.com"
)

type DRPolicyReconciler struct {
	HubClient client.Client
	Scheme    *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DRPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	dpPredicate := utils.ComposePredicates(predicate.GenerationChangedPredicate{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPolicy{}, builder.WithPredicates(dpPredicate)).
		Complete(r)
}

func (r *DRPolicyReconciler) processManagedClusterAddon(ctx context.Context, drp *ramenv1alpha1.DRPolicy, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	logger := log.FromContext(ctx)
	// Create or Update ManagedClusterAddon
	for i, clusterName := range drp.Spec.DRClusters {
		var managedClusterAddOn addonapiv1alpha1.ManagedClusterAddOn
		if err := r.HubClient.Get(ctx, types.NamespacedName{
			Name:      setup.MaintainAgentName,
			Namespace: clusterName,
		}, &managedClusterAddOn); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Info("Cannot find managedClusterAddon, creating")
				annotations := make(map[string]string)
				annotations[utils.DRModeAnnotationKey] = string(mirrorPeer.Spec.Type)

				managedClusterAddOn = addonapiv1alpha1.ManagedClusterAddOn{
					ObjectMeta: metav1.ObjectMeta{
						Name:        setup.MaintainAgentName,
						Namespace:   clusterName,
						Annotations: annotations,
					},
				}
			}
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.HubClient, &managedClusterAddOn, func() error {
			managedClusterAddOn.Spec.InstallNamespace = mirrorPeer.Spec.Items[i].StorageClusterRef.Namespace
			return controllerutil.SetOwnerReference(drp, &managedClusterAddOn, r.Scheme)
		})
		if err != nil {
			logger.Error(err, "Failed to reconcile ManagedClusterAddOn.", "ManagedClusterAddOn", klog.KRef(managedClusterAddOn.Namespace, managedClusterAddOn.Name))
			return err
		}
	}
	return nil
}

func (r *DRPolicyReconciler) getMirrorPeerForClusterSet(ctx context.Context, clusterSet []string) (*multiclusterv1alpha1.MirrorPeer, error) {
	var mpList multiclusterv1alpha1.MirrorPeerList
	err := r.HubClient.List(ctx, &mpList)
	if err != nil {
		klog.Error("could not list mirrorpeers on hub")
		return nil, err
	}

	if len(mpList.Items) == 0 {
		klog.Info("no mirrorpeers found on hub yet")
		return nil, k8serrors.NewNotFound(schema.GroupResource{Group: multiclusterv1alpha1.GroupVersion.Group, Resource: "MirrorPeer"}, "MirrorPeerList")
	}
	for _, mp := range mpList.Items {
		if (mp.Spec.Items[0].ClusterName == clusterSet[0] && mp.Spec.Items[1].ClusterName == clusterSet[1]) ||
			(mp.Spec.Items[1].ClusterName == clusterSet[0] && mp.Spec.Items[0].ClusterName == clusterSet[1]) {
			klog.Infof("found mirrorpeer %q for drpolicy", mp.Name)
			return &mp, nil
		}
	}

	klog.Info("could not find any mirrorpeer for drpolicy")
	return nil, nil
}
func (r *DRPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Infof("running DRPolicy reconciler on hub cluster")
	// Fetch DRPolicy for given Request
	var drpolicy ramenv1alpha1.DRPolicy
	err := r.HubClient.Get(ctx, req.NamespacedName, &drpolicy)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Could not find DRPolicy. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		klog.Error(err, "Failed to get DRPolicy")
		return ctrl.Result{}, err
	}

	// find mirrorpeer for clusterset for the storagecluster namespaces
	mirrorPeer, err := r.getMirrorPeerForClusterSet(ctx, drpolicy.Spec.DRClusters)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}
		klog.Error("error occurred while trying to fetch MirrorPeer for given DRPolicy")
		return ctrl.Result{}, err
	}

	if err = r.processManagedClusterAddon(ctx, &drpolicy, mirrorPeer); err != nil {
		return ctrl.Result{}, err
	}

	if mirrorPeer.Spec.Type == multiclusterv1alpha1.Async {
		clusterFSIDs := make(map[string]string)
		klog.Infof("Fetching clusterFSIDs")
		err = r.fetchClusterFSIDs(ctx, mirrorPeer, clusterFSIDs)
		if err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("an unknown error occured while fetching the cluster fsids, retrying again: %v", err)
		}

		err = r.createOrUpdateManifestWorkForVRC(ctx, mirrorPeer, &drpolicy, clusterFSIDs)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create VolumeReplicationClass via ManifestWork: %v", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *DRPolicyReconciler) createOrUpdateManifestWorkForVRC(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, dp *ramenv1alpha1.DRPolicy, clusterFSIDs map[string]string) error {

	replicationId, err := utils.CreateUniqueReplicationId(clusterFSIDs)
	if err != nil {
		return err
	}

	for _, pr := range mp.Spec.Items {
		manifestWorkName := fmt.Sprintf("vrc-%v", utils.FnvHash(dp.Name)) // Two ManifestWork per DRPolicy
		found := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestWorkName,
				Namespace: pr.ClusterName,
			},
		}

		err := r.HubClient.Get(ctx, types.NamespacedName{Name: found.Name, Namespace: pr.ClusterName}, found)

		switch {
		case err == nil:
			klog.Infof("%s already exists. updating...", manifestWorkName)
		case !errors.IsNotFound(err):
			klog.Error(err, "failed to get ManifestWork: %s", manifestWorkName)
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
				Kind: "VolumeReplicationClass", APIVersion: "replication.storage.openshift.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   vrcName,
				Labels: labels,
			},
			Spec: replicationv1alpha1.VolumeReplicationClassSpec{
				Parameters:  params,
				Provisioner: fmt.Sprintf(RBDProvisionerTemplate, pr.StorageClusterRef.Namespace),
			},
		}

		objJson, err := json.Marshal(vrc)
		if err != nil {
			return fmt.Errorf("failed to marshal %v to JSON, error %w", vrc, err)
		}

		manifest := workv1.Manifest{}
		manifest.RawExtension = runtime.RawExtension{Raw: objJson}

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestWorkName,
				Namespace: pr.ClusterName, //target cluster
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
					Manifests: []workv1.Manifest{
						manifest,
					}},
			}
			return nil
		})

		if err != nil {
			klog.Error(err, "failed to create/update ManifestWork: %s", manifestWorkName)
			return err
		}

		klog.Infof("ManifestWork created for %s", vrcName)
	}

	return nil
}

func (r *DRPolicyReconciler) fetchClusterFSIDs(ctx context.Context, peer *multiclusterv1alpha1.MirrorPeer, clusterFSIDs map[string]string) error {
	var mcList clusterv1.ManagedClusterList
	err := r.HubClient.List(ctx, &mcList)
	if err != nil {
		return err
	}

	for _, pr := range peer.Spec.Items {
		for _, mc := range mcList.Items {
			if mc.Name == pr.ClusterName {
				for _, cc := range mc.Status.ClusterClaims {
					if cc.Name == "cephfsid.odf.openshift.io" {
						clusterFSIDs[pr.ClusterName] = cc.Value
						break
					}
				}
			}
		}
	}

	return nil
}
