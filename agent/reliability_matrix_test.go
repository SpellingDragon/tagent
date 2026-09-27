package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// This file closes the task 3.6 inbox/bus test matrix: lossless field round-
// trip, unencodable-input refusal, fixed-slot non-compaction across a bad slot,
// and the system-role-not-mutated-in-place invariant. The other matrix cells
// (prepare-barrier zero-write gate, A/B partial-commit + restart-mismatch
// replay dedup, full/format/capability rejection) are covered by
// inbox_receipt_test.go, persist_bus_event_test.go and event_bus_spill_test.go.

// snap builds a lossless source_event snapshot for a slot (only the agent layer
// knows the AgentEvent schema; the leaf holds these bytes opaquely).
func snap(id, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"id": id, "type": "external_input", "source": "user",
		"message": map[string]any{"role": "user", "content": content},
	})
	return b
}

// TestReliableBus_FieldRoundTrip_Lossless (F1, 3.6「字段往返」): a fully
// populated event published through the durable inbox comes back on claim with
// ID/Type/Source/Timestamp/full Message/business Metadata intact — nothing
// re-stamped or dropped — and the envelope identity travels in the typed claim,
// never as a Metadata control key.
func TestReliableBus_FieldRoundTrip_Lossless(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)

	ts := time.Date(2026, 3, 1, 12, 30, 45, 123456789, time.UTC)
	orig := &AgentEvent{
		ID:        "evt-fixed-id",
		Type:      "external_input",
		Source:    "tmux",
		Timestamp: ts,
		Message:   &model.Message{Role: model.RoleUser, Content: "hello"},
		Metadata:  map[string]any{"source_session": "s-1", "channel": "cli"},
	}
	rec, err := bus.PublishContext(context.Background(), orig)
	require.NoError(t, err)
	require.True(t, rec.Durable)

	batch := bus.TryPull()
	require.Len(t, batch, 1)
	got := batch[0]

	require.Equal(t, "evt-fixed-id", got.ID, "ID preserved")
	require.Equal(t, "external_input", got.Type, "Type preserved")
	require.Equal(t, "tmux", got.Source, "Source preserved")
	require.True(t, ts.Equal(got.Timestamp), "Timestamp preserved losslessly: %v vs %v", ts, got.Timestamp)
	require.NotNil(t, got.Message)
	require.Equal(t, model.RoleUser, got.Message.Role, "Message.Role preserved")
	require.Equal(t, "hello", got.Message.Content, "Message.Content preserved")
	require.Equal(t, "s-1", got.Metadata["source_session"], "business Metadata preserved")
	require.Equal(t, "cli", got.Metadata["channel"])

	// Identity is a typed claim, not a Metadata control key (3.3).
	require.NotNil(t, got.claim)
	require.Equal(t, "evt-fixed-id", got.claim.RequestID)
	_, leaked := got.Metadata["inbox_path"]
	require.False(t, leaked, "no inbox_path control key may leak into Metadata")
}

// TestReliableBus_UnencodableEventRefused (F1, 3.6「格式拒绝」): an event whose
// payload cannot be JSON-encoded is REFUSED at durable acceptance — the bus
// never silently strips the offending field and durably accepts a lossy
// snapshot, and the refusal leaves nothing on disk.
func TestReliableBus_UnencodableEventRefused(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)

	bad := &AgentEvent{
		ID:        "bad",
		Type:      "external_input",
		Source:    "user",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleUser, Content: "x"},
		// A channel value cannot be marshaled to JSON.
		Metadata: map[string]any{"ch": make(chan int)},
	}
	rec, err := bus.PublishContext(context.Background(), bad)
	require.Error(t, err, "unencodable event must be refused at durable acceptance")
	require.False(t, rec.Durable)
	require.Equal(t, int64(0), bus.DurablePending(), "a refused input leaves no durable item")
}

// TestReliableBus_FixedSlotsNotCompacted (F4, 3.6「序号不压紧」): claim carries
// every slot at its FIXED index — a claim pass never compacts or renumbers the
// multi-slot layout. (deep-review P2-2 sync: the undecodable-slot shape this
// test previously used NO LONGER lets siblings survive — a corrupt slot
// quarantines the whole envelope — so the index-stability contract is pinned
// on the healthy multi-slot form; the quarantining sibling behavior is owned
// by TestUndecodableOneSlotQuarantinesWholeEnvelope.)
func TestReliableBus_FixedSlotsNotCompacted(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)

	env := &reliability.Envelope{
		RequestID: "batch", Source: "user",
		Messages: []reliability.MessageSlot{
			{SourceEvent: snap("a", "c-a")},
			{SourceEvent: snap("b", "c-b")},
			{SourceEvent: snap("c", "c-c")},
		},
	}
	_, err = bus.inbox.Enqueue(env)
	require.NoError(t, err)

	batch := bus.TryPull()
	require.Len(t, batch, 3)
	require.Equal(t, 0, batch[0].claim.Slot)
	require.Equal(t, 1, batch[1].claim.Slot)
	require.Equal(t, 2, batch[2].claim.Slot, "slot indices are carried verbatim, never compacted (F4)")
	require.Equal(t, "c-a", batch[0].Message.Content)
	require.Equal(t, "c-c", batch[2].Message.Content)
}

// TestPersistBusEvent_SystemRoleNotMutatedInPlace (3.6「system role 不被原地修
// 改」): the RoleSystem→RoleUser conversion projects system-injected messages as
// external input, but must operate on a copy — the caller's Message is never
// mutated in place.
func TestPersistBusEvent_SystemRoleNotMutatedInPlace(t *testing.T) {
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	evt := &AgentEvent{
		ID:        "s",
		Type:      "external_input",
		Source:    "user",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleSystem, Content: "[action_tool_result]"},
		Metadata:  map[string]any{},
	}
	require.True(t, cm.persistBusEvent(evt), "a volatile claim-less event persists")

	// The stored fact is projected as external input (user) ...
	refs := cm.projection.GetAll()
	require.Len(t, refs, 1)
	require.Equal(t, "user", refs[0].Role, "system-injected message is projected as external input")
	// ... but the caller's event keeps its system role (copied, not mutated).
	require.Equal(t, model.RoleSystem, evt.Message.Role, "persistBusEvent must not mutate the caller's Message in place")
}
