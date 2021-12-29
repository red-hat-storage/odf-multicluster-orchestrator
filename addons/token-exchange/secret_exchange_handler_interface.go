package addons

type SecretExchangeHandlerInerface interface {
	getBlueSecretFilter(interface{}) bool
	getGreenSecretFilter(interface{}) bool
	createBlueSecret(string, string, *blueSecretTokenExchangeAgentController) error
	createGreenSecret(string, string, *greenSecretTokenExchangeAgentController) error
}
