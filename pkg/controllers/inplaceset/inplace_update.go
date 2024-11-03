package inplaceset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
)

var (

	// InPlaceUpdateStateKey records the state of inplace-update.
	// The value of annotation is PodInPlaceUpdateState.
	InPlaceUpdateStateKey string = v1beta1.GroupVersion.Group + "/inplace-update-state"

	// InPlaceUpdateGraceKey records the spec that Pod should be updated when
	// grace period ends.
	InPlaceUpdateGraceKey string = v1beta1.GroupVersion.Group + "/inplace-update-grace"
)

// InPlaceUpdateContainerStatus records the statuses of the container that are mainly used
// to determine whether the InPlaceUpdate is completed.
type InPlaceUpdateContainerStatus struct {
	ImageID     string `json:"imageID,omitempty"`
	ContainerID string `json:"containerID,omitempty"`
}

// InPlaceUpdateContainerBatch indicates the timestamp and containers for a batch update
type InPlaceUpdateContainerBatch struct {
	// Timestamp is the time for this update batch
	Timestamp metav1.Time `json:"timestamp"`
	// Containers is the name list of containers for this update batch
	Containers []string `json:"containers"`
}

// PodInPlaceUpdateState records latest inplace-update state, including old statuses of containers.
type PodInPlaceUpdateState struct {

	// UpdateTimestamp is the start time when the in-place update happens.
	UpdateTimestamp metav1.Time `json:"updateTimestamp"`

	PodTemplateHash string `json:"podTemplateHash"`

	LastContainerImage map[string]string
	// LastContainerStatuses records the before-in-place-update container statuses. It is a map from ContainerName
	// to InPlaceUpdateContainerStatus
	LastContainerStatuses map[string]InPlaceUpdateContainerStatus `json:"lastContainerStatuses"`

	SpecContainerImages map[string]string `json:"specContainerImages"`

	// ContainerBatchesRecord records the update batches that have patched in this revision.
	ContainerBatchesRecord []InPlaceUpdateContainerBatch `json:"containerBatchesRecord,omitempty"`
}

type UpdateOptions struct {
	GracePeriodSeconds int32
}

// PodUpdateSpec records the images of containers which need to in-place update. set in pod annotations
type PodUpdateSpec struct {
	ContainerImages    map[string]string `json:"containerImages,omitempty"`
	GraceSeconds       int32             `json:"graceSeconds,omitempty"`
	OldContainerImages map[string]string `json:"oldContainerImages,omitempty"`
	// update Spec Generation the inplace update performed on
	PodTemplateHash string `json:"podTemplateHash"`
}

type UpdateResult struct {
	InPlaceUpdate      bool
	UpdateErr          error
	DelayDuration      time.Duration
	NewResourceVersion string
}

func GetInPlaceUpdateGrace(obj metav1.Object) (*PodUpdateSpec, error) {
	v, ok := obj.GetAnnotations()[InPlaceUpdateGraceKey]
	if !ok {
		return nil, nil
	}
	spec := &PodUpdateSpec{}
	if err := json.Unmarshal([]byte(v), spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func GetPodInPlaceUpdateState(obj metav1.Object) (*PodInPlaceUpdateState, error) {
	v, ok := obj.GetAnnotations()[InPlaceUpdateStateKey]
	if !ok {
		return nil, nil
	}
	state := &PodInPlaceUpdateState{}
	if err := json.Unmarshal([]byte(v), state); err != nil {
		return nil, err
	}
	return state, nil
}

func RemoveInPlaceUpdateGrace(obj metav1.Object) {
	delete(obj.GetAnnotations(), InPlaceUpdateGraceKey)
}

// defaultPatchUpdateSpecToPod returns new pod that merges spec into old pod
func patchUpdateSpecToPod(pod *corev1.Pod, updateSpec *PodUpdateSpec, state *PodInPlaceUpdateState) *corev1.Pod {

	klog.V(5).Infof("Begin to in-place update pod %s/%s with update spec %v, state %v", pod.Namespace, pod.Name, DumpJSON(updateSpec), DumpJSON(state))

	// DO NOT modify the fields in spec for it may have to retry on conflict in updatePodInPlace

	// update images and record current imageIDs for the containers to update
	containersImageChanged := sets.NewString()
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		newImage, exists := updateSpec.ContainerImages[c.Name]
		if !exists {
			continue
		}

		if state.LastContainerImage == nil {
			state.LastContainerImage = map[string]string{}
		}

		state.LastContainerImage[c.Name], pod.Spec.Containers[i].Image = pod.Spec.Containers[i].Image, newImage
		containersImageChanged.Insert(c.Name)
	}
	for _, c := range pod.Status.ContainerStatuses {
		if containersImageChanged.Has(c.Name) {
			if state.LastContainerStatuses == nil {
				state.LastContainerStatuses = map[string]InPlaceUpdateContainerStatus{}
			}
			state.LastContainerStatuses[c.Name] = InPlaceUpdateContainerStatus{ImageID: c.ImageID, ContainerID: c.ContainerID}
		}
	}

	state.ContainerBatchesRecord = append(state.ContainerBatchesRecord, InPlaceUpdateContainerBatch{
		Timestamp:  metav1.NewTime(time.Now()),
		Containers: containersImageChanged.List(),
	})

	klog.V(5).Infof("Decide to in-place update pod %s/%s with state %v", pod.Namespace, pod.Name, DumpJSON(state))

	inPlaceUpdateStateJSON, _ := json.Marshal(state)
	pod.Annotations[InPlaceUpdateStateKey] = string(inPlaceUpdateStateJSON)
	return pod
}

// DumpJSON returns the JSON encoding
func DumpJSON(o interface{}) string {
	j, _ := json.Marshal(o)
	return string(j)
}

func (r *InplaceSetReconciler) finishGracePeriod(ips *v1beta1.InplaceSet, pod *corev1.Pod) (time.Duration, error) {
	var delayDuration time.Duration
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		clone := &corev1.Pod{}
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, clone)
		if err != nil {
			return err
		}

		updateSpec, err := GetInPlaceUpdateGrace(clone)
		if updateSpec == nil || err != nil {
			return err
		}
		graceDuration := time.Second * time.Duration(updateSpec.GraceSeconds)

		updateState, err := GetPodInPlaceUpdateState(clone)
		if err == nil && updateState == nil {
			return fmt.Errorf("pod has %s but %s not found", InPlaceUpdateGraceKey, InPlaceUpdateStateKey)
		} else if err != nil {
			return err
		}

		if span := time.Since(updateState.UpdateTimestamp.Time); span < graceDuration {
			delayDuration = roundupSeconds(graceDuration - span)
			return nil
		}

		clone = patchUpdateSpecToPod(clone, updateSpec, updateState)
		RemoveInPlaceUpdateGrace(clone)
		_, err = r.kubeClient.CoreV1().Pods(pod.Namespace).Update(context.TODO(), clone, metav1.UpdateOptions{})
		if err == nil {
			r.EventRecorder.Eventf(ips, corev1.EventTypeNormal, EventSuccessfulInplaceUpdate, `inplace update pod: %s succeed`, pod.Name)
		}
		return err
	})

	return delayDuration, err
}

func roundupSeconds(d time.Duration) time.Duration {
	if d%time.Second == 0 {
		return d
	}
	return (d/time.Second + 1) * time.Second
}

func GetContainerStatus(name string, pod *corev1.Pod) *corev1.ContainerStatus {
	if pod == nil {
		return nil
	}
	for i := range pod.Status.ContainerStatuses {
		v := &pod.Status.ContainerStatuses[i]
		if v.Name == name {
			return v
		}
	}
	return nil
}

func (r *InplaceSetReconciler) refreshPods(ips *v1beta1.InplaceSet, pods []*corev1.Pod) (requeueAfter time.Duration, err error) {
	for _, pod := range pods {
		res := r.refreshPod(ips, pod)
		if res.RefreshErr != nil {
			return 0, res.RefreshErr
		}
		if res.DelayDuration > 0 {
			requeueAfter = res.DelayDuration
			continue
		}
	}
	return requeueAfter, nil
}

type RefreshResult struct {
	RefreshErr    error
	DelayDuration time.Duration
}

func (r *InplaceSetReconciler) refreshPod(ips *v1beta1.InplaceSet, pod *corev1.Pod) RefreshResult {
	spec, err := GetInPlaceUpdateGrace(pod)
	if err != nil {
		return RefreshResult{RefreshErr: err}
	}
	if spec != nil && spec.GraceSeconds > 0 {
		klog.V(5).Infof("pod: %s/%s, grace period: %d", pod.Namespace, pod.Name, spec.GraceSeconds)
		delayDuration, err := r.finishGracePeriod(ips, pod)
		if err != nil {
			return RefreshResult{RefreshErr: err}
		}
		return RefreshResult{DelayDuration: delayDuration}
	}
	state, err := GetPodInPlaceUpdateState(pod)
	if err != nil {
		return RefreshResult{RefreshErr: err}
	}
	if state != nil {
		// check in-place updating has not completed yet
		if checkErr := checkContainersInPlaceUpdateCompleted(pod, state); checkErr != nil {
			klog.V(4).Infof("Check Pod %s/%s in-place update not completed yet: %v", pod.Namespace, pod.Name, checkErr)
			return RefreshResult{}
		}
	}

	if !containsReadinessGate(pod) {
		return RefreshResult{}
	}

	inplaceUpdateReadyCond := getCondition(pod, v1beta1.InPlaceUpdateReady)
	if inplaceUpdateReadyCond != nil && inplaceUpdateReadyCond.Status == corev1.ConditionTrue {
		return RefreshResult{}
	}

	newCondition := corev1.PodCondition{
		Type:               v1beta1.InPlaceUpdateReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	err = r.updateCondition(pod, newCondition)
	return RefreshResult{RefreshErr: err}
}

func checkPodInplaceUpdateTried(pod *corev1.Pod, inplaceSetUpdateSpec *utils.InplaceSetUpdateSpec) (bool, error) {
	tried := true
	state, err := GetPodInPlaceUpdateState(pod)
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	} else {
		if inplaceSetUpdateSpec.PodTemplateHash != state.PodTemplateHash {
			return false, nil
		}
		for c, newImg := range inplaceSetUpdateSpec.NewImages {
			stateSpecImg, ok := state.SpecContainerImages[c]
			if !ok {
				continue
			}
			if newImg != stateSpecImg {
				tried = false
			}
		}
	}
	return tried, nil
}

func (r *InplaceSetReconciler) TryInplaceUpdate(ips *v1beta1.InplaceSet, pod *corev1.Pod, inplaceSetUpdateSpc *utils.InplaceSetUpdateSpec) UpdateResult {

	graceSecond := int32(0)
	if ips.Spec.UpdateStrategy.InPlaceUpdateStrategy != nil {
		graceSecond = ips.Spec.UpdateStrategy.DeepCopy().InPlaceUpdateStrategy.GracePeriodSeconds
	} else if containsReadinessGate(pod) {
		// if readinessgates set, default 1 second
		graceSecond = 1
	}
	tried, err := checkPodInplaceUpdateTried(pod, inplaceSetUpdateSpc)
	if err != nil {
		return UpdateResult{UpdateErr: err}
	}

	if tried {
		klog.V(5).Infof("pod: %s/%s already tried inplace update ", pod.Namespace, pod.Name)
		return UpdateResult{}
	}

	// 2. update condition for pod with readiness-gate
	if containsReadinessGate(pod) {
		newCondition := corev1.PodCondition{
			Type:               v1beta1.InPlaceUpdateReady,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Status:             corev1.ConditionFalse,
			Reason:             "StartInPlaceUpdate",
		}
		if err := r.updateCondition(pod, newCondition); err != nil {
			return UpdateResult{InPlaceUpdate: true, UpdateErr: err}
		}
	}
	spec := &PodUpdateSpec{
		GraceSeconds:    graceSecond,
		ContainerImages: inplaceSetUpdateSpc.NewImages,
		PodTemplateHash: inplaceSetUpdateSpc.PodTemplateHash,
	}
	// 3. update container images
	newResourceVersion, err := r.updatePodInPlace(ips, pod, spec)
	if err != nil {
		return UpdateResult{InPlaceUpdate: true, UpdateErr: err}
	}

	var delayDuration time.Duration
	if graceSecond > 0 {
		delayDuration = time.Second * time.Duration(graceSecond)
	}
	return UpdateResult{InPlaceUpdate: true, DelayDuration: delayDuration, NewResourceVersion: newResourceVersion}
}

// if graceSeconds > 0, update pod with new grace and new state, else update update container images and state and delete grace
func (r *InplaceSetReconciler) updatePodInPlace(ips *v1beta1.InplaceSet, pod *corev1.Pod, spec *PodUpdateSpec) (string, error) {
	var newResourceVersion string
	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		clone := &corev1.Pod{}
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, clone)
		if err != nil {
			return err
		}

		inPlaceUpdateState := PodInPlaceUpdateState{
			UpdateTimestamp:     metav1.NewTime(time.Now()),
			SpecContainerImages: spec.ContainerImages,
			PodTemplateHash:     spec.PodTemplateHash,
		}
		inPlaceUpdateStateJSON, _ := json.Marshal(inPlaceUpdateState)
		if clone.Annotations == nil {
			clone.Annotations = map[string]string{}
		}
		clone.Annotations[InPlaceUpdateStateKey] = string(inPlaceUpdateStateJSON)

		if spec.GraceSeconds <= 0 {
			clone = patchUpdateSpecToPod(clone, spec, &inPlaceUpdateState)
			RemoveInPlaceUpdateGrace(clone)
		} else {
			inPlaceUpdateSpecJSON, _ := json.Marshal(spec)
			clone.Annotations[InPlaceUpdateGraceKey] = string(inPlaceUpdateSpecJSON)
		}

		newPod, updateErr := r.kubeClient.CoreV1().Pods(pod.Namespace).Update(context.TODO(), clone, metav1.UpdateOptions{})
		if updateErr == nil {
			if spec.GraceSeconds <= 0 {
				r.EventRecorder.Eventf(ips, corev1.EventTypeNormal, EventSuccessfulInplaceUpdate, `inplace update pod: %s succeed`, pod.Name)
			}
			newResourceVersion = newPod.ResourceVersion
		}
		return updateErr
	})
	return newResourceVersion, retryErr
}

// InjectReadinessGate injects InPlaceUpdateReady into pod.spec.readinessGates
func InjectReadinessGate(pod *corev1.Pod) {
	for _, r := range pod.Spec.ReadinessGates {
		if r.ConditionType == v1beta1.InPlaceUpdateReady {
			return
		}
	}
	pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{ConditionType: v1beta1.InPlaceUpdateReady})
}

func (r *InplaceSetReconciler) updateCondition(pod *corev1.Pod, condition corev1.PodCondition) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		clone := &corev1.Pod{}
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, clone)
		if err != nil {
			return err
		}

		setPodCondition(clone, condition)
		// We only update the ready condition to False, and let Kubelet update it to True
		if condition.Status == corev1.ConditionFalse {
			updatePodReadyCondition(clone)
		}
		return r.Status().Update(context.TODO(), clone)
	})
}

func getCondition(pod *corev1.Pod, cType corev1.PodConditionType) *corev1.PodCondition {
	for _, c := range pod.Status.Conditions {
		if c.Type == cType {
			return &c
		}
	}
	return nil
}

func setPodCondition(pod *corev1.Pod, condition corev1.PodCondition) {
	for i, c := range pod.Status.Conditions {
		if c.Type == condition.Type {
			if c.Status != condition.Status {
				pod.Status.Conditions[i] = condition
			}
			return
		}
	}
	pod.Status.Conditions = append(pod.Status.Conditions, condition)
}

func updatePodReadyCondition(pod *corev1.Pod) {
	podReady := getCondition(pod, corev1.PodReady)
	if podReady == nil {
		return
	}

	containersReady := getCondition(pod, corev1.ContainersReady)
	if containersReady == nil || containersReady.Status != corev1.ConditionTrue {
		return
	}

	var unreadyMessages []string
	for _, rg := range pod.Spec.ReadinessGates {
		c := getCondition(pod, rg.ConditionType)
		if c == nil {
			unreadyMessages = append(unreadyMessages, fmt.Sprintf("corresponding condition of pod readiness gate %q does not exist.", string(rg.ConditionType)))
		} else if c.Status != corev1.ConditionTrue {
			unreadyMessages = append(unreadyMessages, fmt.Sprintf("the status of pod readiness gate %q is not \"True\", but %v", string(rg.ConditionType), c.Status))
		}
	}

	newPodReady := corev1.PodCondition{
		Type:               corev1.PodReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	}
	// Set "Ready" condition to "False" if any readiness gate is not ready.
	if len(unreadyMessages) != 0 {
		unreadyMessage := strings.Join(unreadyMessages, ", ")
		newPodReady = corev1.PodCondition{
			Type:    corev1.PodReady,
			Status:  corev1.ConditionFalse,
			Reason:  "ReadinessGatesNotReady",
			Message: unreadyMessage,
		}
	}

	setPodCondition(pod, newPodReady)
}

func containsReadinessGate(pod *corev1.Pod) bool {
	for _, r := range pod.Spec.ReadinessGates {
		if r.ConditionType == v1beta1.InPlaceUpdateReady {
			return true
		}
	}
	return false
}

func checkContainersInPlaceUpdateCompleted(pod *corev1.Pod, inPlaceUpdateState *PodInPlaceUpdateState) error {

	containerImages := make(map[string]string, len(pod.Spec.Containers))
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		containerImages[c.Name] = c.Image
		if len(strings.Split(c.Image, ":")) <= 1 {
			containerImages[c.Name] = fmt.Sprintf("%s:latest", c.Image)
		}
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if oldStatus, ok := inPlaceUpdateState.LastContainerStatuses[cs.Name]; ok {
			if oldStatus.ImageID == cs.ImageID && inPlaceUpdateState.LastContainerImage[cs.Name] != containerImages[cs.Name] {
				if oldStatus.ContainerID == cs.ContainerID {
					return fmt.Errorf("container %s imageID and containerID not changed", cs.Name)
				}

				klog.V(5).Infof("container %s imageID not changed, but containerID changed, image tag cause inplace update.", cs.Name)
			}

			delete(inPlaceUpdateState.LastContainerStatuses, cs.Name)
		}
	}

	if len(inPlaceUpdateState.LastContainerStatuses) > 0 {
		return fmt.Errorf("not found statuses of containers %v", inPlaceUpdateState.LastContainerStatuses)
	}

	return nil
}

func calcInplaceUpdateStatus(ips *v1beta1.InplaceSet, filteredPods []*corev1.Pod) (*v1beta1.InplaceUpdateStatus, error) {
	inplacesetUpdateSpec, ok, err := getInplaceSetUpdateSpec(ips)
	if err != nil || !ok {
		return nil, err
	}
	updatedPods, err := getInplaceUpdatedPods(ips, filteredPods, inplacesetUpdateSpec)
	if err != nil {
		return nil, err
	}
	status := &v1beta1.InplaceUpdateStatus{
		PodTemplateHash: inplacesetUpdateSpec.PodTemplateHash,
	}
	for _, pod := range updatedPods {
		state, err := GetPodInPlaceUpdateState(pod)
		if err != nil || state == nil {
			continue
		}
		if podutil.IsPodReady(pod) && checkContainersInPlaceUpdateCompleted(pod, state) == nil {
			status.ReadyReplicas++
			if podutil.IsPodAvailable(pod, ips.Spec.MinReadySeconds, metav1.Now()) {
				status.AvailableReplicas++
			}
		}
	}
	return status, nil
}
