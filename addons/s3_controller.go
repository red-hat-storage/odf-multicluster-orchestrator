package addons

import (
	"context"
	"os"
	"strings"
	"time"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// S3SecretReconciler reconciles a MirrorPeer object
type S3SecretReconciler struct {
	Scheme           *runtime.Scheme
	HubClient        client.Client
	SpokeClient      client.Client
	SpokeClusterName string
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isOurOBC := func(obj interface{}) bool {
		s3MatchString := os.Getenv("S3_EXCHANGE_SOURCE_SECRET_STRING_MATCH")
		if s3MatchString == "" {
			s3MatchString = utils.BucketGenerateName
		}
		if obc, ok := obj.(*obv1alpha1.ObjectBucketClaim); ok {
			return strings.Contains(obc.Name, s3MatchString)
		}
		return false
	}

	s3BucketPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isOurOBC(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isOurOBC(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isOurOBC(e.ObjectNew)
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("s3secret_controller").
		Watches(&source.Kind{Type: &obv1alpha1.ObjectBucketClaim{}}, &handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, s3BucketPredicate)).
		Complete(r)
}

func (r *S3SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var obc obv1alpha1.ObjectBucketClaim

	klog.Infof("Reconciling OBC", "OBC", req.NamespacedName.String())
	err = r.SpokeClient.Get(ctx, req.NamespacedName, &obc)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Could not find OBC. Ignoring since object must have been deleted.")
			return ctrl.Result{}, nil
		}
		klog.Errorf("Failed to get OBC.", err)
		return ctrl.Result{}, err
	}

	if obc.Status.Phase != obv1alpha1.ObjectBucketClaimStatusPhaseBound {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	err = r.syncBlueSecretForS3(ctx, obc.Name, obc.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
