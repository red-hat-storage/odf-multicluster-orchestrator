package addons

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
)

const (
	TokenExchangeName   = "tokenexchange"
	CreatedByLabelKey   = "multicluster.odf.openshift.io/created-by"
	CreatedByLabelValue = TokenExchangeName
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
		o.SpokeClusterName,
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

func getSecret(lister corev1lister.SecretLister, name, namespace string) (*corev1.Secret, error) {
	se, err := lister.Secrets(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil, err
	case err != nil:
		return nil, err
	}
	return se, nil
}

func createSecret(client kubernetes.Interface, recorder events.Recorder, newSecret *corev1.Secret) error {
	_, _, err := resourceapply.ApplySecret(client.CoreV1(), recorder, newSecret)
	if err != nil {
		return fmt.Errorf("failed to apply secret %q in namespace %q. Error %v", newSecret.Name, newSecret.Namespace, err)
	}

	return nil
}
