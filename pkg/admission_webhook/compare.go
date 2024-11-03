package admission_webhook

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

var comparator = &Comparator{
	funcs: map[reflect.Type]CompareObjFunc{},
}

func init() {
	comparator.AddTypeCompareFunc(&v1beta1.ExtendedDeployment{},
		func(newObj runtime.Object, oldObj runtime.Object) (errors []error) {
			return compareForExtendedDeployment(newObj.(*v1beta1.ExtendedDeployment), oldObj.(*v1beta1.ExtendedDeployment))
		})
}

type Comparator struct {
	funcs map[reflect.Type]CompareObjFunc
}

type CompareObjFunc func(newObj runtime.Object, oldObj runtime.Object) (errors []error)

func (c *Comparator) AddTypeCompareFunc(obj runtime.Object, f CompareObjFunc) {
	c.funcs[reflect.TypeOf(obj)] = f
}

func (c *Comparator) Compare(newObj runtime.Object, oldObj runtime.Object) (errors []error) {
	f, ok := c.funcs[reflect.TypeOf(newObj)]
	if !ok {
		return nil
	}

	return f(newObj, oldObj)
}

func compareLabelSelector(new, old *metav1.LabelSelector) (errorAll []error) {
	if !reflect.DeepEqual(new, old) {
		return []error{fmt.Errorf("spec.selector: Invalid value: %v, field is immutable", new)}
	}
	return nil
}

func compareSubSetType(new, old v1beta1.SubsetType) (errorAll []error) {
	if new != old {
		return []error{fmt.Errorf("spec.subsetType: Invalid value: %v, field is immutable", new)}
	}

	return nil
}

func isRegionReplicasTypeConsistent(regions []v1beta1.Region) bool {
	if len(regions) == 0 {
		return true
	}

	targetType := regions[0].Replicas.Type
	for _, region := range regions {
		if region.Replicas != nil && region.Replicas.Type != targetType {
			return false
		}
	}

	return true
}

func compareReplicas(new *v1beta1.ExtendedDeployment) (errorAll []error) {
	if new.Spec.Replicas != nil && *new.Spec.Replicas > 0 {
		for _, region := range new.Spec.Regions {
			if region.Replicas != nil && region.Replicas.Type == intstr.Int {
				if isRegionReplicasTypeConsistent(new.Spec.Regions) {
					klog.Warning("spec.Replicas has set, but region.Replicas type is int, continue for type consistent")
					return nil
				}

				return []error{fmt.Errorf("can not support int replicas for regions:%v", region)}
			}
		}

		totalNum := 0
		for _, region := range new.Spec.Regions {
			if region.Replicas != nil && region.Replicas.Type == intstr.String {

				if !strings.HasSuffix(region.Replicas.StrVal, "%") {
					return []error{fmt.Errorf("region replicas (%s) only support percentage value with a suffix '%%'", region.Replicas.StrVal)}
				}

				intPart := region.Replicas.StrVal[:len(region.Replicas.StrVal)-1]
				percent64, err := strconv.ParseInt(intPart, 10, 32)
				if err != nil {
					return []error{fmt.Errorf("region replicas (%s) should be correct percentage integer: %s", region.Replicas.StrVal, err)}
				}

				if percent64 > int64(100) || percent64 < int64(0) {
					return []error{fmt.Errorf("region replicas (%s) should be in range [0, 100]", region.Replicas.StrVal)}
				}

				totalNum = totalNum + int(percent64)
			}
		}
		if totalNum != 100 {
			return []error{fmt.Errorf("region replicas sum up should be 100")}
		}
	} else {
		for _, region := range new.Spec.Regions {
			if region.Replicas != nil && region.Replicas.Type == intstr.String {
				return []error{fmt.Errorf("region replicas (%s) is not valid", region.Replicas.StrVal)}
			}
		}
	}
	return nil
}

func compareForExtendedDeployment(new, old *v1beta1.ExtendedDeployment) (errors []error) {
	errors = append(errors, compareLabelSelector(new.Spec.Selector, old.Spec.Selector)...)
	errors = append(errors, compareSubSetType(new.Spec.SubsetType, old.Spec.SubsetType)...)
	errors = append(errors, compareReplicas(new)...)
	return
}
