package addons

import (
	"context"
	"log/slog"
	"os"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/apis/replication.storage/v1alpha1"
	"github.com/go-logr/zapr"
	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	routev1 "github.com/openshift/api/route/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/lease"
	addonutils "open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	mgrScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(ramenv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(appsv1.AddToScheme(mgrScheme))
	utilruntime.Must(clientgoscheme.AddToScheme(mgrScheme))
	utilruntime.Must(clusterv1.AddToScheme(mgrScheme))
	utilruntime.Must(multiclusterv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(addonapiv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(replicationv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(corev1.AddToScheme(mgrScheme))
	utilruntime.Must(ocsv1.AddToScheme(mgrScheme))
	utilruntime.Must(obv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(routev1.AddToScheme(mgrScheme))
	utilruntime.Must(rookv1.AddToScheme(mgrScheme))
	utilruntime.Must(extv1.AddToScheme(mgrScheme))
	utilruntime.Must(snapshotv1.AddToScheme(mgrScheme))
	//+kubebuilder:scaffold:scheme
}

func NewAddonAgentCommand() *cobra.Command {
	o := &AddonAgentOptions{}

	cmd := &cobra.Command{
		Use:   "addons",
		Short: "Start the addon agents",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := ctrl.SetupSignalHandler()
			o.RunAgent(ctx)
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// AddonAgentOptions defines the flags for agent
type AddonAgentOptions struct {
	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	HubKubeconfigFile    string
	KubeconfigFile       string
	SpokeClusterName     string
	OdfOperatorNamespace string
	DRMode               string
	DevMode              bool
}

func (o *AddonAgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flags.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.KubeconfigFile, "kubeconfig", "", "Paths to a kubeconfig. Only required if out-of-cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.OdfOperatorNamespace, "odf-operator-namespace", o.OdfOperatorNamespace, "Namespace of ODF operator on the spoke cluster.")
	flags.StringVar(&o.DRMode, "mode", o.DRMode, "The DR mode of token exchange addon. Valid values are: 'sync', 'async'")
	flags.BoolVar(&o.DevMode, "dev", false, "Set to true for dev environment (Text logging)")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AddonAgentOptions) RunAgent(ctx context.Context) {
	zapLogger := utils.GetZapLogger(o.DevMode)
	defer func() {
		if err := zapLogger.Sync(); err != nil {
			zapLogger.Error("Failed to sync zap logger")
		}
	}()
	ctrl.SetLogger(zapr.NewLogger(zapLogger))
	logger := utils.GetLogger(zapLogger)
	logger.Info("Starting addon agents.")
	cc, err := addonutils.NewConfigChecker("agent kubeconfig checker", o.HubKubeconfigFile)
	if err != nil {
		logger.Error("ConfigChecker could not be created", "error", err)
		os.Exit(1)
	}

	logger.Info("Serving health probes on port 8000")
	go setup.ServeHealthProbes(ctx.Done(), ":8000", cc.Check, logger)

	logger.Info("Starting spoke manager")
	go runSpokeManager(ctx, *o, logger)

	logger.Info("Starting hub manager")
	go runHubManager(ctx, *o, logger)

	logger.Info("Addon agent is running, waiting for context cancellation")
	<-ctx.Done()
	logger.Info("Addon agent has stopped")
}

func runHubManager(ctx context.Context, options AddonAgentOptions, logger *slog.Logger) {
	hubConfig, err := utils.GetClientConfig(options.HubKubeconfigFile)
	if err != nil {
		logger.Error("Failed to get kubeconfig", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(hubConfig, ctrl.Options{
		Scheme: mgrScheme,
		Metrics: server.Options{
			BindAddress: "0", // disable metrics
		},
		HealthProbeBindAddress: "0", // disable health probe
		ReadinessEndpointName:  "0", // disable readiness probe
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				options.SpokeClusterName: {},
			},
		},
	})
	if err != nil {
		logger.Error("Failed to start manager", "error", err)
		os.Exit(1)
	}

	spokeKubeConfig, err := utils.GetClientConfig(options.KubeconfigFile)
	if err != nil {
		logger.Error("Failed to get kubeconfig", "error", err)
		os.Exit(1)
	}

	spokeClient, err := utils.GetClientFromConfig(spokeKubeConfig, mgr.GetScheme())
	if err != nil {
		logger.Error("Failed to get spoke client", "error", err)
		os.Exit(1)
	}

	if err = (&MirrorPeerReconciler{
		Scheme:               mgr.GetScheme(),
		HubClient:            mgr.GetClient(),
		SpokeClient:          spokeClient,
		SpokeClusterName:     options.SpokeClusterName,
		OdfOperatorNamespace: options.OdfOperatorNamespace,
		Logger:               logger.With("controller", "MirrorPeerReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create MirrorPeer controller", "controller", "MirrorPeer", "error", err)
		os.Exit(1)
	}

	logger.Info("Starting hub controller manager")
	if err := mgr.Start(ctx); err != nil {
		logger.Error("Problem running hub controller manager", "error", err)
		os.Exit(1)
	}
}

func runSpokeManager(ctx context.Context, options AddonAgentOptions, logger *slog.Logger) {
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
	})

	if err != nil {
		logger.Error("Failed to start manager", "error", err)
		os.Exit(1)
	}

	currentNamespace := os.Getenv("POD_NAMESPACE")

	spokeKubeClient, err := kubernetes.NewForConfig(spokeKubeConfig)
	if err != nil {
		logger.Error("Failed to get spoke kube client", "error", err)
		os.Exit(1)
	}

	if err = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		logger.Info("Starting lease updater")
		leaseUpdater := lease.NewLeaseUpdater(
			spokeKubeClient,
			setup.TokenExchangeName,
			currentNamespace,
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
		logger.Error("Failed to get kubeconfig", "error", err)
		os.Exit(1)
	}

	hubClient, err := utils.GetClientFromConfig(hubConfig, mgr.GetScheme())
	if err != nil {
		logger.Error("Failed to get hub client", "error", err)
		os.Exit(1)
	}

	if err = (&S3SecretReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        hubClient,
		SpokeClient:      mgr.GetClient(),
		SpokeClusterName: options.SpokeClusterName,
		Logger:           logger.With("controller", "S3SecretReconciler"),
	}).SetupWithManager(mgr); err != nil {
		logger.Error("Failed to create S3Secret controller", "controller", "S3Secret", "error", err)
		os.Exit(1)
	}

	logger.Info("Starting spoke controller manager")
	if err := mgr.Start(ctx); err != nil {
		logger.Error("Problem running spoke controller manager", "error", err)
		os.Exit(1)
	}
}
