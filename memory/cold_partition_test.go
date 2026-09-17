package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// discoveryKV is a mockKV with the optional ListPartitionIDs capability —
// the same shape LocalFileKV exposes (type-asserted by Init, NOT part of the
// KVStore six-method interface).
type discoveryKV struct {
	*mockKV
	ids []int
}

func (d *discoveryKV) ListPartitionIDs() []int { return d.ids }

// TestFileSegmentStore_ColdPartitionDiscovery (implementation-hardening 2.4):
// the forgetting scans (TTL / capacity / compaction) Range over
// store.partitions — a partition this process never wrote to used to stay
// invisible to them forever (Init() was an empty function and had no
// production caller). NewFileSegmentStore must now discover persisted
// partitions at construction via the kv backend's optional ListPartitionIDs
// capability, and a backend WITHOUT the capability must keep the old
// lazy-discovery behavior (known limitation, logged) instead of failing.
func TestFileSegmentStore_ColdPartitionDiscovery(t *testing.T) {
	base := newMockKV()
	// Seed one persisted event + one segment-meta key in partition 7 — any
	// key in a partition's namespace proves the partition exists.
	base.data["7:evt:1710676800:1"] = "{}"
	base.data["7:meta:1710676800"] = "{}"

	s, err := NewFileSegmentStore(&discoveryKV{mockKV: base, ids: []int{7}}, nil, ":memory:", 100)
	require.NoError(t, err)
	if _, ok := s.partitions.Load(7); !ok {
		t.Fatal("cold partition 7 not discovered at construction — forgetting scans would skip it forever")
	}
}

// TestFileSegmentStore_NoEnumerationBackend_KeepsLazyDiscovery: a backend
// without ListPartitionIDs must not break construction (known limitation —
// e.g. rustviking today), matching the pre-fix behavior.
func TestFileSegmentStore_NoEnumerationBackend_KeepsLazyDiscovery(t *testing.T) {
	base := &noEnumKV{data: map[string]string{
		"7:evt:1710676800:1": "{}",
	}}

	s, err := NewFileSegmentStore(base, nil, ":memory:", 100)
	require.NoError(t, err)
	if _, ok := s.partitions.Load(7); ok {
		t.Fatal("without enumeration capability discovery must stay lazy (no partition registered)")
	}
}
