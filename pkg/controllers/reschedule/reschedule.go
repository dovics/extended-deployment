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
package reschedule

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/controllers/deployregion"
	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

const (
	errorMsgMemDetect = "Insufficient memory"
	errorMsgCpuDetect = "Insufficient cpu"
)

type ScheduleInfo struct {
	regionsInfo   map[string]*scheduleRegionInfo
	failedRegions []string
}

type scheduleRegionInfo struct {
	name string

	configReplicas     int32 // Configured number of replicas
	scheduleReplicas   int32 // Current number of replicas scheduled into the region
	rescheduleReplicas int32 // Number of replicas planned to be scheduled into the region

	AllocatablePod int64 // Total number of allocatable pods
}

func NewScheduleRegionInfo(deployment *v1beta1.ExtendedDeployment) (*ScheduleInfo, error) {
	regionMap := make(map[string]*scheduleRegionInfo, len(deployment.Spec.Regions))
	for _, region := range deployment.Spec.Regions {
		replicas, err := extendeddeployment.ParseRegionReplicas(*deployment.Spec.Replicas, *region.Replicas)
		if err != nil {
			return nil, err
		}

		regionMap[region.Name] = &scheduleRegionInfo{
			name:               region.Name,
			configReplicas:     replicas,
			rescheduleReplicas: replicas,
		}

		resource, err := RequestResourceByRegion(deployment, region.Name)
		if err != nil {
			return nil, err
		}

		if !deployregion.IsSynced() {
			return nil, ErrNotSynced
		}

		regionMap[region.Name].AllocatablePod = deployregion.GetRegionAllocatablePodNum(region.Name, resource)
	}

	for _, region := range deployment.Status.Regions {
		regionMap[region.RegionName].scheduleReplicas = region.UpdatedReplicas
	}

	return &ScheduleInfo{
		regionsInfo: regionMap,
	}, nil
}

func (info ScheduleInfo) SetRescheduleAnnotation(obj metav1.Object) error {
	spec := makeEmptyAutoScheduleSpec(obj.GetGeneration())

	for regionName, region := range info.regionsInfo {
		spec.Infos[regionName] = &utils.ScheduleReplicas{
			Config:   region.configReplicas,
			Schedule: region.rescheduleReplicas - region.configReplicas,
		}
	}
	spec.FailureRegion = strings.Join(info.failedRegions, ",")

	return utils.SetAutoScheduleSpec(obj, spec)
}

func (info ScheduleInfo) RescheduleFailedRegions(regionsMap map[string]bool) error {
	/*
	 * 1. Iterate through the failed regions and count the number of replicas that need to be transferred.
	 * 2. Determine the resource requests for the partitions based on the non-failed InplaceSets.
	 * 3. Iterate through the nodes in the non-failed regions to count the number of pods that
	 *    can be accommodated and the total remaining resources.
	 * 4. Transfer the replicas to the optimal region one by one, reducing the region's resources after each transfer.
	 * 5. Write the transfer data as annotations.
	 */
	if len(regionsMap) == 0 {
		return nil
	}

	targetRegion := make([]string, 0)
	schedulePodNum := int32(0) // Number of replicas that need to be scheduled
	for name, region := range info.regionsInfo {
		if failed := regionsMap[name]; failed {
			schedulePodNum += region.configReplicas
			info.failedRegions = append(info.failedRegions, name)
			region.rescheduleReplicas = 0
			continue
		}

		targetRegion = append(targetRegion, name)
	}

	return info.reschedule(targetRegion, schedulePodNum)

	/* Resource constraints for partition failure transfers:
	 * 1. Insufficient resource transfer
	 *    If the generation changes, the automatic scheduling content for insufficient resources becomes invalid.
	 * 2. Failure scheduling
	 *    If the generation changes, the previous scheduling becomes invalid and needs to be deleted and rescheduled.
	 * 3. Both insufficient resources and failures exist?
	 *    During failure scheduling, automatic scheduling should not be performed.
	 */
}

func (info ScheduleInfo) RescheduleInsufficientPods(insufficientPods map[string][]*corev1.Pod) error {
	// Get the target regions that can be scheduled
	scheduleInRegions := make([]string, 0)
	for regionName, region := range info.regionsInfo {
		// Exists, indicating that there are pods pending due to insufficient resources,
		// cannot be transferred in
		if _, exists := insufficientPods[regionName]; exists {
			continue
		}
		// The region has replicas to be transferred out, indicating resource
		// insufficiency or failure, cannot be transferred in
		if region.rescheduleReplicas < region.configReplicas {
			continue
		}

		scheduleInRegions = append(scheduleInRegions, regionName)
	}

	for regionName, pods := range insufficientPods {
		desiredReplicas, nowSubsetReplicas := info.regionsInfo[regionName].configReplicas, info.regionsInfo[regionName].scheduleReplicas

		// Number of successfully started Pods
		info.regionsInfo[regionName].rescheduleReplicas = nowSubsetReplicas - int32(len(pods))

		// Number of Pods that failed to start or have not yet started
		out := desiredReplicas - info.regionsInfo[regionName].rescheduleReplicas
		if err := info.reschedule(scheduleInRegions, out); err != nil {
			return err
		}
	}

	return nil
}
func (info ScheduleInfo) reschedule(targetRegions []string, scheduleNum int32) error {
	// Select the optimal partition and transfer replicas one by one
	for i := int32(0); i < scheduleNum; i++ {
		var bestRegion *scheduleRegionInfo
		for _, region := range targetRegions {
			if bestRegion == nil {
				bestRegion = info.regionsInfo[region]
				continue
			}

			if info.regionsInfo[region].AllocatablePod > bestRegion.AllocatablePod {
				bestRegion = info.regionsInfo[region]
			}
		}

		if bestRegion == nil || bestRegion.AllocatablePod <= 0 {
			klog.Errorf("[Reschedule] failure schedule error: no region has enough resource to schedule")
			// rc.ctr.emitWarningEvent(deployment, eventReasonRescheduleFailed,
			// "no region has enough resource to schedule")
			return ErrWithoutAvailableRegions
		} else {
			bestRegion.rescheduleReplicas++
			bestRegion.AllocatablePod--
		}
	}

	return nil
}

func makeEmptyAutoScheduleSpec(generation int64) (spec *utils.AutoScheduleSpec) {
	spec = &utils.AutoScheduleSpec{
		Generation: generation,
		Infos:      map[string]*utils.ScheduleReplicas{},
	}

	return spec
}

func CheckAnnotation(object metav1.Object) (RescheduleType, bool, error) {
	spec, err := utils.GetAutoScheduleSpec(object)
	if err != nil {
		klog.Errorf("[Reschedule] ExtendedDeployment %v/%v query schedule info error: %v",
			object.GetNamespace(), object.GetName(), err)
		return RescheduleUnknown, false, err
	}

	// Generation mismatch, schedule information is outdated
	if spec != nil && spec.Generation != object.GetGeneration() {
		klog.Warningf("[Reschedule] ExtendedDeployment %v/%v schedule info out of date, delete it",
			object.GetNamespace(), object.GetName())

		utils.DelAutoScheduleSpec(object)
		return RescheduleUnknown, false, nil
	}

	// Just for information
	failedRegion, ok := utils.GetFailedFlag(object)
	if !ok { // No region failure
		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v without failed region", object.GetNamespace(), object.GetName())
		if spec == nil {
			klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v has not been rescheduled", object.GetNamespace(), object.GetName())
			return RescheduleNever, true, nil
		}
		if spec.FailureRegion == "" {
			klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v has been reschedule for resource insufficient", object.GetNamespace(), object.GetName())
			return RescheduleForResourceInsufficient, true, nil
		}

		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v has do reschedule for region failed", object.GetNamespace(), object.GetName())
		return RescheduleUnknown, true, nil
	}

	klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v failed region %v", object.GetNamespace(), object.GetName(), failedRegion)
	return RescheduleUnknown, true, nil
}
