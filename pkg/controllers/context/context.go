package context

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"

	"extendeddeployment.io/extended-deployment/cmd/app/options"
	"extendeddeployment.io/extended-deployment/pkg/utils/informermanager"
)

// Context defines the context object for controller.
type Context struct {
	Opts options.Options

	Mgr                         controllerruntime.Manager
	StopChan                    <-chan struct{}
	DynamicClientSet            dynamic.Interface
	ControlPlaneInformerManager informermanager.SingleClusterInformerManager
	Informer                    informers.SharedInformerFactory
	ClientSet                   clientset.Interface
	InplacesetConcurrency       int
}

// InitFunc is used to launch a particular controller.
// Any error returned will cause the controller process to `Fatal`
// The bool indicates whether the controller was enabled.
type InitFunc func(ctx Context) (enabled bool, err error)

// Initializers is a public map of named controller groups
type Initializers map[string]InitFunc

// ControllerNames returns all known controller names
func (i Initializers) ControllerNames() []string {
	return sets.StringKeySet(i).List()
}

// StartControllers starts a set of controllers with a specified ControllerContext
func (i Initializers) StartControllers(ctx Context) error {
	for controllerName, initFn := range i {
		klog.V(1).Infof("Starting %q", controllerName)
		started, err := initFn(ctx)
		if err != nil {
			klog.Errorf("Error starting %q", controllerName)
			return err
		}
		if !started {
			klog.Warningf("Skipping %q", controllerName)
			continue
		}
		klog.Infof("Started %q", controllerName)
	}
	return nil
}
