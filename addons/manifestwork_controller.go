package addons

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ManifestWorkReconciler reconciles a ManifestWork object
type ManifestWorkReconciler struct {
	Scheme      *runtime.Scheme
	HubClient   client.Client
	SpokeClient client.Client
	Logger      *slog.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManifestWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isVRCManifestWork := func(o client.Object) bool {
		mw, ok := o.(*workv1.ManifestWork)
		if !ok {
			return false
		}
		if strings.HasPrefix(mw.Name, "vrc-") {
			return true
		}
		return false
	}

	manifestWorkPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isVRCManifestWork(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isVRCManifestWork(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isVRCManifestWork(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	r.Logger.Info("Setting up controller with manager")
	return ctrl.NewControllerManagedBy(mgr).
		Named("manifestwork_controller").
		For(&workv1.ManifestWork{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, manifestWorkPredicate)).
		Complete(r)
}

func (r *ManifestWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error

	logger := r.Logger.With("ManifestWork", req.NamespacedName.String())
	logger.Info("Reconciling ManifestWork")

	var manifestwork workv1.ManifestWork
	err = r.HubClient.Get(ctx, req.NamespacedName, &manifestwork)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ManifestWork not found on hub, likely deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
	}

	var drpcName string
	for i := range manifestwork.ObjectMeta.OwnerReferences {
		if manifestwork.ObjectMeta.OwnerReferences[i].APIVersion == ramenv1alpha1.GroupVersion.String() &&
			manifestwork.ObjectMeta.OwnerReferences[i].Kind == "DRPolicy" {
			drpcName = manifestwork.ObjectMeta.OwnerReferences[i].Name
		}
	}

	var drpc ramenv1alpha1.DRPolicy
	err = r.HubClient.Get(ctx, types.NamespacedName{Name: drpcName}, &drpc)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
	}

	mirrorpeer, err := utils.GetMirrorPeerForClusterSet(ctx, r.HubClient, drpc.Spec.DRClusters)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
	}

	peerRefs, err := utils.GetPeerRefForProviderCluster(ctx, r.SpokeClient, r.HubClient, mirrorpeer)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
	}

	var clusterIDs []string
	for i := range peerRefs {
		id, err := utils.GetClusterID(ctx, r.HubClient, peerRefs[i].ClusterName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
		}
		clusterIDs = append(clusterIDs, id)
	}

	templateName := manifestwork.Status.ResourceStatus.Manifests[0].ResourceMeta.Name
	templateNamespace := manifestwork.Status.ResourceStatus.Manifests[0].ResourceMeta.Namespace
	for _, id := range clusterIDs {
		var foundConsumer ocsv1alpha1.StorageConsumer
		err = r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: templateNamespace, Name: fmt.Sprintf("storageconsumer-%s", id)}, &foundConsumer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
		}

		expectedConsumer := foundConsumer.DeepCopy()
		vrc := ocsv1alpha1.VolumeReplicationClassSpec{Name: templateName}
		index := slices.Index(expectedConsumer.Spec.VolumeReplicationClasses, vrc)
		if manifestwork.DeletionTimestamp.IsZero() {
			if index == -1 {
				expectedConsumer.Spec.VolumeReplicationClasses = append(expectedConsumer.Spec.VolumeReplicationClasses, vrc)
			}
		} else {
			if index != -1 {
				expectedConsumer.Spec.VolumeReplicationClasses = slices.Delete(expectedConsumer.Spec.VolumeReplicationClasses, index, index)
			}
		}

		if !reflect.DeepEqual(foundConsumer, expectedConsumer) {
			err = r.SpokeClient.Update(ctx, expectedConsumer)
			return ctrl.Result{}, fmt.Errorf("unable to reconcile ManifestWork %q: %w", req.String(), err)
		}
	}
	return ctrl.Result{}, nil
}
