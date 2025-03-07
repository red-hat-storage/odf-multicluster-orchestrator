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
	"log/slog"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	ramendrv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

// MirrorPeerSecretReconciler reconciles MirrorPeer CRs,
// and source/destination Secrets
type MirrorPeerSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *slog.Logger

	testEnvFile      string
	currentNamespace string
}

//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.odf.openshift.io,resources=mirrorpeers/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=secrets;events;configmaps,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile

// Reconcile standard reconcile function for MirrorPeerSecret controller
func (r *MirrorPeerSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("Request", req.String())
	logger.Info("Starting reconciliation for Secret")

	result, err := r.mirrorPeerSecretReconcile(ctx, r.Client, req)

	if err != nil {
		logger.Error("Reconciliation error for Secret", "error", err)
	} else {
		logger.Info("Reconciliation completed for Secret")
	}

	return result, err
}

func (r *MirrorPeerSecretReconciler) mirrorPeerSecretReconcile(ctx context.Context, rc client.Client, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("Request", req.NamespacedName)
	logger.Info("Reconciling secret")

	var peerSecret corev1.Secret
	err := rc.Get(ctx, req.NamespacedName, &peerSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Secret not found, processing deletion", "Request", req.NamespacedName)
			return ctrl.Result{}, processDeletedSecrets(ctx, rc, req.NamespacedName, logger)
		}
		logger.Error("Error retrieving secret", "error", err, "Request", req.NamespacedName)
		return ctrl.Result{}, err
	}

	if utils.IsSecretInternal(&peerSecret) {
		err = createOrUpdateSecretsFromInternalSecret(ctx, rc, r.currentNamespace, &peerSecret, nil, logger)
		if err != nil {
			logger.Error("Failed to update from internal secret", "error", err, "Secret", peerSecret.Name)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logger := r.Logger
	logger.Info("Setting up controller with manager")
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(utils.SourceOrDestinationPredicate)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.secretConfigMapFunc)).
		Complete(r)
}

func (r *MirrorPeerSecretReconciler) secretConfigMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := r.Logger
	ConfigMapRamenConfigKeyName := "ramen_manager_config.yaml"
	var cm *corev1.ConfigMap
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return []reconcile.Request{}
	}

	if _, ok := cm.Data[ConfigMapRamenConfigKeyName]; !ok {
		return []reconcile.Request{}
	}

	ramenConfig := &ramendrv1alpha1.RamenConfig{}
	err := yaml.Unmarshal([]byte(cm.Data[ConfigMapRamenConfigKeyName]), ramenConfig)
	if err != nil {
		logger.Error("Failed to unmarshal RamenConfig from ConfigMap", "error", err, "ConfigMapName", cm.Name)
		return []reconcile.Request{}
	}

	internalSecretLabel, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.Equals, []string{string(utils.InternalLabel)})
	if err != nil {
		logger.Error("Failed to create label requirement for internal secrets", "error", err)
		return []reconcile.Request{}
	}

	internalSecretSelector := labels.NewSelector().Add(*internalSecretLabel)
	listOpts := &client.ListOptions{
		Namespace:     "", //All Namespaces
		LabelSelector: internalSecretSelector,
	}

	secrets := &corev1.SecretList{}
	if err := r.Client.List(ctx, secrets, listOpts); err != nil {
		logger.Error("Failed to list secrets based on label selector", "error", err, "Selector", internalSecretSelector)
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(secrets.Items))
	for i, secret := range secrets.Items {
		requests[i].Name = secret.GetName()
		requests[i].Namespace = secret.GetNamespace()
	}

	logger.Info("Generated reconcile requests from internal secrets", "NumberOfRequests", len(requests))
	return requests
}
