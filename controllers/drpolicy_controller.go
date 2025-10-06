package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	templatev1 "github.com/openshift/api/template/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	DefaultMirroringMode                              = "snapshot"
	MirroringModeKey                                  = "mirroringMode"
	SchedulingIntervalKey                             = "schedulingInterval"
	RBDVolumeReplicationClassNameTemplate             = "rbd-volumereplicationclass-%v"
	RBDVolumeGroupReplicationClassNameTemplate        = "rbd-volumegroupreplicationclass-%v"
	RBDProvisionerName                                = "openshift-storage.rbd.csi.ceph.com"
	RBDFlattenVolumeReplicationClassNameTemplate      = "rbd-flatten-volumereplicationclass-%v"
	RBDFlattenVolumeGroupReplicationClassNameTemplate = "rbd-flatten-volumegroupreplicationclass-%v"
	RBDFlattenVolumeReplicationClassLabelKey          = "replication.storage.openshift.io/flatten-mode"
	RBDFlattenVolumeReplicationClassLabelValue        = "force"
	RBDVolumeReplicationClassDefaultAnnotation        = "replication.storage.openshift.io/is-default-class"
	RamenMaintenanceModeLabelKey                      = "ramendr.openshift.io/maintenancemodes"
	RamenMaintenanceModeLabelValue                    = "Failover"
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
	mirrorPeer, err := utils.GetMirrorPeerForClusterSet(ctx, r.HubClient, drpolicy.Spec.DRClusters)
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

	err = r.createOrUpdateManifestWorkForVRCAndVGRC(ctx, mirrorPeer, &drpolicy)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create VolumeReplicationClass via ManifestWork: %v", err)
	}

	logger.Info("Successfully reconciled DRPolicy")
	return ctrl.Result{}, nil
}

func (r *DRPolicyReconciler) createOrUpdateManifestWorkForVRCAndVGRC(ctx context.Context, mp *multiclusterv1alpha1.MirrorPeer, dp *ramenv1alpha1.DRPolicy) error {
	logger := r.Logger.With("DRPolicy", dp.Name, "MirrorPeer", mp.Name)

	var vrcList []replicationv1alpha1.VolumeReplicationClass
	var vgrcList []replicationv1alpha1.VolumeGroupReplicationClass

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
			Labels: map[string]string{
				RamenMaintenanceModeLabelKey: RamenMaintenanceModeLabelValue,
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

	vgrc := replicationv1alpha1.VolumeGroupReplicationClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VolumeGroupReplicationClass",
			APIVersion: "replication.storage.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf(RBDVolumeGroupReplicationClassNameTemplate, utils.FnvHash(dp.Spec.SchedulingInterval)),
			Labels: map[string]string{
				RamenMaintenanceModeLabelKey: RamenMaintenanceModeLabelValue,
			},
			Annotations: map[string]string{
				RBDVolumeReplicationClassDefaultAnnotation: "true",
			},
		},
		Spec: replicationv1alpha1.VolumeGroupReplicationClassSpec{
			Parameters: map[string]string{
				MirroringModeKey:      DefaultMirroringMode,
				SchedulingIntervalKey: dp.Spec.SchedulingInterval,
			},
			Provisioner: RBDProvisionerName,
		},
	}
	vgrcList = append(vgrcList, vgrc)

	if dp.Spec.ReplicationClassSelector.MatchLabels[RBDFlattenVolumeReplicationClassLabelKey] == RBDFlattenVolumeReplicationClassLabelValue {
		vrcFlatten := *vrc.DeepCopy()
		vrcFlatten.Name = fmt.Sprintf(RBDFlattenVolumeReplicationClassNameTemplate, utils.FnvHash(dp.Spec.SchedulingInterval))
		vrcFlatten.Labels = map[string]string{
			RBDFlattenVolumeReplicationClassLabelKey: RBDFlattenVolumeReplicationClassLabelValue,
			RamenMaintenanceModeLabelKey:             RamenMaintenanceModeLabelValue,
		}
		vrcFlatten.Annotations = map[string]string{}
		vrcFlatten.Spec.Parameters["flattenMode"] = "force"
		vrcList = append(vrcList, vrcFlatten)

		vgrcFlatten := *vgrc.DeepCopy()
		vgrcFlatten.Name = fmt.Sprintf(RBDFlattenVolumeGroupReplicationClassNameTemplate, utils.FnvHash(dp.Spec.SchedulingInterval))
		vgrcFlatten.Labels = map[string]string{
			RBDFlattenVolumeReplicationClassLabelKey: RBDFlattenVolumeReplicationClassLabelValue,
			RamenMaintenanceModeLabelKey:             RamenMaintenanceModeLabelValue,
		}
		vgrcFlatten.Annotations = map[string]string{}
		vgrcFlatten.Spec.Parameters["flattenMode"] = "force"
		vgrcList = append(vgrcList, vgrcFlatten)
	}

	cm, err := utils.FetchClientInfoConfigMap(ctx, r.HubClient, r.CurrentNamespace)
	if err != nil {
		return err
	}

	manifestWorkName := fmt.Sprintf("vrc-%v", utils.FnvHash(dp.Name))
	vgrcManifestWorkName := fmt.Sprintf("vgrc-%v", utils.FnvHash(dp.Name))
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

		var vgrcManifestList []workv1.Manifest
		for _, vgrc := range vgrcList {
			for _, cephblockpool := range cInfo.ProviderInfo.InfoCephBlockPools {
				if cephblockpool.MirrorEnabled {
					vgrcPoolName := fmt.Sprintf("%s-%v", vgrc.Name, utils.FnvHash(cephblockpool.Name))
					vgrc.Spec.Parameters["pool"] = cephblockpool.Name
					vgrcTemplateJson, err := getTemplateForVGRC(vgrc, vgrcPoolName, cInfo.ProviderInfo.NamespacedName.Namespace)
					if err != nil {
						return fmt.Errorf("failed to get template for VGRC %q, error %w", vrc.Name, err)
					}
					vgrcManifestList = append(vgrcManifestList, workv1.Manifest{
						RawExtension: runtime.RawExtension{
							Raw: vgrcTemplateJson,
						},
					})
				}
			}
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
				Labels: map[string]string{
					utils.CreatedByLabelKey:  utils.CreatorMulticlusterOrchestrator,
					utils.CreatedForClientID: cInfo.ClientID,
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

		vgrcmw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vgrcManifestWorkName,
				Namespace: cInfo.ProviderInfo.ProviderManagedClusterName,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       dp.Kind,
						Name:       dp.Name,
						UID:        dp.UID,
						APIVersion: dp.APIVersion,
					},
				},
				Labels: map[string]string{
					utils.CreatedByLabelKey:  utils.CreatorMulticlusterOrchestrator,
					utils.CreatedForClientID: cInfo.ClientID,
				},
			},
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.HubClient, &vgrcmw, func() error {
			vgrcmw.Spec = workv1.ManifestWorkSpec{
				Workload: workv1.ManifestsTemplate{
					Manifests: vgrcManifestList,
				},
			}
			return nil
		})

		if err != nil {
			logger.Error("Failed to create/update ManifestWork", "ManifestWorkName", vgrcManifestWorkName, "error", err)
			return err
		}

		logger.Info("ManifestWork created/updated successfully", "ManifestWorkName", vgrcManifestWorkName, "VolumeGroupReplicationClassName", vgrc.Name)
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
			Labels: map[string]string{
				utils.CreatedByLabelKey:  utils.CreatorMulticlusterOrchestrator,
				utils.ObjectKindLabelKey: "VolumeReplicationClass",
			},
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

func getTemplateForVGRC(vgrc replicationv1alpha1.VolumeGroupReplicationClass, vgrcName, templateNamespace string) ([]byte, error) {
	vgrc.Name = vgrcName
	vgrcJson, err := json.Marshal(vgrc)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal %v to JSON, error %w", vgrc, err)
	}

	vgrcTemplate := templatev1.Template{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Template",
			APIVersion: "template.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      vgrc.Name,
			Namespace: templateNamespace,
			Labels: map[string]string{
				utils.CreatedByLabelKey:  utils.CreatorMulticlusterOrchestrator,
				utils.ObjectKindLabelKey: "VolumeGroupReplicationClass",
			},
		},
		Objects: []runtime.RawExtension{
			{
				Raw: vgrcJson,
			},
		},
	}

	vgrcTemplateJson, err := json.Marshal(vgrcTemplate)
	if err != nil {
		return []byte{}, fmt.Errorf("failed to marshal %v to JSON, error %w", vgrcTemplate, err)
	}

	return vgrcTemplateJson, nil
}
