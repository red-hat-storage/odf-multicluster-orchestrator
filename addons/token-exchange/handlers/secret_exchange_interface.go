package handlers

import (
	corev1 "k8s.io/api/core/v1"
)

type SecretExchangeInerface interface {
	GetObjectFilter(interface{}) bool
	GenerateBlueSecret(string, string, string, string) (*corev1.Secret, error)
}
