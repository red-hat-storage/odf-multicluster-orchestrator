package controllers

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"

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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// TODO: Remove/update this function in a later release when cleanup is not required.
func preRunGarbageCollection(cl client.Client, logger *slog.Logger) error {
	ctx := context.TODO()
	req, err := labels.NewRequirement("openshiftVersion-major-minor", selection.In, []string{"4.16", "4.17", "4.18"})
	if err != nil {
		return fmt.Errorf("couldn't create ManagedCluster selector requirement. %w", err)
	}

	var managedClusters clusterv1.ManagedClusterList
	err = cl.List(ctx, &managedClusters, &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*req),
	})
	if err != nil {
		return fmt.Errorf("couldn't list ManagedClusters. %w", err)
	}
	logger.Info(fmt.Sprintf("Found %v ManagedClusters matching given requirements", len(managedClusters.Items)), "Requirements", req)

	for idx := range managedClusters.Items {
		maintenanceAddon := addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "maintenance",
				Namespace: managedClusters.Items[idx].Name,
			},
		}
		err = cl.Delete(ctx, &maintenanceAddon, &client.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete maintenance addon from cluster %q. %w", managedClusters.Items[idx].Name, err)
		}
		if err == nil || errors.IsNotFound(err) {
			logger.Info("Deleted maintenance addon from ManagedCluster", "ManagedCluster", managedClusters.Items[idx].Name)
		}
	}
	return nil
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

	preRunClient, err := client.New(config, client.Options{
		Scheme: mgrScheme,
		Cache:  nil,
	})
	if err != nil {
		logger.Error("Failed to create a client for pre-run garbage collection", "error", err)
		os.Exit(1)
	}

	logger.Info("Starting garbage collection before starting manager")
	err = preRunGarbageCollection(preRunClient, logger)
	if err != nil {
		logger.Error("Pre-run garbage collection failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Finished garbage collection")

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
