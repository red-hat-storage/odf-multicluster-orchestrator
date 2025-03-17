package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

type ManagedClusterViewReconciler struct {
	Client           client.Client
	Logger           *slog.Logger
	testEnvFile      string
	CurrentNamespace string
}

const (
	ODFInfoConfigMapName    = "odf-info"
	ConfigMapResourceType   = "ConfigMap"
	ClientInfoConfigMapName = "odf-client-info"
)

func (r *ManagedClusterViewReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Logger.Info("Setting up ManagedClusterViewReconciler with manager")
	managedClusterViewPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			obj, ok := e.ObjectNew.(*viewv1beta1.ManagedClusterView)
			if !ok {
				return false
			}
			return hasODFInfoInScope(obj)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			obj, ok := e.Object.(*viewv1beta1.ManagedClusterView)
			if !ok {
				return false
			}
			return hasODFInfoInScope(obj)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&viewv1beta1.ManagedClusterView{}, builder.WithPredicates(managedClusterViewPredicate, predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

func hasODFInfoInScope(mc *viewv1beta1.ManagedClusterView) bool {
	if mc.Spec.Scope.Name == utils.ODFInfoConfigMapName && mc.Spec.Scope.Resource == ConfigMapResourceType {
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

	if err := createOrUpdateConfigMap(ctx, r.Client, r.CurrentNamespace, managedClusterView, r.Logger); err != nil {
		logger.Error("Failed to create or update ConfigMap for ManagedClusterView", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled ManagedClusterView")

	return ctrl.Result{}, nil
}

func createOrUpdateConfigMap(ctx context.Context, c client.Client, operatorNamespace string, managedClusterView viewv1beta1.ManagedClusterView, logger *slog.Logger) error {
	logger = logger.With("ManagedClusterView", managedClusterView.Name, "Namespace", managedClusterView.Namespace)

	// Initialize an empty map to hold the result data.
	var resultData map[string]interface{}
	err := json.Unmarshal(managedClusterView.Status.Result.Raw, &resultData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal result data. %w", err)
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
		err := yaml.Unmarshal([]byte(yamlContent), &odfInfo)
		if err != nil {
			return fmt.Errorf("failed to unmarshal ODF info data for key %s: %w", key, err)
		}

		providerPublicEndpoint := odfInfo.StorageCluster.Annotations[ocsv1alpha1.ApiServerExportedAddressAnnotationName]
		if providerPublicEndpoint == "" {
			logger.Info("StorageProviderPublicEndpoint is not available.")
		}
		providerInfo := utils.ProviderInfo{
			Version:                       odfInfo.Version,
			DeploymentType:                odfInfo.DeploymentType,
			CephClusterFSID:               odfInfo.StorageCluster.CephClusterFSID,
			StorageProviderEndpoint:       odfInfo.StorageCluster.StorageProviderEndpoint,
			NamespacedName:                odfInfo.StorageCluster.NamespacedName,
			StorageSystemName:             odfInfo.StorageSystemName,
			ProviderManagedClusterName:    managedClusterView.Namespace,
			StorageProviderPublicEndpoint: providerPublicEndpoint,
		}

		if len(odfInfo.Clients) == 0 {
			clientInfo := utils.ClientInfo{
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
				managedCluster, err := utils.GetManagedClusterById(ctx, c, client.ClusterID)
				if err != nil {
					return err
				}
				clientInfo := utils.ClientInfo{
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
			Name:      ClientInfoConfigMapName,
			Namespace: operatorNamespace,
		},
	}
	err = c.Get(ctx, types.NamespacedName{Name: ClientInfoConfigMapName, Namespace: operatorNamespace}, configMap)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ConfigMap. %w", err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	op, err := controllerutil.CreateOrUpdate(ctx, c, configMap, func() error {

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}

		for clientKey, clientInfo := range clientInfoMap {
			configMap.Data[clientKey] = clientInfo
		}

		mcvOwnerRefs := managedClusterView.GetOwnerReferences()
		for _, mcvOwnerRef := range mcvOwnerRefs {
			exists := false
			for _, existingOwnerRef := range configMap.OwnerReferences {
				if existingOwnerRef.UID == mcvOwnerRef.UID {
					exists = true
					break
				}
			}
			if !exists {
				falseValue := false
				mcvOwnerRef.Controller = &falseValue
				configMap.OwnerReferences = append(configMap.OwnerReferences, mcvOwnerRef)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap. %w", err)
	}

	logger.Info(fmt.Sprintf("ConfigMap %s in namespace %s has been %s", ClientInfoConfigMapName, operatorNamespace, op))

	return nil
}
