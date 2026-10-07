package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetClientFromConfig returns a controller-runtime Client for a rest.Config
func GetClientFromConfig(config *rest.Config, scheme *runtime.Scheme) (client.Client, error) {
	cl, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// GetClientConfig returns the rest.Config for a kubeconfig file
func GetClientConfig(kubeConfigFile string) (*rest.Config, error) {
	clientconfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigFile)
	if err != nil {
		return nil, fmt.Errorf("get clientconfig failed: %w", err)
	}
	return clientconfig, nil
}
