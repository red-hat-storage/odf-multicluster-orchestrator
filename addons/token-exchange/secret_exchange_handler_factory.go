package addons

import (
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretExchangeHandler struct {
	RegisteredHandlers map[string]SecretExchangeHandlerInerface
}

var secretExchangeHander *SecretExchangeHandler

func registerHandler(spokeKubeConfig *rest.Config) error {
	if secretExchangeHander == nil {
		scheme := runtime.NewScheme()

		if err := ocsv1.AddToScheme(scheme); err != nil {
			return fmt.Errorf("failed to add ocsv1 scheme to runtime scheme: %v", err)
		}
		if err := routev1.AddToScheme(scheme); err != nil {
			return fmt.Errorf("failed to add routev1 scheme to runtime scheme: %v", err)
		}
		client, err := client.New(spokeKubeConfig, client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		secretExchangeHander = &SecretExchangeHandler{
			RegisteredHandlers: map[string]SecretExchangeHandlerInerface{
				RookSecretHandlerName: RookSecretHandler{
					spokeClient: client,
				},
				S3SecretHandlerName: S3SecretHandler{
					spokeClient: client,
				},
			},
		}
	}

	return nil
}

func getSecretHandler(secretHandlerName string) (SecretExchangeHandlerInerface, error) {
	if handler, ok := secretExchangeHander.RegisteredHandlers[secretHandlerName]; ok {
		return handler, nil
	}
	return nil, fmt.Errorf("unable to find secret handler %q", secretHandlerName)
}

func getWhitelistedHandlers() []string {
	// TODO whitelist handlers based on MirrorPeer spec
	return []string{RookSecretHandlerName, S3SecretHandlerName}
}
