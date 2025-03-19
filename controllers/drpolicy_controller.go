package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/apis/replication.storage/v1alpha1"
	templatev1 "github.com/openshift/api/template/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DefaultMirroringMode                         = "snapshot"
	MirroringModeKey                             = "mirroringMode"
	SchedulingIntervalKey                        = "schedulingInterval"
	RBDVolumeReplicationClassNameTemplate        = "rbd-volumereplicationclass-%v"
	RBDProvisionerName                           = "openshift-storage.rbd.csi.ceph.com"
	RBDFlattenVolumeReplicationClassNameTemplate = "rbd-flatten-volumereplicationclass-%v"
	RBDFlattenVolumeReplicationClassLabelKey     = "replication.storage.openshift.io/flatten-mode"
	RBDFlattenVolumeReplicationClassLabelValue   = "force"
	RBDVolumeReplicationClassDefaultAnnotation   = "replication.storage.openshift.io/is-default-class"
)

type DRPolicyReconciler struct {
	HubClient client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger

	testEnvFile      string
	CurrentNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *DRPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up DRPolicyReconciler with manager")

	cmToDRPolicyMapFunc := func(ctx context.Context, object client.Object) []ctrl.Request {
		r.Logger.Debug("Mapping ConfigMap to DRPolicy", "ConfigMap", client.ObjectKeyFromObject(object))
		var reqs []ctrl.Request
		_, ok := object.(*corev1.ConfigMap)
		if !ok {
			r.Logger.Debug("Unable to cast object into a ConfigMap. Not requeing any requests.")
			return reqs
		}
		var drpolicyList ramenv1alpha1.DRPolicyList
		err := r.HubClient.List(ctx, &drpolicyList)
		if err != nil {
			r.Logger.Debug("Unable to fetch list of all DRPolicies. Not requeing any requests.")
			return reqs
		}
		for _, mp := range drpolicyList.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: mp.Name}})
		}
		r.Logger.Info("DRPolicy reconcile requests generated based on ConfigMap change.", "RequestCount", len(reqs), "Requests", reqs)
		return reqs
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.DRPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(cmToDRPolicyMapFunc),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
				cm, ok := object.(*corev1.ConfigMap)
				if !ok || cm.Name != utils.ClientInfoConfigMapName || cm.Namespace != r.CurrentNamespace {
					return false
				}
				return true
			}))).
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

	if mirrorPeer.Spec.Type != multiclusterv1alpha1.Async {
		return ctrl.Result{}, nil
	}

	err = r.createOrUpdateManifestWorkForVRC(ctx, mirrorPeer, &drpolicy)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create VolumeReplicationClass via ManifestWork: %v", err)
	}

	logger.Info("Successfully reconciled DRPolicy")
	return ctrl.Result{}, nil
}

func (r *DRPolicyReconciler) createOrUpdateManifestWorkForVRC(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, dp *ramenv1alpha1.DRPolicy) error {
	logger := r.Logger.With("DRPolicy", dp.Name, "MirrorPeer", mp.Name)

	var vrcList []replicationv1alpha1.VolumeReplicationClass

	vrc := replicationv1alpha1.VolumeReplicationClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VolumeReplicationClass",
			APIVersion: "replication.storage.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(RBDVolumeReplicationClassNameTemplate, utils.FnvHash(dp.Spec.SchedulingInterval)),
			Annotations: map[string]string{
				RBDVolumeReplicationClassDefaultAnnotation: "true",
			},
		},
		Spec: replicationv1alpha1.VolumeReplicationClassSpec{
			Parameters: map[string]string{
				MirroringModeKey:      DefaultMirroringMode,
				SchedulingIntervalKey: dp.Spec.SchedulingInterval,
			},
			Provisioner: RBDProvisionerName,
		},
	}
	vrcList = append(vrcList, vrc)

	if dp.Spec.ReplicationClassSelector.MatchLabels[RBDFlattenVolumeReplicationClassLabelKey] == RBDFlattenVolumeReplicationClassLabelValue {
		vrcFlatten := *vrc.DeepCopy()
		vrcFlatten.Name = fmt.Sprintf(RBDFlattenVolumeReplicationClassNameTemplate, utils.FnvHash(dp.Spec.SchedulingInterval))
		vrcFlatten.Labels = map[string]string{
			RBDFlattenVolumeReplicationClassLabelKey: RBDFlattenVolumeReplicationClassLabelValue,
		}
		vrcFlatten.Annotations = map[string]string{}
		vrcFlatten.Spec.Parameters["flattenMode"] = "force"
		vrcList = append(vrcList, vrcFlatten)
	}

	cm, err := utils.FetchClientInfoConfigMap(ctx, r.HubClient, r.CurrentNamespace)
	if err != nil {
		return err
	}

	manifestWorkName := fmt.Sprintf("vrc-%v", utils.FnvHash(dp.Name))
	for _, pr := range mp.Spec.Items {
		cInfo, err := utils.GetClientInfoFromConfigMap(cm.Data, utils.GetKey(pr.ClusterName, pr.StorageClusterRef.Name))
		if err != nil {
			return err
		}

		var manifestList []workv1.Manifest
		for _, vrc := range vrcList {
			vrcTemplateJson, err := getTemplateForVRC(vrc, cInfo.ProviderInfo.NamespacedName.Namespace)
			if err != nil {
				return fmt.Errorf("failed to get template for VRC %q, error %w", vrc.Name, err)
			}
			manifestList = append(manifestList, workv1.Manifest{
				RawExtension: runtime.RawExtension{
					Raw: vrcTemplateJson,
				},
			})
		}

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestWorkName,
				Namespace: cInfo.ProviderInfo.ProviderManagedClusterName,
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

		logger.Info("ManifestWork created/updated successfully", "ManifestWorkName", manifestWorkName, "VolumeReplicationClassName", vrc.Name)
	}

	return nil
}

func getTemplateForVRC(vrc replicationv1alpha1.VolumeReplicationClass, templateNamespace string) ([]byte, error) {
	vrcJson, err := json.Marshal(vrc)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal %v to JSON, error %w", vrc, err)
	}

	vrcTemplate := templatev1.Template{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Template",
			APIVersion: "template.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      vrc.Name,
			Namespace: templateNamespace,
		},
		Objects: []runtime.RawExtension{
			{
				Raw: vrcJson,
			},
		},
	}

	vrcTemplateJson, err := json.Marshal(vrcTemplate)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal %v to JSON, error %w", vrcTemplate, err)
	}

	return vrcTemplateJson, nil
}
