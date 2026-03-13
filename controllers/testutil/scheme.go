//go:build unit
// +build unit

package testutil

import (
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	placementv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	appsubapis "open-cluster-management.io/multicloud-operators-subscription/pkg/apis"
)

var TestScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(clusterv1.AddToScheme(s))
	utilruntime.Must(multiclusterv1alpha1.AddToScheme(s))
	utilruntime.Must(addonapiv1alpha1.AddToScheme(s))
	utilruntime.Must(consolev1.AddToScheme(s))
	utilruntime.Must(ramenv1alpha1.AddToScheme(s))
	utilruntime.Must(workv1.AddToScheme(s))
	utilruntime.Must(viewv1beta1.AddToScheme(s))
	utilruntime.Must(placementv1beta1.AddToScheme(s))
	utilruntime.Must(argov1alpha1.AddToScheme(s))
	utilruntime.Must(appsubapis.AddToScheme(s))
	utilruntime.Must(replicationv1alpha1.AddToScheme(s))
	utilruntime.Must(ocsv1.AddToScheme(s))
	utilruntime.Must(ocsv1alpha1.AddToScheme(s))
	utilruntime.Must(obv1alpha1.AddToScheme(s))
	utilruntime.Must(routev1.AddToScheme(s))
	utilruntime.Must(rookv1.AddToScheme(s))
	utilruntime.Must(extv1.AddToScheme(s))
	utilruntime.Must(snapshotv1.AddToScheme(s))
	utilruntime.Must(templatev1.AddToScheme(s))
	return s
}()
