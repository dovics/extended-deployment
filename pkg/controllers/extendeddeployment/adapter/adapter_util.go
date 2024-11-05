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
package adapter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

const BETA_STEP_SIZE = 1
const (
	// Deployment annotation
	AnnotationRevision   = "deployment.extendeddeployment.io/revision"
	AnnotationRollbackTo = "deployment.extendeddeployment.io/rollback-to"
	// 不为空代表发生了故障，内容为故障分区，格式：region1,region2,...
	AnnotationFailedFlag = "deployment.extendeddeployment.io/failed-flag"
	// 升级确认
	AnnotationUpgradeConfirm = "deployment.extendeddeployment.io/upgrade-confirm"
	// 分组批次号
	AnnotationRollTerm = "deployment.extendeddeployment.io/roll-term"
	// 资源不足自动调度/故障调度 对期望副本数的修改
	AnnotationReschedule = "deployment.extendeddeployment.io/reschedule"

	// InplaceSet annotation
	IpsAnnotationRegionName           = "inplaceset.extendeddeployment.io/region-name"
	IpsAnnotationTemplateHash         = "inplaceset.extendeddeployment.io/template-hash"
	IpsAnnotationInplacesetUpdateSpec = "inplaceset.extendeddeployment.io/inplaceset-update-spec"
	IpsAnnotationInplacesetStatus     = "inplaceset.extendeddeployment.io/inplaceset-status"
	IpsAnnotationConfigHash           = "inplaceset.extendeddeployment.io/config-hash"
	// Rolling group number
	IpsAnnotationRollTerm = "inplaceset.extendeddeployment.io/roll-term"
	// Historical replica count when failure occurs
	IpsAnnotationFailedOldReplicas = "inplaceset.extendeddeployment.io/failed-old-replicas"
	IpsAnnotationDesiredReplicas   = "inplaceset.extendeddeployment.io/desired-replicas"
	IpsAnnotationRevision          = "inplaceset.extendeddeployment.io/revision"
	IpsAnnotationRegionFailed      = "inplaceset.extendeddeployment.io/region-failed"
)

var annotationsToSkip = map[string]bool{
	corev1.LastAppliedConfigAnnotation: true,
	AnnotationUpgradeConfirm:           true,
	AnnotationFailedFlag:               true,
	AnnotationRollbackTo:               true,
	AnnotationRevision:                 true,
	AnnotationRollTerm:                 true,
	// RevisionHistoryAnnotation:      true,
	// DesiredReplicasAnnotation:      true,
	// MaxReplicasAnnotation:          true,
}

// SetNewReplicaSetAnnotations sets new replica set's annotations appropriately by updating its revision and
// copying required deployment annotations to it; it returns true if replica set's annotation is changed.
func SetNewReplicaSetAnnotations(deployment *v1beta1.ExtendedDeployment, newOM *metav1.ObjectMeta) bool {
	// First, copy deployment's annotations (except for apply and revision annotations)
	annotationChanged := copyDeploymentAnnotationsToReplicaSet(deployment, newOM)
	// Then, update replica set's revision annotation
	if newOM.Annotations == nil {
		newOM.Annotations = make(map[string]string)
	}
	return annotationChanged
}

func SetSubsetRegionAffinity(spec *corev1.PodSpec, dr *v1beta1.DeployRegion) {
	if spec.Affinity == nil {
		spec.Affinity = &corev1.Affinity{}
	}
	nodeAffinity := utils.CloneNodeAffinityAndAdd(spec.Affinity.NodeAffinity, dr.Spec.MatchLabels)
	spec.Affinity.NodeAffinity = nodeAffinity
}

// copyDeploymentAnnotationsToReplicaSet copies deployment's annotations to replica set's annotations,
// and returns true if replica set's annotation is changed.
// Note that apply and revision annotations are not copied.
func copyDeploymentAnnotationsToReplicaSet(deployment *v1beta1.ExtendedDeployment, om *metav1.ObjectMeta) bool {
	rsAnnotationsChanged := false
	if om.Annotations == nil {
		om.Annotations = make(map[string]string)
	}
	for k, v := range deployment.Annotations {
		// newRS revision is updated automatically in getNewReplicaSet, and the deployment's revision number is then updated
		// by copying its newRS revision number. We should not copy deployment's revision to its newRS, since the update of
		// deployment revision number may fail (revision becomes stale) and the revision number in newRS is more reliable.
		if _, exist := om.Annotations[k]; skipCopyAnnotation(k) || (exist && om.Annotations[k] == v) {
			continue
		}
		om.Annotations[k] = v
		rsAnnotationsChanged = true
	}
	return rsAnnotationsChanged
}

func skipCopyAnnotation(key string) bool {
	return annotationsToSkip[key]
}
