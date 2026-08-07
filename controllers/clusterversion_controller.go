/*
Copyright 2026 Red Hat Data Foundation.
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
	"fmt"

	"log/slog"

	configv1 "github.com/openshift/api/config/v1"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/console"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
)

type ClusterVersionReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Logger            *slog.Logger
	ConsolePort       int
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch
// +kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=get;create;update
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;create;update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=tlsprofiles,verbs=get;list;watch

func (r *ClusterVersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if err := r.reconcilePhases(ctx); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ClusterVersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return r.reconcilePhases(ctx)
	})); err != nil {
		return err
	}

	mapToClusterVersion := func(_ context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{
			{NamespacedName: types.NamespacedName{Name: "version"}},
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ClusterVersion{}).
		Watches(&ocstlsv1.TLSProfile{},
			handler.EnqueueRequestsFromMapFunc(mapToClusterVersion),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					return obj.GetName() == utils.TLSProfileName
				}),
				predicate.GenerationChangedPredicate{},
			),
		).
		Complete(r)
}

func (r *ClusterVersionReconciler) reconcilePhases(ctx context.Context) error {
	ocpVersion, err := utils.GetOpenShiftVersion(ctx, r.Client)
	if err != nil {
		return err
	}

	if err := r.ensureConsolePlugin(ctx, ocpVersion); err != nil {
		return fmt.Errorf("could not ensure console plugin: %w", err)
	}

	if err := r.ensureNginxConfigMap(ctx); err != nil {
		return fmt.Errorf("could not ensure nginx config map: %w", err)
	}

	return nil
}

func (r *ClusterVersionReconciler) ensureConsolePlugin(ctx context.Context, clusterVersion string) error {
	basePath := console.GetBasePath(clusterVersion)

	odfService := console.GetService(console.PluginName, r.ConsolePort, r.OperatorNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, &odfService, func() error {
		return nil
	}); err != nil {
		return err
	}

	odfConsolePlugin := console.GetConsolePluginCR(r.ConsolePort, console.PluginName, r.OperatorNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, &odfConsolePlugin, func() error {
		if odfConsolePlugin.Spec.Backend.Service != nil && odfConsolePlugin.Spec.Backend.Service.BasePath != basePath {
			r.Logger.Info(fmt.Sprintf("Set the BasePath for odf-multicluster-console plugin as '%s'", basePath))
			odfConsolePlugin.Spec.Backend.Service.BasePath = basePath
		}
		return nil
	}); err != nil {
		return err
	}

	consoleDeployment := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: console.PluginName, Namespace: r.OperatorNamespace}, consoleDeployment); err != nil {
		return err
	}

	return r.ensureConsoleNetworkPolicy(ctx, consoleDeployment)
}

func (r *ClusterVersionReconciler) ensureConsoleNetworkPolicy(ctx context.Context, consoleDeployment *appsv1.Deployment) error {
	networkPolicy := console.GetNetworkPolicy(r.OperatorNamespace)
	desiredSpec := networkPolicy.Spec
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, networkPolicy, func() error {
		networkPolicy.Spec = desiredSpec
		return controllerutil.SetControllerReference(consoleDeployment, networkPolicy, r.Scheme)
	})
	return err
}

func (r *ClusterVersionReconciler) resolveTLSConfig(ctx context.Context) *ocstlsv1.OpenSSLConfig {
	tlsProfile := &ocstlsv1.TLSProfile{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: utils.TLSProfileName, Namespace: r.OperatorNamespace}, tlsProfile); err != nil {
		if !k8serrors.IsNotFound(err) {
			r.Logger.Error("Failed to get TLSProfile", "error", err)
		}
		return nil
	}

	cfg, found := ocstlsv1.GetConfigForServer(tlsProfile, utils.TLSProfileDomain, utils.TLSProfileServer)
	if !found {
		return nil
	}

	if err := ocstlsv1.ValidateTLSConfig(cfg); err != nil {
		r.Logger.Error("Invalid TLS config, falling back to defaults", "domain", utils.TLSProfileDomain, "server", utils.TLSProfileServer, "error", err)
		return nil
	}

	return ocstlsv1.OpenSSLConfigFrom(ocstlsv1.GetGoTLSConfig(cfg))
}

func (r *ClusterVersionReconciler) ensureNginxConfigMap(ctx context.Context) error {
	ossl := r.resolveTLSConfig(ctx)
	nginxConf, err := console.GenerateNginxConf(ossl)
	if err != nil {
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}
	cm := console.GetNginxConfConfigMap(r.OperatorNamespace, "")
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{console.NginxConfKey: nginxConf}
		return nil
	})
	return err
}
