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

	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	mirrorPeer = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mirrorpeer",
		},
		// Spec to be filled manually for each individual case
	}
	ns11 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-provider-cluster1",
		},
	}
	ns22 = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-provider-cluster2",
		},
	}
	ns1a = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-provider-clustera",
		},
	}
	ns2b = v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-provider-clusterb",
		},
	}
	mirrorPeerLookupKey = types.NamespacedName{Namespace: mirrorPeer.Namespace, Name: mirrorPeer.Name}
)

func GetFakeS3SecretForPeerRef(peer multiclusterv1alpha1.PeerRef, mirrorPeer *multiclusterv1alpha1.MirrorPeer) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GetSecretNameByPeerRef(peer, utils.S3ProfilePrefix),
			Namespace: peer.ClusterName,
			Labels: map[string]string{
				"multicluster.odf.openshift.io/secret-type": "INTERNAL",
			},
			Annotations: map[string]string{
				"multicluster.odf.openshift.io/mirrorpeer": mirrorPeer.Name,
			},
		},
		Data: map[string][]byte{
			"namespace":            []byte("openshift-storage"),
			"secret-data":          []byte(`{"AWS_ACCESS_KEY_ID":"dXNlcjEyMzQ=","AWS_SECRET_ACCESS_KEY":"cGFzc3dvcmQxMjM0","s3Bucket":"b2RyYnVja2V0LWJjZjMwNDFmMjFkNw==","s3CompatibleEndpoint":"aHR0cHM6Ly9zMy1vcGVuc2hpZnQtc3RvcmFnZS5hcHBzLmh1Yi01MTY3NjNiMC0yZjQzLTRmMGYtYWI3Zi0wYzI4YjYzM2FjMTAuZGV2Y2x1c3Rlci5vcGVuc2hpZnQuY29t","s3ProfileName":"czNwcm9maWxlLWxvY2FsLWNsdXN0ZXItb2NzLXN0b3JhZ2VjbHVzdGVy","s3Region":"bm9vYmFh"}`),
			"secret-origin":        []byte("S3"),
			"storage-cluster-name": []byte("ocs-storagecluster"),
		},
	}
}

var _ = Describe("MirrorPeer Validations", func() {
	When("creating MirrorPeer", func() {
		It("should return validation error", func() {
			By("creating MirrorPeer with null spec", func() {
				newMirrorPeer := mirrorPeer.DeepCopy()
				newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-null-spec"
				err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
				Expect(err).To(HaveOccurred())
			})
			By("creating MirrorPeer with 1 Item", func() {
				newMirrorPeer := mirrorPeer.DeepCopy()
				newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-1-item"
				newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
					Type: "async",
					Items: []multiclusterv1alpha1.PeerRef{
						{
							ClusterName: "test-provider-cluster",
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-storagecluster",
								Namespace: "test-storagecluster-ns",
							},
						},
					},
				}
				err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
				Expect(err).To(HaveOccurred())
			})
			By("creating MirrorPeer without MirrorPeer.Spec.Items[*].ClusterName ", func() {
				newMirrorPeer := mirrorPeer.DeepCopy()
				newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-without-clustername"
				newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
					Type: "async",
					Items: []multiclusterv1alpha1.PeerRef{
						{
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-storagecluster-1",
								Namespace: "test-storagecluster-ns1",
							},
						},
						{
							ClusterName: "some-other-cluster",
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-storagecluster-2",
								Namespace: "test-storagecluster-ns2",
							},
						},
					},
				}
				err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
				// TODO: Check why Required field validation is not enforced by apiserver
				// Ideally, creating MirrorPeer without ClusterName should fail
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
			By("creating MirrorPeer without MirrorPeer.Spec.Items[*].StorageClusterRef ", func() {
				newMirrorPeer := mirrorPeer.DeepCopy()
				newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-without-storageclusterref"
				newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
					Type: "async",
					Items: []multiclusterv1alpha1.PeerRef{
						{
							ClusterName: "test-provider-cluster1",
						},
						{
							ClusterName: "test-provider-cluster2",
						},
					},
				}
				err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
				// TODO: Check why Required field validation is not enforced by apiserver
				// Ideally, creating MirrorPeer without StorageClusterRef should fail
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

		})

		It("should not return validation error", func() {
			By("creating MirrorPeer with all fields well defined", func() {
				newMirrorPeer := mirrorPeer.DeepCopy()
				newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-all-fields"
				newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
					Type: "async",
					Items: []multiclusterv1alpha1.PeerRef{
						{
							ClusterName: "test-provider-cluster1",
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-storagecluster-1",
								Namespace: "test-storagecluster-ns1",
							},
						},
						{
							ClusterName: "test-provider-cluster2",
							StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
								Name:      "test-storagecluster-2",
								Namespace: "test-storagecluster-ns2",
							},
						},
					},
				}
				err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	When("updating MirrorPeer", func() {
		BeforeEach(func() {
			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-update"
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Type: "async",
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-cluster1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-1",
							Namespace: "test-storagecluster-ns1",
						},
					},
					{
						ClusterName: "test-provider-cluster2",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-2",
							Namespace: "test-storagecluster-ns2",
						},
					},
				},
			}
			err := k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-update"
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should return validation error ", func() {
			By("updating MirrorPeer with len(MirrorPeer.Spec.Items) < 2", func() {
				var newMirrorPeer multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      "test-mirrorpeer-update",
					Namespace: "",
				}, &newMirrorPeer)
				Expect(err).NotTo(HaveOccurred())
				newMirrorPeer.Spec.Items = []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-cluster1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-1",
							Namespace: "test-storagecluster-ns1",
						},
					},
				}
				err = k8sClient.Update(context.TODO(), &newMirrorPeer, &client.UpdateOptions{})
				Expect(err).To(HaveOccurred())
			})
			By("updating MirrorPeer.Spec.Items to new values", func() {
				var newMirrorPeer multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      "test-mirrorpeer-update",
					Namespace: "",
				}, &newMirrorPeer)
				Expect(err).NotTo(HaveOccurred())
				newMirrorPeer.Spec.Items = []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-cluster11",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-11",
							Namespace: "test-storagecluster-ns11",
						},
					},
					{
						ClusterName: "test-provider-cluster22",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-22",
							Namespace: "test-storagecluster-ns22",
						},
					},
				}
				err = k8sClient.Update(context.TODO(), &newMirrorPeer, &client.UpdateOptions{})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	When("updating MirrorPeer", func() {
		BeforeEach(func() {
			os.Setenv("POD_NAMESPACE", "openshift-operators")
			managedcluster1 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider-clustera",
				},
				Spec: clusterv1.ManagedClusterSpec{},
			}
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			managedcluster2 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider-clusterb",
				},
				Spec: clusterv1.ManagedClusterSpec{},
			}
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &ns1a, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns2b, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-update1"
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Type: "async",
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-clustera",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-1",
							Namespace: "test-storagecluster-ns1",
						},
					},
					{
						ClusterName: "test-provider-clusterb",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-2",
							Namespace: "test-storagecluster-ns2",
						},
					},
				},
			}
			err = k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			// giving time for the resources to be created
			time.Sleep(1 * time.Second)
		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-update1"
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.DeleteAllOf(context.TODO(), &clusterv1.ManagedCluster{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: "",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), &ns1a, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns2b, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			// giving time for the resources to be destroyed
			time.Sleep(1 * time.Second)
			os.Unsetenv("POD_NAMESPACE")
		})
		It("should not return validation error ", func() {
			By("updating MirrorPeer.Spec.Items with reversed array index", func() {
				var newMirrorPeer multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      "test-mirrorpeer-update1",
					Namespace: "",
				}, &newMirrorPeer)
				Expect(err).NotTo(HaveOccurred())
				newMirrorPeer.Spec.Items = []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-clusterb",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-2",
							Namespace: "test-storagecluster-ns2",
						},
					},
					{
						ClusterName: "test-provider-clustera",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-1",
							Namespace: "test-storagecluster-ns1",
						},
					},
				}
				err = k8sClient.Update(context.TODO(), &newMirrorPeer, &client.UpdateOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

var _ = Describe("MirrorPeerReconciler Reconcile", func() {
	When("creating MirrorPeer", func() {
		BeforeEach(func() {
			managedcluster1 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider-cluster1",
				},
				Spec: clusterv1.ManagedClusterSpec{},
			}
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			managedcluster2 := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider-cluster2",
				},
				Spec: clusterv1.ManagedClusterSpec{},
			}
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &ns11, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &ns22, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-create"
			newMirrorPeer.Spec = multiclusterv1alpha1.MirrorPeerSpec{
				Type: "async",
				Items: []multiclusterv1alpha1.PeerRef{
					{
						ClusterName: "test-provider-cluster1",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-1",
							Namespace: "test-storagecluster-ns1",
						},
					},
					{
						ClusterName: "test-provider-cluster2",
						StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
							Name:      "test-storagecluster-2",
							Namespace: "test-storagecluster-ns2",
						},
					},
				},
			}
			err = k8sClient.Create(context.TODO(), newMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			newMirrorPeer := mirrorPeer.DeepCopy()
			newMirrorPeer.ObjectMeta.Name = "test-mirrorpeer-create"
			err := k8sClient.Delete(context.TODO(), newMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.DeleteAllOf(context.TODO(), &clusterv1.ManagedCluster{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: "",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), &ns11, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &ns22, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

		})
		It("should be able to read ManagedCluster object", func() {
			By("providing valid ManagedCluster names", func() {

				r := controllers.MirrorPeerReconciler{
					Client:           k8sClient,
					Scheme:           k8sClient.Scheme(),
					Logger:           utils.GetLogger(utils.GetZapLogger(true)),
					CurrentNamespace: "openshift-operators",
				}

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: "test-mirrorpeer-create",
					},
				}

				_, err := r.Reconcile(context.TODO(), req)
				Expect(err).NotTo(HaveOccurred())

			})
		})
	})
})
