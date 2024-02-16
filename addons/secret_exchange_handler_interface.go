package addons

// common func between different secret handlers
type SecretExchangeHandlerInterface interface {
	getBlueSecretFilter(interface{}) bool
	getGreenSecretFilter(interface{}) bool
	syncBlueSecret(string, string) error
	syncGreenSecret(string, string) error
}
