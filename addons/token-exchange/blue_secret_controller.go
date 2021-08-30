package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	RookType                     = "kubernetes.io/rook"
	DefaultBlueSecretMatchString = "cluster-peer-token"
)

var (
	blueSecretMatchString string
)

type blueSecretTokenExchangeAgentController struct {
	hubKubeClient     kubernetes.Interface
	hubSecretLister   corev1lister.SecretLister
	spokeKubeClient   kubernetes.Interface
	spokeSecretLister corev1lister.SecretLister
	clusterName       string
	recorder          events.Recorder
	spokeKubeConfig   *rest.Config
}

func newblueSecretTokenExchangeAgentController(
	hubKubeClient kubernetes.Interface,
	hubSecretInformers corev1informers.SecretInformer,
	spokeKubeClient kubernetes.Interface,
	spokeSecretInformers corev1informers.SecretInformer,
	clusterName string,
	recorder events.Recorder,
	spokeKubeConfig *rest.Config,
) factory.Controller {
	c := &blueSecretTokenExchangeAgentController{
		hubKubeClient:     hubKubeClient,
		hubSecretLister:   hubSecretInformers.Lister(),
		spokeKubeClient:   spokeKubeClient,
		spokeSecretLister: spokeSecretInformers.Lister(),
		clusterName:       clusterName,
		recorder:          recorder,
		spokeKubeConfig:   spokeKubeConfig,
	}
	klog.Infof("creating managed cluster to hub secret sync controller")

	blueSecretMatchString = os.Getenv("TOKEN_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
	if blueSecretMatchString == "" {
		blueSecretMatchString = DefaultBlueSecretMatchString
	}

	queueKeyFn := func(obj runtime.Object) string {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			return ""
		}
		return key
	}

	eventFilterFn := func(obj interface{}) bool {
		if s, ok := obj.(*corev1.Secret); ok {
			if s.Type == RookType && strings.Contains(s.ObjectMeta.Name, blueSecretMatchString) {
				return true
			}
		}
		return false
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

	secret, err := getSecret(c.spokeSecretLister, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get the secret %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	sc, err := c.getStorageClusterFromRookSecret(secret)
	if err != nil {
		return fmt.Errorf("failed to get the storage cluster name from the sercet %q in namespace %q in managed cluster. Error %v", name, namespace, err)
	}

	newSecret, err := createBlueSecret(secret, sc, c.clusterName)
	if err != nil {
		return fmt.Errorf("failed to create secret from the managed cluster secret %q from namespace %v for the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
	}

	err = createSecret(c.hubKubeClient, c.recorder, &newSecret)
	if err != nil {
		return fmt.Errorf("failed to sync managed cluster secret %q from namespace %v to the hub cluster in namespace %q err: %v", secret.Name, secret.Namespace, c.clusterName, err)
	}

	klog.Infof("successfully synced managed cluster secret %q from namespace %v to the hub cluster in namespace %q", secret.Name, secret.Namespace, c.clusterName)

	return nil
}

func (c *blueSecretTokenExchangeAgentController) getStorageClusterFromRookSecret(secret *corev1.Secret) (storageCluster string, err error) {
	for _, v := range secret.ObjectMeta.OwnerReferences {
		if v.Kind != "CephCluster" {
			continue
		}

		rclient, err := rookclient.NewForConfig(c.spokeKubeConfig)
		if err != nil {
			return "", fmt.Errorf("unable to create a rook client err: %v", err)
		}

		found, err := rclient.CephV1().CephClusters(secret.Namespace).Get(context.TODO(), v.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("unable to fetch ceph cluster err: %v", err)
		}

		for _, owner := range found.ObjectMeta.OwnerReferences {
			if owner.Kind != "StorageCluster" {
				continue
			}
			storageCluster = owner.Name
		}

		if storageCluster != "" {
			break
		}
	}

	if storageCluster == "" {
		return storageCluster, fmt.Errorf("could not get storageCluster name")
	}
	return storageCluster, nil
}

func createBlueSecret(secret *corev1.Secret, storageCluster string, managedCluster string) (nsecret corev1.Secret, err error) {
	if secret == nil {
		return nsecret, fmt.Errorf("cannot create secret on the hub, source secret nil")
	}

	secretData, err := json.Marshal(secret.Data)
	if err != nil {
		return nsecret, fmt.Errorf("cannot create secret on the hub, marshalling failed")
	}

	nSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.CreateUniqueSecretName(managedCluster, secret.Namespace, storageCluster),
			Namespace: managedCluster,
			Labels: map[string]string{
				common.SecretLabelTypeKey: string(common.SourceLabel),
			},
		},
		Type: common.SecretLabelTypeKey,
		Data: map[string][]byte{
			common.SecretDataKey:         secretData,
			common.NamespaceKey:          []byte(secret.Namespace),
			common.StorageClusterNameKey: []byte(storageCluster),
		},
	}
	return nSecret, nil
}
