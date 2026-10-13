// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Package memory is the runtime's durable original-text layer: it names one
// stored original and the reference metadata that scopes it, commits those bytes
// and returns them byte for byte, and carries the ordered projection and the
// retention guard built beside them.
//
// The package speaks the fact vocabulary of the event package and nothing above
// it, so the durable side cannot be reversed into the intelligent or governing
// side. This is the phase 1 skeleton: it declares shapes and holds no behaviour.
package memory

import (
	"context"

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
// It is the port replicas are materialised from and exact recall is served from,
// so a read never answers with a model view in place of the original. Bytes are
// committed before any bounded preview or segment entry exists.
type OriginalStore interface {
	// Put commits the original bytes of ref. Content may be deduplicated by
	// blob, while owner, format and retention policy scope the reference
	// metadata, so an accepted reference is never invalidated by a later Put.
	Put(ctx context.Context, ref OriginalRef, body []byte) error

	// Get returns the exact recall result for key: the original bytes,
	// byte-identical to the bytes Put accepted, or a named
	// missing/expired/failed classification (see RecallResult). Wiring Get to
	// RecallResult gives the classification a real consumer (review gate P2-2).
	Get(ctx context.Context, key event.EventID) RecallResult

	// ReadSegment returns at most numLines lines of the original stored under
	// key starting at line startLine, so an oversized original is consumed by
	// segment rather than returned whole.
	ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
}
