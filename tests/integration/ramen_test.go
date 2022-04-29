//go:build integration

/*
Copyright 2022 Red Hat OpenShift Data Foundation.

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
	ramenv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var _ = Describe("Ramen Resource Tests", func() {

	namespace1 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-1",
		},
	}
	namespace2 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-2",
		},
	}
	managedcluster1 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-1",
		},
	}
	managedcluster2 := clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc-2",
		},
	}
	fakeMirrorPeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-mirror-peer-3",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type: "async",
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "mc-1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster1",
						Namespace: "test-ns",
					},
				},
				{
					ClusterName: "mc-2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster2",
						Namespace: "test-ns",
					},
				},
			},
		},
	}

	pr1 := fakeMirrorPeer.Spec.Items[0]
	pr2 := fakeMirrorPeer.Spec.Items[1]

	secretNN1 := types.NamespacedName{
		Name:      utils.GetSecretNameByPeerRef(pr1),
		Namespace: pr1.ClusterName,
	}

	storageClusterNN1 := types.NamespacedName{
		Name:      pr1.StorageClusterRef.Name,
		Namespace: pr1.StorageClusterRef.Namespace,
	}

	sec1 := utils.CreateSourceSecret(secretNN1, storageClusterNN1, []byte(`{"cluster":"b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy","token":"ZXlKbWMybGtJam9pWXpSak56SmpNRE10WXpCbFlpMDBZMlppTFRnME16RXRNekExTmpZME16UmxZV1ZqSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkVkbGxyTldrM04xbG9TMEpCUVZZM2NFZHlVVXBrU1VvelJtZGpjVWxGVUZWS0wzYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak13TGpFd01TNHlORGs2TmpjNE9Td3hOekl1TXpBdU1UZ3pMakU1TURvMk56ZzVMREUzTWk0ek1DNHlNak11TWpFd09qWTNPRGtpTENKdVlXMWxjM0JoWTJVaU9pSnZjR1Z1YzJocFpuUXRjM1J2Y21GblpTSjk="}`), utils.OriginMap["RookOrigin"])

	secretNN2 := types.NamespacedName{
		Name:      utils.GetSecretNameByPeerRef(pr2),
		Namespace: pr2.ClusterName,
	}

	storageClusterNN2 := types.NamespacedName{
		Name:      pr2.StorageClusterRef.Name,
		Namespace: pr2.StorageClusterRef.Namespace,
	}

	sec2 := utils.CreateSourceSecret(secretNN2, storageClusterNN2, []byte(`{"cluster":"b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy","token":"ZXlKbWMybGtJam9pWXpSak56SmpNRE10WXpCbFlpMDBZMlppTFRnME16RXRNekExTmpZME16UmxZV1ZqSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkVkbGxyTldrM04xbG9TMEpCUVZZM2NFZHlVVXBrU1VvelJtZGpjVWxGVUZWS0wzYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak13TGpFd01TNHlORGs2TmpjNE9Td3hOekl1TXpBdU1UZ3pMakU1TURvMk56ZzVMREUzTWk0ek1DNHlNak11TWpFd09qWTNPRGtpTENKdVlXMWxjM0JoWTJVaU9pSnZjR1Z1YzJocFpuUXRjM1J2Y21GblpTSjk="}`), utils.OriginMap["RookOrigin"])

	s3sec1 := GetFakeS3SecretForPeerRef(pr1)
	s3sec2 := GetFakeS3SecretForPeerRef(pr2)

	When("MirrorPeer is reconciled", func() {

		BeforeEach(func() {
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), sec1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), sec2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), s3sec1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), s3sec2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), fakeMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			// giving time for the resources to be created
			time.Sleep(1 * time.Second)
		})

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), &managedcluster1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(context.TODO(), sec1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), sec2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), s3sec1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), s3sec2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), fakeMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			// giving time for the resources to be destroyed
			time.Sleep(1 * time.Second)
		})

		It("should create DRClusters", func() {
			hubClusterSecrets := []*corev1.Secret{sec1, sec2}
			s3ClusterSecrets := []*corev1.Secret{s3sec1, s3sec2}
			for i, pr := range fakeMirrorPeer.Spec.Items {
				dc := &ramenv1alpha1.DRCluster{}
				hsec := hubClusterSecrets[i]
				ssec := s3ClusterSecrets[i]
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pr.ClusterName, Namespace: "default"}, dc)
				Expect(err).NotTo(HaveOccurred())
				var hubSecret corev1.Secret
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: hsec.Name, Namespace: hsec.Namespace}, &hubSecret)
				Expect(err).NotTo(HaveOccurred())
				var s3Secret corev1.Secret
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: ssec.Name, Namespace: ssec.Namespace}, &s3Secret)
				Expect(err).NotTo(HaveOccurred())
				rookToken, err := utils.UnmarshalHubSecret(&hubSecret)
				Expect(err).NotTo(HaveOccurred())
				s3Token, err := utils.UnmarshalS3Secret(&s3Secret)
				Expect(err).NotTo(HaveOccurred())
				Expect(dc.Spec.S3ProfileName).To(Equal(s3Token.S3ProfileName))
				Expect(string(dc.Spec.Region)).To(Equal(rookToken.FSID))
			}
		})
	})
})
