package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
//
// Externalizer is the oversized-output port: it commits the durable original of a
// result, materialises a replica of it on disk, and returns a bounded preview together
// with the byte-bounded segment entry that reaches the whole result.
//
// The redline list (4 rules):
// MUST commit the durable original before the replica or the preview exists.
// MUST report a replica write that failed as ErrReplicaWriteFailed beside the still-effective reference.
// MUST NOT serve a bounded preview in place of an original that was never kept.
// MUST NOT discard the original a failed materialisation was made from.
type Externalizer interface {
	// Externalize commits body as the durable original of ref, materialises a replica
	// of it, and returns the bounded preview and the segment entry for it. A replica
	// write that fails is reported as ErrReplicaWriteFailed, while the returned
	// reference stays effective.
	Externalize(ctx context.Context, ref memory.OriginalRef, body []byte) (Externalized, error)
}

// Externalized is one externalized oversized result: the durable reference its bytes
// were committed under, the bounded preview that stands in for the whole result, and
// the byte-bounded segment entry through which the whole original is read back.
type Externalized struct {
	// Original is the reference the original bytes were committed under. It stays
	// effective even when the replica write failed.
	Original memory.OriginalRef
	// Preview is the bounded text presented in place of the whole result.
	Preview string
	// Segment is the byte-bounded entry a later read reaches the whole original
	// through, on exactly the terms of memory.SegmentRequest and
	// memory.SegmentResult: the entry names the byte position the read starts at
	// and the maximum number of bytes one segment returns, and the read answers
	// with the segment bytes, the byte position the next segment resumes from and
	// whether the end of the original was reached. A zero MaxBytes is served as
	// memory.DefaultSegmentMaxBytes and never as an unbounded read, segments are
	// cut at byte boundaries whatever the content holds and concatenate back to
	// the exact bytes of the original, and a segment that reaches the end reports
	// EOF with a zero Next that carries no resumption point. A line count bounds
	// no byte at all, so the entry carries a byte offset and a byte bound only.
	Segment memory.SegmentRequest
}

// ErrReplicaWriteFailed reports that an oversized result was committed as an original,
// but its on-disk replica could not be written. The durable reference of that original
// stays effective, so the result is still served from the original.
var ErrReplicaWriteFailed = errors.New("workspace: replica write failed")

// DefaultMaxTokens is the model window the interception threshold falls back to when a
// caller names none: a zero MaxTokens is read as 8000, so the threshold is never
// computed from a window no model has.
const DefaultMaxTokens = 8000

// CharsPerToken is the character estimate one token carries when the interception
// threshold is computed, so the bound is expressed in the characters a model result
// actually spends.
const CharsPerToken = 4

// DefaultPreviewRunes is the number of leading characters one bounded preview keeps
// before the ticket names the whole original. The preview is further narrowed to a
// quarter of the threshold, so a preview never spends the budget it stands in for.
const DefaultPreviewRunes = 2000

// FormatToolResult names the result formats the four producers of oversized results
// commit their originals under. Every producer takes the one externalize path below and
// differs only in the format it is committed in, so a generic tool result, a task result,
// a tmux output and an overflow-bound result share one threshold and one preview form,
// and none of them grows a truncation of its own.
const (
	// FormatToolResult is the format of a generic tool result.
	FormatToolResult = "tool-output"
	// FormatTaskResult is the format of a task result.
	FormatTaskResult = "task-result"
	// FormatTmuxOutput is the format of a tmux output.
	FormatTmuxOutput = "tmux-output"
	// FormatOverflow is the format of a result externalized because it overflowed its
	// bound.
	FormatOverflow = "overflow"
)

// ThresholdForMaxTokens returns the character count above which a result is
// externalized: MaxTokens/2*4, the one formula every producer decides on. A zero or
// negative MaxTokens is read as DefaultMaxTokens, so the threshold is always finite and
// always aligned with the model window the result is spent in.
func ThresholdForMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	return maxTokens / 2 * CharsPerToken
}

// ErrOriginalStoreRequired reports an externalizer constructed without the original
// store it commits results into: no original could be kept before a preview of it is
// served.
var ErrOriginalStoreRequired = errors.New("workspace: externalizing a result requires an original store")

// ErrResultScopeRequired reports a result scope that names no owner, format or retention
// policy: a reference committed under it aligns with no read scope, so the original
// could never be recalled under the authority it was produced for.
var ErrResultScopeRequired = errors.New("workspace: the result scope names no owner, format or retention policy")

// ErrToolRequired reports a bounded tool constructed without the native callable tool it
// intercepts, so there is no call whose result could be bounded.
var ErrToolRequired = errors.New("workspace: the bounded tool wraps no callable tool")

// ResultScope names the reference metadata one producer commits its oversized results
// under: the owner the result is produced for, the format it is produced in, and the
// retention policy its original is held under. It is the deduplication scope 03.1
// registers references with, so two producers of byte-identical results share the blob
// while their references stay side by side.
type ResultScope struct {
	// Owner is the address the result is produced for.
	Owner event.Address
	// Format is the format the result is produced in, one of the Format* constants for
	// the four registered producers.
	Format string
	// RetentionPolicy is the retention policy the original is held under.
	RetentionPolicy string
}

// OriginalRefFor returns the reference one result body is committed under inside
// scope: the key is the SHA-256 digest of the body in hex, so the same bytes produce the
// same key whatever producer meets them, while the owner, format and retention policy
// keep the references of different scopes apart. Every producer derives its reference
// here, so a result is committed under one identity regardless of which of the four
// producers externalized it.
func OriginalRefFor(scope ResultScope, body []byte) memory.OriginalRef {
	digest := sha256.Sum256(body)
	return memory.OriginalRef{
		Key:             event.EventID(hex.EncodeToString(digest[:])),
		Owner:           scope.Owner,
		Format:          scope.Format,
		RetentionPolicy: scope.RetentionPolicy,
	}
}

// ResultExternalizer is the one externalize path of the runtime: it commits the
// original of an oversized result before any preview of that result exists, and it
// answers with a bounded preview and the byte-bounded segment entry the whole original
// is read back through.
//
// Generic tool results, task results, tmux outputs and overflow-bound results all run
// through this one path, each under its own Format* name, so the threshold, the preview
// form and the commit order are stated once. A replica materialised on disk is optional
// and stays beside the original: a failed replica write never discards the original it
// was made from, and a replica reclaimed later is materialised again from that original.
type ResultExternalizer struct {
	originals    memory.OriginalStore
	materializer *Materializer
	threshold    int
	consumer     string
	previewRunes int
	segmentBytes int64
}

// ExternalizerOption names one setting of the externalize path at construction time.
type ExternalizerOption func(*ResultExternalizer)

// WithMaxTokens sets the model window the threshold is computed from, through
// ThresholdForMaxTokens. A zero or negative value is served as DefaultMaxTokens.
func WithMaxTokens(maxTokens int) ExternalizerOption {
	return func(externalizer *ResultExternalizer) {
		externalizer.threshold = ThresholdForMaxTokens(maxTokens)
	}
}

// WithResultMaterializer wires the replica materializer an externalized result is
// materialised through, so the oversized result stands on disk beside the original it
// was committed as. A materializer bound to another read scope than the references
// externalized here is refused by name at externalization, before any original is
// committed.
func WithResultMaterializer(materializer *Materializer) ExternalizerOption {
	return func(externalizer *ResultExternalizer) { externalizer.materializer = materializer }
}

// WithExternalizeConsumer names the consumer identity replicas of externalized results
// are materialised for, so two consumers of one original land on their own stable paths
// instead of overwriting one another. An empty name keeps the default identity.
func WithExternalizeConsumer(consumer string) ExternalizerOption {
	return func(externalizer *ResultExternalizer) {
		if consumer != "" {
			externalizer.consumer = consumer
		}
	}
}

// WithPreviewRunes bounds the preview to at most runes leading characters. A zero or
// negative bound keeps DefaultPreviewRunes; either way the preview is narrowed to a
// quarter of the threshold, so it stays well below the bound it stands in for.
func WithPreviewRunes(runes int) ExternalizerOption {
	return func(externalizer *ResultExternalizer) {
		if runes > 0 {
			externalizer.previewRunes = runes
		}
	}
}

// NewResultExternalizer returns the externalize path over originals, with the
// threshold, the preview bound and the optional replica materializer the caller names.
// A nil original store is refused by name before any result could be externalized.
func NewResultExternalizer(originals memory.OriginalStore, options ...ExternalizerOption) (*ResultExternalizer, error) {
	if originals == nil {
		return nil, ErrOriginalStoreRequired
	}
	externalizer := &ResultExternalizer{
		originals:    originals,
		threshold:    ThresholdForMaxTokens(0),
		consumer:     "externalize",
		previewRunes: DefaultPreviewRunes,
		segmentBytes: memory.DefaultSegmentMaxBytes,
	}
	for _, option := range options {
		option(externalizer)
	}
	return externalizer, nil
}

// Threshold returns the character count above which a result is externalized, so a
// producer can decide on exactly the bound this path commits under.
func (e *ResultExternalizer) Threshold() int { return e.threshold }

// Exceeds reports whether a serialized result of the given character count is over the
// threshold and has to take the externalize path. The four producers decide here, so no
// producer carries a bound of its own.
func (e *ResultExternalizer) Exceeds(chars int) bool { return chars > e.threshold }

// DecideExternalize answers the overflow decision face of the plugin pipeline: a result
// of size characters is externalized exactly when it exceeds the one threshold, so an
// overflowing result takes the same path as every other oversized result instead of a
// policy of its own.
func (e *ResultExternalizer) DecideExternalize(ctx context.Context, id event.EventID, size int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return e.Exceeds(size), nil
}

// Externalize commits body as the durable original of ref, materialises an optional
// replica of it, and answers with the bounded preview and the segment entry for it. The
// original is committed before the preview exists, so a preview is never served in place
// of an original that was never kept: a commit that fails is reported with no preview at
// all.
//
// A reference outside the scope the wired materializer reads in is refused by name
// before any original is committed, and a replica write that fails is reported as
// ErrReplicaWriteFailed beside the still effective reference and its preview.
func (e *ResultExternalizer) Externalize(ctx context.Context, ref memory.OriginalRef, body []byte) (Externalized, error) {
	if ref.Key == "" {
		return Externalized{}, memory.ErrKeyRequired
	}
	if e.materializer != nil && ref.ReadScope() != e.materializer.Reader().Scope() {
		return Externalized{}, ErrReplicaUnauthorized
	}
	if err := e.originals.Put(ctx, ref, body); err != nil {
		return Externalized{}, err
	}
	externalized := Externalized{
		Original: ref,
		Preview:  e.boundedPreview(body, ref),
		Segment:  memory.SegmentRequest{From: memory.SegmentPosition{}, MaxBytes: e.segmentBytes},
	}
	if e.materializer == nil {
		return externalized, nil
	}
	if _, err := e.materializer.Materialize(ctx, ref, e.consumer); err != nil {
		return externalized, fmt.Errorf("%w: %w", ErrReplicaWriteFailed, err)
	}
	return externalized, nil
}

// boundedPreview renders the bounded preview of one externalized result: a leading run
// of the original followed by the ticket that names the original, its total size and the
// segment entry the whole of it is read back through. The leading run is cut at a rune
// boundary and is already over by the time the ticket is appended, so the preview stays
// a presentation of the original and never stands in for it.
func (e *ResultExternalizer) boundedPreview(body []byte, ref memory.OriginalRef) string {
	bound := e.previewRunes
	if quarter := e.threshold / 4; quarter < bound {
		bound = quarter
	}
	if bound < 0 {
		bound = 0
	}
	head := string(body)
	if runes := []rune(head); len(runes) > bound {
		head = string(runes[:bound])
	}
	return fmt.Sprintf("%s\n[externalized result: %d bytes in total, original %s kept; read it in segments of at most %d bytes from offset 0]",
		head, len(body), ref.Key, e.segmentBytes)
}

// BoundedTool is the native wrapper injection port of the externalize path: it wraps one
// native callable tool and routes an oversized result through the one externalize path
// before that result reaches the model. A result at or below the threshold is handed
// back unchanged, a nil result is not intercepted at all, and the declaration of the
// wrapped tool is passed through untouched.
//
// The wrapper is a tagent-face type rather than a second tool protocol: the composition
// root builds it over the native callable tool of an agent, so every tool family — the
// generic shell and file tools and the agent-kind tools alike — is bounded on the same
// path without a root tool package or a truncation of its own.
type BoundedTool struct {
	inner        trpctool.CallableTool
	externalizer *ResultExternalizer
	scope        ResultScope
}

// NewBoundedTool returns the bounded tool over inner, committing oversized results
// under scope through externalizer. A missing tool, a missing externalizer and a scope
// that names no owner, format or retention policy are each refused by name.
func NewBoundedTool(inner trpctool.CallableTool, externalizer *ResultExternalizer, scope ResultScope) (*BoundedTool, error) {
	if inner == nil {
		return nil, ErrToolRequired
	}
	if externalizer == nil {
		return nil, ErrOriginalStoreRequired
	}
	if scope.Format == "" || scope.RetentionPolicy == "" || scope.Owner.IsZero() {
		return nil, ErrResultScopeRequired
	}
	return &BoundedTool{inner: inner, externalizer: externalizer, scope: scope}, nil
}

// BoundedTool satisfies the native callable tool port.
var _ trpctool.CallableTool = (*BoundedTool)(nil)

// Declaration returns the declaration of the wrapped tool unchanged, so bounding a
// result changes nothing the model is told about the tool itself.
func (t *BoundedTool) Declaration() *trpctool.Declaration { return t.inner.Declaration() }

// Call runs one native call and bounds what it returns: a result whose serialized form
// is over the threshold is committed as a durable original and answered with its bounded
// preview and ticket, an error from the wrapped tool is passed through untouched, a nil
// result is not intercepted, a result that cannot be serialized is handed back unchanged
// because no size could bound it, and a result inside the bound is returned exactly as
// the tool produced it.
//
// An original that could not be committed is reported as an error with no preview, so a
// ticket never names an original that was not kept. A replica that could not be written
// is reported beside the preview the committed original still answers: the preview is
// returned together with ErrReplicaWriteFailed rather than dropping a result whose
// original is safe.
func (t *BoundedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	result, err := t.inner.Call(ctx, jsonArgs)
	if err != nil {
		return result, err
	}
	if result == nil {
		return nil, nil
	}
	body, err := json.Marshal(result)
	if err != nil {
		return result, nil
	}
	if !t.externalizer.Exceeds(len(body)) {
		return result, nil
	}
	externalized, err := t.externalizer.Externalize(ctx, OriginalRefFor(t.scope, body), body)
	if err != nil && !errors.Is(err, ErrReplicaWriteFailed) {
		return nil, fmt.Errorf("workspace: externalizing the oversized result: %w", err)
	}
	return externalized.Preview, err
}
