package recall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// traceChainTable answers one fixed chain and names every key it was asked for,
// so the walk's reads are countable.
type traceChainTable struct {
	events map[event.EventID]memory.EventReference
	reads  []event.EventID
}

// QueryEvents refuses an enumeration: a causal walk reads its chain by identity,
// so a match set is a cost this walk never pays.
func (c *traceChainTable) QueryEvents(context.Context, memory.QueryOptions) ([]memory.EventReference, error) {
	return nil, errors.New("the causal walk asked for a match set")
}

// Event answers the fixed reference of one key and names it.
func (c *traceChainTable) Event(key event.EventID) (memory.EventReference, bool) {
	c.reads = append(c.reads, key)
	reference, held := c.events[key]
	return reference, held
}

// Reference answers no original: the walk answers items and never text.
func (c *traceChainTable) Reference(event.EventID) (ledger.OriginalRef, bool) {
	return ledger.OriginalRef{}, false
}

// traceChainParents answers the parent links of one fixed chain and names every
// key it was asked for.
type traceChainParents struct {
	parent map[event.EventID]event.EventID
	reads  []event.EventID
}

// GetParent answers the configured parent of one key and names it.
func (c *traceChainParents) GetParent(key event.EventID) (event.EventID, error) {
	c.reads = append(c.reads, key)
	return c.parent[key], nil
}

// traceErrorParents answers one fixed parent-read failure.
type traceErrorParents struct{ err error }

// GetParent returns the configured failure.
func (t traceErrorParents) GetParent(event.EventID) (event.EventID, error) {
	return "", t.err
}

// TestTraceKeepsEventChainFailuresNamed pins failures from an unreadable starting or parent event reaching the caller by name.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestTraceKeepsEventChainFailuresNamed(t *testing.T) {
	items := map[event.EventID]memory.EventReference{
		"chain-2": {Key: "chain-2", PartitionID: memory.PartitionIDFromName("agent-a")},
	}
	for _, failure := range []error{ledger.ErrArchiveMissing, ledger.ErrArchiveCorrupt, errors.New("chain read failed")} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Run("starting event", func(t *testing.T) {
				tool := &traceTool{base: newBase(Reads{
					Table: tableStub{failures: map[event.EventID]error{"absent-start": failure}},
					Trace: &traceChainParents{},
				}, []string{"agent-a"})}
				answered, err := tool.Call(context.Background(), []byte(`{"key":"absent-start"}`))
				if !errors.Is(err, failure) || answered != nil {
					t.Fatalf("the archive-failed start answered (%v, %v), want the named failure", answered, err)
				}
			})
			t.Run("parent event", func(t *testing.T) {
				tool := &traceTool{base: newBase(Reads{
					Table: tableStub{
						events:   items,
						failures: map[event.EventID]error{"absent-parent": failure},
					},
					Trace: &traceChainParents{parent: map[event.EventID]event.EventID{"chain-2": "absent-parent"}},
				}, []string{"agent-a"})}
				answered, err := tool.Call(context.Background(), []byte(`{"key":"chain-2"}`))
				if !errors.Is(err, failure) || answered != nil {
					t.Fatalf("the archive-failed parent answered (%v, %v), want the named failure", answered, err)
				}
			})
		})
	}
}

// TestTraceKeepsArchiveFailuresNamed pins archive faults staying named and ordinary chain stops keeping their reasons.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestTraceKeepsArchiveFailuresNamed(t *testing.T) {
	items := map[event.EventID]memory.EventReference{
		"chain-2": {Key: "chain-2", PartitionID: memory.PartitionIDFromName("agent-a")},
	}
	for _, failure := range []error{ledger.ErrArchiveMissing, ledger.ErrArchiveCorrupt} {
		t.Run(failure.Error(), func(t *testing.T) {
			tool := &traceTool{base: newBase(Reads{Table: &traceChainTable{events: items}, Trace: traceErrorParents{err: failure}}, []string{"agent-a"})}
			answered, err := tool.Call(context.Background(), []byte(`{"key":"chain-2"}`))
			if !errors.Is(err, failure) || answered != nil {
				t.Fatalf("the archive-failed trace answered (%v, %v), want the named failure", answered, err)
			}
		})
	}
	relation := errors.New("relation read failed")
	tool := &traceTool{base: newBase(Reads{Table: &traceChainTable{events: items}, Trace: traceErrorParents{err: relation}}, []string{"agent-a"})}
	answered, err := tool.Call(context.Background(), []byte(`{"key":"chain-2"}`))
	if err != nil {
		t.Fatalf("the ordinary relation failure returned an error: %v", err)
	}
	var answer traceAnswer
	if err := json.Unmarshal([]byte(answered.(string)), &answer); err != nil {
		t.Fatalf("decode the relation-failure answer: %v", err)
	}
	if answer.Reason != reasonRelationError {
		t.Fatalf("the ordinary relation failure answered %+v, want reason %q", answer, reasonRelationError)
	}
	missing := &traceTool{base: newBase(Reads{
		Table: &traceChainTable{events: items},
		Trace: &traceChainParents{parent: map[event.EventID]event.EventID{"chain-2": "absent-parent"}},
	}, []string{"agent-a"})}
	answered, err = missing.Call(context.Background(), []byte(`{"key":"chain-2"}`))
	if err != nil {
		t.Fatalf("the missing ancestor returned an error: %v", err)
	}
	if err := json.Unmarshal([]byte(answered.(string)), &answer); err != nil {
		t.Fatalf("decode the missing-ancestor answer: %v", err)
	}
	if answer.Reason != reasonMissingAncestor {
		t.Fatalf("the missing ancestor answered %+v, want reason %q", answer, reasonMissingAncestor)
	}
}

// TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks pins what one causal walk pays.
//   - a chain of twelve facts answers twelve table lookups and twelve parent lookups, one of each per fact the answer carries
//   - negative: a step that read the chain instead of its own fact answers more lookups than the depth, and the count names both beside the depth
//   - the keys either face was asked for are the chain's alone, so no step enumerates the history the chain stands in
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks(t *testing.T) {
	const depth = 12
	chain := make([]event.EventID, 0, depth)
	items := map[event.EventID]memory.EventReference{}
	parents := map[event.EventID]event.EventID{}
	partition := memory.PartitionIDFromName("agent-a")
	for at := 1; at <= depth; at++ {
		key := event.EventID(fmt.Sprintf("chain-%d", at))
		chain = append(chain, key)
		items[key] = memory.EventReference{Key: key, PartitionID: partition, Type: event.TypeExternalInput, Summary: "ask", Timestamp: int64(at)}
		if at > 1 {
			parents[key] = chain[at-2]
		}
	}
	table := &traceChainTable{events: items}
	trace := &traceChainParents{parent: parents}
	tool := &traceTool{base: newBase(Reads{Table: table, Trace: trace}, []string{"agent-a"})}
	encoded, err := tool.Call(context.Background(), []byte(fmt.Sprintf(`{"key":%q,"max_steps":50}`, chain[depth-1])))
	if err != nil {
		t.Fatalf("walk the chain of %d facts: %v", depth, err)
	}
	var answer traceAnswer
	if err := json.Unmarshal([]byte(encoded.(string)), &answer); err != nil {
		t.Fatalf("decode the walk answer: %v", err)
	}
	if !answer.Complete || answer.Count != depth || len(answer.Items) != depth {
		t.Fatalf("the walk answered count=%d items=%d complete=%v, want the %d facts of the chain up to its root",
			answer.Count, len(answer.Items), answer.Complete, depth)
	}
	if len(trace.reads) != depth || len(table.reads) != depth {
		t.Fatalf("the walk of depth %d paid %d parent lookups and %d event lookups, want one of each per fact: a step that read the chain pays a walk of the history for every fact it answers",
			depth, len(trace.reads), len(table.reads))
	}
	named := map[event.EventID]bool{}
	for _, key := range append(append([]event.EventID{}, trace.reads...), table.reads...) {
		named[key] = true
	}
	for key := range named {
		if _, onChain := items[key]; !onChain {
			t.Fatalf("the walk read %s, which stands outside the chain: the walk costs the chain it follows and never the history around it", key)
		}
	}
}
