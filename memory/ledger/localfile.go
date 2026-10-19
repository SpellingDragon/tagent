package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
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
// unit (a key or a prefix) and one commit unit (a batch applied behind the
// barrier). Writes reach the backing store only when a commit returns success,
// so a caller that reports success to its own consumer commits first and
// reports what that commit returned.
type kvPort interface {
	kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error)
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
)

// localFileKV is the localfile tier key-value view: writes land in memory at
// once and a commit serializes every shard touched since the last barrier into
// its own snapshot file, written to a temporary file and swapped in by one
// atomic rename. A shard is the key namespace up to its first colon, so one
// partition of the segment store commits through exactly one snapshot file and a
// barrier costs the shards it touches. A new process reads back every value a
// successful barrier wrote, and snapshot content it cannot parse fails the open.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
type localFileKV struct {
	mu     sync.Mutex
	dir    string
	shards map[string]map[string][]byte
	dirty  map[string]struct{}
}

// openLocalFileKV opens the key-value view rooted at root, reading back every
// shard snapshot written under it.
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
		dir:    dir,
		shards: map[string]map[string][]byte{},
		dirty:  map[string]struct{}{},
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		label, err := url.PathUnescape(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%w: shard directory %q", ErrSnapshotCorrupt, entry.Name())
		}
		shard, err := readShardSnapshot(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		kv.shards[label] = shard
	}
	return kv, nil
}

var _ kvPort = (*localFileKV)(nil)

// kvRead returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
func (k *localFileKV) kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if fetch.key != "" {
		value, held := k.shards[shardLabelOf(fetch.key)][fetch.key]
		if !held {
			return nil, nil
		}
		return []kvPair{{key: fetch.key, value: append([]byte(nil), value...)}}, nil
	}
	var out []kvPair
	for _, shard := range k.shards {
		for key, value := range shard {
			if strings.HasPrefix(key, fetch.prefix) {
				out = append(out, kvPair{key: key, value: append([]byte(nil), value...)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
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
		if shard, held := k.shards[label]; held {
			if _, existed := shard[key]; existed {
				delete(shard, key)
				k.dirty[label] = struct{}{}
			}
		}
	}
	for _, put := range batch.puts {
		label := shardLabelOf(put.key)
		shard, held := k.shards[label]
		if !held {
			shard = map[string][]byte{}
			k.shards[label] = shard
		}
		shard[put.key] = append([]byte(nil), put.value...)
		k.dirty[label] = struct{}{}
	}
	return k.flushLocked()
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
// state, through a temporary file and one atomic rename.
func (k *localFileKV) writeShard(label string) error {
	dir := filepath.Join(k.dir, url.PathEscape(label))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(shardSnapshot{Version: shardSnapshotVersion, Entries: k.shards[label]})
	if err != nil {
		return err
	}
	temp := filepath.Join(dir, shardSnapshotTempName)
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(dir, shardFileName))
}

// readShardSnapshot reads back the snapshot file of one shard, dropping the
// temporary file an interrupted barrier may have left there. Snapshot content
// that does not parse as the recorded key-value state fails the open.
func readShardSnapshot(dir string) (map[string][]byte, error) {
	snapshot := filepath.Join(dir, shardFileName)
	_ = os.Remove(filepath.Join(dir, shardSnapshotTempName))
	data, err := os.ReadFile(snapshot)
	if errors.Is(err, os.ErrNotExist) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	var snap shardSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, snapshot, err)
	}
	if snap.Version != shardSnapshotVersion {
		return nil, fmt.Errorf("%w: %s carries snapshot version %d", ErrSnapshotCorrupt, snapshot, snap.Version)
	}
	if snap.Entries == nil {
		snap.Entries = map[string][]byte{}
	}
	return snap.Entries, nil
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
// that succeeds carries the commit to any later process. Blobs are stored once
// per distinct content under their digest, references are stored per key, and a
// commit is published to readers only after its barrier returns success.
type kvOriginalStore struct {
	mu      sync.Mutex
	kv      kvPort
	windows map[string]time.Duration
	blobs   map[string]*originalBlob
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
		blobs:   map[string]*originalBlob{},
		byKey:   map[event.EventID]*originalEntry{},
	}
	for policy, window := range windows {
		store.windows[policy] = window
	}
	return store
}

var _ OriginalStore = (*kvOriginalStore)(nil)

// load publishes every stored reference whose blob is stored with it, so a
// commit whose parts disagree is left out of the readable state.
func (s *kvOriginalStore) load() error {
	pairs, err := s.kv.kvRead(context.Background(), kvFetch{})
	if err != nil {
		return err
	}
	blobs := map[string][]byte{}
	refs := map[event.EventID][]storedRef{}
	for _, pair := range pairs {
		if digest, ok := strings.CutPrefix(pair.key, "blob:"); ok {
			blobs[digest] = pair.value
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
			bytes, held := blobs[record.Digest]
			if !held {
				continue
			}
			blob := s.blobs[record.Digest]
			if blob == nil {
				blob = &originalBlob{digest: record.Digest, length: record.Length, bytes: bytes}
				s.blobs[record.Digest] = blob
			}
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
// barrier returns success. Identical bytes are stored once and shared, a
// reference already accepted is kept, a second scope on the same key joins the
// first, and other bytes under a held key are refused.
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
	blobKey := blobKVKey(digest)
	blobStaged := s.blobs[digest] == nil
	staged := []kvStaged{{key: refKey, value: previousValue, present: held}}
	batch := kvBatch{puts: []kvPair{{key: refKey, value: value}}}
	if blobStaged {
		staged = append(staged, kvStaged{key: blobKey})
		batch.puts = append(batch.puts, kvPair{key: blobKey, value: body})
	}
	if err := s.kv.kvCommit(ctx, batch); err != nil {
		return s.abort(ctx, fmt.Errorf("%w: %w", ErrCommitBarrier, err), staged)
	}
	if blobStaged {
		s.blobs[digest] = &originalBlob{digest: digest, length: length, bytes: append([]byte(nil), body...)}
	}
	if !held {
		entry = &originalEntry{refs: map[refScope]referenceRecord{}}
		s.byKey[ref.Key] = entry
	}
	entry.blob = s.blobs[digest]
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

// BlobCount returns the number of distinct contents held, so content
// deduplication stays observable.
func (s *kvOriginalStore) BlobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.blobs)
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

// blobKVKey returns the key the bytes of one content digest are stored under.
func blobKVKey(digest string) string {
	return "blob:" + digest
}
