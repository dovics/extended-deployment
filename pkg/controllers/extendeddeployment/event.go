package extendeddeployment

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	eventReasonExtendedDeploymentConfigError       = "ExtendedDeploymentConfigError"
	eventReasonExtendedDeploymentResourceTypeError = "ExtendedDeploymentResourceTypeError"

	eventReasonInplacesetCreateSuccess = "InplaceSetCreateSuccess"
	eventReasonInplacesetDeleteSuccess = "InplaceSetDeleteSuccess"
	eventReasonInplacesetScaleSuccess  = "InplaceSetScaleSuccess"

	// Rollback
	eventReasonRollbackDone = "DeploymentRollback"

	// upgrade confirm
	eventReasonTermConfirmed = "TermConfirmed"
)

func (dc *ExtendedDeploymentReconciler) emitWarningEvent(obj runtime.Object, reason, message string) {
	dc.EventRecorder.Eventf(obj, corev1.EventTypeWarning, reason, message)
}

func (dc *ExtendedDeploymentReconciler) emitNormalEvent(obj runtime.Object, reason, message string) {
	dc.EventRecorder.Eventf(obj, corev1.EventTypeNormal, reason, message)
}
