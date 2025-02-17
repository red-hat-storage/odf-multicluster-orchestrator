package utils

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
)

/*
fnv64a is a 64-bit non-cryptographic hash algorithm with a low collision and a high distribution rate.
https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function
*/
func FnvHash(s string) uint32 {
	h := fnv.New32a()
	_, err := h.Write([]byte(s))
	if err != nil {
		return 0
	}
	return h.Sum32()
}

// CreateUniqueName function creates a sha512 hex sum from the given parameters
func CreateUniqueName(params ...string) string {
	genStr := strings.Join(params, "-")
	return fmt.Sprintf("%x", sha512.Sum512([]byte(genStr)))
}

// CreateUniqueSecretName function creates a name of 40 chars using sha512 hex sum from the given parameters
func CreateUniqueSecretName(managedCluster, storageClusterNamespace, storageClusterName string, prefix ...string) string {
	if len(prefix) > 0 {
		return CreateUniqueName(prefix[0], managedCluster, storageClusterNamespace, storageClusterName)[0:39]
	}
	return CreateUniqueName(managedCluster, storageClusterNamespace, storageClusterName)[0:39]
}

func CreateUniqueSecretNameForClient(providerKey, clientKey1, clientKey2 string) string {
	return CreateUniqueName(providerKey, clientKey1, clientKey2)[0:39]
}

func CreateUniqueReplicationId(storageIds ...string) (string, error) {
	if len(storageIds) < 2 {
		return "", fmt.Errorf("replicationID can not be generated due to missing cluster StorageIds")
	}

	sort.Strings(storageIds)

	id := CalculateMD5Hash(storageIds)
	return id, nil
}

func GenerateUniqueIdForMirrorPeer(mirrorPeer multiclusterv1alpha1.MirrorPeer) string {
	var checksum [20]byte
	var peerAccumulator []string
	for _, peer := range mirrorPeer.Spec.Items {
		peerAccumulator = append(peerAccumulator, GetKey(peer.ClusterName, peer.StorageClusterRef.Name))
	}
	sort.Strings(peerAccumulator)
	checksum = sha1.Sum([]byte(strings.Join(peerAccumulator, "-")))
	// truncate to bucketGenerateName + "-" + first 12 (out of 20) byte representations of sha1 checksum
	return hex.EncodeToString(checksum[:])
}

func GetKey(clusterName, clientName string) string {
	return fmt.Sprintf("%s_%s", clusterName, clientName)
}

func CalculateMD5Hash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		errStr := fmt.Errorf("failed to marshal for %#v", value)
		panic(errStr)
	}
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}
