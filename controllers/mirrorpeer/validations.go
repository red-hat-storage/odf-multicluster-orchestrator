/*
Copyright 2026 Red Hat Data Foundation.

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

package mirrorpeer

import (
	"context"
	"fmt"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/odf"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	if peerRef.ClusterName == "" || peerRef.StorageClusterRef.Name == "" {
		return fmt.Errorf("validation: MirrorPeer.Spec.Items fields must not be empty or undefined")
	}
	return nil
}

func isManagedCluster(ctx context.Context, client client.Client, clusterName string) error {
	var mcluster clusterv1.ManagedCluster
	err := client.Get(ctx, types.NamespacedName{Name: clusterName}, &mcluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("validation: ManagedCluster %q not found : %q is not a managed cluster", clusterName, clusterName)
		}
		return fmt.Errorf("validation: unable to get ManagedCluster %q: error: %v", clusterName, err)
	}
	return nil
}

func isVersionCompatible(peerRef multiclusterv1alpha1.PeerRef, clientInfoMap map[string]string) error {
	clientInfo, err := odf.GetClientInfoFromConfigMap(clientInfoMap, utils.GetKey(peerRef.ClusterName, peerRef.StorageClusterRef.Name))
	if err != nil {
		return fmt.Errorf("validation: unable to get client info: error: %v", err)
	}
	eq, err := utils.CompareSemverMajorMinorVersions(clientInfo.ProviderInfo.Version, version.Version, utils.Eq)
	if err != nil {
		return fmt.Errorf("validation: unable to parse versions: error: %v", err)
	}
	if !eq {
		return fmt.Errorf("validation: StorageCluster version %q on ManagedCluster %q is incompatible with Multicluster Orchestrator version %q", clientInfo.ProviderInfo.Version, peerRef.ClusterName, version.Version)
	}
	return nil
}

