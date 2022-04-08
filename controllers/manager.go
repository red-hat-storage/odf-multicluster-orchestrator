package controllers

import (
	"context"
	"flag"
	"os"

	"github.com/openshift/library-go/pkg/operator/events"
	tokenexchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	mgrScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(mgrScheme))
	utilruntime.Must(clusterv1.AddToScheme(mgrScheme))

	utilruntime.Must(multiclusterv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(addonapiv1alpha1.AddToScheme(mgrScheme))
	//+kubebuilder:scaffold:scheme
}

type ManagerOptions struct {
	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	ZapOpts              zap.Options
}

func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

func (o *ManagerOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flags.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	o.ZapOpts = zap.Options{
		Development: true,
	}
	o.ZapOpts.BindFlags(flag.CommandLine)
}

func NewManagerCommand() *cobra.Command {
	mgrOpts := NewManagerOptions()
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Multicluster Orchestrator for ODF",
		Run: func(cmd *cobra.Command, args []string) {
			mgrOpts.runManager()
		},
	}
	mgrOpts.AddFlags(cmd)
	return cmd
}

func (o *ManagerOptions) runManager() {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&o.ZapOpts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 mgrScheme,
		MetricsBindAddress:     o.MetricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: o.ProbeAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       "1d19c724.odf.openshift.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&MirrorPeerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MirrorPeer")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err = (&MirrorPeerSecretReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MirrorPeer")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("initializing token exchange addon")
	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "problem getting kubeclient")
	}

	namespace := os.Getenv("POD_NAMESPACE")
	controllerRef, err := events.GetControllerReferenceForCurrentPod(context.TODO(), kubeClient, namespace, nil)
	if err != nil {
		setupLog.Info("unable to get owner reference (falling back to namespace)", "error", err)
	}
	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events(namespace), tokenexchange.TokenExchangeName, controllerRef)

	agentImage := os.Getenv("TOKEN_EXCHANGE_IMAGE")

	tokenExchangeAddon := tokenexchange.TokenExchangeAddon{
		KubeClient: kubeClient,
		Recorder:   eventRecorder,
		AgentImage: agentImage,
	}

	err = (&multiclusterv1alpha1.MirrorPeer{}).SetupWebhookWithManager(mgr)

	if err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "MirrorPeer")
		os.Exit(1)
	}

	setupLog.Info("creating addon manager")
	addonMgr, err := addonmanager.New(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "problem creating addon manager")
	}
	err = addonMgr.AddAgent(&tokenExchangeAddon)
	if err != nil {
		setupLog.Error(err, "problem adding token exchange addon to addon manager")
	}

	g, ctx := errgroup.WithContext(ctrl.SetupSignalHandler())

	setupLog.Info("starting manager")
	g.Go(func() error {
		err := mgr.Start(ctx)
		return err
	})

	setupLog.Info("starting addon manager")
	g.Go(func() error {
		err := addonMgr.Start(ctx)
		return err
	})

	if err := g.Wait(); err != nil {
		setupLog.Error(err, "received an error. exiting..")
		os.Exit(1)
	}
}
