package extendeddeployment

import (
	"fmt"
	"math"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/controllers/extendeddeployment/adapter"
)

func (m *SubsetControl) reconcileNewSubset(cd *v1beta1.ExtendedDeployment, allRSs []*adapter.Subset, newRS *adapter.Subset, region *adapter.RegionInfo) (bool, error) {
	if *(newRS.Spec.Replicas) == region.DesiredReplicas {
		return false, nil
	}
	if *(newRS.Spec.Replicas) > region.DesiredReplicas {
		klog.V(6).Infof("scale down %s replicas %d -> %d", m.key, *newRS.Spec.Replicas, region.DesiredReplicas)
		err := m.ScaleSubset(cd, newRS, region.DesiredReplicas)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	newReplicasCount, err := NewRSNewReplicas(cd, allRSs, newRS, region)
	if err != nil {
		return false, err
	}
	klog.V(6).Infof("scale up %s replicas %d -> %d", m.key, *newRS.Spec.Replicas, newReplicasCount)
	err = m.ScaleSubset(cd, newRS, newReplicasCount)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (m *SubsetControl) reconcileOldSubsets(cd *v1beta1.ExtendedDeployment, allRSs []*adapter.Subset, oldRSs []*adapter.Subset, newRS *adapter.Subset, region *adapter.RegionInfo, regionName string) (bool, error) {
	oldPodsCount := GetReplicasCount(oldRSs)
	if oldPodsCount == 0 {
		return false, nil
	}
	allPodsCount := GetReplicasCount(allRSs)
	maxUnavailable := MaxUnavailable(cd, region)
	minAvailable := region.DesiredReplicas - maxUnavailable
	newRSUnavailablePodCount := *newRS.Spec.Replicas - newRS.Status.AvailableReplicas
	maxScaleDown := allPodsCount - minAvailable - newRSUnavailablePodCount
	if maxScaleDown <= 0 {
		return false, nil
	}
	cleanupCount, err := m.cleanupUnhealthyReplicas(cd, regionName, maxScaleDown)
	if err != nil {
		return false, nil
	}

	allRSs = append(region.Olds, newRS)
	scaleDownCount, err := m.scaleDownOldSubsetsForRollingUpdate(cd, allRSs, oldRSs, region)
	if err != nil {
		return false, nil
	}
	totalScaleDown := cleanupCount + scaleDownCount
	return totalScaleDown > 0, nil
}

func (m *SubsetControl) scaleDownOldSubsetsForRollingUpdate(cd *v1beta1.ExtendedDeployment, allRSs []*adapter.Subset, oldRSs []*adapter.Subset, region *adapter.RegionInfo) (int32, error) {
	maxUnavailable := MaxUnavailable(cd, region)
	minAvailable := region.DesiredReplicas - maxUnavailable
	availablePodCount := GetReplicasCount(allRSs)
	if availablePodCount <= minAvailable {
		return 0, nil
	}
	totalScaleDown := int32(0)
	totalScaleDownCount := availablePodCount - minAvailable
	for _, targetRS := range oldRSs {
		if totalScaleDown >= totalScaleDownCount {
			break
		}
		if *(targetRS.Spec.Replicas) == 0 {
			continue
		}
		scaleDownCount := int32(math.Min(float64(*(targetRS.Spec.Replicas)), float64(totalScaleDownCount-totalScaleDown)))
		newReplicasCount := *(targetRS.Spec.Replicas) - scaleDownCount
		if newReplicasCount > *(targetRS.Spec.Replicas) {
			return 0, fmt.Errorf("when scaling down old RS, got invalid request to scale dow %s/%s %d -> %d", targetRS.Namespace, targetRS.Name, *(targetRS.Spec.Replicas), newReplicasCount)
		}
		klog.V(6).Infof("scale down %s/%s replicas %d -> %d", targetRS.Namespace, targetRS.Name, *targetRS.Spec.Replicas, newReplicasCount)
		err := m.ScaleSubset(cd, targetRS, newReplicasCount)
		if err != nil {
			return totalScaleDown, err
		}
		totalScaleDown += scaleDownCount
	}
	return totalScaleDown, nil
}

func NewRSNewReplicas(cd *v1beta1.ExtendedDeployment, allRSs []*adapter.Subset, newRS *adapter.Subset, region *adapter.RegionInfo) (int32, error) {
	maxSurge, err := intstr.GetValueFromIntOrPercent(cd.Spec.Strategy.UpdateStrategy.RollingUpdate.MaxSurge, int(region.DesiredReplicas), true)
	if err != nil {
		return 0, err
	}
	currentPodCount := GetReplicasCount(allRSs)
	maxTotalPods := region.DesiredReplicas + int32(maxSurge)
	if currentPodCount >= maxTotalPods {
		return *newRS.Spec.Replicas, nil
	}
	scaleUpCount := maxTotalPods - currentPodCount
	scaleUpCount = int32(math.Min(float64(scaleUpCount), float64(region.DesiredReplicas-*(newRS.Spec.Replicas))))
	return *(newRS.Spec.Replicas) + scaleUpCount, nil
}

func MaxUnavailable(cd *v1beta1.ExtendedDeployment, region *adapter.RegionInfo) int32 {
	if region.DesiredReplicas == 0 {
		return 0
	}
	_, maxUnavailable, _ := ResolveFenceposts(cd.Spec.Strategy.UpdateStrategy.RollingUpdate.MaxSurge, cd.Spec.Strategy.UpdateStrategy.RollingUpdate.MaxUnavailable, region.DesiredReplicas)
	return maxUnavailable
}

func ResolveFenceposts(maxSurge, maxUnavailable *intstr.IntOrString, desired int32) (int32, int32, error) {
	surge, err := intstr.GetValueFromIntOrPercent(intstr.ValueOrDefault(maxSurge, intstr.FromInt(0)), int(desired), true)
	if err != nil {
		return 0, 0, err
	}
	unavailable, err := intstr.GetValueFromIntOrPercent(intstr.ValueOrDefault(maxUnavailable, intstr.FromInt(0)), int(desired), true)
	if err != nil {
		return 0, 0, err
	}
	if surge == 0 && unavailable == 0 {
		unavailable = 1
	}
	return int32(surge), int32(unavailable), nil
}

func GetReplicasCount(allRSs []*adapter.Subset) int32 {
	count := int32(0)
	for _, rs := range allRSs {
		count += *rs.Spec.Replicas
	}
	return count
}
