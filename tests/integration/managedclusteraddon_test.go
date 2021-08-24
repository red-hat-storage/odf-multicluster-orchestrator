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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tokenexchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
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
			Name: "cluster1",
		},
	}
	ns2 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
		},
	}
	mirrorPeer1LookupKey = types.NamespacedName{Namespace: mirrorPeer1.Namespace, Name: mirrorPeer1.Name}
	mcAddOn1LookupKey    = types.NamespacedName{Namespace: "cluster1", Name: tokenexchange.TokenExchangeName}
	mcAddOn2LookupKey    = types.NamespacedName{Namespace: "cluster2", Name: tokenexchange.TokenExchangeName}
	mcAddOn1             = addonapiv1alpha1.ManagedClusterAddOn{}
	mcAddOn2             = addonapiv1alpha1.ManagedClusterAddOn{}
)

var _ = Describe("ManagedClusterAddOn creation, updation and deletion", func() {
	When("creating or updating ManagedClusterAddOn", func() {
		BeforeEach(func() {
			newMirrorPeer := mirrorPeer1.DeepCopy()
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "cluster1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
					{
						ClusterName: "cluster2",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
				},
			}
			managedcluster1 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
			}
			managedcluster2 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster2",
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

			err = k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer1.DeepCopy()
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})

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
		})
	})
})
