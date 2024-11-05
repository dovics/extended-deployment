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
package admission_webhook

import (
	"errors"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/apis/apps"
	apiappsv1 "k8s.io/kubernetes/pkg/apis/apps/v1"
	"k8s.io/kubernetes/pkg/apis/core/validation"

	_ "k8s.io/kubernetes/pkg/apis/apps/install" // 必须有，保证schema注册

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

func init() {
	validator.AddTypeValidateFunc(&v1beta1.ExtendedDeployment{},
		func(obj runtime.Object) (errors []error) {
			return validateForExtendedDeployment(obj.(*v1beta1.ExtendedDeployment))
		})
	validator.AddTypeValidateFunc(&v1beta1.InplaceSet{},
		func(obj runtime.Object) (errors []error) {
			return validateForInPlaceSet(obj.(*v1beta1.InplaceSet))
		})
}

type ValidateObjFunc func(obj runtime.Object) (errors []error)

type Validator struct {
	funcs map[reflect.Type]ValidateObjFunc
}

var validator = &Validator{
	funcs: map[reflect.Type]ValidateObjFunc{},
}

func (v *Validator) AddTypeValidateFunc(obj runtime.Object, f ValidateObjFunc) {
	v.funcs[reflect.TypeOf(obj)] = f
}

func (v *Validator) Validate(obj runtime.Object) (errors []error) {
	f, ok := v.funcs[reflect.TypeOf(obj)]
	if !ok {
		return nil
	}
	return f(obj)
}

func validatePodTemplate(template *corev1.PodTemplateSpec) (errorAll []error) {
	if template.Spec.RestartPolicy != "" &&
		template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		return []error{errors.New("extendeddeployment pod.spec.restartPolicy need Always or null")}
	}
	// 将其构建为一个 rs ，设置默认值后转为内部版本
	rs := &appsv1.ReplicaSet{
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: template.Labels},
			Template: *template,
		},
	}
	apiappsv1.SetObjectDefaults_ReplicaSet(rs)
	out, err := runtime.UnsafeObjectConvertor(legacyscheme.Scheme).ConvertToVersion(rs, runtime.InternalGroupVersioner)
	if err != nil {
		return []error{err}
	}

	got, ok := out.(*apps.ReplicaSet)
	if !ok {
		return []error{fmt.Errorf("convert to version return object error: %+v", got)}
	}

	errs := validation.ValidatePodTemplateSpec(&got.Spec.Template, field.NewPath("spec.template"), validation.PodValidationOptions{})
	for _, e := range errs {
		errorAll = append(errorAll, e)
	}
	return
}

func validateForExtendedDeployment(obj *v1beta1.ExtendedDeployment) (errors []error) {
	errors = append(errors, validatePodTemplate(&obj.Spec.Template)...)
	return
}

func validateForInPlaceSet(obj *v1beta1.InplaceSet) (errors []error) {
	errors = append(errors, validatePodTemplate(&obj.Spec.Template)...)
	return
}
