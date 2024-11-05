/*
Copyright 2024.

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
package inplaceset

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	corev1 "k8s.io/api/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/integer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dovics/extendeddeployment/api/v1beta1"
	"github.com/dovics/extendeddeployment/pkg/utils"
	"github.com/dovics/extendeddeployment/pkg/utils/informermanager"
)

const (
	// Realistic value of the burstReplica field for the replica set manager based off
	// performance requirements for kubernetes 1.0.
	BurstReplicas = 200

	EventSuccessfulInplaceUpdate = `SuccessfulInplaceUpdate`
)

var (
	gvk = schema.GroupVersionKind{
		Group:   v1beta1.GroupVersion.Group,
		Version: v1beta1.GroupVersion.Version,
		Kind:    "InplaceSet",
	}
	ControllerName = "inplaceset"
)

// InplaceSetReconciler reconciles a InplaceSet object
type InplaceSetReconciler struct {
	apiReader            client.Reader
	DisableInplaceUpdate bool
	client.Client
	Scheme           *runtime.Scheme
	EventRecorder    record.EventRecorder
	kubeClient       clientset.Interface
	ConcurrencyCount int
	InformerManager  informermanager.SingleClusterInformerManager

	podControl controller.PodControlInterface

	// A InplaceSet is temporarily suspended after creating/deleting these many replicas.
	// It resumes normal action after observing the watch events for them.
	burstReplicas int

	// // A TTLCache of pod creates/deletes each rc expects to see.
	// expectations *controller.UIDTrackingControllerExpectations
}

// +kubebuilder:rbac:groups=extendeddeployment.io,resources=inplacesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extendeddeployment.io,resources=inplacesets/status,verbs=get;update;patch

func (r *InplaceSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(4).Infof("get reconcile req: %s", req.NamespacedName)
	ips := &v1beta1.InplaceSet{}

	err := r.Get(ctx, req.NamespacedName, ips)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if ips.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	requeueAfter, err := r.syncReplicaSet(ctx, ips)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func NewInplaceSetReconciler(disableInplaceUpdate bool, apiReader client.Reader, ctrlClient client.Client,
	runtimeScheme *runtime.Scheme, kubeClient clientset.Interface, burstReplicas int,
	recorder record.EventRecorder) *InplaceSetReconciler {
	r := &InplaceSetReconciler{
		apiReader:            apiReader,
		DisableInplaceUpdate: disableInplaceUpdate,
		Client:               ctrlClient,
		Scheme:               runtimeScheme,
		kubeClient:           kubeClient,
		podControl: controller.RealPodControl{
			KubeClient: kubeClient,
			Recorder:   recorder,
		},
		EventRecorder: recorder,
		burstReplicas: burstReplicas,
	}
	return r
}

// SetupWithManager sets up the controller with the Manager.
func (r *InplaceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return utilerrors.NewAggregate([]error{
		ctrl.NewControllerManagedBy(mgr).
			For(&v1beta1.InplaceSet{}).
			Owns(&corev1.Pod{}).
			Complete(r),
		mgr.Add(r),
	})
}

// Start starts an asynchronous loop that monitors the status of cluster.
func (dc *InplaceSetReconciler) Start(ctx context.Context) error {
	return nil
}

// getInplaceSetsWithSameController returns a list of InplaceSets with the same
// owner as the given InplaceSet.
func (r *InplaceSetReconciler) getInplaceSetsWithSameController(ips *v1beta1.InplaceSet) []*v1beta1.InplaceSet {
	controllerRef := metav1.GetControllerOf(ips)
	if controllerRef == nil {
		utilruntime.HandleError(fmt.Errorf("InplaceSet has no controller: %v", ips))
		return nil
	}

	allRSs, err := r.list(ips.Namespace)
	if err != nil {
		utilruntime.HandleError(err)
		return nil
	}

	var relatedRSs []*v1beta1.InplaceSet
	for _, r := range allRSs {
		if ref := metav1.GetControllerOf(r); ref != nil && ref.UID == controllerRef.UID {
			relatedRSs = append(relatedRSs, r)
		}
	}

	if klog.V(2).Enabled() {
		var relatedNames []string
		for _, r := range relatedRSs {
			relatedNames = append(relatedNames, r.Name)
		}
		klog.InfoS("Found related InplaceSets", "replicaSet", klog.KObj(ips), "relatedInplaceSets", relatedNames)
	}

	return relatedRSs
}

// getPodInplaceSets returns a list of InplaceSets matching the given pod.
// func (r *InplaceSetReconciler) getPodInplaceSets(pod *corev1.Pod) []*v1beta1.InplaceSet {
// 	rss, err := r.GetPodInplaceSets(pod)
// 	if err != nil {
// 		return nil
// 	}
// 	if len(rss) > 1 {
// 		// ControllerRef will ensure we don't do anything crazy, but more than one
// 		// item in this list nevertheless constitutes user error.
// 		utilruntime.HandleError(fmt.Errorf("user error! more than one is selecting pods with labels: %+v", pod.Labels))
// 	}
// 	return rss
// }

// GetPodReplicaSets returns a list of ReplicaSets that potentially match a pod.
// Only the one specified in the Pod's ControllerRef will actually manage it.
// Returns an error only if no matching ReplicaSets are found.
func (r *InplaceSetReconciler) GetPodInplaceSets(pod *corev1.Pod) ([]*v1beta1.InplaceSet, error) {
	if len(pod.Labels) == 0 {
		return nil, fmt.Errorf("no ReplicaSets found for pod %v because it has no labels", pod.Name)
	}

	list, err := r.list(pod.Namespace)
	if err != nil {
		return nil, err
	}

	rss := make([]*v1beta1.InplaceSet, len(list))
	for _, ips := range list {
		if ips.Namespace != pod.Namespace {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(ips.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("invalid selector: %v", err)
		}

		// If a ReplicaSet with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		rss = append(rss, ips)
	}

	if len(rss) == 0 {
		return nil, fmt.Errorf("could not find InplaceSet for pod %s in namespace %s with labels: %v", pod.Name, pod.Namespace, pod.Labels)
	}

	return rss, nil
}

// func (r *InplaceSetReconciler) get(ns, name string) (*v1beta1.InplaceSet, error) {
// 	ips := &v1beta1.InplaceSet{}
// 	err := r.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, ips)
// 	return ips, err
// }

func (r *InplaceSetReconciler) apiGet(ns, name string) (*v1beta1.InplaceSet, error) {
	ips := &v1beta1.InplaceSet{}
	err := r.apiReader.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, ips)
	return ips, err
}

func (r *InplaceSetReconciler) list(ns string) (list []*v1beta1.InplaceSet, err error) {
	ipss := &v1beta1.InplaceSetList{}
	opts := []client.ListOption{
		client.InNamespace(ns),
	}
	err = r.List(context.TODO(), ipss, opts...)
	for k := range ipss.Items {
		list = append(list, &ipss.Items[k])
	}
	return
}

// syncReplicaSet will sync the ReplicaSet with the given key if it has had its expectations fulfilled,
// meaning it did not expect to see any more of its pods created or deleted. This function is not meant to be
// invoked concurrently with the same key.
func (r *InplaceSetReconciler) syncReplicaSet(ctx context.Context, ips *v1beta1.InplaceSet) (requeueAfter time.Duration, err error) {
	logger := klog.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing %q (%s/%s)", ips.Namespace, ips.Name, time.Since(startTime))
	}()

	if ips.DeletionTimestamp != nil {
		return 0, nil
	}
	// TODO：check spec.Template.metadata nil problem
	// ips.Spec.Template.Labels = map[string]string{`inplaceset`: name}
	selector, err := metav1.LabelSelectorAsSelector(ips.Spec.Selector)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error converting pod selector to selector: %v", err))
		return 0, nil
	}

	// list all pods to include the pods that don't match the ips`s selector
	// anymore but has the stale controller ref.
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, &client.ListOptions{Namespace: ips.Namespace, LabelSelector: selector})
	if err != nil {
		return 0, err
	}
	allPods := make([]*corev1.Pod, 0, len(podList.Items))
	for k := range podList.Items {
		allPods = append(allPods, &podList.Items[k])
	}
	// Ignore inactive pods.
	filteredPods := controller.FilterActivePods(logger, allPods)

	// NOTE: filteredPods are pointing to objects from cache - if you need to
	// modify them, you need to copy it first.
	filteredPods, err = r.claimPods(ctx, ips, selector, filteredPods)
	if err != nil {
		return 0, err
	}

	requeueAfter, err = r.refreshPods(ips, filteredPods)
	klog.Infof("refreshPods requeueAfter : %v", requeueAfter)
	if err != nil {
		return 0, err
	}
	rsNeedsSync := int(*ips.Spec.Replicas) != len(filteredPods)

	var manageReplicasErr error
	if rsNeedsSync && ips.DeletionTimestamp == nil {
		manageReplicasErr = r.manageReplicas(ctx, filteredPods, ips)
	}
	if manageReplicasErr == nil && !r.DisableInplaceUpdate {
		requeueAfterT, manageInplaceUpdateErr := r.manageInplaceUpdate(filteredPods, ips)
		if requeueAfterT > 0 {
			requeueAfter = requeueAfterT
		}
		if manageInplaceUpdateErr != nil {
			klog.Errorf("inplace update err: %s", manageInplaceUpdateErr)
		}

		klog.Infof("manageInplaceUpdate requeueAfter  %v", requeueAfter)
	}

	ips = ips.DeepCopy()
	newStatus := calculateStatus(ips, filteredPods, manageReplicasErr)

	// Always updates status as pods come up or die.
	updatedRS, err := r.updateInplaceSetStatus(ips, newStatus)
	if err != nil {
		// Multiple things could lead to this update failing. Requeuing the replica set ensures
		// Returning an error causes a requeue without forcing a hotloop
		return 0, err
	}
	// Resync the ReplicaSet after MinReadySeconds as a last line of defense to guard against clock-skew.
	if manageReplicasErr == nil && updatedRS.Spec.MinReadySeconds > 0 &&
		updatedRS.Status.ReadyReplicas == *(updatedRS.Spec.Replicas) &&
		updatedRS.Status.AvailableReplicas != *(updatedRS.Spec.Replicas) {
		requeueAfter = time.Duration(updatedRS.Spec.MinReadySeconds) * time.Second

		klog.Infof("updatedRS.Status.AvailableReplicas != *(updatedRS.Spec.Replicas) requeueAfter : %v", requeueAfter)
	}

	klog.Infof("return requeueAfter: %s , manageReplicasErr: %v", requeueAfter, manageReplicasErr)
	return requeueAfter, manageReplicasErr
}

// slowStartBatch tries to call the provided function a total of 'count' times,
// starting slow to check for errors, then speeding up if calls succeed.
//
// It groups the calls into batches, starting with a group of initialBatchSize.
// Within each batch, it may call the function multiple times concurrently.
//
// If a whole batch succeeds, the next batch may get exponentially larger.
// If there are any failures in a batch, all remaining batches are skipped
// after waiting for the current batch to complete.
//
// It returns the number of successful calls to the function.
func slowStartBatch(count int, initialBatchSize int, fn func() error) (int, error) {
	remaining := count
	successes := 0
	for batchSize := integer.IntMin(remaining, initialBatchSize); batchSize > 0; batchSize = integer.IntMin(2*batchSize, remaining) {
		errCh := make(chan error, batchSize)
		var wg sync.WaitGroup
		wg.Add(batchSize)
		for i := 0; i < batchSize; i++ {
			go func() {
				defer wg.Done()
				if err := fn(); err != nil {
					errCh <- err
				}
			}()
		}
		wg.Wait()
		curSuccesses := batchSize - len(errCh)
		successes += curSuccesses
		if len(errCh) > 0 {
			return successes, <-errCh
		}
		remaining -= batchSize
	}
	return successes, nil
}

func (r *InplaceSetReconciler) claimPods(ctx context.Context, ips *v1beta1.InplaceSet, selector labels.Selector, filteredPods []*corev1.Pod) ([]*corev1.Pod, error) {
	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing Pods (see #42639).
	canAdoptFunc := controller.RecheckDeletionTimestamp(func(context.Context) (metav1.Object, error) {
		fresh, err := r.apiGet(ips.Namespace, ips.Name)
		if err != nil {
			return nil, err
		}
		if fresh.UID != ips.UID {
			return nil, fmt.Errorf("original %v/%v is gone: got uid %v, wanted %v", ips.Namespace, ips.Name, fresh.UID, ips.UID)
		}
		return fresh, nil
	})
	cm := controller.NewPodControllerRefManager(r.podControl, ips, selector, gvk, canAdoptFunc)
	return cm.ClaimPods(ctx, filteredPods)
}

// manageReplicas checks and updates replicas for the given ReplicaSet.
// Does NOT modify <filteredPods>.
// It will requeue the replica set in case of an error while creating/deleting pods.
func (r *InplaceSetReconciler) manageReplicas(ctx context.Context, filteredPods []*corev1.Pod, ips *v1beta1.InplaceSet) error {

	diff := len(filteredPods) - int(*(ips.Spec.Replicas))
	if diff < 0 {
		diff *= -1
		if diff > r.burstReplicas {
			diff = r.burstReplicas
		}
		// TODO: Track UIDs of creates just like deletes. The problem currently
		// is we'd need to wait on the result of a create to record the pod's
		// UID, which would require locking *across* the create, which will turn
		// into a performance bottleneck. We should generate a UID for the pod
		// beforehand and store it via ExpectCreations.
		klog.V(2).InfoS("Too few replicas", "replicaSet", klog.KObj(ips), "need", *(ips.Spec.Replicas), "creating", diff)
		// Batch the pod creates. Batch sizes start at SlowStartInitialBatchSize
		// and double with each successful iteration in a kind of "slow start".
		// This handles attempts to start large numbers of pods that would
		// likely all fail with the same error. For example a project with a
		// low quota that attempts to create a large number of pods will be
		// prevented from spamming the API service with the pod create requests
		// after one of its pods fails.  Conveniently, this also prevents the
		// event spam that those failures would generate.
		_, err := slowStartBatch(diff, controller.SlowStartInitialBatchSize, func() error {
			err := r.podControl.CreatePods(ctx, ips.Namespace, &ips.Spec.Template, ips, metav1.NewControllerRef(ips, gvk))
			if err != nil {
				klog.Infof("failed to create pods, ns: %s, spec: %#v, err: %s", ips.Namespace, &ips.Spec.Template, err)
				if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
					// if the namespace is being terminated, we don't have to do
					// anything because any creation will fail
					return nil
				}
			}
			return err
		})

		return err
	} else if diff > 0 {
		if diff > r.burstReplicas {
			diff = r.burstReplicas
		}
		klog.V(2).InfoS("Too many replicas", "replicaSet", klog.KObj(ips), "need", *(ips.Spec.Replicas), "deleting", diff)

		relatedPods, err := r.getIndirectlyRelatedPods(ips)
		utilruntime.HandleError(err)

		// Choose which Pods to delete, preferring those in earlier phases of startup.
		podsToDelete := getPodsToDelete(filteredPods, relatedPods, diff)

		errCh := make(chan error, diff)
		var wg sync.WaitGroup
		wg.Add(diff)
		for _, pod := range podsToDelete {
			go func(targetPod *corev1.Pod) {
				defer wg.Done()
				if err := r.podControl.DeletePod(ctx, ips.Namespace, targetPod.Name, ips); err != nil {
					// Decrement the expected number of deletes because the informer won't observe this deletion
					podKey := controller.PodKey(targetPod)
					if !apierrors.IsNotFound(err) {
						klog.V(2).Infof("Failed to delete, decremented expectations for %v %s/%s", podKey, ips.Namespace, ips.Name)
						errCh <- err
					}
				}
			}(pod)
		}
		wg.Wait()

		select {
		case err := <-errCh:
			// all errors have been reported before and they're likely to be the same, so we'll only return the first one we hit.
			if err != nil {
				return err
			}
		default:
		}
	}

	return nil
}

// getIndirectlyRelatedPods returns all pods that are owned by any ReplicaSet
// that is owned by the given ReplicaSet's owner.
func (r *InplaceSetReconciler) getIndirectlyRelatedPods(ips *v1beta1.InplaceSet) ([]*corev1.Pod, error) {
	var relatedPods []*corev1.Pod
	seen := make(map[types.UID]*v1beta1.InplaceSet)
	for _, relatedRS := range r.getInplaceSetsWithSameController(ips) {
		selector, err := metav1.LabelSelectorAsSelector(relatedRS.Spec.Selector)
		if err != nil {
			return nil, err
		}
		pods := &corev1.PodList{}
		err = r.List(context.TODO(), pods, &client.ListOptions{
			LabelSelector: selector,
			Namespace:     relatedRS.Namespace,
		})
		if err != nil {
			return nil, err
		}
		for k, pod := range pods.Items {
			if otherRS, found := seen[pod.UID]; found {
				klog.V(5).Infof("Pod %s/%s is owned by both %s/%s and %s/%s", pod.Namespace, pod.Name, otherRS.Namespace, otherRS.Name, relatedRS.Namespace, relatedRS.Name)
				continue
			}
			seen[pod.UID] = relatedRS
			relatedPods = append(relatedPods, &pods.Items[k])
		}
	}
	if klog.V(4).Enabled() {
		var relatedNames []string
		for _, related := range relatedPods {
			relatedNames = append(relatedNames, related.Name)
		}
		klog.Infof("Found %d related pods for %s/%s: %v", len(relatedPods), ips.Namespace, ips.Name, strings.Join(relatedNames, ", "))
	}
	return relatedPods, nil
}

func getPodsToDelete(filteredPods, relatedPods []*corev1.Pod, diff int) []*corev1.Pod {
	// No need to sort pods if we are about to delete all of them.
	// diff will always be <= len(filteredPods), so not need to handle > case.
	if diff < len(filteredPods) {
		podsWithRanks := getPodsRankedByRelatedPodsOnSameNode(filteredPods, relatedPods)
		sort.Sort(podsWithRanks)
		reportSortingDeletionAgeRatioMetric(filteredPods, diff)
	}
	return filteredPods[:diff]
}

func reportSortingDeletionAgeRatioMetric(filteredPods []*corev1.Pod, diff int) {
	// now := time.Now()
	youngestTime := time.Time{}
	// first we need to check all of the ready pods to get the youngest, as they may not necessarily be sorted by timestamp alone
	for _, pod := range filteredPods {
		if pod.CreationTimestamp.Time.After(youngestTime) && podutil.IsPodReady(pod) {
			youngestTime = pod.CreationTimestamp.Time
		}
	}

	// for each pod chosen for deletion, report the ratio of its age to the youngest pod's age
	for _, pod := range filteredPods[:diff] {
		if !podutil.IsPodReady(pod) {
			continue
		}
	}
}

// getPodsRankedByRelatedPodsOnSameNode returns an ActivePodsWithRanks value
// that wraps podsToRank and assigns each pod a rank equal to the number of
// active pods in relatedPods that are colocated on the same node with the pod.
// relatedPods generally should be a superset of podsToRank.
func getPodsRankedByRelatedPodsOnSameNode(podsToRank, relatedPods []*corev1.Pod) controller.ActivePodsWithRanks {
	podsOnNode := make(map[string]int)
	for _, pod := range relatedPods {
		if controller.IsPodActive(pod) {
			podsOnNode[pod.Spec.NodeName]++
		}
	}
	ranks := make([]int, len(podsToRank))
	for i, pod := range podsToRank {
		ranks[i] = podsOnNode[pod.Spec.NodeName]
	}
	return controller.ActivePodsWithRanks{Pods: podsToRank, Rank: ranks}
}

func calculateStatus(ips *v1beta1.InplaceSet, filteredPods []*corev1.Pod, manageReplicasErr error) v1beta1.InplaceSetStatus {
	newStatus := ips.Status
	// Count the number of pods that have labels matching the labels of the pod
	// template of the replica set, the matching pods may have more
	// labels than are in the template. Because the label of podTemplateSpec is
	// a superset of the selector of the replica set, so the possible
	// matching pods must be part of the filteredPods.
	fullyLabeledReplicasCount := 0
	readyReplicasCount := 0
	availableReplicasCount := 0
	templateLabel := labels.Set(ips.Spec.Template.Labels).AsSelectorPreValidated()
	for _, pod := range filteredPods {
		if templateLabel.Matches(labels.Set(pod.Labels)) {
			fullyLabeledReplicasCount++
		}
		if podutil.IsPodReady(pod) {
			readyReplicasCount++
			if podutil.IsPodAvailable(pod, ips.Spec.MinReadySeconds, metav1.Now()) {
				availableReplicasCount++
			}
		}
	}

	failureCond := GetCondition(ips.Status, appsv1.ReplicaSetReplicaFailure)
	if manageReplicasErr != nil && failureCond == nil {
		var reason string
		if diff := len(filteredPods) - int(*(ips.Spec.Replicas)); diff < 0 {
			reason = "FailedCreate"
		} else if diff > 0 {
			reason = "FailedDelete"
		}
		cond := NewReplicaSetCondition(appsv1.ReplicaSetReplicaFailure, corev1.ConditionTrue, reason, manageReplicasErr.Error())
		SetCondition(&newStatus, cond)
	} else if manageReplicasErr == nil && failureCond != nil {
		RemoveCondition(&newStatus, appsv1.ReplicaSetReplicaFailure)
	}

	newStatus.Replicas = int32(len(filteredPods))
	newStatus.FullyLabeledReplicas = int32(fullyLabeledReplicasCount)
	newStatus.ReadyReplicas = int32(readyReplicasCount)
	newStatus.AvailableReplicas = int32(availableReplicasCount)
	newStatus.InplaceUpdateStatus, _ = calcInplaceUpdateStatus(ips, filteredPods)
	return newStatus
}

// NewReplicaSetCondition creates a new replicaset condition.
func NewReplicaSetCondition(condType appsv1.ReplicaSetConditionType, status corev1.ConditionStatus, reason, msg string) appsv1.ReplicaSetCondition {
	return appsv1.ReplicaSetCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            msg,
	}
}

// GetCondition returns a replicaset condition with the provided type if it exists.
func GetCondition(status v1beta1.InplaceSetStatus, condType appsv1.ReplicaSetConditionType) *appsv1.ReplicaSetCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetCondition adds/replaces the given condition in the replicaset status. If the condition that we
// are about to add already exists and has the same status and reason then we are not going to update.
func SetCondition(status *v1beta1.InplaceSetStatus, condition appsv1.ReplicaSetCondition) {
	currentCond := GetCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// RemoveCondition removes the condition with the provided type from the replicaset status.
func RemoveCondition(status *v1beta1.InplaceSetStatus, condType appsv1.ReplicaSetConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

// filterOutCondition returns a new slice of replicaset conditions without conditions with the provided type.
func filterOutCondition(conditions []appsv1.ReplicaSetCondition, condType appsv1.ReplicaSetConditionType) []appsv1.ReplicaSetCondition {
	newConditions := []appsv1.ReplicaSetCondition{}
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

// updateInplaceSetStatus attempts to update the Status.Replicas of the given ReplicaSet, with a single GET/PUT retry.
func (r *InplaceSetReconciler) updateInplaceSetStatus(
	ips *v1beta1.InplaceSet,
	newStatus v1beta1.InplaceSetStatus) (*v1beta1.InplaceSet, error) {
	maredStatus, err := json.Marshal(newStatus)
	if err != nil {
		return nil, err
	}
	if ips.Annotations == nil {
		ips.Annotations = make(map[string]string)
	}
	ips.Annotations[utils.IpsAnnotationInplacesetStatus] = string(maredStatus)

	// This is the steady state. It happens when the ReplicaSet doesn't have any expectations, since
	// we do a periodic relist every 30s. If the generations differ but the replicas are
	// the same, a caller might've resized to the same replica count.
	if ips.Status.Replicas == newStatus.Replicas &&
		ips.Status.FullyLabeledReplicas == newStatus.FullyLabeledReplicas &&
		ips.Status.ReadyReplicas == newStatus.ReadyReplicas &&
		ips.Status.AvailableReplicas == newStatus.AvailableReplicas &&
		ips.Generation == ips.Status.ObservedGeneration &&
		reflect.DeepEqual(ips.Status.Conditions, newStatus.Conditions) &&
		(ips.Status.InplaceUpdateStatus == newStatus.InplaceUpdateStatus && newStatus.InplaceUpdateStatus == nil ||
			ips.Status.InplaceUpdateStatus != nil && newStatus.InplaceUpdateStatus != nil &&
				ips.Status.InplaceUpdateStatus.PodTemplateHash == newStatus.InplaceUpdateStatus.PodTemplateHash &&
				ips.Status.InplaceUpdateStatus.ReadyReplicas == newStatus.InplaceUpdateStatus.ReadyReplicas &&
				ips.Status.InplaceUpdateStatus.AvailableReplicas == newStatus.InplaceUpdateStatus.AvailableReplicas) {
		return ips, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = ips.Generation

	klog.V(4).Infof(fmt.Sprintf("Updating status for %s/%s, ", ips.Namespace, ips.Name) +
		fmt.Sprintf("replicas %d->%d (need %d), ", ips.Status.Replicas, newStatus.Replicas, *(ips.Spec.Replicas)) +
		fmt.Sprintf("fullyLabeledReplicas %d->%d, ", ips.Status.FullyLabeledReplicas, newStatus.FullyLabeledReplicas) +
		fmt.Sprintf("readyReplicas %d->%d, ", ips.Status.ReadyReplicas, newStatus.ReadyReplicas) +
		fmt.Sprintf("availableReplicas %d->%d, ", ips.Status.AvailableReplicas, newStatus.AvailableReplicas) +
		fmt.Sprintf("sequence No: %v->%v", ips.Status.ObservedGeneration, newStatus.ObservedGeneration))

	for i := 0; i < 2; i++ {
		ips.Status = newStatus
		err = r.Status().Update(context.TODO(), ips)
		if err == nil {
			return ips, nil
		}
		// Update the Inplaceset with the latest resource version for the next poll
		ips, err = r.apiGet(ips.Namespace, ips.Name)
		if err != nil {
			return nil, err
		}
	}

	return ips, err
}
