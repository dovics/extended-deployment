package extendeddeployment

import (
	"context"
	"encoding/json"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller/history"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/controllers/extendeddeployment/adapter"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

type RollbackConfig struct {
	// The revision to rollback to. If set to 0, rollback to the last revision.
	// +optional
	Revision int64 `json:"revision,omitempty" protobuf:"varint,1,opt,name=revision"`
}

func setRollbackTo(d *v1beta1.ExtendedDeployment, rollbackTo *RollbackConfig) {
	if rollbackTo == nil {
		delete(d.Annotations, utils.AnnotationRollbackTo)
		return
	}
	if d.Annotations == nil {
		d.Annotations = make(map[string]string)
	}
	d.Annotations[utils.AnnotationRollbackTo] = strconv.FormatInt(rollbackTo.Revision, 10)
}

// newRevision creates a new ControllerRevision containing a patch that reapplies the target state of set.
// The Revision of the returned ControllerRevision is set to revision. If the returned error is nil, the returned
// ControllerRevision is valid. StatefulSet revisions are stored as patches that re-apply the current state of set
// to a new StatefulSet using a strategic merge patch to replace the saved state of the new StatefulSet.
func newRevision(d *v1beta1.ExtendedDeployment, revision int64, collisionCount *int32) (*appsv1.ControllerRevision, error) {
	specMarshaled, err := json.Marshal(d.Spec)
	if err != nil {
		return nil, err
	}
	cr, err := history.NewControllerRevision(d,
		v1beta1.ExtendedDeploymentGVK,
		d.Spec.Selector.MatchLabels,
		runtime.RawExtension{Raw: specMarshaled},
		revision,
		collisionCount)
	if err != nil {
		return nil, err
	}
	if cr.ObjectMeta.Annotations == nil {
		cr.ObjectMeta.Annotations = make(map[string]string)
	}
	for key, value := range d.Annotations {
		cr.ObjectMeta.Annotations[key] = value
	}
	return cr, nil
}

// nextRevision finds the next valid revision number based on revisions. If the length of revisions
// is 0 this is 1. Otherwise, it is 1 greater than the largest revision's Revision. This method
// assumes that revisions has been sorted by Revision.
func nextRevision(revisions []*appsv1.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision + 1
}

func maxRevision(revisions []*appsv1.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision
}

func maxRevisionForSubsets(objs []*adapter.Subset) (int64, error) {
	max := int64(0)
	for _, obj := range objs {
		v, err := utils.RevisionToInt64(obj)
		if err != nil {
			return 0, err
		}
		if v > max {
			max = v
		}
	}
	return max, nil
}

func (dc *ExtendedDeploymentReconciler) listRevisions(d *v1beta1.ExtendedDeployment) ([]*appsv1.ControllerRevision, error) {
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		klog.Errorf("extendeddeployment %v/%v label to selector error: %v", d.Namespace, d.Name, err)
		return nil, err
	}
	revisions, err := dc.controllerHistory.ListControllerRevisions(d, selector)
	if err != nil {
		klog.Errorf("extendeddeployment %v/%v list controller revisions error: %v", d.Namespace, d.Name, err)
	}
	return revisions, err
}

// updateDeploymentAndClearRollbackTo sets .spec.rollbackTo to nil and update the input deployment
// It is assumed that the caller will have updated the deployment template appropriately (in case
// we want to rollback).
func (dc *ExtendedDeploymentReconciler) updateDeploymentAndClearRollbackTo(ctx context.Context, d *v1beta1.ExtendedDeployment) error {
	klog.V(4).Infof("Cleans up rollbackTo of deployment %v/%v", d.Namespace, d.Name)
	setRollbackTo(d, nil)
	err := dc.Update(ctx, d)
	if err != nil {
		klog.Errorf("cleans up rollbackTo of deployment %v/%v error: %v", d.Namespace, d.Name, err)
	}
	return err
}

// syncRevisions 处理revision
func (dc *ExtendedDeploymentReconciler) syncRevisions(ctx context.Context, deploy *v1beta1.ExtendedDeployment) (bool, error) {
	d := deploy

	revisions, err := dc.listRevisions(d)
	if err != nil {
		return false, err
	}

	history.SortControllerRevisions(revisions)
	curRvName, err := utils.Revision(d)
	if err != nil {
		klog.Errorf("extendeddeployment %v/%v get current revision error: %v", d.Namespace, d.Name, err)
		return false, err
	}

	var collisionCount int32
	if d.Status.CollisionCount != nil {
		collisionCount = *d.Status.CollisionCount
	}

	if d.Annotations == nil {
		d.Annotations = make(map[string]string)
	}

	updateRv, err := newRevision(d, nextRevision(revisions), &collisionCount)
	if err != nil {
		klog.Warningf("extendeddeployment %v/%v create new revision error: %v", d.Namespace, d.Name, err)
		return false, err
	}
	rvs := history.FindEqualRevisions(revisions, updateRv)
	var lastEqualRevision *appsv1.ControllerRevision
	if len(rvs) > 0 {
		lastEqualRevision = rvs[len(rvs)-1]
	}
	if lastEqualRevision != nil {
		if lastEqualRevision == revisions[len(revisions)-1] { // 没有改变
			klog.InfoS("return %v/%v lastEqualRevision== revisions",
				d.Namespace, d.Name)
			return false, nil
		}

		// 版本回退，将最后一个匹配的revision版本号修改为新版本号
		oldRvNum := lastEqualRevision.Revision
		newRvNum := updateRv.Revision
		updateRv, err = dc.controllerHistory.UpdateControllerRevision(lastEqualRevision, updateRv.Revision)
		if err != nil {
			klog.Warningf("extendeddeployment %v/%v update old revision %v from %v to %v error: %v",
				d.Namespace, d.Name, lastEqualRevision.Name, oldRvNum, newRvNum, err)
			return false, err
		}

		d.Annotations[utils.AnnotationRevision] = strconv.Itoa(int(updateRv.Revision))
		delete(d.Annotations, utils.AnnotationUpgradeConfirm)
		if err := dc.updateAnnotations(ctx, d); err != nil {
			klog.Errorf("update revision extendeddeployment %v:%v annotations error: %v",
				d.Namespace, d.Name, err)
			return false, err
		}
		klog.InfoS("1 updateAnnotations %v/%v AnnotationRevision to %s",
			d.Namespace, d.Name,
			d.Annotations[utils.AnnotationRevision])

		return true, nil
	}

	// 没有匹配，创建新版本
	updateRv, err = dc.controllerHistory.CreateControllerRevision(d, updateRv, &collisionCount)
	if err != nil {
		klog.Warningf("create a new controller revision error: %v", err)
		return false, err
	}

	d.Annotations[utils.AnnotationRevision] = strconv.Itoa(int(updateRv.Revision))
	delete(d.Annotations, utils.AnnotationUpgradeConfirm)

	if (d.Status.CollisionCount == nil && collisionCount > 0) ||
		(d.Status.CollisionCount != nil && *d.Status.CollisionCount != collisionCount) {
		d.Status.CollisionCount = &collisionCount
		if err = dc.Client.Update(ctx, d); err != nil { // 修改status
			klog.Warningf("update ExtendedDeployment %v/%v error: %v", d.Namespace, d.Name, err)
			return false, err
		}
		klog.InfoS("2 updateAnnotations %v/%v AnnotationRevision to %s",
			d.Namespace, d.Name,
			d.Annotations[utils.AnnotationRevision])
		// FIXME:
		// 更新 status 后，还需要继续更新注解，
		// 如果不更新直接结束，不重新入队，因为是更新status，不会再次触发，会导致更新事件被忽略
		// 如果不更新立即结束，立即重新入队，又会导致两次更新间隔很短，也会出现错误
		// 如果继续更新注解，可能会导致更新注解错误
	} else {
		if err := dc.updateAnnotations(ctx, d); err != nil {
			klog.Errorf("update revision extendeddeployment %v:%v annotations error: %v",
				d.Namespace, d.Name, err)
			return false, err
		}
	}

	revisions = append(revisions, updateRv)
	var curRv *appsv1.ControllerRevision
	if len(curRvName) > 0 {
		rv, _ := strconv.ParseInt(curRvName, 10, 64)
		for i, v := range revisions {
			if v.Revision == rv {
				curRv = revisions[i]
				break
			}
		}
	}

	err = dc.truncateHistory(deploy, revisions, curRv, updateRv)
	if err != nil {
		return false, err
	}
	return true, nil
}

// truncateHistory 删除多余的历史版本
func (dc *ExtendedDeploymentReconciler) truncateHistory(deploy *v1beta1.ExtendedDeployment, revisions []*appsv1.ControllerRevision, curRv *appsv1.ControllerRevision, updateRv *appsv1.ControllerRevision) error {
	// 直接对历史版本截断，无需做任何判断
	historyLimit := int(deploy.Spec.HistoryLimit)
	his := make([]*appsv1.ControllerRevision, 0, len(revisions))
	for _, r := range revisions {
		his = append(his, r)
	}

	if len(his) <= historyLimit {
		return nil
	}
	history.SortControllerRevisions(his)
	his = his[:len(his)-historyLimit-1]

	for i := 0; i < len(his); i++ {
		if err := dc.controllerHistory.DeleteControllerRevision(his[i]); err != nil {
			klog.Warningf("delete old revision %v error: %v", his[i].Name, err)
			return err
		}
	}

	return nil
}
