package extendeddeployment

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/apps"
	"k8s.io/utils/integer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment/adapter"
	"github.com/dovics/extendeddeployment/pkg/utils"
	"github.com/dovics/extendeddeployment/pkg/utils/hash"
	"github.com/dovics/extendeddeployment/pkg/utils/overridemanager"
	"github.com/dovics/extendeddeployment/pkg/utils/refmanager"
)

const updateRetries = 5

type SubsetControl struct {
	ctx       context.Context
	startTime time.Time
	endTime   time.Time

	controller *ExtendedDeploymentReconciler

	key string // namespace/name

	regionInfoMap map[string]*adapter.RegionInfo // 按region将Subset分组

	autoScheduleSpec *utils.AutoScheduleSpec // 自动调度信息，如果没有调度，为null

	allSubsets    []*adapter.Subset // 所有 Subset
	unusedSubsets []*adapter.Subset // 分区与当前配置不匹配的 Subset

	client.Client
	scheme  *runtime.Scheme
	adapter adapter.Adapter
	rec     record.EventRecorder
}

func (m *SubsetControl) SetDeployKey(key string) {
	m.key = key
}

func (m *SubsetControl) QuerySubsetAndRegionInfo(cd *v1beta1.ExtendedDeployment, regionMap map[string]*v1beta1.DeployRegion) error {
	allSubsets, err := m.GetAllSubsets(cd)
	if err != nil {
		klog.Errorf("GetAllSubsets error: %s", err.Error())
		return err
	}
	m.allSubsets = allSubsets
	return m.groupSubset(cd, regionMap)
}

// GetAllSubsets returns all subsets owned by the UnitedDeployment.
func (m *SubsetControl) GetAllSubsets(cd *v1beta1.ExtendedDeployment) (subSets []*adapter.Subset, err error) {
	selector, err := metav1.LabelSelectorAsSelector(cd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	setList := m.adapter.NewResourceListObject()
	err = m.Client.List(context.TODO(), setList, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     cd.Namespace,
	})
	if err != nil {
		return nil, err
	}

	manager, err := refmanager.New(m.Client, cd.Spec.Selector, cd, m.scheme)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(setList).Elem().FieldByName("Items")
	selected := make([]metav1.Object, v.Len())
	for i := 0; i < v.Len(); i++ {
		selected[i] = v.Index(i).Addr().Interface().(metav1.Object)
	}
	claimedSets, err := manager.ClaimOwnedObjects(selected)
	if err != nil {
		return nil, err
	}

	for _, claimedSet := range claimedSets {
		subSet, err := m.convertToSubset(claimedSet)
		if err != nil {
			return nil, err
		}
		// 剔除已删除状态、Owner不对的subset
		if subSet.DeletionTimestamp != nil ||
			(len(subSet.OwnerReferences) == 0 || subSet.OwnerReferences[0].UID != cd.UID) {
			continue
		}
		subSets = append(subSets, subSet)
	}

	return subSets, nil
}

func (m *SubsetControl) NewSubset(cd *v1beta1.ExtendedDeployment, ri *adapter.RegionInfo, replicas int32) (*adapter.Subset, error) {
	maxRevision64, err := maxRevisionForSubsets(ri.Olds)
	if err != nil {
		klog.Errorf("%s failed to get max revision, region: %s, error: %v", m.key, ri.Region.Name, err)
		return nil, err
	}
	revision := maxRevision64 + 1
	if ri.New != nil {
		if ri.New.Annotations == nil {
			ri.New.Annotations = make(map[string]string)
		}
		existRevision := int64(0)
		val, exist := ri.New.Annotations[utils.AnnotationRevision]
		if exist {
			existRevision, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				klog.Errorf("%s failed to parse exist revision, region: %s, error: %v", m.key, ri.Region.Name, err)
				return nil, err
			}
		}
		if !exist || existRevision < revision {
			ri.New.Annotations[utils.AnnotationRevision] = strconv.Itoa(int(revision))
			err := m.UpdateSubsetAnnotations(ri.New, false)
			if err != nil {
				klog.Errorf("%s failed to update annotations, region: %s, error: %v", m.key, ri.Region.Name, err)
				return nil, err
			}
		}
		return ri.New, nil
	}

	klog.V(4).Infof("%s start CreateSubset, region: %s", m.key, ri.Region.Name)
	subset, err := m.CreateSubset(cd, ri.Region, replicas, strconv.Itoa(int(revision)))
	if err != nil {
		klog.Errorf("%s CreateSubset error: %s, region: %s", m.key, err.Error(), ri.Region.Name)
		return nil, err
	}
	klog.V(4).Infof("%s finish CreateSubset, region: %s new: %s", m.key, ri.Region.Name, ri.New.Name)
	return subset, nil
}

// CreateSubset creates the Subset depending on the inputs.
func (m *SubsetControl) CreateSubset(cd *v1beta1.ExtendedDeployment, dr *v1beta1.DeployRegion, replicas int32, revision string) (*adapter.Subset, error) {
	set := m.adapter.NewResourceObject()

	if cd.Status.CollisionCount == nil {
		cd.Status.CollisionCount = new(int32)
	}

	overrideTemplate := overridemanager.ApplyOverridePolicies(dr.Name, cd) // 使用override后的template

	b, _ := json.Marshal(overrideTemplate)
	d := md5.Sum(b)
	templateHash := fmt.Sprintf("%x", d)

	if err := m.adapter.ApplySubsetTemplate(cd, dr, overrideTemplate, replicas, set); err != nil {
		return nil, err
	}

	// 额外添加注解
	// 添加hash，用于匹配extendeddeployment和subset
	// 添加分区名称
	anno := set.GetAnnotations()
	if anno == nil {
		anno = make(map[string]string)
	}
	anno[utils.IpsAnnotationTemplateHash] = templateHash
	anno[utils.IpsAnnotationRegionName] = dr.Name
	anno[utils.AnnotationRevision] = revision
	set.SetAnnotations(anno)
	set.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(cd, v1beta1.ExtendedDeploymentGVK)})

	err := m.Create(context.TODO(), set)
	if err != nil {
		return nil, err
	}
	m.rec.Eventf(cd, corev1.EventTypeNormal, eventReasonInplacesetCreateSuccess,
		fmt.Sprintf("Create %v %v/%v success, replicas %v", cd.Spec.SubsetType, set.GetName(), set.GetNamespace(), replicas))
	return m.convertToSubset(set)
}

// UpdateSubsetAnnotations is used to update the subset. The target Subset workload can be found with the input subset.
func (m *SubsetControl) UpdateSubsetAnnotations(subset *adapter.Subset, isInplaceUpdate bool) error {
	set := m.adapter.NewResourceObject()
	var updateError error
	for i := 0; i < updateRetries; i++ {
		getError := m.Client.Get(context.TODO(), m.objectKey(&subset.ObjectMeta), set)
		if getError != nil {
			return getError
		}
		set.SetAnnotations(subset.Annotations)
		if isInplaceUpdate {
			m.setInplaceUpdate(set, subset)
		}
		updateError = m.Client.Update(context.TODO(), set)
		if updateError == nil {
			break
		}
	}

	if updateError != nil {
		return updateError
	}

	return nil
}

func (m *SubsetControl) setInplaceUpdate(obj runtime.Object, ss *adapter.Subset) {
	set := obj.(*v1beta1.InplaceSet)
	set.Spec.Template.Spec.Containers = ss.Spec.Template.Spec.Containers
}

// DeleteSubset is called to delete the subset. The target Subset workload can be found with the input subset.
func (m *SubsetControl) DeleteSubset(cd *v1beta1.ExtendedDeployment, subset *adapter.Subset) error {
	set := subset.Spec.SubsetRef.Resources[0].(client.Object)
	err := m.Delete(context.TODO(), set, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		return err
	}
	m.rec.Eventf(cd, corev1.EventTypeNormal, eventReasonInplacesetDeleteSuccess,
		fmt.Sprintf("Delete %v %v/%v success", cd.Spec.SubsetType, set.GetNamespace(), set.GetName()))
	return nil
}

// GetRegionInfo 获取region及其对应的subset信息
func (m *SubsetControl) GetRegionInfo() map[string]*adapter.RegionInfo {
	return m.regionInfoMap
}

// ManageSubsets 调度workload
func (m *SubsetControl) ManageSubsets(cd *v1beta1.ExtendedDeployment) (anyRegionUpdated bool, err error) {
	if err = m.ClearUnused(cd); err != nil {
		return false, err
	}

	for regionName, ri := range m.regionInfoMap {
		updated := false
		switch cd.Spec.Strategy.UpdateStrategy.Type {
		case v1beta1.InPlaceIfPossibleUpdateStrategyType, v1beta1.InPlaceOnlyUpdateStrategyType:
			canInPlaceUpdate := true
			if canInPlaceUpdate && cd.Spec.SubsetType != v1beta1.InPlaceSetSubsetType {
				klog.Warningf("%s inplace update strategy set but no use inplaceset subset type", m.key)
				m.rec.Eventf(cd, corev1.EventTypeWarning, eventReasonInplacesetScaleSuccess,
					"Inplace update strategy set but no use inplaceset subset type")
				canInPlaceUpdate = false
			}

			if canInPlaceUpdate && m.controller.DisableInplaceUpdate {
				klog.Infof("the controller has turned off the inplace update feature, %s inplace update check skip", m.key)
				canInPlaceUpdate = false
			}

			if canInPlaceUpdate {
				klog.V(4).Infof("%s start CheckInplaceUpdate, region: %s", m.key, regionName)
				doInplaceUpdate, err := m.CheckInplaceUpdate(cd, ri, regionName)
				if err != nil {
					klog.Errorf("%s CheckInplaceUpdate error: %s, region: %s", m.key, err.Error(), regionName)
					return updated, err
				}

				if doInplaceUpdate {
					updated = true
					klog.V(4).Infof("%s in place update, region: %s", m.key, regionName)
					continue
				}

				if cd.Spec.Strategy.UpdateStrategy.Type == v1beta1.InPlaceOnlyUpdateStrategyType {
					klog.Warningf("%s inplace update strategy set InPlaceOnly, but can't inplace update", m.key)
					return updated, fmt.Errorf("inplace update strategy set InPlaceOnly, but can't inplace update")
				}
			}

			fallthrough
		case v1beta1.RecreateUpdateStrategyType:
			stepSize := calculateStepSize(cd, ri.DesiredReplicas, 0)
			ri.New, err = m.NewSubset(cd, ri, stepSize)
			if err != nil {
				return false, err
			}

			klog.V(4).Infof("%s start regionSyncOnce, region: %s", m.key, regionName)
			u, err := m.regionSyncOnce(cd, regionName, !updated)
			if err != nil {
				klog.Errorf("%s regionSyncOnce error: %s, region: %s", m.key, err.Error(), regionName)
				return updated, err
			}
			if u {
				updated = true
				klog.V(4).Infof("%s finish regionSyncOnce, region: %s", m.key, regionName)
			}
		case v1beta1.RollingUpdateStrategyType:
			if cd.Spec.Strategy.UpdateStrategy.RollingUpdate != nil {
				if ri.New == nil {
					newRS, err := m.convertToSubset(m.adapter.NewResourceObject())
					if err != nil {
						klog.Errorf("%s new RS error: %v", m.key, err)
						return false, err
					}
					if newRS.Spec.Replicas == nil {
						newRS.Spec.Replicas = new(int32)
					}
					newReplicasCount, err := NewRSNewReplicas(cd, ri.Olds, newRS, ri)
					if err != nil {
						klog.Errorf("%s  new replicas count error: %v", m.key, err)
						return false, err
					}
					ri.New, err = m.NewSubset(cd, ri, newReplicasCount)
					if err != nil {
						klog.Errorf("%s new subset error: %v", m.key, err)
						return updated, err
					}
					updated = true
				}
				allRSs := append(ri.Olds, ri.New)
				_, err = m.reconcileNewSubset(cd, allRSs, ri.New, ri)
				if err != nil {
					klog.Errorf("scale up %s error: %v", m.key, err)
					return false, err
				}
				_, err = m.reconcileOldSubsets(cd, allRSs, ri.Olds, ri.New, ri, regionName)
				if err != nil {
					klog.Errorf("scale down %s error: %v", m.key, err)
					return false, err
				}
				updated = true
			}
		case v1beta1.DeleteAllFirstUpdateStrategyType:
			return m.rolloutDeleteAllFirst(cd, regionName)
		}

		if updated {
			anyRegionUpdated = true
		}
	}

	return
}

// ScaleSubset scales the replicas of subset
func (m *SubsetControl) ScaleSubset(cd *v1beta1.ExtendedDeployment, ss *adapter.Subset, newReplicas int32) error {
	if *(ss.Spec.Replicas) == newReplicas {
		return nil
	}
	set := m.adapter.NewResourceObject()
	var updateError error
	for i := 0; i < updateRetries; i++ {
		getError := m.Client.Get(context.TODO(), m.objectKey(&ss.ObjectMeta), set)
		if getError != nil {
			return getError
		}
		m.adapter.SetReplicas(set, newReplicas)
		updateError = m.Client.Update(context.TODO(), set)
		if updateError == nil {
			break
		}
	}

	if updateError != nil {
		return updateError
	}
	oldReplicas := *ss.Spec.Replicas
	m.rec.Eventf(cd, corev1.EventTypeNormal, eventReasonInplacesetScaleSuccess,
		fmt.Sprintf("Scale %v %v/%v from %v to %v success",
			cd.Spec.SubsetType, ss.Namespace, ss.Name, oldReplicas, newReplicas))
	klog.Infof("%s %s/%s scale %d to %d success", cd.Spec.SubsetType, ss.Namespace, ss.Name, oldReplicas, newReplicas)
	return nil
}

// convertToSubset 将不同类型资源转换为subset
func (m *SubsetControl) convertToSubset(set metav1.Object) (*adapter.Subset, error) {

	subset := &adapter.Subset{}
	subset.ObjectMeta = metav1.ObjectMeta{
		Name:                       set.GetName(),
		GenerateName:               set.GetGenerateName(),
		Namespace:                  set.GetNamespace(),
		SelfLink:                   set.GetSelfLink(),
		UID:                        set.GetUID(),
		ResourceVersion:            set.GetResourceVersion(),
		Generation:                 set.GetGeneration(),
		CreationTimestamp:          set.GetCreationTimestamp(),
		DeletionTimestamp:          set.GetDeletionTimestamp(),
		DeletionGracePeriodSeconds: set.GetDeletionGracePeriodSeconds(),
		Labels:                     set.GetLabels(),
		Annotations:                set.GetAnnotations(),
		OwnerReferences:            set.GetOwnerReferences(),
		Finalizers:                 set.GetFinalizers(),
		// ClusterName:                set.GetClusterName(),
	}

	//specReplicas, specPartition, statusReplicas, statusReadyReplicas, statusUpdatedReplicas, statusUpdatedReadyReplicas, err := m.adapter.GetReplicaDetails(set, updatedRevision)
	err := m.adapter.GetReplicaDetails(set, subset)
	if err != nil {
		return subset, err
	}

	subset.Spec.SubsetRef.Resources = append(subset.Spec.SubsetRef.Resources, set)

	return subset, nil
}

func (m *SubsetControl) objectKey(objMeta *metav1.ObjectMeta) client.ObjectKey {
	return types.NamespacedName{
		Namespace: objMeta.Namespace,
		Name:      objMeta.Name,
	}
}

// ClearUnused 删除无用subset
func (m *SubsetControl) ClearUnused(cd *v1beta1.ExtendedDeployment) error {
	for _, ss := range m.unusedSubsets {
		if err := m.DeleteSubset(cd, ss); err != nil {
			klog.V(4).Infof("%s DeleteSubset error: %s, name: %s", m.key, err.Error(), ss.Name)
			return err
		}
	}
	return nil
}

func calculateStepSize(cd *v1beta1.ExtendedDeployment, desiredReplicas int32, replicas int32) int32 {
	var stepSize int32 = 0
	switch cd.Spec.Strategy.RolloutStrategy {
	case v1beta1.GroupRolloutStrategyType:
		stepSize = cd.Spec.Strategy.BatchSize
	case v1beta1.BetaRolloutStrategyType:
		if replicas == 0 {
			stepSize = BetaStepSize
		} else {
			stepSize = cd.Spec.Strategy.BatchSize
		}
	}

	diff := desiredReplicas - replicas
	factor := int32(1)
	if diff < 0 { // 需要缩容
		diff = -diff
		factor = -1
	}

	stepSize = integer.Int32Min(stepSize, diff)

	if stepSize == 0 {
		return 0
	}

	stepSize *= factor

	return stepSize
}

// CheckInplaceUpdate 检查是否执行原地升级
func (m *SubsetControl) CheckInplaceUpdate(cd *v1beta1.ExtendedDeployment, info *adapter.RegionInfo, regionName string) (bool, error) {

	var ss *adapter.Subset
	if info.New != nil && len(info.Olds) == 0 {
		ss = info.New
	} else if len(info.Olds) > 0 && info.New == nil {
		ss = info.Olds[0]
	}
	if ss == nil {
		return false, nil
	}

	spec, exists, _ := utils.GetInplaceSetUpdateSpec(ss.Annotations)
	// 第一次创建的inplaceset，不需要升级
	if !exists && info.New != nil {
		return false, nil
	}
	//异常情况，需要清理InplacesetUpdateSpec
	if len(info.Olds) > 0 && exists {
		utils.DelInplacesetUpdateSpec(ss.Annotations)
	}
	// 判断是否可以开始原地升级
	if !m.isCanInplaceUpdate(cd, ss) {
		return false, nil
	}

	if spec == nil {
		spec = &utils.InplaceSetUpdateSpec{}
	}

	// 执行扩缩容
	stepSize := calculateStepSize(cd, info.DesiredReplicas, int32(spec.UpdatePodNum))
	if stepSize <= 0 {
		return false, nil
	}

	// 为避免业务出现中断, 在出现全量更新时，将步长缩小为 1
	if spec.UpdatePodNum == 0 && stepSize == info.DesiredReplicas {
		stepSize = 1
	}

	if ss.Annotations == nil {
		ss.Annotations = make(map[string]string)
	}
	// 使用已经 override 后的 template
	overrideTemplate := overridemanager.ApplyOverridePolicies(regionName, cd)
	ss.Spec.Template.Spec.Containers = overrideTemplate.Spec.Containers
	spec.UpdatePodNum += int(stepSize)
	spec.PodTemplateHash = hash.HashObject(ss.Spec.Template)
	utils.SetInplacesetUpdateSpec(ss.Annotations, spec)

	b, _ := json.Marshal(overrideTemplate)
	d := md5.Sum(b)

	ss.Annotations[utils.IpsAnnotationTemplateHash] = fmt.Sprintf("%x", d)

	err := m.UpdateSubsetAnnotations(ss, true)
	if err != nil {
		return false, err
	}
	ss.Status.AvailableReplicas -= stepSize
	info.New = ss
	m.rec.Eventf(cd, corev1.EventTypeNormal, eventReasonInplacesetScaleSuccess,
		fmt.Sprintf("Inplace update scale InplaceSet %v/%v from %v to %v success",
			ss.Namespace, ss.Name, int32(spec.UpdatePodNum)-stepSize, spec.UpdatePodNum))
	return true, nil
}

// groupSubset 整理subset和region数据
func (m *SubsetControl) groupSubset(cd *v1beta1.ExtendedDeployment, regionMap map[string]*v1beta1.DeployRegion) error {
	// 构建分区映射
	m.regionInfoMap = make(map[string]*adapter.RegionInfo)
	for _, region := range cd.Spec.Regions {
		replicas, err := ParseRegionReplicas(*cd.Spec.Replicas, *region.Replicas)
		if err != nil {
			return err
		}

		m.regionInfoMap[region.Name] = &adapter.RegionInfo{
			ConfiguredReplicas: replicas,
			DesiredReplicas:    replicas,
			Region:             regionMap[region.Name],
		}
	}

	// 将 Subset 分组到分区中
	m.unusedSubsets = make([]*adapter.Subset, 0)
	for _, ss := range m.allSubsets {
		regionName := getSubsetRegionName(ss)
		rss, ok := m.regionInfoMap[regionName]
		if !ok {
			m.unusedSubsets = append(m.unusedSubsets, ss)
			continue
		}

		// 判断是否是最新的
		if rss.New != nil {
			rss.Olds = append(rss.Olds, ss)
			continue
		}

		if m.subsetTemplateEqualExtendedDeployment(ss, cd) {
			rss.New = ss
			continue
		}
		rss.Olds = append(rss.Olds, ss)
	}

	// 检查是否有自动调度注解，如果有，更新分区期望副本数
	spec, err := utils.GetAutoScheduleSpec(cd)
	if err != nil {
		klog.Errorf("extendeddeployment %v get reschedule info error: %v", m.key, err)
		return err
	}
	if spec != nil {
		m.autoScheduleSpec = spec
		for name, ris := range m.regionInfoMap {
			if regionInfo, ok := spec.Infos[name]; ok {
				ris.DesiredReplicas = regionInfo.DesiredReplicas()
			}
		}
	}

	// 计算分区同步状态
	for _, ri := range m.regionInfoMap {
		if ri.New == nil {
			continue
		}
		desiredReplicas := ri.DesiredReplicas
		ss := ri.New

		// 修正当前批次是否达到期望状态判断逻辑
		// 根据分区当前副本和期望副本，可以知道是扩容还是缩容
		// - 扩容：当前批次滚动完成的判断标准是 ==> 可用副本数 >= 设置的副本数
		// - 缩容：无需判断
		isScaleDown := desiredReplicas < *ss.Spec.Replicas

		spec, inplaceUpdate, _ := utils.GetInplaceSetUpdateSpec(ss.Annotations)
		if inplaceUpdate {
			// FIXME: 处于原地升级状态，如果此时分区期望副本数被修改，应如何处理
			// 此处不做处理，等待期升级完成后，会检查到副本数不一致，会再次触发滚动
			if ss.Status.InplaceUpdateStatus != nil &&
				ss.Status.InplaceUpdateStatus.PodTemplateHash == spec.PodTemplateHash {
				ri.ReplicasDesired = int32(spec.UpdatePodNum) == desiredReplicas
				ri.AvailableDesired = ss.Status.InplaceUpdateStatus.AvailableReplicas >= int32(spec.UpdatePodNum)
				if !ri.AvailableDesired && isScaleDown {
					ri.AvailableDesired = ss.Status.InplaceUpdateStatus.AvailableReplicas >= desiredReplicas
				}
			}
		} else {
			ri.ReplicasDesired = *ss.Spec.Replicas == desiredReplicas
			ri.AvailableDesired = isScaleDown || ss.Status.AvailableReplicas >= *ss.Spec.Replicas
		}

		klog.V(4).Infof("%v %v/%v ReplicasDesired=%v AvailableDesired=%v ,  inplaceUpdate=%v",
			cd.Spec.SubsetType, ss.Namespace, ss.Name, ri.ReplicasDesired, ri.AvailableDesired, inplaceUpdate)
	}
	return nil
}

// regionSyncOnce 按步长执行一次subset同步
func (m *SubsetControl) regionSyncOnce(cd *v1beta1.ExtendedDeployment, regionName string, updateNewSs bool) (updated bool, err error) {
	desiredReplicas := m.regionInfoMap[regionName].DesiredReplicas
	info := m.regionInfoMap[regionName]

	oldReplicas := int32(0) // 旧版本副本总数
	for _, is := range info.Olds {
		oldReplicas += *is.Spec.Replicas
	}

	maxCleanupCount := oldReplicas + info.New.Status.AvailableReplicas - desiredReplicas
	cleanupCount, err := m.cleanupUnhealthyReplicas(cd, regionName, maxCleanupCount)
	klog.Infof("regionSyncOnce %v-%v %v ,updateNewSs=%v ,oldReplicas=%v,maxCleanupCount=%v,cleanupCount=%v", cd.Namespace, cd.Name, regionName, updateNewSs,
		oldReplicas, maxCleanupCount, cleanupCount)
	if err != nil {
		return
	}

	updated = true

	needScaleDown := maxCleanupCount - cleanupCount

	klog.Infof("regionSyncOnce %v-%v %v ,updateNewSs=%v ,needScaleDown=%v", cd.Namespace, cd.Name, regionName, updateNewSs,
		needScaleDown)

	// old+new的总副本数超过了期望副本数，old需要缩容
	if needScaleDown > 0 {
		for i := 0; i < len(info.Olds); i++ {
			ss := info.Olds[i]

			// 一个inplaceset副本数足够缩容
			if *ss.Spec.Replicas > needScaleDown {
				if err = m.ScaleSubset(cd, ss, *ss.Spec.Replicas-needScaleDown); err != nil {
					return
				}
				klog.Infof("regionSyncOnce %v-%v %v ,updateNewSs=%v ,scaleSubset", cd.Namespace, cd.Name, regionName, updateNewSs)
				updated = true
				break
			}
			needScaleDown -= *ss.Spec.Replicas
		}
	}

	if !updateNewSs {
		return
	}

	stepSize := calculateStepSize(cd, info.DesiredReplicas, *info.New.Spec.Replicas)

	klog.Infof("regionSyncOnce %v-%v %v ,updateNewSs=%v ,calculateStepSize", cd.Namespace, cd.Name, regionName, updateNewSs, stepSize)

	if err = m.ScaleSubset(cd, info.New, *info.New.Spec.Replicas+stepSize); err != nil {
		klog.Infof("regionSyncOnce %v-%v %v ,updateNewSs=%v ,scaleSubset err", cd.Namespace, cd.Name, regionName, updateNewSs, err.Error())
		return
	}
	updated = true

	return
}

func (m *SubsetControl) cleanupUnhealthyReplicas(cd *v1beta1.ExtendedDeployment, regionName string, maxCleanupCount int32) (int32, error) {
	sort.Sort(adapter.SubSetsByCreationTimestamp(m.regionInfoMap[regionName].Olds))

	totalScaleDown := int32(0)
	updatedSss := []*adapter.Subset{}
	for i, targetSs := range m.regionInfoMap[regionName].Olds {
		if totalScaleDown >= maxCleanupCount {
			updatedSss = append(updatedSss, m.regionInfoMap[regionName].Olds[i:]...)
			break
		}

		klog.Infof("targetSs.Spec.Replicas: %d, targetSs.Status.AvailableReplicas %d",
			*targetSs.Spec.Replicas, targetSs.Status.AvailableReplicas)
		if *targetSs.Spec.Replicas == targetSs.Status.AvailableReplicas {
			updatedSss = append(updatedSss, targetSs)
			continue
		}

		klog.Infof("Found %d avaliable pods in old Subset %s/%s",
			targetSs.Status.AvailableReplicas, targetSs.Namespace, targetSs.Name)

		scaleDownCount := integer.Int32Min(*targetSs.Spec.Replicas-targetSs.Status.AvailableReplicas,
			maxCleanupCount-totalScaleDown)
		newReplicas := *targetSs.Spec.Replicas - scaleDownCount
		if newReplicas > *targetSs.Spec.Replicas {
			return 0, fmt.Errorf("when cleaning up unhealthy subsets, got invalid request to scale down %s/%s %d -> %d",
				targetSs.Namespace, targetSs.Name, targetSs.Spec.Replicas, newReplicas)
		}

		if newReplicas == 0 {
			if err := m.DeleteSubset(cd, targetSs); err != nil {
				return 0, err
			}
		} else {

			if err := m.ScaleSubset(cd, targetSs, newReplicas); err != nil {
				return 0, err
			}
			targetSs.Spec.Replicas = &newReplicas
			updatedSss = append(updatedSss, targetSs)
		}

		totalScaleDown += scaleDownCount
	}

	m.regionInfoMap[regionName].Olds = updatedSss
	return totalScaleDown, nil
}

// subsetTemplateEqualExtendedDeployment 检查template是否相等
func (m *SubsetControl) subsetTemplateEqualExtendedDeployment(ss *adapter.Subset, cd *v1beta1.ExtendedDeployment) bool {
	regionName := getSubsetRegionName(ss)
	template := overridemanager.ApplyOverridePolicies(regionName, cd)
	ssTemplateHash, ok := ss.GetAnnotations()[utils.IpsAnnotationTemplateHash]
	// 新的判断方法，通过hash
	if ok {
		b, _ := json.Marshal(template)
		d := md5.Sum(b)
		templateHash := fmt.Sprintf("%x", d)
		return ssTemplateHash == templateHash
	}

	// 兼容重构之前版本的判等逻辑
	region := m.regionInfoMap[regionName]
	// 使用已经 override 后的 template

	// 设置节点选择亲和性，注意：添加亲和性，不是覆盖
	if template.Spec.Affinity == nil {
		template.Spec.Affinity = &corev1.Affinity{}
	}
	nodeAffinity := utils.CloneNodeAffinityAndAdd(template.Spec.Affinity.NodeAffinity, region.Region.Spec.MatchLabels)
	template.Spec.Affinity.NodeAffinity = nodeAffinity

	ssTemplate := ss.Spec.Template.DeepCopy()
	delete(template.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	delete(ssTemplate.Labels, appsv1.DefaultDeploymentUniqueLabelKey)

	return apiequality.Semantic.DeepEqual(template, ssTemplate)
}

// isCanInplaceUpdate 检查inplaceset是否可以原地升级
func (m *SubsetControl) isCanInplaceUpdate(cd *v1beta1.ExtendedDeployment, ss *adapter.Subset) bool {
	regionName := getSubsetRegionName(ss)
	region := m.regionInfoMap[regionName]

	template := overridemanager.ApplyOverridePolicies(regionName, cd)
	if template.Spec.Affinity == nil {
		template.Spec.Affinity = &corev1.Affinity{}
	}
	nodeAffinity := utils.CloneNodeAffinityAndAdd(template.Spec.Affinity.NodeAffinity, region.Region.Spec.MatchLabels)
	template.Spec.Affinity.NodeAffinity = nodeAffinity

	ipsTemplate := ss.Spec.Template.DeepCopy()
	delete(template.Labels, apps.DefaultDeploymentUniqueLabelKey)
	delete(ipsTemplate.Labels, apps.DefaultDeploymentUniqueLabelKey)

	desiredReplicas := region.DesiredReplicas
	if len(template.Spec.Containers) != len(ipsTemplate.Spec.Containers) ||
		desiredReplicas > *ss.Spec.Replicas {
		return false
	}

	for i := range template.Spec.Containers {
		template.Spec.Containers[i].Image = ""
		ipsTemplate.Spec.Containers[i].Image = ""
	}
	return apiequality.Semantic.DeepEqual(template, ipsTemplate)
}

// rolloutDeleteAllFirst implements the logic for deletefirst a replica set.
func (m *SubsetControl) rolloutDeleteAllFirst(cd *v1beta1.ExtendedDeployment, regionName string) (bool, error) {
	klog.V(4).Infof("%s DeleteAllFirst rollout start ", m.key)
	ri := m.regionInfoMap[regionName]
	// scale down old replica sets.
	scaledDown, err := m.scaleDownOldReplicaSetsForDeleteAllFirst(ri.Olds, cd)
	if err != nil {
		return false, err
	}
	if scaledDown {
		return true, nil
	}

	// Do not process a deployment when it has old pods running.
	running, err := m.oldPodsRunning(ri.Olds, ri)
	if err != nil || running {
		return false, err
	}

	// If we need to create a new RS, create it now.
	ri.New, err = m.NewSubset(cd, ri, ri.DesiredReplicas)
	if err != nil {
		return false, err
	}

	// scale the subset to desired replicas
	if *(ri.New.Spec.Replicas) != ri.DesiredReplicas {
		if err := m.ScaleSubset(cd, ri.New, ri.DesiredReplicas); err != nil {
			klog.Errorf("%s failed to sync replicas error: %v, region: %s", m.key, err, regionName)
			return false, err
		}
	}

	// Sync deployment status.
	return true, nil
}

func (m *SubsetControl) scaleDownOldReplicaSetsForDeleteAllFirst(oldSss []*adapter.Subset, cd *v1beta1.ExtendedDeployment) (bool, error) {
	scaled := false
	for i, ss := range oldSss {
		if *ss.Spec.Replicas == 0 {
			continue
		}

		if err := m.ScaleSubset(cd, ss, 0); err != nil {
			return false, err
		}

		oldSss[i] = ss
		scaled = true
	}

	return scaled, nil
}

// oldPodsRunning returns whether there are old pods running or any of the old ReplicaSets thinks that it runs pods.
func (m *SubsetControl) oldPodsRunning(oldSSs []*adapter.Subset, region *adapter.RegionInfo) (bool, error) {
	if oldPods := getActualReplicaCountForSubets(oldSSs); oldPods > 0 {
		return true, nil
	}

	if oldAvaliablePods := getAvailableReplicaCountForSubsets(oldSSs); oldAvaliablePods > 0 {
		return true, nil
	}

	var errs []error
	for _, old := range region.Olds {
		podList := &corev1.PodList{}
		selector, err := metav1.LabelSelectorAsSelector(old.Spec.Selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = m.Client.List(context.TODO(), podList, &client.ListOptions{LabelSelector: selector})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, pod := range podList.Items {
			if metav1.IsControlledBy(&pod, old) {
				return true, nil
			}
		}
	}

	return false, utilerrors.NewAggregate(errs)
}

// UpdateSubsetStrategy update subset update strategy
func (m *SubsetControl) UpdateSubsetStrategy(cd *v1beta1.ExtendedDeployment) (err error) {
	for _, ri := range m.regionInfoMap {
		if ri.New == nil {
			continue
		}

		var needsUpdate = false
		if cd.Spec.Strategy.MinReadySeconds != ri.New.Spec.MinReadySeconds {
			needsUpdate = true
		}

		if !needsUpdate && cd.Spec.SubsetType == v1beta1.InPlaceSetSubsetType {
			switch cd.Spec.Strategy.UpdateStrategy.Type {
			case v1beta1.InPlaceIfPossibleUpdateStrategyType, v1beta1.InPlaceOnlyUpdateStrategyType:
				if ri.New.Spec.UpdateStrategy.Type != v1beta1.InPlaceIfPossibleUpdateStrategyType {
					needsUpdate = true
				}
			case v1beta1.DeleteAllFirstUpdateStrategyType, v1beta1.RecreateUpdateStrategyType, v1beta1.RollingUpdateStrategyType:
				if ri.New.Spec.UpdateStrategy.Type != v1beta1.RecreateUpdateStrategyType {
					needsUpdate = true
				}
			}

			if !needsUpdate && cd.Spec.Strategy.UpdateStrategy.GracePeriodSeconds !=
				ri.New.Spec.UpdateStrategy.InPlaceUpdateStrategy.GracePeriodSeconds {
				needsUpdate = true
			}

		}

		if needsUpdate {
			ri.New, err = m.updateSubSetStrategy(cd, ri.New)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// updateSubSetStrategy update Subset update strategy
func (m *SubsetControl) updateSubSetStrategy(cd *v1beta1.ExtendedDeployment, ss *adapter.Subset) (*adapter.Subset, error) {
	set := m.adapter.NewResourceObject()
	var updateError error
	for i := 0; i < updateRetries; i++ {
		getError := m.Client.Get(context.TODO(), m.objectKey(&ss.ObjectMeta), set)
		if getError != nil {
			return nil, getError
		}
		m.adapter.SetUpdateStrategy(set, cd)
		updateError = m.Client.Update(context.TODO(), set)
		if updateError == nil {
			break
		}
	}

	if updateError != nil {
		return nil, updateError
	}

	m.rec.Eventf(cd, corev1.EventTypeNormal, eventReasonInplacesetScaleSuccess,
		fmt.Sprintf("update %v %v/%v update stratrgy success",
			cd.Spec.SubsetType, ss.Namespace, ss.Name))
	klog.Infof("debug: update %v %v/%v update stratrgy success", cd.Spec.SubsetType, ss.Namespace, ss.Name)
	return m.convertToSubset(set)
}
