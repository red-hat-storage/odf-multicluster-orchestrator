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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var _ = Describe("MirrorPeer Status Tests", func() {

	When("Source secrets have not been created", func() {

		namespace1 := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-managed-cluster-1",
			},
		}
		namespace2 := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-managed-cluster-2",
			},
		}
		managedcluster1 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-managed-cluster-1",
			},
		}
		managedcluster2 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-managed-cluster-2",
			},
		}
		fakeMirrorPeer := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "fake-mirror-peer-2",
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "new-managed-cluster-1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster1",
							Namespace: "test-ns",
						},
					},
					{
						ClusterName: "new-managed-cluster-2",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster2",
							Namespace: "test-ns",
						},
					},
				},
			},
		}

		BeforeEach(func() {
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), fakeMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

		})

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), fakeMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have Phase: ExchangingSecret", func() {
			Eventually(func() multiclusterv1alpha1.PhaseType {
				var mp multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      fakeMirrorPeer.Name,
					Namespace: fakeMirrorPeer.Namespace,
				}, &mp)
				Expect(err).NotTo(HaveOccurred())
				return mp.Status.Phase
			}, 10*time.Second, 2*time.Second).Should(Equal(multiclusterv1alpha1.ExchangingSecret))

		})

	})

	When("Source secrets have been created", func() {

		namespace1 := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster1",
			},
		}
		namespace2 := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster2",
			},
		}
		managedcluster1 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster1",
			},
		}
		managedcluster2 := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "managed-cluster2",
			},
		}

		fakeMirrorPeer := &multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "fake-mirror-peer",
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "managed-cluster1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster1",
							Namespace: "test-ns",
						},
					},
					{
						ClusterName: "managed-cluster2",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster2",
							Namespace: "test-ns",
						},
					},
				},
			},
		}

		BeforeEach(func() {
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			pr1 := fakeMirrorPeer.Spec.Items[0]
			pr2 := fakeMirrorPeer.Spec.Items[1]

			secretNN1 := types.NamespacedName{
				Name:      common.CreateUniqueSecretName(pr1.ClusterName, pr1.StorageClusterRef.Namespace, pr1.StorageClusterRef.Name),
				Namespace: pr1.ClusterName,
			}

			storageClusterNN1 := types.NamespacedName{
				Name:      pr1.StorageClusterRef.Name,
				Namespace: pr1.StorageClusterRef.Namespace,
			}

			sec1 := common.CreateSourceSecret(secretNN1, storageClusterNN1, []byte("SecretData1"), common.RookOrigin)

			secretNN2 := types.NamespacedName{
				Name:      common.CreateUniqueSecretName(pr2.ClusterName, pr2.StorageClusterRef.Namespace, pr2.StorageClusterRef.Name),
				Namespace: pr2.ClusterName,
			}

			storageClusterNN2 := types.NamespacedName{
				Name:      pr2.StorageClusterRef.Name,
				Namespace: pr2.StorageClusterRef.Namespace,
			}

			sec2 := common.CreateSourceSecret(secretNN2, storageClusterNN2, []byte("SecretData2"), common.RookOrigin)

			err = k8sClient.Create(context.TODO(), sec1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), sec2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), fakeMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

		})

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), fakeMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have Phase: ExchangedSecret", func() {
			Eventually(func() multiclusterv1alpha1.PhaseType {
				var mp multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      fakeMirrorPeer.Name,
					Namespace: fakeMirrorPeer.Namespace,
				}, &mp)
				Expect(err).NotTo(HaveOccurred())
				return mp.Status.Phase
			}, 5*time.Minute, 2*time.Second).Should(Equal(multiclusterv1alpha1.ExchangedSecret))

		})
	})
})
