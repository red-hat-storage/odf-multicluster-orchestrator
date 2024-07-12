package addons

import (
	"context"
	"fmt"
	"log/slog"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
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
	Logger           *slog.Logger
}

func (r *MaintenanceModeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up controller with manager")

	return ctrl.NewControllerManagedBy(mgr).
		Named("maintenancemode_controller").
		Watches(&ramenv1alpha1.MaintenanceMode{}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func (r *MaintenanceModeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("MaintenanceMode", req.NamespacedName)
	logger.Info("Starting reconcile for MaintenanceMode")

	var mmode ramenv1alpha1.MaintenanceMode
	err := r.SpokeClient.Get(ctx, req.NamespacedName, &mmode)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("MaintenanceMode not found, ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		logger.Error("Failed to get MaintenanceMode", "error", err)
		return ctrl.Result{}, err
	}

	cephClusters, err := utils.FetchAllCephClusters(ctx, r.SpokeClient)
	if err != nil {
		logger.Error("Failed to fetch all CephClusters", "error", err)
		return ctrl.Result{}, err
	}

	if len(cephClusters.Items) == 0 {
		logger.Info("No CephClusters available on the cluster")
		return ctrl.Result{Requeue: true}, nil
	}

	actionableCephCluster := filterCephClustersByStorageIdOrReplicationId(cephClusters, mmode.Spec.TargetID, mmode.Spec.StorageProvisioner)
	if actionableCephCluster == nil {
		logger.Info("No CephCluster present with required parameters for maintenance", "StorageId/ReplicationId", mmode.Spec.TargetID, "Provisioner", mmode.Spec.StorageProvisioner)
		return ctrl.Result{Requeue: true}, nil
	}

	if !mmode.GetDeletionTimestamp().IsZero() || len(mmode.Spec.Modes) == 0 {
		if !utils.ContainsString(mmode.GetFinalizers(), MaintenanceModeFinalizer) {
			return ctrl.Result{}, nil
		}
		result, err := r.startMaintenanceActions(ctx, &mmode, actionableCephCluster, false)
		if err != nil {
			logger.Error("Failed to complete maintenance actions", "error", err)
			return result, err
		}
		if !result.Requeue {
			mmode.Finalizers = utils.RemoveString(mmode.Finalizers, MaintenanceModeFinalizer)
			if err := r.SpokeClient.Update(ctx, &mmode); err != nil {
				logger.Error("Failed to remove finalizer from MaintenanceMode", "error", err)
				return ctrl.Result{}, err
			}
			logger.Info("MaintenanceMode disabled")
		}
		return result, nil
	}

	if !utils.ContainsString(mmode.GetFinalizers(), MaintenanceModeFinalizer) {
		r.Logger.Info("Adding finalizer to MaintenanceMode", "finalizer", MaintenanceModeFinalizer)
		mmode.Finalizers = append(mmode.Finalizers, MaintenanceModeFinalizer)
		if err := r.SpokeClient.Update(ctx, &mmode); err != nil {
			r.Logger.Error("Failed to add finalizer to MaintenanceMode", "error", err)
			return ctrl.Result{}, err
		}
	}

	if mmode.Status.State == ramenv1alpha1.MModeStateCompleted && mmode.Status.ObservedGeneration == mmode.Generation {
		return ctrl.Result{}, nil
	}

	result, err := r.startMaintenanceActions(ctx, &mmode, actionableCephCluster, true)
	if statusErr := r.SpokeClient.Status().Update(ctx, &mmode); statusErr != nil {
		r.Logger.Error("Failed to update status of MaintenanceMode", "error", statusErr)
		return result, statusErr
	}
	if err != nil {
		r.Logger.Error("Failed to complete maintenance actions", "error", err)
	}
	return result, err
}

func (r *MaintenanceModeReconciler) startMaintenanceActions(ctx context.Context, mmode *ramenv1alpha1.MaintenanceMode, cluster *rookv1.CephCluster, scaleDown bool) (ctrl.Result, error) {
	r.Logger.Info("Starting actions on MaintenanceMode", "MaintenanceMode", mmode.Name)
	for _, mode := range mmode.Spec.Modes {
		SetStatus(r.Logger, mmode, mode, ramenv1alpha1.MModeStateProgressing, nil)
		switch mode {
		case ramenv1alpha1.MModeFailover:
			var replicas int
			if scaleDown {
				r.Logger.Info("Scaling down RBD mirror deployments")
				replicas = 0
			} else {
				r.Logger.Info("Scaling up RBD mirror deployments")
				replicas = 1
			}
			result, err := r.scaleRBDMirrorDeployment(ctx, *cluster, replicas)
			if err != nil {
				r.Logger.Error("Failed to scale RBD mirror deployments", "error", err)
				SetStatus(r.Logger, mmode, mode, ramenv1alpha1.MModeStateError, err)
				return result, err
			}
			if !result.Requeue {
				SetStatus(r.Logger, mmode, mode, ramenv1alpha1.MModeStateCompleted, nil)
			}
			return result, nil
		default:
			err := fmt.Errorf("no actions found for %q mode", mode)
			r.Logger.Error("No actions found for specified mode", "mode", mode, "error", err)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MaintenanceModeReconciler) scaleRBDMirrorDeployment(ctx context.Context, cluster rookv1.CephCluster, replicas int) (ctrl.Result, error) {
	isMirroringEnabled, err := r.isStorageClusterMirroringEnabled(ctx, cluster)
	if err != nil {
		r.Logger.Error("Error checking mirroring enablement on StorageCluster", "CephCluster", cluster.Name, "error", err)
		return ctrl.Result{}, err
	}
	if !isMirroringEnabled {
		r.Logger.Info("StorageCluster mirroring is not enabled yet. Please enable it manually or wait for it to be enabled to perform further maintenance actions")
		return ctrl.Result{Requeue: true}, nil
	}

	deployments, err := GetDeploymentsStartingWith(ctx, r.SpokeClient, cluster.Namespace, RBDMirrorDeploymentNamePrefix, r.Logger)
	if err != nil {
		r.Logger.Error("Failed to get deployments starting with prefix", "namespace", cluster.Namespace, "prefix", RBDMirrorDeploymentNamePrefix, "error", err)
		return ctrl.Result{}, err
	}

	for _, deploymentName := range deployments {
		r.Logger.Info("Scaling deployment", "deploymentName", deploymentName, "namespace", cluster.Namespace, "replicas", replicas)
		requeue, err := utils.ScaleDeployment(ctx, r.SpokeClient, deploymentName, cluster.Namespace, int32(replicas))
		if err != nil {
			r.Logger.Error("Failed to scale deployment", "deploymentName", deploymentName, "namespace", cluster.Namespace, "error", err)
			return ctrl.Result{Requeue: requeue}, err
		}
	}
	return ctrl.Result{}, nil
}

// Checks the mirroring status of the storagecluster. Deployment will not be present if mirroring is not enabled.
func (r *MaintenanceModeReconciler) isStorageClusterMirroringEnabled(ctx context.Context, cephCluster rookv1.CephCluster) (bool, error) {
	var sc ocsv1.StorageCluster
	scName := getOwnerName(cephCluster)
	if scName == "" {
		return false, fmt.Errorf("no StorageCluster found for given CephCluster %s", cephCluster.Name)
	}

	err := r.SpokeClient.Get(ctx, types.NamespacedName{Name: scName, Namespace: cephCluster.Namespace}, &sc)
	if err != nil {
		r.Logger.Error("No StorageCluster found for CephCluster", "CephCluster", cephCluster.Name, "error", err)
		return false, err
	}

	r.Logger.Info("StorageCluster mirroring is enabled", "CephCluster", cephCluster.Name, "StorageCluster", sc.Name)
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

func SetStatus(logger *slog.Logger, mmode *ramenv1alpha1.MaintenanceMode, mode ramenv1alpha1.MMode, state ramenv1alpha1.MModeState, err error) {
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

func GetDeploymentsStartingWith(ctx context.Context, spokeClient client.Client, namespace string, prefix string, logger *slog.Logger) ([]string, error) {
	deploymentList := &appsv1.DeploymentList{}
	rookMirrorDeploymentLabel, err := labels.NewRequirement("app", selection.Equals, []string{"rook-ceph-rbd-mirror"})
	if err != nil {
		logger.Error("Cannot parse new requirement", "error", err)
		return nil, err
	}

	deploymentSelector := labels.NewSelector().Add(*rookMirrorDeploymentLabel)
	listOpts := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: deploymentSelector,
	}

	err = spokeClient.List(ctx, deploymentList, listOpts)
	if err != nil {
		logger.Error("Failed to list deployments", "namespace", namespace, "error", err)
		return nil, err
	}

	var deploymentNames []string

	for _, deployment := range deploymentList.Items {
		if len(deployment.ObjectMeta.Name) < len(prefix) || deployment.ObjectMeta.Name[:len(prefix)] != prefix {
			continue
		}
		deploymentNames = append(deploymentNames, deployment.ObjectMeta.Name)
	}

	logger.Info("Deployments found with specified prefix", "prefix", prefix, "deploymentNames", deploymentNames)
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
