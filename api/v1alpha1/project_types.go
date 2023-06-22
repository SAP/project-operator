/*
SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectSpec defines the desired state of Project.
type ProjectSpec struct {
	// AdminUsers contains users that will be granted admin privileges on the project namespace.
	AdminUsers []string `json:"adminUsers,omitempty"`
	// AdminGroups contains groups that will be granted admin privileges on the project namespace.
	AdminGroups []string `json:"adminGroups,omitempty"`
	// ViewerUsers contains users that will be granted display privileges on the project namespace.
	ViewerUsers []string `json:"viewerUsers,omitempty"`
	// ViewerGroups contains groups that will be granted display privileges on the project namespace.
	ViewerGroups []string `json:"viewerGroups,omitempty"`
	// Additional labels that should be set on the on the project namespace
	Labels map[string]string `json:"labels,omitempty"`
	// Additional annotations that should be set on on the project namespace
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ProjectStatus defines the observed state of Project.
type ProjectStatus struct {
	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Timestamp when this resource was last observed
	LastObservedAt *metav1.Time `json:"lastObservedAt,omitempty"`
	// Conditions
	Conditions []Condition `json:"conditions,omitempty"`
	// Overall resource state
	State State `json:"state,omitempty"`
	// Project namespace
	Namespace string `json:"namespace,omitempty"`
}

// Project Condition
type Condition struct {
	// Condition type (such as 'Ready', ...)
	Type ConditionType `json:"type"`
	// Condition status (one of True, False, Unknown)
	//+kubebuilder:validation:Enum=True;False;Unknown
	Status ConditionStatus `json:"status"`
	// Timestamp of the last change of ConditionStatus
	//+optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a programmatically usable identifier explaining the current ConditionStatus.
	//+optional
	Reason string `json:"reason,omitempty"`
	// Message is a human readable explanation of the current ConditionStatus.
	//+optional
	Message string `json:"message,omitempty"`
}

// Condition type. Currently, only the 'Ready' type is used.
type ConditionType string

const (
	// Condition type representing the 'Ready' condition
	ConditionTypeReady ConditionType = "Ready"
)

// Condition Status. Can be one of 'True', 'False', 'Unknown'.
type ConditionStatus string

const (
	// Condition status 'True'
	ConditionTrue ConditionStatus = "True"
	// Condition status 'False'
	ConditionFalse ConditionStatus = "False"
	// Condition status 'Unknown'
	ConditionUnknown ConditionStatus = "Unknown"
)

// Project state. Can be one of 'Ready', 'Processing', 'Error', 'Deleting'.
type State string

const (
	// Project state 'Ready'
	StateReady State = "Ready"
	// Project state 'Processing'
	StateProcessing State = "Processing"
	// Project state 'Error'
	StateError State = "Error"
	// Project state 'DeletionBlocked'
	StateDeletionBlocked State = "DeletionBlocked"
	// Project state 'Deleting'
	StateDeleting State = "Deleting"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Project is the Schema for the projects API.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ProjectSpec `json:"spec,omitempty"`
	// +kubebuilder:default={"observedGeneration":-1}
	Status ProjectStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ProjectList contains a list of Project.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}

// Set condition on ProjectStatus
func (s *ProjectStatus) SetCondition(condType ConditionType, condStatus ConditionStatus, reason string, message string) *Condition {
	var cond *Condition
	for i := 0; i < len(s.Conditions); i++ {
		if s.Conditions[i].Type == condType {
			cond = &s.Conditions[i]
			break
		}
	}
	if cond == nil {
		s.Conditions = append(s.Conditions, Condition{Type: condType})
		cond = &s.Conditions[len(s.Conditions)-1]
	}
	if condStatus != cond.Status {
		cond.Status = condStatus
		cond.LastTransitionTime = &[]metav1.Time{metav1.Now()}[0]
	}
	cond.Reason = reason
	cond.Message = message
	return cond
}

// Set 'Ready' condition and State on ProjectStatus; provided state will be equally stored as condition reason.
func (s *ProjectStatus) SetReadyCondition(condStatus ConditionStatus, state State, message string) {
	s.SetCondition(ConditionTypeReady, condStatus, string(state), message)
	s.State = state
}
