package extendeddeployment

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment/adapter"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

// 获取Subset分区名称
func getSubsetRegionName(ss *adapter.Subset) string {
	return ss.Annotations[utils.IpsAnnotationRegionName]
}

// 获取InplaceSets的实际副本数量
func getActualReplicaCountForSubets(ISs []*adapter.Subset) int32 {
	totalActual := int32(0)
	for _, IS := range ISs {
		if IS != nil {
			totalActual += IS.Status.Replicas
		}
	}
	return totalActual
}

// 获取InplaceSets已经Ready的副本数量
func getReadyReplicaCountForSubsets(ISs []*adapter.Subset) int32 {
	totalReady := int32(0)
	for _, IS := range ISs {
		if IS != nil {
			totalReady += IS.Status.ReadyReplicas
		}
	}
	return totalReady
}

// 获取InplaceSets状态为Available的副本数量
func getAvailableReplicaCountForSubsets(ISs []*adapter.Subset) int32 {
	totalAvailable := int32(0)
	for _, IS := range ISs {
		if IS != nil {
			totalAvailable += IS.Status.AvailableReplicas
		}
	}
	return totalAvailable
}

// 获取InplaceSets的所有副本数
func getReplicaCountForSubsets(ISs []*adapter.Subset) int32 {
	total := int32(0)
	for _, IS := range ISs {
		if IS != nil {
			total += *(IS.Spec.Replicas)
		}
	}
	return total
}

func ParseRegionReplicas(totalReplicas int32, regionReplicas intstr.IntOrString) (int32, error) {
	if regionReplicas.Type == intstr.Int {
		if regionReplicas.IntVal < 0 {
			return 0, fmt.Errorf("region replicas (%d) should not be less than 0", regionReplicas.IntVal)
		}

		return regionReplicas.IntVal, nil
	}

	if totalReplicas < 0 {
		return 0, fmt.Errorf("region replicas (%v) should not be string when extendeddeployment replicas is empty", regionReplicas.StrVal)
	}

	strVal := regionReplicas.StrVal
	if !strings.HasSuffix(strVal, "%") {
		return 0, fmt.Errorf("region replicas (%s) only support integer value or percentage value with a suffix '%%'", strVal)
	}

	intPart := strVal[:len(strVal)-1]
	percent64, err := strconv.ParseInt(intPart, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("region replicas (%s) should be correct percentage integer: %s", strVal, err)
	}

	if percent64 > int64(100) || percent64 < int64(0) {
		return 0, fmt.Errorf("region replicas (%s) should be in range [0, 100]", strVal)
	}

	return int32(round(float64(totalReplicas) * float64(percent64) / 100)), nil
}

func round(x float64) int {
	return int(math.Floor(x + 0.5))
}
