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

// InplaceSetAdapter implements the Adapter interface for Deployment objects
type InplaceSetAdapter struct {
	client.Client

	Scheme *runtime.Scheme
}

// NewResourceObject creates a empty Deployment object.
func (a *InplaceSetAdapter) NewResourceObject() client.Object {
	return &v1beta1.InplaceSet{}
}

// NewResourceListObject creates a empty DeploymentList object.
func (a *InplaceSetAdapter) NewResourceListObject() ObjectList {
	return &v1beta1.InplaceSetList{}
}

// GetStatusObservedGeneration returns the observed generation of the subset.
func (a *InplaceSetAdapter) GetStatusObservedGeneration(obj metav1.Object) int64 {
	return obj.(*v1beta1.InplaceSet).Status.ObservedGeneration
}

// GetReplicaDetails returns the replicas detail the subset needs.
func (a *InplaceSetAdapter) GetReplicaDetails(obj metav1.Object, subset *Subset) (err error) {
	// Convert to InplaceSet Object
	set := obj.(*v1beta1.InplaceSet)

	// spec
	subset.Spec.Replicas = set.Spec.Replicas
	subset.Spec.MinReadySeconds = set.Spec.MinReadySeconds
	subset.Spec.UpdateStrategy.Type = set.Spec.UpdateStrategy.Type
	if set.Spec.UpdateStrategy.InPlaceUpdateStrategy != nil {
		subset.Spec.UpdateStrategy.InPlaceUpdateStrategy = &InPlaceUpdateStrategy{
			GracePeriodSeconds: set.Spec.UpdateStrategy.InPlaceUpdateStrategy.GracePeriodSeconds,
		}
	}

	subset.Spec.Template = *set.Spec.Template.DeepCopy()
	subset.Spec.Selector = set.Spec.Selector.DeepCopy()

	// status
	subset.Status.Replicas = set.Status.Replicas
	subset.Status.AvailableReplicas = set.Status.AvailableReplicas
	if set.Status.InplaceUpdateStatus != nil {
		subset.Status.InplaceUpdateStatus = &InplaceUpdateStatus{
			AvailableReplicas: set.Status.InplaceUpdateStatus.AvailableReplicas,
			PodTemplateHash:   set.Status.InplaceUpdateStatus.PodTemplateHash,
		}
	}

	return
}

// ApplySubsetTemplate updates the subset to the latest revision, depending on the DeploymentTemplate.
func (a *InplaceSetAdapter) ApplySubsetTemplate(cd *v1beta1.ExtendedDeployment, dr *v1beta1.DeployRegion,
	template *corev1.PodTemplateSpec, replicas int32, obj runtime.Object) error {

	set := obj.(*v1beta1.InplaceSet)
	ssTemplate := template.DeepCopy()

	podTemplateSpecHash := controller.ComputeHash(ssTemplate, cd.Status.CollisionCount)
	*cd.Status.CollisionCount++

	// pod template label 和 inplaceset label selector 同步更新
	ssTemplate.Labels = utils.CloneAndAddLabel(template.Labels,
		appsv1.DefaultDeploymentUniqueLabelKey, podTemplateSpecHash)
	newISSelector := utils.CloneSelectorAndAddLabel(cd.Spec.Selector,
		appsv1.DefaultDeploymentUniqueLabelKey, podTemplateSpecHash)
	SetSubsetRegionAffinity(&ssTemplate.Spec, dr)

	set.Name = cd.Name + "-" + dr.Name + "-" + podTemplateSpecHash
	set.Namespace = cd.Namespace
	// 添加了一个region标签，用于根据分区查询到分区下的Subset
	set.Labels = utils.CloneAndAddLabel(template.Labels, utils.RegionLabel, dr.Name)
	set.Spec.Selector = newISSelector
	set.Spec.Replicas = &replicas
	set.Spec.Template = *ssTemplate

	set.Spec.MinReadySeconds = cd.Spec.Strategy.MinReadySeconds

	switch cd.Spec.Strategy.UpdateStrategy.Type {
	case v1beta1.InPlaceIfPossibleUpdateStrategyType, v1beta1.InPlaceOnlyUpdateStrategyType:
		set.Spec.UpdateStrategy.Type = v1beta1.InPlaceIfPossibleUpdateStrategyType
	case v1beta1.DeleteAllFirstUpdateStrategyType, v1beta1.RecreateUpdateStrategyType, v1beta1.RollingUpdateStrategyType:
		set.Spec.UpdateStrategy.Type = v1beta1.RecreateUpdateStrategyType
	}
	set.Spec.UpdateStrategy.InPlaceUpdateStrategy = &v1beta1.InPlaceUpdateStrategy{
		GracePeriodSeconds: cd.Spec.Strategy.UpdateStrategy.GracePeriodSeconds,
	}

	SetNewReplicaSetAnnotations(cd, &set.ObjectMeta)

	return nil
}

func (a *InplaceSetAdapter) SetReplicas(obj runtime.Object, replicas int32) {
	set := obj.(*v1beta1.InplaceSet)
	set.Spec.Replicas = &replicas
}

func (a *InplaceSetAdapter) SetUpdateStrategy(obj runtime.Object, cd *v1beta1.ExtendedDeployment) {
	set := obj.(*v1beta1.InplaceSet)
	set.Spec.MinReadySeconds = cd.Spec.Strategy.MinReadySeconds
	switch cd.Spec.Strategy.UpdateStrategy.Type {
	case v1beta1.InPlaceIfPossibleUpdateStrategyType, v1beta1.InPlaceOnlyUpdateStrategyType:
		set.Spec.UpdateStrategy.Type = v1beta1.InPlaceIfPossibleUpdateStrategyType
	case v1beta1.DeleteAllFirstUpdateStrategyType, v1beta1.RecreateUpdateStrategyType, v1beta1.RollingUpdateStrategyType:
		set.Spec.UpdateStrategy.Type = v1beta1.RecreateUpdateStrategyType
	}

	set.Spec.UpdateStrategy.InPlaceUpdateStrategy = &v1beta1.InPlaceUpdateStrategy{
		GracePeriodSeconds: cd.Spec.Strategy.UpdateStrategy.GracePeriodSeconds,
	}
}
