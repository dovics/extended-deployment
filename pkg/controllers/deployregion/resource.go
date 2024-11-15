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
package deployregion

import (
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// NodeSummary represents the summary of nodes status in a specific cluster.
type NodeSummary struct {
	// TotalNum is the total number of nodes in the cluster.
	// +optional
	TotalNum int32 `json:"totalNum,omitempty"`
	// ReadyNum is the number of ready nodes in the cluster.
	// +optional
	ReadyNum int32 `json:"readyNum,omitempty"`
}

// ResourceSummary represents the summary of resources in the member cluster.
type ResourceSummary struct {
	// Allocatable represents the resources of a cluster that are available for scheduling.
	// Total amount of allocatable resources on all nodes.
	// +optional
	Allocatable corev1.ResourceList `json:"allocatable,omitempty"`
	// Allocating represents the resources of a cluster that are pending for scheduling.
	// Total amount of required resources of all Pods that are waiting for scheduling.
	// +optional
	Allocating corev1.ResourceList `json:"allocating,omitempty"`
	// Allocated represents the resources of a cluster that have been scheduled.
	// Total amount of required resources of all Pods that have been scheduled to nodes.
	// +optional
	Allocated corev1.ResourceList `json:"allocated,omitempty"`
}

// ReplicaRequirements represents the requirements required by each replica.
type ReplicaRequirements struct {
	// ResourceRequest represents the resources required by each replica.
	// +optional
	ResourceRequest corev1.ResourceList `json:"resourceRequest,omitempty"`
}

// Resource is a collection of compute resource.
type Resource struct {
	MilliCPU         int64
	Memory           int64
	EphemeralStorage int64
	AllowedPodNumber int64
}

// EmptyResource creates a empty resource object and returns.
func EmptyResource() *Resource {
	return &Resource{}
}

// Add is used to add two resources.
func (r *Resource) Add(rl corev1.ResourceList) {
	if r == nil {
		return
	}

	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			r.MilliCPU += rQuant.MilliValue()
		case corev1.ResourceMemory:
			r.Memory += rQuant.Value()
		case corev1.ResourcePods:
			r.AllowedPodNumber += rQuant.Value()
		case corev1.ResourceEphemeralStorage:
			r.EphemeralStorage += rQuant.Value()
		}
	}
}

func (r *Resource) AddResource(a *Resource) {
	r.MilliCPU += a.MilliCPU
	r.Memory += a.Memory
	r.EphemeralStorage += a.EphemeralStorage
	r.AllowedPodNumber += a.AllowedPodNumber
}

func (r *Resource) SubResource(a *Resource) {
	r.MilliCPU -= a.MilliCPU
	r.Memory -= a.Memory
	r.EphemeralStorage -= a.EphemeralStorage
	r.AllowedPodNumber -= a.AllowedPodNumber
}

// Sub is used to subtract two resources.
// Return error when the minuend is less than the subtrahend.
func (r *Resource) Sub(rl corev1.ResourceList) error {
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			cpu := rQuant.MilliValue()
			if r.MilliCPU < cpu {
				return fmt.Errorf("cpu difference is less than 0, remain %d, got %d", r.MilliCPU, cpu)
			}
			r.MilliCPU -= cpu
		case corev1.ResourceMemory:
			mem := rQuant.Value()
			if r.Memory < mem {
				return fmt.Errorf("memory difference is less than 0, remain %d, got %d", r.Memory, mem)
			}
			r.Memory -= mem
		case corev1.ResourcePods:
			pods := rQuant.Value()
			if r.AllowedPodNumber < pods {
				return fmt.Errorf("allowed pod difference is less than 0, remain %d, got %d", r.AllowedPodNumber, pods)
			}
			r.AllowedPodNumber -= pods
		case corev1.ResourceEphemeralStorage:
			ephemeralStorage := rQuant.Value()
			if r.EphemeralStorage < ephemeralStorage {
				return fmt.Errorf("allowed storage number difference is less than 0, remain %d, got %d", r.EphemeralStorage, ephemeralStorage)
			}
			r.EphemeralStorage -= ephemeralStorage
		}
	}
	return nil
}

// SetMaxResource compares with ResourceList and takes max value for each Resource.
func (r *Resource) SetMaxResource(rl corev1.ResourceList) {
	if r == nil {
		return
	}

	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			if cpu := rQuant.MilliValue(); cpu > r.MilliCPU {
				r.MilliCPU = cpu
			}
		case corev1.ResourceMemory:
			if mem := rQuant.Value(); mem > r.Memory {
				r.Memory = mem
			}
		case corev1.ResourceEphemeralStorage:
			if ephemeralStorage := rQuant.Value(); ephemeralStorage > r.EphemeralStorage {
				r.EphemeralStorage = ephemeralStorage
			}
		case corev1.ResourcePods:
			if pods := rQuant.Value(); pods > r.AllowedPodNumber {
				r.AllowedPodNumber = pods
			}
		}
	}
}

// ResourceList returns a resource list of this resource.
func (r *Resource) ResourceList() corev1.ResourceList {
	result := corev1.ResourceList{}
	if r.MilliCPU > 0 {
		result[corev1.ResourceCPU] = *resource.NewMilliQuantity(r.MilliCPU, resource.DecimalSI)
	}
	if r.Memory > 0 {
		result[corev1.ResourceMemory] = *resource.NewQuantity(r.Memory, resource.BinarySI)
	}
	if r.EphemeralStorage > 0 {
		result[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(r.EphemeralStorage, resource.BinarySI)
	}
	if r.AllowedPodNumber > 0 {
		result[corev1.ResourcePods] = *resource.NewQuantity(r.AllowedPodNumber, resource.DecimalSI)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// MaxDivided returns how many replicas that the resource can be divided.
func (r *Resource) MaxDivided(rl corev1.ResourceList) int64 {
	res := int64(math.MaxInt64)
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			if cpu := rQuant.MilliValue(); cpu > 0 {
				res = MinInt64(res, r.MilliCPU/cpu)
			}
		case corev1.ResourceMemory:
			if mem := rQuant.Value(); mem > 0 {
				res = MinInt64(res, r.Memory/mem)
			}
		case corev1.ResourceEphemeralStorage:
			if ephemeralStorage := rQuant.Value(); ephemeralStorage > 0 {
				res = MinInt64(res, r.EphemeralStorage/ephemeralStorage)
			}
		}
	}
	res = MinInt64(res, r.AllowedPodNumber)
	return res
}

// LessEqual returns whether all dimensions of resources in r are less than or equal with that of rr.
func (r *Resource) LessEqual(rr *Resource) bool {
	lessEqualFunc := func(l, r int64) bool {
		return l <= r
	}

	if !lessEqualFunc(r.MilliCPU, rr.MilliCPU) {
		return false
	}
	if !lessEqualFunc(r.Memory, rr.Memory) {
		return false
	}
	if !lessEqualFunc(r.EphemeralStorage, rr.EphemeralStorage) {
		return false
	}
	if !lessEqualFunc(r.AllowedPodNumber, rr.AllowedPodNumber) {
		return false
	}

	return true
}

// AddPodTemplateRequest add the effective request resource of a pod template to the origin resource.
// If pod container limits are specified, but requests are not, default requests to limits.
// The code logic is almost the same as kubernetes.
// https://github.com/kubernetes/kubernetes/blob/f7cdbe2c96cc12101226686df9e9819b4b007c5c/pkg/apis/core/v1/defaults.go#L147-L181
func (r *Resource) AddPodTemplateRequest(podSpec *corev1.PodSpec) *Resource {
	// DeepCopy first because we may modify the Resources.Requests field.
	podSpec = podSpec.DeepCopy()
	for i := range podSpec.Containers {
		// set requests to limits if requests are not specified, but limits are
		if podSpec.Containers[i].Resources.Limits != nil {
			if podSpec.Containers[i].Resources.Requests == nil {
				podSpec.Containers[i].Resources.Requests = make(corev1.ResourceList)
			}
			for key, value := range podSpec.Containers[i].Resources.Limits {
				if _, exists := podSpec.Containers[i].Resources.Requests[key]; !exists {
					podSpec.Containers[i].Resources.Requests[key] = value.DeepCopy()
				}
			}
		}
	}
	for i := range podSpec.InitContainers {
		if podSpec.InitContainers[i].Resources.Limits != nil {
			if podSpec.InitContainers[i].Resources.Requests == nil {
				podSpec.InitContainers[i].Resources.Requests = make(corev1.ResourceList)
			}
			for key, value := range podSpec.InitContainers[i].Resources.Limits {
				if _, exists := podSpec.InitContainers[i].Resources.Requests[key]; !exists {
					podSpec.InitContainers[i].Resources.Requests[key] = value.DeepCopy()
				}
			}
		}
	}
	return r.AddPodRequest(podSpec)
}

// AddPodRequest add the effective request resource of a pod to the origin resource.
// The Pod's effective request is the higher of:
// - the sum of all app containers(spec.Containers) request for a resource.
// - the effective init containers(spec.InitContainers) request for a resource.
// The effective init containers request is the highest request on all init containers.
func (r *Resource) AddPodRequest(podSpec *corev1.PodSpec) *Resource {
	for _, container := range podSpec.Containers {
		r.Add(container.Resources.Requests)
	}
	for _, container := range podSpec.InitContainers {
		r.SetMaxResource(container.Resources.Requests)
	}
	// If Overhead is being utilized, add to the total requests for the pod.
	// We assume the EnablePodOverhead feature gate of member cluster is set (it is on by default since 1.18).
	if podSpec.Overhead != nil {
		r.Add(podSpec.Overhead)
	}
	return r
}

// AddResourcePods adds pod resources into the Resource.
// Notice that a pod request resource list does not contain a request for pod resources,
// this function helps to add the pod resources.
func (r *Resource) AddResourcePods(pods int64) {
	r.Add(corev1.ResourceList{
		corev1.ResourcePods: *resource.NewQuantity(pods, resource.DecimalSI),
	})
}

// MinInt64 returns the smaller of two int64 numbers.
func MinInt64(a, b int64) int64 {
	if a <= b {
		return a
	}
	return b
}
