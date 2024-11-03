package admission_webhook

import (
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
)

func init() {
	defaulter.addTypeDefaultingFunc(&v1beta1.ExtendedDeployment{},
		func(obj runtime.Object) {
			setDefaultsForExtendedDeployment(obj.(*v1beta1.ExtendedDeployment))
		})
	defaulter.addTypeDefaultingFunc(&v1beta1.InplaceSet{},
		func(obj runtime.Object) {
			setDefaultsForInPlaceSet(obj.(*v1beta1.InplaceSet))
		})
}

type ObjSetDefaultFunc func(obj runtime.Object)

var defaulter = &Defaulter{
	funcMap: map[reflect.Type]ObjSetDefaultFunc{},
}

// GetDefaulter 为兼容已创建的资源，同步过程中，先设置默认值
func GetDefaulter() *Defaulter {
	return defaulter
}

type Defaulter struct {
	funcMap map[reflect.Type]ObjSetDefaultFunc
}

func (d *Defaulter) addTypeDefaultingFunc(obj runtime.Object, f ObjSetDefaultFunc) {
	if _, ok := d.funcMap[reflect.TypeOf(obj)]; ok {
		panic(fmt.Errorf("type %v has been added to defaulter", reflect.TypeOf(obj)))
	}
	d.funcMap[reflect.TypeOf(obj)] = f
}

func (d *Defaulter) SetDefault(obj runtime.Object) {
	f, ok := d.funcMap[reflect.TypeOf(obj)]
	if !ok {
		return
	}
	f(obj)
}

func containsReadinessGate(gates []corev1.PodReadinessGate, c corev1.PodConditionType) bool {
	for i := range gates {
		if gates[i].ConditionType == c {
			return true
		}
	}
	return false
}

// setDefaultsForExtendedDeployment
func setDefaultsForExtendedDeployment(obj *v1beta1.ExtendedDeployment) {
	if obj.Spec.Strategy.AutoReschedule == nil {
		obj.Spec.Strategy.AutoReschedule = &v1beta1.AutoReschedule{
			Enable:         false,
			TimeoutSeconds: 60,
		}
	}
	if obj.Spec.Strategy.UpdateStrategy == nil {
		obj.Spec.Strategy.UpdateStrategy = &v1beta1.UpdateStrategy{
			Type:               v1beta1.RecreateUpdateStrategyType,
			GracePeriodSeconds: 5,
		}
	} else if obj.Spec.Strategy.UpdateStrategy.Type == "" {
		obj.Spec.Strategy.UpdateStrategy.Type = v1beta1.RecreateUpdateStrategyType
	}

	if obj.Spec.Strategy.UpdateStrategy.Type == v1beta1.RollingUpdateStrategyType &&
		obj.Spec.Strategy.UpdateStrategy.RollingUpdate == nil {
		defaultPercent := intstr.FromString("25%")
		obj.Spec.Strategy.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &defaultPercent,
			MaxSurge:       &defaultPercent,
		}
	}

	if obj.Spec.Strategy.RolloutStrategy == "" {
		obj.Spec.Strategy.RolloutStrategy = v1beta1.BetaRolloutStrategyType
	}

	if obj.Spec.Strategy.UpdateStrategy.Type == v1beta1.InPlaceIfPossibleUpdateStrategyType &&
		!containsReadinessGate(obj.Spec.Template.Spec.ReadinessGates, v1beta1.InPlaceUpdateReady) {
		obj.Spec.Template.Spec.ReadinessGates = append(obj.Spec.Template.Spec.ReadinessGates,
			corev1.PodReadinessGate{ConditionType: v1beta1.InPlaceUpdateReady})
	}
	return
}

// setDefaultsForInPlaceSet
func setDefaultsForInPlaceSet(obj *v1beta1.InplaceSet) {
	if obj.Spec.UpdateStrategy.Type == v1beta1.InPlaceIfPossibleUpdateStrategyType &&
		!containsReadinessGate(obj.Spec.Template.Spec.ReadinessGates, v1beta1.InPlaceUpdateReady) {
		obj.Spec.Template.Spec.ReadinessGates = append(obj.Spec.Template.Spec.ReadinessGates,
			corev1.PodReadinessGate{ConditionType: v1beta1.InPlaceUpdateReady})
	}

	return
}
