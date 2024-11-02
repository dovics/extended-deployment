package deployregion

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type podEventHandler struct {
}

func (p *podEventHandler) addToQueue(obj metav1.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	req := ctrl.Request{}
	req.Name = obj.GetName()
	req.Namespace = PrefixPodType + obj.GetNamespace()
	q.AddRateLimited(req)
}

// Create is called in response to an create event - e.g. Pod Creation.
func (p *podEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	p.addToQueue(e.Object, q)
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (p *podEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	p.addToQueue(e.ObjectNew, q)
	if e.ObjectNew.GetUID() != e.ObjectOld.GetUID() {
		p.addToQueue(e.ObjectOld, q)
	}
}

// Delete is called in response to a delete event - e.g. Pod Deleted.
func (p *podEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	p.addToQueue(e.Object, q)
}

// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
func (p *podEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	p.addToQueue(e.Object, q)
}
