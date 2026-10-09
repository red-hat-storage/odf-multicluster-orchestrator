package utils

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
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
