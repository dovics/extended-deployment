package admission_webhook

import (
	"fmt"
	"testing"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
)

func TestValidate(t *testing.T) {
	_ = v1beta1.AddToScheme(legacyscheme.Scheme)
	obj := &v1beta1.ExtendedDeployment{
		Spec: v1beta1.ExtendedDeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test",
							Image: "test:v1",
						},
					},
				},
			},
		},
	}
	errs := validateForExtendedDeployment(obj)
	fmt.Println(errs)
}
