package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"reflect"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	TestManagedClusterName      = "test"
	TestStorageClusterName      = "StorageCluster"
	TestCephClusterName         = "CephCluter"
	TestStorageClusterNamespace = "spokeNS"
	TestPeerTokenSecretName     = "cluster-peer-token-ocs-storagecluster-cephcluster"
)

func fakeSpokeClientForGreenSecret(t *testing.T) client.Client {
	obj := []runtime.Object{
		&ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TestStorageClusterName,
				Namespace: TestStorageClusterNamespace,
			},
			Spec: ocsv1.StorageClusterSpec{
				Mirroring: ocsv1.MirroringSpec{
					PeerSecretNames: []string{},
				},
			},
		},
	}
	scheme := runtime.NewScheme()
	err := ocsv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsv1 scheme")
	}
	return cfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).Build()
}

func fakeRookClient(t *testing.T) *rookclient.Clientset {
	obj := []runtime.Object{
		&cephv1.CephCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TestCephClusterName,
				Namespace: TestStorageClusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "StorageCluster",
						Name: TestStorageClusterName,
					},
				},
			},
			Spec: cephv1.ClusterSpec{},
		},
	}
	rclient := rookclient.NewSimpleClientset(obj...)
	return rclient
}

func getFakeToken() map[string][]byte {
	return map[string][]byte{"token": []byte("fakeToken")}
}

func fakeSecretData(t *testing.T) []byte {
	tokenBytes, err := json.Marshal(getFakeToken())
	assert.NoError(t, err)
	return tokenBytes
}

func getFakeRookBlueSecretExchangeController(t *testing.T) *blueSecretTokenExchangeAgentController {
	spokeResources := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TestPeerTokenSecretName,
				Namespace: TestStorageClusterNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "CephCluster",
						Name: TestCephClusterName,
					},
				},
			},
			Data: getFakeToken(),
			Type: RookType,
		},
	}

	fakeHubKubeClient := kubefake.NewSimpleClientset([]runtime.Object{}...)
	fakeSpokeKubeClient := kubefake.NewSimpleClientset(spokeResources...)
	fakeHubInformerFactory := kubeinformers.NewSharedInformerFactory(fakeHubKubeClient, time.Minute*10)
	fakeSpokeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeSpokeKubeClient, time.Minute*10)

	secretStore := fakeSpokeInformerFactory.Core().V1().Secrets().Informer().GetStore()
	for _, object := range spokeResources {
		err := secretStore.Add(object)
		assert.NoError(t, err)
	}

	return &blueSecretTokenExchangeAgentController{
		hubKubeClient:        fakeHubKubeClient,
		spokeKubeClient:      fakeSpokeKubeClient,
		spokeSecretLister:    fakeSpokeInformerFactory.Core().V1().Secrets().Lister(),
		hubSecretLister:      fakeHubInformerFactory.Core().V1().Secrets().Lister(),
		spokeConfigMapLister: fakeSpokeInformerFactory.Core().V1().ConfigMaps().Lister(),
		clusterName:          TestManagedClusterName,
		recorder:             eventstesting.NewTestingEventRecorder(t),
	}
}

func getFakeRookGreenSecretExchangeController(t *testing.T) *greenSecretTokenExchangeAgentController {
	hubResources := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "greensecretwithwrongsc",
				Namespace: "ns",
				Labels: map[string]string{
					utils.SecretLabelTypeKey: string(utils.DestinationLabel),
				},
			},
			Data: map[string][]byte{
				"namespace":            []byte(TestStorageClusterNamespace),
				"secret-data":          fakeSecretData(t),
				"storage-cluster-name": []byte("wrongstoragecluster"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "greensecret",
				Namespace: "ns",
				Labels: map[string]string{
					utils.SecretLabelTypeKey: string(utils.DestinationLabel),
				},
			},
			Data: map[string][]byte{
				"namespace":            []byte(TestStorageClusterNamespace),
				"secret-data":          fakeSecretData(t),
				"storage-cluster-name": []byte(TestStorageClusterName),
			},
		},
	}

	fakeHubKubeClient := kubefake.NewSimpleClientset(hubResources...)
	fakeSpokeKubeClient := kubefake.NewSimpleClientset([]runtime.Object{}...)
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
		clusterName:       TestManagedClusterName,
		recorder:          eventstesting.NewTestingEventRecorder(t),
	}
}

func TestRookGreenSecretSnyc(t *testing.T) {
	rookHandler := rookSecretHandler{
		spokeClient: fakeSpokeClientForGreenSecret(t),
		rookClient:  fakeRookClient(t),
	}
	cases := []testCase{
		{
			name:          "green secret not found in hub 1 ",
			namespaceName: "ns/wrongname",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "green secret not found in hub 2",
			namespaceName: "wrongns/test",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "green secret found in hub with invalid storage cluster",
			namespaceName: "ns/greensecretwithwrongsc",
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "green secret found in hub",
			namespaceName: "ns/greensecret",
			errExpected:   false,
			syncExpected:  true,
		},
	}

	registerFakeSecretHandler()
	fakeCtrl := getFakeRookGreenSecretExchangeController(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			namespace, name, err := cache.SplitMetaNamespaceKey(c.namespaceName)
			assert.NoError(t, err)
			err = rookHandler.syncGreenSecret(name, namespace, fakeCtrl)
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.syncExpected {
				actualSecret, err := fakeCtrl.spokeKubeClient.CoreV1().Secrets(TestStorageClusterNamespace).Get(context.TODO(), name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, actualSecret.GetLabels()[utils.CreatedByLabelKey], TokenExchangeName)
				ctx := context.TODO()
				sc := &ocsv1.StorageCluster{}
				err = rookHandler.spokeClient.Get(ctx, types.NamespacedName{Name: TestStorageClusterName, Namespace: TestStorageClusterNamespace}, sc)
				assert.NoError(t, err)
				assert.Equal(t, []string{name}, sc.Spec.Mirroring.PeerSecretNames)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func getExpectedRookBlueSecret(t *testing.T) *corev1.Secret {
	secretData, err := json.Marshal(getFakeToken())
	assert.NoError(t, err)

	data := map[string][]byte{
		utils.SecretDataKey:         secretData,
		utils.NamespaceKey:          []byte(TestStorageClusterNamespace),
		utils.StorageClusterNameKey: []byte(TestStorageClusterName),
		utils.SecretOriginKey:       []byte(utils.OriginMap["RookOrigin"]),
	}
	expectedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.CreateUniqueSecretName(TestManagedClusterName, TestStorageClusterNamespace, TestStorageClusterName),
			Namespace: TestManagedClusterName,
			Labels: map[string]string{
				utils.SecretLabelTypeKey: string(utils.SourceLabel),
			},
		},
		Type: utils.SecretLabelTypeKey,
		Data: data,
	}

	return &expectedSecret
}

func TestRookBlueSecretSnyc(t *testing.T) {
	rookHandler := rookSecretHandler{
		spokeClient: fakeSpokeClientForGreenSecret(t),
		rookClient:  fakeRookClient(t),
	}
	cases := []testCase{
		{
			name:          "peer secret not found 1",
			namespaceName: fmt.Sprintf("%s/wrongname", TestStorageClusterNamespace),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "peer secret not found 2",
			namespaceName: fmt.Sprintf("wrongns/%s", TestPeerTokenSecretName),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "blue secret created in hub",
			namespaceName: fmt.Sprintf("%s/%s", TestStorageClusterNamespace, TestPeerTokenSecretName),
			errExpected:   false,
			syncExpected:  true,
		},
	}

	registerFakeSecretHandler()
	fakeCtrl := getFakeRookBlueSecretExchangeController(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			namespace, name, err := cache.SplitMetaNamespaceKey(c.namespaceName)
			assert.NoError(t, err)
			err = rookHandler.syncBlueSecret(name, namespace, fakeCtrl)
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.syncExpected {
				actualSecret, err := fakeCtrl.hubKubeClient.CoreV1().Secrets(TestManagedClusterName).Get(context.TODO(), utils.CreateUniqueSecretName(TestManagedClusterName, TestStorageClusterNamespace, TestStorageClusterName), metav1.GetOptions{})
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(getExpectedRookBlueSecret(t), actualSecret))
			} else {
				assert.Error(t, err)
			}
		})
	}
}
