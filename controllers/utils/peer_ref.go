package utils

import (
	"context"
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DoesAnotherMirrorPeerPointToPeerRef checks if another mirrorpeer is pointing to the provided peer ref
func DoesAnotherMirrorPeerPointToPeerRef(ctx context.Context, rc client.Client, peerRef *multiclusterv1alpha1.PeerRef) (bool, error) {
	mirrorPeers, err := FetchAllMirrorPeers(ctx, rc)
	if err != nil {
		return false, err
	}
	count := 0
	for i := range mirrorPeers {
		if ContainsPeerRef(mirrorPeers[i].Spec.Items, peerRef) {
			count++
		}
	}

	return count > 1, nil
}

// DoesAnotherMirrorPeerContainTheSchedulingInterval checks if a scheduling interval is being used by another mirrorpeer pointing to the same peer ref
func DoesAnotherMirrorPeerContainTheSchedulingInterval(ctx context.Context, rc client.Client, current *multiclusterv1alpha1.MirrorPeer, schedulingInterval string, peerRef *multiclusterv1alpha1.PeerRef) (bool, error) {
	mirrorPeers, err := FetchAllMirrorPeers(ctx, rc)
	if err != nil {
		return false, err
	}
	mirrorPeers = RemoveMirrorPeer(mirrorPeers, *current)
	for i := range mirrorPeers {
		if ContainsString(mirrorPeers[i].Spec.SchedulingIntervals, schedulingInterval) && ContainsPeerRef(mirrorPeers[i].Spec.Items, peerRef) {
			return true, nil
		}
	}

	return false, nil
}

// GetPeerRefForSpokeCluster returns the peer ref for the cluster name
func GetPeerRefForSpokeCluster(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) (*multiclusterv1alpha1.PeerRef, error) {
	for _, v := range mp.Spec.Items {
		if v.ClusterName == spokeClusterName {
			return &v, nil
		}
	}
	return nil, fmt.Errorf("PeerRef for cluster %s under mirrorpeer %s not found", spokeClusterName, mp.Name)
}
