/*
Copyright 2016 The Kubernetes Authors.

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

package utils

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/apps"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
)

const (
	// RollbackRevisionNotFound is not found rollback event reason
	RollbackRevisionNotFound = "DeploymentRollbackRevisionNotFound"
	// RollbackTemplateUnchanged is the template unchanged rollback event reason
	RollbackTemplateUnchanged = "DeploymentRollbackTemplateUnchanged"

	RegionLabel = "inplaceset-region"
)

// NewDeploymentCondition creates a new deployment condition.
func NewDeploymentCondition(condType appsv1.DeploymentConditionType, status v1.ConditionStatus, reason, message string) *appsv1.DeploymentCondition {
	return &appsv1.DeploymentCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// GetDeploymentCondition returns the condition with the provided type.
func GetDeploymentCondition(status v1beta1.ExtendedDeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetDeploymentCondition updates the deployment to include the provided condition. If the condition that
// we are about to add already exists and has the same status and reason then we are not going to update.
func SetDeploymentCondition(status *v1beta1.ExtendedDeploymentStatus, condition appsv1.DeploymentCondition) {
	currentCond := GetDeploymentCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	// Do not update lastTransitionTime if the status of the condition doesn't change.
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// filterOutCondition returns a new slice of deployment conditions without conditions with the provided type.
func filterOutCondition(conditions []appsv1.DeploymentCondition, condType appsv1.DeploymentConditionType) []appsv1.DeploymentCondition {
	var newConditions []appsv1.DeploymentCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

// skipCopyAnnotation returns true if we should skip copying the annotation with the given annotation key
// TODO: How to decide which annotations should / should not be copied?
//
//	See https://github.com/kubernetes/kubernetes/pull/20035#issuecomment-179558615
func skipCopyAnnotation(key string) bool {
	return annotationsToSkip[key]
}

// TODO: switch RsListFunc and podListFunc to full namespacers

// RsListFunc returns the ReplicaSet from the ReplicaSet namespace and the List metav1.ListOptions.
type RsListFunc func(string, metav1.ListOptions) ([]*v1beta1.InplaceSet, error)

// EqualIgnoreHash returns true if two given podTemplateSpec are equal, ignoring the diff in value of Labels[pod-template-hash]
// We ignore pod-template-hash because:
//  1. The hash result would be different upon podTemplateSpec API changes
//     (e.g. the addition of a new field will cause the hash code to change)
//  2. The deployment template won't have hash labels
func EqualIgnoreHash(template1, template2 *v1beta1.ExtendedDeploymentSpec) bool {
	t1Copy := template1.DeepCopy()
	t2Copy := template2.DeepCopy()
	// Remove hash labels from template.Labels before comparing
	delete(t1Copy.Template.Labels, apps.DefaultDeploymentUniqueLabelKey)
	delete(t2Copy.Template.Labels, apps.DefaultDeploymentUniqueLabelKey)
	return apiequality.Semantic.DeepEqual(t1Copy, t2Copy)
}

// ReplicaSetsByCreationTimestamp sorts a list of ReplicaSet by creation timestamp, using their names as a tie breaker.
type ReplicaSetsByCreationTimestamp []v1beta1.InplaceSet

func (o ReplicaSetsByCreationTimestamp) Len() int      { return len(o) }
func (o ReplicaSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ReplicaSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// Revision returns the revision number of the input object.
func Revision(obj runtime.Object) (string, error) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return ``, err
	}
	anno := acc.GetAnnotations()
	if anno == nil {
		return "", nil
	}
	v := anno[AnnotationRevision]
	return v, nil
}

// SetFromReplicaSetTemplate sets the desired PodTemplateSpec from a replica set template to the given deployment.
func SetFromReplicaSetTemplate(deployment *v1beta1.ExtendedDeployment, spec v1beta1.ExtendedDeploymentSpec) *v1beta1.ExtendedDeployment {
	deployment.Spec = spec
	deployment.Spec.Template.ObjectMeta.Labels = CloneAndRemoveLabel(
		deployment.Spec.Template.ObjectMeta.Labels,
		appsv1.DefaultDeploymentUniqueLabelKey)
	return deployment
}

var annotationsToSkip = map[string]bool{
	v1.LastAppliedConfigAnnotation: true,
	AnnotationUpgradeConfirm:       true,
	AnnotationFailedFlag:           true,
	AnnotationRollbackTo:           true,
	AnnotationRevision:             true,
	AnnotationRollTerm:             true,
	//RevisionHistoryAnnotation:      true,
	//DesiredReplicasAnnotation:      true,
	//MaxReplicasAnnotation:          true,
}

func getSkippedAnnotations(annotations map[string]string) map[string]string {
	skippedAnnotations := make(map[string]string)
	for k, v := range annotations {
		if skipCopyAnnotation(k) {
			skippedAnnotations[k] = v
		}
	}
	return skippedAnnotations
}

// SetDeploymentAnnotationsTo sets deployment's annotations as given RS's annotations.
// This action should be done if and only if the deployment is rolling back to this rs.
// Note that apply and revision annotations are not changed.
func SetDeploymentAnnotationsTo(deployment *v1beta1.ExtendedDeployment, rollbackToRev *appsv1.ControllerRevision) {
	deployment.Annotations = getSkippedAnnotations(deployment.Annotations)
	for k, v := range rollbackToRev.Annotations {
		if !skipCopyAnnotation(k) {
			deployment.Annotations[k] = v
		}
	}
}

func QueryExtendedDeployment(c client.Client, ctx context.Context, key types.NamespacedName) (*v1beta1.ExtendedDeployment, error) {
	ret := &v1beta1.ExtendedDeployment{}
	err := c.Get(ctx, key, ret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("ExtendedDeployment query %v error: %v", key.String(), err)
		return nil, err
	}

	// 为兼容之前已经创建的资源，设置默认值
	// admission_webhook.GetDefaulter().SetDefault(ret)

	return ret, nil
}
