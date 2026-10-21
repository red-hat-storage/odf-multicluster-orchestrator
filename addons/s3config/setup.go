package s3config

import (
	"context"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"slices"

	"github.com/openshift/library-go/pkg/assets"
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

var s3ConfigDeploymentFiles = []string{
	"manifests/spoke_serviceaccount.yaml",
	"manifests/spoke_role.yaml",
	"manifests/spoke_rolebinding.yaml",
	"manifests/spoke_deployment.yaml",
}

//go:embed manifests
var s3ConfigManifestFiles embed.FS

// S3ConfigAddons implements addon-framework AgentAddon interface for s3config addon
type S3ConfigAddons struct {
	Client     client.Client
	AgentImage string
	AddonName  string
}

// Manifests generates manifestworks to deploy the s3config addon agent on the managed cluster
func (a *S3ConfigAddons) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
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

	manifestConfig := struct {
		KubeConfigSecret      string
		ClusterName           string
		AddonInstallNamespace string
		Image                 string
		Group                 string
		User                  string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", a.AddonName),
		AddonInstallNamespace: installNamespace,
		ClusterName:           cluster.Name,
		Image:                 a.AgentImage,
		Group:                 groups[0],
		User:                  user,
	}

	for _, file := range s3ConfigDeploymentFiles {
		template, err := s3ConfigManifestFiles.ReadFile(file)
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

// GetAgentAddonOptions returns the options of s3config addon agent
func (a *S3ConfigAddons) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: a.AddonName,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: agent.KubeClientSignerConfigurations(a.AddonName, a.AddonName),
			CSRApproveCheck:   a.csrApproveCheck,
			PermissionConfig:  a.permissionConfig,
		},
	}
}

// csrApproveCheck checks the addon agent csr
func (a *S3ConfigAddons) csrApproveCheck(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn, csr *certificatesv1.CertificateSigningRequest) bool {
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

	if !requestingOrgs.Has(authenticatedGroup) || !requestingOrgs.Has(addOnGroup) || !requestingOrgs.Has(clusterAddOnGroup) {
		klog.V(4).Infof("csr %q was not recognized: missing required groups", csr.Name)
		return false
	}

	return agentUserName == x509cr.Subject.CommonName
}

// permissionConfig generates RBAC permissions for s3config addon agent on hub
func (a *S3ConfigAddons) permissionConfig(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	groups := agent.DefaultGroups(cluster.Name, a.AddonName)
	user := agent.DefaultUser(cluster.Name, a.AddonName, a.AddonName)
	clusterName := cluster.Name
	ctx := context.TODO()

	// Hub cluster role for accessing s3Configuration resource on hub cluster
	clusterrole := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management:s3config:agent",
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, a.Client, &clusterrole, func() error {
		clusterrole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"multicluster.odf.openshift.io"},
				Resources: []string{"s3configuration"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
		}
		return nil
	})
	if err != nil {
		return err
	}

	clusterrolebinding := rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "open-cluster-management:s3config:agent",
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &clusterrolebinding, func() error {
		if clusterrolebinding.CreationTimestamp.IsZero() {
			clusterrolebinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "open-cluster-management:s3config:agent",
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
		if !slices.Contains(clusterrolebinding.Subjects, uSub) {
			clusterrolebinding.Subjects = append(clusterrolebinding.Subjects, uSub)
		}
		if !slices.Contains(clusterrolebinding.Subjects, gSub) {
			clusterrolebinding.Subjects = append(clusterrolebinding.Subjects, gSub)
		}
		return nil
	})
	if err != nil {
		return err
	}

	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "open-cluster-management:s3config:agent",
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
		}
		return nil
	})
	if err != nil {
		return err
	}

	rolebinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "open-cluster-management:s3config:agent",
			Namespace: clusterName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, a.Client, &rolebinding, func() error {
		if rolebinding.CreationTimestamp.IsZero() {
			rolebinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "open-cluster-management:s3config:agent",
			}
		}
		gSub := rbacv1.Subject{
			Kind:     "Group",
			Name:     groups[0],
			APIGroup: "rbac.authorization.k8s.io",
		}
		if !slices.Contains(rolebinding.Subjects, gSub) {
			rolebinding.Subjects = append(rolebinding.Subjects, gSub)
		}
		return nil
	})

	return err
}
