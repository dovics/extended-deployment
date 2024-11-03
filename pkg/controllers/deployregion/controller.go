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

package deployregion

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/autoscaling"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

const (
	PrefixNodeType = "node+"
	PrefixPodType  = "pod+"
)

// DeployRegionReconciler reconciles a DeployRegion object
type DeployRegionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	nodeLabels map[string]string // 筛选node时的附加 label
}

func (r *DeployRegionReconciler) init() {
	// TODO: read it from configure file
	r.nodeLabels = map[string]string{
		"node-role.kubernetes.io/compute": "true",
	}
}

//+kubebuilder:rbac:groups=extendeddeployment.io,resources=deployregions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extendeddeployment.io,resources=deployregions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=extendeddeployment.io,resources=deployregions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DeployRegion object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *DeployRegionReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()

	ctx := &syncContext{
		ctx:        c,
		req:        req,
		controller: r,
		Cache:      gCache,
	}
	err := ctx.Sync()
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DeployRegionReconciler) Setup(stopChan <-chan struct{}, f informers.SharedInformerFactory) error {
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeployRegionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.init()

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.DeployRegion{}).
		Watches(&corev1.Pod{}, &podEventHandler{}).
		Watches(&corev1.Node{}, &nodeEventHandler{}).
		Complete(r)
}

func (r *DeployRegionReconciler) QueryAllRegions(ctx context.Context) (regions *v1beta1.DeployRegionList, err error) {
	regions = &v1beta1.DeployRegionList{}
	err = r.List(ctx, regions)
	if err != nil {
		klog.Errorf("query all region error: %v", err)
		return nil, err
	}
	return regions, nil
}

func (r *DeployRegionReconciler) QueryRegion(ctx context.Context, name string) (region *v1beta1.DeployRegion, err error) {
	key := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	region = &v1beta1.DeployRegion{}
	err = r.Get(ctx, key, region)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("query deploy region %v error: %v", name, err)
		return nil, err
	}
	return
}

func (r *DeployRegionReconciler) QueryNode(ctx context.Context, name string) (node *corev1.Node, err error) {
	key := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	node = &corev1.Node{}
	err = r.Get(ctx, key, node)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("query node %v error: %v", name, err)
		return nil, err
	}
	return node, nil
}

func (r *DeployRegionReconciler) IsNodeBelongRegion(node *corev1.Node, spec *v1beta1.DeployRegionSpec) bool {
	matchLabels := autoscaling.DeepCopyStringMap(spec.MatchLabels)
	for k, v := range r.nodeLabels {
		matchLabels[k] = v
	}
	selector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: matchLabels,
	})
	return selector.Matches(labels.Set(node.Labels))
}

func (r *DeployRegionReconciler) QueryAllNode(ctx context.Context) (nodes *corev1.NodeList, err error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: r.nodeLabels,
	})
	if err != nil {
		return nil, err
	}
	nodes = &corev1.NodeList{}
	err = r.List(ctx, nodes, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		klog.Errorf("query all node error: %v", err)
		return nil, err
	}
	return nodes, nil
}

func (r *DeployRegionReconciler) QueryNodesInRegion(ctx context.Context, region *v1beta1.DeployRegion) (nodes *corev1.NodeList, err error) {
	matchLabels := autoscaling.DeepCopyStringMap(region.Spec.MatchLabels)
	for k, v := range r.nodeLabels {
		matchLabels[k] = v
	}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: matchLabels,
	})
	if err != nil {
		klog.Errorf("region %v create label selector error: %v", region.Name, err)
		return nil, err
	}

	nodes = &corev1.NodeList{}
	err = r.List(ctx, nodes, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		klog.Errorf("query nodes in region %v error: %v", region.Name, err)
		return nil, err
	}

	return nodes, nil
}

func (r *DeployRegionReconciler) QueryAllPod(ctx context.Context) (pods *corev1.PodList, err error) {
	pods = &corev1.PodList{}
	err = r.List(ctx, pods)
	if err != nil {
		klog.Errorf("query all pods error: %v", err)
		return nil, err
	}

	return pods, nil
}

func (r *DeployRegionReconciler) QueryPod(ctx context.Context, ns, name string) (pod *corev1.Pod, err error) {
	key := types.NamespacedName{
		Namespace: ns,
		Name:      name,
	}
	pod = &corev1.Pod{}
	err = r.Get(ctx, key, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("query pod %v/%v error: %v", ns, name, err)
		return nil, err
	}
	return pod, nil
}

func (r *DeployRegionReconciler) QueryInplaceSetsInRegion(ctx context.Context,
	region string) (ips *v1beta1.InplaceSetList, err error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{utils.RegionLabel: region},
	})
	if err != nil {
		return nil, err
	}
	ips = &v1beta1.InplaceSetList{}
	err = r.List(ctx, ips, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		klog.Errorf("query inplacesets in region %v error: %v", region, err)
		return nil, err
	}
	return ips, nil
}
