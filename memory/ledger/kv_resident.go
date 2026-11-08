// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The resident entry set every durable engine keeps beside its persisted record:
// an LRU over key-value pairs with a byte budget, so what the process holds is
// bounded whatever the store persists.
package ledger

import "container/list"

const (
	// residentSetBudgetBytes is the byte budget of the resident entry set: the
	// keys and values resident entries carry plus a fixed per-entry cost. It
	// bounds what the process holds, never what the engine persists. The 1 MiB
	// budget saturates before the smoke gate's stable window, so the measured
	// slope stays at steady state; the engine's record remains the source for
	// cold entries.
	residentSetBudgetBytes int64 = 1 << 20
	// residentEntryOverheadBytes is the accounting cost of one resident entry
	// beside its key and value bytes.
	residentEntryOverheadBytes int64 = 96
)

// kvResidentEntry is one key-value pair kept in the resident set: the bytes
// themselves beside their accounting size.
type kvResidentEntry struct {
	key   string
	value []byte
	size  int64
}

// kvResidentSet is the entry-level resident set of one key-value view: an LRU
// over key-value pairs with a byte budget. The engine's persisted record stays
// the persistent unit, so evicting an entry removes memory only; the next read
// of the key reads it back from the record that carries it.
type kvResidentSet struct {
	order   *list.List
	entries map[string]*list.Element
	bytes   int64
}

// newKVResidentSet returns an empty resident set.
func newKVResidentSet() *kvResidentSet {
	return &kvResidentSet{order: list.New(), entries: map[string]*list.Element{}}
}

// get answers the value resident under key and moves it to the front of the
// recency order. The caller copies what it hands out, so the resident entry
// stays the one copy the process holds.
func (s *kvResidentSet) get(key string) ([]byte, bool) {
	element, held := s.entries[key]
	if !held {
		return nil, false
	}
	s.order.MoveToFront(element)
	return element.Value.(*kvResidentEntry).value, true
}

// put makes one entry resident, replacing an earlier value under the same key
// and evicting the least recently used entries until the byte budget holds. An
// entry larger than the whole budget is evicted immediately, so the resident
// bytes never exceed the budget however large one value is.
func (s *kvResidentSet) put(key string, value []byte) {
	size := int64(len(key)+len(value)) + residentEntryOverheadBytes
	if element, held := s.entries[key]; held {
		entry := element.Value.(*kvResidentEntry)
		s.bytes += size - entry.size
		entry.value = value
		entry.size = size
		s.order.MoveToFront(element)
	} else {
		entry := &kvResidentEntry{key: key, value: value, size: size}
		s.entries[key] = s.order.PushFront(entry)
		s.bytes += size
	}
	for s.bytes > residentSetBudgetBytes {
		last := s.order.Back()
		if last == nil {
			break
		}
		s.removeEntry(last.Value.(*kvResidentEntry).key, last)
	}
}

// remove drops one key from the resident set, if it holds one.
func (s *kvResidentSet) remove(key string) {
	element, held := s.entries[key]
	if !held {
		return
	}
	s.removeEntry(key, element)
}

// removeEntry drops one accounted element.
func (s *kvResidentSet) removeEntry(key string, element *list.Element) {
	s.bytes -= element.Value.(*kvResidentEntry).size
	delete(s.entries, key)
	s.order.Remove(element)
}
