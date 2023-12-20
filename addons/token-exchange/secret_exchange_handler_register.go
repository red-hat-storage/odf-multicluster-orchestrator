package addons

import (
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"k8s.io/client-go/rest"
)

type SecretExchangeHandler struct {
	RegisteredHandlers map[string]SecretExchangeHandlerInterface
}

var secretExchangeHandler *SecretExchangeHandler

// intialize secretExchangeHandler with handlers
func registerHandler(mode multiclusterv1alpha1.DRType, spokeKubeConfig *rest.Config, hubKubeConfig *rest.Config) error {
	if mode != multiclusterv1alpha1.Sync && mode != multiclusterv1alpha1.Async {
		return fmt.Errorf("unknown mode %q detected, please check the mirrorpeer created", mode)
	}

	// a generic client which is common between all handlers
	genericSpokeClient, err := getClient(spokeKubeConfig)
	if err != nil {
		return err
	}
	genericHubClient, err := getClient(hubKubeConfig)
	if err != nil {
		return err
	}

	secretExchangeHandler = &SecretExchangeHandler{
		RegisteredHandlers: make(map[string]SecretExchangeHandlerInterface),
	}

	secretExchangeHandler.RegisteredHandlers[utils.S3SecretHandlerName] = s3SecretHandler{
		spokeClient: genericSpokeClient,
		hubClient:   genericHubClient,
	}
	secretExchangeHandler.RegisteredHandlers[utils.RookSecretHandlerName] = rookSecretHandler{
		spokeClient: genericSpokeClient,
		hubClient:   genericHubClient,
	}

	return nil
}
