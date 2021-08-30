package common

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

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
