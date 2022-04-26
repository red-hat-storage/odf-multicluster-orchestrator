/*
Copyright 2021 Red Hat OpenShift Data Foundation.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package console

import (
	"context"
	"fmt"

	consolev1alpha1 "github.com/openshift/api/console/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	odfMulticlusterPluginName = "odf-multicluster-console"
	pluginBasePath            = "/"

	proxyAlias            = "acm-thanos-querier"
	proxyServiceName      = "rbac-query-proxy"
	proxyServiceNamespace = "open-cluster-management-observability"
	proxyServicePort      = 8443
	pluginDisplayName     = "ODF Multicluster Plugin"

	servicePortName         = "console-port"
	serviceSecretAnnotation = "service.alpha.openshift.io/serving-cert-secret-name"
	serviceLabelKey         = "app.kubernetes.io/name"
)

func getService(serviceName string, port int, deploymentNamespace string) apiv1.Service {
	return apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: deploymentNamespace,
			Annotations: map[string]string{
				serviceSecretAnnotation: fmt.Sprintf("%s-serving-cert", serviceName),
			},
			Labels: map[string]string{
				serviceLabelKey: odfMulticlusterPluginName,
			},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Protocol:   apiv1.ProtocolTCP,
					TargetPort: intstr.IntOrString{IntVal: int32(port)},
					Port:       int32(port),
					Name:       servicePortName,
				},
			},
			Selector: map[string]string{
				serviceLabelKey: odfMulticlusterPluginName,
			},
		},
	}
}

func getConsolePluginCR(consolePort int, serviceName string, deploymentNamespace string) consolev1alpha1.ConsolePlugin {
	return consolev1alpha1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: odfMulticlusterPluginName,
		},
		Spec: consolev1alpha1.ConsolePluginSpec{
			DisplayName: pluginDisplayName,
			Service: consolev1alpha1.ConsolePluginService{
				Name:      serviceName,
				Namespace: deploymentNamespace,
				Port:      int32(consolePort),
				BasePath:  pluginBasePath,
			},
			Proxy: []consolev1alpha1.ConsolePluginProxy{
				{
					Type:      consolev1alpha1.ProxyTypeService,
					Alias:     proxyAlias,
					Authorize: true,
					Service: consolev1alpha1.ConsolePluginProxyServiceConfig{
						Name:      proxyServiceName,
						Namespace: proxyServiceNamespace,
						Port:      int32(proxyServicePort),
					},
				},
			},
		},
	}
}

func InitConsole(ctx context.Context, client client.Client, odfPort int, deploymentNamespace string) error {
	deployment := appsv1.Deployment{}
	if err := client.Get(context.TODO(), types.NamespacedName{
		Name:      odfMulticlusterPluginName,
		Namespace: deploymentNamespace,
	}, &deployment); err != nil {
		return err
	}
	// Create core ODF multicluster console service
	odfService := getService(odfMulticlusterPluginName, odfPort, deploymentNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, client, &odfService, func() error {
		return nil
	}); err != nil {
		return err
	}
	// Create core ODF multicluster plugin
	odfConsolePlugin := getConsolePluginCR(odfPort, odfService.ObjectMeta.Name, deploymentNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, client, &odfConsolePlugin, func() error {
		return nil
	}); err != nil {
		return err
	}

	return nil
}
