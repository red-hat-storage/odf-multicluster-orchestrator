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

package controllers

import (
	"context"
	"fmt"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func undefinedMirrorPeerSpec(spec multiclusterv1alpha1.MirrorPeerSpec) error {
	if reflect.DeepEqual(spec, multiclusterv1alpha1.MirrorPeerSpec{}) {
		return fmt.Errorf("validation: MirrorPeer.Spec must not be empty")
	}
	return nil
}

func uniqueSpecItems(spec multiclusterv1alpha1.MirrorPeerSpec) error {
	if reflect.DeepEqual(spec.Items[0], spec.Items[1]) {
		return fmt.Errorf("validation: MirrorPeer.Spec.Items fields must be unique within a MirrorPeer object")
	}
	return nil
}

func emptySpecItems(peerRef multiclusterv1alpha1.PeerRef) error {
	if peerRef.ClusterName == "" || peerRef.StorageClusterRef.Name == "" || peerRef.StorageClusterRef.Namespace == "" {
		return fmt.Errorf("validation: MirrorPeer.Spec.Items fields must not be empty or undefined")
	}
	return nil
}

func isManagedCluster(ctx context.Context, client client.Client, clusterName string) error {
	var mcluster clusterv1.ManagedCluster
	err := client.Get(ctx, types.NamespacedName{Name: clusterName}, &mcluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("validation: ManagedCluster %q not found : %q is not a managed cluster", clusterName, clusterName)
		}
		return fmt.Errorf("validation: unable to get ManagedCluster %q: error: %v", clusterName, err)
	}
	return nil
}
