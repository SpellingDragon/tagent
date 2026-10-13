package workspace

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/memory"
)

// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
//
// Externalizer is the oversized-output port: it commits the durable original of a
// result, materialises a replica of it on disk, and returns a bounded preview together
// with the segment entry that reaches the whole result.
//
// The original is committed before the replica or the preview exists, so a bounded view
// is never served in place of an original that was never kept. A replica that cannot be
// written is reported as ErrReplicaWriteFailed alongside the durable reference, which
// stays effective: a failed materialisation never discards the original it was made
// from, and that original remains the source a later materialisation is served from.
type Externalizer interface {
	// Externalize commits body as the durable original of ref, materialises a replica
	// of it, and returns the bounded preview and the segment entry for it. A replica
	// write that fails is reported as ErrReplicaWriteFailed, while the returned
	// reference stays effective.
	Externalize(ctx context.Context, ref memory.OriginalRef, body []byte) (Externalized, error)
}

// Externalized is one externalized oversized result: the durable reference its bytes
// were committed under, the bounded preview that stands in for the whole result, and
// the segment entry through which the whole original is read back.
type Externalized struct {
	// Original is the reference the original bytes were committed under. It stays
	// effective even when the replica write failed.
	Original memory.OriginalRef
	// Preview is the bounded text presented in place of the whole result.
	Preview string
	// Segment is the entry a later read reaches the whole original through.
	Segment SegmentEntry
}

// SegmentEntry is the entry a segment read reaches one original through: the first line
// the segment starts at and the maximum number of lines one segment returns.
type SegmentEntry struct {
	// StartLine is the first line the segment starts at.
	StartLine int
	// NumLines is the maximum number of lines one segment read returns.
	NumLines int
}

// ErrReplicaWriteFailed reports that an oversized result was committed as an original,
// but its on-disk replica could not be written. The durable reference of that original
// stays effective, so the result is still served from the original.
var ErrReplicaWriteFailed = errors.New("workspace: replica write failed")
