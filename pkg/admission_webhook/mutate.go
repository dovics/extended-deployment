package admission_webhook

import (
	"context"
	"encoding/json"
	"fmt"

	adv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/dovics/extendeddeployment/api/v1beta1"
)

type CrInfo struct {
	obj runtime.Object
}

type MutateHandler struct {
	client  client.Client
	decoder admission.Decoder

	crs map[string]CrInfo
}

func NewMutateHandler() *MutateHandler {
	h := &MutateHandler{
		crs: map[string]CrInfo{},
	}

	h.addCrInfo(v1beta1.ExtendedDeploymentGVK, &v1beta1.ExtendedDeployment{})
	h.addCrInfo(v1beta1.InplacesetGVK, &v1beta1.InplaceSet{})

	return h
}

func (h *MutateHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	defer func() {
		err := recover()
		if err != nil {
			resp = admission.Denied(fmt.Sprintf("mutate error: %v", err))
		}
	}()

	// decode
	crInfo, ok := h.crs[req.Kind.String()]
	if !ok {
		return admission.Denied("unknown type")
	}

	obj := crInfo.obj.DeepCopyObject()
	if err := h.decoder.DecodeRaw(req.Object, obj); err != nil {
		return admission.Denied(fmt.Sprintf("object can not be decoded: %s", err))
	}

	// validate
	if errs := validator.Validate(obj.DeepCopyObject()); len(errs) > 0 {
		return admission.Denied(errors.NewAggregate(errs).Error())
	}

	// create operation, set default values
	if req.Operation == adv1.Create {
		defaulter.SetDefault(obj)

		if req.Kind.Kind == "ExtendedDeployment" {
			if errs := compareReplicas(obj.DeepCopyObject().(*v1beta1.ExtendedDeployment)); len(errs) > 0 {
				return admission.Denied(errors.NewAggregate(errs).Error())
			}
		}

		current, _ := json.Marshal(obj)
		return admission.PatchResponseFromRaw(req.Object.Raw, current)
	}

	if req.Operation == adv1.Update {
		oldObj := crInfo.obj.DeepCopyObject()
		if err := h.decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
			return admission.Denied(fmt.Sprintf("object can not be decoded: %s", err))
		}

		if errs := comparator.Compare(obj.DeepCopyObject(), oldObj.DeepCopyObject()); len(errs) > 0 {
			return admission.Denied(errors.NewAggregate(errs).Error())
		}
	}

	return admission.Allowed("")
}

func (h *MutateHandler) InjectClient(c client.Client) error {
	h.client = c
	return nil
}

func (h *MutateHandler) InjectDecoder(d admission.Decoder) error {
	h.decoder = d
	return nil
}

func (h *MutateHandler) addCrInfo(gvk schema.GroupVersionKind, obj runtime.Object) {
	h.crs[gvk.String()] = CrInfo{
		obj: obj.DeepCopyObject(),
	}
}
