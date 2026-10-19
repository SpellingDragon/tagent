package memoryledgerexample

import (
	"context"

	"github.com/SpellingDragon/tagent/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// scopedReference is one reference registered inside a read scope.
type scopedReference struct {
	key     event.EventID
	scope   ledger.ReadScope
	body    []byte
	expired bool
}

// scopedStore is the canonical scoped read face: it aggregates only the references
// registered inside the scope the read names.
type scopedStore struct {
	refs []scopedReference
}

// GetScoped returns the exact recall of key as visible inside scope, and reads a key whose
// references all lie outside scope as missing.
func (s *scopedStore) GetScoped(_ context.Context, key event.EventID, scope ledger.ReadScope) ledger.RecallResult {
	for _, ref := range s.refs {
		if ref.key != key || ref.scope != scope {
			continue
		}
		if ref.expired {
			return ledger.RecallResult{Key: key, Outcome: ledger.RecallExpired}
		}
		return ledger.RecallResult{Key: key, Outcome: ledger.RecallFound, Body: append([]byte(nil), ref.body...)}
	}
	return ledger.RecallResult{Key: key, Outcome: ledger.RecallMissing}
}

// ReadSegmentScoped returns one byte-bounded segment of the original as visible inside
// scope.
func (s *scopedStore) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope, req ledger.SegmentRequest) (ledger.SegmentResult, error) {
	result := s.GetScoped(ctx, key, scope)
	switch result.Outcome {
	case ledger.RecallFound:
	case ledger.RecallExpired:
		return ledger.SegmentResult{}, ledger.ErrOriginalExpired
	default:
		return ledger.SegmentResult{}, ledger.ErrOriginalMissing
	}
	size := req.MaxBytes
	if size <= 0 || size > int64(len(result.Body)) {
		size = int64(len(result.Body))
	}
	body := result.Body[:size]
	next := ledger.SegmentPosition{Offset: req.From.Offset + int64(len(body))}
	return ledger.SegmentResult{Body: body, Next: next, EOF: next.Offset >= int64(len(result.Body))}, nil
}

var _ ledger.ScopedOriginalStore = (*scopedStore)(nil)

// ExampleScopedOriginalStore reads one original inside the scope it was registered under,
// cuts one byte-bounded segment at a byte boundary and reports the same key as missing
// under another scope.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func ExampleScopedOriginalStore() {
	ctx := context.Background()
	scope := ledger.ReadScope{
		Owner:           event.Address{AgentID: "assistant", SessionID: "session-1"},
		Format:          "text",
		RetentionPolicy: "durable",
	}
	store := &scopedStore{refs: []scopedReference{
		{key: "event-1", scope: scope, body: []byte("original text")},
	}}

	if result := store.GetScoped(ctx, "event-1", scope); result.Outcome != ledger.RecallFound {
		return
	}
	segment, err := store.ReadSegmentScoped(ctx, "event-1", scope, ledger.SegmentRequest{MaxBytes: 8})
	if err != nil || len(segment.Body) != 8 || segment.EOF {
		return
	}
	other := ledger.ReadScope{Owner: event.Address{AgentID: "other", SessionID: "session-2"}, Format: "text", RetentionPolicy: "durable"}
	if result := store.GetScoped(ctx, "event-1", other); result.Outcome != ledger.RecallMissing {
		return
	}
	if _, err := store.ReadSegmentScoped(ctx, "event-1", other, ledger.SegmentRequest{MaxBytes: 8}); err != ledger.ErrOriginalMissing {
		return
	}
}
