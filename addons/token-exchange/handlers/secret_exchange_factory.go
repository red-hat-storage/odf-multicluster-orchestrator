package handlers

import (
	"fmt"

	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
)

var (
	RegisteredHandlers map[string]SecretExchangeInerface
)

func GetSecretHandler(secretHandlerName string, spokeKubeConfig *rest.Config, spokeSecretLister corev1lister.SecretLister, spokeConfigMapLister corev1lister.ConfigMapLister) (SecretExchangeInerface, error) {
	if RegisteredHandlers == nil {
		RegisteredHandlers = map[string]SecretExchangeInerface{
			MCGSecretHandlerName: MCGSecretHandler{
				spokeKubeConfig:      spokeKubeConfig,
				spokeSecretLister:    spokeSecretLister,
				spokeConfigMapLister: spokeConfigMapLister,
			},
			RookSecretHandlerName: RookSecretHandler{
				spokeKubeConfig:      spokeKubeConfig,
				spokeSecretLister:    spokeSecretLister,
				spokeConfigMapLister: spokeConfigMapLister,
			},
		}
	}

	if handler, ok := RegisteredHandlers[secretHandlerName]; ok {
		return handler, nil
	}
	return nil, fmt.Errorf("unable to find secret handler %q", secretHandlerName)
}

func GetWhitelistedHandlers() []string {
	// TODO whitelist handlers based on MirrorPeer spec
	whitelistedHandler := []string{RookSecretHandlerName, MCGSecretHandlerName}
	return whitelistedHandler
}
