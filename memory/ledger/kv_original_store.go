// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The original store every durable engine keeps: the reference and independent
// body model of the in-memory tier, committed through the atomic store seam so it
// runs over any engine. Nothing here names a file layout.
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// ErrCommitBarrier reports a commit whose barrier did not complete, so nothing
// of it is published.
var ErrCommitBarrier = errors.New("memory: commit barrier failed")

// ErrSnapshotCorrupt reports stored content that cannot be read back as the
// key-value state it claims to hold.
var ErrSnapshotCorrupt = errors.New("memory: stored record is corrupt")

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
	kv      AtomicStore
	windows map[string]time.Duration
	byKey   map[event.EventID]*originalEntry
	loaded  bool
}

// newKVOriginalStore opens the original store without reading it. The existing
// whole-store load runs on the first actual access, with unchanged semantics.
func newKVOriginalStore(kv AtomicStore, windows map[string]time.Duration) (*kvOriginalStore, error) {
	return newEmptyKVOriginalStore(kv, windows), nil
}

// newEmptyKVOriginalStore returns the original store over kv with no stored
// state loaded, so a tier whose view starts empty holds the model without a
// load step.
func newEmptyKVOriginalStore(kv AtomicStore, windows map[string]time.Duration) *kvOriginalStore {
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

// ensureLoaded runs the existing whole-store load on first access. Failures are
// returned without being cached, so a repaired backend can be retried.
func (s *kvOriginalStore) ensureLoaded() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	if err := s.load(); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

// load publishes references only with the independent body of the same event
// key; a reference without that body fails by name instead of disappearing.
//
// - The load reads the two entries it files, the body and the reference, by prefix: a whole-store read of a durable tier walks every event body the segment store owns, so the load would pay for the record it never reads.
func (s *kvOriginalStore) load() error {
	blobPairs, err := s.kv.Read(context.Background(), Fetch{Prefix: "blob:"})
	if err != nil {
		return err
	}
	refPairs, err := s.kv.Read(context.Background(), Fetch{Prefix: "ref:"})
	if err != nil {
		return err
	}
	blobs := make(map[event.EventID][]byte, len(blobPairs))
	for _, pair := range blobPairs {
		if name, ok := strings.CutPrefix(pair.Key, "blob:"); ok {
			blobs[event.EventID(name)] = pair.Value
		}
	}
	refs := make(map[event.EventID][]storedRef, len(refPairs))
	for _, pair := range refPairs {
		name, ok := strings.CutPrefix(pair.Key, "ref:")
		if !ok {
			continue
		}
		var stored []storedRef
		if err := json.Unmarshal(pair.Value, &stored); err != nil {
			return fmt.Errorf("%w: reference record %q: %w", ErrSnapshotCorrupt, name, err)
		}
		refs[event.EventID(name)] = stored
	}
	byKey := map[event.EventID]*originalEntry{}
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
			byKey[key] = entry
		}
	}
	s.byKey = byKey
	return nil
}

// Put commits the original bytes of ref and publishes the reference once its barrier returns success.
//   - Different event keys always own independent bodies; a second scope on the same key joins its references, and other bytes under a held key are refused
//   - The body is written ahead of the reference that names it, so a reader never sees a reference whose body is not already durable
//   - A body whose reference commit failed is an orphan no reader can reach, and CollectOrphanBodies removes it
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
	if err := s.ensureLoaded(); err != nil {
		return err
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
	value, err := json.Marshal(append(s.storedRefsOf(entry), storedRef{
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
	if blobStaged {
		if err := s.kv.Commit(ctx, Batch{Puts: []Pair{{Key: blobKey, Value: body}}}); err != nil {
			return fmt.Errorf("%w: %w", ErrCommitBarrier, err)
		}
	}
	if err := s.kv.Commit(ctx, Batch{Puts: []Pair{{Key: refKey, Value: value}}}); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitBarrier, err)
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

// CollectOrphanBodies deletes every stored body no reference record names: the
// body of a commit whose reference commit failed is durable and unreachable, so
// it is collected here instead of being kept forever. It returns the number of
// bodies removed, and removes nothing a readable reference still names.
func (s *kvOriginalStore) CollectOrphanBodies(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := s.ensureLoaded(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	named := make(map[event.EventID]struct{}, len(s.byKey))
	for key := range s.byKey {
		named[key] = struct{}{}
	}
	s.mu.Unlock()
	pairs, err := s.kv.Read(ctx, Fetch{Prefix: "blob:"})
	if err != nil {
		return 0, err
	}
	var orphans []string
	for _, pair := range pairs {
		key, held := strings.CutPrefix(pair.Key, "blob:")
		if !held {
			continue
		}
		if _, live := named[event.EventID(key)]; !live {
			orphans = append(orphans, pair.Key)
		}
	}
	if len(orphans) == 0 {
		return 0, nil
	}
	if err := s.kv.Commit(ctx, Batch{Deletes: orphans}); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrCommitBarrier, err)
	}
	return len(orphans), nil
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
	if err := s.ensureLoaded(); err != nil {
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
	if err := s.ensureLoaded(); err != nil {
		return nil, err
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
	if err := s.ensureLoaded(); err != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byKey)
}

// References returns every reference registered under key, one per reference
// scope in a stable order, so which owners, formats and retention policies still
// name the original stays observable.
func (s *kvOriginalStore) References(key event.EventID) []OriginalRef {
	if err := s.ensureLoaded(); err != nil {
		return nil
	}
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

// refKVKey returns the key the references of one original are stored under.
func refKVKey(key event.EventID) string {
	return "ref:" + string(key)
}

// blobKVKey returns the independent body key of one event.
func blobKVKey(key event.EventID) string {
	return "blob:" + string(key)
}
