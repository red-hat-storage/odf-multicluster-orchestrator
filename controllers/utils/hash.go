package utils

import (
	"crypto/sha512"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
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

func CreateUniqueReplicationId(clusterFSIDs map[string]string) (string, error) {
	var fsids []string
	for _, v := range clusterFSIDs {
		if v != "" {
			fsids = append(fsids, v)
		}
	}

	if len(fsids) < 2 {
		return "", fmt.Errorf("replicationID can not be generated due to missing cluster FSID")
	}

	// To ensure reliability of hash generation
	sort.Strings(fsids)
	return CreateUniqueName(fsids...)[0:39], nil
}
