package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §3.5: the regenerate-key recovery fallback is DELETED. A durable claim whose
// frozen prepared_fact is corrupt (undecodable) or incomplete (current-format
// damage) MUST gate the store and surface — never mint a fresh Snowflake key,
// which would silently double-write the input under a new identity. A complete
// prepared_fact is still reused verbatim.

func newPreparedGateCM() *ContextManager {
	return &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
}

func evtWithPreparedClaim(fact json.RawMessage) *AgentEvent {
	return &AgentEvent{
		ID: "x", Type: "external_input", Source: "user", Timestamp: time.Now(),
		Message:  &model.Message{Role: model.RoleUser, Content: "hello"},
		Metadata: map[string]any{},
		claim:    &durableClaim{Path: "/tmp/e.json", RequestID: "r", Slot: 0, PreparedFact: fact},
	}
}

func TestPersistBusEvent_GatesUndecodablePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	require.False(t, cm.persistBusEvent(evtWithPreparedClaim(json.RawMessage("{ not valid json"))),
		"a corrupt durable prepared_fact must gate the store, not restamp a fresh key")
	require.Empty(t, cm.projection.GetAll(), "gating must not append a phantom projection ref")
}

func TestPersistBusEvent_GatesIncompletePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	// Decodes fine but carries no fixed identity/summary/attribution → incomplete.
	incomplete := json.RawMessage(`{"content":"hello"}`)
	require.False(t, cm.persistBusEvent(evtWithPreparedClaim(incomplete)),
		"an incomplete prepared_fact must gate the store, not restamp a fresh key")
	require.Empty(t, cm.projection.GetAll())
}

func TestPersistBusEvent_ReusesCompletePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	complete, err := json.Marshal(memory.FullEvent{
		EventKey:     987654321,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "hello",
		Content:      "hello",
		Metadata:     map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(complete)),
		"a complete prepared_fact is reused verbatim and stored")
	require.Len(t, cm.projection.GetAll(), 1)
}
