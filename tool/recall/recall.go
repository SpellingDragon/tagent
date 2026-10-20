// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// Package recall is the model-facing recall surface of the memory axis: the
// four tools a recall agent calls — a retrieval query, an exact read of one
// event, the recent page and the causal chain — each answering what the memory
// side already holds rather than a retrieval of its own.
//
// The tools consume the read faces one runtime wires; they own no store and
// keep no copy of an event. A face a deployment leaves unwired refuses the tool
// that needs it by name, so a call never answers an empty set in place of a
// capability the deployment does not carry.
package recall

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/memory/search"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// QueryToolName, GetToolName, RecentToolName and TraceToolName are the
// vocabulary both the prompt and the declaration spell, so a recall agent
// calling a name it was told about reaches the tool that answers it. They are
// frozen: a rename is a prompt change and a declaration change in one breath.
const (
	// QueryToolName is the name of the semantic retrieval tool.
	QueryToolName = "recall_query"
	// GetToolName is the name of the exact read tool.
	GetToolName = "recall_get"
	// RecentToolName is the name of the recent page tool.
	RecentToolName = "recall_recent"
	// TraceToolName is the name of the causal chain tool.
	TraceToolName = "recall_trace"
)

// maxPage is the page bound the retrieval and the recent page clamp to, so one
// call never turns into an unbounded walk of the store.
const maxPage = 100

// defaultTraceSteps and maxTraceSteps bound one causal walk: the steps a call
// runs under when it names none, and the ceiling a call is clamped to.
const (
	defaultTraceSteps = 20
	maxTraceSteps     = 50
)

// defaultReadBytes and maxReadBytes bound the bytes one exact read answers
// with: a read of an original is not a corpus.
const (
	defaultReadBytes int64 = 8 << 10
	maxReadBytes     int64 = 32 << 10
)

// ErrRecallUnwired reports a tool whose read face the deployment did not wire:
// the call is refused by name rather than answered as an empty result, so the
// caller can tell "nothing matched" apart from "nothing could be read".
var ErrRecallUnwired = errors.New("recall: the deployment wires no read face for this tool")

// ErrRecallKey reports a call that names no event key: neither a ticket nor a
// key could be read out of the argument, so no event is addressed.
var ErrRecallKey = errors.New("recall: the call names no event key")

// ErrRecallMissing reports a causal walk whose first key the read face does not
// hold: the chain starts at an event nothing answers for, so there is no link
// to follow and no part to return.
var ErrRecallMissing = errors.New("recall: the read face holds no such event")

// reasonMissing, reasonExpired and reasonOutOfScope name why an answer serves
// no text for a key: the read face holds no such event, the original left its
// retention window, or the event lies outside the granted partitions.
const (
	reasonMissing    = "missing"
	reasonExpired    = "expired"
	reasonOutOfScope = "out_of_scope"
)

// Reads is the read face the four tools consume: the retrieval port, the
// embedding capability of this deployment, the observation table, the scoped
// original store and the causal relation ledger.
//
// A field left nil is a deployment that wires nothing there, and the tool that
// needs it refuses by name with ErrRecallUnwired.
type Reads struct {
	// Searcher answers one retrieval query inside the granted partitions.
	Searcher search.Searcher
	// Embedder is the embedding capability of this deployment: nil runs no
	// semantic retrieval at all, and the retrieval tool refuses by name.
	Embedder memory.Embedder
	// Table answers the event references a query matches and the reference of
	// one landed event.
	Table Table
	// Originals serves the text behind one landed event inside its read scope.
	Originals ledger.ScopedOriginalStore
	// Trace answers the parent link of one event.
	Trace *replay.MemoryRelations
}

// base is what the four tools share: the read face and the partitions the
// caller granted them. The granted set is fixed at construction, so no argument
// of any call can widen what these tools may read.
type base struct {
	reads  Reads
	names  []string
	ids    []int
	scoped map[int]bool
}

// newBase fixes the granted partition set of one tool family: the names the
// answer reports and the identifiers the references are checked against.
func newBase(reads Reads, partitions []string) base {
	names := append([]string(nil), partitions...)
	ids := make([]int, 0, len(names))
	scoped := make(map[int]bool, len(names))
	for _, name := range names {
		id := memory.PartitionIDFromName(name)
		if scoped[id] {
			continue
		}
		scoped[id] = true
		ids = append(ids, id)
	}
	return base{reads: reads, names: names, ids: ids, scoped: scoped}
}

// admits reports whether one reference partition lies inside the granted set.
// An empty set admits nothing.
func (b base) admits(partition int) bool { return b.scoped[partition] }

// New returns the four recall tools over one read face, in a fixed order:
// recall_query, recall_get, recall_recent, recall_trace. The partitions are the
// authorization this caller grants; an empty set grants nothing, and the tools
// answer that no event is inside their scope rather than reading the store.
func New(reads Reads, partitions []string) []trpctool.CallableTool {
	b := newBase(reads, partitions)
	return []trpctool.CallableTool{
		&queryTool{base: b},
		&getTool{base: b},
		&recentTool{base: b},
		&traceTool{base: b},
	}
}

// eventItem is one event as every tool of this package answers it: the ticket
// the caller can hand back, the small rendering of the event, and the partition
// the reference is filed under.
type eventItem struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID `json:"key"`
	// Type is the frozen event type of the event.
	Type string `json:"type"`
	// Summary is the stored summary of the event.
	Summary string `json:"summary"`
	// Time is the event time in Unix milliseconds.
	Time int64 `json:"time"`
	// Partition is the partition the event is filed under.
	Partition int `json:"partition"`
}

// itemOf renders one event reference as the item an answer carries.
func itemOf(ref memory.EventReference) eventItem {
	return eventItem{
		Key:       ref.Key,
		Type:      ref.Type,
		Summary:   ref.Summary,
		Time:      ref.Timestamp,
		Partition: ref.PartitionID,
	}
}

// unwired refuses one tool of this package whose read face the deployment left
// unwired, naming both the tool and the missing capability.
func unwired(tool string) error {
	return fmt.Errorf("%s: %w", tool, ErrRecallUnwired)
}

// encode renders one answer as the JSON text a tool call returns, so the caller
// reads the same field names whatever tool answered it.
func encode(tool string, answer any) (any, error) {
	body, err := json.Marshal(answer)
	if err != nil {
		return nil, fmt.Errorf("%s: encode the answer: %w", tool, err)
	}
	return string(body), nil
}

// clampPage resolves the page bound one call runs under: the default when it
// names none, and the ceiling when it names more than one page may carry.
func clampPage(limit int) int {
	switch {
	case limit <= 0:
		return search.DefaultLimit
	case limit > maxPage:
		return maxPage
	default:
		return limit
	}
}
