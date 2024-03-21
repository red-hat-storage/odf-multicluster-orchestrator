package addons

import (
	"context"
	"fmt"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type MaintenanceModeReconciler struct {
	Scheme           *runtime.Scheme
	SpokeClient      client.Client
	SpokeClusterName string
}

func (r *MaintenanceModeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("maintenancemode_controller").
		Watches(&ramenv1alpha1.MaintenanceMode{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func (r *MaintenanceModeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Info("starting reconcile for maintenancemode")
	// Fetch MaintenanceMode for given Request
	var mmode ramenv1alpha1.MaintenanceMode
	err := r.SpokeClient.Get(ctx, req.NamespacedName, &mmode)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("Could not find MaintenanceMode. Ignoring since object must have been deleted.")
			return ctrl.Result{}, nil
		}
		klog.Error("Failed to get MaintenanceMode.", err)
		return ctrl.Result{}, err
	}

	cephClusters, err := utils.FetchAllCephClusters(ctx, r.SpokeClient)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cephClusters == nil || len(cephClusters.Items) == 0 {
		klog.Infof("No CephClusters available on the cluster.")
		return ctrl.Result{Requeue: true}, nil
	}

	actionableCephCluster := filterCephClustersByStorageIdOrReplicationId(cephClusters, mmode.Spec.TargetID, mmode.Spec.StorageProvisioner)

	if actionableCephCluster == nil {
		klog.Infof("No CephCluster present with required parameters for maintenance. StorageId/ReplicationId=%s , Provisioner=%s ", mmode.Spec.TargetID, mmode.Spec.StorageProvisioner)
		// Requeueing the request as the cephcluster can be potentially found if it is labeled later.
		return ctrl.Result{Requeue: true}, nil
	}

	if !mmode.GetDeletionTimestamp().IsZero() || len(mmode.Spec.Modes) == 0 {
		if !utils.ContainsString(mmode.GetFinalizers(), MaintenanceModeFinalizer) {
			return ctrl.Result{}, nil
		}
		result, err := r.startMaintenanceActions(ctx, &mmode, actionableCephCluster, false)
		if err != nil {
			klog.Errorf("failed to complete maintenance actions on %s. err=%v", mmode.Name, err)
			return result, err
		}
		if !result.Requeue {
			mmode.Finalizers = utils.RemoveString(mmode.Finalizers, MaintenanceModeFinalizer)
			err = r.SpokeClient.Update(ctx, &mmode)
			if err != nil {
				klog.Errorf("failed to remove finalizer from MaintenanceMode %s. err=%v", mmode.Name, err)
				return ctrl.Result{}, err
			}
		}
		klog.Info("MaintenanceMode disabled")
		return result, nil
	}

	if !utils.ContainsString(mmode.GetFinalizers(), MaintenanceModeFinalizer) {
		klog.Infof("finalizer not found on MaintenanceMode. adding Finalizer=%s", MaintenanceModeFinalizer)
		mmode.Finalizers = append(mmode.Finalizers, MaintenanceModeFinalizer)
		if err := r.SpokeClient.Update(ctx, &mmode); err != nil {
			klog.Errorf("failed to add finalizer to MaintenanceMode=%s. err=%v ", mmode.Name, err)
			return ctrl.Result{}, err
		}
	}

	if mmode.Status.State == ramenv1alpha1.MModeStateCompleted && mmode.Status.ObservedGeneration == mmode.Generation {
		return ctrl.Result{}, nil
	}

	result, err := r.startMaintenanceActions(ctx, &mmode, actionableCephCluster, true)
	statusErr := r.SpokeClient.Status().Update(ctx, &mmode)
	if statusErr != nil {
		klog.Errorf("failed to update status of maintenancemode=%s. err=%v", mmode.Name, statusErr)
		return result, statusErr
	}
	if err != nil {
		klog.Errorf("failed to complete maintenance actions on %s. err=%v", mmode.Name, err)
	}
	return result, err
}

func (r *MaintenanceModeReconciler) startMaintenanceActions(ctx context.Context, mmode *ramenv1alpha1.MaintenanceMode, cluster *rookv1.CephCluster, scaleDown bool) (ctrl.Result, error) {
	klog.Infof("starting actions on MaintenanceMode %q", mmode.Name)
	for _, mode := range mmode.Spec.Modes {
		SetStatus(mmode, mode, ramenv1alpha1.MModeStateProgressing, nil)
		switch mode {
		case ramenv1alpha1.MModeFailover:
			var replicas int
			if scaleDown {
				klog.Info("Scaling down RBD mirror deployments")
				replicas = 0
			} else {
				klog.Info("Scaling up RBD mirror deployments")
				replicas = 1
			}
			result, err := r.scaleRBDMirrorDeployment(ctx, *cluster, replicas)
			if err != nil {
				SetStatus(mmode, mode, ramenv1alpha1.MModeStateError, err)
				return result, err
			}
			if !result.Requeue {
				SetStatus(mmode, mode, ramenv1alpha1.MModeStateCompleted, nil)
			}
			return result, err
		default:
			return ctrl.Result{}, fmt.Errorf("no actions found for %q mode", mode)
		}
	}
	return ctrl.Result{}, nil
}

func (r *MaintenanceModeReconciler) scaleRBDMirrorDeployment(ctx context.Context, cluster rookv1.CephCluster, replicas int) (ctrl.Result, error) {
	isMirroringEnabled, err := r.isStorageClusterMirroringEnabled(ctx, cluster)
	if err != nil {
		klog.Errorf("error occurred on checking mirroring enablement on storagecluster for cephcluster %s", cluster.Name)
		return ctrl.Result{}, err
	}
	if !isMirroringEnabled {
		klog.Info("storagecluster mirroring is not enabled yet. please enable it manually or wait for it to be enabled to perform further maintenance actions")
		return ctrl.Result{Requeue: true}, nil
	}
	deployments, err := GetDeploymentsStartingWith(ctx, r.SpokeClient, cluster.Namespace, RBDMirrorDeploymentNamePrefix)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, deploymentName := range deployments {
		requeue, err := utils.ScaleDeployment(ctx, r.SpokeClient, deploymentName, cluster.Namespace, int32(replicas))
		return ctrl.Result{Requeue: requeue}, err
	}
	return ctrl.Result{}, nil
}

// Checks the mirroring status of the storagecluster. Deployment will not be present if mirroring is not enabled.
func (r *MaintenanceModeReconciler) isStorageClusterMirroringEnabled(ctx context.Context, cephCluster rookv1.CephCluster) (bool, error) {
	var sc ocsv1.StorageCluster
	scName := getOwnerName(cephCluster)
	if scName == "" {
		return false, fmt.Errorf("no storagecluster found for given cephcluster %s", cephCluster.Name)
	}
	err := r.SpokeClient.Get(ctx, types.NamespacedName{Name: scName, Namespace: cephCluster.Namespace}, &sc)
	if err != nil {
		klog.Errorf("no storagecluster found for given cephcluster %s", cephCluster.Name)
		return false, err
	}
	return true, nil
}

func getOwnerName(cluster rookv1.CephCluster) string {
	for _, owner := range cluster.OwnerReferences {
		if owner.Kind != "StorageCluster" {
			continue
		}
		return owner.Name
	}
	return ""
}

func SetStatus(mmode *ramenv1alpha1.MaintenanceMode, mode ramenv1alpha1.MMode, state ramenv1alpha1.MModeState, err error) {
	klog.Infof("mode: %s, status: %s, generation: %d, error: %v ", mode, state, mmode.Generation, err)
	mmode.Status.State = state
	mmode.Status.ObservedGeneration = mmode.Generation
	k8smeta.SetStatusCondition(&mmode.Status.Conditions, newCondition(state, mode, mmode.Generation, err))
}

func newCondition(state ramenv1alpha1.MModeState, mode ramenv1alpha1.MMode, generation int64, err error) metav1.Condition {
	var reason string
	var message string
	var status metav1.ConditionStatus

	switch state {
	case ramenv1alpha1.MModeStateProgressing:
		reason = "MaintenanceProgressing"
		message = fmt.Sprintf("Maintenance(mode=%s) of cluster is in progress", mode)
		status = metav1.ConditionTrue
	case ramenv1alpha1.MModeStateError:
		reason = "MaintenanceError"
		message = fmt.Sprintf("Maintenance(mode=%s) of cluster is in error state, err: %v", mode, err)
		status = metav1.ConditionTrue
	case ramenv1alpha1.MModeStateCompleted:
		reason = "MaintenanceCompleted"
		message = fmt.Sprintf("Maintenance(mode=%s) of cluster has completed successfully", mode)
		status = metav1.ConditionTrue
	default:
		reason = "MaintenanceUnknown"
		message = fmt.Sprintf("Maintenance(mode=%s) of cluster is unknown state", mode)
		status = metav1.ConditionUnknown
	}
	return metav1.Condition{
		Type:               string(ramenv1alpha1.MModeConditionFailoverActivated),
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	}
}

func GetDeploymentsStartingWith(ctx context.Context, spokeClient client.Client, namespace string, prefix string) ([]string, error) {
	deploymentList := &appsv1.DeploymentList{}
	rookMirrorDeploymentLabel, err := labels.NewRequirement("app", selection.Equals, []string{"rook-ceph-rbd-mirror"})
	if err != nil {
		klog.Error(err, "cannot parse new requirement")
	}

	deploymentSelector := labels.NewSelector().Add(*rookMirrorDeploymentLabel)
	listOpts := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: deploymentSelector,
	}

	err = spokeClient.List(ctx, deploymentList, listOpts)
	if err != nil {
		return nil, err
	}

	var deploymentNames []string

	for _, deployment := range deploymentList.Items {
		if len(deployment.ObjectMeta.Name) < len(prefix) || deployment.ObjectMeta.Name[:len(prefix)] != prefix {
			continue
		}
		deploymentNames = append(deploymentNames, deployment.ObjectMeta.Name)
	}

	return deploymentNames, nil
}

func filterCephClustersByStorageIdOrReplicationId(clusters *rookv1.CephClusterList, targetId string, provisioner string) *rookv1.CephCluster {
	for _, cc := range clusters.Items {
		fullProvisionerName := fmt.Sprintf(RBDProvisionerTemplate, cc.Namespace)
		if cc.Status.CephStatus.FSID == targetId && provisioner == fullProvisionerName {
			return &cc
		}
		if cc.Labels != nil && cc.Labels[utils.CephClusterReplicationIdLabel] == targetId {
			return &cc
		}
	}
	return nil
}
