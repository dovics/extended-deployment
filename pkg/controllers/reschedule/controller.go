package reschedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/informers"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/api/v1/pod"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"extendeddeployment.io/extended-deployment/api/v1beta1"
	"extendeddeployment.io/extended-deployment/pkg/controllers/deployregion"
	"extendeddeployment.io/extended-deployment/pkg/controllers/extendeddeployment"
	"extendeddeployment.io/extended-deployment/pkg/controllers/extendeddeployment/adapter"
	"extendeddeployment.io/extended-deployment/pkg/utils"
)

// RescheduleReconciler reconciles a DeployRegion object
type RescheduleReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder

	podLister corelisters.PodLister
	podSynced cache.InformerSynced
}

// +kubebuilder:rbac:groups=extendeddeployment.io,resources=extendeddeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extendeddeployment.io,resources=extendeddeployments/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DeployRegion object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *RescheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	result := ctrl.Result{}
	err := r.reconcile(req)
	if err != nil {
		retryErr, ok := err.(*RetryError)
		if ok {
			result.RequeueAfter = time.Duration(retryErr.After) * time.Second
		}
	}
	return result, err
}

func (r *RescheduleReconciler) reconcile(req ctrl.Request) error {
	ctx := context.TODO()

	deploymentObject, err := utils.QueryExtendedDeployment(r.Client, ctx, req.NamespacedName)
	if err != nil {
		return err
	}

	if deploymentObject == nil {
		return nil
	}

	if !deploymentObject.Spec.Strategy.AutoReschedule.Enable {
		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v auto reschedule feature isn't enable", req.NamespacedName)
		if utils.DelAutoScheduleSpec(deploymentObject) {
			if err := r.Update(ctx, deploymentObject); err != nil {
				return err
			}
		}

		DelRescheduleCondition(deploymentObject)
		return r.Status().Update(ctx, deploymentObject)
	}

	if !deployregion.IsExtendedDeploymentProgressFinished(deploymentObject) {
		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v is Processing, can't do reschedule", req.NamespacedName)
		return nil
	}

	lastRescheduleType, shouldContinue, err := CheckAnnotation(deploymentObject)
	if err != nil {
		return err
	}

	if !shouldContinue {
		klog.V(4).Infof("[Reschedule] ExtendedDeployment %v: auto reschedule exited for annotation", req.NamespacedName)
		if err := r.Update(ctx, deploymentObject); err != nil {
			return err
		}

		DelRescheduleCondition(deploymentObject)
		return r.Status().Update(ctx, deploymentObject)
	}

	rescheduleType := RescheduleNever
	regionsMap, shouldReschedule := GetFailedRegions(deploymentObject)
	klog.V(4).Infof("[Reschedule] ExtendedDeployment %v regions: %v", req.NamespacedName, regionsMap)
	if shouldReschedule {
		rescheduleType = RescheduleForFailedRegion
	}

	// 资源不足处于pending的pod
	insufficientPods := map[string][]*corev1.Pod{}
	subsetsMap, err := QuerySubsetAndRegionInfo(ctx, r.Client, deploymentObject, r.Scheme)
	if err != nil {
		return err
	}

	// 检查是否有 Pod 因资源不足而处于 Pending
	for regionName, subsets := range subsetsMap {
		if regionsMap[regionName] {
			continue
		}

		for _, subset := range subsets {
			pods, sec, err := QueryInsufficientPodsBySubset(subset, r.podLister)
			if err != nil {
				klog.Errorf("[Reschedule] ExtendedDeployment %v failed to query insufficient pods, error: %v", req.NamespacedName, err)
				return err
			}

			//klog.V(4).Infof("[Reschedule] ExtendedDeployment %v: \n subset: %v\ninsufficientPods %v\nsec: %v", req.NamespacedName,subset, pods, sec)
			if len(pods) > 0 {
				insufficientPods[regionName] = append(insufficientPods[regionName], pods...)
				if sec > deploymentObject.Spec.Strategy.AutoReschedule.TimeoutSeconds {
					rescheduleType = RescheduleForResourceInsufficient
					continue
				}

				// FIXME: 某一个pod调度等待时间未超时，其他pod是否就不能调度，是否应该暂时不调度这个pod，已经达到时间的pod就调度
				klog.V(4).Infof("[Reschedule] ExtendedDeployment %v wait pending pod in %v", req.NamespacedName, regionName)
				return ErrPendingNotTimeout
			}
		}
	}

	if rescheduleType == RescheduleNever {
		if lastRescheduleType == RescheduleForResourceInsufficient {
			klog.V(4).Infof("[Reschedule] ExtendedDeployment %v has reschedule for resource insufficient, exit", req.NamespacedName)
			return nil
		}

		if lastRescheduleType == RescheduleNever {
			klog.V(4).Infof("[Reschedule] ExtendedDeployment %v don't need for reschedule", req.NamespacedName)
			return nil
		}
	}

	klog.V(4).Infof("[Reschedule] ExtendedDeployment %v: start failure schedule", req.NamespacedName)
	scheduleInfo, err := NewScheduleRegionInfo(deploymentObject)
	if err != nil {
		klog.Errorf("[Reschedule] ExtendedDeployment %v failed to create schedule info: %v", req.NamespacedName, err)
		return err
	}

	if err := scheduleInfo.RescheduleFailedRegions(regionsMap); err != nil {
		r.EventRecorder.Eventf(deploymentObject, corev1.EventTypeWarning,
			"RescheduleFailed", `reschedule error: %v`, err)
		klog.Errorf("[Reschedule] ExtendedDeployment %v reschedule for failed region error: %v", req.NamespacedName, err)
		return err
	}

	if err := scheduleInfo.RescheduleInsufficientPods(insufficientPods); err != nil {
		r.EventRecorder.Eventf(deploymentObject, corev1.EventTypeWarning,
			"RescheduleFailed", `reschedule error: %v`, err)
		klog.Errorf("[Reschedule] ExtendedDeployment %v reschedule for insufficient pods failed: %v", req.NamespacedName, err)
		return err
	}

	if err := scheduleInfo.SetRescheduleAnnotation(deploymentObject); err != nil {
		klog.Errorf("[Reschedule] ExtendedDeployment %v failed to set schedule annotation: %v", req.NamespacedName, err)
		return err
	}

	if err := r.Update(ctx, deploymentObject); err != nil {
		klog.Errorf("[Reschedule] ExtendedDeployment %v update failed, error: %v", req.NamespacedName, err)
		return err
	}

	SetRescheduleCondition(deploymentObject, rescheduleType)
	return r.Status().Update(ctx, deploymentObject)
}

// Start starts an asynchronous loop that monitors the status of cluster.
func (r *RescheduleReconciler) Start(ctx context.Context) error {
	if !cache.WaitForNamedCacheSync(extendeddeployment.ControllerName, ctx.Done(), r.podSynced) {
		return fmt.Errorf("can not wait for resource syncd")
	}
	return nil
}

func (r *RescheduleReconciler) Setup(stopChan <-chan struct{}, f informers.SharedInformerFactory) error {
	podInformer := f.Core().V1().Pods()

	r.podLister = podInformer.Lister()

	r.podSynced = podInformer.Informer().HasSynced

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RescheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return utilerrors.NewAggregate([]error{
		ctrl.NewControllerManagedBy(mgr).
			For(&v1beta1.ExtendedDeployment{}).
			// For(&v1beta1.DeployRegion{}).
			// Watches(&source.Kind{Type: &v1beta1.ExtendedDeployment{}}, &extendeddeploymentEventHandler{}).
			Complete(r),
		mgr.Add(r),
	})
}

func QueryInsufficientPodsBySubset(subset *adapter.Subset, podLister corelisters.PodLister) (ret []*corev1.Pod, pendingSec int, err error) {
	selector, err := metav1.LabelSelectorAsSelector(subset.Spec.Selector)
	if err != nil {
		return nil, 0, err
	}

	klog.V(4).Infof("[Reschedule] SubSet %v/%v query pods %v", subset.Namespace, subset.Name, selector)

	pods, err := podLister.Pods(subset.Namespace).List(selector)
	if err != nil {
		klog.Errorf("[Reschedule] ExtendedDeployment %v/%v query pods error: %v",
			subset.Namespace, subset.Name, err)
		return nil, 0, err
	}

	for _, p := range pods {
		if p.DeletionTimestamp != nil ||
			(len(p.OwnerReferences) == 0 || p.OwnerReferences[0].UID != subset.UID) {
			continue
		}
		if _, c := pod.GetPodCondition(&p.Status, corev1.PodScheduled); c != nil {
			if strings.Contains(c.Message, errorMsgCpuDetect) ||
				strings.Contains(c.Message, errorMsgMemDetect) {
				ret = append(ret, p)
				klog.V(4).Infof("[Reschedule] Pod %v/%v pending for %v", p.Namespace, p.Name, c.Message)
				sec := time.Now().Sub(c.LastTransitionTime.Time).Seconds()
				if int(sec) > pendingSec {
					pendingSec = int(sec)
				}
			}
		}
	}

	return ret, pendingSec, nil
}
