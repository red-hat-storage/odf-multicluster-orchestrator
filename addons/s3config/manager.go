package s3config

import (
	"context"
	"log/slog"
	"os"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"

	"github.com/go-logr/zapr"
	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/lease"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	mgrScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(mgrScheme))
	utilruntime.Must(multiclusterv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(corev1.AddToScheme(mgrScheme))
	utilruntime.Must(obv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(routev1.AddToScheme(mgrScheme))
}

func NewS3ConfigAddonAgentCommand() *cobra.Command {
	o := &S3ConfigAddonOptions{}

	cmd := &cobra.Command{
		Use:   "s3config-addon",
		Short: "Start the S3Configuration addon agent",
		Run: func(cmd *cobra.Command, args []string) {
			o.RunAgent(cmd.Context())
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// S3ConfigAddonOptions defines the flags for the S3Config addon agent
type S3ConfigAddonOptions struct {
	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	HubKubeconfigFile    string
	KubeconfigFile       string
	SpokeClusterName     string
	InstallNamespace     string
	DevMode              bool
}

func (o *S3ConfigAddonOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flags.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager.")
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.KubeconfigFile, "kubeconfig", "", "Paths to a kubeconfig. Only required if out-of-cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.InstallNamespace, "install-namespace", "openshift-storage", "Namespace where the addon is installed and where to watch resources.")
	flags.BoolVar(&o.DevMode, "dev", false, "Set to true for dev environment (Text logging)")
}

// RunAgent starts the S3Config addon agent
func (o *S3ConfigAddonOptions) RunAgent(ctx context.Context) {
	zapLogger := utils.GetZapLogger(o.DevMode)
	defer func() {
		if err := zapLogger.Sync(); err != nil {
			zapLogger.Error("Failed to sync zap logger")
		}
	}()
	ctrl.SetLogger(zapr.NewLogger(zapLogger))
	logger := utils.GetLogger(zapLogger)
	logger.Info("Starting S3Config addon agent", "Version", version.Version)

	logger.Info("Starting spoke manager for S3Config addon")
	runSpokeManager(ctx, *o, logger)

	logger.Info("S3Config addon agent is running, waiting for context cancellation")
	<-ctx.Done()
	logger.Info("S3Config addon agent has stopped")
}

func runSpokeManager(ctx context.Context, options S3ConfigAddonOptions, logger *slog.Logger) {
	spokeKubeConfig, err := utils.GetClientConfig(options.KubeconfigFile)
	if err != nil {
		logger.Error("Failed to get kubeconfig", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(spokeKubeConfig, ctrl.Options{
		Scheme: mgrScheme,
		Metrics: server.Options{
			BindAddress: "0", // disable metrics
		},
		HealthProbeBindAddress: "0", // disable health probe
		ReadinessEndpointName:  "0", // disable readiness probe
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				options.InstallNamespace: {},
			},
		},
	})

	if err != nil {
		logger.Error("Failed to start manager", "error", err)
		os.Exit(1)
	}

	// Create kubernetes clientset for lease updater
	spokeKubeClient, err := kubernetes.NewForConfig(spokeKubeConfig)
	if err != nil {
		logger.Error("Failed to get spoke kube client", "error", err)
		os.Exit(1)
	}

	// Add lease updater for health reporting
	if err = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		logger.Info("Starting lease updater for s3config addon")
		leaseUpdater := lease.NewLeaseUpdater(
			spokeKubeClient,
			utils.S3ConfigAddonName, // addon name (must match ManagedClusterAddOn name)
			options.InstallNamespace,
		)
		leaseUpdater.Start(ctx)
		<-ctx.Done()
		return nil
	})); err != nil {
		logger.Error("Failed to start lease updater", "error", err)
		os.Exit(1)
	}

	hubConfig, err := utils.GetClientConfig(options.HubKubeconfigFile)
	if err != nil {
		logger.Error("Failed to get hub kubeconfig", "error", err)
		os.Exit(1)
	}

	hubClient, err := utils.GetClientFromConfig(hubConfig, mgr.GetScheme())
	if err != nil {
		logger.Error("Failed to get hub client", "error", err)
		os.Exit(1)
	}

	// Only start the S3Config addon reconciler
	if err = (&S3ConfigAddonReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        hubClient,
		SpokeClient:      mgr.GetClient(),
		SpokeClusterName: options.SpokeClusterName,
		Logger:           logger.With("controller", "S3ConfigAddonReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create S3ConfigAddon controller", "controller", "S3ConfigAddon", "error", err)
		os.Exit(1)
	}

	logger.Info("Starting spoke controller manager for S3Config addon")
	if err := mgr.Start(ctx); err != nil {
		logger.Error("Problem running spoke controller manager", "error", err)
		os.Exit(1)
	}
}
