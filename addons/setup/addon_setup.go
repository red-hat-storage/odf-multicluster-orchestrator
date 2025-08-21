package setup

import (
	"context"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"reflect"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	certificatesv1 "k8s.io/api/certificates/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

//go:embed tokenexchange-manifests
var exchangeManifestFiles embed.FS

type Addons struct {
	Client     client.Client
	AgentImage string
	AddonName  string
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
		HubOperatorNamespace  string
		Image                 string
		DRMode                string
		Group                 string
		User                  string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", a.AddonName),
		AddonInstallNamespace: installNamespace,
		OdfOperatorNamespace:  odfOperatorNamespace,
		HubOperatorNamespace:  addon.Annotations[utils.HubOperatorNamespaceKey],
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
	clusterName := cluster.Name
	var err error
	ctx := context.TODO()

	clusterrole := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management:token-exchange:agent",
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &clusterrole, func() error {
		clusterrole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"multicluster.odf.openshift.io"},
				Resources: []string{"mirrorpeers"},
				Verbs:     []string{"get", "list", "watch", "update"},
			},
			{
				APIGroups: []string{"cluster.open-cluster-management.io"},
				Resources: []string{"managedclusters"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
		}
		return nil
	})
	if err != nil {
		return err
	}

	clusterrolebinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management:token-exchange:agent",
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &clusterrolebinding, func() error {
		if clusterrolebinding.CreationTimestamp.IsZero() {
			clusterrolebinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "open-cluster-management:token-exchange:agent",
			}
		}
		uSub := rbacv1.Subject{
			Kind:     "User",
			Name:     user,
			APIGroup: "rbac.authorization.k8s.io",
		}
		gSub := rbacv1.Subject{
			Kind:     "Group",
			Name:     groups[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
		if !containsSubject(clusterrolebinding.Subjects, &uSub) {
			clusterrolebinding.Subjects = append(clusterrolebinding.Subjects, uSub)
		}
		if !containsSubject(clusterrolebinding.Subjects, &gSub) {
			clusterrolebinding.Subjects = append(clusterrolebinding.Subjects, gSub)
		}
		return nil
	})
	if err != nil {
		return err
	}

	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "open-cluster-management:token-exchange:agent",
			Namespace: clusterName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch", "create", "delete", "update"},
			},
			{
				APIGroups: []string{"addon.open-cluster-management.io"},
				Resources: []string{"managedclusteraddons"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{"addon.open-cluster-management.io"},
				Resources: []string{"managedclusteraddons/finalizers"},
				Verbs:     []string{"update"},
			},
			{
				APIGroups: []string{"addon.open-cluster-management.io"},
				Resources: []string{"managedclusteraddons/status"},
				Verbs:     []string{"patch", "update"},
			},
		}
		return nil
	})
	if err != nil {
		return err
	}

	rolebinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "open-cluster-management:token-exchange:agent",
			Namespace: clusterName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &rolebinding, func() error {
		if rolebinding.CreationTimestamp.IsZero() {
			rolebinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "open-cluster-management:token-exchange:agent",
			}
		}
		gSub := rbacv1.Subject{
			Kind:     "Group",
			Name:     groups[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
		if !containsSubject(rolebinding.Subjects, &gSub) {
			rolebinding.Subjects = append(rolebinding.Subjects, gSub)
		}
		return nil
	})

	return err
}

func containsSubject(slice []rbacv1.Subject, subject *rbacv1.Subject) bool {
	for i := range slice {
		if reflect.DeepEqual(slice[i], *subject) {
			return true
		}
	}
	return false
}
