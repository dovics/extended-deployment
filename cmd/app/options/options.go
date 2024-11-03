package options

import (
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/dovics/extendeddeployment/pkg/sharedcli/ratelimiterflag"
)

const (
	defaultBindAddress = "0.0.0.0"
	defaultPort        = 10357

	defaultLeaseDuration = 15 * time.Second
	defaultRenewDeadline = 10 * time.Second
	defaultRetryPeriod   = 2 * time.Second

	defaultLeaseID        = "exteneddeployment"
	defaultLeaseNamespace = "extened-system"
)

// Options contains everything necessary to create and run controller-manager.
type Options struct {
	// KubeAPIQPS is the QPS to use while talking with apiserver.
	KubeAPIQPS float32
	// KubeAPIBurst is the burst to allow while talking with apiserver.
	KubeAPIBurst int
	// ResyncPeriod is the base frequency the informers are resynced.
	// Defaults to 0, which means the created informer will never do resyncs.
	ResyncPeriod metav1.Duration
	// BindAddress is the IP address on which to listen for the --secure-port port.
	BindAddress string
	// SecurePort is the port that the the server serves at.
	// Note: We hope support https in the future once controller-runtime provides the functionality.
	SecurePort int
	//set Propagation 并发数
	PropagationWorkSyncs int

	// LeaderElection determines whether or not to use leader election when
	// starting the manager.
	LeaderElection bool
	// LeaderElectionNamespace determines the namespace in which the leader
	// election configmap will be created.
	LeaderElectionNamespace string
	// LeaderElectionID determines the name of the configmap that leader election
	// will use for holding the leader lock.
	LeaderElectionID string
	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack. Default is 15 seconds.
	LeaseDuration time.Duration
	// RenewDeadline is the duration that the acting controlplane will retry
	// refreshing leadership before giving up. Default is 10 seconds.
	RenewDeadline time.Duration
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions. Default is 2 seconds.
	RetryPeriod time.Duration

	// SkippedPropagatingNamespaces is a list of namespaces that will be skipped for propagating.
	SkippedPropagatingNamespaces []string

	// InplaceWorkSyncs is the number of resource templates that are allowed to sync concurrently.
	InplaceWorkSyncs int

	RateLimiterOpts ratelimiterflag.Options

	// admission webhook's config
	CertsDir       string
	WebhookPort    int
	DisableWebhook bool

	PprofPort int

	DisableInplaceUpdate bool
}

type CertsConfig struct {
	ClientCaFile, TlsCertFile, TlsPrivateKey string
}

// NewOptions builds an empty options.
func NewOptions() *Options {
	return &Options{}
}

// AddFlags adds flags to the specified FlagSet.
func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.Float32Var(&o.KubeAPIQPS, "kube-api-qps", 40.0, "QPS to use while talking with karmada-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.IntVar(&o.KubeAPIBurst, "kube-api-burst", 60, "Burst to use while talking with karmada-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.DurationVar(&o.ResyncPeriod.Duration, "resync-period", 0, "Base frequency the informers are resynced.")
	flags.StringVar(&o.BindAddress, "bind-address", defaultBindAddress,
		"The IP address on which to listen for the --secure-port port.")
	flags.IntVar(&o.SecurePort, "secure-port", defaultPort,
		"The secure port on which to serve HTTPS.")
	flags.StringSliceVar(&o.SkippedPropagatingNamespaces, "skipped-propagating-namespaces", []string{},
		"Comma-separated namespaces that should be skipped from propagating in addition to the default skipped namespaces(karmada-system, karmada-cluster, namespaces prefixed by kube- and karmada-es-).")
	flags.IntVar(&o.InplaceWorkSyncs, "inplaceset-resource-template-syncs", 5, "The number of resource templates that are allowed to sync concurrently.")
	//o.RateLimiterOpts.AddFlags(flags)

	flags.BoolVar(&o.LeaderElection, "leader-elect", false, ""+
		"Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated "+
		"components for high availability.")
	flags.DurationVar(&o.LeaseDuration, "leader-elect-lease-duration", defaultLeaseDuration, ""+
		"The duration that non-leader candidates will wait after observing a leadership "+
		"renewal until attempting to acquire leadership of a led but unrenewed leader "+
		"slot. This is effectively the maximum duration that a leader can be stopped "+
		"before it is replaced by another candidate. This is only applicable if leader "+
		"election is enabled.")
	flags.DurationVar(&o.RenewDeadline, "leader-elect-renew-deadline", defaultRenewDeadline, ""+
		"The interval between attempts by the acting master to renew a leadership slot "+
		"before it stops leading. This must be less than or equal to the lease duration. "+
		"This is only applicable if leader election is enabled.")
	flags.DurationVar(&o.RetryPeriod, "leader-elect-retry-period", defaultRetryPeriod, ""+
		"The duration the clients should wait between attempting acquisition and renewal "+
		"of a leadership. This is only applicable if leader election is enabled.")

	flags.StringVar(&o.LeaderElectionID, "leader-elect-lease-id", defaultLeaseID, ""+
		"The name of lease object that is used for locking during "+
		"leader election.")
	flags.StringVar(&o.LeaderElectionNamespace, "leader-elect-lease-namespace", defaultLeaseNamespace, ""+
		"The namespace of lease object that is used for locking during "+
		"leader election.")

	flags.StringVar(&o.CertsDir, "certs-dir", "/etc/tls-certs", "dir certs file located in")

	flags.IntVar(&o.WebhookPort, "webhook-port", 8000, "Server Port for Webhook")
	flags.BoolVar(&o.DisableWebhook, "disable-webhook", false, "Disable the webhook component")
	flags.IntVar(&o.PprofPort, `pprof-port`, 0, `http port for pprof`)

	flags.BoolVar(&o.DisableInplaceUpdate, "disable-inplace-update", true, "Disable inplace update")
}

// Validate checks Options and return a slice of found errs.
func (o *Options) Validate() field.ErrorList {
	errs := field.ErrorList{}
	return errs
}
