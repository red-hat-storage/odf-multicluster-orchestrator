package addons

// common func between different secret handlers
type SecretExchangeHandlerInterface interface {
	getBlueSecretFilter(interface{}) (ClusterType, bool)
	getGreenSecretFilter(interface{}) bool
	syncBlueSecret(string, string, *blueSecretTokenExchangeAgentController) error
	syncGreenSecret(string, string, *greenSecretTokenExchangeAgentController) error
}
