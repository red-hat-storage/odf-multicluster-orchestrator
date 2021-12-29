package handlers

import (
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
)

type SecretExchangeController struct {
	hubKubeClient        kubernetes.Interface
	hubSecretLister      corev1lister.SecretLister
	spokeKubeClient      kubernetes.Interface
	spokeSecretLister    corev1lister.SecretLister
	spokeConfigMapLister corev1lister.ConfigMapLister
	clusterName          string
	agentNamespace       string
	recorder             events.Recorder
	spokeKubeConfig      *rest.Config
}
