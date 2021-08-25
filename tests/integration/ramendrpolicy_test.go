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
	"sigs.k8s.io/controller-runtime/pkg/client"

	ramendrv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	mirrorPeerObject = multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer-test",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
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
		},
	}
)

var _ = Describe("RamenDR Policy creation, updation, and deletion", func() {
	When("Creating and updating RamenDR Policy", func() {
		It("should not throw validation errors", func() {
			ramenDRPolicy := ramendrv1alpha1.DRPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DRPolicy",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "drpolicy",
				},
			}
			By("creating MirrorPeer", func() {
				err := k8sClient.Create(context.TODO(), &mirrorPeerObject, &client.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
			By("fetching RamenDR Policy which was created automatically", func() {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ramenDRPolicy.GenerateName}, &ramenDRPolicy)
				Expect(err).NotTo(HaveOccurred())
			})
			By("Updating RamenDR Policy", func() {
				By("updating SchedulingInterval", func() {
					ramenDRPolicy.Spec.SchedulingInterval = "2h"
					err := k8sClient.Update(context.TODO(), &ramenDRPolicy, &client.UpdateOptions{})
					Expect(err).NotTo(HaveOccurred())
				})
				By("updating ReplicationClassSelector", func() {
					ramenDRPolicy.Spec.ReplicationClassSelector = metav1.LabelSelector{
						MatchLabels: map[string]string{
							"class": "ramendr",
						},
					}
					err := k8sClient.Update(context.TODO(), &ramenDRPolicy, &client.UpdateOptions{})
					Expect(err).NotTo(HaveOccurred())
				})
				By("updating DRClusterSet", func() {
					ramenDRPolicy.Spec.DRClusterSet = []ramendrv1alpha1.ManagedCluster{
						{Name: "cluster-a", S3ProfileName: ""},
						{Name: "cluster-b", S3ProfileName: ""},
						{Name: "cluster-c", S3ProfileName: ""},
					}
					err := k8sClient.Update(context.TODO(), &ramenDRPolicy, &client.UpdateOptions{})
					Expect(err).NotTo(HaveOccurred())
				})
			})
			By("cleaning up the MirrorPeer instance", func() {
				err := k8sClient.Delete(context.TODO(), &mirrorPeerObject, &client.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
