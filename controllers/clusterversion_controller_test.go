package controllers

import (
	"context"
	"log/slog"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/console"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(configv1.AddToScheme(s))
	utilruntime.Must(consolev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(corev1.AddToScheme(s))
	return s
}

func TestEnsureConsolePlugin(t *testing.T) {
	testNamespace := "openshift-operators"
	testPort := 9001

	cases := []struct {
		name             string
		clusterVersion   string
		expectedBasePath string
	}{
		{
			name:             "OCP 4.21 sets main base path",
			clusterVersion:   "4.21.3",
			expectedBasePath: console.MAIN_BASE_PATH,
		},
		{
			name:             "OCP 4.23 nightly sets compatibility base path",
			clusterVersion:   "4.23.0-0.nightly-2026-06-14-141125",
			expectedBasePath: console.COMPATIBILITY_BASE_PATH,
		},
		{
			name:             "OCP 5.0 sets compatibility base path",
			clusterVersion:   "5.0.1",
			expectedBasePath: console.COMPATIBILITY_BASE_PATH,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := newTestScheme()
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      console.PluginName,
					Namespace: testNamespace,
				},
			}
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment).Build()

			r := &ClusterVersionReconciler{
				Client:            client,
				Scheme:            scheme,
				Logger:            slog.Default(),
				ConsolePort:       testPort,
				OperatorNamespace: testNamespace,
			}

			err := r.ensureConsolePlugin(context.TODO(), c.clusterVersion)
			assert.NoError(t, err)

			plugin := &consolev1.ConsolePlugin{}
			err = client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin)
			assert.NoError(t, err)
			assert.Equal(t, c.expectedBasePath, plugin.Spec.Backend.Service.BasePath)
		})
	}
}

func TestEnsureConsolePluginUpdatesBasePath(t *testing.T) {
	testNamespace := "openshift-operators"
	testPort := 9001
	scheme := newTestScheme()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      console.PluginName,
			Namespace: testNamespace,
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment).Build()

	r := &ClusterVersionReconciler{
		Client:            client,
		Scheme:            scheme,
		Logger:            slog.Default(),
		ConsolePort:       testPort,
		OperatorNamespace: testNamespace,
	}

	// First call with OCP 4.21 — main path
	err := r.ensureConsolePlugin(context.TODO(), "4.21.3")
	assert.NoError(t, err)

	plugin := &consolev1.ConsolePlugin{}
	err = client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin)
	assert.NoError(t, err)
	assert.Equal(t, console.MAIN_BASE_PATH, plugin.Spec.Backend.Service.BasePath)

	// Second call with OCP 4.23 — should update to compatibility path
	err = r.ensureConsolePlugin(context.TODO(), "4.23.0-0.nightly-2026-06-14-141125")
	assert.NoError(t, err)

	err = client.Get(context.TODO(), types.NamespacedName{Name: console.PluginName}, plugin)
	assert.NoError(t, err)
	assert.Equal(t, console.COMPATIBILITY_BASE_PATH, plugin.Spec.Backend.Service.BasePath)
}
