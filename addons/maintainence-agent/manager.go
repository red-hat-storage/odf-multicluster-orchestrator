package maintenance

import (
	"context"
	"os"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	mgrScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(mgrScheme))
	utilruntime.Must(clusterv1.AddToScheme(mgrScheme))

	utilruntime.Must(addonapiv1alpha1.AddToScheme(mgrScheme))

	utilruntime.Must(corev1.AddToScheme(mgrScheme))
	utilruntime.Must(ocsv1.AddToScheme(mgrScheme))
	utilruntime.Must(obv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(routev1.AddToScheme(mgrScheme))

	utilruntime.Must(ramenv1alpha1.AddToScheme(mgrScheme))
	utilruntime.Must(rookv1.AddToScheme(mgrScheme))
	utilruntime.Must(appsv1.AddToScheme(mgrScheme))
	//+kubebuilder:scaffold:scheme
}

func runManager(ctx context.Context, spokeConfig *rest.Config, spokeClusterName string) {

	mgr, err := ctrl.NewManager(spokeConfig, ctrl.Options{
		Scheme: mgrScheme,
		Port:   9443,
	})
	if err != nil {
		klog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&MaintenanceModeReconciler{
		SpokeClient:      mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		SpokeClusterName: spokeClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "MaintenanceMode")
		os.Exit(1)
	}

	g, _ := errgroup.WithContext(ctrl.SetupSignalHandler())

	klog.Info("starting MaintenanceMode controller manager")
	g.Go(func() error {
		err := mgr.Start(ctx)
		return err
	})

	if err := g.Wait(); err != nil {
		klog.Error(err, "received an error. exiting..")
		os.Exit(1)
	}
}
