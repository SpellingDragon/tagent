// 本文件负责可靠总线的落盘矩阵：字段往返无损、不可编码事件必须拒绝而非降级写入、固定槽位
// 不得被压实、系统角色事件不得被就地改写。
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
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

// snap builds a lossless source_event snapshot for a slot (only the agent layer
// knows the AgentEvent schema; the leaf holds these bytes opaquely).
func snap(id, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"id": id, "type": "external_input", "source": "user",
		"message": map[string]any{"role": "user", "content": content},
	})
	return b
}

// TestReliableBus_FieldRoundTrip_Lossless 钉住 字段填满的事件经持久收件箱领取回来时必须原样带回标识、类型、来源、时间、完整消息与业务元数据。
// - 不得重打时间戳也不得丢项；信封身份随类型化领取传递，而非作为元数据里的控制键。
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

	require.NotNil(t, got.claim)
	require.Equal(t, "evt-fixed-id", got.claim.RequestID)
	_, leaked := got.Metadata["inbox_path"]
	require.False(t, leaked, "no inbox_path control key may leak into Metadata")
}

// TestReliableBus_UnencodableEventRefused 钉住 载荷无法编码的事件在持久化受理处就被拒绝。
// - 不得剥掉出问题的字段后收下有损快照；拒绝也不得在盘上留下任何东西。
func TestReliableBus_UnencodableEventRefused(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)

	bad := &AgentEvent{
		ID:        "bad",
		Type:      "external_input",
		Source:    "user",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleUser, Content: "x"},
		Metadata:  map[string]any{"ch": make(chan int)},
	}
	rec, err := bus.PublishContext(context.Background(), bad)
	require.Error(t, err, "unencodable event must be refused at durable acceptance")
	require.False(t, rec.Durable)
	require.Equal(t, int64(0), bus.DurablePending(), "a refused input leaves no durable item")
}

// TestReliableBus_FixedSlotsNotCompacted 钉住 claim carries every slot at its FIXED index — a claim pass never compacts or renumbers the multi-slot layout.。
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

// TestPersistBusEvent_SystemRoleNotMutatedInPlace 钉住 把系统角色投成外部输入时必须在副本上转换，绝不就地改写调用方的消息。
// - 就地改写会污染调用方后续复用同一条消息的所有路径。
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

	refs := cm.projection.GetAll()
	require.Len(t, refs, 1)
	require.Equal(t, "user", refs[0].Role, "system-injected message is projected as external input")
	require.Equal(t, model.RoleSystem, evt.Message.Role, "persistBusEvent must not mutate the caller's Message in place")
}
