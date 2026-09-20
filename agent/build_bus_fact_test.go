package agent

import (
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §3.4: buildBusFact freezes the COMPLETE canonical message (tool fields carried
// on the existing FullEvent payload fields), and preserves the original source
// and FULL business Metadata as an EXACT JSON snapshot under one reserved control
// key — without spreading arbitrary business keys across the trusted control
// namespace, and without mutating the source message's original role.
func TestBuildBusFact_FreezesFullMessageAndNamespacedSourceSnapshot(t *testing.T) {
	cm := newTestContextManager("s34", &loopMockModel{}, nil, nil, nil)
	evt := &AgentEvent{
		ID:        "src-1",
		Type:      tagentevent.TypeExternalInput,
		Source:    "user",
		Timestamp: time.Now(),
		Message: &model.Message{
			Role:      model.RoleSystem, // system-injected: normalized on a copy, original MUST stay
			Content:   "turn the light on",
			ToolCalls: []model.ToolCall{{ID: "call_1"}},
		},
		Metadata: map[string]any{"chat_id": "c-42", "genealogy": "root/7"},
	}

	fact := cm.buildBusFact(evt)

	// Tool fields must be frozen onto the canonical fact (previously only Content
	// was kept, so a tool-bearing input fact lost its calls).
	require.Equal(t, evt.Message.ToolCalls, fact.ToolCalls, "canonical fact must freeze the message's tool calls")

	// Original source + full business Metadata preserved losslessly as an exact
	// snapshot under ONE reserved key.
	snap, err := tagentevent.DecodeSourceSnapshot(fact.Metadata[tagentevent.MetaKeySourceSnapshot])
	require.NoError(t, err)
	require.Equal(t, "user", snap.Source)
	require.Equal(t, "c-42", snap.Metadata["chat_id"], "business Metadata must survive losslessly")
	require.Equal(t, "root/7", snap.Metadata["genealogy"])

	// Arbitrary business keys must NOT be spread into the control namespace.
	require.NotContains(t, fact.Metadata, "chat_id", "business key must not leak into the control namespace")
	require.NotContains(t, fact.Metadata, "genealogy")

	// The source message's original role must be unchanged (normalization applies
	// only to the copy that feeds eventType/summary).
	require.Equal(t, model.RoleSystem, evt.Message.Role, "buildBusFact must not mutate the source message's role")
}
