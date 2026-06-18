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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

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
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update

func (r *ClusterVersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ocpVersion, err := utils.GetOpenShiftVersion(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureConsolePlugin(ctx, ocpVersion); err != nil {
		r.Logger.Error("Could not ensure compatibility for odf-multicluster-console plugin", "error", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClusterVersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		clusterVersion, err := utils.GetOpenShiftVersion(ctx, r.Client)
		if err != nil {
			return err
		}

		return r.ensureConsolePlugin(ctx, clusterVersion)
	}))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ClusterVersion{}).
		Complete(r)
}

func (r *ClusterVersionReconciler) ensureConsolePlugin(ctx context.Context, clusterVersion string) error {
	basePath := console.GetBasePath(clusterVersion)

	deployment := appsv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      console.PluginName,
		Namespace: r.OperatorNamespace,
	}, &deployment); err != nil {
		return err
	}

	odfService := console.GetService(console.PluginName, r.ConsolePort, r.OperatorNamespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, &odfService, func() error {
		return nil
	}); err != nil {
		return err
	}

	odfConsolePlugin := console.GetConsolePluginCR(r.ConsolePort, console.PluginName, r.OperatorNamespace)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &odfConsolePlugin, func() error {
		if odfConsolePlugin.Spec.Backend.Service != nil && odfConsolePlugin.Spec.Backend.Service.BasePath != basePath {
			r.Logger.Info(fmt.Sprintf("Set the BasePath for odf-multicluster-console plugin as '%s'", basePath))
			odfConsolePlugin.Spec.Backend.Service.BasePath = basePath
		}
		return nil
	})
	return err
}
