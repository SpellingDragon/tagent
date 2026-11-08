package recall

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// traceStub answers one parent or one named error for the parentOf tests.
type traceStub struct {
	parent event.EventID
	err    error
}

// GetParent returns the configured answer.
func (s traceStub) GetParent(event.EventID) (event.EventID, error) { return s.parent, s.err }

// tableStub answers the declared durable table from fixed maps, and names the
// failure of the keys a check marks unreadable through ChainFailureNamer.
type tableStub struct {
	events   map[event.EventID]memory.EventReference
	refs     map[event.EventID]ledger.OriginalRef
	failures map[event.EventID]error
}

// QueryEvents answers no match: the exact read never queries.
func (s tableStub) QueryEvents(context.Context, memory.QueryOptions) ([]memory.EventReference, error) {
	return nil, nil
}

// Event answers the fixed reference of one key.
func (s tableStub) Event(key event.EventID) (memory.EventReference, bool) {
	reference, held := s.events[key]
	return reference, held
}

// Reference answers the fixed original reference of one key.
func (s tableStub) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	reference, held := s.refs[key]
	return reference, held
}

// ChainFailure names the failure under one key and the empty nil otherwise.
func (s tableStub) ChainFailure(key event.EventID) error { return s.failures[key] }

// TestParentOfNamesRelationError pins parent failures apart from no parent.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestParentOfNamesRelationError(t *testing.T) {
	tool := &getTool{base: base{reads: Reads{Trace: traceStub{err: errors.New("parent read failed")}}}}
	item, err := tool.parentOf("child")
	if item != nil || err == nil {
		t.Fatalf("the failing trace returned item=%v err=%v, want the failure named", item, err)
	}
	if !strings.Contains(err.Error(), "parent read failed") {
		t.Fatalf("the failing trace answered %v, want the cause it carried", err)
	}
	tool.reads.Trace = traceStub{parent: ""}
	item, err = tool.parentOf("child")
	if item != nil || err != nil {
		t.Fatalf("the empty parent returned item=%v err=%v, want no parent and no failure", item, err)
	}
	tool.reads.Table = tableStub{failures: map[event.EventID]error{"child-parent": ledger.ErrSegmentBodyMissing}}
	tool.reads.Trace = traceStub{parent: "child-parent"}
	item, err = tool.parentOf("child")
	if item != nil || !errors.Is(err, ledger.ErrSegmentBodyMissing) {
		t.Fatalf("the corrupt parent returned item=%v err=%v, want the corruption named", item, err)
	}
}

// TestRecallReasonFoundConsistent pins the answer vocabulary: an answer that served text carries no reason, whatever else the read had to report.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestRecallReasonFoundConsistent(t *testing.T) {
	ctx := context.Background()
	originals := ledger.NewMemoryOriginalStore()
	events := map[event.EventID]memory.EventReference{}
	refs := map[event.EventID]ledger.OriginalRef{}
	for _, key := range []event.EventID{"served", "parented", "orphaned"} {
		ref := ledger.OriginalRef{Key: key, Format: "text"}
		if err := originals.Put(ctx, ref, []byte("the served text of "+string(key)+"\n")); err != nil {
			t.Fatalf("commit the original of %s: %v", key, err)
		}
		refs[key] = ref
		events[key] = memory.EventReference{Key: key, PartitionID: memory.PartitionIDFromName("agent-a"), Type: event.TypeExternalInput, Summary: "ask"}
	}
	tool := &getTool{base: newBase(Reads{
		Table:     tableStub{events: events, refs: refs},
		Originals: originals,
		Trace:     traceStub{},
	}, []string{"agent-a"})}
	for _, call := range []struct {
		name string
		args string
	}{
		{name: "served text alone", args: `{"key":"served"}`},
		{name: "served text beside no parent", args: `{"key":"orphaned","include_parent":true}`},
	} {
		answered, err := tool.Call(ctx, []byte(call.args))
		if err != nil {
			t.Fatalf("%s refused with %v, want the segment served", call.name, err)
		}
		answer := getAnswer{}
		if err := json.Unmarshal([]byte(answered.(string)), &answer); err != nil {
			t.Fatalf("%s answered undecodable text: %v", call.name, err)
		}
		if !answer.Found {
			t.Fatalf("%s answered found=false, want the served text reported", call.name)
		}
		if answer.Reason != "" {
			t.Fatalf("%s answered found=true beside reason=%q, want the two never contradicting", call.name, answer.Reason)
		}
	}
	tool.reads.Trace = traceStub{err: errors.New("parent read failed")}
	if answered, err := tool.Call(ctx, []byte(`{"key":"parented","include_parent":true}`)); err == nil {
		t.Fatalf("the failing parent read answered %v, want the failure named", answered)
	}
}
