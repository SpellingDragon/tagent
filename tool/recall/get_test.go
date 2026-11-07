package recall

import (
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// traceStub answers one parent or one named error for the parentOf tests.
type traceStub struct {
	parent event.EventID
	err    error
}

// GetParent returns the configured answer.
func (s traceStub) GetParent(event.EventID) (event.EventID, error) { return s.parent, s.err }

// TestParentOfNamesRelationError pins parent failures apart from no parent.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestParentOfNamesRelationError(t *testing.T) {
	tool := &getTool{base: base{reads: Reads{Trace: traceStub{err: errors.New("parent read failed")}}}}
	item, reason := tool.parentOf("child")
	if item != nil || reason != reasonRelationError {
		t.Fatalf("the failing trace returned item=%v reason=%q, want relation_error", item, reason)
	}
	tool.reads.Trace = traceStub{parent: ""}
	item, reason = tool.parentOf("child")
	if item != nil || reason != "" {
		t.Fatalf("the empty parent returned item=%v reason=%q, want no parent and no error", item, reason)
	}
}
