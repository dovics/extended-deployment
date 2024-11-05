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
package deployregion

func IsSynced() bool {
	return gCache.Synced
}

func IsRegionFailed(regionName string) bool {
	return gCache.Regions[regionName].status == regionStatusFailed
}

func GetRegionAllocatablePodNum(region string, res *Resource) (podNum int64) {
	regionInfo, ok := gCache.Regions[region]
	if !ok || regionInfo.status == regionStatusFailed {
		return 0
	}

	podNum = 0
	for _, node := range regionInfo.Nodes {
		all := *node.Allocatable
		all.SubResource(res)

		podNum += all.MaxDivided(res.ResourceList())
	}
	return
}
