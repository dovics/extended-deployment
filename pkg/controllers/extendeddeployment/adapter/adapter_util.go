package adapter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

const BETA_STEP_SIZE = 1
const (
	// Deployment annotation
	AnnotationRevision       = "deployment.extendeddeployment.io/revision"
	AnnotationRollbackTo     = "deployment.extendeddeployment.io/rollback-to"
	AnnotationFailedFlag     = "deployment.extendeddeployment.io/failed-flag"     // 不为空代表发生了故障，内容为故障分区，格式：region1,region2,...
	AnnotationUpgradeConfirm = "deployment.extendeddeployment.io/upgrade-confirm" // 升级确认
	AnnotationRollTerm       = "deployment.extendeddeployment.io/roll-term"       // 分组批次号
	AnnotationReschedule     = "deployment.extendeddeployment.io/reschedule"      // 资源不足自动调度/故障调度 对期望副本数的修改

	// InplaceSet annotation
	IpsAnnotationRegionName           = "inplaceset.extendeddeployment.io/region-name"
	IpsAnnotationTemplateHash         = "inplaceset.extendeddeployment.io/template-hash"
	IpsAnnotationInplacesetUpdateSpec = "inplaceset.extendeddeployment.io/inplaceset-update-spec"
	IpsAnnotationInplacesetStatus     = "inplaceset.extendeddeployment.io/inplaceset-status"
	IpsAnnotationConfigHash           = "inplaceset.extendeddeployment.io/config-hash"
	IpsAnnotationRollTerm             = "inplaceset.extendeddeployment.io/roll-term"           // Rolling group number
	IpsAnnotationFailedOldReplicas    = "inplaceset.extendeddeployment.io/failed-old-replicas" // Historical replica count when failure occurs
	IpsAnnotationDesiredReplicas      = "inplaceset.extendeddeployment.io/desired-replicas"
	IpsAnnotationRevision             = "inplaceset.extendeddeployment.io/revision"
	IpsAnnotationRegionFailed         = "inplaceset.extendeddeployment.io/region-failed"
)

var annotationsToSkip = map[string]bool{
	corev1.LastAppliedConfigAnnotation: true,
	AnnotationUpgradeConfirm:           true,
	AnnotationFailedFlag:               true,
	AnnotationRollbackTo:               true,
	AnnotationRevision:                 true,
	AnnotationRollTerm:                 true,
	//RevisionHistoryAnnotation:      true,
	//DesiredReplicasAnnotation:      true,
	//MaxReplicasAnnotation:          true,
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
