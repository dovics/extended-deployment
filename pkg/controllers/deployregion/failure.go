package deployregion

import (
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

const (
	// TODO: read it from configure file
	// 分区故障与恢复node所占比率
	regionFailureRate = 1.0 // 100% 失败，认为是分区故障
	regionResumeRate  = 0.0 // 没有失败，认为故障恢复
)

func (d *syncContext) QueryExtendedDeploymentsInRegion(regionName string) map[string]types.NamespacedName {
	// 对当前分区下的 ExtendedDeployment 进行操作
	inplacesets, err := d.controller.QueryInplaceSetsInRegion(d.ctx, regionName)
	if err != nil {
		stopReconcile(err)
	}

	deployMap := map[string]types.NamespacedName{}
	for i := range inplacesets.Items {
		ips := &inplacesets.Items[i]
		if len(ips.OwnerReferences) == 0 {
			continue
		}
		if ips.OwnerReferences[0].Kind != v1beta1.ExtendedDeploymentGVK.Kind {
			continue
		}
		key := types.NamespacedName{
			Namespace: ips.Namespace,
			Name:      ips.OwnerReferences[0].Name,
		}
		deployMap[key.String()] = key
	}

	return deployMap
}

func (d *syncContext) CheckRegionStatus(regionName string) {
	regionInfo := d.Cache.Regions[regionName]
	failNum := 0
	totalNum := len(regionInfo.Nodes)
	for _, node := range regionInfo.Nodes {
		if !node.IsReady {
			failNum++
		}
	}

	failedRate := float64(failNum) / float64(totalNum)
	newStatus := regionStatusUnknown
	if failedRate >= regionFailureRate {
		newStatus = regionStatusFailed
	} else if failedRate <= regionResumeRate {
		newStatus = regionStatusOK
	}

	if regionInfo.status == newStatus { // 分区状态没有变化，无需处理
		return
	}

	deployMap := d.QueryExtendedDeploymentsInRegion(regionName)
	for _, key := range deployMap {
		deploy, err := utils.QueryExtendedDeployment(d.controller.Client, d.ctx, key)
		if err != nil {
			stopReconcile(err)
		}
		if deploy == nil {
			continue
		}

		// 找到该 ExtendedDeployment 故障的分区列表
		failedRegions := make([]string, 0)
		for _, region := range deploy.Spec.Regions {
			tempStatus := regionStatusUnknown
			if region.Name == regionName {
				tempStatus = newStatus
			} else {
				tempStatus = d.Cache.Regions[region.Name].status
			}
			if tempStatus == regionStatusFailed {
				failedRegions = append(failedRegions, region.Name)
			}
		}

		if IsExtendedDeploymentProgressFinished(deploy) {
			// 设置故障注解，由 ExtendedDeployment controller 去做调解
			// 注：此分区的故障恢复，并不表示该 ExtendedDeployment 就没有分区故障了
			err = d.setFailureAnnotation(deploy, failedRegions)
			if err != nil {
				stopReconcile(err)
			}
		}
	}

	regionInfo.status = newStatus
}

func (d *syncContext) setFailureAnnotation(deploy *v1beta1.ExtendedDeployment, failedRegions []string) error {
	if len(failedRegions) == 0 { // 故障恢复
		if _, ok := deploy.Annotations[utils.AnnotationFailedFlag]; !ok {
			return nil
		}
		delete(deploy.Annotations, utils.AnnotationFailedFlag)
		return d.controller.Update(d.ctx, deploy)
	}

	// 设置故障
	sort.Strings(failedRegions)
	value := strings.Join(failedRegions, ",")
	if deploy.Annotations == nil {
		deploy.Annotations = map[string]string{}
	}
	curValue := deploy.Annotations[utils.AnnotationFailedFlag]
	if value == curValue {
		return nil
	}

	deploy.Annotations[utils.AnnotationFailedFlag] = value
	return d.controller.Update(d.ctx, deploy)
}

func IsExtendedDeploymentProgressFinished(deployment *v1beta1.ExtendedDeployment) bool {
	var isStatusConditionOk bool
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == corev1.ConditionFalse {
			isStatusConditionOk = true
		}
	}

	return isStatusConditionOk
}
