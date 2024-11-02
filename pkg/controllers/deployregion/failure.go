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

func (d *syncContext) QueryCibDeploymentsInRegion(regionName string) (cibdMap map[string]types.NamespacedName) {
	// 对当前分区下的 CibDeployment 进行操作
	inplacesets, err := d.controller.QueryInplaceSetsInRegion(d.ctx, regionName)
	if err != nil {
		stopReconcile(err)
	}

	cibdMap = map[string]types.NamespacedName{}
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
		cibdMap[key.String()] = key
	}

	return
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

	cibdMap := d.QueryCibDeploymentsInRegion(regionName)
	for _, key := range cibdMap {
		cibd, err := utils.QueryCibDeployment(d.controller.Client, d.ctx, key)
		if err != nil {
			stopReconcile(err)
		}
		if cibd == nil {
			continue
		}

		// 找到该 CibDeployment 故障的分区列表
		failedRegions := make([]string, 0)
		for _, region := range cibd.Spec.Regions {
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

		if IsCibDeploymentProgressFinished(cibd) {
			// 设置故障注解，由 CibDeployment controller 去做调解
			// 注：此分区的故障恢复，并不表示该 CibDeployment 就没有分区故障了
			err = d.setFailureAnnotation(cibd, failedRegions)
			if err != nil {
				stopReconcile(err)
			}
		}
	}

	regionInfo.status = newStatus
}

func (d *syncContext) setFailureAnnotation(cibd *v1beta1.ExtendedDeployment, failedRegions []string) error {
	if len(failedRegions) == 0 { // 故障恢复
		if _, ok := cibd.Annotations[utils.AnnotationFailedFlag]; !ok {
			return nil
		}
		delete(cibd.Annotations, utils.AnnotationFailedFlag)
		return d.controller.Update(d.ctx, cibd)
	}

	// 设置故障
	sort.Strings(failedRegions)
	value := strings.Join(failedRegions, ",")
	if cibd.Annotations == nil {
		cibd.Annotations = map[string]string{}
	}
	curValue, _ := cibd.Annotations[utils.AnnotationFailedFlag]
	if value == curValue {
		return nil
	}

	cibd.Annotations[utils.AnnotationFailedFlag] = value
	return d.controller.Update(d.ctx, cibd)
}

func IsCibDeploymentProgressFinished(deployment *v1beta1.ExtendedDeployment) bool {
	var isStatusConditionOk bool
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == corev1.ConditionFalse {
			isStatusConditionOk = true
		}
	}

	return isStatusConditionOk
}
