package extendeddeployment

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment/adapter"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

const (
	statusNormal             = "[Normal]"
	statusRegionFailure      = "[Failure]"
	statusRegionInsufficient = "[Insufficient]"

	syncWaitTimout = 60 * 5
)

func (dc *ExtendedDeploymentReconciler) updateAnnotations(ctx context.Context, deploy *v1beta1.ExtendedDeployment) error {
	cd := &v1beta1.ExtendedDeployment{}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		cd, err = utils.QueryExtendedDeployment(dc.Client, ctx, types.NamespacedName{
			Namespace: deploy.Namespace,
			Name:      deploy.Name,
		})
		if err != nil {
			return err
		}
		cd.Annotations = deploy.Annotations
		return dc.Update(ctx, cd)
	})
	if err != nil {
		klog.Errorf("update annotation conflict: %v", err.Error())
		return err
	}
	deploy = cd
	return nil
}

func (dc *ExtendedDeploymentReconciler) cleanOldSubsets(cd *v1beta1.ExtendedDeployment, ci adapter.ControlInterface, region *adapter.RegionInfo) error {
	if len(region.Olds) <= int(cd.Spec.HistoryLimit) {
		return nil
	}
	diff := len(region.Olds) - int(cd.Spec.HistoryLimit)
	sort.Sort(adapter.SubSetsByRevision(region.Olds))
	var errs []error
	for i := 0; i < diff; i++ {
		rs := region.Olds[i]
		if *(rs.Spec.Replicas) != 0 || rs.Status.AvailableReplicas != 0 || rs.DeletionTimestamp != nil {
			continue
		}
		klog.V(6).Infof("clean subset %s/%s for extended deployment %s/%s", rs.Namespace, rs.Name, cd.Namespace, cd.Name)
		if err := ci.DeleteSubset(cd, rs); err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

// cleanAfterSaturated 所有分区都饱和，执行清理操作
func (dc *ExtendedDeploymentReconciler) cleanAfterSaturated(ctx context.Context, deploy *v1beta1.ExtendedDeployment, key string,
	regionInfos map[string]*adapter.RegionInfo, ci adapter.ControlInterface) error {
	// 删除滚动过程中需要使用到的注解
	needDelAnno := []string{
		utils.AnnotationUpgradeConfirm, // 删除确认注解
	}
	if utils.DelAnnotations(deploy.Annotations, needDelAnno) {
		if err := dc.updateAnnotations(ctx, deploy); err != nil {
			klog.Errorf("delete extendeddeployment %v annotations error: %v",
				key, err)
			return err
		}
	} else {
		err := dc.SyncStatusOnly(ctx, deploy, regionInfos, true, true)
		if err != nil {
			return err
		}
	}

	for _, ri := range regionInfos {
		if utils.DelInplacesetUpdateSpec(ri.New.Annotations) {
			if err := ci.UpdateSubsetAnnotations(ri.New, false); err != nil {
				klog.Errorf("delete %v %v/%v annotations error: %v",
					deploy.Spec.SubsetType, ri.New.Namespace, ri.New.Name, err)
				return err
			}
		}

		for _, ss := range ri.Olds {
			if *(ss.Spec.Replicas) == 0 {
				continue
			}
			if err := ci.ScaleSubset(deploy, ss, 0); err != nil {
				return err
			}
		}
		if err := dc.cleanOldSubsets(deploy, ci, ri); err != nil {
			return err
		}
	}
	err := ci.ClearUnused(deploy)
	if err != nil {
		klog.Errorf("%v ClearUnused error: %v",
			deploy.Spec.SubsetType, err)
		return err
	}
	return nil
}

// 计算ExtendedDeployment的状态并返回，如果错误就返回nil
func (dc *ExtendedDeploymentReconciler) calculateStatus(deploy *v1beta1.ExtendedDeployment, regionInfos map[string]*adapter.RegionInfo) (status *v1beta1.ExtendedDeploymentStatus) {

	status = &v1beta1.ExtendedDeploymentStatus{
		CollisionCount:    deploy.Status.CollisionCount,
		LastTermStartTime: deploy.Status.LastTermStartTime,
	}

	// Copy conditions one by one so we won't mutate the original object.
	conditions := deploy.Status.Conditions
	status.Conditions = append(status.Conditions, conditions...)

	allRegionSynced := true
	allRegions := make([]string, 0)
	allReplicas := int32(0)
	for regionName, info := range regionInfos {
		regionAllISs := append(info.Olds, info.New)
		replicas := getActualReplicaCountForSubets(regionAllISs)
		availableReplicas := getAvailableReplicaCountForSubsets(regionAllISs)
		unavailableReplicas := getReplicaCountForSubsets(regionAllISs) - availableReplicas
		regionSt := v1beta1.RegionStatus{
			RegionName:          regionName,
			Replicas:            replicas,
			UpdatedReplicas:     getActualReplicaCountForSubets([]*adapter.Subset{info.New}),
			ReadyReplicas:       getReadyReplicaCountForSubsets(regionAllISs),
			AvailableReplicas:   availableReplicas,
			UnavailableReplicas: unavailableReplicas,
		}
		status.Regions = append(status.Regions, regionSt)

		// regionName: actualReplicas/regionDesiredReplicas
		allRegions = append(allRegions, fmt.Sprintf("%v: %v/%v",
			regionName, regionSt.Replicas, info.ConfiguredReplicas))
		allReplicas += replicas
		if !info.ReplicasDesired || !info.AvailableDesired {
			allRegionSynced = false
		}
	}

	// 排序，避免多次同步状态时，分区顺序不一致
	sort.Strings(allRegions)
	spec, _ := utils.GetAutoScheduleSpec(deploy)
	if spec == nil {
		status.Overview = statusNormal
	} else {
		if spec.FailureRegion != "" {
			status.Overview = statusRegionFailure
		} else {
			status.Overview = statusRegionInsufficient
		}
	}

	status.Overview += strings.Join(allRegions, "; ")
	status.Replicas = allReplicas

	if allRegionSynced {
		c := utils.NewDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionFalse,
			"AllRegionSynced", "all region is synced")
		utils.SetDeploymentCondition(status, *c)
	} else {
		t, _ := strconv.ParseInt(status.LastTermStartTime, 10, 64)
		termTimeout := deploy.Spec.Strategy.SyncWaitTimeout
		if termTimeout == 0 {
			termTimeout = syncWaitTimout
		}
		if t != 0 && time.Now().UTC().Unix()-t > int64(termTimeout) {
			klog.Warningf("%s/%s sync wait timeout", deploy.Namespace, deploy.Name)
			c := utils.NewDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionFalse,
				"SyncWaitTimeout", "sync wait timeout")
			utils.SetDeploymentCondition(status, *c)
		} else {
			c := utils.NewDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue,
				"NotAllRegionSynced", "not all region is synced")
			utils.SetDeploymentCondition(status, *c)
		}
	}

	selector, ise := ValidatedLabelSelectorAsSelector(deploy.Spec.Selector)
	if ise == nil {
		status.LabelSelector = selector.String()
	} else {
		klog.Warningf("%s/%s ValidatedLabelSelectorAsSelector err , %s", deploy.Namespace, deploy.Name, ise.Error())
	}

	return status
}

func ValidatedLabelSelectorAsSelector(ps *metav1.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}

	selector := labels.NewSelector()
	for k, v := range ps.MatchLabels {
		r := newRequirement(k, selection.Equals, []string{v})
		selector = selector.Add(*r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch expr.Operator {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid pod selector operator", expr.Operator)
		}
		r := newRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		selector = selector.Add(*r)
	}
	return selector, nil
}

func newRequirement(key string, op selection.Operator, vals []string) *labels.Requirement {
	sel := &labels.Requirement{}
	selVal := reflect.ValueOf(sel)
	val := reflect.Indirect(selVal)

	keyField := val.FieldByName("key")
	keyFieldPtr := (*string)(unsafe.Pointer(keyField.UnsafeAddr()))
	*keyFieldPtr = key

	opField := val.FieldByName("operator")
	opFieldPtr := (*selection.Operator)(unsafe.Pointer(opField.UnsafeAddr()))
	*opFieldPtr = op

	if len(vals) > 0 {
		valuesField := val.FieldByName("strValues")
		valuesFieldPtr := (*[]string)(unsafe.Pointer(valuesField.UnsafeAddr()))
		*valuesFieldPtr = vals
	}

	return sel
}

func (dc *ExtendedDeploymentReconciler) UpdateProgressing(ctx context.Context, deploy *v1beta1.ExtendedDeployment) error {
	cd := &v1beta1.ExtendedDeployment{}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		cd, err = utils.QueryExtendedDeployment(dc.Client, ctx, types.NamespacedName{
			Namespace: deploy.Namespace,
			Name:      deploy.Name,
		})
		if err != nil {
			return err
		}

		c := utils.NewDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue,
			"NotAllRegionSynced", "not all region is synced")
		utils.SetDeploymentCondition(&cd.Status, *c)
		return dc.Status().Update(ctx, cd)
	})
	if err != nil {
		klog.Errorf("extendeddeployment %s/%s update status error: %v", deploy.Namespace, deploy.Name, err)
		return err
	}
	return nil
}

func (dc *ExtendedDeploymentReconciler) SyncStatusOnly(ctx context.Context, deploy *v1beta1.ExtendedDeployment,
	regionInfos map[string]*adapter.RegionInfo, refreshTime bool, saturated bool) error {

	// 设置term开始时间
	if saturated {
		deploy.Status.LastTermStartTime = ""
	} else if refreshTime || deploy.Status.LastTermStartTime == "" {
		deploy.Status.LastTermStartTime = strconv.FormatInt(time.Now().UTC().Unix(), 10)
	}

	cd := &v1beta1.ExtendedDeployment{}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		cd, err = utils.QueryExtendedDeployment(dc.Client, ctx, types.NamespacedName{
			Namespace: deploy.Namespace,
			Name:      deploy.Name,
		})
		if err != nil {
			return err
		}
		cd.Status.LastTermStartTime = deploy.Status.LastTermStartTime
		cd.Status.CollisionCount = deploy.Status.CollisionCount
		status := dc.calculateStatus(cd, regionInfos)
		if reflect.DeepEqual(status, cd.Status) {
			return nil
		}
		cd.Status = *status
		return dc.Status().Update(ctx, cd)
	})
	if err != nil {
		klog.Errorf("extendeddeployment %s/%s update status error: %v", deploy.Namespace, deploy.Name, err)
		return err
	}
	deploy = cd
	return nil
}
