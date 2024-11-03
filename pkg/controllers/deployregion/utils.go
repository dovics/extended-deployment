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
