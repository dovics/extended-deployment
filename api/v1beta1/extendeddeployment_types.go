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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// InPlaceUpdateStrategy defines the strategies for in-place update.
type InPlaceUpdateStrategy struct {
	// GracePeriodSeconds is the timespan between set Pod status to not-ready and update images in Pod spec
	// when in-place update a Pod.
	// +kubebuilder:default:=5
	// +optional
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`
}

// RolloutStrategyType is a type that defines the strategy for rolling out updates.
type RolloutStrategyType string

// UpdateStrategyType defines the type of update strategy for pods.
type UpdateStrategyType string

// SubsetType defines the type of pod subsets involved in the update strategy.
type SubsetType string

const (
	//Beta策略
	BetaRolloutStrategyType RolloutStrategyType = "Beta"
	//分组策略
	GroupRolloutStrategyType RolloutStrategyType = "Group"

	// DeleteAllFirstUpdateStrategyTypeUpdateStrategyType indicates that we kill all existing
	// pods before creating new ones
	DeleteAllFirstUpdateStrategyType UpdateStrategyType = "DeleteAllFirst"
	// RecreateUpdateStrategyType indicates that we always delete Pod and create new Pod
	// during Pod update, which is the default behavior.
	RecreateUpdateStrategyType UpdateStrategyType = "ReCreate"
	// RollingUpdateStrategyType indicates that we always delete Pod and create new Pod
	// during Pod update, which is the default behavior.
	RollingUpdateStrategyType UpdateStrategyType = "RollingUpdate"
	// InPlaceOnlyUpdateStrategyType indicates that we must in-place update Pod. Currently,
	// only image update of pod spec is allowed. Any other changes to the pod spec will refused.
	InPlaceOnlyUpdateStrategyType UpdateStrategyType = "InPlaceOnly"
	// InPlaceIfPossibleUpdateStrategyType indicates that we try to in-place update Pod instead of
	// recreating Pod when possible. Currently, only image update of pod spec is allowed. Any other changes to the pod
	// spec will fall back to ReCreate CloneSetUpdateStrategyType where pod will be recreated.
	InPlaceIfPossibleUpdateStrategyType UpdateStrategyType = "InPlaceIfPossible"

	//支持的subset类型
	InPlaceSetSubsetType SubsetType = "InplaceSet"
	ReplicaSetSubsetType SubsetType = "ReplicaSet"
	DeploymentSubsetType SubsetType = "Deployment"
)

const (
	// DefaultExtendedDeploymentUniqueLabelKey extended-deployment的pod模板标识
	DefaultExtendedDeploymentUniqueLabelKey = "extended-pod-template-hash"
)

type UpdateStrategy struct {
	// 升级策略
	// +kubebuilder:validation:Enum=DeleteAllFirst;ReCreate;RollingUpdate;InPlaceIfPossible
	// +kubebuilder:default:=ReCreate
	// +optional
	Type UpdateStrategyType `json:"type,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=5
	// +optional
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`

	// Rolling update config params,Present only if UpdateStrategyType = "RollingUpdate"
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
}

type Strategy struct {
	// 分组发布的步长控制
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	BatchSize int32 `json:"batchSize"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=5
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`
	//是否等待确认
	// +kubebuilder:default:=false
	// +optional
	NeedWaitingForConfirm bool `json:"needWaitingForConfirm"`
	//是否开启第一次等待确认
	// +kubebuilder:default:=false
	// +optional
	NeedFirstConfirm bool `json:"needFirstConfirm"`
	//等待就绪超时时间
	// +kubebuilder:default:=0
	// +optional
	SyncWaitTimeout int32 `json:"syncWaitTimeout,omitempty"`
	//发布策略
	// +kubebuilder:validation:Enum=Beta;Group
	// +kubebuilder:default:=Beta
	// +optional
	RolloutStrategy RolloutStrategyType `json:"rolloutStrategy"`
	//更新阻止
	// +optional
	Pause bool `json:"pause,omitempty"`

	//故障转移策略，pod pending起不来，并且因为资源不足
	// +optional
	AutoReschedule *AutoReschedule `json:"autoReschedule,omitempty"`
	// 升级策略
	// +optional
	UpdateStrategy *UpdateStrategy `json:"updateStrategy,omitempty"`
}

type AutoReschedule struct {
	// +kubebuilder:default:=false
	// +optional
	Enable bool `json:"enable"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=60
	// +optional
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// ExtendedDeploymentSpec defines the desired state of ExtendedDeployment.
type ExtendedDeploymentSpec struct {
	//保存的历史rs记录
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=5
	// +optional
	HistoryLimit int32 `json:"historyLimit,omitempty"`
	//rs的selector 匹配
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	//关联的资源类型
	// +optional
	SubsetType SubsetType `json:"subsetType,omitempty"`
	//extended-deployment更新策略
	// +optional
	Strategy Strategy `json:"strategy"`

	// Template describes the pods that will be created.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Template v1.PodTemplateSpec `json:"template"`
	// 分区总副本数
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=0
	Replicas *int32 `json:"replicas,omitempty"`
	//分区选择
	// +optional
	Regions []Region `json:"regions,omitempty"`
	//分区重载
	// +optional
	Overriders []Overriders `json:"overriders,omitempty"`
}

// Placement represents the rule for select clusters.
type Region struct {
	//分区名称
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	//分区期望副本数
	Replicas *intstr.IntOrString `json:"replicas,omitempty"`
}

// Overriders offers various alternatives to represent the override rules.
//
// If more than one alternatives exist, they will be applied with following order:
// - ImageOverrider
// - Plaintext
type Overriders struct {
	//分区名字
	Name string `json:"name,omitempty"`
	// Plaintext represents override rules defined with plaintext overriders.
	// +optional
	Plaintext []PlaintextOverrider `json:"overriders,omitempty"`
}

// PlaintextOverrider is a simple overrider that overrides target fields
// according to path, operator and value.
type PlaintextOverrider struct {
	// Path indicates the path of target field
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Operator indicates the operation on target field.
	// Available operators are: add, update and remove.
	// +kubebuilder:validation:Enum=add;remove;replace
	Operator string `json:"op"`

	// Value to be applied to target field.
	// Must be empty when operator is Remove.
	// +optional
	Value apiextensionsv1.JSON `json:"value,omitempty"`
}

// ExtendedDeploymentStatus defines the observed state of ExtendedDeployment.
type ExtendedDeploymentStatus struct {
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// +optional
	LabelSelector string `json:"labelSelector,omitempty"`
	// +optional
	Regions []RegionStatus `json:"regions,omitempty"`
	// Represents the latest available observations of a deployment's current state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []appsv1.DeploymentCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,6,rep,name=conditions"`
	// Count of hash collisions for the Deployment. The Deployment controller uses this
	// field as a collision avoidance mechanism when it needs to create the name for the
	// newest ReplicaSet.
	// +optional
	CollisionCount *int32 `json:"collisionCount,omitempty" protobuf:"varint,8,opt,name=collisionCount"`

	// 分区统计，regionName: availableReplicas/desiredReplicas
	// +optional
	Overview string `json:"overview,omitempty"`

	// 上一次term开始的时间
	// +optional
	LastTermStartTime string `json:"lastTermStartTime,omitempty"`
}

// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.labelSelector
// +kubebuilder:resource:shortName={extendeddeploy,ed}
// +kubebuilder:printcolumn:name="RegionOverview",type="string",JSONPath=".status.overview",description="Overview is a string that describe all regions information, the format is \"RegionName1: ActualReplicas/DesiredReplicas\""
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="CreationTimestamp is a timestamp representing the server time when this object was created. It is not guaranteed to be set in happens-before order across separate operations. Clients may not set this value. It is represented in RFC3339 form and is in UTC."

// ExtendedDeployment is the Schema for the extendeddeployments API.
type ExtendedDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExtendedDeploymentSpec   `json:"spec,omitempty"`
	Status ExtendedDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExtendedDeploymentList contains a list of ExtendedDeployment.
type ExtendedDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExtendedDeployment `json:"items"`
}

type RegionStatus struct {
	RegionName string `json:"region,omitempty"`
	// 所有InplaceSet副本数总和
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// Total number of non-terminated pods targeted by this deployment that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty" protobuf:"varint,3,opt,name=updatedReplicas"`

	// readyReplicas is the number of pods targeted by this Deployment with a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty" protobuf:"varint,7,opt,name=readyReplicas"`
	// Total number of available pods (ready for at least minReadySeconds) targeted by this deployment.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty" protobuf:"varint,4,opt,name=availableReplicas"`

	// Total number of unavailable pods targeted by this deployment. This is the total number of
	// pods that are still required for the deployment to have 100% available capacity. They may
	// either be pods that are running but not yet available or pods that still have not been created.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty" protobuf:"varint,5,opt,name=unavailableReplicas"`
}

func init() {
	SchemeBuilder.Register(&ExtendedDeployment{}, &ExtendedDeploymentList{})
}
