package maintenance

import (
	"context"
	"fmt"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"
)

type MaintenanceModeReconciler struct {
	Scheme           *runtime.Scheme
	SpokeClient      client.Client
	SpokeClusterName string
}

const (
	RBDProvisionerTemplate        = "%s.rbd.csi.ceph.com"
	MaintenanceModeFinalizer      = "maintenance.multicluster.odf.openshift.io"
	RBDMirrorDeploymentNamePrefix = "rook-ceph-rbd-mirror"
)

func (r *MaintenanceModeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ramenv1alpha1.MaintenanceMode{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func (r *MaintenanceModeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.Info("starting reconcile for maintenancemode")
	// Fetch MaintenanceMode for given Request
	var mmode ramenv1alpha1.MaintenanceMode
	err := r.SpokeClient.Get(ctx, req.NamespacedName, &mmode)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Info("could not find MaintenanceMode. ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		klog.Error(err, "failed to get MaintenanceMode")
		return ctrl.Result{}, err
	}

	if mmode.GetDeletionTimestamp().IsZero() {
		if !utils.ContainsString(mmode.GetFinalizers(), MaintenanceModeFinalizer) {
			klog.Info("finalizer not found on MaintenanceMode. adding Finalizer ", MaintenanceModeFinalizer)
			mmode.Finalizers = append(mmode.Finalizers, MaintenanceModeFinalizer)
			if err := r.SpokeClient.Update(ctx, &mmode); err != nil {
				klog.Error(err, "failed to add finalizer to MaintenanceMode", mmode.Name)
				return ctrl.Result{}, err
			}
		}
	}

	cephClusters, err := fetchAllCephClusters(ctx, r.SpokeClient)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cephClusters == nil || len(cephClusters.Items) == 0 {
		klog.Info("no CephClusters available on the cluster")
		return ctrl.Result{}, nil
	}

	actionableCephClusters := filterCephClustersByStorageId(cephClusters, mmode.Spec.TargetID, mmode.Spec.StorageProvisioner)

	if len(actionableCephClusters) == 0 {
		klog.Info("no CephClusters present with required parameters for maintenance", "StorageId", mmode.Spec.TargetID, "Provisioner", mmode.Spec.StorageProvisioner)
		return ctrl.Result{}, nil
	}

	if !mmode.GetDeletionTimestamp().IsZero() || len(mmode.Spec.Modes) == 0 {
		result, err := r.startMaintenanceActions(ctx, mmode, actionableCephClusters, false)
		if err != nil {
			return result, err
		}
		mmode.Finalizers = utils.RemoveString(mmode.Finalizers, MaintenanceModeFinalizer)

		if err := r.SpokeClient.Update(ctx, &mmode); err != nil {
			klog.Error(err, "failed to remove finalizer from MaintenanceMode ", err)
			return ctrl.Result{}, err
		}
		klog.Info("MaintenanceMode deleted, skipping reconciliation")
		return ctrl.Result{}, nil
	} else {
		_, err := r.startMaintenanceActions(ctx, mmode, actionableCephClusters, true)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *MaintenanceModeReconciler) startMaintenanceActions(ctx context.Context, mmode ramenv1alpha1.MaintenanceMode, clusters []rookv1.CephCluster, scaleDown bool) (ctrl.Result, error) {
	klog.Info("starting actions on MaintenanceMode", "MaintenanceMode ", mmode.Name)
	for _, mode := range mmode.Spec.Modes {
		res, err := r.updateStatus(ctx, mmode, mode, ramenv1alpha1.MModeStateProgressing, nil)
		if err != nil {
			return res, err
		}
		switch mode {
		case ramenv1alpha1.MMode(ramenv1alpha1.ActionFailover):
			var replicas int
			if scaleDown {
				klog.Info("Scaling down RBD mirror deployments")
				replicas = 0
			} else {
				klog.Info("Scaling up RBD mirror deployments")
				replicas = 1
			}
			_, err := r.scaleRBDMirrorDeployment(ctx, clusters, replicas)
			if err != nil {
				res, statusErr := r.updateStatus(ctx, mmode, mode, ramenv1alpha1.MModeStateError, err)
				if statusErr != nil {
					return res, fmt.Errorf("failed to update error status %v while having error %v", statusErr, err)
				}
				return res, err
			}
			res, err = r.updateStatus(ctx, mmode, mode, ramenv1alpha1.MModeStateCompleted, nil)
			if err != nil {
				return res, err
			}
		default:
			return ctrl.Result{}, fmt.Errorf("no actions found for %q mode", mode)
		}
	}
	return ctrl.Result{}, nil
}

func (r *MaintenanceModeReconciler) scaleRBDMirrorDeployment(ctx context.Context, clusters []rookv1.CephCluster, replicas int) (ctrl.Result, error) {
	for _, cephCluster := range clusters {
		deployments, err := GetDeploymentsStartingWith(ctx, r.SpokeClient, cephCluster.Namespace, RBDMirrorDeploymentNamePrefix)
		if err != nil {
			return ctrl.Result{}, err
		}

		for _, deploymentName := range deployments {
			err = scaleDeployment(ctx, r.SpokeClient, deploymentName, cephCluster.Namespace, int32(replicas))
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MaintenanceModeReconciler) updateStatus(ctx context.Context, mmode ramenv1alpha1.MaintenanceMode, mode ramenv1alpha1.MMode, state ramenv1alpha1.MModeState, err error) (ctrl.Result, error) {
	klog.Infof("mode: %s, status: %s, generation: %d, error: %v ", mode, state, mmode.Generation, err)
	mmode.Status.State = state
	mmode.Status.ObservedGeneration = mmode.Generation
	var conditions []metav1.Condition
	if len(mmode.Status.Conditions) == 0 {
		conditions = make([]metav1.Condition, 0)
		conditions = append(conditions, newCondition(state, mode, mmode.Generation, err))
	}
	k8smeta.SetStatusCondition(&conditions, newCondition(state, mode, mmode.Generation, err))
	mmode.Status.Conditions = conditions
	statusErr := r.SpokeClient.Status().Update(ctx, &mmode)
	if statusErr != nil {
		klog.Error(statusErr)
		if k8serrors.IsConflict(statusErr) {
			// The object is being updated, and we need to requeue the request again for updates.
			klog.Info("the object is being updated, retrying the request again...")
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}
		return ctrl.Result{}, statusErr
	}
	return ctrl.Result{}, nil
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
		Type:               string(mode),
		Status:             status,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

func GetDeploymentsStartingWith(ctx context.Context, spokeClient client.Client, namespace string, prefix string) ([]string, error) {
	deploymentList := &appsv1.DeploymentList{}
	rookMirrorDeploymentLabel, err := labels.NewRequirement("app", selection.In, []string{"rook-ceph-rbd-mirror"})
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

func scaleDeployment(ctx context.Context, client client.Client, deploymentName string, namespace string, replicas int32) error {
	var deployment appsv1.Deployment
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, &deployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s not found in namespace %s", deploymentName, namespace)
		}
		return err
	}
	deployment.Spec.Replicas = &replicas
	err = client.Update(ctx, &deployment)
	if err != nil {
		return err
	}
	var updatedDeployment appsv1.Deployment
	for {
		err := client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace, Name: deploymentName,
		}, &updatedDeployment)
		if err != nil {
			return err
		}
		if updatedDeployment.Status.Replicas == replicas {
			break
		}
	}
	return nil
}

func filterCephClustersByStorageId(clusters *rookv1.CephClusterList, storageId string, provisioner string) []rookv1.CephCluster {
	cephClusters := make([]rookv1.CephCluster, 0)
	for _, cc := range clusters.Items {
		fullProvisionerName := fmt.Sprintf(RBDProvisionerTemplate, cc.Namespace)
		if cc.Status.CephStatus.FSID == storageId && provisioner == fullProvisionerName {
			cephClusters = append(cephClusters, cc)
		}
	}
	return cephClusters
}

func fetchAllCephClusters(ctx context.Context, client client.Client) (*rookv1.CephClusterList, error) {
	var cephClusters rookv1.CephClusterList
	err := client.List(ctx, &cephClusters)
	if err != nil {
		return nil, fmt.Errorf("failed to list CephClusters %v", err)
	}

	if len(cephClusters.Items) == 0 {
		klog.Info("no CephClusters found on current cluster")
		return nil, nil
	}
	return &cephClusters, nil
}
