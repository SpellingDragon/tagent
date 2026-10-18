package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ErrKeyRequired reports a commit that carries no key, so no read could ever
// name the original again.
var ErrKeyRequired = errors.New("memory: original reference carries no key")

// ErrIntegrityMismatch reports original bytes that do not match the completeness
// information recorded for them, so the read is refused instead of served.
var ErrIntegrityMismatch = errors.New("memory: bytes do not match the recorded integrity")

// ErrContentConflict reports a commit whose key already names other bytes: one
// key names one original, so the second content is refused.
var ErrContentConflict = errors.New("memory: key already names other bytes")

// ErrOriginalMissing reports a segmented read of a key no reference names.
var ErrOriginalMissing = errors.New("memory: no reference names the key")

// ErrOriginalExpired reports a segmented read of a key whose references are all
// outside their retention windows.
var ErrOriginalExpired = errors.New("memory: every reference is outside its retention window")

// ErrSegmentRange reports a segment request that starts before the first line or
// asks for no lines at all.
var ErrSegmentRange = errors.New("memory: segment must start at line 1 or later and read at least one line")

// ErrSegmentPastEnd reports a segment request whose start line lies past the
// last stored line.
var ErrSegmentPastEnd = errors.New("memory: segment starts past the last line")

// MemoryOption shapes one MemoryOriginalStore at construction.
type MemoryOption func(*MemoryOriginalStore)

// WithRetentionWindow registers the window one retention policy keeps its
// references readable for. A policy with no registered window keeps its
// references readable indefinitely, and a window of zero or less reports every
// read as expired.
func WithRetentionWindow(policy string, window time.Duration) MemoryOption {
	return func(store *MemoryOriginalStore) {
		store.windows[policy] = window
	}
}

// originalBlob is one stored content, shared by every reference that carries the
// same bytes, together with the completeness information those bytes were
// committed with.
type originalBlob struct {
	digest string
	length int64
	bytes  []byte
}

// verify reports whether the stored bytes still match the completeness
// information recorded for them.
func (b *originalBlob) verify() error {
	if int64(len(b.bytes)) != b.length || digestOf(b.bytes) != b.digest {
		return ErrIntegrityMismatch
	}
	return nil
}

// referenceRecord is one registered reference: the metadata it was accepted
// under and the time it was accepted, which its retention window is measured
// from.
type referenceRecord struct {
	ref      OriginalRef
	storedAt time.Time
}

// originalEntry is everything held under one key: the blob its bytes live in and
// one reference record per deduplication scope that registered it.
type originalEntry struct {
	blob *originalBlob
	refs map[refScope]referenceRecord
}

// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// MemoryOriginalStore is the in-memory OriginalStore: the reference and blob
// model of the original text — bytes committed once per distinct content, every
// reference registered under its own owner, format and retention policy, exact
// recall served byte for byte and segmented reading handing out whole lines.
//
// The model is implemented once over the key-value seam, and this tier is that
// implementation held in a view that never leaves the process: a durable tier
// runs the same code over its own view, so nothing about recall, retention or
// deduplication is restated per tier. The tier holds only what the process
// holds: it survives no restart, and no result it returns claims otherwise.
type MemoryOriginalStore struct {
	*kvOriginalStore
}

// NewMemoryOriginalStore returns an empty in-memory original store, shaped by
// the given options.
func NewMemoryOriginalStore(options ...MemoryOption) *MemoryOriginalStore {
	store := &MemoryOriginalStore{kvOriginalStore: newEmptyKVOriginalStore(newMemKV(), nil)}
	for _, option := range options {
		option(store)
	}
	return store
}

var _ OriginalStore = (*MemoryOriginalStore)(nil)

// digestOf returns the canonical digest text of the given bytes.
func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// splitLines splits body into whole lines, each keeping its trailing newline, so
// concatenating every line reproduces the stored bytes exactly.
func splitLines(body []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range body {
		if b == '\n' {
			lines = append(lines, body[start:i+1])
			start = i + 1
		}
	}
	if start < len(body) {
		lines = append(lines, body[start:])
	}
	return lines
}
