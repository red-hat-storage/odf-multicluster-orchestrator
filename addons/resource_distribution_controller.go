package addons

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"

	templatev1 "github.com/openshift/api/template/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ResourceDistributionReconciler reconciles a ManifestWork object
type ResourceDistributionReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
	Logger           *slog.Logger
	CurrentNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceDistributionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up controller with manager")
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{
			utils.CreatedByLabelKey: utils.CreatorMulticlusterOrchestrator,
		},
	})
	if err != nil {
		return err
	}

	storageConsumerPredicate := predicate.Funcs{
		CreateFunc: func(tce event.TypedCreateEvent[client.Object]) bool {
			return true
		},
		UpdateFunc: func(tue event.TypedUpdateEvent[client.Object]) bool {
			oldConsumer, ok := tue.ObjectOld.(*ocsv1alpha1.StorageConsumer)
			if !ok {
				return false
			}
			newConsumer, ok := tue.ObjectNew.(*ocsv1alpha1.StorageConsumer)
			if !ok {
				return false
			}
			if !reflect.DeepEqual(oldConsumer.Spec.VolumeReplicationClasses, newConsumer.Spec.VolumeReplicationClasses) {
				return true
			}
			if !reflect.DeepEqual(oldConsumer.Status.Client, newConsumer.Status.Client) {
				return true
			}
			return false
		},
	}

	eventHandler := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []ctrl.Request {
		return []ctrl.Request{reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: r.CurrentNamespace,
				Name:      utils.StorageClientMappingConfigMapName,
			},
		}}
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("resource_distribution_controller").
		For(&corev1.ConfigMap{}, builder.WithPredicates(
			predicate.NewPredicateFuncs(
				func(object client.Object) bool {
					return object.GetName() == utils.StorageClientMappingConfigMapName && object.GetNamespace() == r.CurrentNamespace
				}),
		)).
		Watches(&templatev1.Template{}, eventHandler, builder.WithPredicates(labelSelectorPredicate)).
		Watches(&ocsv1alpha1.StorageConsumer{}, eventHandler, builder.WithPredicates(storageConsumerPredicate)).
		WithEventFilter(predicate.NewPredicateFuncs(
			func(object client.Object) bool {
				return object.GetNamespace() == r.CurrentNamespace
			}),
		).
		Complete(r)
}

func removeFinalizerFromObject(ctx context.Context, cl client.Client, o client.Object, finalizer string) error {
	if controllerutil.RemoveFinalizer(o, finalizer) {
		if err := cl.Update(ctx, o); err != nil {
			return err
		}
	}
	return nil
}

func addFinalizerToObject(ctx context.Context, cl client.Client, o client.Object, finalizer string) error {
	if controllerutil.AddFinalizer(o, finalizer) {
		if err := cl.Update(ctx, o); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResourceDistributionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("Request", req)
	logger.Info("Distributing resources to all StorageConsumers.")

	var err error

	var addonDeletionlock corev1.ConfigMap
	err = r.SpokeClient.Get(ctx, types.NamespacedName{Namespace: r.CurrentNamespace, Name: AddonDeletionlockName}, &addonDeletionlock)
	if err != nil {
		return ctrl.Result{}, err
	}

	var storageClientMapping *corev1.ConfigMap
	storageClientMapping, err = utils.GetStorageClientMapping(ctx, r.SpokeClient, r.CurrentNamespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to distribute resources to StorageConsumers: %w", err)
	}

	addVRCToConsumers := []string{}
	storageConsumerList := ocsv1alpha1.StorageConsumerList{}
	templateList := templatev1.TemplateList{}

	err = r.SpokeClient.List(ctx, &storageConsumerList, client.InNamespace(r.CurrentNamespace))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to distribute resources to StorageConsumers: %w", err)
	}
	if len(storageConsumerList.Items) == 0 {
		logger.Info("No StorageConsumer to add/remove resources.")
		addonDeletionlock.Data = nil
		if err = r.SpokeClient.Update(ctx, &addonDeletionlock); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	err = r.SpokeClient.List(ctx, &templateList, client.InNamespace(r.CurrentNamespace), client.MatchingLabels{
		utils.CreatedByLabelKey: utils.CreatorMulticlusterOrchestrator,
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to distribute resources to StorageConsumers: %w", err)
	}
	if len(templateList.Items) == 0 {
		logger.Info("No resources to distribute to StorageConsumers.")
		addonDeletionlock.Data = nil
		if err = r.SpokeClient.Update(ctx, &addonDeletionlock); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if addonDeletionlock.Data == nil {
		addonDeletionlock.Data = make(map[string]string)
	}

	var manifestWorkList workv1.ManifestWorkList
	listOpts := &client.ListOptions{
		Namespace: r.SpokeClusterName,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			utils.CreatedByLabelKey: utils.CreatorMulticlusterOrchestrator,
		}),
	}
	if err = r.HubClient.List(ctx, &manifestWorkList, listOpts); err != nil {
		return ctrl.Result{}, err
	}

	tmpAddonDeletionLockData := addonDeletionlock.Data
	addonDeletionlock.Data = make(map[string]string)
	for _, mw := range manifestWorkList.Items {
		if mw.DeletionTimestamp.IsZero() {
			cId := mw.Labels[utils.CreatedForClientID]
			if _, ok := addonDeletionlock.Data[cId]; !ok {
				addonDeletionlock.Data[cId] = ""
			}
		}
	}

	if !reflect.DeepEqual(tmpAddonDeletionLockData, addonDeletionlock.Data) {
		logger.Info("Updating addonDeletionLock cm with clientIDs to which resources are distributed")
		if err = r.SpokeClient.Update(ctx, &addonDeletionlock); err != nil {
			return ctrl.Result{}, err
		}
	}

	for k := range storageClientMapping.Data {
		for consumerIndex := range storageConsumerList.Items {
			if storageConsumerList.Items[consumerIndex].Status.Client != nil && storageConsumerList.Items[consumerIndex].Status.Client.ID == k {
				addVRCToConsumers = append(addVRCToConsumers, storageConsumerList.Items[consumerIndex].GetName())
			}
		}
	}

	for _, template := range templateList.Items {
		logger.Info("Distributing template", "Template", template.GetName())
		if template.DeletionTimestamp.IsZero() {
			err = addFinalizerToObject(ctx, r.SpokeClient, &template, ResourceDistributionFinalizer)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		for _, foundConsumer := range storageConsumerList.Items {
			expectedConsumer := foundConsumer.DeepCopy()

			if template.GetLabels()[utils.ObjectKindLabelKey] == "VolumeReplicationClass" {
				vrc := ocsv1alpha1.VolumeReplicationClassSpec{
					CommonClassSpec: ocsv1alpha1.CommonClassSpec{
						Name: template.Name,
					},
				}
				index := slices.IndexFunc(expectedConsumer.Spec.VolumeReplicationClasses, func(expectedVRC ocsv1alpha1.VolumeReplicationClassSpec) bool {
					return expectedVRC.Name == vrc.CommonClassSpec.Name
				})
				if template.DeletionTimestamp.IsZero() {
					if slices.Contains(addVRCToConsumers, foundConsumer.GetName()) {
						logger.Info("Adding VRC to StorageConsumer", "VRC", vrc, "StorageConsumer", foundConsumer.GetName())
						if index == -1 {
							expectedConsumer.Spec.VolumeReplicationClasses = append(expectedConsumer.Spec.VolumeReplicationClasses, vrc)
						}
					} else {
						logger.Info("StorageConsumer does not need this VRC. Removing VRC from StorageConsumer", "VRC", vrc, "StorageConsumer", foundConsumer.GetName())
						if index != -1 {
							expectedConsumer.Spec.VolumeReplicationClasses = slices.Delete(expectedConsumer.Spec.VolumeReplicationClasses, index, index+1)
						}
					}
				} else {
					logger.Info("Template is being deleted. Removing VRC from StorageConsumer", "VRC", vrc, "StorageConsumer", foundConsumer.GetName())
					if index != -1 {
						expectedConsumer.Spec.VolumeReplicationClasses = slices.Delete(expectedConsumer.Spec.VolumeReplicationClasses, index, index+1)
					}
				}
				if !reflect.DeepEqual(expectedConsumer.Spec.VolumeReplicationClasses, foundConsumer.Spec.VolumeReplicationClasses) {
					err := r.SpokeClient.Update(ctx, expectedConsumer)
					if err != nil {
						return ctrl.Result{}, fmt.Errorf("unable to add VolumeReplicationClasses to StorageConsumer %q: %w", expectedConsumer.Name, err)
					}
				}
				logger.Info("VolumeReplicationClass was updated on StorageConsumers.", "StorageConsumer", expectedConsumer.GetName())
			}

			if utils.EnableCG && template.GetLabels()[utils.ObjectKindLabelKey] == "VolumeGroupReplicationClass" {
				vgrc := ocsv1alpha1.VolumeGroupReplicationClassSpec{
					CommonClassSpec: ocsv1alpha1.CommonClassSpec{
						Name: template.Name,
					},
				}
				index := slices.IndexFunc(expectedConsumer.Spec.VolumeGroupReplicationClasses, func(expectedVRC ocsv1alpha1.VolumeGroupReplicationClassSpec) bool {
					return expectedVRC.Name == vgrc.CommonClassSpec.Name
				})
				if template.DeletionTimestamp.IsZero() {
					if slices.Contains(addVRCToConsumers, foundConsumer.GetName()) {
						logger.Info("Adding VGRC to StorageConsumer", "VGRC", vgrc, "StorageConsumer", foundConsumer.GetName())
						if index == -1 {
							expectedConsumer.Spec.VolumeGroupReplicationClasses = append(expectedConsumer.Spec.VolumeGroupReplicationClasses, vgrc)
						}
					} else {
						logger.Info("StorageConsumer does not need this VGRC. Removing VGRC from StorageConsumer", "VGRC", vgrc, "StorageConsumer", foundConsumer.GetName())
						if index != -1 {
							expectedConsumer.Spec.VolumeGroupReplicationClasses = slices.Delete(expectedConsumer.Spec.VolumeGroupReplicationClasses, index, index+1)
						}
					}
				} else {
					logger.Info("Template is being deleted. Removing VGRC from StorageConsumer", "VGRC", vgrc, "StorageConsumer", foundConsumer.GetName())
					if index != -1 {
						expectedConsumer.Spec.VolumeGroupReplicationClasses = slices.Delete(expectedConsumer.Spec.VolumeGroupReplicationClasses, index, index+1)
					}
				}
				if !reflect.DeepEqual(expectedConsumer.Spec.VolumeGroupReplicationClasses, foundConsumer.Spec.VolumeGroupReplicationClasses) {
					err := r.SpokeClient.Update(ctx, expectedConsumer)
					if err != nil {
						return ctrl.Result{}, fmt.Errorf("unable to add VolumeGroupReplicationClasses to StorageConsumer %q: %w", expectedConsumer.Name, err)
					}
				}
				logger.Info("VolumeGroupReplicationClass was updated on StorageConsumers.", "StorageConsumer", expectedConsumer.GetName())
			}
		}
		if !template.DeletionTimestamp.IsZero() {
			err = removeFinalizerFromObject(ctx, r.SpokeClient, &template, ResourceDistributionFinalizer)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	logger.Info("Successfully distributed resources to all StorageConsumers.")
	return ctrl.Result{}, nil
}
