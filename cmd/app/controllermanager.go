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
package app

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	zaplogfmt "github.com/sykesm/zap-logfmt"
	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/dovics/extendeddeployment/cmd/app/options"
	controllerscontext "github.com/dovics/extendeddeployment/pkg/controllers/context"
	"github.com/dovics/extendeddeployment/pkg/controllers/deployregion"
	"github.com/dovics/extendeddeployment/pkg/controllers/extendeddeployment"
	"github.com/dovics/extendeddeployment/pkg/controllers/inplaceset"
	"github.com/dovics/extendeddeployment/pkg/controllers/reschedule"
	"github.com/dovics/extendeddeployment/pkg/sharedcli/klogflag"
	"github.com/dovics/extendeddeployment/pkg/utils/gclient"
	"github.com/dovics/extendeddeployment/pkg/utils/informermanager"
	"github.com/dovics/extendeddeployment/pkg/version"
	webhook "github.com/dovics/extendeddeployment/pkg/webhook/v1beta1"
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

	// Initialize a logger for the controller runtime
	leveler := uzap.LevelEnablerFunc(func(level zapcore.Level) bool {
		// Set the level fairly high since it's so verbose
		return level >= zapcore.DPanicLevel
	})
	stackTraceLeveler := uzap.LevelEnablerFunc(func(level zapcore.Level) bool {
		// Attempt to suppress the stack traces in the logs since they are so verbose.
		// The controller runtime seems to ignore this since the stack is still always printed.
		return false
	})
	logfmtEncoder := zaplogfmt.NewEncoder(uzap.NewProductionEncoderConfig())
	logger := zap.New(
		zap.Level(leveler),
		zap.StacktraceLevel(stackTraceLeveler),
		zap.UseDevMode(false),
		zap.WriteTo(os.Stdout),
		zap.Encoder(logfmtEncoder))
	log.SetLogger(logger)

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

	// webhook
	if !opts.DisableWebhook {
		if err := webhook.SetupExtendedDeploymentWebhookWithManager(controllerManager); err != nil {
			klog.Fatalf("failed to add deployment webhook server: %v", err)
		}

		if err := webhook.SetupInplaceSetWebhookWithManager(controllerManager); err != nil {
			klog.Fatalf("failed to add inplaceset webhook server: %v", err)
		}
	}

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
	controllers["exteneddeployment"] = startExtendedDeploymentController
	controllers["placeset"] = startPlacesetController
	controllers["deployregion"] = startDeployRegionController
	controllers["reschedule"] = startRescheduleController
}

func startExtendedDeploymentController(ctx controllerscontext.Context) (enabled bool, err error) {
	mgr := ctx.Mgr

	clusterController := &extendeddeployment.ExtendedDeploymentReconciler{
		Client:               mgr.GetClient(),
		KubeClient:           ctx.ClientSet,
		Scheme:               mgr.GetScheme(),
		EventRecorder:        mgr.GetEventRecorderFor(extendeddeployment.ControllerName),
		DisableInplaceUpdate: ctx.Opts.DisableInplaceUpdate,
	}
	if err := clusterController.Setup(ctx.Informer); err != nil {
		return false, err
	}

	if err := clusterController.SetupWithManager(mgr); err != nil {
		return false, err
	}
	return true, nil
}

func startDeployRegionController(ctx controllerscontext.Context) (enabled bool, err error) {
	mgr := ctx.Mgr
	controller := &deployregion.DeployRegionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	if err := controller.Setup(ctx.StopChan, ctx.Informer); err != nil {
		return false, err
	}

	if err := controller.SetupWithManager(mgr); err != nil {
		return false, err
	}

	return true, nil
}

func startPlacesetController(ctx controllerscontext.Context) (enabled bool, err error) {
	mgr := ctx.Mgr
	ctrl := inplaceset.NewInplaceSetReconciler(
		ctx.Opts.DisableInplaceUpdate,
		mgr.GetAPIReader(),
		mgr.GetClient(),
		mgr.GetScheme(),
		ctx.ClientSet,
		50,
		mgr.GetEventRecorderFor(inplaceset.ControllerName),
	)

	if err := ctrl.SetupWithManager(mgr); err != nil {
		return false, err
	}
	return true, nil
}

func startRescheduleController(ctx controllerscontext.Context) (enabled bool, err error) {
	mgr := ctx.Mgr
	controller := &reschedule.RescheduleReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: mgr.GetEventRecorderFor(reschedule.ControllerName),
	}

	if err := controller.Setup(ctx.StopChan, ctx.Informer); err != nil {
		return false, err
	}

	if err := controller.SetupWithManager(mgr); err != nil {
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
	// block until informer sync complete
	sharedInformer.Start(stopChan)
	sharedInformer.WaitForCacheSync(stopChan)
	go func() {
		<-stopChan
		informermanager.StopInstance()
	}()
}
