package deployregion

import (
	"extendeddeployment.io/extended-deployment/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

var gCache = NewCache()

type AllocatedResource struct {
	MilliCPU         int64
	Memory           int64
	EphemeralStorage int64
	AllowedPodNumber int64
}

type nodeResourceCache struct {
	Name        string
	IsReady     bool
	Allocatable *Resource // 可分配资源
	Allocated   *Resource // 已分配资源

	Pods map[string]int // Pod 列表
}

type regionStatus string

const (
	regionStatusUnknown regionStatus = ""
	regionStatusOK      regionStatus = "ok"
	regionStatusFailed  regionStatus = "failed"
)

type regionResourceCache struct {
	Name  string
	Spec  *v1beta1.DeployRegionSpec
	Nodes map[string]*nodeResourceCache

	status regionStatus // 分区状态
}

type allResourceCache struct {
	Synced  bool
	Regions map[string]*regionResourceCache

	// node所在region映射(暂时不考虑中途修改node label)
	NodeToRegion map[string]string
}

func NewCache() *allResourceCache {
	return &allResourceCache{
		Regions: map[string]*regionResourceCache{},
	}
}

func (c *regionResourceCache) isNodeReady(node *corev1.Node) bool {
	for i := range node.Status.Conditions {
		if node.Status.Conditions[i].Type == corev1.NodeReady {
			return node.Status.Conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

func (c *regionResourceCache) AddNode(node *corev1.Node) *nodeResourceCache {
	ret := &nodeResourceCache{
		Name:        node.Name,
		IsReady:     c.isNodeReady(node),
		Allocatable: EmptyResource(),
		Allocated:   EmptyResource(),
		Pods:        map[string]int{},
	}
	ret.Allocatable.Add(node.Status.Allocatable)
	c.Nodes[node.Name] = ret
	return ret
}

func (c *regionResourceCache) DelNode(node *corev1.Node) {
	delete(c.Nodes, node.Name)
}

func (c *regionResourceCache) OnNodeUpdate(node *corev1.Node) {
	nodeCache, exists := c.Nodes[node.Name]
	if !exists {
		return
	}

	// 仅更新node状态和可分配资源
	nodeCache.IsReady = c.isNodeReady(node)
	nodeCache.Allocatable = EmptyResource()
	nodeCache.Allocatable.Add(node.Status.Allocatable)
}

func (c *nodeResourceCache) Score() int64 {
	if !c.IsReady {
		return 0
	}
	allocatable := c.Allocatable.MilliCPU + c.Allocatable.Memory
	allocated := c.Allocated.MilliCPU + c.Allocated.Memory
	return allocatable - allocated
}

func (c *nodeResourceCache) podKey(pod *corev1.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

func (c *nodeResourceCache) AddPod(pod *corev1.Pod) {
	if pod.Status.Phase == corev1.PodSucceeded ||
		pod.Status.Phase == corev1.PodFailed ||
		pod.DeletionTimestamp != nil {
		return
	}

	c.Pods[c.podKey(pod)] = 0
	for _, container := range pod.Spec.Containers {
		c.Allocated.Add(container.Resources.Requests)
	}
}

func (c *nodeResourceCache) OnPodUpdate(pod *corev1.Pod) {
	key := c.podKey(pod)
	_, exists := c.Pods[key]
	if !exists {
		c.AddPod(pod)
		return
	}

	if pod.DeletionTimestamp != nil ||
		pod.Status.Phase == corev1.PodSucceeded ||
		pod.Status.Phase == corev1.PodFailed {
		// 生命周期结束，回收Pod分配资源
		for _, container := range pod.Spec.Containers {
			_ = c.Allocated.Sub(container.Resources.Requests)
		}
		delete(c.Pods, key)
	}
}

func (c *allResourceCache) AddRegion(region *v1beta1.DeployRegion) *regionResourceCache {
	regionInfo := &regionResourceCache{
		Name:   region.Name,
		Spec:   region.Spec.DeepCopy(),
		Nodes:  map[string]*nodeResourceCache{},
		status: regionStatusUnknown,
	}
	c.Regions[region.Name] = regionInfo
	return regionInfo
}

func (c *allResourceCache) DelRegion(region *v1beta1.DeployRegion) {
	delete(c.Regions, region.Name)
}
