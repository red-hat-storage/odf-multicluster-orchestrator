/*
Copyright 2021 Red Hat OpenShift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MirrorPeerSecretReconciler reconciles MirrorPeer CRs,
// and source/destination Secrets
type MirrorPeerSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=secrets;events,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile

// Reconcile standard reconcile function for MirrorPeerSecret controller
func (r *MirrorPeerSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// reconcile for 'Secrets' (source or destination)
	return ctrl.Result{}, mirrorPeerSecretReconcile(ctx, r.Client, req)
}

func mirrorPeerSecretReconcile(ctx context.Context, rc client.Client, req ctrl.Request) error {
	var err error
	logger := log.FromContext(ctx, "controller", "MirrorPeerSecret")
	var peerSecret corev1.Secret
	err = rc.Get(ctx, req.NamespacedName, &peerSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Secret not found", "req", req)
			return processDeletedSecrets(ctx, rc, req.NamespacedName)
		}
		logger.Error(err, "Error in getting secret", "request", req)
		return err
	}
	if common.IsSecretSource(&peerSecret) {
		if err := common.ValidateSourceSecret(&peerSecret); err != nil {
			logger.Error(err, "Provided source secret is not valid", "secret", peerSecret)
			return err
		}
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, &peerSecret)
		if err != nil {
			logger.Error(err, "Updating the destination secret failed", "secret", peerSecret)
			return err
		}
	} else if common.IsSecretDestination(&peerSecret) {
		// a destination secret updation happened
		err = processDestinationSecretUpdation(ctx, rc, &peerSecret)
		if err != nil {
			logger.Error(err, "Restoring destination secret failed", "secret", peerSecret)
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(common.SourceOrDestinationPredicate)).
		Complete(r)
}
