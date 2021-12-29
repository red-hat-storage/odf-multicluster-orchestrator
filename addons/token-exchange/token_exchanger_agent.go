package addons

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange/handlers"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
)

const (
	TokenExchangeName = "tokenexchange"
)

func NewAgentCommand() *cobra.Command {
	o := NewAgentOptions()
	cmd := controllercmd.
		NewControllerCommandConfig(TokenExchangeName, version.Info{Major: "0", Minor: "1"}, o.RunAgent).
		NewCommand()
	cmd.Use = TokenExchangeName
	cmd.Short = "Start the token exchange addon agent"

	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
	Namespace         string
}

// NewAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.Namespace, "namespace", o.Namespace, "Agent install namespace.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	klog.Infof("Running %q", TokenExchangeName)

	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	spokeKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(spokeKubeClient, 10*time.Minute)

	hubRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	hubKubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}
	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute, informers.WithNamespace(o.SpokeClusterName))
	err = handlers.RegisterHandler(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	greenSecretAgent := newgreenSecretTokenExchangeAgentController(
		hubKubeClient,
		hubKubeInformerFactory.Core().V1().Secrets(),
		spokeKubeClient,
		spokeKubeInformerFactory.Core().V1().Secrets(),
		o.SpokeClusterName,
		o.Namespace,
		controllerContext.KubeConfig,
		controllerContext.EventRecorder,
	)

	blueSecretAgent := newblueSecretTokenExchangeAgentController(
		hubKubeClient,
		hubKubeInformerFactory.Core().V1().Secrets(),
		spokeKubeClient,
		spokeKubeInformerFactory.Core().V1().Secrets(),
		o.SpokeClusterName,
		o.Namespace,
		controllerContext.EventRecorder,
		controllerContext.KubeConfig,
	)

	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		TokenExchangeName,
		controllerContext.OperatorNamespace,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go spokeKubeInformerFactory.Start(ctx.Done())
	go greenSecretAgent.Run(ctx, 1)
	go blueSecretAgent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}
