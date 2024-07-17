package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FetchConfigMap fetches a ConfigMap with a given name from a given namespace
func FetchConfigMap(ctx context.Context, c client.Client, name, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ConfigMap %s in namespace %s: %v", name, namespace, err)
	}
	return configMap, nil
}
