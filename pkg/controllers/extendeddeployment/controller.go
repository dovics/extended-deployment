package extendeddeployment

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller/history"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment/adapter"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

const (
	confirmTrue    = "true"
	confirmFalse   = "false"
	ControllerName = "cluster-controller"
	BetaStepSize   = 1
)

const (
	termInit = iota
	termReady
	termNotReady
)

// ExtendedDeploymentReconciler reconciles a ExtendedDeployment object
type ExtendedDeploymentReconciler struct {
	client.Client
	KubeClient    clientset.Interface
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder

	controllerHistory history.Interface
	podLister         corelisters.PodLister
	nodeLister        corelisters.NodeLister

	listerSynced   []cache.InformerSynced
	subSetControls map[v1beta1.SubsetType]adapter.ControlInterface

	DisableInplaceUpdate bool
}

// Start starts an asynchronous loop that monitors the status of cluster.
func (dc *ExtendedDeploymentReconciler) Start(ctx context.Context) error {
	if !cache.WaitForNamedCacheSync(ControllerName, ctx.Done(), dc.listerSynced...) {
		return fmt.Errorf("can not wait for resource syncd")
	}

	return nil
}

func (dc *ExtendedDeploymentReconciler) Setup(f informers.SharedInformerFactory) error {
	podInformer := f.Core().V1().Pods()
	nodesInformer := f.Core().V1().Nodes()
	revisionInformer := f.Apps().V1().ControllerRevisions()

	dc.podLister = podInformer.Lister()
	dc.nodeLister = nodesInformer.Lister()

	dc.controllerHistory = history.NewHistory(dc.KubeClient, revisionInformer.Lister())
	dc.listerSynced = append(dc.listerSynced, revisionInformer.Informer().HasSynced)
	dc.listerSynced = append(dc.listerSynced, podInformer.Informer().HasSynced)
	dc.listerSynced = append(dc.listerSynced, nodesInformer.Informer().HasSynced)
	dc.subSetControls = map[v1beta1.SubsetType]adapter.ControlInterface{
		v1beta1.InPlaceSetSubsetType: &SubsetControl{
			controller: dc,

			Client: dc.Client,
			scheme: dc.Scheme,
			rec:    dc.EventRecorder,
			adapter: &adapter.InplaceSetAdapter{
				Client: dc.Client,
				Scheme: dc.Scheme,
			},
		},
		v1beta1.DeploymentSubsetType: &SubsetControl{
			controller: dc,

			Client: dc.Client,
			scheme: dc.Scheme,
			rec:    dc.EventRecorder,
			adapter: &adapter.DeploymentAdapter{
				Client: dc.Client,
				Scheme: dc.Scheme,
			},
		},
		v1beta1.ReplicaSetSubsetType: &SubsetControl{
			controller: dc,

			Client: dc.Client,
			scheme: dc.Scheme,
			rec:    dc.EventRecorder,
			adapter: &adapter.ReplicaSetAdapter{
				Client: dc.Client,
				Scheme: dc.Scheme,
			},
		},
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (dc *ExtendedDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return utilerrors.NewAggregate([]error{
		ctrl.NewControllerManagedBy(mgr).For(&v1beta1.ExtendedDeployment{}).
			Owns(&v1beta1.InplaceSet{}).
			Owns(&appsv1.ControllerRevision{}).
			Owns(&v1beta1.DeployRegion{}).
			Owns(&appsv1.Deployment{}).
			Owns(&appsv1.ReplicaSet{}).
			Complete(dc),
		mgr.Add(dc),
	})
}

// +kubebuilder:rbac:groups=extendeddeployment.io,resources=extendeddeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extendeddeployment.io,resources=extendeddeployments/status,verbs=get;update;patch

// Reconcile reconcile
func (dc *ExtendedDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	key := fmt.Sprintf("%v/%v", req.Namespace, req.Name)
	startTime := time.Now()

	klog.V(4).Infof("===> Started syncing extendeddeployment %q", req.NamespacedName)

	defer func() {
		e := recover()
		err = dc.stopHandler(err, e) // 处理 recover 错误
		endTime := time.Now()
		ms := float64(endTime.Sub(startTime).Microseconds()) / 1000

		klog.V(4).Infof("<=== Finished syncing extendeddeployment %q (%.3f ms)",
			req.NamespacedName, ms)

	}()

	deploy, err := utils.QueryExtendedDeployment(dc.Client, ctx, req.NamespacedName)
	if err != nil || deploy == nil {
		return ctrl.Result{}, err
	}

	if deploy.DeletionTimestamp != nil {
		klog.V(4).Infof("extendeddeployment %v is deleted, return", key)
		return ctrl.Result{}, nil
	}
	//检查回滚
	needRollback, err := dc.checkRollback(ctx, deploy)
	if err != nil {
		return ctrl.Result{}, err
	}
	if needRollback {
		klog.V(4).Infof("%s need roll back", key)
		return ctrl.Result{}, nil
	}
	//同步revision
	needSyncRevison, err := dc.syncRevisions(ctx, deploy)
	if err != nil {
		klog.Errorf("%s sync revision error: %s", key, err.Error())
		return ctrl.Result{}, err
	}
	klog.V(4).Infof("%s start syncRevisions, type %s ,needSyncRevison %s ", key, deploy.Spec.SubsetType, needSyncRevison)
	if needSyncRevison {
		return ctrl.Result{}, nil
	}
	//校验selector
	if err = dc.checkSelector(deploy); err != nil {
		klog.Errorf("%s check selector error: %s", key, err.Error())
		return ctrl.Result{}, nil
	}
	//校验region
	regionMap, err := dc.checkRegions(ctx, deploy, key)
	if err != nil {
		return ctrl.Result{}, err
	}

	if deploy.Spec.SubsetType == "" {
		deploy.Spec.SubsetType = v1beta1.InPlaceSetSubsetType
	}

	klog.V(4).Infof("%s start getSubsetControl, type %s", key, deploy.Spec.SubsetType)

	//获取subset控制器
	subsetControl, err := dc.getSubsetControl(deploy)
	if err != nil {
		klog.Errorf("%s getSubsetControl error: %s", key, err.Error())
		return ctrl.Result{}, err
	}
	subsetControl.SetDeployKey(key)

	klog.V(4).Infof("%s start QuerySubsetAndRegionInfo ", key)

	//查询subset及region数据
	if err = subsetControl.QuerySubsetAndRegionInfo(deploy, regionMap); err != nil {
		klog.V(4).Infof("%s QuerySubsetAndRegionInfo error: %s ", key, err.Error())
		return ctrl.Result{}, err
	}

	regionInfos := subsetControl.GetRegionInfo()

	klog.V(4).Infof("%s start UpdateSubsetStrategy", key)
	if err := subsetControl.UpdateSubsetStrategy(deploy); err != nil {
		klog.V(4).Infof("%s UpdateSubsetStrategy error: %s ", key, err.Error())
		return ctrl.Result{}, err
	}

	klog.V(4).Infof("%s start checkSaturated ", key)
	//检查是否达到最终状态
	if dc.checkSaturated(regionInfos) {
		klog.V(4).Infof("%s start cleanAfterSaturated ", key)
		if err = dc.cleanAfterSaturated(ctx, deploy, key, regionInfos, subsetControl); err != nil {
			klog.V(4).Infof("%s cleanAfterSaturated error: %s ", key, err.Error())
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	klog.V(4).Infof("%s start check term ", key)

	//检查term
	termFlag := dc.checkIfTermReady(regionInfos)
	klog.V(4).Infof("termInit=0,termReady=1,termNotReady=2 , --> term termFlag = %v , Conditions=%+v , %s",
		termFlag,
		deploy.Status.Conditions,
		key)
	if termFlag == termReady {
		isProgressing := true
		for _, condition := range deploy.Status.Conditions {
			if condition.Type == appsv1.DeploymentProgressing &&
				condition.Status == corev1.ConditionFalse {
				isProgressing = false
			}
		}
		klog.V(4).Infof("%s , isProgressing = %v", key, isProgressing)
		if isProgressing {
			needWait, err := dc.checkUpgradeConfirm(ctx, deploy)
			if err != nil {
				klog.Errorf("%s check confirm annotation error: %s", key, err.Error())
				return ctrl.Result{RequeueAfter: 1 * time.Second}, err
			}
			if needWait {
				klog.V(4).Infof("%s wait to confirm annotation.", key)
				// 刷新时间，避免等待确认超时
				return ctrl.Result{}, dc.SyncStatusOnly(ctx, deploy, regionInfos, true, false)
			} else {
				klog.V(4).Infof("%s term next exec when needWait=false", key)
			}
		} else {
			klog.V(4).Infof("%s term next exec when isProgressing=false", key)
		}

	} else if termFlag == termNotReady {
		klog.V(4).Infof("%s term not ready, wait", key)
		return ctrl.Result{}, dc.SyncStatusOnly(ctx, deploy, regionInfos, false, false)
	}

	klog.V(4).Infof("%s start ManageSubsets ", key)

	//调度workload
	updated, err := subsetControl.ManageSubsets(deploy)
	if err != nil {
		klog.Errorf("%s ManageSubsets error: %s", key, err.Error())
		return ctrl.Result{RequeueAfter: 1 * time.Second}, err
	}

	klog.V(4).Infof("%s start sync status with refreshing time, updated:%s,", key, updated)

	err = dc.SyncStatusOnly(ctx, deploy, regionInfos, true, false)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !updated {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	} else {
		err = dc.UpdateProgressing(ctx, deploy)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (dc *ExtendedDeploymentReconciler) stopHandler(err error, e interface{}) (newErr error) {
	if e == nil {
		return err
	}

	newErr = fmt.Errorf("recovered error: %v\nstack:\n%v", e, string(debug.Stack()))
	klog.Error(newErr.Error())

	return
}

// checkUpgradeConfirm 等待升级确认
func (dc *ExtendedDeploymentReconciler) checkUpgradeConfirm(ctx context.Context, deploy *v1beta1.ExtendedDeployment) (bool, error) {
	// 需要确认
	// 如果注解不存在，无需确认，是第一次同步
	// 如果注解存在，必须为 true
	if deploy.Spec.Strategy.NeedWaitingForConfirm {
		confirm, exists := deploy.Annotations[utils.AnnotationUpgradeConfirm]
		if !exists {
			if deploy.Annotations == nil {
				deploy.Annotations = make(map[string]string)
			}
			deploy.Annotations[utils.AnnotationUpgradeConfirm] = confirmFalse
			return true, dc.updateAnnotations(ctx, deploy)
		}

		if confirm != confirmTrue {
			return true, nil
		}

		//开启了第一次等待确认， 后面就不需要确认了.
		if deploy.Spec.Strategy.NeedFirstConfirm {
			return false, nil
		}

		dc.emitNormalEvent(deploy, eventReasonTermConfirmed, "Term confirmed.")
		utils.DelAnnotations(deploy.Annotations, []string{utils.AnnotationUpgradeConfirm})
		return false, dc.updateAnnotations(ctx, deploy)
	}

	_, exists := deploy.Annotations[utils.AnnotationUpgradeConfirm]
	if exists {
		utils.DelAnnotations(deploy.Annotations, []string{utils.AnnotationUpgradeConfirm})
		return false, dc.updateAnnotations(ctx, deploy)
	}
	return false, nil
}

// checkSelector 检查template中的selector
func (dc *ExtendedDeploymentReconciler) checkSelector(deploy *v1beta1.ExtendedDeployment) error {
	// 检查selector是否一致
	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		err = fmt.Errorf("label selector is invalid: %v", err)
		dc.emitWarningEvent(deploy, eventReasonExtendedDeploymentConfigError, err.Error())
		return err
	}
	if selector.String() == labels.Everything().String() ||
		selector.String() == labels.Nothing().String() {
		err = fmt.Errorf("label selector is invalid, shloud not be everything or nothing")
		dc.emitWarningEvent(deploy, eventReasonExtendedDeploymentConfigError, err.Error())
		return err
	}
	if !selector.Matches(labels.Set(deploy.Spec.Template.Labels)) {
		err = fmt.Errorf("label selector not match template labels")
		dc.emitWarningEvent(deploy, eventReasonExtendedDeploymentConfigError, err.Error())
		return err
	}
	return nil
}

// getSubsetControl 获取subset ControlInterface
func (dc *ExtendedDeploymentReconciler) getSubsetControl(deploy *v1beta1.ExtendedDeployment) (adapter.ControlInterface, error) {
	ci := dc.subSetControls[v1beta1.InPlaceSetSubsetType]
	if ciNew, ok := dc.subSetControls[deploy.Spec.SubsetType]; ok {
		ci = ciNew
	} else if deploy.Spec.SubsetType == "" {
		return ci, nil
	} else {
		err := fmt.Errorf("resource type not supported")
		dc.emitWarningEvent(deploy, eventReasonExtendedDeploymentResourceTypeError, err.Error())
		return nil, err
	}
	return ci, nil
}

// checkIfTermReady 检查本轮次同步是否完成
func (dc *ExtendedDeploymentReconciler) checkIfTermReady(regionInfoMap map[string]*adapter.RegionInfo) int {

	for _, ri := range regionInfoMap {
		if ri.New == nil {
			return termInit
		}
		if !ri.AvailableDesired {
			return termNotReady
		}
	}

	return termReady
}

// checkSaturated 检查是否达到最终状态
func (dc *ExtendedDeploymentReconciler) checkSaturated(regionInfoMap map[string]*adapter.RegionInfo) bool {
	for _, ri := range regionInfoMap {
		if !ri.ReplicasDesired || !ri.AvailableDesired {
			// 未饱和，退出继续后续流程
			return false
		}
	}
	return true
}
