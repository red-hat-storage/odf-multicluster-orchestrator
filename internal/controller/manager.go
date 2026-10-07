package controller

import (
	"context"
	"crypto/tls"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/s3configuration"
	"os"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/acm"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller/ramen"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/go-logr/zapr"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/spf13/cobra"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsubapis "open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
	appv1beta1 "sigs.k8s.io/application/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
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
	utilruntime.Must(configv1.AddToScheme(mgrScheme))
	utilruntime.Must(ocstlsv1.AddToScheme(mgrScheme))

	utilruntime.Must(ramenv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(workv1.AddToScheme(mgrScheme))
	utilruntime.Must(viewv1beta1.AddToScheme(mgrScheme))
	utilruntime.Must(placementv1beta1.AddToScheme(mgrScheme))
	utilruntime.Must(argov1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(appsubapis.AddToScheme(mgrScheme))
	utilruntime.Must(appv1beta1.AddToScheme(mgrScheme))
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
		Short: "Multicluster Orchestrator for DF",
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

	// Create selector for Object(s) with operator-managed labels.
	// Selects secrets that have the "multicluster.odf.openshift.io/created-by" label key,
	// regardless of value (tokenexchange, mirrorpeersecret, etc.).
	createByMCOLabelReq, err := labels.NewRequirement(utils.CreatedByLabelKey, selection.Exists, nil)
	if err != nil {
		logger.Error("Failed to create secret label requirement", "error", err)
		os.Exit(1)
	}
	createByMCOLabelSelector := labels.NewSelector().Add(*createByMCOLabelReq)

	// Configure cache to only cache the resources we actually need.
	// This reduces memory footprint by filtering out unnecessary resources.
	cacheOptions := cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			// Only cache ManagedClusterAddOn with required label
			&addonapiv1alpha1.ManagedClusterAddOn{}: {
				Label: createByMCOLabelSelector,
			},
			// Only cache ClusterManagementAddOn with required label
			&addonapiv1alpha1.ClusterManagementAddOn{}: {
				Label: createByMCOLabelSelector,
			},
			// Only cache ManagedClusterViews created by this controller
			&viewv1beta1.ManagedClusterView{}: {
				Label: labels.SelectorFromSet(labels.Set{utils.CreatedByLabelKey: "odf-multicluster-managedcluster-controller"}),
			},
			// Only cache the specific TLSProfile resource we monitor
			&ocstlsv1.TLSProfile{}: {
				Field: fields.SelectorFromSet(fields.Set{"metadata.name": utils.TLSProfileName}),
			},
			// Only cache ConfigMaps in the operator namespace to reduce memory usage.
			// Controllers watch specific ConfigMaps (odf-client-info, ramen-hub-operator-config)
			// which are all in the operator namespace.
			&corev1.ConfigMap{}: {
				Namespaces: map[string]cache.Config{
					currentNamespace: {},
				},
			},
			// Only cache Secrets created by this operator to reduce memory usage.
			// All operator-managed secrets are labeled with "multicluster.odf.openshift.io/created-by"
			// (with values like "tokenexchange" or "mirrorpeersecret").
			// This includes S3 secrets and other secrets managed by the operator across
			// the operator namespace and managed cluster namespaces.
			&corev1.Secret{}: {
				Label: createByMCOLabelSelector,
			},
			// Only cache ManifestWorks created by this operator to reduce memory usage.
			// Only cache ManifestWorks labeled with "multicluster.odf.openshift.io/created-by".
			&workv1.ManifestWork{}: {
				Label: createByMCOLabelSelector,
			},
			// ManagedCluster: Use transform to reduce memory footprint
			// We only need metadata and status.clusterClaims, so strip everything else
			&clusterv1.ManagedCluster{}: {
				Transform: func(i interface{}) (interface{}, error) {
					mc, ok := i.(*clusterv1.ManagedCluster)
					if !ok {
						return i, nil
					}
					// Create a minimal ManagedCluster with only the fields we need
					minimal := &clusterv1.ManagedCluster{
						ObjectMeta: mc.ObjectMeta,
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: mc.Status.ClusterClaims,
						},
					}
					return minimal, nil
				},
			},
		},
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 mgrScheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: o.ProbeAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       "1d19c724.odf.openshift.io",
		Cache:                  cacheOptions,
	})
	if err != nil {
		logger.Error("Failed to start manager", "error", err)
		os.Exit(1)
	}

	if err = (&s3configuration.S3ConfigurationReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           logger.With("controller", "S3Configuration"),
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create S3Configuration controller", "error", err)
		os.Exit(1)
	}

	if err = (&odf.MirrorPeerReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           logger.With("controller", "odf.MirrorPeerReconciler"),
		TestEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create MirrorPeer controller", "error", err)
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err = (&acm.ManagedClusterReconciler{
		Client:           mgr.GetClient(),
		Logger:           logger.With("controller", "acm.ManagedClusterReconciler"),
		TestEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ManagedCluster controller", "error", err)
		os.Exit(1)
	}

	if err = (&acm.ManagedClusterViewReconciler{
		Client:           mgr.GetClient(),
		Logger:           logger.With("controller", "acm.ManagedClusterViewReconciler"),
		TestEnvFile:      o.testEnvFile,
		CurrentNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ManagedClusterView controller", "error", err)
		os.Exit(1)
	}

	if err = (&ramen.DRPlacementControlReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: logger.With("controller", "ramen.DRPlacementControlReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create DRPlacementControl controller", "error", err)
		os.Exit(1)
	}

	if err = (&ramen.ProtectedApplicationViewReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: logger.With("controller", "ramen.ProtectedApplicationViewReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ProtectedApplicationView controller", "error", err)
		os.Exit(1)
	}

	if err = (&ClusterVersionReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Logger:            logger.With("controller", "ClusterVersionReconciler"),
		ConsolePort:       o.MulticlusterConsolePort,
		OperatorNamespace: currentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create ClusterVersion controller", "error", err)
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

	logger.Info("Creating addon manager")
	addonMgr, err := addonmanager.New(config)
	if err != nil {
		logger.Error("Failed to create addon manager", "error", err)
	}

	logger.Info("Initializing token exchange addon")
	tokenExchangeAddon := setup.Addons{
		Client:     mgr.GetClient(),
		AgentImage: utils.GetEnv("TOKEN_EXCHANGE_IMAGE", o.testEnvFile),
		AddonName:  utils.TokenExchangeName,
	}

	err = addonMgr.AddAgent(&tokenExchangeAddon)
	if err != nil {
		logger.Error("Failed to add token exchange addon to addon manager", "error", err)
	}

	logger.Info("Initializing s3config addon")
	s3ConfigAddon := setup.S3ConfigAddons{
		Client:     mgr.GetClient(),
		AgentImage: utils.GetEnv("TOKEN_EXCHANGE_IMAGE", o.testEnvFile), // Same image, different command
		AddonName:  utils.S3ConfigAddonName,
	}

	err = addonMgr.AddAgent(&s3ConfigAddon)
	if err != nil {
		logger.Error("Failed to add s3config addon to addon manager", "error", err)
	}

	if err = (&ramen.DRPolicyReconciler{
		HubClient:        mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           logger.With("controller", "ramen.DRPolicyReconciler"),
		TestEnvFile:      o.testEnvFile,
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
