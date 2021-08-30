package addons

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type greenSecretTokenExchangeAgentController struct {
	hubKubeClient     kubernetes.Interface
	hubSecretLister   corev1lister.SecretLister
	spokeKubeClient   kubernetes.Interface
	spokeSecretLister corev1lister.SecretLister
	clusterName       string
	spokeKubeConfig   *rest.Config
	recorder          events.Recorder
}

func newgreenSecretTokenExchangeAgentController(
	hubKubeClient kubernetes.Interface,
	hubSecretInformers corev1informers.SecretInformer,
	spokeKubeClient kubernetes.Interface,
	spokeSecretInformers corev1informers.SecretInformer,
	clusterName string,
	spokeKubeConfig *rest.Config,
	recorder events.Recorder,
) factory.Controller {
	c := &greenSecretTokenExchangeAgentController{
		hubKubeClient:     hubKubeClient,
		hubSecretLister:   hubSecretInformers.Lister(),
		spokeKubeClient:   spokeKubeClient,
		spokeSecretLister: spokeSecretInformers.Lister(),
		clusterName:       clusterName,
		spokeKubeConfig:   spokeKubeConfig,
		recorder:          recorder,
	}
	queueKeyFn := func(obj runtime.Object) string {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			return ""
		}
		return key
	}

	eventFilterFn := func(obj interface{}) bool {
		metaObj, has := obj.(metav1.Object)
		if !has {
			return false
		}
		return metaObj.GetLabels()[common.SecretLabelTypeKey] == string(common.DestinationLabel)
	}

	hubSecretInformer := hubSecretInformers.Informer()
	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(queueKeyFn, eventFilterFn, hubSecretInformer).
		WithSync(c.sync).
		ToController(fmt.Sprintf("%s-controller", TokenExchangeName), recorder)
}

// sync secrets with label `multicluster.odf.openshift.io/secret-type: GREEN` from hub to managed cluster
func (c *greenSecretTokenExchangeAgentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore secret whose key is not in format: namespace/name
		return nil
	}

	secret, err := getSecret(c.hubSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the sercet %q in namespace %q in hub. Error %v", name, namespace, err)
	}

	if err := validateSecret(*secret); err != nil {
		return fmt.Errorf("failed to validate secret %q", secret.Name)
	}

	data := make(map[string][]byte)
	err = json.Unmarshal(secret.Data[common.SecretDataKey], &data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal secret data for the secret %q in namespace %q. %v", secret.Name, secret.Namespace, err)
	}

	toNamespace := string(secret.Data["namespace"])
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: toNamespace,
			Labels:    map[string]string{CreatedByLabelKey: CreatedByLabelValue},
		},
		Data: data,
	}

	// Create secrete on spoke cluster
	err = createSecret(c.spokeKubeClient, c.recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync hub secret %q in managed cluster in namespace %q. %v", newSecret.Name, toNamespace, err)
	}

	klog.Infof("successfully synced hub secret %q in managed clustr in namespace %q", newSecret.Name, toNamespace)

	storageClusterName := string(secret.Data[common.StorageClusterNameKey])
	err = c.updateStorageCluster(newSecret.Name, storageClusterName, toNamespace)
	if err != nil {
		return fmt.Errorf("failed to update secret name %q in the storageCluster %q in namespace %q. %v", newSecret.Name, storageClusterName, toNamespace, err)
	}

	return nil
}

func (c *greenSecretTokenExchangeAgentController) updateStorageCluster(secretName, storageClusterName, storageClusterNamespace string) error {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := ocsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to ocsv1 scheme to runtime scheme: %v", err)
	}

	cl, err := client.New(config.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	sc := &ocsv1.StorageCluster{}
	err = cl.Get(ctx, types.NamespacedName{Name: storageClusterName, Namespace: storageClusterNamespace}, sc)
	if err != nil {
		return fmt.Errorf("failed to get storage cluster %q in namespace %q. %v", storageClusterName, storageClusterNamespace, err)
	}

	// Update secret name
	if !contains(sc.Spec.Mirroring.PeerSecretNames, secretName) {
		sc.Spec.Mirroring.PeerSecretNames = append(sc.Spec.Mirroring.PeerSecretNames, secretName)
		err := cl.Update(ctx, sc)
		if err != nil {
			return fmt.Errorf("failed to update storagecluster %q in the namespace %q. %v", storageClusterName, storageClusterNamespace, err)
		}
	}

	return nil
}

func validateSecret(secret corev1.Secret) error {
	if secret.GetLabels()[common.SecretLabelTypeKey] != string(common.DestinationLabel) {
		return fmt.Errorf("secret %q in namespace %q is not a green secret. Skip syncing with the spoke cluster", secret.Name, secret.Namespace)
	}

	if secret.Data == nil {
		return fmt.Errorf("secret data not found for the secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data["namespace"]) == "" {
		return fmt.Errorf("missing storageCluster namespace info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[common.StorageClusterNameKey]) == "" {
		return fmt.Errorf("missing storageCluster name info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	if string(secret.Data[common.SecretDataKey]) == "" {
		return fmt.Errorf("missing secret-data info in secret %q in namespace %q", secret.Name, secret.Namespace)
	}

	return nil
}

// contains checks if an item exists in a given list.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
