// 契约: docs/wiki/memory/memory-architecture.md#partitionidfromname
//
// The identity to partition mapping of the memory side: one name resolves to
// one stable partition ID, so every writer and every authorized read scope
// that names it meets the same partition and no two runs disagree.
package memory

import (
	"hash/fnv"
	"sync"
)

// PartitionBits is the width of the partition range one name is folded into:
// the ten bits the segment key layout reserves for partitions.
const PartitionBits = 10

// partitionNames caches the derived partition of each name, so one name is
// hashed once and resolves to one identity for the life of the process.
var partitionNames sync.Map

// PartitionIDFromName derives the stable partition ID of one name: the FNV-1a
// hash of the name folded into the ten-bit partition range. The same name
// always derives the same ID and the derivation is cached per name.
func PartitionIDFromName(name string) int {
	if cached, held := partitionNames.Load(name); held {
		return cached.(int)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	id := int(hash.Sum32() % (1 << PartitionBits))
	partitionNames.Store(name, id)
	return id
}
