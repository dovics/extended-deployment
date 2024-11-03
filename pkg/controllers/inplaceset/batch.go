package inplaceset

import (
	"time"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

func getInplaceSetUpdateSpec(ips *v1beta1.InplaceSet) (*utils.InplaceSetUpdateSpec, bool, error) {
	spec, ok, err := utils.GetInplaceSetUpdateSpec(ips.Annotations)
	if err != nil {
		return spec, ok, err
	}
	newImgs := map[string]string{}
	if ok && spec != nil {
		for _, c := range ips.Spec.Template.Spec.Containers {
			newImgs[c.Name] = c.Image
		}
		spec.NewImages = newImgs
		return spec, ok, nil
	}
	return nil, false, nil
}

func getNeedInplaceUpdatePods(ips *v1beta1.InplaceSet, pods []*corev1.Pod, inplacesetUpdateSpec *utils.InplaceSetUpdateSpec) ([]*corev1.Pod, error) {
	if ips.Spec.UpdateStrategy.Type == v1beta1.RecreateUpdateStrategyType {
		return nil, nil
	}
	notTriedPods, err := getInplaceUpdateNotTriedPods(pods, inplacesetUpdateSpec)
	if err != nil {
		return nil, err
	}
	total := inplacesetUpdateSpec.UpdatePodNum
	if total > len(pods) {
		total = len(pods)
	}
	triedPodsNum := len(pods) - len(notTriedPods)
	if triedPodsNum >= total {
		return nil, nil
	}
	needToTryNum := total - triedPodsNum
	if needToTryNum >= len(notTriedPods) {
		return notTriedPods, nil
	} else {
		return notTriedPods[:needToTryNum], nil
	}
}

func getInplaceUpdateNotTriedPods(pods []*corev1.Pod, inplacesetUpdateSpec *utils.InplaceSetUpdateSpec) ([]*corev1.Pod, error) {
	notTriedPods := make([]*corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		tried, err := checkPodInplaceUpdateTried(pod, inplacesetUpdateSpec)
		if err != nil {
			return nil, err
		}
		if !tried {
			notTriedPods = append(notTriedPods, pod)
		}
	}
	return notTriedPods, nil
}

func getInplaceUpdatedPods(ips *v1beta1.InplaceSet, pods []*corev1.Pod, inplacesetUpdateSpec *utils.InplaceSetUpdateSpec) ([]*corev1.Pod, error) {
	updated := make([]*corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		state, err := GetPodInPlaceUpdateState(pod)
		if err != nil || state == nil {
			continue
		}
		if inplacesetUpdateSpec.PodTemplateHash != state.PodTemplateHash {
			continue
		}
		podImgs := map[string]string{}
		for _, c := range pod.Spec.Containers {
			podImgs[c.Name] = c.Image
		}
		for _, specContainer := range ips.Spec.Template.Spec.Containers {
			podContainerImg, ok := podImgs[specContainer.Name]
			if !ok || podContainerImg != specContainer.Image {
				klog.Warningf("pod: %s/%s has the same spec hash: %s with inplaceset: %s/%s, but images not same",
					pod.Namespace, pod.Name, state.PodTemplateHash, ips.Namespace, ips.Name)
				continue
			}
		}
		updated = append(updated, pod)
	}
	return updated, nil
}

func (r *InplaceSetReconciler) manageInplaceUpdate(filteredPods []*corev1.Pod, ips *v1beta1.InplaceSet) (requeAfter time.Duration, err error) {
	inplacesetUpdateSpec, ok, err := getInplaceSetUpdateSpec(ips)
	if err != nil || !ok {
		return 0, err
	}

	needInplaceUpdatePods, err := getNeedInplaceUpdatePods(ips, filteredPods, inplacesetUpdateSpec)
	if err != nil {
		return 0, err
	}

	for _, pod := range needInplaceUpdatePods {
		res := r.TryInplaceUpdate(ips, pod, inplacesetUpdateSpec)
		klog.Infof("TryInplaceUpdate pod=%+v ,  res=%+v", pod.Namespace+"/"+pod.Name, res)
		if res.UpdateErr != nil {
			return 0, err
		}
		if res.InPlaceUpdate {
			if res.DelayDuration > 0 {
				requeAfter = res.DelayDuration
			}
		}
	}
	return requeAfter, nil
}
