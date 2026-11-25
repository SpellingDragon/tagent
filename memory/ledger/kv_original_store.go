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
	// segments is the view of the same engine the store reads a committed event
	// body through: the one place the text of an original lives once its
	// reference records no body of its own. It opens on the first original
	// answered from its event, so a store whose references carry their bodies
	// opens none.
	segments *FileSegmentStore
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
// caller that needs the whole set, which is the count of keys holding a body; a
// caller that names one identity — a bare read, a commit of one fact and a scoped
// read of one key alike — reads that identity through entryLocked instead.
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
	refPairs, err := s.kv.Read(ctx, Fetch{Key: refKVKey(key)})
	if err != nil {
		return nil, false, err
	}
	s.identityKeys.Add(1)
	return s.entryWithRefsLocked(ctx, key, refPairs)
}

// entryWithRefsLocked answers the entry one identity holds from the references already
// read for it: it reads the body beside them and publishes both records together, so a
// caller that read the references to decide a read pays for no second read of them. The
// caller holds the lock.
func (s *kvOriginalStore) entryWithRefsLocked(ctx context.Context, key event.EventID, refPairs []Pair) (*originalEntry, bool, error) {
	blobPairs, err := s.kv.Read(ctx, Fetch{Key: blobKVKey(key)})
	if err != nil {
		return nil, false, err
	}
	s.identityKeys.Add(1)
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

// referencesLocked answers the references one identity holds, read by the reference key
// alone: one point read that states the scope and the expiry of the identity and never
// its body. The entry it answers is not published, because a read that publishes one
// reads the body beside the reference (see entryLocked). The caller holds the lock.
func (s *kvOriginalStore) referencesLocked(ctx context.Context, key event.EventID) (map[event.EventID]*originalEntry, []Pair, error) {
	refPairs, err := s.kv.Read(ctx, Fetch{Key: refKVKey(key)})
	if err != nil {
		return nil, nil, err
	}
	s.identityKeys.Add(1)
	refs, err := referencesOf(refPairs)
	if err != nil {
		return nil, nil, err
	}
	return refs, refPairs, nil
}

// scopedOnlyLocked answers the state a scoped read of one key decides over: the one
// identity the read names, never the original family behind it. The reference of the
// identity is read alone first, so scope and expiry are decided before any body is
// read: a hidden or expired original never turns an unreadable body into a failed
// read. The entry a refused read decides over is not published, so a later read that
// may see the identity answers from the records it reads itself. The caller holds the
// lock.
func (s *kvOriginalStore) scopedOnlyLocked(ctx context.Context, key event.EventID, scope ReadScope) (map[event.EventID]*originalEntry, error) {
	entry, cached := s.byKey[key]
	var refPairs []Pair
	if !cached {
		refs, pairs, err := s.referencesLocked(ctx, key)
		if err != nil {
			return nil, err
		}
		var held bool
		entry, held = refs[key]
		if !held {
			return map[event.EventID]*originalEntry{}, nil
		}
		refPairs = pairs
	}
	record, registered := entry.refs[scope.identity()]
	if !registered || !readableWithin(s.windows, record, time.Now()) {
		return map[event.EventID]*originalEntry{key: entry}, nil
	}
	if refPairs != nil {
		resolved, held, err := s.entryWithRefsLocked(ctx, key, refPairs)
		if err != nil {
			return nil, err
		}
		if !held {
			return map[event.EventID]*originalEntry{}, nil
		}
		entry = resolved
	}
	if entry.blob == nil {
		resolved, held, err := s.resolveLocked(ctx, key)
		if err != nil {
			return nil, err
		}
		if !held {
			return map[event.EventID]*originalEntry{}, nil
		}
		entry = resolved
	}
	return map[event.EventID]*originalEntry{key: entry}, nil
}

// resolveLocked answers the entry one identity holds with the bytes of its original
// in hand: the body the store keeps beside the reference, or, for a reference that
// records no body of its own, the text the committed event of that identity carries.
// A read of an original therefore answers the same bytes whether its commit stored
// them beside the reference or only inside the event they belong to. The caller holds
// the lock.
//   - The text read back out of the event is checked against the completeness every
//     reference of the identity declares, so a body the reference does not name is
//     refused by name instead of served
func (s *kvOriginalStore) resolveLocked(ctx context.Context, key event.EventID) (*originalEntry, bool, error) {
	entry, held, err := s.entryLocked(ctx, key)
	if err != nil || !held || entry.blob != nil {
		return entry, held, err
	}
	body, err := s.eventBodyLocked(ctx, key)
	if err != nil {
		return nil, false, err
	}
	text, err := storedEventText(body)
	if err != nil {
		return nil, false, err
	}
	blob := &originalBlob{digest: digestOf(text), length: int64(len(text)), bytes: text}
	for _, record := range entry.refs {
		if record.ref.Integrity.Digest != blob.digest || record.ref.Integrity.Length != blob.length {
			return nil, false, ErrIntegrityMismatch
		}
	}
	entry.blob = blob
	return entry, true, nil
}

// eventBodyLocked answers the body the chain committed for one identity. The view it
// reads through is opened over the engine this store already holds, so the original
// side names no second store, and a reference the chain holds no event for is refused
// by name instead of reported as an absent original. The caller holds the lock.
func (s *kvOriginalStore) eventBodyLocked(ctx context.Context, key event.EventID) ([]byte, error) {
	if s.segments == nil {
		segments, err := newFileSegmentStore(s.kv)
		if err != nil {
			return nil, err
		}
		s.segments = segments
	}
	stored, held, err := s.segments.GetEvent(ctx, key)
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, fmt.Errorf("%w: original %q has a reference but no stored body", ErrSnapshotCorrupt, key)
	}
	return stored.Body, nil
}

// storedEventText answers the original text one committed event body carries: the
// content of the native model payload the body holds under its message field. The
// event body is the one place that text is committed, so the read takes it from the
// body itself and never from a copy beside it; bytes that carry no such payload are
// refused by name instead of served as an empty original.
func storedEventText(body []byte) ([]byte, error) {
	// eventPayload is the part of the body schema this read names: the content of
	// the native model payload, under the message field the schema keeps it in.
	type eventPayload struct {
		Content string `json:"content"`
	}
	var carried struct {
		Message *eventPayload `json:"message"`
	}
	if err := json.Unmarshal(body, &carried); err != nil {
		return nil, fmt.Errorf("%w: the committed event body does not decode: %w", ErrSnapshotCorrupt, err)
	}
	if carried.Message == nil {
		return nil, fmt.Errorf("%w: the committed event body carries no message", ErrSnapshotCorrupt)
	}
	return []byte(carried.Message.Content), nil
}

// load publishes the references the store holds beside the bodies it keeps for them.
// A reference that records no body of its own is filed as it stands, because the text
// of that original is committed inside its event and this load reads no event body.
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

// referencesOf answers the entries one family of reference records states: one entry
// per event key, holding the references of that key and no body. A body the store
// keeps beside them is filed by originalsOf.
func referencesOf(refPairs []Pair) (map[event.EventID]*originalEntry, error) {
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
			ref := OriginalRef{
				Key:             key,
				Owner:           event.Address{AgentID: event.AgentID(record.OwnerAgent), SessionID: event.SessionID(record.OwnerSession)},
				Format:          record.Format,
				RetentionPolicy: record.RetentionPolicy,
				Integrity:       Integrity{Digest: record.Digest, Length: record.Length},
			}
			entry.refs[ref.scope()] = referenceRecord{ref: ref, storedAt: record.StoredAt}
		}
		if len(entry.refs) > 0 {
			byKey[key] = entry
		}
	}
	return byKey, nil
}

// originalsOf answers the entries two families of stored records state together: one
// entry per event key, holding the references of that key beside the body the same
// records carry for it. A reference the records carry no body beside is filed as it
// stands rather than refused: the text of that original lives in the event its
// identity names, and the read of that identity answers it from there.
//   - The body is filed under the one completeness the references of its key state
//     together: references that name different digests or lengths are corrupt records,
//     refused by name instead of answered from whichever record the walk reached last
func originalsOf(blobPairs, refPairs []Pair) (map[event.EventID]*originalEntry, error) {
	byKey, err := referencesOf(refPairs)
	if err != nil {
		return nil, err
	}
	blobs := make(map[event.EventID][]byte, len(blobPairs))
	for _, pair := range blobPairs {
		if name, ok := strings.CutPrefix(pair.Key, "blob:"); ok {
			blobs[event.EventID(name)] = pair.Value
		}
	}
	for key, entry := range byKey {
		bytes, carried := blobs[key]
		if !carried {
			continue
		}
		digest, length := "", int64(0)
		stated := false
		for _, record := range entry.refs {
			integrity := record.ref.Integrity
			if !stated {
				digest, length = integrity.Digest, integrity.Length
				stated = true
				continue
			}
			if integrity.Digest != digest || integrity.Length != length {
				return nil, fmt.Errorf("%w: original %q states bytes %q of length %d beside bytes %q of length %d", ErrSnapshotCorrupt, key, digest, length, integrity.Digest, integrity.Length)
			}
		}
		entry.blob = &originalBlob{digest: digest, length: length, bytes: bytes}
	}
	return byKey, nil
}

// Put commits the original bytes of ref and publishes the reference once its barrier returns success.
//   - Different event keys always own independent bodies; a second scope on the same key joins its references, and other bytes under a held key are refused
//   - A bare commit names no event, so the bytes it carries are the only copy the store holds of that original and they travel in one batch with the reference that names them
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
	if plan.bodyHeld {
		plan.puts = append([]Pair{{Key: blobKVKey(ref.Key), Value: body}}, plan.puts...)
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
	// bodyHeld reports that this commit introduces the bytes of the key: the
	// publication files them into the entry, and a commit whose reader is the store
	// itself writes them beside the reference because no event carries them.
	bodyHeld bool
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
// store already registered under its scope. The bytes of the key are published to
// the reads of this process and written to the store only where no event carries
// them: the fact commit keeps its text inside the event of the same key, and the
// batch it lands therefore carries the reference alone. The caller loaded the
// store and holds the lock.
func (s *kvOriginalStore) planFactLocked(ctx context.Context, ref OriginalRef, body []byte, digest string, length int64) (*factOriginalPlan, error) {
	entry, held, err := s.entryLocked(ctx, ref.Key)
	if err != nil {
		return nil, err
	}
	if held {
		if err := entry.refuseOtherBytes(digest); err != nil {
			return nil, err
		}
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
	return &factOriginalPlan{
		puts:     []Pair{{Key: refKVKey(ref.Key), Value: value}},
		key:      ref.Key,
		entry:    entry,
		ref:      ref,
		scope:    scope,
		digest:   digest,
		length:   length,
		body:     body,
		bodyHeld: !held || entry.blob == nil,
		staged:   !held,
	}, nil
}

// refuseOtherBytes reports whether the entry already names bytes other than digest:
// the body the store keeps beside the references states the content of the key, and
// so does every reference registered beside it, because one identity stands for one
// immutable fact. A key whose body the store keeps only inside its event is guarded
// by the references alone, so the absence of a stored body never widens what a
// commit may land under an identity a reader already holds.
func (e *originalEntry) refuseOtherBytes(digest string) error {
	if e.blob != nil && e.blob.digest != digest {
		return ErrContentConflict
	}
	for _, record := range e.refs {
		if record.ref.Integrity.Digest != "" && record.ref.Integrity.Digest != digest {
			return ErrContentConflict
		}
	}
	return nil
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
	if plan.bodyHeld {
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
	entry, held, err := s.resolveLocked(ctx, key)
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
	entry, held, err := s.resolveLocked(ctx, key)
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

// BlobCount returns the number of event keys holding an original, one per
// reference the store resolves.
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
