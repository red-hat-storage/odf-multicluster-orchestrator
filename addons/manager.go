package addons

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/apis/replication.storage/v1alpha1"
	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/spf13/cobra"
	submarinerv1alpha1 "github.com/submariner-io/submariner-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	runtimewait "k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	addonutils "open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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
	utilruntime.Must(submarinerv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(rookv1.AddToScheme(mgrScheme))
	utilruntime.Must(extv1.AddToScheme(mgrScheme))
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
	SpokeClusterName     string
	DRMode               string
	ZapOpts              zap.Options
}

func (o *AddonAgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flags.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.DRMode, "mode", o.DRMode, "The DR mode of token exchange addon. Valid values are: 'sync', 'async'")
	gfs := flag.NewFlagSet("", flag.ExitOnError)
	o.ZapOpts = zap.Options{
		Development: true,
	}
	o.ZapOpts.BindFlags(gfs)
	flags.AddGoFlagSet(gfs)
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AddonAgentOptions) RunAgent(ctx context.Context) {
	klog.Infof("Starting addon agents.")

	cc, err := addonutils.NewConfigChecker("agent kubeconfig checker", o.HubKubeconfigFile)
	if err != nil {
		klog.Error(err, "ConfigChecker could not be created")
		os.Exit(1)
	}

	go setup.ServeHealthProbes(ctx.Done(), ":8000", cc.Check)
	go runSpokeManager(ctx, *o)
	go runHubManager(ctx, *o)

	<-ctx.Done()
}

// GetClientFromConfig returns a controller-runtime Client for a rest.Config
func GetClientFromConfig(config *rest.Config) (client.Client, error) {
	cl, err := client.New(config, client.Options{Scheme: mgrScheme})
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// GetClientConfig returns the rest.Config for a kubeconfig file
func GetClientConfig(kubeConfigFile string) (*rest.Config, error) {
	clientconfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigFile)
	if err != nil {
		return nil, fmt.Errorf("get clientconfig failed: %w", err)
	}
	return clientconfig, nil
}

func runHubManager(ctx context.Context, options AddonAgentOptions) {
	hubConfig, err := GetClientConfig(options.HubKubeconfigFile)
	if err != nil {
		klog.Error(err, "unable to get kubeconfig")
		os.Exit(1)
	}

	spokeClient, err := GetClientFromConfig(ctrl.GetConfigOrDie())
	if err != nil {
		klog.Error(err, "unable to get spoke client")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(hubConfig, ctrl.Options{
		Scheme:                 mgrScheme,
		Port:                   9443,
		MetricsBindAddress:     "0", // disable metrics
		HealthProbeBindAddress: "0", // disable health probe
		ReadinessEndpointName:  "0", // disable readiness probe
		Namespace:              options.SpokeClusterName,
	})
	if err != nil {
		klog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&MirrorPeerReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        mgr.GetClient(),
		SpokeClient:      spokeClient,
		SpokeClusterName: options.SpokeClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "MirrorPeer")
		os.Exit(1)
	}

	if err = (&GreenSecretReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        mgr.GetClient(),
		SpokeClient:      spokeClient,
		SpokeClusterName: options.SpokeClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "GreenSecret")
		os.Exit(1)
	}

	klog.Info("starting hub controller manager")
	if err := mgr.Start(ctx); err != nil {
		klog.Error(err, "problem running hub controller manager")
		os.Exit(1)
	}
}

func runSpokeManager(ctx context.Context, options AddonAgentOptions) {
	hubConfig, err := GetClientConfig(options.HubKubeconfigFile)
	if err != nil {
		klog.Error(err, "unable to get kubeconfig")
		os.Exit(1)
	}

	hubClient, err := GetClientFromConfig(hubConfig)
	if err != nil {
		klog.Error(err, "unable to get spoke client")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 mgrScheme,
		Port:                   9443,
		MetricsBindAddress:     "0", // disable metrics
		HealthProbeBindAddress: "0", // disable health probe
		ReadinessEndpointName:  "0", // disable readiness probe
	})

	if err != nil {
		klog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		klog.Infof("Waiting for MaintenanceMode CRD to be created. MaintenanceMode controller is not running yet.")
		// Wait for 45s as it takes time for MaintenanceMode CRD to be created.
		return runtimewait.PollUntilContextCancel(ctx, 15*time.Second, true,
			func(ctx context.Context) (done bool, err error) {
				var crd extv1.CustomResourceDefinition
				readErr := mgr.GetAPIReader().Get(ctx, types.NamespacedName{Name: "maintenancemodes.ramendr.openshift.io"}, &crd)
				if readErr != nil {
					klog.Error("Unable to get MaintenanceMode CRD", readErr)
					// Do not initialize err as we want to retry.
					// err!=nil or done==true will end polling.
					done = false
					return
				}
				if crd.Spec.Group == "ramendr.openshift.io" && crd.Spec.Names.Kind == "MaintenanceMode" {
					if err = (&MaintenanceModeReconciler{
						Scheme:           mgr.GetScheme(),
						SpokeClient:      mgr.GetClient(),
						SpokeClusterName: options.SpokeClusterName,
					}).SetupWithManager(mgr); err != nil {
						klog.Error("Unable to create MaintenanceMode controller.", err)
						return
					}
					klog.Infof("MaintenanceMode CRD exists. MaintenanceMode controller is now running.")
					done = true
					return
				}
				done = false
				return
			})
	})); err != nil {
		klog.Error("unable to poll MaintenanceMode", err)
		os.Exit(1)
	}

	if err = (&BlueSecretReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        hubClient,
		SpokeClient:      mgr.GetClient(),
		SpokeClusterName: options.SpokeClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "BlueSecret")
		os.Exit(1)
	}

	if err = (&S3SecretReconciler{
		Scheme:           mgr.GetScheme(),
		HubClient:        hubClient,
		SpokeClient:      mgr.GetClient(),
		SpokeClusterName: options.SpokeClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "S3Secret")
		os.Exit(1)
	}

	klog.Info("starting spoke controller manager")
	if err := mgr.Start(ctx); err != nil {
		klog.Error(err, "problem running spoke controller manager")
		os.Exit(1)
	}
}
