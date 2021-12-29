package addons

import (
	"fmt"

	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	"k8s.io/client-go/rest"
)

type SecretExchangeHandler struct {
	RegisteredHandlers map[string]SecretExchangeHandlerInerface
}

var secretExchangeHandler *SecretExchangeHandler

// intialize secretExchangeHandler with handlers
func registerHandler(spokeKubeConfig *rest.Config) error {
	// rook specific client
	rookClient, err := rookclient.NewForConfig(spokeKubeConfig)
	if err != nil {
		return fmt.Errorf("failed to add rook client: %v", err)
	}

	// a generic spoke client which is common between all handlers
	genericSpokeClient, err := getClient(spokeKubeConfig)
	if err != nil {
		return err
	}

	secretExchangeHandler = &SecretExchangeHandler{
		RegisteredHandlers: map[string]SecretExchangeHandlerInerface{
			RookSecretHandlerName: rookSecretHandler{
				spokeClient: genericSpokeClient,
				rookClient:  rookClient,
			},
		},
	}

	return nil
}
