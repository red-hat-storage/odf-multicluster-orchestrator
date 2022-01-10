package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	TestS3ObjectClaimName          = "odrbucket-12345"
	TestS3ObjectClaimNameNamesapce = "drns"
	TestS3BucketName               = "s3bucketname"
	TestAwsAccessKeyId             = "awskeyid"
	TestAwssecretaccesskey         = "awsaccesskey"
	TestS3RouteHost                = "s3.endpoint"
	StorageClusterName             = "ocs-storagecluster"
	StorageClusterNamespace        = "openshift-storage"
)

func fakeClientForS3Secret(t *testing.T) client.Client {
	obj := []runtime.Object{
		&routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      S3RouteName,
				Namespace: StorageClusterNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: TestS3RouteHost,
			},
		},
		&multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mirrorpeer",
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: TestManagedClusterName,
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      StorageClusterName,
							Namespace: StorageClusterNamespace,
						},
					},
					{
						ClusterName: "destCluster",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      StorageClusterName,
							Namespace: StorageClusterNamespace,
						},
					},
				},
			},
		},
	}

	return cfake.NewClientBuilder().WithScheme(mgrScheme).WithRuntimeObjects(obj...).Build()
}

func getFakeS3Secret() map[string][]byte {
	return map[string][]byte{
		common.AwsAccessKeyId:     []byte(TestAwsAccessKeyId),
		common.AwsSecretAccessKey: []byte(TestAwssecretaccesskey),
	}
}

func getFakeS3ConfigMap() map[string]string {
	return map[string]string{
		S3BucketName:   TestS3BucketName,
		S3BucketRegion: common.S3Region,
	}
}

func getFakeS3BlueSecretExchangeController(t *testing.T) *blueSecretTokenExchangeAgentController {
	spokeResources := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TestS3ObjectClaimName,
				Namespace: TestS3ObjectClaimNameNamesapce,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ObjectBucketClaim",
						Name: TestS3ObjectClaimNameNamesapce,
					},
				},
			},
			Data: getFakeS3Secret(),
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TestS3ObjectClaimName,
				Namespace: TestS3ObjectClaimNameNamesapce,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ObjectBucketClaim",
						Name: TestS3ObjectClaimNameNamesapce,
					},
				},
			},
			Data: getFakeS3ConfigMap(),
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "odrbucket-secretonly",
				Namespace: TestS3ObjectClaimNameNamesapce,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ObjectBucketClaim",
						Name: TestS3ObjectClaimNameNamesapce,
					},
				},
			},
			Data: getFakeS3Secret(),
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "odrbucket-configmaponly",
				Namespace: TestS3ObjectClaimNameNamesapce,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ObjectBucketClaim",
						Name: TestS3ObjectClaimNameNamesapce,
					},
				},
			},
			Data: getFakeS3ConfigMap(),
		},
	}

	fakeHubKubeClient := kubefake.NewSimpleClientset([]runtime.Object{}...)
	fakeSpokeKubeClient := kubefake.NewSimpleClientset(spokeResources...)
	fakeHubInformerFactory := kubeinformers.NewSharedInformerFactory(fakeHubKubeClient, time.Minute*10)
	fakeSpokeInformerFactory := kubeinformers.NewSharedInformerFactory(fakeSpokeKubeClient, time.Minute*10)

	secretStore := fakeSpokeInformerFactory.Core().V1().Secrets().Informer().GetStore()
	for _, object := range spokeResources {
		if s, ok := object.(*corev1.Secret); ok {
			err := secretStore.Add(s)
			assert.NoError(t, err)
		}
	}

	configMapStore := fakeSpokeInformerFactory.Core().V1().ConfigMaps().Informer().GetStore()
	for _, object := range spokeResources {
		if s, ok := object.(*corev1.ConfigMap); ok {
			err := configMapStore.Add(s)
			assert.NoError(t, err)
		}
	}

	return &blueSecretTokenExchangeAgentController{
		hubKubeClient:        fakeHubKubeClient,
		spokeKubeClient:      fakeSpokeKubeClient,
		spokeSecretLister:    fakeSpokeInformerFactory.Core().V1().Secrets().Lister(),
		spokeConfigMapLister: fakeSpokeInformerFactory.Core().V1().ConfigMaps().Lister(),
		hubSecretLister:      fakeHubInformerFactory.Core().V1().Secrets().Lister(),
		clusterName:          TestManagedClusterName,
		recorder:             eventstesting.NewTestingEventRecorder(t),
	}
}

func getExpectedS3BlueSecret(t *testing.T) *corev1.Secret {
	secretData, err := json.Marshal(
		map[string][]byte{
			common.AwsAccessKeyId:     []byte(TestAwsAccessKeyId),
			common.AwsSecretAccessKey: []byte(TestAwssecretaccesskey),
			common.S3BucketName:       []byte(TestS3BucketName),
			common.S3Endpoint:         []byte(fmt.Sprintf("https://%s", TestS3RouteHost)),
			common.S3Region:           []byte(common.S3Region),
			common.S3ProfileName:      []byte(fmt.Sprintf("%s-%s-%s", common.S3ProfilePrefix, TestManagedClusterName, StorageClusterName)),
		},
	)
	assert.NoError(t, err)

	data := map[string][]byte{
		common.SecretDataKey:         secretData,
		common.NamespaceKey:          []byte(StorageClusterNamespace),
		common.StorageClusterNameKey: []byte(StorageClusterName),
		common.SecretOriginKey:       []byte(common.S3Origin),
	}
	expectedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.CreateUniqueSecretName(TestManagedClusterName, StorageClusterNamespace, StorageClusterName, common.S3ProfilePrefix),
			Namespace: TestManagedClusterName,
			Labels: map[string]string{
				common.SecretLabelTypeKey: string(common.InternalLabel),
			},
		},
		Type: common.SecretLabelTypeKey,
		Data: data,
	}

	return &expectedSecret
}

func TestS3BlueSecretSnyc(t *testing.T) {
	s3Handler := s3SecretHandler{
		spokeClient: fakeClientForS3Secret(t),
		hubClient:   fakeClientForS3Secret(t),
	}
	cases := []testCase{
		{
			name:          "s3 secret not found 1",
			namespaceName: fmt.Sprintf("%s/wrongname", TestS3ObjectClaimNameNamesapce),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "s3 secret not found 2",
			namespaceName: fmt.Sprintf("wrongns/%s", TestS3ObjectClaimName),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "s3 configmap not found 3",
			namespaceName: fmt.Sprintf("%s/odrbucket-secretonly", TestS3ObjectClaimNameNamesapce),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "s3 secret not found 4",
			namespaceName: fmt.Sprintf("%s/odrbucket-configmaponly", TestS3ObjectClaimNameNamesapce),
			errExpected:   true,
			syncExpected:  false,
		},
		{
			name:          "blue secret created in hub",
			namespaceName: fmt.Sprintf("%s/%s", TestS3ObjectClaimNameNamesapce, TestS3ObjectClaimName),
			errExpected:   false,
			syncExpected:  true,
		},
	}

	registerFakeSecretHandler()
	fakeCtrl := getFakeS3BlueSecretExchangeController(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			namespace, name, err := cache.SplitMetaNamespaceKey(c.namespaceName)
			assert.NoError(t, err)
			err = s3Handler.syncBlueSecret(name, namespace, fakeCtrl)
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if c.syncExpected {
				actualSecret, err := fakeCtrl.hubKubeClient.CoreV1().Secrets(TestManagedClusterName).Get(context.TODO(), common.CreateUniqueSecretName(TestManagedClusterName, StorageClusterNamespace, StorageClusterName, common.S3ProfilePrefix), metav1.GetOptions{})
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(getExpectedS3BlueSecret(t), actualSecret))
			} else {
				assert.Error(t, err)
			}
		})
	}
}
