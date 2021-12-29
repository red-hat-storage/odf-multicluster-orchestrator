package handlers

import (
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	RegisteredHandlers map[string]SecretExchangeInerface
}

var secretHander *Handler

// singleton
func RegisterHandler(spokeKubeConfig *rest.Config) error {
	if secretHander == nil {
		scheme := runtime.NewScheme()
		if err := ocsv1.AddToScheme(scheme); err != nil {
			return fmt.Errorf("failed to add ocsv1 scheme to runtime scheme: %v", err)
		}
		client, err := client.New(spokeKubeConfig, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		secretHander = &Handler{
			RegisteredHandlers: map[string]SecretExchangeInerface{
				RookSecretHandlerName: RookSecretHandler{
					spokeClient: client,
				},
			},
		}
	}

	return nil
}

func GetSecretHandler(secretHandlerName string) (SecretExchangeInerface, error) {
	if handler, ok := secretHander.RegisteredHandlers[secretHandlerName]; ok {
		return handler, nil
	}
	return nil, fmt.Errorf("unable to find secret handler %q", secretHandlerName)
}

func GetWhitelistedHandlers() []string {
	// TODO whitelist handlers based on MirrorPeer spec
	return []string{RookSecretHandlerName}
}
