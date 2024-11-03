package adapter

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

// InPlaceUpdateStrategy defines the strategies for in-place update.
type InPlaceUpdateStrategy struct {
	// GracePeriodSeconds is the timespan between set Pod status to not-ready and update images in Pod spec
	// when in-place update a Pod.
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`
}

type UpdateStrategy struct {

	// Type indicates the type of the CloneSetUpdateStrategy.
	// Default is ReCreate.
	// +kubebuilder:validation:Enum=ReCreate;InPlaceIfPossible
	Type v1beta1.UpdateStrategyType `json:"type,omitempty"`

	// InPlaceUpdateStrategy contains strategies for in-place update.
	InPlaceUpdateStrategy *InPlaceUpdateStrategy `json:"inPlaceUpdateStrategy,omitempty"`
}

// SubsetSpec defines the desired state of InplaceSet
type SubsetSpec struct {
	// Replicas is the number of desired replicas.
	// This is a pointer to distinguish between explicit zero and unspecified.
	// Defaults to 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller/#what-is-a-replicationcontroller
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty" protobuf:"varint,4,opt,name=minReadySeconds"`

	// UpdateStrategy indicates the UpdateStrategy that will be employed to
	// update Pods in the inplaceset when a revision is made to Template.
	// +optional
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`

	//// Selector is a label query over pods that should match the replica count.
	//// Label keys and values that must match in order to be controlled by this replica set.
	//// It must match the pod template's labels.
	//// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,2,opt,name=selector"`

	// Template is the object that describes the pod that will be created if
	// insufficient replicas are detected.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller#pod-template
	// +optional
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`

	SubsetRef ResourceRef
}

// SubsetStatus defines the observed state of SubSet
type SubsetStatus struct {
	// Replicas is the most recently observed number of replicas.
	// More info: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller/#what-is-a-replicationcontroller
	Replicas int32 `json:"replicas" protobuf:"varint,1,opt,name=replicas"`

	// The number of pods that have labels matching the labels of the pod template of the replicaset.
	// +optional
	//FullyLabeledReplicas int32 `json:"fullyLabeledReplicas,omitempty" protobuf:"varint,2,opt,name=fullyLabeledReplicas"`

	// the status of inplace update
	// +optional
	InplaceUpdateStatus *InplaceUpdateStatus `json:"inplaceUpdateStatus,omitempty"`

	// readyReplicas is the number of pods targeted by this ReplicaSet with a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty" protobuf:"varint,4,opt,name=readyReplicas"`

	// The number of available replicas (ready for at least minReadySeconds) for this replica set.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty" protobuf:"varint,5,opt,name=availableReplicas"`

	// ObservedGeneration reflects the generation of the most recently observed ReplicaSet.
	// +optional
	//ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`

	// Represents the latest available observations of a replica set's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	//Conditions []appsv1.ReplicaSetCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,6,rep,name=conditions"`
}

type InplaceUpdateStatus struct {
	// +optional
	//ReadyReplicas int32 `json:"readyReplicas" protobuf:"varint,4,opt,name=readyReplicas"`

	// The number of available replicas (ready for at least minReadySeconds) for this replica set.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas" protobuf:"varint,5,opt,name=availableReplicas"`

	// for checking inplaceset update version
	// +optional
	PodTemplateHash string `json:"podTemplateHash"`
}

// ResourceRef stores the Subset resource it represents.
type ResourceRef struct {
	Resources []metav1.Object
}

type Subset struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Spec   SubsetSpec   `json:"spec,omitempty"`
	Status SubsetStatus `json:"status,omitempty"`
}

type SubsetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subset `json:"items"`
}

type RegionInfo struct {
	New  *Subset
	Olds []*Subset

	// 当前region状态
	// spec副本等于期望副本数
	ReplicasDesired bool
	// status可用副本数等于spec副本数
	// 在 replicasDesired 和 availableDesired 都为true时，表示当前region已饱和
	AvailableDesired bool

	Region             *v1beta1.DeployRegion
	DesiredReplicas    int32 // 实际期望的服务数（因为有资源不足自动调度，会修改配置副本数）
	ConfiguredReplicas int32 // 配置的副本数
}

// ControlInterface defines the interface that UnitedDeployment uses to list, create, update, and delete Subsets.
type ControlInterface interface {
	// QuerySubsetAndRegionInfo 查询subset和region信息
	QuerySubsetAndRegionInfo(cd *v1beta1.ExtendedDeployment, regionMatchLabels map[string]*v1beta1.DeployRegion) error
	// GetAllSubsets returns the subsets which are managed by the UnitedDeployment.
	GetAllSubsets(cd *v1beta1.ExtendedDeployment) ([]*Subset, error)
	// NewSubset return new rs, create subset if not found
	NewSubset(cd *v1beta1.ExtendedDeployment, ri *RegionInfo, replicas int32) (*Subset, error)
	// CreateSubset creates the subset depending on the inputs.
	CreateSubset(cd *v1beta1.ExtendedDeployment, dr *v1beta1.DeployRegion, replicas int32, revision string) (*Subset, error)
	// UpdateSubsetAnnotations updates the target subset with the input information.
	UpdateSubsetAnnotations(subSet *Subset, isInplaceUpdate bool) error
	// DeleteSubset is used to delete the input subset.
	DeleteSubset(cd *v1beta1.ExtendedDeployment, subset *Subset) error
	// ManageSubsets manage replicas of subsets according to expired replicas
	ManageSubsets(cd *v1beta1.ExtendedDeployment) (isReady bool, err error)
	// GetRegionInfo returns RegionInfo map
	GetRegionInfo() map[string]*RegionInfo
	// SetDeployKey set namespace/name of extendeddeployment
	SetDeployKey(string)
	// ClearUnused clear unused subsets
	ClearUnused(cd *v1beta1.ExtendedDeployment) error
	// UpdateSubsetStrategy update subset update strategy
	UpdateSubsetStrategy(cd *v1beta1.ExtendedDeployment) (err error)
	// ScaleSubset update subset replicas
	ScaleSubset(cd *v1beta1.ExtendedDeployment, ss *Subset, newReplicas int32) error
}

type SubSetsByCreationTimestamp []*Subset

func (o SubSetsByCreationTimestamp) Len() int      { return len(o) }
func (o SubSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o SubSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}

	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

type SubSetsByRevision []*Subset

func (o SubSetsByRevision) Len() int      { return len(o) }
func (o SubSetsByRevision) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o SubSetsByRevision) Less(i, j int) bool {
	revision1, err1 := utils.RevisionToInt64(o[i])
	revision2, err2 := utils.RevisionToInt64(o[j])
	if err1 != nil || err2 != nil || revision1 == revision2 {
		return SubSetsByCreationTimestamp(o).Less(i, j)
	}
	return revision1 < revision2
}
