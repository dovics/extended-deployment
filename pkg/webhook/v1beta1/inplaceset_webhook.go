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
	"k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	extendeddeploymentiov1beta1 "github.com/dovics/extendeddeployment/api/v1beta1"
)

// nolint:unused
// log is for logging in this package.
var inplacesetlog = logf.Log.WithName("inplaceset-resource")

// SetupInplaceSetWebhookWithManager registers the webhook for InplaceSet in the manager.
func SetupInplaceSetWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&extendeddeploymentiov1beta1.InplaceSet{}).
		WithValidator(&InplaceSetCustomValidator{}).
		WithDefaulter(&InplaceSetCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-extendeddeployment-io-v1beta1-inplaceset,mutating=true,failurePolicy=fail,sideEffects=None,groups=extendeddeployment.io,resources=inplacesets,verbs=create;update,versions=v1beta1,name=minplaceset-v1beta1.kb.io,admissionReviewVersions=v1

// InplaceSetCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind InplaceSet when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type InplaceSetCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &InplaceSetCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind InplaceSet.
func (d *InplaceSetCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	inplaceset, ok := obj.(*extendeddeploymentiov1beta1.InplaceSet)

	if !ok {
		return fmt.Errorf("expected an InplaceSet object but got %T", obj)
	}
	inplacesetlog.Info("Defaulting for InplaceSet", "name", inplaceset.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-extendeddeployment-io-v1beta1-inplaceset,mutating=false,failurePolicy=fail,sideEffects=None,groups=extendeddeployment.io,resources=inplacesets,verbs=create;update,versions=v1beta1,name=vinplaceset-v1beta1.kb.io,admissionReviewVersions=v1

// InplaceSetCustomValidator struct is responsible for validating the InplaceSet resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type InplaceSetCustomValidator struct {
	//TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &InplaceSetCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type InplaceSet.
func (v *InplaceSetCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	inplaceset, ok := obj.(*extendeddeploymentiov1beta1.InplaceSet)
	if !ok {
		return nil, fmt.Errorf("expected a InplaceSet object but got %T", obj)
	}
	inplacesetlog.Info("Validation for InplaceSet upon creation", "name", inplaceset.GetName())

	if errs := validateForInPlaceSet(inplaceset); len(errs) > 0 {
		return nil, errors.NewAggregate(errs)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type InplaceSet.
func (v *InplaceSetCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	inplaceset, ok := newObj.(*extendeddeploymentiov1beta1.InplaceSet)
	if !ok {
		return nil, fmt.Errorf("expected a InplaceSet object for the newObj but got %T", newObj)
	}
	inplacesetlog.Info("Validation for InplaceSet upon update", "name", inplaceset.GetName())

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type InplaceSet.
func (v *InplaceSetCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	inplaceset, ok := obj.(*extendeddeploymentiov1beta1.InplaceSet)
	if !ok {
		return nil, fmt.Errorf("expected a InplaceSet object but got %T", obj)
	}
	inplacesetlog.Info("Validation for InplaceSet upon deletion", "name", inplaceset.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
