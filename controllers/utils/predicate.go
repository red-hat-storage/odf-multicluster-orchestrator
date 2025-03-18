package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var InternalSecretPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return IsSecretInternal(e.Object)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return IsSecretInternal(e.Object)
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return (IsSecretInternal(e.ObjectOld) && IsSecretInternal(e.ObjectNew))
	},
	GenericFunc: func(_ event.GenericEvent) bool {
		return false
	},
}

var SourceSecretPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return IsSecretSource(e.Object)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return IsSecretSource(e.Object)
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return (IsSecretSource(e.ObjectOld) && IsSecretSource(e.ObjectNew))
	},
	GenericFunc: func(_ event.GenericEvent) bool {
		return false
	},
}
