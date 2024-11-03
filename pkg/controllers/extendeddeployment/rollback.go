package extendeddeployment

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller/history"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

func getSpecByRevision(r *appsv1.ControllerRevision) (*v1beta1.ExtendedDeploymentSpec, error) {
	raw := r.Data.Raw
	spec := &v1beta1.ExtendedDeploymentSpec{}
	if err := json.Unmarshal(raw, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func (dc *ExtendedDeploymentReconciler) rollbackToRevision(ctx context.Context, d *v1beta1.ExtendedDeployment,
	rev *appsv1.ControllerRevision) (err error) {
	spec, err := getSpecByRevision(rev)
	if err != nil {
		klog.Warningf("get spec by revision %v error: %v", rev.Name, err)
		return err
	}
	if !utils.EqualIgnoreHash(&d.Spec, spec) {
		klog.V(4).Infof("Rolling back deployment %q to template spec %+v", d.Name, spec.Template.Spec)
		utils.SetFromReplicaSetTemplate(d, *spec)
		utils.SetDeploymentAnnotationsTo(d, rev)
		err = dc.updateDeploymentAndClearRollbackTo(ctx, d)
		if err != nil {
			klog.Warningf("roll back deployment %v to revision %v error: %v", d.Name, rev.Revision, err)
		} else {
			dc.emitNormalEvent(d, eventReasonRollbackDone, fmt.Sprintf("Rolled back deployment %v to revision %v", d.Name, rev.Revision))
		}
		return err
	}
	klog.V(4).Infof("Rolling back to a revision that contains the same template as current deployment %q, skipping rollback...", d.Name)
	eventMsg := fmt.Sprintf("The rollback revision contains the same template as current deployment %v", d.Name)
	dc.emitWarningEvent(d, utils.RollbackTemplateUnchanged, eventMsg)
	return nil
}

func (dc *ExtendedDeploymentReconciler) rollbackTo(ctx context.Context, revision int64, deploy *v1beta1.ExtendedDeployment) error {
	revisions, err := dc.listRevisions(deploy)
	if err != nil {
		return err
	}

	history.SortControllerRevisions(revisions)
	var targetRevision *appsv1.ControllerRevision
	if revision == 0 { // 表示回滚到上一个版本
		if len(revisions) < 2 {
			dc.emitWarningEvent(deploy, utils.RollbackRevisionNotFound,
				"Unable to find last revision ")
			return dc.updateDeploymentAndClearRollbackTo(ctx, deploy)
		}
		targetRevision = revisions[len(revisions)-2]
	} else {
		for _, r := range revisions {
			if r.Revision == revision {
				targetRevision = r
			}
		}
	}

	if targetRevision == nil {
		dc.emitWarningEvent(deploy, utils.RollbackRevisionNotFound,
			"Unable to find revision: "+strconv.FormatInt(revision, 10))
		return dc.updateDeploymentAndClearRollbackTo(ctx, deploy)
	}

	return dc.rollbackToRevision(ctx, deploy, targetRevision)
}

// checkRollback 检查回滚
func (dc *ExtendedDeploymentReconciler) checkRollback(ctx context.Context, deploy *v1beta1.ExtendedDeployment) (bool, error) {
	revision, ok := deploy.Annotations[utils.AnnotationRollbackTo]
	if !ok {
		return false, nil
	}

	versionNum := int64(0)
	revision = strings.TrimSpace(revision)
	if revision != "" {
		v, err := strconv.ParseInt(revision, 10, 64)
		if err != nil { // 无效的版本号，直接忽略
			dc.emitWarningEvent(deploy, utils.RollbackRevisionNotFound,
				"invalid revision: "+revision)

			return ok, dc.updateDeploymentAndClearRollbackTo(ctx, deploy)
		}
		versionNum = v
	}

	err := dc.rollbackTo(ctx, versionNum, deploy)

	return ok, err
}
