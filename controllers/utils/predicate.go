package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// Name Predicate return a predicate the filter events produced
// by resources that matches the given name
func NamePredicate(name string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == name
	})
}

// Namespace Predicate return a predicate the filter events produced
// by resources that matches the given namespace
func NamespacePredicate(namespace string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == namespace
	})
}

// EventTypePredicate return a predicate to filter events based on their
// respective event type. This helper allows for the selection of multiple
// types resulting in a predicate that can filter in more than a single event
// type
func EventTypePredicate(create, update, del, generic bool) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(_ event.CreateEvent) bool {
			return create
		},
		UpdateFunc: func(_ event.UpdateEvent) bool {
			return update
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			return del
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return generic
		},
	}
}
