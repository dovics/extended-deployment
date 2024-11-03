package deployregion

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

type syncContext struct {
	ctx context.Context
	req ctrl.Request

	controller *DeployRegionReconciler
	Cache      *allResourceCache // 全局缓存

	namespace string
	name      string
}

type stopError struct {
	errmsg string
}

func (e *stopError) Error() string {
	return e.errmsg
}

func stopReconcile(err error) {
	if err != nil {
		panic(&stopError{
			errmsg: err.Error(),
		})
	}
	panic(errors.New(("stopReconcile")))
}

func (d *syncContext) handlePodEvent() {
	// 1. 找到所在node
	// 2. 找到所在region
	pod, err := d.controller.QueryPod(d.ctx, d.namespace, d.name)
	if err != nil || pod == nil {
		stopReconcile(err)
	}

	nodeName := pod.Spec.NodeName
	if len(nodeName) == 0 {
		return
	}

	regionName, ok := d.Cache.NodeToRegion[nodeName]
	if !ok {
		return
	}

	region := d.Cache.Regions[regionName]
	nodeInfo := region.Nodes[nodeName]
	nodeInfo.OnPodUpdate(pod)
}

func (d *syncContext) handleNodeEvent() {
	// 1. 找到node所在region
	// 2. 如果映射中找不到，就遍历region
	// 3. 检查node是否在region中存在
	node, err := d.controller.QueryNode(d.ctx, d.name)
	if err != nil || node == nil {
		stopReconcile(err)
	}

	if regionName, ok := d.Cache.NodeToRegion[node.Name]; ok {
		ready := d.Cache.Regions[regionName].Nodes[node.Name].IsReady
		d.Cache.Regions[regionName].OnNodeUpdate(node)
		if ready != d.Cache.Regions[regionName].Nodes[node.Name].IsReady { // 节点状态变化，需要检查分区状态是否变化
			d.CheckRegionStatus(regionName)
		}
		return
	}

	regionName := ""
	for _, region := range d.Cache.Regions {
		if d.controller.IsNodeBelongRegion(node, region.Spec) {
			regionName = region.Name
			break
		}
	}

	if regionName == "" {
		return
	}

	nodeInfo := d.Cache.Regions[regionName].AddNode(node)
	d.Cache.NodeToRegion[node.Name] = regionName

	pods, err := d.controller.QueryAllPod(d.ctx)
	if err != nil {
		stopReconcile(err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.NodeName == node.Name {
			nodeInfo.AddPod(pod)
		}
	}

	// 新增节点，需要检查分区状态变化
	d.CheckRegionStatus(regionName)
}

func (d *syncContext) handleRegionCreate(region *v1beta1.DeployRegion) {
	regionCache := d.Cache.AddRegion(region)

	// 1. 根据标签，查询所有 node
	// 2. 查询所有 pod ，遍历，如果在这些 node 上，就记录
	nodes, err := d.controller.QueryNodesInRegion(d.ctx, region)
	if err != nil {
		stopReconcile(err)
	}
	for i := range nodes.Items {
		regionCache.AddNode(&nodes.Items[i])
		d.Cache.NodeToRegion[nodes.Items[i].Name] = regionCache.Name
	}

	pods, err := d.controller.QueryAllPod(d.ctx)
	if err != nil {
		stopReconcile(err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		nodeInfo, ok := regionCache.Nodes[pod.Spec.NodeName]
		if ok {
			nodeInfo.AddPod(pod)
		}
	}

	d.Cache.Regions[region.Name] = regionCache

	// 检查一次分区故障
	d.CheckRegionStatus(region.Name)
}

func (d *syncContext) handleRegionEvent() {
	region, err := d.controller.QueryRegion(d.ctx, d.name)
	if err != nil || region == nil {
		stopReconcile(err)
	}

	if region.DeletionTimestamp != nil {
		d.Cache.DelRegion(region)
		return
	}

	if _, ok := d.Cache.Regions[d.name]; ok {
		d.CheckRegionStatus(region.Name)
		return
	}

	d.handleRegionCreate(region)
}

func (d *syncContext) waitCacheSynced() {
	d.Cache.Regions = map[string]*regionResourceCache{}
	d.Cache.NodeToRegion = map[string]string{}

	regions, err := d.controller.QueryAllRegions(d.ctx)
	if err != nil {
		stopReconcile(err)
	}

	for i := range regions.Items {
		region := &regions.Items[i]
		regionInfo := d.Cache.AddRegion(region)

		nodes, err := d.controller.QueryNodesInRegion(d.ctx, region)
		if err != nil {
			stopReconcile(err)
		}

		for j := range nodes.Items {
			node := &nodes.Items[j]
			if name, ok := d.Cache.NodeToRegion[node.Name]; ok {
				err = fmt.Errorf("node %v belong to multi region, [%v, %v]",
					node.Name, name, region.Name)
				stopReconcile(err)
			}

			regionInfo.AddNode(node)
			d.Cache.NodeToRegion[node.Name] = region.Name
		}
	}

	pods, err := d.controller.QueryAllPod(d.ctx)
	if err != nil {
		stopReconcile(err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if name, ok := d.Cache.NodeToRegion[pod.Spec.NodeName]; ok {
			regionInfo := d.Cache.Regions[name]
			regionInfo.Nodes[pod.Spec.NodeName].AddPod(pod)
		} else {
			klog.V(4).Infof("pod %v/%v is not belong to any region", pod.Namespace, pod.Name)
		}
	}

	d.Cache.Synced = true

	for name := range d.Cache.Regions {
		d.CheckRegionStatus(name)
	}
}

func (d *syncContext) Sync() (err error) {
	defer func() {
		if e := recover(); e != nil {
			switch t := e.(type) {
			case *stopError: // 程序主动中断
				err = t
			default: // 异常抛出的错误，需要记录错误
				err = fmt.Errorf("recovered error: %v\nstack:\n%v",
					e, string(debug.Stack()))
				klog.Error(err.Error())
			}
		}
	}()

	if !d.Cache.Synced {
		d.waitCacheSynced()
	}

	d.name = d.req.Name
	d.namespace = d.req.Namespace
	if strings.HasPrefix(d.namespace, PrefixPodType) { // pod 事件
		d.namespace = d.namespace[len(PrefixPodType):]
		d.handlePodEvent()
	} else if strings.HasPrefix(d.namespace, PrefixNodeType) { // node 事件
		d.namespace = ""
		d.handleNodeEvent()
	} else { // DeployRegion
		d.handleRegionEvent()
	}
	return nil
}
