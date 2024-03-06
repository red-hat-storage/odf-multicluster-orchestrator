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

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	ramendrv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"
)

// MirrorPeerSecretReconciler reconciles MirrorPeer CRs,
// and source/destination Secrets
type MirrorPeerSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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
	// reconcile for 'Secrets' (source or destination)
	return mirrorPeerSecretReconcile(ctx, r.Client, req)
}

func updateSecretWithHubRecoveryLabel(ctx context.Context, rc client.Client, peerSecret corev1.Secret) error {
	logger := log.FromContext(ctx, "controller", "MirrorPeerSecret")
	logger.Info("Adding backup labels to the secret")

	if peerSecret.ObjectMeta.Labels == nil {
		peerSecret.ObjectMeta.Labels = make(map[string]string)
	}

	_, err := controllerutil.CreateOrUpdate(ctx, rc, &peerSecret, func() error {
		peerSecret.ObjectMeta.Labels[utils.HubRecoveryLabel] = ""
		return nil
	})

	return err
}

func mirrorPeerSecretReconcile(ctx context.Context, rc client.Client, req ctrl.Request) (ctrl.Result, error) {
	var err error
	logger := log.FromContext(ctx, "controller", "MirrorPeerSecret")
	var peerSecret corev1.Secret
	err = rc.Get(ctx, req.NamespacedName, &peerSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Secret not found", "req", req)
			return ctrl.Result{}, processDeletedSecrets(ctx, rc, req.NamespacedName)
		}
		logger.Error(err, "Error in getting secret", "request", req)
		return ctrl.Result{}, err
	}
	if utils.IsSecretSource(&peerSecret) {
		if err := utils.ValidateSourceSecret(&peerSecret); err != nil {
			logger.Error(err, "Provided source secret is not valid", "secret", peerSecret.Name, "namespace", peerSecret.Namespace)
			return ctrl.Result{}, err
		}
		if !utils.HasHubRecoveryLabels(&peerSecret) {
			err = updateSecretWithHubRecoveryLabel(ctx, rc, peerSecret)
			if err != nil {
				logger.Error(err, "Error occured while adding backup labels to secret. Requeing the request")
				return ctrl.Result{}, err
			}
		}
		err = createOrUpdateDestinationSecretsFromSource(ctx, rc, &peerSecret)
		if err != nil {
			logger.Error(err, "Updating the destination secret failed", "secret", peerSecret.Name, "namespace", peerSecret.Namespace)
			return ctrl.Result{}, err
		}
	} else if utils.IsSecretDestination(&peerSecret) {
		// a destination secret updation happened
		err = processDestinationSecretUpdation(ctx, rc, &peerSecret)
		if err != nil {
			logger.Error(err, "Restoring destination secret failed", "secret", peerSecret.Name, "namespace", peerSecret.Namespace)
			return ctrl.Result{}, err
		}
	} else if utils.IsSecretInternal(&peerSecret) {
		err = createOrUpdateSecretsFromInternalSecret(ctx, rc, &peerSecret, nil)
		if !utils.HasHubRecoveryLabels(&peerSecret) {
			err = updateSecretWithHubRecoveryLabel(ctx, rc, peerSecret)
			if err != nil {
				logger.Error(err, "Error occured while adding backup labels to secret. Requeing the request")
				return ctrl.Result{}, err
			}
		}
		if err != nil {
			logger.Error(err, "Updating the secret from internal secret is failed", "secret", peerSecret.Name, "namespace", peerSecret.Namespace)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MirrorPeerSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(utils.SourceOrDestinationPredicate)).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, handler.EnqueueRequestsFromMapFunc(r.secretConfigMapFunc)).
		Complete(r)
}

func (r *MirrorPeerSecretReconciler) secretConfigMapFunc(obj client.Object) []reconcile.Request {
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
		return []reconcile.Request{}
	}

	secrets := &corev1.SecretList{}
	internalSecretLabel, err := labels.NewRequirement(utils.SecretLabelTypeKey, selection.Equals, []string{string(utils.InternalLabel)})
	if err != nil {
		klog.Error(err, "cannot parse new requirement")
		return []reconcile.Request{}
	}

	internalSecretSelector := labels.NewSelector().Add(*internalSecretLabel)
	listOpts := &client.ListOptions{
		Namespace:     "", //All Namespaces
		LabelSelector: internalSecretSelector,
	}

	if err := r.Client.List(context.TODO(), secrets, listOpts); err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(secrets.Items))
	for i, secret := range secrets.Items {
		requests[i].Name = secret.GetName()
		requests[i].Namespace = secret.GetNamespace()
	}

	return requests
}
