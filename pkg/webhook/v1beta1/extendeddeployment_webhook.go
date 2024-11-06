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

package v1beta1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	extendeddeploymentiov1beta1 "github.com/dovics/extendeddeployment"
)

// nolint:unused
// log is for logging in this package.
var extendeddeploymentlog = logf.Log.WithName("extendeddeployment-resource")

// SetupExtendedDeploymentWebhookWithManager registers the webhook for ExtendedDeployment in the manager.
func SetupExtendedDeploymentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&extendeddeploymentiov1beta1.ExtendedDeployment{}).
		WithValidator(&ExtendedDeploymentCustomValidator{}).
		WithDefaulter(&ExtendedDeploymentCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-extendeddeployment-io-v1beta1-extendeddeployment,mutating=true,failurePolicy=fail,sideEffects=None,groups=extendeddeployment.io,resources=extendeddeployments,verbs=create;update,versions=v1beta1,name=mextendeddeployment-v1beta1.kb.io,admissionReviewVersions=v1

// ExtendedDeploymentCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind ExtendedDeployment when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ExtendedDeploymentCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &ExtendedDeploymentCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind ExtendedDeployment.
func (d *ExtendedDeploymentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	extendeddeployment, ok := obj.(*extendeddeploymentiov1beta1.ExtendedDeployment)

	if !ok {
		return fmt.Errorf("expected an ExtendedDeployment object but got %T", obj)
	}
	extendeddeploymentlog.Info("Defaulting for ExtendedDeployment", "name", extendeddeployment.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-extendeddeployment-io-v1beta1-extendeddeployment,mutating=false,failurePolicy=fail,sideEffects=None,groups=extendeddeployment.io,resources=extendeddeployments,verbs=create;update,versions=v1beta1,name=vextendeddeployment-v1beta1.kb.io,admissionReviewVersions=v1

// ExtendedDeploymentCustomValidator struct is responsible for validating the ExtendedDeployment resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ExtendedDeploymentCustomValidator struct {
	//TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &ExtendedDeploymentCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ExtendedDeployment.
func (v *ExtendedDeploymentCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	extendeddeployment, ok := obj.(*extendeddeploymentiov1beta1.ExtendedDeployment)
	if !ok {
		return nil, fmt.Errorf("expected a ExtendedDeployment object but got %T", obj)
	}
	extendeddeploymentlog.Info("Validation for ExtendedDeployment upon creation", "name", extendeddeployment.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ExtendedDeployment.
func (v *ExtendedDeploymentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	extendeddeployment, ok := newObj.(*extendeddeploymentiov1beta1.ExtendedDeployment)
	if !ok {
		return nil, fmt.Errorf("expected a ExtendedDeployment object for the newObj but got %T", newObj)
	}
	extendeddeploymentlog.Info("Validation for ExtendedDeployment upon update", "name", extendeddeployment.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ExtendedDeployment.
func (v *ExtendedDeploymentCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	extendeddeployment, ok := obj.(*extendeddeploymentiov1beta1.ExtendedDeployment)
	if !ok {
		return nil, fmt.Errorf("expected a ExtendedDeployment object but got %T", obj)
	}
	extendeddeploymentlog.Info("Validation for ExtendedDeployment upon deletion", "name", extendeddeployment.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
