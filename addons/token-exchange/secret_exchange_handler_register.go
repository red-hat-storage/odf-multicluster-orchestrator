package addons

import (
	"fmt"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	"k8s.io/client-go/rest"
)

type SecretExchangeHandler struct {
	RegisteredHandlers map[string]SecretExchangeHandlerInerface
}

var secretExchangeHandler *SecretExchangeHandler

// intialize secretExchangeHandler with handlers
func registerHandler(mode multiclusterv1alpha1.DRType, spokeKubeConfig *rest.Config, hubKubeConfig *rest.Config) error {
	// rook specific client
	rookClient, err := rookclient.NewForConfig(spokeKubeConfig)
	if err != nil {
		return fmt.Errorf("failed to add rook client: %v", err)
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
		RegisteredHandlers: make(map[string]SecretExchangeHandlerInerface),
	}

	switch mode {
	case multiclusterv1alpha1.Async:
		secretExchangeHandler.RegisteredHandlers[utils.RookSecretHandlerName] = rookSecretHandler{
			rookClient:  rookClient,
			spokeClient: genericSpokeClient,
			hubClient:   genericHubClient,
		}
		secretExchangeHandler.RegisteredHandlers[utils.S3SecretHandlerName] = s3SecretHandler{
			spokeClient: genericSpokeClient,
			hubClient:   genericHubClient,
		}
	case multiclusterv1alpha1.Sync:
		secretExchangeHandler.RegisteredHandlers[utils.S3SecretHandlerName] = s3SecretHandler{
			spokeClient: genericSpokeClient,
			hubClient:   genericHubClient,
		}
	}

	return nil
}
