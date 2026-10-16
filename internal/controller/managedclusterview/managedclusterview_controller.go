package managedclusterview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/ibmstoragescale"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	yamlv3 "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ManagedClusterViewReconciler struct {
	Client           client.Client
	Logger           *slog.Logger
	Scheme           *runtime.Scheme
	TestEnvFile      string
	CurrentNamespace string
}

const (
	configMapResourceType = "ConfigMap"
)

// +kubebuilder:rbac:groups=view.open-cluster-management.io,resources=managedclusterviews,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update

func (r *ManagedClusterViewReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up ManagedClusterViewReconciler with manager")
	managedClusterViewPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			obj, ok := e.ObjectNew.(*viewv1beta1.ManagedClusterView)
			if !ok {
				return false
			}
			return isStorageVendorConfigMapMCV(obj)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*viewv1beta1.ManagedClusterView)
			if !ok {
				return false
			}
			return isStorageVendorConfigMapMCV(obj)
		},
	}

	// Predicate to watch only our storage vendor ConfigMaps
	configMapPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isStorageVendorConfigMap(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isStorageVendorConfigMap(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isStorageVendorConfigMap(e.Object)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&viewv1beta1.ManagedClusterView{}, builder.WithPredicates(managedClusterViewPredicate, predicate.ResourceVersionChangedPredicate{})).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.configMapToManagedClusterViews), builder.WithPredicates(configMapPredicate)).
		Complete(r)
}

// configMapToManagedClusterViews maps a ConfigMap event to reconcile requests for all relevant ManagedClusterViews
func (r *ManagedClusterViewReconciler) configMapToManagedClusterViews(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}

	// Determine which vendor's ConfigMap changed
	var mcvPrefix string
	switch cm.Name {
	case odf.ClientInfoConfigMapName:
		mcvPrefix = utils.ODFMCVNameTemplatePrefix
	case ibmstoragescale.IbmStorageScaleConfigMapName:
		mcvPrefix = utils.IbmScaleMCVNameTemplatePrefix
	default:
		return nil
	}

	// List all ManagedClusterViews
	mcvList := &viewv1beta1.ManagedClusterViewList{}
	if err := r.Client.List(ctx, mcvList); err != nil {
		r.Logger.Error("Failed to list ManagedClusterViews", "error", err)
		return nil
	}

	var requests []reconcile.Request
	for _, mcv := range mcvList.Items {
		// Only enqueue MCVs for the specific vendor whose ConfigMap changed
		if strings.HasPrefix(mcv.Name, mcvPrefix) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      mcv.Name,
					Namespace: mcv.Namespace,
				},
			})
		}
	}

	return requests
}

func isStorageVendorConfigMap(obj client.Object) bool {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return false
	}
	// Watch our specific ConfigMaps
	return cm.Name == odf.ClientInfoConfigMapName || cm.Name == ibmstoragescale.IbmStorageScaleConfigMapName
}

func isStorageVendorConfigMapMCV(mcv *viewv1beta1.ManagedClusterView) bool {
	// Only watch ConfigMap MCVs
	if mcv.Spec.Scope.Resource != configMapResourceType {
		return false
	}

	// Check if it's an ODF MCV
	if strings.HasPrefix(mcv.Name, utils.ODFMCVNameTemplatePrefix) {
		return true
	}

	// Check if it's a IBM Storage Scale MCV
	if strings.HasPrefix(mcv.Name, utils.IbmScaleMCVNameTemplatePrefix) {
		return true
	}

	return false
}

func (r *ManagedClusterViewReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.Logger.With("ManagedClusterView", req.NamespacedName)
	logger.Info("Reconciling ManagedClusterView")

	var managedClusterView viewv1beta1.ManagedClusterView
	if err := r.Client.Get(ctx, req.NamespacedName, &managedClusterView); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error("Failed to get ManagedClusterView", "error", err)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var err error
	// Determine which storage vendor this MCV is for and process accordingly
	switch {
	case strings.HasPrefix(managedClusterView.Name, utils.ODFMCVNameTemplatePrefix):
		err = r.createOrUpdateODFConfigMap(ctx, managedClusterView)
	case strings.HasPrefix(managedClusterView.Name, utils.IbmScaleMCVNameTemplatePrefix):
		err = r.createOrUpdateFAConfigMap(ctx, managedClusterView)
	default:
		logger.Info("Skipping ManagedClusterView - not a recognized storage vendor MCV")
		return ctrl.Result{}, nil
	}

	if err != nil {
		logger.Error("Failed to create or update ConfigMap for ManagedClusterView", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled ManagedClusterView")

	return ctrl.Result{}, nil
}

func (r *ManagedClusterViewReconciler) createOrUpdateODFConfigMap(ctx context.Context, managedClusterView viewv1beta1.ManagedClusterView) error {
	// Get the ManagedCluster that owns this ManagedClusterView
	// The MCV namespace is the same as the ManagedCluster name
	managedCluster := &clusterv1.ManagedCluster{}
	managedCluster.Name = managedClusterView.Namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(managedCluster), managedCluster); err != nil {
		return fmt.Errorf("failed to get ManagedCluster %s: %w", managedClusterView.Namespace, err)
	}

	// Initialize an empty map to hold the result data.
	var resultData map[string]interface{}
	err := json.Unmarshal(managedClusterView.Status.Result.Raw, &resultData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal result data: %w", err)
	}

	data, ok := resultData["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected data format in result: %v", resultData["data"])
	}

	clientInfoMap := make(map[string]string)

	for key, value := range data {
		if !strings.Contains(key, ".yaml") {
			continue
		}

		yamlContent, ok := value.(string)
		if !ok {
			return fmt.Errorf("unexpected value format in data for key %s: expected string, got %T", key, value)
		}
		var odfInfo ocsv1alpha1.OdfInfoData
		err := yamlv3.Unmarshal([]byte(yamlContent), &odfInfo)
		if err != nil {
			return fmt.Errorf("failed to unmarshal DF info data for key %s: %w", key, err)
		}

		providerPublicEndpoint := odfInfo.StorageCluster.Annotations[ocsv1alpha1.ApiServerExportedAddressAnnotationName]
		if providerPublicEndpoint == "" {
			r.Logger.Info("StorageProviderPublicEndpoint is not available.")
		}
		cephblockPoolsInfo := []odf.InfoCephBlockPool{}
		for _, cephblockpool := range odfInfo.StorageCluster.InfoCephBlockPools {
			cephblockPoolsInfo = append(cephblockPoolsInfo, odf.InfoCephBlockPool{
				Name:          cephblockpool.Name,
				MirrorEnabled: cephblockpool.MirrorEnabled,
			})
		}
		providerInfo := odf.ProviderInfo{
			Version:                       odfInfo.Version,
			DeploymentType:                odfInfo.DeploymentType,
			CephClusterFSID:               odfInfo.StorageCluster.CephClusterFSID,
			StorageProviderEndpoint:       odfInfo.StorageCluster.StorageProviderEndpoint,
			NamespacedName:                odfInfo.StorageCluster.NamespacedName,
			ProviderManagedClusterName:    managedClusterView.Namespace,
			StorageProviderPublicEndpoint: providerPublicEndpoint,
		}

		if len(cephblockPoolsInfo) > 0 {
			providerInfo.InfoCephBlockPools = cephblockPoolsInfo
		}

		if len(odfInfo.Clients) == 0 {
			clientInfo := odf.ClientInfo{
				ClusterID:                "",
				Name:                     "",
				ProviderInfo:             providerInfo,
				ClientManagedClusterName: "",
				ClientID:                 "",
			}
			clientInfoJSON, err := json.Marshal(clientInfo)
			if err != nil {
				return fmt.Errorf("failed to marshal client info for key %s: %w", key, err)
			}

			clientInfoMap[utils.GetKey(managedClusterView.Namespace, odfInfo.StorageCluster.NamespacedName.Name)] = string(clientInfoJSON)
		} else {
			for _, client := range odfInfo.Clients {
				managedCluster, err := utils.GetManagedClusterById(ctx, r.Client, client.ClusterID)
				if err != nil {
					return err
				}
				clientInfo := odf.ClientInfo{
					ClusterID:                client.ClusterID,
					Name:                     client.Name,
					ProviderInfo:             providerInfo,
					ClientManagedClusterName: managedCluster.Name,
					ClientID:                 client.ClientID,
				}
				clientInfoJSON, err := json.Marshal(clientInfo)
				if err != nil {
					return fmt.Errorf("failed to marshal client info for key %s: %w", key, err)
				}

				clientInfoMap[utils.GetKey(managedCluster.Name, client.Name)] = string(clientInfoJSON)
			}
		}
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odf.ClientInfoConfigMapName,
			Namespace: r.CurrentNamespace,
		},
	}
	err = r.Client.Get(ctx, types.NamespacedName{Name: odf.ClientInfoConfigMapName, Namespace: r.CurrentNamespace}, configMap)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {

		if configMap.Labels == nil {
			configMap.Labels = make(map[string]string)
		}
		configMap.Labels[utils.HubRecoveryLabel] = ""

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}

		for clientKey, clientInfo := range clientInfoMap {
			configMap.Data[clientKey] = clientInfo
		}

		// Add ManagedCluster as a non-controller owner for garbage collection
		// Note: Cannot add MCV as owner due to cross-namespace owner reference restrictions
		if err := controllerutil.SetOwnerReference(managedCluster, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set ManagedCluster owner reference: %w", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	r.Logger.Info(fmt.Sprintf("ConfigMap %s in namespace %s has been %s", odf.ClientInfoConfigMapName, r.CurrentNamespace, op))

	return nil
}

func (r *ManagedClusterViewReconciler) createOrUpdateFAConfigMap(ctx context.Context, managedClusterView viewv1beta1.ManagedClusterView) error {
	// The MCV namespace is the same as the ManagedCluster name
	managedCluster := &clusterv1.ManagedCluster{}
	managedCluster.Name = managedClusterView.Namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(managedCluster), managedCluster); err != nil {
		return fmt.Errorf("failed to get ManagedCluster %s: %w", managedClusterView.Namespace, err)
	}

	// Initialize an empty map to hold the result data.
	var resultData map[string]interface{}
	if err := json.Unmarshal(managedClusterView.Status.Result.Raw, &resultData); err != nil {
		return fmt.Errorf("failed to unmarshal result data: %w", err)
	}

	data, ok := resultData["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected data format in result: %v", resultData["data"])
	}

	scaleInfoMap := make(map[string]string)

	for key, value := range data {
		if !strings.Contains(key, ".yaml") {
			continue
		}

		yamlContent, ok := value.(string)
		if !ok {
			return fmt.Errorf("unexpected value format in data for key %s: expected string, got %T", key, value)
		}

		var scaleInfo ibmstoragescale.IbmStorageScaleInfo
		err := yamlv3.Unmarshal([]byte(yamlContent), &scaleInfo)
		if err != nil {
			return fmt.Errorf("failed to unmarshal FA replication info for key %s: %w", key, err)
		}

		// Marshal the FA info as JSON to store in the hub ConfigMap
		faInfoJSON, err := json.Marshal(scaleInfo)
		if err != nil {
			return fmt.Errorf("failed to marshal FA info for key %s: %w", key, err)
		}

		// Use consistent key format: managedclustername-scaleClusterName
		configMapKey := utils.GetKey(managedClusterView.Namespace, scaleInfo.Cluster.Name)
		scaleInfoMap[configMapKey] = string(faInfoJSON)
	}

	configMap := &corev1.ConfigMap{}
	configMap.Name = ibmstoragescale.IbmStorageScaleConfigMapName
	configMap.Namespace = r.CurrentNamespace

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {

		utils.AddLabel(configMap, utils.HubRecoveryLabel, "")

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}

		for clusterName, faInfo := range scaleInfoMap {
			configMap.Data[clusterName] = faInfo
		}

		// Add ManagedCluster as a non-controller owner for garbage collection
		// Note: Cannot add MCV as owner due to cross-namespace owner reference restrictions
		if err := controllerutil.SetOwnerReference(managedCluster, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set ManagedCluster owner reference: %w", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	r.Logger.Info(fmt.Sprintf("ConfigMap %s in namespace %s has been %s", ibmstoragescale.IbmStorageScaleConfigMapName, r.CurrentNamespace, op))

	return nil
}
