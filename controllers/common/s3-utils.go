package common

import "os"

const (
	RamenHubNamespace  = "openshift-dr-system"
	BucketGenerateName = "odrbucket"
)

func GetEnv(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
