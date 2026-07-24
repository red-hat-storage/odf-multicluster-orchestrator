package backend

import (
	"context"
	"log/slog"
	"sync"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StorageBackend interface {
	Name() string
	ClusterClaimKey() string

	// Phase 1: backend-specific replication setup.
	EnsureReplicationReady(ctx context.Context, c client.Client, logger *slog.Logger,
		mirrorPeer *multiclusterv1alpha1.MirrorPeer, clientInfoMap map[string]string) (bool, error)

	// Phase 2: resolve S3 credentials into a clean S3Profile.
	GetS3Profile(ctx context.Context, c client.Client, scheme *runtime.Scheme,
		peerRef multiclusterv1alpha1.PeerRef, mirrorPeer *multiclusterv1alpha1.MirrorPeer,
		clientInfoMap map[string]string, ramenNamespace string, logger *slog.Logger) (*utils.S3Profile, string, error)

	// Cleanup on MirrorPeer deletion.
	DeleteStorageBackendResources(ctx context.Context, c client.Client,
		mirrorPeer *multiclusterv1alpha1.MirrorPeer, logger *slog.Logger) error
}

var (
	mu       sync.RWMutex
	byName   = map[string]StorageBackend{}
	byClaim  = map[string]StorageBackend{}
)

func Register(be StorageBackend) {
	mu.Lock()
	defer mu.Unlock()
	byName[be.Name()] = be
	byClaim[be.ClusterClaimKey()] = be
}

func Get(name string) (StorageBackend, bool) {
	mu.RLock()
	defer mu.RUnlock()
	be, ok := byName[name]
	return be, ok
}

func ForClusterClaim(claimKey string) (StorageBackend, bool) {
	mu.RLock()
	defer mu.RUnlock()
	be, ok := byClaim[claimKey]
	return be, ok
}

func AllForClaims(claimKeys []string) []StorageBackend {
	mu.RLock()
	defer mu.RUnlock()
	seen := map[string]struct{}{}
	var result []StorageBackend
	for _, key := range claimKeys {
		if be, ok := byClaim[key]; ok {
			if _, dup := seen[be.Name()]; !dup {
				seen[be.Name()] = struct{}{}
				result = append(result, be)
			}
		}
	}
	return result
}
