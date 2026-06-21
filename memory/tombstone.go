package memory

import (
	"encoding/json"
	"fmt"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== TombstoneSet ====================
//
// TombstoneSet manages tombstoned EventKeys.
// Tombstones are memory-resident (map[int64]bool) and persisted
// to RustViking KV for crash recovery.

// TombstonePrefix is the KV key prefix for tombstone persistence.
// Format: {pid}:tomb:{event_key} → "1"
const tombstonePersistPrefix = "tomb"

// TombstoneSet manages the set of tombstoned EventKeys.
type TombstoneSet struct {
	mu    sync.RWMutex
	keys  map[int64]bool // EventKey → tombstoned
	rel   RelationStore  // For cascading parent repair
	kv    KVStore        // For persistence
	pid   int            // Partition ID for persistence keys
	dirty bool           // Whether persistence is needed
}

// NewTombstoneSet creates a TombstoneSet.
func NewTombstoneSet(rel RelationStore, kv KVStore, pid int) *TombstoneSet {
	return &TombstoneSet{
		keys: make(map[int64]bool),
		rel:  rel,
		kv:   kv,
		pid:  pid,
	}
}

// MarkTombstone marks an event as tombstoned.
// Before marking, it triggers cascading parent repair for the event's children.
func (ts *TombstoneSet) MarkTombstone(key int64) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	// 1. Mark as tombstoned FIRST so findAliveAncestor skips this key
	ts.mu.Lock()
	ts.keys[key] = true
	ts.dirty = true
	ts.mu.Unlock()

	// 2. Repair children's parent references
	children, err := ts.rel.GetChildren(key)
	if err != nil {
		log.Errorf("[Tombstone] GetChildren failed key=%d: %v", key, err)
	}

	if len(children) > 0 {
		// Find the nearest alive ancestor for this key
		ancestor := ts.findAliveAncestor(key)
		for _, child := range children {
			if ancestor != 0 {
				if err := ts.rel.SetParent(child, ancestor); err != nil {
					log.Errorf("[Tombstone] SetParent failed child=%d ancestor=%d: %v", child, ancestor, err)
				}
			} else {
				// No alive ancestor found, child becomes root
				if err := ts.rel.SetParent(child, 0); err != nil {
					log.Errorf("[Tombstone] SetParent root failed child=%d: %v", child, err)
				}
			}
		}
	}

	// 3. Remove own relations
	if err := ts.rel.RemoveRelations(key); err != nil {
		log.Errorf("[Tombstone] RemoveRelations failed key=%d: %v", key, err)
	}

	// 4. Persist to KV store
	return ts.persistKey(key)
}

// IsTombstone checks if an event key is tombstoned.
func (ts *TombstoneSet) IsTombstone(key int64) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.keys[key]
}

// RemoveTombstones removes tombstone entries after compaction.
func (ts *TombstoneSet) RemoveTombstones(keys []int64) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, key := range keys {
		delete(ts.keys, key)
	}

	if ts.kv != nil {
		batchOps := make([]KVOp, 0, len(keys))
		for _, key := range keys {
			tombKVKey := TombstoneKeyStr(ts.pid, key)
			batchOps = append(batchOps, KVOp{Type: "delete", Key: tombKVKey})
		}
		if len(batchOps) > 0 {
			return ts.kv.KVBatch(batchOps)
		}
	}
	return nil
}

// AllTombstones returns all tombstoned keys.
func (ts *TombstoneSet) AllTombstones() []int64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	keys := make([]int64, 0, len(ts.keys))
	for k := range ts.keys {
		keys = append(keys, k)
	}
	return keys
}

// Count returns the number of tombstoned keys.
func (ts *TombstoneSet) Count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.keys)
}

// Snapshot returns a serializable snapshot of the tombstone set.
func (ts *TombstoneSet) Snapshot() (map[int64]bool, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	snap := make(map[int64]bool, len(ts.keys))
	for k := range ts.keys {
		snap[k] = true
	}
	return snap, nil
}

// LoadSnapshot restores the tombstone set from a snapshot.
func (ts *TombstoneSet) LoadSnapshot(data map[int64]bool) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.keys = make(map[int64]bool, len(data))
	for k := range data {
		ts.keys[k] = true
	}
	ts.dirty = false
	return nil
}

// RecoverFromKV restores tombstone state from KV store on startup.
func (ts *TombstoneSet) RecoverFromKV() error {
	if ts.kv == nil {
		return nil
	}

	tombPrefix := TombstonePrefix(ts.pid)
	pairs, err := ts.kv.KVScan(tombPrefix, 0)
	if err != nil {
		return err
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "tomb" {
			continue
		}
		if pk.EventKey != 0 {
			ts.keys[pk.EventKey] = true
		}
	}
	ts.dirty = false
	return nil
}

// IsDirty returns whether the tombstone set has unpersisted changes.
func (ts *TombstoneSet) IsDirty() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.dirty
}

// ==================== Internal ====================

// persistKey writes a single tombstone key to KV store.
// If KV store is nil, skip persistence (in-memory only mode).
func (ts *TombstoneSet) persistKey(key int64) error {
	if ts.kv == nil {
		return nil
	}
	tombKVKey := TombstoneKeyStr(ts.pid, key)
	return ts.kv.KVPut(tombKVKey, "1")
}

// findAliveAncestor walks the parent chain to find the nearest alive (non-tombstoned) ancestor.
func (ts *TombstoneSet) findAliveAncestor(key int64) int64 {
	visited := make(map[int64]bool)
	current := key
	for current != 0 && !visited[current] {
		visited[current] = true
		if !ts.IsTombstone(current) {
			return current
		}
		parent, err := ts.rel.GetParent(current)
		if err != nil {
			return 0
		}
		current = parent
	}
	return 0
}

// ==================== JSON Serialization ====================

// TombstoneSnapshot is the JSON-serializable snapshot format.
type TombstoneSnapshot struct {
	Keys []int64 `json:"keys"`
}

// MarshalJSON serializes the tombstone set.
func (ts *TombstoneSet) MarshalJSON() ([]byte, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	snap := TombstoneSnapshot{
		Keys: make([]int64, 0, len(ts.keys)),
	}
	for k := range ts.keys {
		snap.Keys = append(snap.Keys, k)
	}
	return json.Marshal(snap)
}

// UnmarshalJSON deserializes the tombstone set.
func (ts *TombstoneSet) UnmarshalJSON(data []byte) error {
	var snap TombstoneSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.keys = make(map[int64]bool, len(snap.Keys))
	for _, k := range snap.Keys {
		ts.keys[k] = true
	}
	return nil
}
