package addons

import (
	"context"
	"fmt"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
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
		isMatched := false
		for _, handler := range secretExchangeHandler.RegisteredHandlers {
			isMatched = isMatched || handler.getGreenSecretFilter(obj)
		}
		return isMatched
	}

	hubSecretInformer := hubSecretInformers.Informer()
	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(queueKeyFn, eventFilterFn, hubSecretInformer).
		WithSync(c.sync).
		ToController(fmt.Sprintf("%s-controller", setup.TokenExchangeName), recorder)
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

	for _, handler := range secretExchangeHandler.RegisteredHandlers {
		err = handler.syncGreenSecret(name, namespace, c)
		if err != nil {
			return err
		}
	}

	return nil
}
