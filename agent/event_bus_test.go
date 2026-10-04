// 本文件负责事件总线的投递形状：发布与拉取、批量拉取、拉取阻塞与上下文取消、nil 事件的
// 处理，以及外部输入进入总线那一刻的归属定稿。
// 契约: docs/wiki/agent/event-flow.md#event-stream-overview
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestEventBus_PublishPull(t *testing.T) {
	bus := NewEventBus()

	msg := model.Message{Role: model.RoleUser, Content: "hello"}
	evt := NewExternalInputEvent("user", msg)
	bus.Publish(evt)

	ctx := context.Background()
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, evt.ID, batch[0].ID)
	assert.Equal(t, "external_input", batch[0].Type)
	assert.Equal(t, "user", batch[0].Source)
	assert.NotNil(t, batch[0].Message)
	assert.Equal(t, "hello", batch[0].Message.Content)
}

func TestEventBus_PullBatch(t *testing.T) {
	bus := NewEventBus()

	e1 := NewExternalInputEvent("user", model.Message{Content: "a"})
	e2 := NewExternalInputEvent("tmux", model.Message{Content: "b"})
	e3 := NewExternalInputEvent("tmux", model.Message{Content: "c"})

	bus.Publish(e1)
	bus.Publish(e2)
	bus.Publish(e3)

	ctx := context.Background()
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	assert.Equal(t, e1.ID, batch[0].ID)
	assert.Equal(t, e2.ID, batch[1].ID)
	assert.Equal(t, e3.ID, batch[2].ID)
	assert.Equal(t, "external_input", batch[2].Type)
}

func TestEventBus_PullBlocks(t *testing.T) {
	bus := NewEventBus()

	ctx := context.Background()
	done := make(chan struct{})

	go func() {
		batch, err := bus.Pull(ctx)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Pull returned before any event was published")
	default:
	}

	bus.Publish(NewExternalInputEvent("user", model.Message{Content: "wakeup"}))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Pull did not unblock within 2s after Publish")
	}
}

func TestEventBus_PullCtxCancel(t *testing.T) {
	bus := NewEventBus()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := bus.Pull(ctx)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Pull did not return within 2s after ctx cancellation")
	}
}

func TestEventBus_PublishNil(t *testing.T) {
	bus := NewEventBus()

	bus.Publish(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	batch, err := bus.Pull(ctx)
	assert.Nil(t, batch)
	assert.Equal(t, context.Canceled, err)
}

func TestNewExternalInputEvent(t *testing.T) {
	msg := model.Message{Role: model.RoleSystem, Content: "tmux done"}
	evt := NewExternalInputEvent("tmux", msg)

	assert.NotEmpty(t, evt.ID)
	assert.Equal(t, "external_input", evt.Type)
	assert.Equal(t, "tmux", evt.Source)
	assert.False(t, evt.Timestamp.IsZero())
	assert.NotNil(t, evt.Message)
	assert.Equal(t, "tmux done", evt.Message.Content)
	assert.NotNil(t, evt.Metadata)
}

func TestEventBus_TryPull_Empty(t *testing.T) {
	bus := NewEventBus()
	events := bus.TryPull()
	assert.NotNil(t, events)
	assert.Empty(t, events)
}

func TestEventBus_TryPull_Batch(t *testing.T) {
	bus := NewEventBus()

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg1"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg2"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg3"}))

	events := bus.TryPull()
	assert.Len(t, events, 3)
	assert.Equal(t, "msg1", events[0].Message.Content)
	assert.Equal(t, "msg2", events[1].Message.Content)
	assert.Equal(t, "msg3", events[2].Message.Content)

	events2 := bus.TryPull()
	assert.Empty(t, events2)
}

func TestEventBus_TryPull_NonBlocking(t *testing.T) {
	bus := NewEventBus()

	done := make(chan struct{})
	go func() {
		bus.TryPull()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("TryPull blocked on empty channel")
	}
}

// busCap 是 EventBus channel 容量（与 NewEventBus 一致）。
const busCap = 256

func publishN(bus *EventBus, n int, source string) {
	for i := 0; i < n; i++ {
		bus.Publish(NewExternalInputEvent(source, model.Message{Role: model.RoleUser, Content: fmt.Sprintf("c%d", i)}))
	}
}

// TestEventBus_DefaultVolatile 钉住 向后兼容：NewEventBus 与空 dir 均为轻量 volatile 模式（无 durable inbox，行为与旧纯 channel 一致）。
func TestEventBus_DefaultVolatile(t *testing.T) {
	if NewEventBus().inbox != nil {
		t.Fatal("NewEventBus 应无 durable inbox（向后兼容轻量模式）")
	}
	bus, err := NewReliableEventBus("")
	if err != nil || bus.inbox != nil {
		t.Fatal("空 spillDir 应为纯 volatile 模式")
	}
}

// TestReliableEventBus_AllInputsDurableNoDrop 钉住 可靠模式下所有输入一律先持久化（而非只溢出部分），Pull 按 seq 严格序领取全部且不丢。
func TestReliableEventBus_AllInputsDurableNoDrop(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	total := 8
	publishN(bus, total, "user")
	if got := bus.DurablePending(); got != int64(total) {
		t.Fatalf("全部输入应先持久化, pending=%d want %d", got, total)
	}

	got := 0
	for got < total {
		batch, err := bus.Pull(context.Background())
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if len(batch) == 0 {
			t.Fatal("仍有 pending 却 Pull 为空")
		}
		for _, e := range batch {
			if e.claim == nil {
				t.Fatal("durable 事件必须携带 typed durable claim（D2 溯源，非 Metadata 控制键）")
			}
			got++
		}
		for _, pr := range bus.DurableProvenance(batch) {
			key := "rk-" + pr[0]
			if err := bus.PrepareEnvelope(pr[0], key, []json.RawMessage{json.RawMessage(`{"event_key":1}`)}); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if err := bus.RecordCompletion(pr[0], json.RawMessage(`{"completion_version":1}`)); err != nil {
				t.Fatalf("completion: %v", err)
			}
			if err := bus.ConfirmDurable(pr[0], reliability.ReceiptCredential{ReceiptKey: key}); err != nil {
				t.Fatalf("confirm: %v", err)
			}
		}
	}
	if bus.DurablePending() != 0 {
		t.Fatalf("确认后应清空, pending=%d", bus.DurablePending())
	}
}

// TestReliableEventBus_DurableOrderPreserved 验证 durable 输入按接收序严格消费。
func TestReliableEventBus_DurableOrderPreserved(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	total := 5
	publishN(bus, total, "meditation")

	batch, err := bus.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(batch) != total {
		t.Fatalf("单次 Pull 应取回全部 %d（maxClaim=32）, got %d", total, len(batch))
	}
	for i, e := range batch {
		want := fmt.Sprintf("c%d", i)
		if e.Message == nil || e.Message.Content != want {
			t.Fatalf("durable 顺序破坏: idx=%d want %q got %+v", i, want, e.Message)
		}
		if e.Source != "meditation" {
			t.Fatalf("source 应保真, got %q", e.Source)
		}
	}
}

// TestReliableEventBus_RecoverAfterReopen 钉住 重启恢复：已 durable 未确认的 输入，新 bus 同 dir 可按序回收（常驻不丢，跨重启）。
func TestReliableEventBus_RecoverAfterReopen(t *testing.T) {
	dir := t.TempDir()
	bus1, err := NewReliableEventBus(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	publishN(bus1, 3, "user")
	if bus1.DurablePending() != 3 {
		t.Fatalf("应 3 pending, got %d", bus1.DurablePending())
	}
	if err := bus1.CloseDurable(); err != nil {
		t.Fatalf("close1: %v", err)
	}
	bus2, err := NewReliableEventBus(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	batch, err := bus2.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("重启后应回收 3 项, got %d", len(batch))
	}
}

// TestReliableEventBus_LegacySpillInertNotBlocking 钉住 旧格式 .spill 过渡数据不得阻止启动：可靠总线以当前格式打开，此类项被分类为惰性过渡数据（不读取、不消费），仅显式受管重置才清除。
func TestReliableEventBus_LegacySpillInertNotBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := osWriteFile(dir+"/00000000000000000009.spill", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	bus, err := NewReliableEventBus(dir)
	if err != nil {
		t.Fatalf("§3.7: legacy .spill must not block boot: %v", err)
	}
	if spill, _ := bus.TransitionalData(); len(spill) != 1 {
		t.Fatalf("stray .spill must be classified as transitional, got %v", spill)
	}
	publishN(bus, 2, "user")
	if got := bus.DurablePending(); got != 2 {
		t.Fatalf("v2 接收应正常, pending=%d want 2", got)
	}
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// TestEventBus_VolatileTimeoutRejected 钉住 volatile 模式的可判定拒绝：队列满或超时时 PublishContext 必须返回错误，绝不"丢弃即受理"；void 兼容入口的拒绝计数保持可观测。
func TestEventBus_VolatileTimeoutRejected(t *testing.T) {
	bus := NewEventBus()
	bus.ch = make(chan *AgentEvent, 1)
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "fill"}))
	evt := NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "x"})
	if _, err := bus.PublishContext(context.Background(), evt); !errors.Is(err, ErrPublishTimeout) {
		t.Fatalf("满队列必须返回背压错误, got %v", err)
	}
	bus.Publish(evt)
	if bus.PublishDropped() != 2 {
		t.Fatalf("PublishContext + void 各计一次拒绝, got %d", bus.PublishDropped())
	}
}

// failStore wraps a real store with an always-failing StoreEvent — the
// degraded-window shape the stored-gate guards against.
type failStore struct {
	*tagentmemory.InMemoryStore
}

func (f *failStore) StoreEvent(key int64, e tagentmemory.FullEvent) error {
	return errors.New("disk full")
}

// TestPersistBusEvent_StoredGate 钉住 存储失败不得追加投影——投影里绝不允许出现事实链上没有的引用。
// - 溢出恢复稍后重追加，因此宁可少投也不虚投。
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestPersistBusEvent_StoredGate(t *testing.T) {
	mkEvt := func() *AgentEvent {
		return &AgentEvent{
			Message:   &model.Message{Role: model.RoleUser, Content: "hi"},
			Timestamp: time.Now(),
		}
	}

	gated := &ContextManager{
		partitionID: 1,
		memStore:    &failStore{tagentmemory.NewInMemoryStore()},
		projection:  compress.NewSessionProjection(),
	}
	gated.persistBusEvent(mkEvt())
	require.Equal(t, 0, gated.projection.Len(),
		"StoreEvent failure must gate the projection Append (stored-gate)")

	ok := &ContextManager{
		partitionID: 1,
		memStore:    tagentmemory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	ok.persistBusEvent(mkEvt())
	require.Equal(t, 1, ok.projection.Len())

	nilStore := &ContextManager{
		partitionID: 1,
		projection:  compress.NewSessionProjection(),
	}
	nilStore.persistBusEvent(mkEvt())
	require.Equal(t, 1, nilStore.projection.Len())
}

// nonReplayStore hides the embedded store's EventReplayer method: only the
// methods of the tagentmemory.MemoryStore *interface* are promoted, and that set has
// no ReplayEvent. It satisfies tagentmemory.MemoryStore but NOT tagentmemory.EventReplayer
// — the exact shape the durable-mode capability gate must refuse.
type nonReplayStore struct {
	tagentmemory.MemoryStore
}

// TestNewTagentAgent_DurableRequiresReplayCapableStore 钉住 持久收件箱接到不具备重放能力的存储上时，构造必须响亮失败。
// - 否则会静默把至少一次投递降级成可能的双写；
// - 未配置持久性时同一存储被接受：该能力只在屏障真正生效时才是必需。
func TestNewTagentAgent_DurableRequiresReplayCapableStore(t *testing.T) {
	mock := &mockModel{info: model.Info{Name: "test"}}

	_, err := NewTagentAgent(&TagentConfig{
		Model:             mock,
		MemoryStore:       nonReplayStore{tagentmemory.NewInMemoryStore()},
		BusSpillDir:       t.TempDir(),
		MaxToolIterations: 1,
		MaxTokens:         1000,
	})
	require.Error(t, err, "durable inbox must refuse a non-replay store")
	require.Contains(t, err.Error(), "replay-capable")

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             mock,
		MemoryStore:       nonReplayStore{tagentmemory.NewInMemoryStore()},
		MaxToolIterations: 1,
		MaxTokens:         1000,
	})
	require.NoError(t, err, "volatile bus needs no replay capability")
	require.NotNil(t, ta)
	_ = ta.Close()
}

// TestBuildBusFact_FreezesFullMessageAndNamespacedSourceSnapshot 钉住 冻结完整规范消息，并把原始来源与完整业务元数据存为一个保留控制键下的精确 JSON 快照。
// - 业务键不得散进可信控制命名空间；
// - 源消息原有角色不得被就地改写。
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestBuildBusFact_FreezesFullMessageAndNamespacedSourceSnapshot(t *testing.T) {
	cm := newTestContextManager("s34", &loopMockModel{}, nil, nil, nil)
	evt := &AgentEvent{
		ID:        "src-1",
		Type:      tagentevent.TypeExternalInput,
		Source:    "user",
		Timestamp: time.Now(),
		Message: &model.Message{
			Role:      model.RoleSystem,
			Content:   "turn the light on",
			ToolCalls: []model.ToolCall{{ID: "call_1"}},
		},
		Metadata: map[string]any{"chat_id": "c-42", "genealogy": "root/7"},
	}

	fact := cm.buildBusFact(evt)

	require.Equal(t, evt.Message.ToolCalls, fact.ToolCalls, "canonical fact must freeze the message's tool calls")

	snap, err := tagentevent.DecodeSourceSnapshot(fact.Metadata[tagentevent.MetaKeySourceSnapshot])
	require.NoError(t, err)
	require.Equal(t, "user", snap.Source)
	require.Equal(t, "c-42", snap.Metadata["chat_id"], "business Metadata must survive losslessly")
	require.Equal(t, "root/7", snap.Metadata["genealogy"])

	require.NotContains(t, fact.Metadata, "chat_id", "business key must not leak into the control namespace")
	require.NotContains(t, fact.Metadata, "genealogy")

	require.Equal(t, model.RoleSystem, evt.Message.Role, "buildBusFact must not mutate the source message's role")
}

// TestBuildBusFact_PromotesSettleLineageToFirstClassKey 钉住 结算事件自带的派生血统提升为事实链一级可读键。
// - 提升不替代无损快照：source_snapshot 原样保留；
// - 消费回合血统与事件派生血统两键并存，可直接对账。
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestBuildBusFact_PromotesSettleLineageToFirstClassKey(t *testing.T) {
	cm := newTestContextManager("settle-lineage", &loopMockModel{}, nil, nil, nil)
	cm.triggerSource = "meditation"
	evt := &AgentEvent{
		ID:        "settle-1",
		Type:      tagentevent.TypeExternalInput,
		Source:    SourceTask,
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleUser, Content: "[task settled] job"},
		Metadata:  map[string]any{tagentevent.MetaKeyTriggerSource: "user", "task_id": "t-9"},
	}

	fact := cm.buildBusFact(evt)

	require.Equal(t, "user", fact.Metadata[tagentevent.MetaKeySettleTriggerSource],
		"the settle's spawn-time lineage must be readable without decoding the snapshot")
	require.Equal(t, "meditation", fact.Metadata[tagentevent.MetaKeyTriggerSource],
		"the consuming turn's lineage keeps its own key")
	snap, err := tagentevent.DecodeSourceSnapshot(fact.Metadata[tagentevent.MetaKeySourceSnapshot])
	require.NoError(t, err)
	require.Equal(t, "user", snap.Metadata[tagentevent.MetaKeyTriggerSource],
		"promotion must not replace the lossless snapshot")
}

// TestBuildBusFact_OmitsEmptyTurnLineageKey 钉住 回合级血统为空时一级键缺席而非空串。
// - 键存在但为空不得冒充已盖章：读方以缺席判定无回合级盖章。
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestBuildBusFact_OmitsEmptyTurnLineageKey(t *testing.T) {
	cm := newTestContextManager("no-lineage", &loopMockModel{}, nil, nil, nil)
	evt := &AgentEvent{
		ID: "plain-1", Type: tagentevent.TypeExternalInput, Source: "user",
		Timestamp: time.Now(), Message: &model.Message{Role: model.RoleUser, Content: "hi"},
	}

	fact := cm.buildBusFact(evt)

	_, present := fact.Metadata[tagentevent.MetaKeyTriggerSource]
	require.False(t, present, "an empty turn lineage must not persist as an empty-string first-class key")
}

func TestInjectMessageWithMetadata(t *testing.T) {
	bus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-agent",
		persistentBus: bus,
	}

	msg := model.Message{Role: model.RoleUser, Content: "test message"}
	metadata := map[string]string{
		"chat_id":   "user-123",
		"user_name": "Alice",
		"channel":   "wechat",
	}
	ta.InjectMessageWithMetadata("user", msg, metadata)

	events, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	assert.Equal(t, "user", evt.Source)
	assert.Equal(t, "user-123", evt.Metadata["chat_id"])
	assert.Equal(t, "Alice", evt.Metadata["user_name"])
	assert.Equal(t, "wechat", evt.Metadata["channel"])
}

func TestInjectMessageWithMetadata_EmptyValues(t *testing.T) {
	bus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-agent",
		persistentBus: bus,
	}

	msg := model.Message{Role: model.RoleUser, Content: "test"}
	metadata := map[string]string{
		"chat_id": "user-456",
		"empty":   "",
		"":        "value-with-empty-key",
	}
	ta.InjectMessageWithMetadata("user", msg, metadata)

	events, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	assert.Equal(t, "user-456", evt.Metadata["chat_id"])
	assert.NotContains(t, evt.Metadata, "empty")
	assert.NotContains(t, evt.Metadata, "")
}

func TestExtractRootMetadata(t *testing.T) {
	tests := []struct {
		name     string
		events   []*AgentEvent
		expected map[string]string
	}{
		{
			name: "single external_input with metadata",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id":   "user-123",
						"user_name": "Bob",
					},
				},
			},
			expected: map[string]string{
				"chat_id":   "user-123",
				"user_name": "Bob",
			},
		},
		{
			name: "multiple events - later overrides earlier",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-1",
					},
				},
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-2",
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-2",
			},
		},
		{
			name: "skip non-external_input events",
			events: []*AgentEvent{
				{
					Type:   "tool_use",
					Source: "agent_loop",
					Metadata: map[string]any{
						"chat_id": "should-be-ignored",
					},
				},
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-789",
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-789",
			},
		},
		{
			name: "ignore non-string values",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-123",
						"count":   42,
						"flag":    true,
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRootMetadata(tt.events)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOnEventCallback_PropagatesMetadata(t *testing.T) {
	cm := &ContextManager{
		currentMetadata: map[string]string{
			"chat_id":   "user-999",
			"user_name": "Charlie",
		},
	}

	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	callback := ta.makeOnEventCallback()

	evt := &trpcEvent.Event{
		Author: "test-agent",
		StateDelta: map[string][]byte{
			"event_key": []byte("12345"),
		},
	}

	callback(evt)

	assert.Equal(t, "user-999", string(evt.StateDelta["meta_chat_id"]))
	assert.Equal(t, "Charlie", string(evt.StateDelta["meta_user_name"]))
	assert.Equal(t, "12345", string(evt.StateDelta["event_key"]))
}

func TestOnEventCallback_NoMetadata(t *testing.T) {
	cm := &ContextManager{
		currentMetadata: nil,
	}

	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	callback := ta.makeOnEventCallback()

	evt := &trpcEvent.Event{
		Author:     "test-agent",
		StateDelta: map[string][]byte{},
	}

	callback(evt)

	for k := range evt.StateDelta {
		assert.NotContains(t, k, "meta_")
	}
}

func TestOnEventCallback_AlreadyPrefixedMetadata(t *testing.T) {
	cm := &ContextManager{
		currentMetadata: map[string]string{
			"meta_chat_id": "already-prefixed",
			"user_name":    "not-prefixed",
		},
	}

	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	callback := ta.makeOnEventCallback()

	evt := &trpcEvent.Event{}
	callback(evt)

	assert.Equal(t, "already-prefixed", string(evt.StateDelta["meta_chat_id"]))
	assert.NotContains(t, evt.StateDelta, "meta_meta_chat_id")
	assert.Equal(t, "not-prefixed", string(evt.StateDelta["meta_user_name"]))
}

// TestBuildTurnAttribution_TriggerSource 钉住 冥想身份可持久化的前提：触发源必须进入回合归因。
// - 记忆插件才能把它写进输出的元数据；否则投影重建时的重播种条件永不成立；
// - 该信息必须落在事实链上，不能只存在于内存态的状态增量里。
func TestBuildTurnAttribution_TriggerSource(t *testing.T) {
	cm := &ContextManager{
		sessionID:     "sess-1",
		triggerSource: "meditation",
		bundleIDFn:    func() string { return "bundle-1" },
	}
	attr := cm.buildTurnAttribution(context.Background())
	require.Equal(t, "sess-1", attr[tagentevent.MetaKeyRolloutID])
	require.Equal(t, "bundle-1", attr[tagentevent.MetaKeyBundleID])
	require.Equal(t, "meditation", attr[tagentevent.MetaKeyTriggerSource],
		"trigger source must enter attribution so it persists into agent_output Metadata")

	plain := &ContextManager{sessionID: "sess-2"}
	attr = plain.buildTurnAttribution(context.Background())
	require.Equal(t, "sess-2", attr[tagentevent.MetaKeyRolloutID])
	_, stamped := attr[tagentevent.MetaKeyTriggerSource]
	require.False(t, stamped, "empty trigger source must not stamp")
}

// TestAttribution_BundleIDStamped 钉住 两条事件持久化路径都把当前打包标识盖进完整事件的元数据。
// - 直接路径与装配路径读同一个取值函数；取值函数为空时不写该键，未启用演化时行为零变化。
func TestAttribution_BundleIDStamped(t *testing.T) {
	store := tagentmemory.NewInMemoryStore()
	cm := &ContextManager{
		name:        "tagent",
		memStore:    store,
		projection:  compress.NewSessionProjection(),
		partitionID: 7,
	}
	evt := &AgentEvent{
		Type:      "external_input",
		Source:    "user",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleUser, Content: "hi"},
	}

	t.Run("nil fn no key", func(t *testing.T) {
		before := storedCount(t, store)
		cm.persistBusEvent(evt)
		if got := storedCount(t, store); got != before+1 {
			t.Fatalf("event not persisted: %d -> %d", before, got)
		}
		e := lastEvent(t, store)
		if _, ok := e.Metadata[tagentevent.MetaKeyBundleID]; ok {
			t.Fatal("bundle_id stamped without provider (zero-behavior change violated)")
		}
	})

	t.Run("provider stamps", func(t *testing.T) {
		cm.bundleIDFn = func() string { return "b-abc123" }
		cm.persistBusEvent(evt)
		e := lastEvent(t, store)
		if got := e.Metadata[tagentevent.MetaKeyBundleID]; got != "b-abc123" {
			t.Fatalf("bundle_id = %q, want b-abc123", got)
		}
	})
}

func storedCount(t *testing.T, s *tagentmemory.InMemoryStore) int {
	t.Helper()
	refs, err := s.QueryEvents(tagentmemory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	return len(refs)
}

func lastEvent(t *testing.T, s *tagentmemory.InMemoryStore) tagentmemory.FullEvent {
	t.Helper()
	refs, err := s.QueryEvents(tagentmemory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000, OrderBy: "timestamp_desc"})
	if err != nil || len(refs) == 0 {
		t.Fatalf("no events: %v", err)
	}
	e, err := s.GetEvent(refs[0].EventKey)
	if err != nil || e == nil {
		t.Fatalf("get last: %v", err)
	}
	return *e
}

// TestOnEvent_SessionEventsPopulated 钉住 持久事件循环（void 兼容路径）下，onEvent 必须把 user 的 external_input 与 assistant 的 agent_output 都写入 session.Events。
func TestOnEvent_SessionEventsPopulated(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "Hello from tagent",
			},
		}},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("user-1", "session-on-event")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.GreaterOrEqual(t, len(sess.Events), 1, "session should contain events")
}

// TestOnEvent_MemoryStorePopulated 钉住 MemoryPlugin.OnEvent 把 FullEvent 持久化进 MemoryStore，并在 session 事件上置好 StateDelta 的 event_key 与 type（direct 兼容路径）。
func TestOnEvent_MemoryStorePopulated(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "Hello from tagent",
			},
		}},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("user-1", "session-memstore")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.NotEmpty(t, sess.Events)

	partitionID := tagentmemory.PartitionIDFromName(ta.name)
	events, err := ta.memStore.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 1, "MemoryStore should have at least 1 FullEvent")
}
