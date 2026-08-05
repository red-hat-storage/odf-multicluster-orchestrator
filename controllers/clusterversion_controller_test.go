package controllers

import (
	"context"
	"log/slog"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/console"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "openshift-operators"

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(configv1.AddToScheme(s))
	utilruntime.Must(consolev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(ocstlsv1.AddToScheme(s))
	return s
}

func newTestReconciler(scheme *runtime.Scheme, objs ...runtime.Object) *ClusterVersionReconciler {
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	return &ClusterVersionReconciler{
		Client:            client,
		Scheme:            scheme,
		Logger:            slog.Default(),
		ConsolePort:       9001,
		OperatorNamespace: testNamespace,
	}
}

func TestEnsureConsolePlugin(t *testing.T) {
	cases := []struct {
		name             string
		clusterVersion   string
		expectedBasePath string
	}{
		{"OCP 4.21 sets main base path", "4.21.3", console.MAIN_BASE_PATH},
		{"OCP 4.23 nightly sets compatibility base path", "4.23.0-0.nightly-2026-06-14-141125", console.COMPATIBILITY_BASE_PATH},
		{"OCP 5.0 sets compatibility base path", "5.0.1", console.COMPATIBILITY_BASE_PATH},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := newTestScheme()
			r := newTestReconciler(scheme)

			require.NoError(t, r.ensureConsolePlugin(context.TODO(), c.clusterVersion))

			plugin := &consolev1.ConsolePlugin{}
			require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin))
			assert.Equal(t, c.expectedBasePath, plugin.Spec.Backend.Service.BasePath)
		})
	}
}

func TestEnsureConsolePluginUpdatesBasePath(t *testing.T) {
	scheme := newTestScheme()
	r := newTestReconciler(scheme)

	require.NoError(t, r.ensureConsolePlugin(context.TODO(), "4.21.3"))

	plugin := &consolev1.ConsolePlugin{}
	require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin))
	assert.Equal(t, console.MAIN_BASE_PATH, plugin.Spec.Backend.Service.BasePath)

	require.NoError(t, r.ensureConsolePlugin(context.TODO(), "4.23.0-0.nightly-2026-06-14-141125"))
	require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin))
	assert.Equal(t, console.COMPATIBILITY_BASE_PATH, plugin.Spec.Backend.Service.BasePath)
}

func TestEnsureNginxConfigMap_NoTLSProfile(t *testing.T) {
	scheme := newTestScheme()
	r := newTestReconciler(scheme)

	require.NoError(t, r.ensureNginxConfigMap(context.TODO()))

	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{
		Name: console.NginxConfigMapName, Namespace: testNamespace,
	}, cm))
	assert.NotContains(t, cm.Data[console.NginxConfKey], "ssl_protocols")
	assert.Contains(t, cm.Data[console.NginxConfKey], "listen       9001 ssl;")
}

func TestEnsureNginxConfigMap_WithMatchingTLSProfile(t *testing.T) {
	scheme := newTestScheme()
	tlsProfile := &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.TLSProfileName,
			Namespace: testNamespace,
		},
		Spec: ocstlsv1.TLSProfileSpec{
			Rules: []ocstlsv1.TLSProfileRules{{
				Selectors: []ocstlsv1.Selector{
					ocstlsv1.Selector(utils.TLSProfileDomain + "/" + utils.TLSProfileServer),
				},
				Config: ocstlsv1.TLSConfig{
					Version: ocstlsv1.VersionTLS1_3,
					Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
					Groups:  []ocstlsv1.TLSGroupName{"X25519", "secp256r1"},
				},
			}},
		},
	}
	r := newTestReconciler(scheme, tlsProfile)

	require.NoError(t, r.ensureNginxConfigMap(context.TODO()))

	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{
		Name: console.NginxConfigMapName, Namespace: testNamespace,
	}, cm))
	assert.Contains(t, cm.Data[console.NginxConfKey], "ssl_protocols TLSv1.3;")
	assert.Contains(t, cm.Data[console.NginxConfKey], "ssl_conf_command Ciphersuites")
}

func TestEnsureNginxConfigMap_WithNonMatchingTLSProfile(t *testing.T) {
	scheme := newTestScheme()
	tlsProfile := &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.TLSProfileName,
			Namespace: testNamespace,
		},
		Spec: ocstlsv1.TLSProfileSpec{
			Rules: []ocstlsv1.TLSProfileRules{{
				Selectors: []ocstlsv1.Selector{"noobaa.io"},
				Config: ocstlsv1.TLSConfig{
					Version: ocstlsv1.VersionTLS1_3,
					Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256"},
					Groups:  []ocstlsv1.TLSGroupName{"X25519"},
				},
			}},
		},
	}
	r := newTestReconciler(scheme, tlsProfile)

	require.NoError(t, r.ensureNginxConfigMap(context.TODO()))

	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{
		Name: console.NginxConfigMapName, Namespace: testNamespace,
	}, cm))
	assert.NotContains(t, cm.Data[console.NginxConfKey], "ssl_protocols")
}
