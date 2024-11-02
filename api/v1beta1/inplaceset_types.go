/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// InPlaceUpdateReady must be added into template.spec.readinessGates when pod podUpdatePolicy
	// is InPlaceIfPossible or InPlaceOnly. The condition in podStatus will be updated to False before in-place
	// updating and updated to True after the update is finished. This ensures pod to remain at NotReady state while
	// in-place update is happening.
	InPlaceUpdateReady v1.PodConditionType = "InPlaceUpdateReady"
)

type InplaceSetUpdateStrategy struct {
	// Type indicates the type of the CloneSetUpdateStrategy.
	// Default is ReCreate.
	// +kubebuilder:validation:Enum=ReCreate;InPlaceIfPossible
	// +kubebuilder:default:=ReCreate
	// +optional
	Type UpdateStrategyType `json:"type,omitempty"`

	// InPlaceUpdateStrategy contains strategies for in-place update.
	// +optional
	InPlaceUpdateStrategy *InPlaceUpdateStrategy `json:"inPlaceUpdateStrategy,omitempty"`
}

// InplaceSetSpec defines the desired state of InplaceSet
type InplaceSetSpec struct {
	// Replicas is the number of desired replicas.
	// This is a pointer to distinguish between explicit zero and unspecified.
	// Defaults to 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller/#what-is-a-replicationcontroller
	// +kubebuilder:default:=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +kubebuilder:default:=10
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty" protobuf:"varint,4,opt,name=minReadySeconds"`

	// UpdateStrategy indicates the UpdateStrategy that will be employed to
	// update Pods in the inplaceset when a revision is made to Template.
	// +optional
	UpdateStrategy InplaceSetUpdateStrategy `json:"updateStrategy,omitempty"`

	// Selector is a label query over pods that should match the replica count.
	// Label keys and values that must match in order to be controlled by this replica set.
	// It must match the pod template's labels.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,2,opt,name=selector"`

	// Template is the object that describes the pod that will be created if
	// insufficient replicas are detected.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller#pod-template
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`
}

// InplaceSetStatus defines the observed state of InplaceSet
type InplaceSetStatus struct {
	// Replicas is the most recently observed number of replicas.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller/#what-is-a-replicationcontroller
	// +optional
	Replicas int32 `json:"replicas" protobuf:"varint,1,opt,name=replicas"`

	// The number of pods that have labels matching the labels of the pod template of the replicaset.
	// +optional
	FullyLabeledReplicas int32 `json:"fullyLabeledReplicas,omitempty" protobuf:"varint,2,opt,name=fullyLabeledReplicas"`

	// the status of inplace update
	// +optional
	InplaceUpdateStatus *InplaceUpdateStatus `json:"inplaceUpdateStatus,omitempty"`

	// readyReplicas is the number of pods targeted by this ReplicaSet with a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas" protobuf:"varint,4,opt,name=readyReplicas"`

	// The number of available replicas (ready for at least minReadySeconds) for this replica set.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas" protobuf:"varint,5,opt,name=availableReplicas"`

	// ObservedGeneration reflects the generation of the most recently observed ReplicaSet.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`

	// Represents the latest available observations of a replica set's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []appsv1.ReplicaSetCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,6,rep,name=conditions"`
}

type InplaceUpdateStatus struct {
	// +optional
	ReadyReplicas int32 `json:"readyReplicas" protobuf:"varint,4,opt,name=readyReplicas"`

	// The number of available replicas (ready for at least minReadySeconds) for this replica set.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas" protobuf:"varint,5,opt,name=availableReplicas"`

	// for checking inplaceset update version
	// +optional
	PodTemplateHash string `json:"podTemplateHash"`
}

// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.labelSelector
// +kubebuilder:resource:shortName={is, ips}
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".spec.replicas",description="Replicas is the most recently observed number of replicas."
// +kubebuilder:printcolumn:name="Current",type="integer",JSONPath=".status.replicas",description="Replicas is the most recently observed number of replicas."
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="readyReplicas is the number of pods targeted by this ReplicaSet with a Ready Condition."
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableReplicas",description="The number of available replicas (ready for at least minReadySeconds) for this replica set."
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="CreationTimestamp is a timestamp representing the server time when this object was created. It is not guaranteed to be set in happens-before order across separate operations. Clients may not set this value. It is represented in RFC3339 form and is in UTC."

// InplaceSet is the Schema for the inplacesets API
type InplaceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InplaceSetSpec   `json:"spec,omitempty"`
	Status InplaceSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InplaceSetList contains a list of InplaceSet
type InplaceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InplaceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InplaceSet{}, &InplaceSetList{})
}
