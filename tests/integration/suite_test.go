//go:build integration
// +build integration

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

package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	//+kubebuilder:scaffold:imports
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	var err error
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../..", "config", "crd", "bases"),
			filepath.Join("..", "testdata"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = multiclusterv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = addonapiv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = clusterv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = ramenv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: ":8080", // disable metrics
		},
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
		LeaderElectionID:       "1d19c724.odf.openshift.io",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(mgr).NotTo(BeNil())

	fakeLogger := utils.GetLogger(utils.GetZapLogger(true))
	err = (&controllers.MirrorPeerReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Logger:           fakeLogger,
		CurrentNamespace: "openshift-operators",
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&controllers.MirrorPeerSecretReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: fakeLogger,
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	nsOpenshiftOperators := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-operators",
		},
	}
	err = k8sClient.Create(context.TODO(), nsOpenshiftOperators, &client.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	odfClientInfoConfigMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: "openshift-operators",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: viewv1beta1.GroupVersion.String(),
					Kind:       "ManagedClusterView",
					Name:       "mcv-1",
					UID:        "mcv-uid",
				},
			},
		},
		Data: map[string]string{
			"mc-1_test-storagecluster1":                    "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"mc-2_test-storagecluster2":                    "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"test-provider-cluster1_test-storagecluster-1": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"test-provider-cluster2_test-storagecluster-2": "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"cluster1_test-storagecluster":                 "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
			"cluster2_test-storagecluster":                 "{\"providerInfo\":{\"version\":\"4.19.0\", \"deploymentType\": \"external\"}}",
		},
	}
	err = k8sClient.Create(context.TODO(), odfClientInfoConfigMap, &client.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
