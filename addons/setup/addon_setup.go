package setup

import (
	"context"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(scheme.AddToScheme(genericScheme))
}

var tokenExchangeDeploymentFiles = []string{
	"tokenexchange-manifests/spoke_serviceaccount.yaml",
	"tokenexchange-manifests/spoke_clusterrole.yaml",
	"tokenexchange-manifests/spoke_role.yaml",
	"tokenexchange-manifests/spoke_clusterrolebinding.yaml",
	"tokenexchange-manifests/spoke_rolebinding.yaml",
	"tokenexchange-manifests/spoke_deployment.yaml",
}

var agentHubPermissionFiles = []string{
	"hub-manifests/te_hub_role.yaml",
	"hub-manifests/te_hub_rolebinding.yaml",
	"hub-manifests/te_hub_clusterrole.yaml",
	"hub-manifests/te_hub_clusterrolebinding.yaml",
}

//go:embed tokenexchange-manifests
var exchangeManifestFiles embed.FS

//go:embed hub-manifests
var hubManifests embed.FS

type Addons struct {
	KubeClient kubernetes.Interface
	Recorder   events.Recorder
	AgentImage string
	AddonName  string
}
type TokenExchangeAddon struct {
	Addons
}

// Manifests generates manifestworks to deploy the token exchange addon agent on the managed cluster
func (a *Addons) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects := []runtime.Object{}

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = "default"
	}

	if len(a.AgentImage) == 0 {
		return objects, fmt.Errorf("image not provided for agent %q", a.AddonName)
	}

	groups := agent.DefaultGroups(cluster.Name, a.AddonName)
	user := agent.DefaultUser(cluster.Name, a.AddonName, a.AddonName)

	var odfOperatorNamespace string
	if utils.HasRequiredODFKey(cluster) {
		odfOperatorNamespacedName, err := utils.GetNamespacedNameForClusterInfo(*cluster)
		if err != nil {
			return objects, fmt.Errorf("error while getting ODF operator namespace on the spoke cluster %q. %w", cluster.Name, err)
		}
		odfOperatorNamespace = odfOperatorNamespacedName.Namespace
	} else {
		return objects, fmt.Errorf("error while getting ODF operator namespace on the spoke cluster %q. Expected ClusterClaim does not exist", cluster.Name)
	}

	manifestConfig := struct {
		KubeConfigSecret      string
		ClusterName           string
		AddonInstallNamespace string
		OdfOperatorNamespace  string
		Image                 string
		DRMode                string
		Group                 string
		User                  string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", a.AddonName),
		AddonInstallNamespace: installNamespace,
		OdfOperatorNamespace:  odfOperatorNamespace,
		ClusterName:           cluster.Name,
		Image:                 a.AgentImage,
		DRMode:                addon.Annotations[utils.DRModeAnnotationKey],
		Group:                 groups[0],
		User:                  user,
	}

	for _, file := range tokenExchangeDeploymentFiles {
		template, err := exchangeManifestFiles.ReadFile(file)
		if err != nil {
			return objects, err
		}
		raw := assets.MustCreateAssetFromTemplate(file, template, &manifestConfig).Data
		object, _, err := genericCodec.Decode(raw, nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	return objects, nil
}

// GetAgentAddonOptions returns the options of  addon agent
func (a *Addons) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: a.AddonName,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: agent.KubeClientSignerConfigurations(a.AddonName, a.AddonName),
			CSRApproveCheck:   a.csrApproveCheck,
			PermissionConfig:  a.permissionConfig,
		},
	}
}

// To check the addon agent csr, we check
// 1. if the signer name in csr request is valid.
// 2. if organization field and commonName field in csr request is valid.
// 3. if user name in csr is the same as commonName field in csr request.
func (a *Addons) csrApproveCheck(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
	groups := agent.DefaultGroups(cluster.Name, a.AddonName)
	clusterAddOnGroup := groups[0]
	addOnGroup := groups[1]
	authenticatedGroup := groups[2]
	agentUserName := agent.DefaultUser(cluster.Name, a.AddonName, a.AddonName)

	if csr.Spec.SignerName != certificatesv1.KubeAPIServerClientSignerName {
		klog.V(4).Infof("csr %q was not recognized: SignerName not recognized", csr.Name)
		return false
	}

	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		klog.V(4).Infof("csr %q was not recognized: PEM block type is not CERTIFICATE REQUEST", csr.Name)
		return false
	}

	x509cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		klog.V(4).Infof("csr %q was not recognized: %v", csr.Name, err)
		return false
	}

	requestingOrgs := sets.NewString(x509cr.Subject.Organization...)
	if requestingOrgs.Len() != 3 {
		klog.V(4).Infof("csr %q was not recognized: insufficient Subject.Organization information", csr.Name)
		return false
	}

	if !requestingOrgs.Has(authenticatedGroup) {
		klog.V(4).Infof("csr %q was not recognized: missing authenticated group", csr.Name)
		return false
	}

	if !requestingOrgs.Has(addOnGroup) {
		klog.V(4).Infof("csr %q was not recognized: missing addon group", csr.Name)
		return false
	}

	if !requestingOrgs.Has(clusterAddOnGroup) {
		klog.V(4).Infof("csr %q was not recognized: missing cluster addon group", csr.Name)
		return false
	}

	return agentUserName == x509cr.Subject.CommonName
}

// Generates manifestworks to deploy the required roles of addon agent
func (a *Addons) permissionConfig(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	groups := agent.DefaultGroups(cluster.Name, a.AddonName)
	user := agent.DefaultUser(cluster.Name, a.AddonName, a.AddonName)
	config := struct {
		ClusterName string
		Group       string
		User        string
	}{
		ClusterName: cluster.Name,
		Group:       groups[0],
		User:        user,
	}

	results := resourceapply.ApplyDirectly(
		context.TODO(),
		resourceapply.NewKubeClientHolder(a.KubeClient),
		a.Recorder,
		resourceapply.NewResourceCache(),
		func(name string) ([]byte, error) {
			template, err := hubManifests.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		agentHubPermissionFiles...,
	)

	errs := []error{}
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, result.Error)
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}
