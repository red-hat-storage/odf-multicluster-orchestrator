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
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var (
	mirrorPeer1 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer1",
		},
	}
	ns1 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster11",
		},
	}
	ns2 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster22",
		},
	}
	ns3 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-operators-addon",
		},
	}

	mirrorPeer1LookupKey = types.NamespacedName{Namespace: mirrorPeer1.Namespace, Name: mirrorPeer1.Name}
	mcAddOn1LookupKey    = types.NamespacedName{Namespace: "cluster11", Name: setup.TokenExchangeName}
	mcAddOn2LookupKey    = types.NamespacedName{Namespace: "cluster22", Name: setup.TokenExchangeName}
	mcAddOn1             = addonapiv1alpha1.ManagedClusterAddOn{}
	mcAddOn2             = addonapiv1alpha1.ManagedClusterAddOn{}
)

var _ = Describe("ManagedClusterAddOn creation, updation and deletion", func() {
	odfClientInfoConfigMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: "openshift-operators-addon",
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
			"cluster11_test-storagecluster": `
{
    "clusterId": "cluster11",
    "name": "cluster11",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "cluster11",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluster11",
    "clientId": "client-1"
}
`,
			"cluster22_test-storagecluster": `
{
    "clusterId": "cluster22",
    "name": "cluster22",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "cluster22",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluter22",
    "clientId": "client-2"
}
`,
		},
	}
	When("creating or updating ManagedClusterAddOn", func() {
		BeforeEach(func() {
			Expect(os.Setenv("POD_NAMESPACE", "openshift-operators-addon")).To(BeNil())
			newMirrorPeer := mirrorPeer1.DeepCopy()
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Type: "async",
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "cluster11",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
					{
						ClusterName: "cluster22",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
				},
			}
			managedcluster1 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster11",
				},
			}
			managedcluster2 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster22",
				},
			}
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns3, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), odfClientInfoConfigMap, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer1.DeepCopy()
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), odfClientInfoConfigMap, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &mcAddOn1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &mcAddOn2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.DeleteAllOf(context.TODO(), &clusterv1.ManagedCluster{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: "",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns3, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			Expect(os.Unsetenv("POD_NAMESPACE")).To(BeNil())
		})
		It("should not return any error", func() {
			By("polling for the created ManagedClusterAddOn", func() {
				Eventually(func() error {
					err := k8sClient.Get(context.TODO(), mcAddOn1LookupKey, &mcAddOn1)
					if err != nil {
						return err
					}
					return nil
				}, 20*time.Second, 2*time.Second).ShouldNot(HaveOccurred())

				Eventually(func() error {
					err := k8sClient.Get(context.TODO(), mcAddOn2LookupKey, &mcAddOn2)
					if err != nil {
						return err
					}
					return nil
				}, 20*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
			})
			By("updating ManagedClusterAddOn", func() {
				mcAddOn1.Spec.InstallNamespace = "new-test-namespace"
				err := k8sClient.Update(context.TODO(), &mcAddOn1, &client.UpdateOptions{})
				Expect(err).NotTo(HaveOccurred())
				//ToDO: Spec should reconcile back to mirrorPeer.Spec.Items[i].StorageClusterRef.Namespace
				// But right now it is getting updated if user will update ManagedClusterAddOn object.
				Eventually(func() string {
					k8sClient.Get(context.TODO(), mcAddOn1LookupKey, &mcAddOn1)
					return mcAddOn1.Spec.InstallNamespace
				}, 20*time.Second, 2*time.Second).Should(Equal("new-test-namespace"))
			})

			By("having invalid annotations", func() {
				Eventually(func() string {
					k8sClient.Get(context.TODO(), mcAddOn1LookupKey, &mcAddOn1)
					return mcAddOn1.Annotations[utils.DRModeAnnotationKey]
				}, 20*time.Second, 2*time.Second).Should(Equal("async"))

				Eventually(func() string {
					k8sClient.Get(context.TODO(), mcAddOn2LookupKey, &mcAddOn2)
					return mcAddOn2.Annotations[utils.DRModeAnnotationKey]
				}, 20*time.Second, 2*time.Second).Should(Equal("async"))
			})
		})
	})
})
