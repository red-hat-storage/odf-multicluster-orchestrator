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
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// MirrorPeerPlusPlus provides more functionalities to `MirrorPeer`
type MirrorPeerPlusPlus multiclusterv1alpha1.MirrorPeer

func (mppp MirrorPeerPlusPlus) GetManagedClusters() []clusterv1.ManagedCluster {
	var managedClusters []clusterv1.ManagedCluster
	for _, eachItem := range mppp.Spec.Items {
		managedCluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: eachItem.ClusterName,
			},
			Spec: clusterv1.ManagedClusterSpec{},
		}
		managedClusters = append(managedClusters, managedCluster)
	}
	return managedClusters
}

// GetClusterNamespaces returns all the namespaces of each StorageCluster
// in the MirrorPeer items
func (mppp MirrorPeerPlusPlus) GetClusterNamespaces() []corev1.Namespace {
	uniqueNamespaceMap := map[string]struct{}{}
	for _, eachPeer := range mppp.Spec.Items {
		uniqueNamespaceMap[eachPeer.ClusterName] = struct{}{}
	}
	var namespaceArr []corev1.Namespace
	for namespace := range uniqueNamespaceMap {
		namespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
			Spec: corev1.NamespaceSpec{
				Finalizers: []corev1.FinalizerName{},
			},
		}
		namespaceArr = append(namespaceArr, namespace)
	}
	return namespaceArr
}

// GetDummySourceSecrets generates some dummy source secrets according to the MirrorPeer items
func (mppp MirrorPeerPlusPlus) GetDummySourceSecrets(prefixName string) []corev1.Secret {
	var sourceSecrets []corev1.Secret
	for i, eachPR := range mppp.Spec.Items {
		secretNN := types.NamespacedName{
			Name:      fmt.Sprintf("%s-%d", prefixName, i),
			Namespace: eachPR.ClusterName,
		}
		storageClusterNN := types.NamespacedName{
			Name:      eachPR.StorageClusterRef.Name,
			Namespace: eachPR.StorageClusterRef.Namespace,
		}
		sourceSecret := common.CreateSourceSecret(secretNN, storageClusterNN, []byte("MySecretData1234"))
		sourceSecrets = append(sourceSecrets, *sourceSecret)
	}
	return sourceSecrets
}

var _ = Describe("MirrorPeerSecret Controller", func() {
	var (
		ctx           = context.TODO()
		mpName        = "mirrorpeer-sample"
		counter int32 = 0
	)

	var getSourceSecrets = func(ctx context.Context, rc client.Client) ([]corev1.Secret, error) {
		var sourceList corev1.SecretList
		var hasSourceLabel client.MatchingLabels = make(client.MatchingLabels)
		hasSourceLabel[common.SecretLabelTypeKey] = string(common.SourceLabel)
		err := rc.List(ctx, &sourceList, hasSourceLabel)
		return sourceList.Items, err
	}

	BeforeEach(func() {
		atomic.AddInt32(&counter, 1)
		// counter.Increment()
		items := []multiclusterv1alpha1.PeerRef{
			{
				ClusterName: fmt.Sprintf("cluster11%d", counter),
				StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
					Name:      "test-storagecluster-1",
					Namespace: "test-namespace-321",
				},
			},
			{
				ClusterName: fmt.Sprintf("cluster12%d", counter),
				StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
					Name:      "test-storagecluster-2",
					Namespace: "test-namespace-321",
				},
			},
		}

		mirrorPeer := multiclusterv1alpha1.MirrorPeer{
			ObjectMeta: metav1.ObjectMeta{
				Name: mpName,
			},
			Spec: multiclusterv1alpha1.MirrorPeerSpec{
				Items: items,
			},
		}
		mirrorPeerPP := MirrorPeerPlusPlus(mirrorPeer)

		By("creating managed clusters", func() {
			managedClusters := mirrorPeerPP.GetManagedClusters()
			for _, eachMC := range managedClusters {
				var currMC clusterv1.ManagedCluster
				mcNN := types.NamespacedName{Namespace: eachMC.Namespace, Name: eachMC.Name}
				err := k8sClient.Get(ctx, mcNN, &currMC)
				if k8serrors.IsNotFound(err) {
					err = k8sClient.Create(ctx, &eachMC)
				}
				Expect(err).To(BeNil())
			}
		})

		By("creating the namespaces", func() {
			namespaces := mirrorPeerPP.GetClusterNamespaces()
			for _, eachNspace := range namespaces {
				err := k8sClient.Create(ctx, &eachNspace)
				if k8serrors.IsAlreadyExists(err) {
					err = nil
				}
				Expect(err).To(BeNil())
			}
		})

		By("creating the MirrorPeer", func() {
			mirrorPeer := multiclusterv1alpha1.MirrorPeer(mirrorPeerPP)
			err := k8sClient.Create(ctx, &mirrorPeer)
			Expect(err).To(BeNil())
		})

		By("creating the Source secrets", func() {
			sourceSecrets := mirrorPeerPP.GetDummySourceSecrets("my-source-secret")
			for _, sourceSecret := range sourceSecrets {
				err := k8sClient.Create(ctx, &sourceSecret)
				Expect(err).To(BeNil())
				time.Sleep(500 * time.Millisecond)
			}
		})
	})

	// when a source secret exists, it should have created corresponding destination secrets
	When("Source secrets exist", func() {
		// and it should have created the destination secrets
		It("should create corresponding destination secrets", func() {
			var sourceSecrets []corev1.Secret
			var err error
			By("checking the source secrets exist", func() {
				sourceSecrets, err = getSourceSecrets(ctx, k8sClient)
				Expect(err).ShouldNot(HaveOccurred())
				// sources should not be nil
				Expect(sourceSecrets).NotTo(BeEmpty())
			})

			By("each source has a corresponding destination secret", func() {
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					for _, eachPeer := range peers {
						var destinationSecret corev1.Secret
						destinationNN := types.NamespacedName{Name: eachSource.Name, Namespace: eachPeer.ClusterName}
						err = k8sClient.Get(ctx, destinationNN, &destinationSecret)
						Expect(err).To(BeNil())
					}
				}
			})
		})
	})

	// when source secrets are changed,
	// corresponding destination secrets should have been updated
	When("updating the source secrets", func() {
		It("should update the destination secrets", func() {
			By("update each source secret", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())

				newSecretData := "My-New-Secret-Data-123"
				for i, eachSource := range sourceSecrets {
					eachSource.Data[common.SecretDataKey] = []byte(fmt.Sprintf("%s-%d", newSecretData, i))
					err := k8sClient.Update(ctx, &eachSource)
					Expect(err).To(BeNil())
					time.Sleep(500 * time.Millisecond)
				}
			})

			By("comparing each destination secret data to it's source", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).ToNot(BeEmpty())
					for _, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Name: eachSource.Name, Namespace: eachPeer.ClusterName}
						err = k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						Expect(destSecret.Data[common.SecretDataKey]).To(BeEquivalentTo(eachSource.Data[common.SecretDataKey]))
					}
				}
			})
		})
	})

	// when deleting a source secret, it's matching destination should also be deleted
	When("deleting the source secrets", func() {
		It("should delete the corresponding destination secrets", func() {
			By("deleting each source secret", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					err := k8sClient.Delete(ctx, &eachSource)
					Expect(err).To(BeNil())
				}
			})

			By("checking whether each destination exists or not", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).ToNot(BeNil())
					Expect(peers).NotTo(BeEmpty())
					for _, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Name: eachSource.Name, Namespace: eachPeer.ClusterName}
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).NotTo(BeNil())
						Expect(k8serrors.IsNotFound(err)).To(BeTrue())
					}
				}
			})
		})
	})

	// when destination secrets are updated,
	// they should be reverted back to it's original form
	When("destination secret data is updated", func() {
		It("should be restored to it's original", func() {
			By("update each destination secret and reconcile", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).NotTo(BeEmpty())
					for i, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Namespace: eachPeer.ClusterName, Name: eachSource.Name}
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						destSecret.Data[common.SecretDataKey] = []byte(fmt.Sprintf("something-new-%d", i))
						err = k8sClient.Update(ctx, &destSecret)
						Expect(err).To(BeNil())
					}
				}
			})

			By("checking each destination secret is restored back", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).NotTo(BeEmpty())
					for _, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Namespace: eachPeer.ClusterName, Name: eachSource.Name}
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						Expect(destSecret.Data[common.SecretDataKey]).
							To(BeEquivalentTo(eachSource.Data[common.SecretDataKey]))
					}
				}
			})
		})
	})

	// when destination secrets are deleted, it should be restored
	When("destination secrets are deleted", func() {
		It("should be restored from it's corresponding source", func() {
			sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
			Expect(err).To(BeNil())
			var mpList multiclusterv1alpha1.MirrorPeerList
			err = k8sClient.List(ctx, &mpList)
			Expect(err).To(BeNil())

			By("deleting each destination secret and reconciling", func() {
				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).NotTo(BeEmpty())
					for _, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Namespace: eachPeer.ClusterName, Name: eachSource.Name}
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						err = k8sClient.Delete(ctx, &destSecret)
						Expect(err).To(BeNil())
						time.Sleep(500 * time.Millisecond)
					}
				}
			})

			By("checking whether each destination is restored", func() {
				var mpList multiclusterv1alpha1.MirrorPeerList
				err = k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					Expect(peers).NotTo(BeEmpty())
					for _, eachPeer := range peers {
						var destSecret corev1.Secret
						destNN := types.NamespacedName{Namespace: eachPeer.ClusterName, Name: eachSource.Name}
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						Expect(destSecret.Data[common.SecretDataKey]).
							To(BeEquivalentTo(eachSource.Data[common.SecretDataKey]))
					}
				}
			})
		})
	})
	// when the MirrorPeer CR is updated,
	// corresponding new destination secrets should be created
	// and old destination secrets should not be lingering around.
	When("MirrorPeer CR is updated", func() {
		// var oldPeer *multiclusterv1alpha1.PeerRef
		newPeer := multiclusterv1alpha1.PeerRef{
			ClusterName: "cluster13",
			StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
				Name:      "storagecluster-3",
				Namespace: "test-namespace-3",
			},
		}

		It("corresponding destination secrets should be created and updated", func() {
			By("confirming the current source secrets", func() {
				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				Expect(sourceSecrets).ToNot(BeEmpty())
			})

			By("confirming the current destination secrets", func() {
				var mpList multiclusterv1alpha1.MirrorPeerList
				err := k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				sourceSecrets, err := getSourceSecrets(ctx, k8sClient)
				Expect(err).To(BeNil())
				for _, eachSource := range sourceSecrets {
					peers, err := controllers.PeersConnectedToSecret(&eachSource, mpList.Items)
					Expect(err).To(BeNil())
					for _, eachPeer := range peers {
						var destinationSecret corev1.Secret
						destinationNN := types.NamespacedName{Name: eachSource.Name, Namespace: eachPeer.ClusterName}
						err = k8sClient.Get(ctx, destinationNN, &destinationSecret)
						Expect(err).To(BeNil())
					}
				}
			})

			By("creating the pre-requisite resources", func() {
				var mirrorPeerPP MirrorPeerPlusPlus
				var mirrorPR multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(ctx, types.NamespacedName{Name: mpName}, &mirrorPR)
				Expect(err).To(BeNil())
				mirrorPeerPP = MirrorPeerPlusPlus(mirrorPR)
				// oldPeer = mirrorPR.Spec.Items[1].DeepCopy()
				mirrorPR.Spec.Items[1] = newPeer
				Expect(mirrorPR.Spec.Items).To(BeEquivalentTo(mirrorPeerPP.Spec.Items))
				clusterNamespaces := mirrorPeerPP.GetClusterNamespaces()
				for _, ns := range clusterNamespaces {
					var existingNS corev1.Namespace
					if err = k8sClient.Get(ctx,
						types.NamespacedName{Name: ns.Name}, &existingNS); err == nil {
						continue
					}
					Expect(k8serrors.IsNotFound(err)).To(BeTrue())
					err = k8sClient.Create(ctx, &ns)
					Expect(err).To(BeNil())
				}
				mgrClusters := mirrorPeerPP.GetManagedClusters()
				for _, eachMgrCluster := range mgrClusters {
					var mgrCluster clusterv1.ManagedCluster
					err = k8sClient.Get(ctx, types.NamespacedName{
						Name:      eachMgrCluster.Name,
						Namespace: eachMgrCluster.Namespace}, &mgrCluster)
					if err == nil {
						continue
					}
					Expect(k8serrors.IsNotFound(err)).To(BeTrue())
					err = k8sClient.Create(ctx, &eachMgrCluster)
					Expect(err).To(BeNil())
				}
				sourceNN := types.NamespacedName{Name: "source-3", Namespace: newPeer.ClusterName}
				storageClusterNN := types.NamespacedName{Name: newPeer.StorageClusterRef.Name, Namespace: newPeer.StorageClusterRef.Namespace}
				newSource := common.CreateSourceSecret(sourceNN, storageClusterNN, []byte("new-secret-3"))
				err = k8sClient.Create(ctx, newSource)
				Expect(err).To(BeNil())
				err = k8sClient.Update(ctx, &mirrorPR)
				Expect(err).To(BeNil())
				time.Sleep(500 * time.Millisecond)
			})

			By("confirming all destination secrets are created", func() {
				var mirrorPR multiclusterv1alpha1.MirrorPeer
				err := k8sClient.Get(ctx, types.NamespacedName{Name: mpName}, &mirrorPR)
				Expect(err).To(BeNil())
				mirrorPeerPP := MirrorPeerPlusPlus(mirrorPR)
				mpList := []multiclusterv1alpha1.MirrorPeer{mirrorPR}

				for _, eachPeer := range mirrorPeerPP.Spec.Items {
					var sourceList corev1.SecretList
					var hasSourceLabel client.MatchingLabels = make(client.MatchingLabels)
					hasSourceLabel[common.SecretLabelTypeKey] = string(common.SourceLabel)
					var inNamespace client.InNamespace = client.InNamespace(eachPeer.ClusterName)
					err := k8sClient.List(ctx, &sourceList, hasSourceLabel, inNamespace)
					Expect(err).To(BeNil())
					Expect(sourceList.Items).NotTo(BeEmpty())
					Expect(len(sourceList.Items) == 1).To(BeTrue())
					sourceSecret := sourceList.Items[0]
					connectedPeers := controllers.PeersConnectedToPeerRef(eachPeer, mpList)
					for _, eachConnectedPeer := range connectedPeers {
						destNN := types.NamespacedName{Namespace: eachConnectedPeer.ClusterName, Name: sourceSecret.Name}
						var destSecret corev1.Secret
						err := k8sClient.Get(ctx, destNN, &destSecret)
						Expect(err).To(BeNil())
						Expect(destSecret.Data[common.SecretDataKey]).To(BeEquivalentTo(sourceSecret.Data[common.SecretDataKey]))
						Expect(destSecret.Labels).To(HaveKey(common.SecretLabelTypeKey))
						Expect(destSecret.Labels[common.SecretLabelTypeKey]).To(BeEquivalentTo(common.DestinationLabel))
					}
				}
			})

			By("should remove any orphan destination secrets", func() {
				time.Sleep(500 * time.Millisecond)
				var mpList multiclusterv1alpha1.MirrorPeerList
				err := k8sClient.List(ctx, &mpList)
				Expect(err).To(BeNil())

				var destList corev1.SecretList
				var hasDestinationLabel client.MatchingLabels = make(client.MatchingLabels)
				hasDestinationLabel[common.SecretLabelTypeKey] = string(common.DestinationLabel)
				err = k8sClient.List(ctx, &destList, hasDestinationLabel)
				Expect(err).To(BeNil())
				for _, eachDestSecret := range destList.Items {
					peerRef, err := common.CreatePeerRefFromSecret(&eachDestSecret)
					Expect(err).To(BeNil())
					connectedPeers := controllers.PeersConnectedToPeerRef(peerRef, mpList.Items)
					Expect(connectedPeers).NotTo(BeEmpty())
					var sourceListOptions []client.ListOption
					for _, eachConnectedPeer := range connectedPeers {
						var inNamespace client.InNamespace = client.InNamespace(eachConnectedPeer.ClusterName)
						sourceListOptions = append(sourceListOptions, inNamespace)
					}
					var hasSourceLabel client.MatchingLabels = make(client.MatchingLabels)
					Expect(err).To(BeNil())
					hasSourceLabel[common.SecretLabelTypeKey] = string(common.SourceLabel)

					sourceListOptions = append(sourceListOptions, hasSourceLabel)
					var allConnectedSources corev1.SecretList
					err = k8sClient.List(ctx, &allConnectedSources, sourceListOptions...)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(allConnectedSources.Items).NotTo(BeEmpty())
					var oneOfTheNameShouldMatch bool
					for _, eachConnectedSource := range allConnectedSources.Items {
						if eachConnectedSource.Name == eachDestSecret.Name {
							oneOfTheNameShouldMatch = true
							break
						}
					}
					Expect(oneOfTheNameShouldMatch).To(BeTrue())
				}
			})
		})
	})

	AfterEach(func() {
		By("removing the Source secrets", func() {
			var sourceList corev1.SecretList
			var hasSourceLabel client.MatchingLabels = make(client.MatchingLabels)
			hasSourceLabel[common.SecretLabelTypeKey] = string(common.SourceLabel)
			err := k8sClient.List(ctx, &sourceList, hasSourceLabel)
			Expect(err).ShouldNot(HaveOccurred())

			for _, sourceSecret := range sourceList.Items {
				err := k8sClient.Delete(ctx, &sourceSecret)
				Expect(err).To(BeNil())
			}
		})

		By("deleting managed clusters", func() {
			// managedClusters := mirrorPeerPP.GetManagedClusters()
			var mcList clusterv1.ManagedClusterList
			err := k8sClient.List(ctx, &mcList)
			Expect(err).ShouldNot(HaveOccurred())
			for _, eachMC := range mcList.Items {
				err = k8sClient.Delete(ctx, &eachMC)
				Expect(err).ShouldNot(HaveOccurred())
			}
		})

		By("making sure all the destination secrets are also removed", func() {
			time.Sleep(1 * time.Second)
			var destList corev1.SecretList
			var hasDestLabel client.MatchingLabels = make(client.MatchingLabels)
			hasDestLabel[common.SecretLabelTypeKey] = string(common.DestinationLabel)
			err := k8sClient.List(ctx, &destList, hasDestLabel)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(destList.Items).To(BeEmpty())
		})

		By("removing all the namespaces", func() {
			var mirrorPeer multiclusterv1alpha1.MirrorPeer
			mpNN := types.NamespacedName{Name: mpName}
			err := k8sClient.Get(ctx, mpNN, &mirrorPeer)
			Expect(err).ShouldNot(HaveOccurred())
			mirrorPeerPP := MirrorPeerPlusPlus(mirrorPeer)
			namespaces := mirrorPeerPP.GetClusterNamespaces()
			for _, eachNspace := range namespaces {
				err := k8sClient.Delete(ctx, &eachNspace)
				Expect(err).ShouldNot(HaveOccurred())
			}
		})

		By("removing the MirrorPeer", func() {
			var mirrorPeer multiclusterv1alpha1.MirrorPeer
			mpNN := types.NamespacedName{Name: mpName}
			err := k8sClient.Get(ctx, mpNN, &mirrorPeer)
			Expect(err).ShouldNot(HaveOccurred())
			err = k8sClient.Delete(ctx, &mirrorPeer)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
