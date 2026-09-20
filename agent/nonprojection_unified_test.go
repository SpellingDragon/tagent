package agent

import (
	"encoding/json"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §5.5 — the non-projection classification is ONE event-package predicate,
// jointly enforced by the normal-commit append, the online spill-replay
// backfill, and the cold-start rebuild. These tests pin the two paths whose
// guards were previously per-site private enums — the spill handler carried
// its own list and MISSED the current internal receipt record (fail-before:
// with the old enum, feeding a receipt event through the handler appended it to
// the projection, breaking the "projection = replayable fold of the fact
// chain" invariant the rebuild relies on).

func TestReplayProjectionHandler_ExcludesCurrentInternalRecords(t *testing.T) {
	ta := durableAgent(t, t.TempDir())
	handler := ReplayProjectionHandler(ta)

	internal := []memory.FullEvent{
		{EventKey: 101, EventType: tagentevent.TypeInboxReceipt},
		{EventKey: 102, EventType: tagentevent.TypeTaskSpawned},
		{EventKey: 103, EventType: tagentevent.TypeResidentSession},
		{EventKey: 104, EventType: tagentevent.TypeContextCompressSummary},
		{EventKey: 105, EventType: tagentevent.TypeAgentOutput,
			Metadata: map[string]string{tagentevent.MetaKeyTaskInlineRecord: "true"}},
	}
	for _, ev := range internal {
		handler(ev)
	}
	require.Equal(t, 0, ta.contextManager.projection.Len(),
		"§5.5: spill backfill excludes EVERY current internal record class, receipt included")

	handler(memory.FullEvent{EventKey: 106, EventType: tagentevent.TypeExternalInput,
		EventSummary: "real history"})
	require.Equal(t, 1, ta.contextManager.projection.Len(),
		"business events still flow back through the same handler")
}

func TestPersistBusEvent_NormalCommitExcludesInternalRecords(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	cm := ta.contextManager

	// A durable claim whose frozen fact carries an internal record type (the
	// guard must not trust the commit path to only ever see business events):
	// the fact is committed to the chain, the projection stays untouched.
	weird, err := json.Marshal(memory.FullEvent{
		EventKey: 9001, PartitionID: 1, EventType: tagentevent.TypeInboxReceipt,
		EventSummary: "s", Timestamp: time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	gated := &AgentEvent{
		ID: "e1", Type: tagentevent.TypeExternalInput,
		Message:   &model.Message{Role: model.RoleUser, Content: "x"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/1.json", RequestID: "r1", Slot: 0, PreparedFact: weird},
	}
	require.True(t, cm.persistBusEvent(gated), "the fact itself still commits")
	require.Equal(t, 0, cm.projection.Len(),
		"§5.5: the normal-commit append shares the event-package predicate — an internal record never occupies the projection")

	// A business-typed frozen fact still appends (guard is exclusion, not a blanket block).
	biz, err := json.Marshal(memory.FullEvent{
		EventKey: 9002, PartitionID: 1, EventType: tagentevent.TypeExternalInput,
		EventSummary: "input", Content: "hello", Timestamp: time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	ok := &AgentEvent{
		ID: "e2", Type: tagentevent.TypeExternalInput,
		Message:   &model.Message{Role: model.RoleUser, Content: "hello"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/2.json", RequestID: "r2", Slot: 0, PreparedFact: biz},
	}
	require.True(t, cm.persistBusEvent(ok))
	require.Equal(t, 1, cm.projection.Len())
}
