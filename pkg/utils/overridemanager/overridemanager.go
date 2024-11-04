/*
Copyright 2022.

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

package overridemanager

import (
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch/v5"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

// OverrideOption define the JSONPatch operator
type OverrideOption struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// ApplyOverridePolicies 返回选定分区的重载Template
func ApplyOverridePolicies(regionName string, deploy *v1beta1.ExtendedDeployment) *v1.PodTemplateSpec {
	orgTemplate := deploy.Spec.Template.DeepCopy()
	if len(deploy.Spec.Overriders) == 0 {
		return orgTemplate
	}
	for j := range deploy.Spec.Regions {
		if deploy.Spec.Regions[j].Name == regionName {

			for i := range deploy.Spec.Overriders {
				if deploy.Spec.Overriders[i].Name == regionName {
					applyTemplate := deploy.Spec.Template.DeepCopy()
					// 解析
					if err := applyJSONPatch(&applyTemplate, parseJSONPatchesByPlaintext(deploy.Spec.Overriders[i])); err != nil {
						klog.Errorf("Failed to apply overrides for resource, error: %v", err)
						return orgTemplate
					}
					return applyTemplate
				}
			}
			return orgTemplate
		}
	}
	return nil
}

// 解析传入的overriders成overrideOption
func parseJSONPatchesByPlaintext(inOverriders v1beta1.Overriders) []OverrideOption {
	outOverriders := make([]OverrideOption, 0, len(inOverriders.Plaintext))
	// 解析传入的overriders
	for i := range inOverriders.Plaintext {
		outOverriders = append(outOverriders, OverrideOption{
			Op:    inOverriders.Plaintext[i].Operator,
			Path:  inOverriders.Plaintext[i].Path,
			Value: inOverriders.Plaintext[i].Value,
		})
	}
	return outOverriders
}

// applyJSONPatch applies the override on to the given **v1.PodSpec object.
func applyJSONPatch(obj **v1.PodTemplateSpec, overrides []OverrideOption) error {
	jsonPatchBytes, err := json.Marshal(overrides)
	if err != nil {
		return err
	}

	patch, err := jsonpatch.DecodePatch(jsonPatchBytes)
	if err != nil {
		return err
	}

	objectJSONBytes, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	patchedObjectJSONBytes, err := patch.Apply(objectJSONBytes)
	if err != nil {
		return err
	}
	*obj = &v1.PodTemplateSpec{}
	return json.Unmarshal(patchedObjectJSONBytes, obj)
}
