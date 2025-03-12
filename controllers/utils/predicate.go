package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SourceOrDestinationPredicate is a predicate that matches events that check whether a secret has required source or destination labels or not.
var SourceOrDestinationPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return IsSecretSource(e.Object) || IsSecretInternal(e.Object)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return IsSecretSource(e.Object) || IsSecretDestination(e.Object)
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return (IsSecretSource(e.ObjectOld) && IsSecretSource(e.ObjectNew)) ||
			(IsSecretDestination(e.ObjectOld) && IsSecretDestination(e.ObjectNew)) ||
			IsSecretInternal(e.ObjectOld) && IsSecretInternal(e.ObjectNew)
	},
	GenericFunc: func(_ event.GenericEvent) bool {
		return false
	},
}

var SourcePredicate = predicate.Funcs{
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

// ComposePredicates will compose a variable number of predicates and return a predicate that
// will allow events that are allowed by any of the given predicates.
func ComposePredicates(predicates ...predicate.Predicate) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Create(e) {
					return true
				}
			}
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Delete(e) {
					return true
				}
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Update(e) {
					return true
				}
			}
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Generic(e) {
					return true
				}
			}
			return false
		},
	}
}
