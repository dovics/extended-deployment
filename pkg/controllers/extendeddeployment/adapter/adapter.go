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
package adapter

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

type ObjectList interface {
	metav1.ListInterface
	runtime.Object
}

type Adapter interface {
	// NewResourceObject creates a empty subset object.
	NewResourceObject() client.Object
	// NewResourceListObject creates a empty subset list object.
	NewResourceListObject() ObjectList
	// GetStatusObservedGeneration returns the observed generation of the subset.
	GetStatusObservedGeneration(subset metav1.Object) int64
	// GetReplicaDetails returns the replicas information of the subset status.
	GetReplicaDetails(obj metav1.Object, subset *Subset) error
	// ApplySubsetTemplate updates the subset to the latest revision.
	ApplySubsetTemplate(cd *v1beta1.ExtendedDeployment, dr *v1beta1.DeployRegion,
		template *v1.PodTemplateSpec, replicas int32, obj runtime.Object) error
	// SetReplicas set replicas of subset
	SetReplicas(obj runtime.Object, replicas int32)
	// SetUpdateStrategy set MinReadySeconds and GracePeriodSeconds
	SetUpdateStrategy(obj runtime.Object, cd *v1beta1.ExtendedDeployment)
}
