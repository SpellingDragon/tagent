// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Package memory is the runtime's durable original-text layer: it names one
// stored original and the reference metadata that scopes it, commits those bytes
// and returns them byte for byte, and carries the ordered projection and the
// retention guard built beside them.
//
// The package speaks the fact vocabulary of the event package and nothing above
// it, so the durable side cannot be reversed into the intelligent or governing
// side. This is the phase 1 skeleton of that layer: the shapes and the contracts
// that bind them, together with the scoped read face an authorized read runs
// through.
package memory

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// OriginalRef names one stored original blob together with the reference
// metadata that scopes it: the owner that registered the reference, the format
// it was registered in, the retention policy it is held under, and the
// completeness information of its bytes.
//
// The bytes of a blob may be deduplicated by content, while the reference
// metadata stays separate: references that differ in owner, format or retention
// policy MUST NOT overwrite one another, and a later commit MUST NOT invalidate
// an earlier effective reference.
type OriginalRef struct {
	// Key is the canonical identity of the original blob, in its hex form. Two
	// references may share it while their content is byte-identical; the
	// metadata below is what keeps them apart.
	Key event.EventID
	// Owner is the address the reference belongs to. It is part of the
	// deduplication scope, so two owners never overwrite each other.
	Owner event.Address
	// Format is the original format the reference was registered in. It is part
	// of the deduplication scope.
	Format string
	// RetentionPolicy is the retention policy the reference is held under. It is
	// part of the deduplication scope.
	RetentionPolicy string
	// Integrity is the completeness information of the stored bytes.
	Integrity Integrity
}

// refScope is the deduplication scope of one reference: the owner it was
// registered for, the format it was registered in and the retention policy it is
// held under. Two references share a scope only when all three agree, so
// references that differ in any of them stay registered side by side.
type refScope struct {
	owner           event.Address
	format          string
	retentionPolicy string
}

// scope returns the deduplication scope the reference is registered under.
func (r OriginalRef) scope() refScope {
	return refScope{owner: r.Owner, format: r.Format, retentionPolicy: r.RetentionPolicy}
}

// ReadScope is the trusted read scope one reader runs inside: the owner, format and retention policy
// its reads are authorized under, aligned with the metadata a reference was written under.
//
// - A read scope is not a caller's claim: it is bound once from trusted state when a scoped reader is constructed, and the values a caller merely types grant nothing.
// - Holding a bare key is not a read authorization: a read aggregates only the references registered inside its scope, so a key whose references all lie outside it reads as missing, its existence not leaked.
// - The triple is the one references are registered under (see refScope), read here as authorization rather than as deduplication identity: the two scopes decide different things and neither stands in for the other.
// - The tiers keep their baseline faces beside their storage logic; the visibility contract they share lives here, beside the read contract it serves.
type ReadScope struct {
	// Owner is the address the reads are authorized for.
	Owner event.Address
	// Format is the original format the reads are authorized for.
	Format string
	// RetentionPolicy is the retention policy the reads are authorized under.
	RetentionPolicy string
}

// ReadScope returns the read scope this reference was written under: the owner,
// format and retention policy it registered with. A reader bound to exactly
// this scope is a reader this reference is visible to; a reader bound to any
// other scope never aggregates it.
func (r OriginalRef) ReadScope() ReadScope {
	return ReadScope{Owner: r.Owner, Format: r.Format, RetentionPolicy: r.RetentionPolicy}
}

// Validate returns the named refusal that rejects this scope before any read
// runs under it, or nil when the scope may be bound. The only refusal today is
// the zero scope: it names no owner, no format and no retention policy, aligns
// with no reference, and is refused by name instead of being read as
// authorization for everything.
func (s ReadScope) Validate() error {
	if s == (ReadScope{}) {
		return ErrScopeRequired
	}
	return nil
}

// identity returns the registration identity this read scope selects
// references by: the owner, format and retention policy triple references are
// registered under. It only selects references; it makes no deduplication
// decision of its own.
func (s ReadScope) identity() refScope {
	return refScope{owner: s.Owner, format: s.Format, retentionPolicy: s.RetentionPolicy}
}

// ErrScopeRequired reports a read scope that names no owner, format or
// retention policy: it aligns with no reference, so binding it would authorize
// nothing and granting it would authorize everything, and it is refused by
// name instead.
var ErrScopeRequired = errors.New("memory: read scope names no owner, format or retention policy")

// Integrity is the completeness information of one original blob: the digest
// computed over its stored bytes and the byte length those bytes must have.
//
// A read returns the digest and the length together with the bytes, so a
// truncated or substituted original is reported as a failed read instead of
// being served as if it were complete.
type Integrity struct {
	// Digest is the digest over the stored bytes, in the canonical text form of
	// the algorithm that produced it.
	Digest string
	// Length is the exact byte length the stored original must have.
	Length int64
}

// OriginalStore is the original-text port: it commits the bytes a reference
// names and returns them byte for byte, whole or as segments.
//
// The redline list (4 rules):
// MUST commit the bytes a reference names before any bounded preview or segment entry exists.
// MUST return the original byte for byte, whole or as segments.
// MUST NOT answer a read with a model view in place of the original.
// MUST NOT invalidate an accepted reference by a later Put.
type OriginalStore interface {
	// Put commits the original bytes of ref. Content may be deduplicated by
	// blob, while owner, format and retention policy scope the reference
	// metadata, so an accepted reference is never invalidated by a later Put.
	Put(ctx context.Context, ref OriginalRef, body []byte) error

	// Get returns the exact recall result for key: the original bytes,
	// byte-identical to the bytes Put accepted, or a named
	// missing/expired/failed classification (see RecallResult). Wiring Get to
	// RecallResult gives the classification a real consumer (review gate P2-2).
	//
	// Migration form: a bare key is not a read authorization (see ReadScope),
	// and the visibility decision here aggregates every reference of the key
	// whatever scope it was registered under. Authorized reads run through
	// ScopedOriginalStore and ScopedReader, where the trusted scope the read
	// runs inside decides which references take part.
	Get(ctx context.Context, key event.EventID) RecallResult

	// ReadSegment returns at most numLines lines of the original stored under
	// key starting at line startLine, so an oversized original is consumed by
	// segment rather than returned whole.
	//
	// Migration form: this bounds lines, not bytes — one oversized line is
	// returned whole whatever numLines is, so it is no byte bound at all. The
	// byte-bounded face is SegmentRequest and SegmentResult, served by
	// ScopedReader.ReadSegment and ScopedOriginalStore.ReadSegmentScoped, which
	// cut at byte boundaries and never at line boundaries.
	ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
}

// ScopedOriginalStore is the original-text read face that decides visibility
// inside one trusted read scope: it serves the reads that aggregate only the
// references registered inside the scope they name, and nothing else. The bare
// commit and recall face of OriginalStore stays on the store itself, so holding
// this port never hands a caller an unauthorized read.
//
// The redline list (3 rules):
// MUST aggregate only the references registered inside the scope the read names.
// MUST NOT hand a caller the bare commit or recall face of the store itself.
// MUST NOT leak the existence of a key whose references all lie outside the scope, nor be implemented by a store that cannot decide visibility inside one.
type ScopedOriginalStore interface {
	// GetScoped returns the exact recall of key as visible inside scope: the
	// committed bytes while the reference registered inside scope is inside its
	// retention window, a named expired result when that reference is past its
	// window whatever the other references of the key say, a named missing
	// result when no reference names the key inside scope, and a named failed
	// result carrying the cause when the read itself fails. A key whose
	// references all lie outside scope reads as missing, so its existence is
	// not leaked.
	GetScoped(ctx context.Context, key event.EventID, scope ReadScope) RecallResult

	// ReadSegmentScoped returns one byte-bounded segment of the original stored
	// under key as visible inside scope, on exactly the terms of SegmentRequest
	// and SegmentResult. A read it cannot serve inside scope is named by the
	// same errors GetScoped classifies with — ErrOriginalMissing for a key with
	// no reference inside scope, ErrOriginalExpired for one past its window —
	// so an out-of-scope key is refused as missing and leaks nothing.
	ReadSegmentScoped(ctx context.Context, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error)
}

// ScopedReader is an original-text reader bound to one trusted read scope: the
// scope is bound once at construction, and every read it serves runs inside
// that scope, so authorization travels with the reader and never with a bare
// key.
//
// Holding a key is not a read authorization (see ReadScope): a read through
// this reader aggregates only the references registered inside the bound
// scope, and a key whose references lie outside it reads as missing.
type ScopedReader struct {
	store ScopedOriginalStore
	scope ReadScope
}

// NewScopedReader derives a reader bound to scope from store. The scope comes
// from the trusted state of the caller, not from the per-read calls the reader
// then serves, and a scope that names nothing is refused by name with
// ErrScopeRequired before any read runs.
func NewScopedReader(store ScopedOriginalStore, scope ReadScope) (*ScopedReader, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return &ScopedReader{store: store, scope: scope}, nil
}

// Scope returns the trusted read scope the reader is bound to, so a consumer
// can record which authority a result was read under.
func (r *ScopedReader) Scope() ReadScope {
	return r.scope
}

// Get returns the exact recall of key inside the bound scope, on the terms of
// ScopedOriginalStore.GetScoped: references registered outside the scope take
// no part in the visibility decision, so a key the scope cannot see reads as
// missing and leaks nothing.
func (r *ScopedReader) Get(ctx context.Context, key event.EventID) RecallResult {
	return r.store.GetScoped(ctx, key, r.scope)
}

// ReadSegment returns one byte-bounded segment of the original stored under
// key, inside the bound scope: the request names the byte position to resume
// from — carried forward from the Next of the previous result — and the byte
// bound one segment may return, and the result carries the segment, the
// position the next segment resumes from and whether the end of the original
// was reached. Segments are cut at byte boundaries whatever the content holds
// and concatenate back to the exact bytes; a request with a zero MaxBytes is
// served under DefaultSegmentMaxBytes and never without a bound (see
// SegmentRequest).
func (r *ScopedReader) ReadSegment(ctx context.Context, key event.EventID, req SegmentRequest) (SegmentResult, error) {
	return r.store.ReadSegmentScoped(ctx, key, r.scope, req)
}

var (
	_ ScopedOriginalStore = (*MemoryOriginalStore)(nil)
	_ ScopedOriginalStore = (*kvOriginalStore)(nil)
)

// GetScoped returns the exact recall of key as visible inside scope, exactly
// as the in-memory tier serves it.
func (s *kvOriginalStore) GetScoped(ctx context.Context, key event.EventID, scope ReadScope) RecallResult {
	if err := ctx.Err(); err != nil {
		return RecallResult{Key: key, Outcome: RecallFailed, Err: err}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return scopedRecall(s.byKey, s.windows, key, scope)
}

// ReadSegmentScoped returns one byte-bounded segment of the original stored
// under key as visible inside scope, exactly as the in-memory tier serves it.
func (s *kvOriginalStore) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error) {
	if err := ctx.Err(); err != nil {
		return SegmentResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return scopedSegment(s.byKey, s.windows, key, scope, req)
}

// scopedRecall evaluates the exact recall of key inside scope over the given
// reference and window state. Only the reference registered inside scope takes
// part, so the other references of the key never widen what the read sees: an
// expired reference stays expired while a longer one beside it is kept, and a
// key no reference names inside scope reads as missing.
func scopedRecall(byKey map[event.EventID]*originalEntry, windows map[string]time.Duration, key event.EventID, scope ReadScope) RecallResult {
	result := RecallResult{Key: key}
	record, blob, held := scopedReference(byKey, key, scope)
	if !held {
		result.Outcome = RecallMissing
		return result
	}
	if !readableWithin(windows, record, time.Now()) {
		result.Outcome = RecallExpired
		return result
	}
	if err := blob.verify(); err != nil {
		result.Outcome = RecallFailed
		result.Err = err
		return result
	}
	result.Outcome = RecallFound
	result.Body = append([]byte(nil), blob.bytes...)
	return result
}

// scopedSegment serves one byte-bounded segment of key inside scope over the
// given reference and window state, on the terms of SegmentRequest and
// SegmentResult. It classifies exactly as scopedRecall does before it cuts any
// byte, so a read refused inside scope names its refusal and leaks nothing.
func scopedSegment(byKey map[event.EventID]*originalEntry, windows map[string]time.Duration, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error) {
	record, blob, held := scopedReference(byKey, key, scope)
	if !held {
		return SegmentResult{}, ErrOriginalMissing
	}
	if !readableWithin(windows, record, time.Now()) {
		return SegmentResult{}, ErrOriginalExpired
	}
	if err := blob.verify(); err != nil {
		return SegmentResult{}, err
	}
	return segmentBody(blob.bytes, req)
}

// scopedReference resolves the reference of key registered inside scope and the
// blob it names. The last return reports whether any such reference exists: a
// key whose references all lie outside scope resolves to none of them, exactly
// as a key no reference names at all.
func scopedReference(byKey map[event.EventID]*originalEntry, key event.EventID, scope ReadScope) (referenceRecord, *originalBlob, bool) {
	entry, held := byKey[key]
	if !held || entry.blob == nil {
		return referenceRecord{}, nil, false
	}
	record, registered := entry.refs[scope.identity()]
	if !registered {
		return referenceRecord{}, nil, false
	}
	return record, entry.blob, true
}

// readableWithin reports whether record is inside the retention window of its
// policy against the given policy windows: a policy with no registered window
// keeps its references readable, exactly as the tiers read theirs.
func readableWithin(windows map[string]time.Duration, record referenceRecord, now time.Time) bool {
	window, registered := windows[record.ref.RetentionPolicy]
	if !registered {
		return true
	}
	return now.Sub(record.storedAt) < window
}
