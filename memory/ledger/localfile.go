package ledger

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// kvPair is one key-value entry of a read result or a write batch.
type kvPair struct {
	key   string
	value []byte
}

// kvFetch names one read of the key-value seam: the single key it names, or
// every key carrying the prefix it names when no key is named. An empty fetch
// reads the whole view, ordered by key.
type kvFetch struct {
	key    string
	prefix string
}

// kvBatch names one write set committed under one barrier: the keys it puts and
// the keys it deletes. A key stands on one side or the other, so the order the
// two sides are applied in names no different state.
type kvBatch struct {
	puts    []kvPair
	deletes []string
}

// kvPort is the key-value seam the durable stores share across tiers: one read
// unit (a key or a prefix), one key enumeration (so a loader can discover the
// partitions it must read without materializing bodies), and one commit unit (a
// batch applied behind the barrier). Writes reach the backing store only when a
// commit returns success, so a caller that reports success to its own consumer
// commits first and reports what that commit returned.
type kvPort interface {
	kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error)
	kvKeys(ctx context.Context, prefix string) ([]string, error)
	kvCommit(ctx context.Context, batch kvBatch) error
}

// ErrCommitBarrier reports a commit whose barrier did not complete, so nothing
// of it is published.
var ErrCommitBarrier = errors.New("memory: commit barrier failed")

// ErrUnstage reports staged writes that could not be rolled back after a failed
// barrier, kept as the evidence of the failure alongside its cause.
var ErrUnstage = errors.New("memory: staged writes could not be unstaged")

// ErrSnapshotCorrupt reports stored snapshot content that cannot be read back as
// the key-value state it claims to hold.
var ErrSnapshotCorrupt = errors.New("memory: stored snapshot is corrupt")

// shardSnapshot is the on-disk form of one shard: every key-value pair of the
// shard in one file, whose whole content is replaced by one atomic rename.
type shardSnapshot struct {
	Version int               `json:"version"`
	Entries map[string][]byte `json:"entries"`
}

const (
	shardDirName          = "kv"
	shardFileName         = "snapshot.json"
	shardSnapshotVersion  = 1
	shardSnapshotTempName = shardFileName + ".tmp"
	// localKVResidentBudgetBytes is the byte budget of the resident entry set:
	// the keys and values resident entries carry plus a fixed per-entry cost. It
	// bounds what the process holds, never what the shard persists. The 1 MiB
	// budget saturates before the smoke gate's stable window, so the measured
	// slope stays at steady state; the shard remains the record for cold entries.
	localKVResidentBudgetBytes int64 = 1 << 20
	// localKVEntryOverheadBytes is the accounting cost of one resident entry
	// beside its key and value bytes.
	localKVEntryOverheadBytes int64 = 96
)

// kvResidentEntry is one key-value pair kept in the resident set: the bytes
// themselves beside their accounting size.
type kvResidentEntry struct {
	key   string
	value []byte
	size  int64
}

// kvResidentSet is the entry-level resident set of one local key-value view: an
// LRU over key-value pairs with a byte budget. The shard snapshot stays the
// persistent unit, so evicting an entry removes memory only; the next read of
// the key reads it back from the shard snapshot.
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
	size := int64(len(key)+len(value)) + localKVEntryOverheadBytes
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
	for s.bytes > localKVResidentBudgetBytes {
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

// kvEntryOffset locates one encoded value inside a shard snapshot.
type kvEntryOffset struct {
	start int64
	end   int64
}

// localFileKV is the localfile tier key-value view: a commit writes the touched
// shards through a temporary file and one atomic rename, so every value a
// successful barrier covered reaches any later process. A shard snapshot is the
// persistent unit; memory residency is entry-level with a byte budget, so a cold
// entry is evicted from memory and read back from its shard snapshot on the next
// read. The write path reads the old snapshot, merges the dirty entries and
// swaps the result in, so an evicted entry never disappears from the persisted
// state.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
type localFileKV struct {
	mu  sync.Mutex
	dir string
	// shards names every shard the directory holds, whether or not any of its
	// entries is resident.
	shards map[string]struct{}
	// resident is the entry-level LRU: the keys and values the process holds.
	resident *kvResidentSet
	// dirty names the entries a commit changed per shard: a nil value is a
	// delete, and a non-nil one is the bytes the commit put.
	dirty map[string]map[string][]byte
	// offsets indexes one shard by the byte range of each encoded value, built
	// on the first cold read of that shard: a cold read then costs one seek
	// instead of one whole-shard scan.
	offsets map[string]map[string]kvEntryOffset
	// coldScans counts the shard scans one cold key read paid, so a check pins
	// the enumeration cost instead of measuring time.
	coldScans int
}

// openLocalFileKV opens the key-value view rooted at root. Snapshot content that
// cannot be read back as the recorded key-value state fails the open; the
// entries themselves stay on disk until a read makes one resident, so the open
// holds no more than one entry's bytes at a time.
func openLocalFileKV(root string) (*localFileKV, error) {
	dir := filepath.Join(root, shardDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	kv := &localFileKV{
		dir:      dir,
		shards:   map[string]struct{}{},
		resident: newKVResidentSet(),
		offsets:  map[string]map[string]kvEntryOffset{},
		dirty:    map[string]map[string][]byte{},
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		label, err := url.PathUnescape(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%w: shard directory %q", ErrSnapshotCorrupt, entry.Name())
		}
		shardDir := filepath.Join(dir, entry.Name())
		_ = os.Remove(filepath.Join(shardDir, shardSnapshotTempName))
		if err := scanShardSnapshot(shardDir, nil); err != nil {
			return nil, err
		}
		kv.shards[label] = struct{}{}
	}
	return kv, nil
}

var _ kvPort = (*localFileKV)(nil)

// kvRead returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
// A key the resident set evicted is read back from its shard snapshot; a prefix
// read merges the snapshots of the shards it covers with the resident entries,
// so an evicted pair is still part of the answer while the resident bytes stay
// bounded.
func (k *localFileKV) kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if fetch.key != "" {
		if value, held := k.resident.get(fetch.key); held {
			return []kvPair{{key: fetch.key, value: append([]byte(nil), value...)}}, nil
		}
		label := shardLabelOf(fetch.key)
		if _, known := k.shards[label]; !known {
			return nil, nil
		}
		offsets, err := k.shardOffsetsLocked(label)
		if err != nil {
			return nil, err
		}
		at, held := offsets[fetch.key]
		if !held {
			return nil, nil
		}
		found, err := readShardValue(filepath.Join(k.dir, url.PathEscape(label)), at)
		if err != nil {
			return nil, err
		}
		k.resident.put(fetch.key, found)
		return []kvPair{{key: fetch.key, value: append([]byte(nil), found...)}}, nil
	}
	pairs := map[string][]byte{}
	for _, label := range k.scanLabelsLocked(fetch.prefix) {
		err := scanShardSnapshot(filepath.Join(k.dir, url.PathEscape(label)), func(key string, value []byte, _ kvEntryOffset) bool {
			if strings.HasPrefix(key, fetch.prefix) {
				pairs[key] = append([]byte(nil), value...)
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	for element := k.resident.order.Front(); element != nil; element = element.Next() {
		entry := element.Value.(*kvResidentEntry)
		if strings.HasPrefix(entry.key, fetch.prefix) {
			pairs[entry.key] = append([]byte(nil), entry.value...)
		}
	}
	out := make([]kvPair, 0, len(pairs))
	for key, value := range pairs {
		out = append(out, kvPair{key: key, value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
}

// kvKeys returns the keys the prefix names, in key order. The enumeration reads
// the shard snapshots a loader needs to discover the partitions it must read:
// values are decoded one at a time and dropped, so no body is materialized.
func (k *localFileKV) kvKeys(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	keys := map[string]struct{}{}
	for _, label := range k.scanLabelsLocked(prefix) {
		err := scanShardSnapshot(filepath.Join(k.dir, url.PathEscape(label)), func(key string, _ []byte, _ kvEntryOffset) bool {
			if strings.HasPrefix(key, prefix) {
				keys[key] = struct{}{}
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	for element := k.resident.order.Front(); element != nil; element = element.Next() {
		key := element.Value.(*kvResidentEntry).key
		if strings.HasPrefix(key, prefix) {
			keys[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

// scanLabelsLocked names the shards a prefix read has to scan: the one shard
// the prefix names through the label before its first colon, or every shard for
// a prefix that names none. The caller holds the lock.
func (k *localFileKV) scanLabelsLocked(prefix string) []string {
	labels := make([]string, 0, len(k.shards))
	if label := shardLabelOf(prefix); prefix != "" && strings.ContainsRune(prefix, ':') {
		if _, known := k.shards[label]; known {
			return []string{label}
		}
		return nil
	}
	for label := range k.shards {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// kvCommit applies the batch and then runs the barrier: it serializes every
// shard touched since the previous barrier and swaps each snapshot in by one
// atomic rename, so the values a successful return covers are exactly the shards
// it rewrote. A shard whose write or rename fails is returned as the cause and
// keeps its earlier snapshot.
func (k *localFileKV) kvCommit(ctx context.Context, batch kvBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range batch.deletes {
		label := shardLabelOf(key)
		if k.keyHeldLocked(label, key) {
			k.stageDeleteLocked(label, key)
		}
	}
	for _, put := range batch.puts {
		label := shardLabelOf(put.key)
		k.shards[label] = struct{}{}
		k.stagePutLocked(label, put.key, put.value)
		k.resident.put(put.key, append([]byte(nil), put.value...))
	}
	return k.flushLocked()
}

// keyHeldLocked reports whether one key is part of the view, resident or on
// disk. The caller holds the lock.
func (k *localFileKV) keyHeldLocked(label, key string) bool {
	if _, held := k.resident.get(key); held {
		return true
	}
	if _, known := k.shards[label]; !known {
		return false
	}
	held := false
	_ = scanShardSnapshot(filepath.Join(k.dir, url.PathEscape(label)), func(candidate string, _ []byte, _ kvEntryOffset) bool {
		if candidate == key {
			held = true
			return false
		}
		return true
	})
	return held
}

// stagePutLocked records one put in the dirty set of its shard. The caller holds
// the lock.
func (k *localFileKV) stagePutLocked(label, key string, value []byte) {
	if k.dirty[label] == nil {
		k.dirty[label] = map[string][]byte{}
	}
	k.dirty[label][key] = append([]byte{}, value...)
}

// stageDeleteLocked records one delete in the dirty set of its shard and drops
// the resident entry. The caller holds the lock.
func (k *localFileKV) stageDeleteLocked(label, key string) {
	if k.dirty[label] == nil {
		k.dirty[label] = map[string][]byte{}
	}
	k.dirty[label][key] = nil
	k.resident.remove(key)
}

// flushLocked runs the barrier over every shard touched since the last one. The
// caller holds the lock.
func (k *localFileKV) flushLocked() error {
	labels := make([]string, 0, len(k.dirty))
	for label := range k.dirty {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		if err := k.writeShard(label); err != nil {
			return err
		}
		delete(k.dirty, label)
	}
	return nil
}

// writeShard replaces the snapshot file of one shard with its whole current
// state, through a temporary file and one atomic rename: the old snapshot is
// streamed, the dirty entries of the shard replace their old values, and every
// entry the commit did not touch is copied through. The merge is streamed so the
// shard is never held whole in memory.
func (k *localFileKV) writeShard(label string) error {
	changed := k.dirty[label]
	dir := filepath.Join(k.dir, url.PathEscape(label))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp := filepath.Join(dir, shardSnapshotTempName)
	file, err := os.Create(temp)
	if err != nil {
		return err
	}
	writeErr := writeShardSnapshot(file, dir, changed)
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(temp)
		return writeErr
	}
	if err := os.Rename(temp, filepath.Join(dir, shardFileName)); err != nil {
		return err
	}
	delete(k.offsets, label)
	return nil
}

// writeShardSnapshot renders one merged shard snapshot into file: the old
// snapshot streamed entry by entry, then the dirty entries in key order.
func writeShardSnapshot(file *os.File, dir string, changed map[string][]byte) error {
	writer := bufio.NewWriterSize(file, 64<<10)
	if _, err := writer.WriteString(`{"version":` + fmt.Sprint(shardSnapshotVersion) + `,"entries":{`); err != nil {
		return err
	}
	first := true
	writeEntry := func(key string, value []byte) error {
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return err
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if !first {
			if err := writer.WriteByte(','); err != nil {
				return err
			}
		}
		first = false
		if _, err := writer.Write(keyJSON); err != nil {
			return err
		}
		if err := writer.WriteByte(':'); err != nil {
			return err
		}
		_, err = writer.Write(valueJSON)
		return err
	}
	var entryErr error
	err := scanShardSnapshot(dir, func(key string, value []byte, _ kvEntryOffset) bool {
		if _, replaced := changed[key]; replaced {
			return true
		}
		entryErr = writeEntry(key, value)
		return entryErr == nil
	})
	if err != nil {
		return err
	}
	if entryErr != nil {
		return entryErr
	}
	keys := make([]string, 0, len(changed))
	for key := range changed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if changed[key] == nil {
			continue
		}
		if err := writeEntry(key, changed[key]); err != nil {
			return err
		}
	}
	if _, err := writer.WriteString("}}"); err != nil {
		return err
	}
	return writer.Flush()
}

// shardOffsetsLocked answers the entry offsets of one shard, scanning its
// snapshot once on the first cold read. The caller holds the lock.
func (k *localFileKV) shardOffsetsLocked(label string) (map[string]kvEntryOffset, error) {
	if offsets, held := k.offsets[label]; held {
		return offsets, nil
	}
	offsets := map[string]kvEntryOffset{}
	k.coldScans++
	if err := scanShardSnapshot(filepath.Join(k.dir, url.PathEscape(label)), func(key string, _ []byte, at kvEntryOffset) bool {
		offsets[key] = at
		return true
	}); err != nil {
		return nil, err
	}
	k.offsets[label] = offsets
	return offsets, nil
}

// readShardValue reads the one encoded value at the offset its shard index names.
func readShardValue(dir string, at kvEntryOffset) ([]byte, error) {
	path := filepath.Join(dir, shardFileName)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, err)
	}
	defer file.Close()
	span := make([]byte, at.end-at.start)
	if _, err := file.ReadAt(span, at.start); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, err)
	}
	buf := bytes.TrimLeft(span, ", \t\r\n")
	decoder := json.NewDecoder(bytes.NewReader(buf))
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, err)
	}
	rest := buf[decoder.InputOffset():]
	colon := bytes.IndexByte(rest, ':')
	if colon < 0 {
		return nil, fmt.Errorf("%w: %s: the entry carries no value separator", ErrSnapshotCorrupt, path)
	}
	var value []byte
	if err := json.Unmarshal(rest[colon+1:], &value); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, err)
	}
	return value, nil
}

// scanShardSnapshot streams the snapshot of one shard entry by entry. A missing
// snapshot is an empty shard. Content that does not parse as the recorded
// key-value state is reported as ErrSnapshotCorrupt, and a visit that answers
// false ends the scan without reading the rest. The reader holds one decoded
// value at a time, never the whole shard.
func scanShardSnapshot(dir string, visit func(key string, value []byte, at kvEntryOffset) bool) error {
	path := filepath.Join(dir, shardFileName)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	corrupt := func(cause error) error {
		return fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, cause)
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 64<<10))
	token, err := decoder.Token()
	if err != nil {
		return corrupt(err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return corrupt(errors.New("snapshot is not a JSON object"))
	}
	versionSeen := false
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return corrupt(err)
		}
		name, ok := nameToken.(string)
		if !ok {
			return corrupt(errors.New("snapshot field name is not a string"))
		}
		switch name {
		case "version":
			var version int
			if err := decoder.Decode(&version); err != nil {
				return corrupt(err)
			}
			if version != shardSnapshotVersion {
				return corrupt(fmt.Errorf("snapshot version %d", version))
			}
			versionSeen = true
		case "entries":
			entriesToken, err := decoder.Token()
			if err != nil {
				return corrupt(err)
			}
			if delimiter, ok := entriesToken.(json.Delim); !ok || delimiter != '{' {
				return corrupt(errors.New("snapshot entries is not a JSON object"))
			}
			for decoder.More() {
				start := decoder.InputOffset()
				keyToken, err := decoder.Token()
				if err != nil {
					return corrupt(err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return corrupt(errors.New("snapshot entry key is not a string"))
				}
				var value []byte
				if err := decoder.Decode(&value); err != nil {
					return corrupt(err)
				}
				at := kvEntryOffset{start: start, end: decoder.InputOffset()}
				if visit != nil && !visit(key, value, at) {
					return nil
				}
			}
			ending, err := decoder.Token()
			if err != nil {
				return corrupt(err)
			}
			if delimiter, ok := ending.(json.Delim); !ok || delimiter != '}' {
				return corrupt(errors.New("snapshot entries does not end"))
			}
		default:
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return corrupt(err)
			}
		}
	}
	ending, err := decoder.Token()
	if err != nil {
		return corrupt(err)
	}
	if delimiter, ok := ending.(json.Delim); !ok || delimiter != '}' {
		return corrupt(errors.New("snapshot does not end"))
	}
	if !versionSeen {
		return corrupt(errors.New("snapshot carries no version"))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return corrupt(errors.New("snapshot carries trailing content"))
	}
	return nil
}

// shardLabelOf returns the shard a key belongs to: the key namespace up to its
// first colon, which one segment partition shares, and "global" for a key that
// carries no namespace.
func shardLabelOf(key string) string {
	if at := strings.IndexByte(key, ':'); at > 0 {
		return key[:at]
	}
	return "global"
}

// storedRef is the persisted form of one registered reference: the scope it was
// accepted under, the time it was accepted, and the completeness information its
// bytes carry.
type storedRef struct {
	OwnerAgent      string    `json:"owner_agent"`
	OwnerSession    string    `json:"owner_session"`
	Format          string    `json:"format"`
	RetentionPolicy string    `json:"retention_policy"`
	StoredAt        time.Time `json:"stored_at"`
	Digest          string    `json:"digest"`
	Length          int64     `json:"length"`
}

// kvOriginalStore is the OriginalStore the persistent tiers hold: the reference
// and blob model of the in-memory tier, kept in a key-value view so a barrier
// that succeeds carries the commit to any later process. Each event key owns
// an independent body and its references; a commit is published to readers only
// after its barrier returns success.
type kvOriginalStore struct {
	mu      sync.Mutex
	kv      kvPort
	windows map[string]time.Duration
	byKey   map[event.EventID]*originalEntry
}

// newKVOriginalStore opens the original store held in kv, measuring every
// retention policy against the window given for it.
func newKVOriginalStore(kv kvPort, windows map[string]time.Duration) (*kvOriginalStore, error) {
	store := newEmptyKVOriginalStore(kv, windows)
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// newEmptyKVOriginalStore returns the original store over kv with no stored
// state loaded, so a tier whose view starts empty holds the model without a
// load step.
func newEmptyKVOriginalStore(kv kvPort, windows map[string]time.Duration) *kvOriginalStore {
	store := &kvOriginalStore{
		kv:      kv,
		windows: map[string]time.Duration{},
		byKey:   map[event.EventID]*originalEntry{},
	}
	for policy, window := range windows {
		store.windows[policy] = window
	}
	return store
}

var _ OriginalStore = (*kvOriginalStore)(nil)

// load publishes references only with the independent body of the same event
// key; a reference without that body fails by name instead of disappearing.
func (s *kvOriginalStore) load() error {
	pairs, err := s.kv.kvRead(context.Background(), kvFetch{})
	if err != nil {
		return err
	}
	blobs := map[event.EventID][]byte{}
	refs := map[event.EventID][]storedRef{}
	for _, pair := range pairs {
		if name, ok := strings.CutPrefix(pair.key, "blob:"); ok {
			blobs[event.EventID(name)] = pair.value
			continue
		}
		name, ok := strings.CutPrefix(pair.key, "ref:")
		if !ok {
			continue
		}
		var stored []storedRef
		if err := json.Unmarshal(pair.value, &stored); err != nil {
			return fmt.Errorf("%w: reference record %q: %w", ErrSnapshotCorrupt, name, err)
		}
		refs[event.EventID(name)] = stored
	}
	for key, stored := range refs {
		entry := &originalEntry{refs: map[refScope]referenceRecord{}}
		for _, record := range stored {
			bytes, held := blobs[key]
			if !held {
				return fmt.Errorf("%w: original %q has a reference but no independent body", ErrSnapshotCorrupt, key)
			}
			blob := &originalBlob{digest: record.Digest, length: record.Length, bytes: bytes}
			ref := OriginalRef{
				Key:             key,
				Owner:           event.Address{AgentID: event.AgentID(record.OwnerAgent), SessionID: event.SessionID(record.OwnerSession)},
				Format:          record.Format,
				RetentionPolicy: record.RetentionPolicy,
				Integrity:       Integrity{Digest: record.Digest, Length: record.Length},
			}
			entry.blob = blob
			entry.refs[ref.scope()] = referenceRecord{ref: ref, storedAt: record.StoredAt}
		}
		if entry.blob != nil {
			s.byKey[key] = entry
		}
	}
	return nil
}

// Put commits the original bytes of ref and publishes the reference once its
// barrier returns success. Different event keys always own independent bodies;
// a second scope on the same key joins its references, and other bytes under a
// held key are refused.
func (s *kvOriginalStore) Put(ctx context.Context, ref OriginalRef, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref.Key == "" {
		return ErrKeyRequired
	}
	digest := digestOf(body)
	length := int64(len(body))
	if ref.Integrity.Digest != "" && ref.Integrity.Digest != digest {
		return ErrIntegrityMismatch
	}
	if ref.Integrity.Length != 0 && ref.Integrity.Length != length {
		return ErrIntegrityMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held := s.byKey[ref.Key]
	if held && entry.blob != nil && entry.blob.digest != digest {
		return ErrContentConflict
	}
	scope := ref.scope()
	if held {
		if _, accepted := entry.refs[scope]; accepted {
			return nil
		}
	}
	ref.Integrity = Integrity{Digest: digest, Length: length}
	previous := s.storedRefsOf(entry)
	previousValue, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	value, err := json.Marshal(append(previous, storedRef{
		OwnerAgent:      string(ref.Owner.AgentID),
		OwnerSession:    string(ref.Owner.SessionID),
		Format:          ref.Format,
		RetentionPolicy: ref.RetentionPolicy,
		StoredAt:        time.Now(),
		Digest:          digest,
		Length:          length,
	}))
	if err != nil {
		return err
	}
	refKey := refKVKey(ref.Key)
	blobKey := blobKVKey(ref.Key)
	blobStaged := !held || entry.blob == nil
	staged := []kvStaged{{key: refKey, value: previousValue, present: held}}
	batch := kvBatch{}
	if blobStaged {
		staged = append(staged, kvStaged{key: blobKey})
		batch.puts = append(batch.puts, kvPair{key: blobKey, value: body})
	}
	batch.puts = append(batch.puts, kvPair{key: refKey, value: value})
	if err := s.kv.kvCommit(ctx, batch); err != nil {
		return s.abort(ctx, fmt.Errorf("%w: %w", ErrCommitBarrier, err), staged)
	}
	if !held {
		entry = &originalEntry{refs: map[refScope]referenceRecord{}}
		s.byKey[ref.Key] = entry
	}
	if blobStaged {
		entry.blob = &originalBlob{digest: digest, length: length, bytes: append([]byte(nil), body...)}
	}
	entry.refs[scope] = referenceRecord{ref: ref, storedAt: time.Now()}
	return nil
}

// Get returns the exact recall result for key, classified exactly as the
// in-memory tier classifies it: the committed bytes while a reference of the key
// is inside its retention window, and a named missing, expired or failed result
// otherwise.
func (s *kvOriginalStore) Get(ctx context.Context, key event.EventID) RecallResult {
	result := RecallResult{Key: key}
	if err := ctx.Err(); err != nil {
		result.Outcome = RecallFailed
		result.Err = err
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held := s.byKey[key]
	if !held || len(entry.refs) == 0 {
		result.Outcome = RecallMissing
		return result
	}
	if !s.anyReadable(entry) {
		result.Outcome = RecallExpired
		return result
	}
	if err := entry.blob.verify(); err != nil {
		result.Outcome = RecallFailed
		result.Err = err
		return result
	}
	result.Outcome = RecallFound
	result.Body = append([]byte(nil), entry.blob.bytes...)
	return result
}

// ReadSegment returns at most numLines whole lines of the original stored under
// key, starting at line startLine, and names every read it cannot serve exactly
// as the in-memory tier names it.
func (s *kvOriginalStore) ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if startLine < 1 || numLines < 1 {
		return nil, ErrSegmentRange
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held := s.byKey[key]
	if !held || len(entry.refs) == 0 {
		return nil, ErrOriginalMissing
	}
	if !s.anyReadable(entry) {
		return nil, ErrOriginalExpired
	}
	if err := entry.blob.verify(); err != nil {
		return nil, err
	}
	lines := splitLines(entry.blob.bytes)
	if startLine > len(lines) {
		return nil, ErrSegmentPastEnd
	}
	end := len(lines)
	if numLines < end-startLine+1 {
		end = startLine + numLines - 1
	}
	var out []byte
	for _, line := range lines[startLine-1 : end] {
		out = append(out, line...)
	}
	return out, nil
}

// BlobCount returns the number of event keys holding an original body.
func (s *kvOriginalStore) BlobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byKey)
}

// References returns every reference registered under key, one per deduplication
// scope in a stable order, so which owners, formats and retention policies still
// name the original stays observable.
func (s *kvOriginalStore) References(key event.EventID) []OriginalRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held := s.byKey[key]
	if !held {
		return nil
	}
	scopes := make([]refScope, 0, len(entry.refs))
	for scope := range entry.refs {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		a, b := scopes[i], scopes[j]
		if a.owner != b.owner {
			if a.owner.AgentID != b.owner.AgentID {
				return a.owner.AgentID < b.owner.AgentID
			}
			return a.owner.SessionID < b.owner.SessionID
		}
		if a.format != b.format {
			return a.format < b.format
		}
		return a.retentionPolicy < b.retentionPolicy
	})
	refs := make([]OriginalRef, 0, len(scopes))
	for _, scope := range scopes {
		refs = append(refs, entry.refs[scope].ref)
	}
	return refs
}

// storedRefsOf returns the persisted form of every reference already registered
// under the entry, in a stable order.
func (s *kvOriginalStore) storedRefsOf(entry *originalEntry) []storedRef {
	if entry == nil {
		return nil
	}
	out := make([]storedRef, 0, len(entry.refs))
	for _, record := range entry.refs {
		out = append(out, storedRef{
			OwnerAgent:      string(record.ref.Owner.AgentID),
			OwnerSession:    string(record.ref.Owner.SessionID),
			Format:          record.ref.Format,
			RetentionPolicy: record.ref.RetentionPolicy,
			StoredAt:        record.storedAt,
			Digest:          record.ref.Integrity.Digest,
			Length:          record.ref.Integrity.Length,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OwnerAgent != out[j].OwnerAgent {
			return out[i].OwnerAgent < out[j].OwnerAgent
		}
		if out[i].OwnerSession != out[j].OwnerSession {
			return out[i].OwnerSession < out[j].OwnerSession
		}
		if out[i].Format != out[j].Format {
			return out[i].Format < out[j].Format
		}
		return out[i].RetentionPolicy < out[j].RetentionPolicy
	})
	return out
}

// anyReadable reports whether any reference of the entry is inside the retention
// window of its policy right now.
func (s *kvOriginalStore) anyReadable(entry *originalEntry) bool {
	now := time.Now()
	for _, record := range entry.refs {
		window, registered := s.windows[record.ref.RetentionPolicy]
		if !registered || now.Sub(record.storedAt) < window {
			return true
		}
	}
	return false
}

// abort rolls the staged writes of a failed commit back and returns the failure
// that caused it, joined with whatever the rollback itself reports.
func (s *kvOriginalStore) abort(ctx context.Context, cause error, staged []kvStaged) error {
	if err := kvUnstage(ctx, s.kv, staged); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: %w", ErrUnstage, err))
	}
	return cause
}

// kvStaged names one staged write and the state the key held before it, so a
// failed commit can restore exactly that state.
type kvStaged struct {
	key     string
	value   []byte
	present bool
}

// kvUnstage restores every staged key to the state it held before the commit
// whose barrier failed, in one batch behind one barrier.
func kvUnstage(ctx context.Context, kv kvPort, staged []kvStaged) error {
	batch := kvBatch{}
	for _, stage := range staged {
		if stage.present {
			batch.puts = append(batch.puts, kvPair{key: stage.key, value: stage.value})
			continue
		}
		batch.deletes = append(batch.deletes, stage.key)
	}
	return kv.kvCommit(ctx, batch)
}

// refKVKey returns the key the references of one original are stored under.
func refKVKey(key event.EventID) string {
	return "ref:" + string(key)
}

// blobKVKey returns the independent body key of one event.
func blobKVKey(key event.EventID) string {
	return "blob:" + string(key)
}
