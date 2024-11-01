package utils

import (
	"encoding/json"
	"strconv"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"
)

const (
	// InplaceSet annotation
	IpsAnnotationRegionName           = "cib.inplaceset.io/region-name"
	IpsAnnotationTemplateHash         = "cib.inplaceset.io/template-hash"
	IpsAnnotationInplacesetUpdateSpec = "cib.inplaceset.io/inplaceset-update-spec"
	IpsAnnotationInplacesetStatus     = "cib.inplaceset.io/inplaceset-status"
	IpsAnnotationConfigHash           = "cib.inplaceset.io/config-hash"
	IpsAnnotationRollTerm             = "cib.inplaceset.io/roll-term"           // Rolling group number
	IpsAnnotationFailedOldReplicas    = "cib.inplaceset.io/failed-old-replicas" // Historical replica count when failure occurs
	IpsAnnotationDesiredReplicas      = "cib.inplaceset.io/desired-replicas"
	IpsAnnotationRevision             = "cib.inplaceset.io/revision"
	IpsAnnotationRegionFailed         = "cib.inplaceset.io/region-failed"
)

// InplaceSetUpdateSpec spec for controling inplace update, set in inplaceset's annotations
type InplaceSetUpdateSpec struct {
	// genereated by inplaceset spec
	NewImages map[string]string `json:"-"`
	// controling how many pods will be inplace updated
	UpdatePodNum int `json:"updatePodNum"`
	// update generation
	PodTemplateHash string `json:"podTemplateHash"`
}

func GetInplaceSetUpdateSpec(annotations map[string]string) (*InplaceSetUpdateSpec, bool, error) {
	if annotations == nil {
		return nil, false, nil
	}
	specStr, ok := annotations[IpsAnnotationInplacesetUpdateSpec]
	if !ok {
		return nil, false, nil
	}
	spec := &InplaceSetUpdateSpec{}
	if err := json.Unmarshal([]byte(specStr), spec); err != nil {
		return nil, false, err
	}
	return spec, true, nil
}

func SetInplacesetUpdateSpec(annotations map[string]string, spec *InplaceSetUpdateSpec) {
	b, _ := json.Marshal(spec)
	annotations[IpsAnnotationInplacesetUpdateSpec] = string(b)
}

func DelInplacesetUpdateSpec(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	if _, ok := annotations[IpsAnnotationInplacesetUpdateSpec]; ok {
		delete(annotations, IpsAnnotationInplacesetUpdateSpec)
		return true
	}
	return false
}

func GetStrAnnotationByKey(obj interface{}, key string) (v string, exists bool) {
	metaInfo, err := meta.Accessor(obj)
	if err != nil {
		klog.Errorf("object has no meta: %v", err)
		return "", false
	}
	anno := metaInfo.GetAnnotations()
	if anno == nil {
		return "", false
	}
	s, exists := anno[key]
	return s, exists
}

func GetInt64AnnotationByKey(obj interface{}, key string) (v int64, exists bool) {
	s, exists := GetStrAnnotationByKey(obj, key)
	if exists {
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			klog.Errorf("annotation convert to int64 error: %v", err)
			return 0, false
		}
		v = val
	}
	return v, exists
}

func DelAnnotations(annotation map[string]string, keys []string) (update bool) {
	if annotation == nil {
		return false
	}
	update = false
	for i := 0; i < len(keys); i++ {
		_, ok := annotation[keys[i]]
		if ok {
			delete(annotation, keys[i])
			update = true
		}
	}
	return update
}
