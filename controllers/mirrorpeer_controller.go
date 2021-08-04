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

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MirrorPeerReconciler reconciles a MirrorPeer object
type MirrorPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods;secrets;configmaps;events,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=*
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons;clustermanagementaddons,verbs=*
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *MirrorPeerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch MirrorPeer for given Request
	var mirrorPeer multiclusterv1alpha1.MirrorPeer
	err := r.Get(ctx, req.NamespacedName, &mirrorPeer)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Could not find MirrorPeer. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get MirrorPeer")
		return ctrl.Result{}, err
	}

	logger.V(2).Info("Validating MirrorPeer", "MirrorPeer", req.NamespacedName)
	// Validate MirrorPeer
	// MirrorPeer.Spec must be defined
	if err := undefinedMirrorPeerSpec(mirrorPeer.Spec); err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	// MirrorPeer.Spec.Items must be unique
	if err := uniqueSpecItems(mirrorPeer.Spec); err != nil {
		return ctrl.Result{Requeue: false}, err
	}
	for i := range mirrorPeer.Spec.Items {
		// MirrorPeer.Spec.Items must not have empty fields
		if err := emptySpecItems(mirrorPeer.Spec.Items[i]); err != nil {
			// return error and do not requeue since user needs to update the spec
			// when user updates the spec, new reconcile will be triggered
			return reconcile.Result{Requeue: false}, err
		}
		// MirrorPeer.Spec.Items[*].ClusterName must be a valid ManagedCluster
		if err := isManagedCluster(ctx, r.Client, mirrorPeer.Spec.Items[i].ClusterName); err != nil {
			return ctrl.Result{}, err
		}
	}
	logger.V(2).Info("All validations for MirrorPeer passed", "MirrorPeer", req.NamespacedName)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.MirrorPeer{}).
		Complete(r)
}
