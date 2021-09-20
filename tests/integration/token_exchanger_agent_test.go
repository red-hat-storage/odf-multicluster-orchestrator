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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	tokenexchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	//cfg *rest.Config

	mirrorPeer2 = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer1",
		},
	}
	ns13 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster13",
		},
	}
	ns23 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster23",
		},
	}

	greenSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "cluster23",
			Labels: map[string]string{
				common.SecretLabelTypeKey: string(common.DestinationLabel),
			},
		},
		Data: map[string][]byte{
			"namespace":            []byte("spokeNS"),
			"secret-data":          []byte("test"),
			"storage-cluster-name": []byte("StorageCluster1"),
		},
	}
	mirrorPeer13LookupKey = types.NamespacedName{Namespace: mirrorPeer1.Namespace, Name: mirrorPeer1.Name}
	mcAddOn13LookupKey    = types.NamespacedName{Namespace: "cluster13", Name: tokenexchange.TokenExchangeName}
	mcAddOn23LookupKey    = types.NamespacedName{Namespace: "cluster23", Name: tokenexchange.TokenExchangeName}
	mcAddOn13             = addonapiv1alpha1.ManagedClusterAddOn{}
	mcAddOn23             = addonapiv1alpha1.ManagedClusterAddOn{}
)

var _ = Describe("Syncing green secret from hub to spoke", func() {
	When("creating green secret in hub", func() {
		BeforeEach(func() {
			newMirrorPeer := mirrorPeer2.DeepCopy()
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "cluster13",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
					{
						ClusterName: "cluster23",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster",
							Namespace: "test-namespace",
						},
					},
				},
			}
			managedcluster1 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster13",
				},
			}
			managedcluster2 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster23",
				},
			}
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &ns13, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns23, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer1.DeepCopy()
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), &mcAddOn13, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &mcAddOn23, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.DeleteAllOf(context.TODO(), &clusterv1.ManagedCluster{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: "",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns13, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns23, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should  green secret from hub to spoke cluster", func() {
			By("starting token exchange addons on spoke cluster", func() {
				Eventually(func() error {
					err := k8sClient.Get(context.TODO(), mcAddOn13LookupKey, &mcAddOn13)
					if err != nil {
						return err
					}
					return nil
				}, 20*time.Second, 2*time.Second).ShouldNot(HaveOccurred())

				Eventually(func() error {
					err := k8sClient.Get(context.TODO(), mcAddOn23LookupKey, &mcAddOn23)
					if err != nil {
						return err
					}
					return nil
				}, 20*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
			})

			By("syncing green secret from hub to spoke cluster", func() {
				err := k8sClient.Create(context.TODO(), &greenSecret, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() error {
					err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: "cluster13", Name: "test"}, &corev1.Secret{})
					if err != nil {
						fmt.Println(err)
						return err
					}
					return nil
				}, 20*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
			})
		})
	})
})
