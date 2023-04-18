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

type blueSecretTokenExchangeAgentController struct {
	hubKubeClient        kubernetes.Interface
	hubSecretLister      corev1lister.SecretLister
	spokeKubeClient      kubernetes.Interface
	spokeSecretLister    corev1lister.SecretLister
	spokeConfigMapLister corev1lister.ConfigMapLister
	clusterName          string
	recorder             events.Recorder
	spokeKubeConfig      *rest.Config
}

func newblueSecretTokenExchangeAgentController(
	hubKubeClient kubernetes.Interface,
	hubSecretInformers corev1informers.SecretInformer,
	spokeKubeClient kubernetes.Interface,
	spokeSecretInformers corev1informers.SecretInformer,
	spokeConfigMapInformers corev1informers.ConfigMapInformer,
	clusterName string,
	recorder events.Recorder,
	spokeKubeConfig *rest.Config,
) factory.Controller {
	c := &blueSecretTokenExchangeAgentController{
		hubKubeClient:        hubKubeClient,
		hubSecretLister:      hubSecretInformers.Lister(),
		spokeKubeClient:      spokeKubeClient,
		spokeSecretLister:    spokeSecretInformers.Lister(),
		spokeConfigMapLister: spokeConfigMapInformers.Lister(),
		clusterName:          clusterName,
		recorder:             recorder,
		spokeKubeConfig:      spokeKubeConfig,
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
		for _, handler := range secretExchangeHandler.RegisteredHandlers {
			isMatched = handler.getBlueSecretFilter(obj)
		}
		return isMatched
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(queueKeyFn, eventFilterFn, spokeSecretInformers.Informer(), spokeConfigMapInformers.Informer()).
		WithSync(c.sync).
		ToController(fmt.Sprintf("managedcluster-secret-%s-controller", setup.TokenExchangeName), recorder)
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

	for _, handler := range secretExchangeHandler.RegisteredHandlers {
		err = handler.syncBlueSecret(name, namespace, c)
		if err != nil {
			return err
		}
	}

	return nil
}
