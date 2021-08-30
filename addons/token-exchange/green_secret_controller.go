package addons

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type greenSecretTokenExchangeAgentController struct {
	hubKubeClient     kubernetes.Interface
	hubSecretLister   corev1lister.SecretLister
	spokeKubeClient   kubernetes.Interface
	spokeSecretLister corev1lister.SecretLister
	clusterName       string
	recorder          events.Recorder
}

func newgreenSecretTokenExchangeAgentController(
	hubKubeClient kubernetes.Interface,
	hubSecretInformers corev1informers.SecretInformer,
	spokeKubeClient kubernetes.Interface,
	spokeSecretInformers corev1informers.SecretInformer,
	clusterName string,
	recorder events.Recorder,
) factory.Controller {
	c := &greenSecretTokenExchangeAgentController{
		hubKubeClient:     hubKubeClient,
		hubSecretLister:   hubSecretInformers.Lister(),
		spokeKubeClient:   spokeKubeClient,
		spokeSecretLister: spokeSecretInformers.Lister(),
		clusterName:       clusterName,
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

	if secret.GetLabels()[common.SecretLabelTypeKey] != string(common.DestinationLabel) {
		klog.Infof("secret %q in namespace %q is not a green secret. Skip syncing with the spoke cluster", secret.Name, namespace)
		return nil
	}

	toNamespace := string(secret.Data["namespace"])
	if toNamespace == "" {
		return fmt.Errorf("missing storageCluster namespace info in secret %q in namespace %q", secret.Name, namespace)
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: toNamespace,
			Labels:    map[string]string{CreatedByLabelKey: CreatedByLabelValue},
		},
		Data: map[string][]byte{"secret-data": secret.Data[string(common.SecretDataKey)]},
	}

	// Create secrete on spoke cluster
	err = createSecret(c.spokeKubeClient, c.recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync hub secret %q in managed cluster in namespace %q. %v", newSecret.Name, toNamespace, err)
	}

	klog.Infof("successfully synced hub secret %q in managed clustr in namespace %q", newSecret.Name, toNamespace)

	return nil
}
