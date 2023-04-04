package maintenance

import (
	"context"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
	addonutils "open-cluster-management.io/addon-framework/pkg/utils"
)

func NewAgentCommand() *cobra.Command {
	o := NewAgentOptions()
	cmdConfig := controllercmd.
		NewControllerCommandConfig(setup.MaintainAgentName, version.Info{Major: "0", Minor: "1"}, o.RunAgent)

	cmd := cmdConfig.NewCommand()
	cmd.Use = setup.MaintainAgentName
	cmd.Short = "Start the maintenance mode addon"

	o.AddFlags(cmd)

	flags := cmd.Flags()
	flags.BoolVar(&cmdConfig.DisableLeaderElection, "disable-leader-election", true, "Disable leader election for the agent")

	return cmd
}

// AgentOptions defines the flags for agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
}

// NewAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	klog.Infof("Running %q", setup.MaintainAgentName)

	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	cc, err := addonutils.NewConfigChecker("agent kubeconfig checker", o.HubKubeconfigFile)
	if err != nil {
		return err
	}

	go setup.ServeHealthProbes(ctx.Done(), ":8000", cc.Check)

	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		setup.MaintainAgentName,
		controllerContext.OperatorNamespace,
	)

	go leaseUpdater.Start(ctx)
	go runManager(ctx, controllerContext.KubeConfig, o.SpokeClusterName)
	<-ctx.Done()
	return nil
}
