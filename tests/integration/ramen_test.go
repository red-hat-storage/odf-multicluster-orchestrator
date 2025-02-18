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
	viewv1beta1 "github.com/stolostron/multicloud-operators-foundation/pkg/apis/view/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Ramen Resource Tests", func() {
	odfClientInfoConfigMap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "odf-client-info",
			Namespace: "openshift-operators-ramen",
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
			"cluster1_test-storagecluster": `
{
    "clusterId": "cluster1",
    "name": "cluster1",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "cluster1",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluster1",
    "clientId": "client-1"
}
`,
			"cluster2_test-storagecluster": `
{
    "clusterId": "cluster2",
    "name": "cluster2",
    "providerInfo": {
        "version": "4.19.0",
        "deploymentType": "internal",
        "storageSystemName": "odf-storagesystem",
        "providerManagedClusterName": "cluster2",
        "namespacedName": {
            "namespace": "test-namespace",
            "name": "test-storagecluster"
        },
        "storageProviderEndpoint": "fake-endpoint.svc",
        "cephClusterFSID": "fsid",
        "storageProviderPublicEndpoint": "fake-endpoint.svc.cluster.local"
    },
    "clientManagedClusterName": "cluter2",
    "clientId": "client-2"
}
`,
		},
	}

	namespace1 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
	}
	namespace2 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
		},
	}
	namespace3 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-operators-ramen",
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

	fakeMirrorPeer := &multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-mirror-peer-3",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			Type: "async",
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: "cluster1",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-ns",
					},
				},
				{
					ClusterName: "cluster2",
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      "test-storagecluster",
						Namespace: "test-ns",
					},
				},
			},
		},
	}

	onboardingToken1 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "cluster2",
		},
		StringData: map[string]string{
			utils.SecretDataKey: `
{
    "id": "faketokenid",
    "expirationDate": "",
    "subjectRole": "client",
    "storageCluster": "test-storagecluster"
}
`,
		},
	}

	onboardingToken2 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "cluster1",
		},
		StringData: map[string]string{
			utils.SecretDataKey: `
{
    "id": "faketokenid",
    "expirationDate": "",
    "subjectRole": "client",
    "storageCluster": "test-storagecluster"
}
`,
		},
	}

	pr1 := fakeMirrorPeer.Spec.Items[0]
	pr2 := fakeMirrorPeer.Spec.Items[1]

	s3sec1 := GetFakeS3SecretForPeerRef(pr1, pr2, fakeMirrorPeer.Spec.Items[0].ClusterName)
	s3sec2 := GetFakeS3SecretForPeerRef(pr1, pr2, fakeMirrorPeer.Spec.Items[1].ClusterName)

	When("MirrorPeer is reconciled", func() {

		BeforeEach(func() {
			Expect(os.Setenv("POD_NAMESPACE", "openshift-operators-ramen")).To(BeNil())
			err := k8sClient.Create(context.TODO(), &managedcluster1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &managedcluster2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &namespace3, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), &odfClientInfoConfigMap, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), s3sec1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), s3sec2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), fakeMirrorPeer, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var found multiclusterv1alpha1.MirrorPeer
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: fakeMirrorPeer.Namespace, Name: fakeMirrorPeer.Name}, &found)
			Expect(err).NotTo(HaveOccurred())

			onboardingToken1.Name = string(found.DeepCopy().UID)
			err = k8sClient.Create(context.TODO(), onboardingToken1, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			onboardingToken2.Name = string(found.DeepCopy().UID)
			err = k8sClient.Create(context.TODO(), onboardingToken2, &client.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(1 * time.Second)
		})

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), &managedcluster1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &managedcluster2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &odfClientInfoConfigMap, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), s3sec1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), s3sec2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), fakeMirrorPeer, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), onboardingToken1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), onboardingToken2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace1, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace2, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Delete(context.TODO(), &namespace3, &client.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			Expect(os.Unsetenv("POD_NAMESPACE")).To(BeNil())
		})

		It("should create DRClusters", func() {
			s3ClusterSecrets := []*corev1.Secret{s3sec1, s3sec2}
			for i, pr := range fakeMirrorPeer.Spec.Items {
				dc := &ramenv1alpha1.DRCluster{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: pr.ClusterName}, dc)
				Expect(err).NotTo(HaveOccurred())
				ssec := s3ClusterSecrets[i]
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
