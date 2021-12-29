package addons

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	handlers "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange/handlers"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type blueSecretTokenExchangeAgentController struct {
	hubKubeClient        kubernetes.Interface
	hubSecretLister      corev1lister.SecretLister
	spokeKubeClient      kubernetes.Interface
	spokeSecretLister    corev1lister.SecretLister
	spokeConfigMapLister corev1lister.ConfigMapLister
	clusterName          string
	agentNamespace       string
	recorder             events.Recorder
	spokeKubeConfig      *rest.Config
}

func newblueSecretTokenExchangeAgentController(
	hubKubeClient kubernetes.Interface,
	hubSecretInformers corev1informers.SecretInformer,
	spokeKubeClient kubernetes.Interface,
	spokeSecretInformers corev1informers.SecretInformer,
	clusterName string,
	namespace string,
	recorder events.Recorder,
	spokeKubeConfig *rest.Config,
) factory.Controller {
	c := &blueSecretTokenExchangeAgentController{
		hubKubeClient:     hubKubeClient,
		hubSecretLister:   hubSecretInformers.Lister(),
		spokeKubeClient:   spokeKubeClient,
		spokeSecretLister: spokeSecretInformers.Lister(),
		clusterName:       clusterName,
		agentNamespace:    namespace,
		recorder:          recorder,
		spokeKubeConfig:   spokeKubeConfig,
	}
	klog.Infof("creating managed cluster to hub secret sync controller")

	queueKeyFn := func(obj runtime.Object) string {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			return ""
		}
		return key
	}

	eventFilterFn := func(obj interface{}) bool {
		isMatched := false
		for _, handlerName := range handlers.GetWhitelistedHandlers() {
			handler, err := handlers.GetSecretHandler(handlerName, c.spokeKubeConfig, c.spokeSecretLister, c.spokeConfigMapLister)
			if handler != nil && err == nil {
				isMatched = isMatched || handler.GetObjectFilter(obj)
			}
		}
		return isMatched
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(queueKeyFn, eventFilterFn, spokeSecretInformers.Informer()).
		WithSync(c.sync).
		ToController(fmt.Sprintf("managedcluster-secret-%s-controller", TokenExchangeName), recorder)
}

// sync is the main reconcile function that syncs secret from the managed cluster to the hub cluster
func (c *blueSecretTokenExchangeAgentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.Infof("reconciling addon deploy %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore secret whose key is not in format: namespace/name
		return nil
	}

	for _, handlerName := range handlers.GetWhitelistedHandlers() {
		handler, err := handlers.GetSecretHandler(handlerName, c.spokeKubeConfig, c.spokeSecretLister, c.spokeConfigMapLister)
		if err != nil {
			return err
		}
		newSecret, err := handler.GenerateBlueSecret(name, namespace, c.clusterName, c.agentNamespace)
		if newSecret == nil && err == nil {
			// skip hanler which secret filter is not matched
			continue
		}
		if err != nil {
			return err
		}

		err = createSecret(c.hubKubeClient, c.recorder, newSecret)
		if err != nil {
			return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", name, namespace, c.clusterName, err)
		}
	}

	klog.Infof("successfully synced managed cluster secret %q from namespace %v to the hub cluster in namespace %q", name, namespace, c.clusterName)

	return nil
}
