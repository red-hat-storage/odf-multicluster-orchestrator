package handlers

type SecretExchangeInerface interface {
	GetBlueSecretFilter(interface{}) bool
	GetGreenSecretFilter(interface{}) bool
	CreateBlueSecret(string, string, interface{}) error
	CreateGreenSecret(string, string, interface{}) error
}
