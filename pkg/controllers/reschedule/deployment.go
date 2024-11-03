package reschedule

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/controllers/deployregion"
	"github.com/dovics/extendeddeployment/pkg/utils/overridemanager"

	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment/adapter"
	"github.com/dovics/extendeddeployment/pkg/utils"
	"github.com/dovics/extendeddeployment/pkg/utils/refmanager"
)

func RequestResourceByRegion(deployment *v1beta1.ExtendedDeployment, regionName string) (*deployregion.Resource, error) {
	template := overridemanager.ApplyOverridePolicies(regionName, deployment)

	resource := deployregion.EmptyResource()
	for _, c := range template.Spec.Containers {
		resource.Add(c.Resources.Requests)
	}

	if resource.Memory+resource.MilliCPU == 0 {
		klog.Warning("no request found in pod containers")
		return nil, fmt.Errorf("no request found")
	}

	return resource, nil
}

func GetFailedRegions(deployment *v1beta1.ExtendedDeployment) (map[string]bool, bool) {
	failedRegionMap := make(map[string]bool, len(deployment.Spec.Regions))
	hasFailedRegion := false

	failedRegionString, ok := utils.GetFailedFlag(deployment)
	if ok && failedRegionString != "" {
		hasFailedRegion = true
		failedRegions := strings.Split(failedRegionString, ",")
		for _, regionName := range failedRegions {
			failedRegionMap[regionName] = true
		}
	}

	for _, region := range deployment.Spec.Regions {
		if _, ok := failedRegionMap[region.Name]; !ok {
			failedRegionMap[region.Name] = false
		}
	}

	return failedRegionMap, hasFailedRegion
}

func QuerySubsetAndRegionInfo(ctx context.Context, c client.Client, cd *v1beta1.ExtendedDeployment, scheme *runtime.Scheme) (map[string][]*adapter.Subset, error) {
	var ad adapter.Adapter

	switch cd.Spec.SubsetType {
	case v1beta1.DeploymentSubsetType:
		ad = &adapter.DeploymentAdapter{
			Client: c,
			Scheme: scheme,
		}
	case v1beta1.ReplicaSetSubsetType:
		ad = &adapter.ReplicaSetAdapter{
			Client: c,
			Scheme: scheme,
		}
	default:
		ad = &adapter.InplaceSetAdapter{
			Client: c,
			Scheme: scheme,
		}
	}

	key := fmt.Sprintf("%v/%v", cd.Namespace, cd.Name)
	rml, err := checkRegions(ctx, cd, key, c)
	if err != nil {
		return nil, err
	}
	allSubsets, err := getAllSubsets(ctx, ad, c, scheme, cd)
	if err != nil {
		return nil, err
	}

	return groupSubset(allSubsets, cd, rml), nil
}

// GetAllSubsets returns all of subsets owned by the UnitedDeployment.
func getAllSubsets(ctx context.Context, ad adapter.Adapter, c client.Client, scheme *runtime.Scheme, cd *v1beta1.ExtendedDeployment) (subSets []*adapter.Subset, err error) {
	selector, err := metav1.LabelSelectorAsSelector(cd.Spec.Selector)
	if err != nil {
		return nil, err
	}

	setList := ad.NewResourceListObject()
	err = c.List(ctx, setList, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     cd.Namespace,
	})
	if err != nil {
		return nil, err
	}

	manager, err := refmanager.New(c, cd.Spec.Selector, cd, scheme)
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
		subSet, err := convertToSubset(claimedSet, ad)
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

func convertToSubset(set metav1.Object, ad adapter.Adapter) (*adapter.Subset, error) {
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
	err := ad.GetReplicaDetails(set, subset)
	if err != nil {
		return subset, err
	}

	subset.Spec.SubsetRef.Resources = append(subset.Spec.SubsetRef.Resources, set)

	return subset, nil
}

func groupSubset(allSubsets []*adapter.Subset, cd *v1beta1.ExtendedDeployment, rml map[string]map[string]string) map[string][]*adapter.Subset {
	res := make(map[string][]*adapter.Subset)
	for _, region := range cd.Spec.Regions {
		res[region.Name] = make([]*adapter.Subset, 0)
	}

	// 将 Subset 分组到分区中
	for _, ss := range allSubsets {
		regionName := getSubsetRegionName(ss)
		if _, ok := res[regionName]; !ok { // subset 在 extendeddeployment 中已经不存在了
			continue
		}
		if subsetTemplateEqualExtendedDeployment(ss, cd, rml[regionName]) {
			res[regionName] = append(res[regionName], ss)
		}
	}

	return res
}

func checkRegions(ctx context.Context, deploy *v1beta1.ExtendedDeployment, key string, c client.Client) (map[string]map[string]string, error) {
	res := make(map[string]map[string]string)
	regionList := &v1beta1.DeployRegionList{}
	err := c.List(ctx, regionList)
	if err != nil {
		klog.Errorf("[Reschedule] extendeddeployment %v query region list error: %v", key, err)
		return nil, err
	}

	mp := make(map[string]*v1beta1.DeployRegion)
	for i := range regionList.Items {
		r := regionList.Items[i]
		mp[r.Name] = &r
	}

	for _, region := range deploy.Spec.Regions {
		r, ok := mp[region.Name]
		if !ok {
			klog.Errorf("extendeddeployment %v query region error, region %v is not exists",
				key, region.Name)
			err := fmt.Errorf("region %v is not exists, ", region.Name)
			return nil, err
		}
		res[region.Name] = r.Spec.MatchLabels
	}
	return res, nil
}

// subsetTemplateEqualExtendedDeployment 检查template是否相等 TODO 后续复用subset函数
func subsetTemplateEqualExtendedDeployment(ss *adapter.Subset, cd *v1beta1.ExtendedDeployment, matchLabels map[string]string) bool {
	regionName := getSubsetRegionName(ss)
	// 使用已经 override 后的 template
	template := overridemanager.ApplyOverridePolicies(regionName, cd)

	// 设置节点选择亲和性，注意：添加亲和性，不是覆盖
	if template.Spec.Affinity == nil {
		template.Spec.Affinity = &corev1.Affinity{}
	}
	nodeAffinity := utils.CloneNodeAffinityAndAdd(template.Spec.Affinity.NodeAffinity, matchLabels)
	template.Spec.Affinity.NodeAffinity = nodeAffinity

	ssTemplate := ss.Spec.Template.DeepCopy()
	delete(template.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	delete(ssTemplate.Labels, appsv1.DefaultDeploymentUniqueLabelKey)

	return apiequality.Semantic.DeepEqual(template, ssTemplate)
}

// 获取Subset分区名称
func getSubsetRegionName(ss *adapter.Subset) string {
	return ss.Annotations[utils.IpsAnnotationRegionName]
}

func SetRescheduleCondition(deployment *v1beta1.ExtendedDeployment, rescheduleType RescheduleType) {
	var reason, message string
	switch rescheduleType {
	case RescheduleForFailedRegion:
		reason = "FailureSchedule"
		message = "some region is failure"
	case RescheduleForResourceInsufficient:
		reason = "InsufficientSchedule"
		message = "some region resource is insufficient"
	case RescheduleForFailedRegionAndResourceInsufficient:
		reason = "FailureSchedule"
		message = "some region is failure, some region resource is insufficient"
	default:
		reason = "Reschedule Failed"
		message = "Reschedule Failed"
	}

	c := utils.NewDeploymentCondition(DeploymentReschedule, corev1.ConditionTrue,
		reason, message)
	utils.SetDeploymentCondition(&deployment.Status, *c)
}

func DelRescheduleCondition(deployment *v1beta1.ExtendedDeployment) {
	c := utils.NewDeploymentCondition(DeploymentReschedule, corev1.ConditionFalse,
		"all region works well", "all region works well")
	utils.SetDeploymentCondition(&deployment.Status, *c)
}
