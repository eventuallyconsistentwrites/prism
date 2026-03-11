// will use xxhash, extremely fast non-cryptographic hash
package hll

import (
	"github.com/cespare/xxhash"
)

func GetHashString(s string) uint64 {
	return xxhash.Sum64String(s)
}

func GetHash(data []byte) uint64 {
	return xxhash.Sum64(data)
}
