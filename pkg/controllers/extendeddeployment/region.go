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
package extendeddeployment

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

func (dc *ExtendedDeploymentReconciler) checkRegions(ctx context.Context, deploy *v1beta1.ExtendedDeployment, key string) (map[string]*v1beta1.DeployRegion, error) {
	res := make(map[string]*v1beta1.DeployRegion)
	regionList := &v1beta1.DeployRegionList{}
	err := dc.List(ctx, regionList)
	if err != nil {
		klog.Errorf("extendeddeployment %v query region list error: %v", key, err)
		return nil, err
	}

	mp := make(map[string]*v1beta1.DeployRegion)
	for i := range regionList.Items {
		r := regionList.Items[i]
		mp[r.Name] = &r
	}

	totalReplicasHasSet := deploy.Spec.Replicas != nil && *deploy.Spec.Replicas > 0
	for _, region := range deploy.Spec.Regions {
		r, ok := mp[region.Name]
		if !ok {
			klog.Errorf("extendeddeployment %v query region error, region %v is not exists",
				key, region.Name)
			err := fmt.Errorf("region %v is not exists, ", region.Name)
			dc.emitWarningEvent(deploy, "DeployRegionNotExists", err.Error())
			return nil, err
		}
		res[region.Name] = r.DeepCopy()

		if totalReplicasHasSet && region.Replicas.Type == intstr.Int {
			klog.Warningf("extendeddeployment %v region replicas error,  the replicas in region %v should be percent",
				key, region.Name)
			err := fmt.Errorf("the replicas in region %v should be percent, will porocessed according the value in region ", region.Name)
			dc.emitWarningEvent(deploy, "DeployRegionReplicasTypeError", err.Error())
		}
	}

	return res, nil
}
