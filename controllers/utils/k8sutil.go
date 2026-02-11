package utils

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func AddLabel(obj metav1.Object, key string, value string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
		obj.SetLabels(labels)
	}
	if oldValue, exist := labels[key]; !exist || oldValue != value {
		labels[key] = value
		return true
	}
	return false
}
