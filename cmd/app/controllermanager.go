package app

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strconv"

	"github.com/spf13/cobra"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"extendeddeployment.io/extended-deployment/cmd/app/options"
	controllerscontext "extendeddeployment.io/extended-deployment/pkg/controllers/context"
	"extendeddeployment.io/extended-deployment/pkg/controllers/inplaceset"
	"extendeddeployment.io/extended-deployment/pkg/sharedcli/klogflag"
	"extendeddeployment.io/extended-deployment/pkg/version"

	"extendeddeployment.io/extended-deployment/pkg/utils/gclient"
	"extendeddeployment.io/extended-deployment/pkg/utils/informermanager"
)

func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	opts := options.NewOptions()

	cmd := &cobra.Command{
		Use:     "controller-manager",
		Long:    `controller manager runs a bunch of controllers`,
		Version: version.Version(),
		RunE: func(cmd *cobra.Command, args []string) error {
			// validate options
			if errs := opts.Validate(); len(errs) != 0 {
				return errs.ToAggregate()
			}
			return Run(ctx, opts)
		},
	}
	fss := cliflag.NamedFlagSets{}
	genericFlagSet := fss.FlagSet("generic")
	genericFlagSet.AddGoFlagSet(flag.CommandLine)
	genericFlagSet.Lookup("kubeconfig").Usage = "control plane kubeconfig"
	opts.AddFlags(genericFlagSet)

	// Set klog flags
	logsFlagSet := fss.FlagSet("logs")
	klogflag.Add(logsFlagSet)

	// logs 先于 generic
	// 因为 generic 中有 klog-v1 的参数解析，只有先 add logs 才能使得 klog/v2 生效
	cmd.Flags().AddFlagSet(logsFlagSet)
	cmd.Flags().AddFlagSet(genericFlagSet)

	return cmd
}

func Run(ctx context.Context, opts *options.Options) error {
	config, err := controllerruntime.GetConfig()
	if err != nil {
		panic(err)
	}
	config.QPS, config.Burst = opts.KubeAPIQPS, opts.KubeAPIBurst
	controllerManager, err := controllerruntime.NewManager(config, controllerruntime.Options{
		Scheme: gclient.NewSchema(),
		// SyncPeriod:              &opts.ResyncPeriod.Duration,
		HealthProbeBindAddress:  net.JoinHostPort(opts.BindAddress, strconv.Itoa(opts.SecurePort)),
		LivenessEndpointName:    "/healthz",
		Logger:                  log.FromContext(ctx),
		LeaderElection:          opts.LeaderElection,
		LeaderElectionNamespace: opts.LeaderElectionNamespace,
		LeaderElectionID:        opts.LeaderElectionID,
		LeaderElectionConfig:    config,
		LeaseDuration:           &opts.LeaseDuration,
		RenewDeadline:           &opts.RenewDeadline,
		RetryPeriod:             &opts.RetryPeriod,
	})

	if err != nil {
		klog.Errorf("failed to build controller manager: %v", err)
		return err
	}

	if err := controllerManager.AddHealthzCheck("ping", healthz.Ping); err != nil {
		klog.Errorf("failed to add health check endpoint: %v", err)
		return err
	}
	k8sClient, err := clientset.NewForConfig(controllerManager.GetConfig())
	if err != nil {
		klog.Fatalf("failed to start clientset: %v", err)
	}
	setupControllers(k8sClient, controllerManager, opts, ctx.Done())

	// // webhook
	// if !opts.DisableWebhook {
	// 	if err := controllerManager.Add(admission_webhook.NewHookServer(opts.CertsDir, opts.WebhookPort)); err != nil {
	// 		klog.Errorf(`add webhook server`)
	// 		return err
	// 	}
	// }

	// pprof
	if opts.PprofPort != 0 {
		go func() {
			defer func() {
				if err := recover(); err != nil {
					klog.Error(err)
				}
			}()
			if err := http.ListenAndServe(fmt.Sprintf(":%d", opts.PprofPort), nil); err != nil {
				klog.Error(err)
			}
		}()
	}

	// blocks until the context is done.
	if err := controllerManager.Start(ctx); err != nil {
		klog.Errorf("controller manager exits unexpectedly: %v", err)
		return err
	}
	// never reach here
	return nil
}

var controllers = make(controllerscontext.Initializers)

func init() {
	controllers["inplaceset"] = startInplaceSetController
}

func startInplaceSetController(ctx controllerscontext.Context) (enabled bool, err error) {
	mgr := ctx.Mgr
	ctrl := inplaceset.NewInplaceSetReconciler(
		ctx.Opts.DisableInplaceUpdate,
		mgr.GetAPIReader(), mgr.GetClient(), mgr.GetScheme(), ctx.ClientSet, 50, mgr.GetEventRecorderFor(inplaceset.ControllerName))

	if err := ctrl.SetupWithManager(mgr); err != nil {
		return false, err
	}
	return true, nil
}

func setupControllers(client clientset.Interface, mgr controllerruntime.Manager, opts *options.Options, stopChan <-chan struct{}) {
	restConfig := mgr.GetConfig()
	dynamicClientSet := dynamic.NewForConfigOrDie(restConfig)

	sharedInformer := informers.NewSharedInformerFactory(client, 0)
	controlPlaneInformerManager := informermanager.NewSingleClusterInformerManager(dynamicClientSet, 0, stopChan)
	controllerContext := controllerscontext.Context{
		Opts: *opts,

		Mgr:                         mgr,
		StopChan:                    stopChan,
		DynamicClientSet:            dynamicClientSet,
		ControlPlaneInformerManager: controlPlaneInformerManager,
		ClientSet:                   client,
		InplacesetConcurrency:       opts.InplaceWorkSyncs,
		Informer:                    sharedInformer,
	}

	if err := controllers.StartControllers(controllerContext); err != nil {
		klog.Fatalf("error starting controllers: %v", err)
	}
	//block until informer sync complete
	sharedInformer.Start(stopChan)
	sharedInformer.WaitForCacheSync(stopChan)
	go func() {
		<-stopChan
		informermanager.StopInstance()
	}()
}