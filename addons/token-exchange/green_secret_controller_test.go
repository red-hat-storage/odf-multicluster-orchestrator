package addons

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

type fakeSyncContext struct {
	key      string
	queue    workqueue.RateLimitingInterface
	recorder events.Recorder
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

func getFakeTokenExchangeController(t *testing.T, hubResources, spokeResources []runtime.Object) *greenSecretTokenExchangeAgentController {

	fakeHubKubeClient := kubefake.NewSimpleClientset(hubResources...)
	fakeSpokeKubeClient := kubefake.NewSimpleClientset(spokeResources...)
	fakeHubInformerFactory := kubeinformers.NewSharedInformerFactory(fakeHubKubeClient, time.Minute*10)
	fakeSpokeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeSpokeKubeClient, time.Minute*10)

	secretStore := fakeHubInformerFactory.Core().V1().Secrets().Informer().GetStore()
	for _, object := range hubResources {
		err := secretStore.Add(object)
		assert.NoError(t, err)
	}

	return &greenSecretTokenExchangeAgentController{
		hubKubeClient:     fakeHubKubeClient,
		spokeKubeClient:   fakeSpokeKubeClient,
		spokeSecretLister: fakeSpokeInformerFactory.Core().V1().Secrets().Lister(),
		hubSecretLister:   fakeHubInformerFactory.Core().V1().Secrets().Lister(),
		clusterName:       "test",
		recorder:          eventstesting.NewTestingEventRecorder(t),
	}

}

func fakeSecretData(t *testing.T) []byte {
	token := map[string][]byte{"token": []byte("fakeToken")}
	tokenBytes, err := json.Marshal(token)
	assert.NoError(t, err)
	return tokenBytes
}

func TestGreenSecretSync(t *testing.T) {
	cases := []struct {
		name           string
		hubResources   []runtime.Object
		spokeResources []runtime.Object
		errExpected    bool
		syncExpected   bool
	}{
		{
			name:         "no secrets found in hub",
			hubResources: []runtime.Object{},
			errExpected:  true,
			syncExpected: false,
		},
		{
			name: "no green secrets found in hub",
			hubResources: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "ns",
					},
					Data: map[string][]byte{"namespace": []byte("spokeNS")},
				},
			},
			errExpected:  true,
			syncExpected: false,
		},
		{
			name: "skip syncing green secret with no spoke namespace",
			hubResources: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "ns",
						Labels: map[string]string{
							common.SecretLabelTypeKey: string(common.DestinationLabel),
						},
					},
				},
			},
			errExpected:  true,
			syncExpected: false,
		},

		{
			name: "sync green secret from hub to spoke",
			hubResources: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "ns",
						Labels: map[string]string{
							common.SecretLabelTypeKey: string(common.DestinationLabel),
						},
					},
					Data: map[string][]byte{
						"namespace":            []byte("spokeNS"),
						"secret-data":          fakeSecretData(t),
						"storage-cluster-name": []byte("StorageCluster1"),
					},
				},
			},
			spokeResources: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "spokeNS",
					},
				},
			},
			// TODO: fix unit test for updating storageCluster with secret name
			errExpected:  true,
			syncExpected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeCtrl := getFakeTokenExchangeController(t, c.hubResources, c.spokeResources)
			err := fakeCtrl.sync(context.TODO(), NewFakeSyncContext(t, "ns/test"))
			if c.errExpected {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			actualSecret, err := fakeCtrl.spokeKubeClient.CoreV1().Secrets("spokeNS").Get(context.TODO(), "test", metav1.GetOptions{})
			if c.syncExpected {
				assert.NoError(t, err)
				assert.Equal(t, actualSecret.GetLabels()[common.CreatedByLabelKey], CreatedByLabelValue)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
