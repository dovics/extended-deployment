package hash

import (
	"fmt"
	"hash"
	"hash/fnv"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	"github.com/kr/pretty"
)

// DeepHashObject writes specified object to hash using the pretty library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()
	if _, err := pretty.Fprintf(hasher, "%# v", objectToWrite); err != nil {
		klog.Errorf("pretty.Fprintf failed, error: %v", err)
	}
}

func HashObject(obj interface{}) string {
	hasher := fnv.New32a()
	DeepHashObject(hasher, obj)
	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
}
