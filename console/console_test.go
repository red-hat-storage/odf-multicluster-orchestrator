package console

import (
	"context"
	"reflect"
	"testing"

	consolev1 "github.com/openshift/api/console/v1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	testMulticlusterNamespace = "test_multicluster_ns"
	testPort                  = 9003
)

type testCase struct {
	name        string
	port        int
	namespace   string
	errExpected bool
}

func getExpectedConsolePluginSpec() consolev1.ConsolePluginSpec {
	return consolev1.ConsolePluginSpec{
		DisplayName: pluginDisplayName,
		Backend: consolev1.ConsolePluginBackend{
			Service: &consolev1.ConsolePluginService{
				Name:      odfMulticlusterPluginName,
				Namespace: testMulticlusterNamespace,
				Port:      int32(testPort),
				BasePath:  pluginBasePath,
			},
			Type: consolev1.Service,
		},
		Proxy: []consolev1.ConsolePluginProxy{
			{
				Alias:         proxyAlias,
				Authorization: consolev1.UserToken,
				Endpoint: consolev1.ConsolePluginProxyEndpoint{
					Type: consolev1.ProxyTypeService,
					Service: &consolev1.ConsolePluginProxyServiceConfig{
						Name:      proxyServiceName,
						Namespace: proxyServiceNamespace,
						Port:      int32(proxyServicePort),
					},
				},
			},
		},
	}
}

func getExpectedServiceSpec() apiv1.ServiceSpec {
	return apiv1.ServiceSpec{
		Ports: []apiv1.ServicePort{
			{
				Protocol:   apiv1.ProtocolTCP,
				TargetPort: intstr.IntOrString{IntVal: int32(testPort)},
				Port:       int32(testPort),
				Name:       servicePortName,
			},
		},
		Selector: map[string]string{
			serviceLabelKey: odfMulticlusterPluginName,
		},
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := multiclusterv1alpha1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = appsv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add appsv1 scheme")
	}
	err = consolev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add consolev1 scheme")
	}
	err = apiv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add apiv1 scheme")
	}

	return scheme
}

func TestInitConsole(t *testing.T) {
	cases := []testCase{
		{
			name:        "multicluster console deployment not found",
			namespace:   "wrong_ns",
			port:        testPort,
			errExpected: true,
		},
		{
			name:        "successfully deployed multicluster plugin",
			namespace:   testMulticlusterNamespace,
			port:        testPort,
			errExpected: false,
		},
	}
	obj := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odfMulticlusterPluginName,
			Namespace: testMulticlusterNamespace,
		},
	}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&obj).Build()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := InitConsole(context.TODO(), client, c.port, c.namespace)
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				consolePlugin := consolev1.ConsolePlugin{}
				err := client.Get(context.TODO(), types.NamespacedName{
					Name: odfMulticlusterPluginName,
				}, &consolePlugin)
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(getExpectedConsolePluginSpec(), consolePlugin.Spec))
				service := apiv1.Service{}
				err = client.Get(context.TODO(), types.NamespacedName{
					Name:      odfMulticlusterPluginName,
					Namespace: testMulticlusterNamespace,
				}, &service)
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(getExpectedServiceSpec(), service.Spec))
			}
		})
	}
}
