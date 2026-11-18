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
	"sync/atomic"
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
	// wholeSetLoads counts the loads that read the whole original family, and
	// wholeSetPairs the records those loads fetched; identityKeys counts the keys
	// a point read of one identity read. A boot states all three, because the
	// family a load reads is the cost a commit that named no identity pays and two
	// keys are the cost a commit that named one owes, whatever the store holds
	// under them.
	wholeSetLoads atomic.Int64
	wholeSetPairs atomic.Int64
	identityKeys  atomic.Int64
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

// ensureLoaded runs the whole-store load on first access. Failures are returned
// without being cached, so a repaired backend can be retried. It is the load of a
// caller that needs the whole set, which is the count of keys holding a body and
// the scoped read faces that take the stored state as a map; a caller that names
// one identity reads that identity through entryLocked instead.
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

// entryLocked answers the entry one identity holds, reading the body and the
// reference that names it from the store on a miss: two point reads of that
// identity, never a walk of the original family behind it. The caller holds the
// lock.
//   - A reference the store files beside no body fails by name, so a read never answers a reference it cannot serve
//   - The entry a commit of this process published is answered from the store, so a key committed before this read costs no read at all
func (s *kvOriginalStore) entryLocked(ctx context.Context, key event.EventID) (*originalEntry, bool, error) {
	if entry, held := s.byKey[key]; held {
		return entry, true, nil
	}
	blobPairs, err := s.kv.Read(ctx, Fetch{Key: blobKVKey(key)})
	if err != nil {
		return nil, false, err
	}
	refPairs, err := s.kv.Read(ctx, Fetch{Key: refKVKey(key)})
	if err != nil {
		return nil, false, err
	}
	s.identityKeys.Add(2)
	entries, err := originalsOf(blobPairs, refPairs)
	if err != nil {
		return nil, false, err
	}
	entry, held := entries[key]
	if !held {
		return nil, false, nil
	}
	s.byKey[key] = entry
	return entry, true, nil
}

// load publishes references only with the independent body of the same event
// key; a reference without that body fails by name instead of disappearing.
//
// - The load reads the two entries it files, the body and the reference, by prefix: a whole-store read of a durable tier walks every event body the segment store owns, so the load would pay for the record it never reads.
// - It is the load of the whole set alone: a caller that names one identity reads that identity, because a boot that paid this prefix walk on its first commit read the whole original family whatever the fact it committed.
func (s *kvOriginalStore) load() error {
	blobPairs, err := s.kv.Read(context.Background(), Fetch{Prefix: "blob:"})
	if err != nil {
		return err
	}
	refPairs, err := s.kv.Read(context.Background(), Fetch{Prefix: "ref:"})
	if err != nil {
		return err
	}
	byKey, err := originalsOf(blobPairs, refPairs)
	if err != nil {
		return err
	}
	s.wholeSetLoads.Add(1)
	s.wholeSetPairs.Add(int64(len(blobPairs) + len(refPairs)))
	s.byKey = byKey
	return nil
}

// originalsOf answers the entries two families of stored records state together:
// one entry per event key, holding the references of that key beside the
// independent body they name. A reference record whose body the same records do
// not carry fails by name instead of disappearing, so a store never publishes a
// reference a reader can name and never read.
func originalsOf(blobPairs, refPairs []Pair) (map[event.EventID]*originalEntry, error) {
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
			return nil, fmt.Errorf("%w: reference record %q: %w", ErrSnapshotCorrupt, name, err)
		}
		refs[event.EventID(name)] = stored
	}
	byKey := map[event.EventID]*originalEntry{}
	for key, stored := range refs {
		entry := &originalEntry{refs: map[refScope]referenceRecord{}}
		for _, record := range stored {
			bytes, held := blobs[key]
			if !held {
				return nil, fmt.Errorf("%w: original %q has a reference but no independent body", ErrSnapshotCorrupt, key)
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
	return byKey, nil
}

// Put commits the original bytes of ref and publishes the reference once its barrier returns success.
//   - Different event keys always own independent bodies; a second scope on the same key joins its references, and other bytes under a held key are refused
//   - The body and the reference that names it travel in one batch, so a reader never sees a reference whose body is missing and a failed commit leaves neither behind
//   - An original the store holds stays committed: a repeat answers as the duplicate, other bytes under that identity are refused, and a new reference re-files the body
func (s *kvOriginalStore) Put(ctx context.Context, ref OriginalRef, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref.Key == "" {
		return ErrKeyRequired
	}
	digest, length, err := checkOriginalRef(ref, body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planFactLocked(ctx, ref, body, digest, length)
	if err != nil {
		return err
	}
	if plan == nil {
		return nil
	}
	if err := s.kv.Commit(ctx, Batch{Puts: plan.puts}); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitBarrier, err)
	}
	s.publishFactLocked(plan)
	return nil
}

// factOriginalPlan is the original family of one whole-fact commit: the pairs one
// batch carries and the state the commit publishes once its barrier returned.
type factOriginalPlan struct {
	// puts are the pairs the commit writes for this family.
	puts []Pair
	// key names the event key the reference belongs to.
	key event.EventID
	// entry is the entry the publication files.
	entry *originalEntry
	// ref and scope are the reference the commit registers.
	ref   OriginalRef
	scope refScope
	// digest, length and body are the bytes the commit carries.
	digest string
	length int64
	body   []byte
	// blobStaged reports that the body travels in this commit.
	blobStaged bool
	// staged reports that the publication files an entry of its own.
	staged bool
}

// checkOriginalRef answers the completeness information of one original the
// commit carries, or the refusal of a reference whose stated completeness
// disagrees with the bytes about to be committed.
func checkOriginalRef(ref OriginalRef, body []byte) (string, int64, error) {
	digest := digestOf(body)
	length := int64(len(body))
	if ref.Integrity.Digest != "" && ref.Integrity.Digest != digest {
		return "", 0, ErrIntegrityMismatch
	}
	if ref.Integrity.Length != 0 && ref.Integrity.Length != length {
		return "", 0, ErrIntegrityMismatch
	}
	return digest, length, nil
}

// planFactLocked answers the original family of one commit: the pairs one batch
// carries and the state the commit publishes, or nothing for a reference the
// store already registered under its scope. The caller loaded the store and
// holds the lock.
func (s *kvOriginalStore) planFactLocked(ctx context.Context, ref OriginalRef, body []byte, digest string, length int64) (*factOriginalPlan, error) {
	entry, held, err := s.entryLocked(ctx, ref.Key)
	if err != nil {
		return nil, err
	}
	if held && entry.blob != nil && entry.blob.digest != digest {
		return nil, ErrContentConflict
	}
	scope := ref.scope()
	if held {
		if _, accepted := entry.refs[scope]; accepted {
			return nil, nil
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
		return nil, err
	}
	blobStaged := !held || entry.blob == nil
	puts := make([]Pair, 0, 2)
	if blobStaged {
		puts = append(puts, Pair{Key: blobKVKey(ref.Key), Value: body})
	}
	puts = append(puts, Pair{Key: refKVKey(ref.Key), Value: value})
	return &factOriginalPlan{
		puts:       puts,
		key:        ref.Key,
		entry:      entry,
		ref:        ref,
		scope:      scope,
		digest:     digest,
		length:     length,
		body:       body,
		blobStaged: blobStaged,
		staged:     !held,
	}, nil
}

// publishFactLocked files one committed original family: the body, its
// references and the entry the reads of the key answer with.
func (s *kvOriginalStore) publishFactLocked(plan *factOriginalPlan) {
	entry := plan.entry
	if plan.staged {
		staged := &originalEntry{refs: map[refScope]referenceRecord{}}
		if entry != nil {
			for stagedScope, record := range entry.refs {
				staged.refs[stagedScope] = record
			}
		}
		entry = staged
	}
	s.byKey[plan.key] = entry
	if plan.blobStaged {
		entry.blob = &originalBlob{digest: plan.digest, length: plan.length, bytes: append([]byte(nil), plan.body...)}
	}
	entry.refs[plan.scope] = referenceRecord{ref: plan.ref, storedAt: time.Now()}
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
	entry, held, err := s.entryLocked(ctx, key)
	if err != nil {
		result.Outcome = RecallFailed
		result.Err = err
		return result
	}
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
	entry, held, err := s.entryLocked(ctx, key)
	if err != nil {
		return nil, err
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held, err := s.entryLocked(context.Background(), key)
	if err != nil || !held {
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

// OriginalsReads reports what the reads of this store cost: the loads that read the
// whole original family beside the records those loads fetched, and the keys the point
// reads of one identity read. It is the quantity a bounded boot is measured by, because
// a commit that read the family paid for every original the store holds and one that
// names its identity pays for that original alone.
func (s *kvOriginalStore) OriginalsReads() (wholeSetLoads int, wholeSetPairs int64, identityKeys int64) {
	return int(s.wholeSetLoads.Load()), s.wholeSetPairs.Load(), s.identityKeys.Load()
}

// refKVKey returns the key the references of one original are stored under.
func refKVKey(key event.EventID) string {
	return "ref:" + string(key)
}

// blobKVKey returns the independent body key of one event.
func blobKVKey(key event.EventID) string {
	return "blob:" + string(key)
}
