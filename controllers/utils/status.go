package utils

import (
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sets ConditionValidated condition as False in mirrorpeer status.Conditions with provided statusMessage, reason
func SetValidatedFalseCondition(conditions *[]metav1.Condition, observedGeneration int64, statusMessage, conditionReason string) {
	metaapi.SetStatusCondition(conditions, metav1.Condition{
		Message:            statusMessage,
		Type:               multiclusterv1alpha1.ConditionValidated,
		Reason:             conditionReason,
		ObservedGeneration: observedGeneration,
		Status:             metav1.ConditionFalse,
	})
}

// sets ConditionValidated condition as True in mirrorpeer status.Conditions with provided statusMessage, reason
func SetValidatedTrueCondition(conditions *[]metav1.Condition, observedGeneration int64, statusMessage, conditionReason string) {
	metaapi.SetStatusCondition(conditions, metav1.Condition{
		Message:            statusMessage,
		Type:               multiclusterv1alpha1.ConditionValidated,
		Reason:             conditionReason,
		ObservedGeneration: observedGeneration,
		Status:             metav1.ConditionTrue,
	})
}

// sets ConditionConfigured condition as False in mirrorpeer status.Conditions with provided statusMessage, reason
func SetConfiguredFalseCondition(conditions *[]metav1.Condition, observedGeneration int64, statusMessage, conditionReason string) {
	metaapi.SetStatusCondition(conditions, metav1.Condition{
		Message:            statusMessage,
		Type:               multiclusterv1alpha1.ConditionConfigured,
		Reason:             conditionReason,
		ObservedGeneration: observedGeneration,
		Status:             metav1.ConditionFalse,
	})
}

// sets ConditionConfigured condition as True in mirrorpeer status.Conditions with provided statusMessage, reason
func SetConfiguredTrueCondition(conditions *[]metav1.Condition, observedGeneration int64, statusMessage, conditionReason string) {
	metaapi.SetStatusCondition(conditions, metav1.Condition{
		Message:            statusMessage,
		Type:               multiclusterv1alpha1.ConditionConfigured,
		Reason:             conditionReason,
		ObservedGeneration: observedGeneration,
		Status:             metav1.ConditionTrue,
	})
}

// sets ConditionDeleted condition as False in mirrorpeer status.Conditions with provided statusMessage, reason
func SetDeletedFalseCondition(conditions *[]metav1.Condition, observedGeneration int64, statusMessage, conditionReason string) {
	metaapi.SetStatusCondition(conditions, metav1.Condition{
		Message:            statusMessage,
		Type:               multiclusterv1alpha1.ConditionDeleted,
		Reason:             conditionReason,
		ObservedGeneration: observedGeneration,
		Status:             metav1.ConditionFalse,
	})
}
