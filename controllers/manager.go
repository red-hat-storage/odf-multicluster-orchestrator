package controllers

import (
	"context"
	"crypto/tls"
	"os"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/go-logr/zapr"
	consolev1 "github.com/openshift/api/console/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/console"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"
	"github.com/spf13/cobra"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsubapis "open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	mgrScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(mgrScheme))
	utilruntime.Must(clusterv1.AddToScheme(mgrScheme))

	utilruntime.Must(multiclusterv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(addonapiv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(consolev1.AddToScheme(mgrScheme))

	utilruntime.Must(ramenv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(workv1.AddToScheme(mgrScheme))
	utilruntime.Must(viewv1beta1.AddToScheme(mgrScheme))
	utilruntime.Must(placementv1beta1.AddToScheme(mgrScheme))
	utilruntime.Must(argov1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(appsubapis.AddToScheme(mgrScheme))
	// +kubebuilder:scaffold:scheme
}

type ManagerOptions struct {
	MetricsAddr             string
	SecureMetrics           bool
	EnableLeaderElection    bool
	ProbeAddr               string
	MulticlusterConsolePort int
	DevMode                 bool
	KubeconfigFile          string

	testEnvFile string
}

func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

func (o *ManagerOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.MetricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flags.BoolVar(&o.SecureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flags.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.IntVar(&o.MulticlusterConsolePort, "multicluster-console-port", 9001, "The port where the multicluster console server will be serving its payload")
	flags.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flags.BoolVar(&o.DevMode, "dev", false, "Set to true for dev environment (Text logging)")
	flags.StringVar(&o.KubeconfigFile, "kubeconfig", "", "Paths to a kubeconfig. Only required if out-of-cluster.")
	flags.StringVar(&o.testEnvFile, "test-dotenv", "", "Path to a dotenv file for testing purpose only.")
}

func NewManagerCommand() *cobra.Command {
	mgrOpts := NewManagerOptions()
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Multicluster Orchestrator for ODF",
		Run: func(cmd *cobra.Command, args []string) {
			mgrOpts.runManager(cmd.Context())
		},
	}
	mgrOpts.AddFlags(cmd)
	return cmd
}

func (o *ManagerOptions) runManager(ctx context.Context) {
	zapLogger := utils.GetZapLogger(o.DevMode)
	defer func() {
		if err := zapLogger.Sync(); err != nil {
			zapLogger.Error("Failed to sync zap logger")
		}
	}()
	ctrl.SetLogger(zapr.NewLogger(zapLogger))
	logger := utils.GetLogger(zapLogger)

	logger.Info("Starting manager on hub", "version", version.Version)

	currentNamespace := utils.GetEnv("POD_NAMESPACE", o.testEnvFile)

	config, err := utils.GetClientConfig(o.KubeconfigFile)
	if err != nil {
		logger.Error("Failed to get kubeconfig", "error", err)
		os.Exit(1)
	}

	var tlsOpts []func(*tls.Config)
	metricsServerOptions := metricsserver.Options{
		BindAddress:   o.MetricsAddr,
		SecureServing: o.SecureMetrics,
		TLSOpts:       tlsOpts,
	}

	if o.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint.
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 mgrScheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: o.ProbeAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       "1d19c724.odf.openshift.io",
	})
	if err != nil {
		logger.Error("Failed to start manager", "error", err)
		os.Exit(1)
	}

	if err = (&MirrorPeerReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           logger.With("controller", "MirrorPeerReconciler"),
		testEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create MirrorPeer controller", "error", err)
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err = (&ManagedClusterReconciler{
		Client:           mgr.GetClient(),
		Logger:           logger.With("controller", "ManagedClusterReconciler"),
		testEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ManagedCluster controller", "error", err)
		os.Exit(1)
	}

	if err = (&ManagedClusterViewReconciler{
		Client:           mgr.GetClient(),
		Logger:           logger.With("controller", "ManagedClusterViewReconciler"),
		testEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ManagedClusterView controller", "error", err)
		os.Exit(1)
	}

	if err = (&DRPlacementControlReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: logger.With("controller", "DRPlacementControlReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create DRPlacementControl controller", "error", err)
		os.Exit(1)
	}

	if err = (&ProtectedApplicationViewReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: logger.With("controller", "ProtectedApplicationViewReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ProtectedApplicationView controller", "error", err)
		os.Exit(1)
	}

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		err = console.InitConsole(ctx, mgr.GetClient(), o.MulticlusterConsolePort, currentNamespace)
		if err != nil {
			logger.Error("Failed to initialize multicluster console to manager", "error", err)
			return err
		}
		return nil
	})); err != nil {
		logger.Error("Failed to add multicluster console to manager", "error", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error("Failed to set up health check", "error", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error("Failed to set up ready check", "error", err)
		os.Exit(1)
	}

	logger.Info("Initializing token exchange addon")

	tokenExchangeAddon := setup.Addons{
		Client:     mgr.GetClient(),
		AgentImage: utils.GetEnv("TOKEN_EXCHANGE_IMAGE", o.testEnvFile),
		AddonName:  setup.TokenExchangeName,
	}

	logger.Info("Creating addon manager")
	addonMgr, err := addonmanager.New(config)
	if err != nil {
		logger.Error("Failed to create addon manager", "error", err)
	}
	err = addonMgr.AddAgent(&tokenExchangeAddon)
	if err != nil {
		logger.Error("Failed to add token exchange addon to addon manager", "error", err)
	}

	if err = (&DRPolicyReconciler{
		HubClient:        mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           logger.With("controller", "DRPolicyReconciler"),
		testEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create DRPolicy controller", "error", err)
		os.Exit(1)
	}

	g, ctx := errgroup.WithContext(ctx)

	logger.Info("Starting manager")
	g.Go(func() error {
		err := mgr.Start(ctx)
		return err
	})

	logger.Info("Starting addon manager")
	g.Go(func() error {
		err := addonMgr.Start(ctx)
		return err
	})

	if err := g.Wait(); err != nil {
		logger.Error("Received an error while waiting. exiting..", "error", err)
		os.Exit(1)
	}
}
