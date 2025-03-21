package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ODFInfoConfigMapName                = "odf-info"
	ConfigMapResourceType               = "ConfigMap"
	ClientInfoConfigMapName             = "odf-client-info"
	StorageClusterPeerNameAnnotationKey = "ocs.openshift.io/storage-cluster-peer"
)

// FetchConfigMap fetches a ConfigMap with a given name from a given namespace
func FetchConfigMap(ctx context.Context, c client.Client, name, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, configMap)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to fetch ConfigMap %s in namespace %s: %v", name, namespace, err)
	}
	return configMap, nil
}

// GetODFInfoConfigMap fetches the odf-info ConfigMap from the given namespace. This will only work on the managed cluster
func GetODFInfoConfigMap(ctx context.Context, c client.Client, namespace string) (*corev1.ConfigMap, error) {
	return FetchConfigMap(ctx, c, ODFInfoConfigMapName, namespace)
}

func FetchClientInfoConfigMap(ctx context.Context, c client.Client, currentNamespace string) (*corev1.ConfigMap, error) {
	return FetchConfigMap(ctx, c, ClientInfoConfigMapName, currentNamespace)
}

func DecodeConfigMap(objJson []byte) (*corev1.ConfigMap, error) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add to scheme in decode config map: %w", err)
	}
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	obj, _, err := decoder.Decode(objJson, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode objJson: %w", err)
	}

	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("decoded object is not a ConfigMap")
	}

	return configMap, nil
}
