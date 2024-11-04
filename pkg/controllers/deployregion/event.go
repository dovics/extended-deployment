package deployregion

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type baseEventHandler struct {
}

func (h *baseEventHandler) addToQueue(obj metav1.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	req := ctrl.Request{}
	req.Name = obj.GetName()
	req.Namespace = PrefixPodType + obj.GetNamespace()
	q.AddRateLimited(req)
}

// Create is called in response to an create event - e.g. Pod Creation.
func (h *baseEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.addToQueue(e.Object, q)
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (h *baseEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.addToQueue(e.ObjectNew, q)
	if e.ObjectNew.GetUID() != e.ObjectOld.GetUID() {
		h.addToQueue(e.ObjectOld, q)
	}
}

// Delete is called in response to a delete event - e.g. Pod Deleted.
func (h *baseEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.addToQueue(e.Object, q)
}

// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
func (h *baseEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.addToQueue(e.Object, q)
}