package overridemanager

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

func TestApplyOverridePolicies(t *testing.T) {
	orgPodSpec := v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "hello-override",
					Image: "nginx",
					Command: []string{
						"./start.sh",
					},
					Args: []string{
						"--kubectl=.config.yml.yaml",
						"--tt=test",
					},
					WorkingDir:               "",
					Ports:                    nil,
					EnvFrom:                  nil,
					Env:                      nil,
					Resources:                v1.ResourceRequirements{},
					VolumeMounts:             nil,
					VolumeDevices:            nil,
					LivenessProbe:            nil,
					ReadinessProbe:           nil,
					StartupProbe:             nil,
					Lifecycle:                nil,
					TerminationMessagePath:   "",
					TerminationMessagePolicy: "",
					ImagePullPolicy:          "",
					SecurityContext:          nil,
					Stdin:                    false,
					StdinOnce:                false,
					TTY:                      false,
				},
			},
		},
	}
	type test struct {
		region     v1beta1.Region
		overriders *v1beta1.Overriders
		expPodSpec v1.PodTemplateSpec
	}

	tt := []test{
		{
			region: v1beta1.Region{
				Name: "region0",
			},
			overriders: &v1beta1.Overriders{
				Name: "region0",
				Plaintext: []v1beta1.PlaintextOverrider{
					{
						Path:     "/spec/containers/0/image",
						Operator: "replace",
						Value:    apiextensionsv1.JSON{Raw: []byte(`"nginx-override"`)},
					},
					{
						Path:     "/spec/containers/0/command",
						Operator: "replace",
						Value:    apiextensionsv1.JSON{Raw: []byte(`["./startcommand-override.sh","12"]`)},
					},
					{
						Path:     "/spec/containers/0/args",
						Operator: "replace",
						Value:    apiextensionsv1.JSON{Raw: []byte(`["--tt=test"]`)},
					},
				},
			},
			expPodSpec: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "hello-override",
							Image: "nginx-override",
							Command: []string{
								"./startcommand-override.sh", "12",
							},
							Args: []string{
								"--tt=test",
							},
							WorkingDir:               "",
							Ports:                    nil,
							EnvFrom:                  nil,
							Env:                      nil,
							Resources:                v1.ResourceRequirements{},
							VolumeMounts:             nil,
							VolumeDevices:            nil,
							LivenessProbe:            nil,
							ReadinessProbe:           nil,
							StartupProbe:             nil,
							Lifecycle:                nil,
							TerminationMessagePath:   "",
							TerminationMessagePolicy: "",
							ImagePullPolicy:          "",
							SecurityContext:          nil,
							Stdin:                    false,
							StdinOnce:                false,
							TTY:                      false,
						},
					},
				},
			},
		},
		{
			region: v1beta1.Region{
				Name: "region1",
			},
			overriders: &v1beta1.Overriders{
				Name: "region1",
				Plaintext: []v1beta1.PlaintextOverrider{
					{
						Path:     "/spec/containers/0/image",
						Operator: "remove",
					},
					{
						Path:     "/spec/containers/0/command",
						Operator: "remove",
					},
					{
						Path:     "/spec/containers/0/args",
						Operator: "remove",
					},
				},
			},
			expPodSpec: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:                     "hello-override",
							WorkingDir:               "",
							Ports:                    nil,
							EnvFrom:                  nil,
							Env:                      nil,
							Resources:                v1.ResourceRequirements{},
							VolumeMounts:             nil,
							VolumeDevices:            nil,
							LivenessProbe:            nil,
							ReadinessProbe:           nil,
							StartupProbe:             nil,
							Lifecycle:                nil,
							TerminationMessagePath:   "",
							TerminationMessagePolicy: "",
							ImagePullPolicy:          "",
							SecurityContext:          nil,
							Stdin:                    false,
							StdinOnce:                false,
							TTY:                      false,
						},
					},
				},
			},
		},
		{
			region: v1beta1.Region{
				Name: "region1",
			},
			overriders: &v1beta1.Overriders{
				Name: "region1",
				Plaintext: []v1beta1.PlaintextOverrider{
					{
						Path:     "/spec/containers/0/image",
						Operator: "add",
						Value:    apiextensionsv1.JSON{Raw: []byte(`"000"`)},
					},
					{
						Path:     "/spec/containers/0/command",
						Operator: "add",
						Value:    apiextensionsv1.JSON{Raw: []byte(`["111","222"]`)},
					},
					{
						Path:     "/spec/containers/0/args",
						Operator: "add",
						Value:    apiextensionsv1.JSON{Raw: []byte(`["333","444"]`)},
					},
				},
			},
			expPodSpec: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "hello-override",
							Image: "000",
							Command: []string{
								"111",
								"222",
							},
							Args: []string{
								"333",
								"444",
							},
							WorkingDir:               "",
							Ports:                    nil,
							EnvFrom:                  nil,
							Env:                      nil,
							Resources:                v1.ResourceRequirements{},
							VolumeMounts:             nil,
							VolumeDevices:            nil,
							LivenessProbe:            nil,
							ReadinessProbe:           nil,
							StartupProbe:             nil,
							Lifecycle:                nil,
							TerminationMessagePath:   "",
							TerminationMessagePolicy: "",
							ImagePullPolicy:          "",
							SecurityContext:          nil,
							Stdin:                    false,
							StdinOnce:                false,
							TTY:                      false,
						},
					},
				},
			},
		},
		{
			region: v1beta1.Region{
				Name: "region1",
			},
			overriders: &v1beta1.Overriders{
				Name: "region1",
				Plaintext: []v1beta1.PlaintextOverrider{
					{
						Path:     "/spec/containers/0/image",
						Operator: "replace",
						Value:    apiextensionsv1.JSON{Raw: []byte(`"nginx-override"`)},
					},
					{
						Path:     "/spec/containers/0/command",
						Operator: "add",
						Value:    apiextensionsv1.JSON{Raw: []byte(`["111","222"]`)},
					},
					{
						Path:     "/spec/containers/0/args",
						Operator: "remove",
					},
				},
			},
			expPodSpec: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "hello-override",
							Image: "nginx-override",
							Command: []string{
								"111",
								"222",
							},
							WorkingDir:               "",
							Ports:                    nil,
							EnvFrom:                  nil,
							Env:                      nil,
							Resources:                v1.ResourceRequirements{},
							VolumeMounts:             nil,
							VolumeDevices:            nil,
							LivenessProbe:            nil,
							ReadinessProbe:           nil,
							StartupProbe:             nil,
							Lifecycle:                nil,
							TerminationMessagePath:   "",
							TerminationMessagePolicy: "",
							ImagePullPolicy:          "",
							SecurityContext:          nil,
							Stdin:                    false,
							StdinOnce:                false,
							TTY:                      false,
						},
					},
				},
			},
		},
		{
			region: v1beta1.Region{
				Name: "region0",
			},
			overriders: nil,
			expPodSpec: orgPodSpec,
		},
	}

	for i := range tt {
		overriders := []v1beta1.Overriders{}
		if tt[i].overriders == nil {
			overriders = nil
		} else {
			overriders = append(overriders, *tt[i].overriders)
		}
		ret := ApplyOverridePolicies(tt[i].region.Name, &v1beta1.ExtendedDeployment{
			Spec: v1beta1.ExtendedDeploymentSpec{
				Template:   orgPodSpec,
				Regions:    []v1beta1.Region{tt[i].region},
				Overriders: overriders,
			},
		})
		if !reflect.DeepEqual(*ret, tt[i].expPodSpec) {
			t.Errorf("err unexpected ret on %d loop ", i)
		}
	}
}

func TestApplyJSONPatch(t *testing.T) {
	type test struct {
		overrideOption []OverrideOption
		expPodSpec     v1.PodSpec
	}
	orgPododTemplateSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "hello-override",
					Image: "nginx",
					Args: []string{
						"--kubectl=.config.yml.yaml",
						"--tt=test",
					},
					WorkingDir:               "",
					Ports:                    nil,
					EnvFrom:                  nil,
					Env:                      nil,
					Resources:                v1.ResourceRequirements{},
					VolumeMounts:             nil,
					VolumeDevices:            nil,
					LivenessProbe:            nil,
					ReadinessProbe:           nil,
					StartupProbe:             nil,
					Lifecycle:                nil,
					TerminationMessagePath:   "",
					TerminationMessagePolicy: "",
					ImagePullPolicy:          "",
					SecurityContext:          nil,
					Stdin:                    false,
					StdinOnce:                false,
					TTY:                      false,
				},
			},
		},
	}
	tt := []test{
		{
			overrideOption: []OverrideOption{
				{
					Op:    "add",
					Path:  "/spec/containers/0/command",
					Value: []string{"./startcommand-override.sh", "12"},
				},
			},
			expPodSpec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "hello-override",
						Image: "nginx",
						Command: []string{
							"./startcommand-override.sh",
							"12",
						},
						Args: []string{
							"--kubectl=.config.yml.yaml",
							"--tt=test",
						},
						WorkingDir:               "",
						Ports:                    nil,
						EnvFrom:                  nil,
						Env:                      nil,
						Resources:                v1.ResourceRequirements{},
						VolumeMounts:             nil,
						VolumeDevices:            nil,
						LivenessProbe:            nil,
						ReadinessProbe:           nil,
						StartupProbe:             nil,
						Lifecycle:                nil,
						TerminationMessagePath:   "",
						TerminationMessagePolicy: "",
						ImagePullPolicy:          "",
						SecurityContext:          nil,
						Stdin:                    false,
						StdinOnce:                false,
						TTY:                      false,
					},
				},
			},
		},
		{
			overrideOption: []OverrideOption{
				{
					Op:   "remove",
					Path: "/spec/containers/0/args",
				},
			},
			expPodSpec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:                     "hello-override",
						Image:                    "nginx",
						WorkingDir:               "",
						Ports:                    nil,
						EnvFrom:                  nil,
						Env:                      nil,
						Resources:                v1.ResourceRequirements{},
						VolumeMounts:             nil,
						VolumeDevices:            nil,
						LivenessProbe:            nil,
						ReadinessProbe:           nil,
						StartupProbe:             nil,
						Lifecycle:                nil,
						TerminationMessagePath:   "",
						TerminationMessagePolicy: "",
						ImagePullPolicy:          "",
						SecurityContext:          nil,
						Stdin:                    false,
						StdinOnce:                false,
						TTY:                      false,
					},
				},
			},
		},
		{
			overrideOption: []OverrideOption{
				{
					Op:    "replace",
					Path:  "/spec/containers/0/image",
					Value: "nginx-override",
				},
			},
			expPodSpec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "hello-override",
						Image: "nginx-override",
						Args: []string{
							"--kubectl=.config.yml.yaml",
							"--tt=test",
						},
						WorkingDir:               "",
						Ports:                    nil,
						EnvFrom:                  nil,
						Env:                      nil,
						Resources:                v1.ResourceRequirements{},
						VolumeMounts:             nil,
						VolumeDevices:            nil,
						LivenessProbe:            nil,
						ReadinessProbe:           nil,
						StartupProbe:             nil,
						Lifecycle:                nil,
						TerminationMessagePath:   "",
						TerminationMessagePolicy: "",
						ImagePullPolicy:          "",
						SecurityContext:          nil,
						Stdin:                    false,
						StdinOnce:                false,
						TTY:                      false,
					},
				},
			},
		},
	}
	for i := range tt {
		inOrgPododTemplateSpec := orgPododTemplateSpec.DeepCopy()
		if err := applyJSONPatch(&inOrgPododTemplateSpec, tt[i].overrideOption); err == nil {
			if !reflect.DeepEqual((*inOrgPododTemplateSpec).Spec, tt[i].expPodSpec) {
				t.Errorf("err unexpected ret on %d loop ", i)
			}

		} else {
			t.Errorf("err unexpected ret on %d loop err: %v", i, err)
		}
	}
}
