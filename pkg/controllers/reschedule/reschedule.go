package reschedule

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/controllers/deployregion"
	"extendeddeployment.io/extended-deployment/pkg/controllers/extendeddeployment"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

const (
	errorMsgMemDetect = "Insufficient memory"
	errorMsgCpuDetect = "Insufficient cpu"
)

type ScheduleInfo struct {
	regionsInfo   map[string]*scheduleRegionInfo
	failedRegions []string

	err error
}

type scheduleRegionInfo struct {
	name string

	configReplicas     int32
	scheduleReplicas   int32 // 目前调度进入的副本数
	rescheduleReplicas int32 // 准备调度进入的副本数

	AllocatablePod int64 // 可分配的pod总数
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
	 * 1. 遍历故障分区，统计需要转移的副本数
	 * 2. 根据非故障区 InplaceSet 确定分区 request 的资源
	 * 3. 遍历非故障分区的Node，统计非故障分区可以容纳的 pod 数，以及总剩余资源数量
	 * 4. 逐个将待转移副本填入最优分区，每次转入后分区资源降低
	 * 5. 得到转移的数据，将转移数据以注解形式写入
	 */
	if regionsMap == nil || len(regionsMap) == 0 {
		return nil
	}

	targetRegion := make([]string, 0)
	schedulePodNum := int32(0) // 需要调度的副本数
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

	/*  对于分区故障转移后资源的限制
	 * 1. 资源不足转移
	 *    一旦generation发生变化，资源不足自动调度的内容就失效
	 * 2. 故障调度
	 *    一旦generation发生变化，之前的调度失效，需要删除重新调度
	 * 3. 资源不足和故障同时存在？？？
	 *    故障调度时，不可以进行自动调度
	 */
}

func (info ScheduleInfo) RescheduleInsufficientPods(insufficientPods map[string][]*corev1.Pod) error {
	// 获取可以调度的目标region
	scheduleInRegions := make([]string, 0)
	for regionName, region := range info.regionsInfo {
		if _, exists := insufficientPods[regionName]; exists { // 存在，表示有pod因资源不足而pending，无法转入
			continue
		}

		if region.rescheduleReplicas < region.configReplicas { // 分区有副本转出，表示资源不足或存在故障，无法转入
			continue
		}

		scheduleInRegions = append(scheduleInRegions, regionName)
	}

	for regionName, pods := range insufficientPods {
		desiredReplicas, nowSubsetReplicas := info.regionsInfo[regionName].configReplicas, info.regionsInfo[regionName].scheduleReplicas

		// 成功启动的 Pod 数
		info.regionsInfo[regionName].rescheduleReplicas = nowSubsetReplicas - int32(len(pods))

		// 启动失败或者还没有启动的 Pod 数
		out := desiredReplicas - info.regionsInfo[regionName].rescheduleReplicas
		if err := info.reschedule(scheduleInRegions, out); err != nil {
			return err
		}
	}

	return nil
}

func (info ScheduleInfo) reschedule(targetRegions []string, scheduleNum int32) error {
	// 选择最优分区，逐个将副本转移
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
			//rc.ctr.emitWarningEvent(deployment, eventReasonRescheduleFailed,
			//	"no region has enough resource to schedule")
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

	if spec != nil && spec.Generation != object.GetGeneration() { // Generation 不匹配，调度信息过期
		klog.Warningf("[Reschedule] ExtendedDeployment %v/%v schedule info out of date, delete it",
			object.GetNamespace(), object.GetName())

		utils.DelAutoScheduleSpec(object)
		return RescheduleUnknown, false, nil
	}

	// just for info
	failedRegion, ok := utils.GetFailedFlag(object)
	if !ok { // 无分区故障
		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v without failed region", object.GetNamespace(), object.GetName())
		if spec == nil {
			klog.V(4).Infof("[Reschedule] ExtendedDeployment %v/%v has not been reschedule", object.GetNamespace(), object.GetName())
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
