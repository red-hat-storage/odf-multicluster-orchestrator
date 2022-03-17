package addons

import (
	"context"
	"fmt"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
)

type fakeSyncContext struct {
	key      string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
}

type fakeSecretHandler struct{}

type testCase struct {
	name          string
	namespaceName string
	errExpected   bool
	syncExpected  bool
}

func (fakeSecretHandler) getBlueSecretFilter(obj interface{}) bool {
	if metaObj, has := obj.(metav1.Object); has {
		return metaObj.GetName() == "sourcesecret"
	}
	return false
}

func (f fakeSecretHandler) syncBlueSecret(name string, namespace string, c *blueSecretTokenExchangeAgentController) error {
	// mocking blue secret creation logic to verify params
	secret, err := getSecret(c.hubSecretLister, name, namespace)
	if err != nil {
		return err
	}
	if ok := f.getBlueSecretFilter(secret); !ok {
		return fmt.Errorf("not blue secret")
	}
	return nil
}

func (f fakeSyncContext) Queue() workqueue.RateLimitingInterface { return f.queue }
func (f fakeSyncContext) QueueKey() string                       { return f.key }
func (f fakeSyncContext) Recorder() events.Recorder              { return f.recorder }

func NewFakeSyncContext(t *testing.T, key string) *fakeSyncContext {
	return &fakeSyncContext{
		key:      key,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder: eventstesting.NewTestingEventRecorder(t),
	}
}

func getFakeTokenExchangeController(t *testing.T, secretType utils.SecretLabelType) factory.Controller {
	hubResources := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sourcesecret",
				Namespace: "ns",
			},
			Data: map[string][]byte{},
			Type: "source-type",
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "greensecret",
				Namespace: "ns",
			},
			Data: map[string][]byte{},
		},
	}
	fakeHubKubeClient := kubefake.NewSimpleClientset(hubResources...)
	fakeSpokeKubeClient := kubefake.NewSimpleClientset()
	fakeHubInformerFactory := kubeinformers.NewSharedInformerFactory(fakeHubKubeClient, time.Minute*10)
	fakeSpokeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeSpokeKubeClient, time.Minute*10)

	secretStore := fakeHubInformerFactory.Core().V1().Secrets().Informer().GetStore()
	for _, object := range hubResources {
		err := secretStore.Add(object)
		assert.NoError(t, err)
	}

	if secretType == utils.DestinationLabel {
		return newgreenSecretTokenExchangeAgentController(
			fakeHubKubeClient,
			fakeHubInformerFactory.Core().V1().Secrets(),
			fakeSpokeKubeClient,
			fakeSpokeInformerFactory.Core().V1().Secrets(),
			"test",
			&rest.Config{},
			eventstesting.NewTestingEventRecorder(t),
		)
	}

	return newblueSecretTokenExchangeAgentController(
		fakeHubKubeClient,
		fakeHubInformerFactory.Core().V1().Secrets(),
		fakeSpokeKubeClient,
		fakeSpokeInformerFactory.Core().V1().Secrets(),
		fakeSpokeInformerFactory.Core().V1().ConfigMaps(),
		"test",
		eventstesting.NewTestingEventRecorder(t),
		&rest.Config{},
	)
}

func registerFakeSecretHandler() {
	secretExchangeHandler = &SecretExchangeHandler{
		RegisteredHandlers: map[string]SecretExchangeHandlerInerface{
			"FakeSecretHandler": fakeSecretHandler{},
		},
	}
}

func TestBlueSecretSync(t *testing.T) {
	cases := []testCase{
		{
			name:          "peer secret not found in spoke 1 ",
			namespaceName: "ns/wrongname",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "peer secret not found in spoke 2",
			namespaceName: "wrongns/test",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "peer secret found in spoke",
			namespaceName: "ns/sourcesecret",
			errExpected:   false,
			syncExpected:  true,
		},
	}
	registerFakeSecretHandler()
	fakeCtrl := getFakeTokenExchangeController(t, utils.SourceLabel)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := fakeCtrl.Sync(context.TODO(), NewFakeSyncContext(t, c.namespaceName))
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.syncExpected {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
