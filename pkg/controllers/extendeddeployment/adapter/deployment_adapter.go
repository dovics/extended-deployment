package adapter

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

// DeploymentAdapter implements the Adapter interface for Deployment objects
type DeploymentAdapter struct {
	client.Client

	Scheme *runtime.Scheme
}

// NewResourceObject creates an empty Deployment object.
func (a *DeploymentAdapter) NewResourceObject() client.Object {
	return &appsv1.Deployment{}
}

// NewResourceListObject creates an empty DeploymentList object.
func (a *DeploymentAdapter) NewResourceListObject() ObjectList {
	return &appsv1.DeploymentList{}
}

// GetStatusObservedGeneration returns the observed generation of the subset.
func (a *DeploymentAdapter) GetStatusObservedGeneration(obj metav1.Object) int64 {
	return obj.(*appsv1.Deployment).Status.ObservedGeneration
}

// GetReplicaDetails returns the replicas detail the subset needs.
func (a *DeploymentAdapter) GetReplicaDetails(obj metav1.Object, subset *Subset) (err error) {
	// Convert to Deployment Object
	set := obj.(*appsv1.Deployment)

	subset.Spec.Replicas = set.Spec.Replicas
	subset.Spec.MinReadySeconds = set.Spec.MinReadySeconds

	subset.Spec.Template = *set.Spec.Template.DeepCopy()
	subset.Spec.Selector = set.Spec.Selector.DeepCopy()

	subset.Status.Replicas = set.Status.Replicas
	subset.Status.AvailableReplicas = set.Status.AvailableReplicas

	return
}

// ApplySubsetTemplate updates the subset to the latest revision, depending on the DeploymentTemplate.
func (a *DeploymentAdapter) ApplySubsetTemplate(cd *v1beta1.ExtendedDeployment, dr *v1beta1.DeployRegion,
	template *corev1.PodTemplateSpec, replicas int32, obj runtime.Object) error {

	set := obj.(*appsv1.Deployment)
	ssTemplate := template.DeepCopy()

	podTemplateSpecHash := controller.ComputeHash(ssTemplate, cd.Status.CollisionCount)
	*cd.Status.CollisionCount++

	// pod template label 和 inplaceset label selector 同步更新
	// ssTemplate.Labels = utils.CloneAndAddLabel(template.Labels,
	// 	appsv1.DefaultDeploymentUniqueLabelKey, podTemplateSpecHash)
	// newISSelector := utils.CloneSelectorAndAddLabel(cd.Spec.Selector,
	// 	appsv1.DefaultDeploymentUniqueLabelKey, podTemplateSpecHash)
	SetSubsetRegionAffinity(&ssTemplate.Spec, dr)

	set.Name = cd.Name + "-" + dr.Name + "-" + podTemplateSpecHash
	set.Namespace = cd.Namespace
	// 添加了一个region标签，用于根据分区查询到分区下的Subset
	set.Labels = utils.CloneAndAddLabel(template.Labels, utils.RegionLabel, dr.Name)
	set.Spec.Selector = cd.Spec.Selector
	set.Spec.Replicas = &replicas
	set.Spec.Template = *ssTemplate

	set.Spec.MinReadySeconds = cd.Spec.Strategy.MinReadySeconds
	if cd.Spec.Strategy.UpdateStrategy.Type == v1beta1.RecreateUpdateStrategyType {
		set.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
	}

	// 对于Deployment，需要删除原地升级的 ReadinessGate
	readinessGates := set.Spec.Template.Spec.ReadinessGates
	set.Spec.Template.Spec.ReadinessGates = []corev1.PodReadinessGate{}
	for _, r := range readinessGates {
		if r.ConditionType != v1beta1.InPlaceUpdateReady {
			set.Spec.Template.Spec.ReadinessGates = append(
				set.Spec.Template.Spec.ReadinessGates, r)
		}
	}

	SetNewReplicaSetAnnotations(cd, &set.ObjectMeta)

	return nil
}

func (a *DeploymentAdapter) SetReplicas(obj runtime.Object, replicas int32) {
	set := obj.(*appsv1.Deployment)
	set.Spec.Replicas = &replicas
}

func (a *DeploymentAdapter) SetUpdateStrategy(obj runtime.Object, cd *v1beta1.ExtendedDeployment) {
	set := obj.(*appsv1.Deployment)
	set.Spec.MinReadySeconds = cd.Spec.Strategy.MinReadySeconds
}
