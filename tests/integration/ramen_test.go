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
	"os"
	"time"

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

	s3sec1 := GetFakeS3SecretForPeerRef(pr1)
	s3sec2 := GetFakeS3SecretForPeerRef(pr2)

	When("MirrorPeer is reconciled", func() {

		BeforeEach(func() {
			os.Setenv("POD_NAMESPACE", "openshift-operators")
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace2, &client.CreateOptions{})
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

			err = k8sClient.Delete(context.TODO(), s3sec1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), s3sec2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), fakeMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			// giving time for the resources to be destroyed
			time.Sleep(1 * time.Second)
			os.Unsetenv("POD_NAMESPACE")
		})

		It("should create DRClusters", func() {
			s3ClusterSecrets := []*corev1.Secret{s3sec1, s3sec2}
			for i, pr := range fakeMirrorPeer.Spec.Items {
				dc := &ramenv1alpha1.DRCluster{}
				ssec := s3ClusterSecrets[i]
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pr.ClusterName}, dc)
				Expect(err).NotTo(HaveOccurred())
				var s3Secret corev1.Secret
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: ssec.Name, Namespace: ssec.Namespace}, &s3Secret)
				Expect(err).NotTo(HaveOccurred())
				s3Token, err := utils.UnmarshalS3Secret(&s3Secret)
				Expect(err).NotTo(HaveOccurred())
				Expect(dc.Spec.S3ProfileName).To(Equal(s3Token.S3ProfileName))
			}
		})
	})
})
