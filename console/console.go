/*
Copyright 2026 Red Hat Data Foundation.
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
	"fmt"
	"strings"

	consolev1 "github.com/openshift/api/console/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	MAIN_BASE_PATH          = "/"
	COMPATIBILITY_BASE_PATH = "/compatibility/"
	PluginName              = "odf-multicluster-console"
)

var (
	proxyAlias            = "acm-thanos-querier"
	proxyServiceName      = "rbac-query-proxy"
	proxyServiceNamespace = "open-cluster-management-observability"
	proxyServicePort      = 8443
	pluginDisplayName     = "DF Multicluster Plugin"

	servicePortName         = "console-port"
	serviceSecretAnnotation = "service.alpha.openshift.io/serving-cert-secret-name"
	serviceLabelKey         = "app.kubernetes.io/name"
)

func GetService(serviceName string, port int, deploymentNamespace string) apiv1.Service {
	return apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: deploymentNamespace,
			Annotations: map[string]string{
				serviceSecretAnnotation: fmt.Sprintf("%s-serving-cert", serviceName),
			},
			Labels: map[string]string{
				serviceLabelKey: PluginName,
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
				serviceLabelKey: PluginName,
			},
		},
	}
}

func GetConsolePluginCR(consolePort int, serviceName string, deploymentNamespace string) consolev1.ConsolePlugin {
	return consolev1.ConsolePlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name: PluginName,
		},
		Spec: consolev1.ConsolePluginSpec{
			DisplayName: pluginDisplayName,
			Backend: consolev1.ConsolePluginBackend{
				Service: &consolev1.ConsolePluginService{
					Name:      serviceName,
					Namespace: deploymentNamespace,
					Port:      int32(consolePort),
					BasePath:  MAIN_BASE_PATH,
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
		},
	}
}

func GetBasePath(clusterVersion string) string {
	if strings.Contains(clusterVersion, "4.23") || strings.Contains(clusterVersion, "5.0") {
		return COMPATIBILITY_BASE_PATH
	}

	return MAIN_BASE_PATH
}
