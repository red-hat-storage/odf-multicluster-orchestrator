package addons

import (
	"context"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
	addonutils "open-cluster-management.io/addon-framework/pkg/utils"
)

func NewAgentCommand() *cobra.Command {
	o := NewAgentOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig(setup.TokenExchangeName, version.Info{Major: "0", Minor: "1"}, o.RunAgent)

	cmd := cmdConfig.NewCommand()
	cmd.Use = setup.TokenExchangeName
	cmd.Short = "Start the token exchange addon agent"

	o.AddFlags(cmd)

	flags := cmd.Flags()
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", true, "Disable leader election for the agent")

	return cmd
}

// AgentOptions defines the flags for agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
	DRMode            string
}

// NewAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.DRMode, "mode", o.DRMode, "The DR mode of token exchange addon. Valid values are: 'sync', 'async'")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	klog.Infof("Running %q", setup.TokenExchangeName)

	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	spokeKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(spokeKubeClient, 2*time.Minute)

	hubRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	hubKubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}

	cc, err := addonutils.NewConfigChecker("agent kubeconfig checker", o.HubKubeconfigFile)
	if err != nil {
		return err
	}

	go setup.ServeHealthProbes(ctx.Done(), ":8000", cc.Check)

	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 2*time.Minute, informers.WithNamespace(o.SpokeClusterName))
	err = registerHandler(multiclusterv1alpha1.DRType(o.DRMode), controllerContext.KubeConfig, hubRestConfig)
	if err != nil {
		return err
	}

	greenSecretAgent := newgreenSecretTokenExchangeAgentController(
		hubKubeClient,
		hubKubeInformerFactory.Core().V1().Secrets(),
		spokeKubeClient,
		spokeKubeInformerFactory.Core().V1().Secrets(),
		o.SpokeClusterName,
		controllerContext.KubeConfig,
		controllerContext.EventRecorder,
	)

	blueSecretAgent := newblueSecretTokenExchangeAgentController(
		hubKubeClient,
		hubKubeInformerFactory.Core().V1().Secrets(),
		spokeKubeClient,
		spokeKubeInformerFactory.Core().V1().Secrets(),
		spokeKubeInformerFactory.Core().V1().ConfigMaps(),
		o.SpokeClusterName,
		controllerContext.EventRecorder,
		controllerContext.KubeConfig,
	)

	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		setup.TokenExchangeName,
		controllerContext.OperatorNamespace,
	)

	go leaseUpdater.Start(ctx)
	go hubKubeInformerFactory.Start(ctx.Done())
	go spokeKubeInformerFactory.Start(ctx.Done())
	go greenSecretAgent.Run(ctx, 1)
	go blueSecretAgent.Run(ctx, 1)
	runManager(ctx, hubRestConfig, controllerContext.KubeConfig, o.SpokeClusterName)
	<-ctx.Done()
	return nil
}
