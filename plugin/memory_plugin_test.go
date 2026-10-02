package plugin

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestMemoryPlugin_OnEvent_StoresFullEvent 钉住一次事件的完整落库：类型、摘要与两个存储标识。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#skip-set
func TestMemoryPlugin_OnEvent_StoresFullEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	evt := &trpcEvent.Event{
		InvocationID: "inv-1",
		Author:       "tagent",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "test message"}},
			},
		},
	}

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)
	require.NotNil(t, result)

	stats := store.GetStats()
	assert.Equal(t, 1, stats.TotalEvents, "one event should be stored")

	allEvents := store.AllEvents()
	require.Len(t, allEvents, 1)
	assert.Equal(t, tagentevent.TypeExternalInput, allEvents[0].EventType)
	assert.Equal(t, "test message", allEvents[0].EventSummary)
	assert.NotZero(t, allEvents[0].EventKey, "Snowflake EventKey should be non-zero")
	assert.NotZero(t, allEvents[0].PartitionID, "PartitionID should be non-zero")
}

func TestMemoryPlugin_OnEvent_WritesStateDelta(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	evt := &trpcEvent.Event{
		InvocationID: "inv-1",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleAssistant, Content: "response"}},
			},
		},
	}

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)

	require.NotNil(t, result.StateDelta)
	assert.NotEmpty(t, result.StateDelta["event_key"], "event_key should be written to StateDelta")
	assert.Equal(t, tagentevent.TypeAgentOutput, string(result.StateDelta["event_type"]))
}

func TestMemoryPlugin_OnEvent_ParentChain(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	now := time.Now()
	events := []*trpcEvent.Event{
		{
			InvocationID: "inv-1",
			Timestamp:    now,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "first"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Timestamp:    now.Add(10 * time.Millisecond),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleAssistant, Content: "second"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Timestamp:    now.Add(20 * time.Millisecond),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "third"}},
				},
			},
		},
	}

	for _, evt := range events {
		_, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
		require.NoError(t, err)
	}

	allEvents := store.AllEvents()
	require.Len(t, allEvents, 3)

	parent0, _ := store.GetParent(allEvents[0].EventKey)
	assert.Zero(t, parent0, "first event should have zero ParentKey")

	parent1, _ := store.GetParent(allEvents[1].EventKey)
	assert.Equal(t, allEvents[0].EventKey, parent1)

	parent2, _ := store.GetParent(allEvents[2].EventKey)
	assert.Equal(t, allEvents[1].EventKey, parent2)
}

func TestMemoryPlugin_OnEvent_NilEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "nil event should return nil")
}

func TestMemoryPlugin_OnEvent_SkipsEventsWithoutChoices(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	cases := []*trpcEvent.Event{
		{InvocationID: "inv-1", Response: nil},
		{InvocationID: "inv-1", Response: &model.Response{Done: true, Choices: nil}},
		{InvocationID: "inv-1", Response: &model.Response{Done: true, Choices: []model.Choice{}}},
	}

	for _, evt := range cases {
		result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Nil(t, result.StateDelta, "sync events must not get StateDelta")
	}

	stats := store.GetStats()
	assert.Zero(t, stats.TotalEvents, "sync events should not be persisted")
}

// minimalStore 包装 InMemoryStore 但刻意不实现 RelationStoreProvider，用于验证缺关系存储时的降级。
type minimalStore struct {
	memory.MemoryStore
}

func TestMemoryPlugin_OnEvent_NoRelationStoreProvider(t *testing.T) {
	inner := memory.NewInMemoryStore()
	store := &minimalStore{MemoryStore: inner}
	p := NewMemoryPlugin(store)

	evt := &trpcEvent.Event{
		InvocationID: "inv-1",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "test"}},
			},
		},
	}

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)
	require.NotNil(t, result)

	stats := inner.GetStats()
	assert.Equal(t, 1, stats.TotalEvents, "event should still be stored")
}

// TestMemoryPlugin_LastEventKeysBounded pins that the causal-chain map evicts oldest-by-event-key entries once over the cap.
// - Newest chains survive and oldest go first, because event keys are time-monotonic within a partition.
func TestMemoryPlugin_LastEventKeysBounded(t *testing.T) {
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}
	total := maxLastEventKeys + 100
	for i := 0; i < total; i++ {
		p.lastEventKeys[fmt.Sprintf("p:s%d", i)] = int64(1000 + i)
	}
	p.evictOldestLastEventKeysLocked()

	if len(p.lastEventKeys) != maxLastEventKeys {
		t.Fatalf("map len = %d, want %d (bounded)", len(p.lastEventKeys), maxLastEventKeys)
	}
	if _, ok := p.lastEventKeys["p:s0"]; ok {
		t.Fatal("oldest causal chain survived eviction — must go first")
	}
	newest := fmt.Sprintf("p:s%d", total-1)
	if _, ok := p.lastEventKeys[newest]; !ok {
		t.Fatalf("newest causal chain %q must survive", newest)
	}
}

// storeErrStore 让每次 StoreEvent 失败（读与回放由内嵌 store 提供），用于构造「框架只记日志并继续」的形态。
type storeErrStore struct{ *memory.InMemoryStore }

func (s *storeErrStore) StoreEvent(int64, memory.FullEvent) error {
	return errors.New("disk full")
}

const mergedInput = "msg-A\n\n---\n\nmsg-B"

// rootInv is a zero invocation whose GetParentInvocation() is nil → treated as the root.
func rootInv() *agent.Invocation { return &agent.Invocation{} }

// echoCtx returns a context carrying an attempt credential expecting mergedInput.
func echoCtx(merged string) context.Context {
	return WithEchoCredential(context.Background(), &EchoCredential{
		AttemptToken:  "resident#attempt-1",
		Agent:         "resident",
		Session:       "sess-1",
		MergedMessage: merged,
		CommittedKeys: []int64{111, 222},
	})
}

// userEvent builds a framework-shaped user input echo (root, author "user").
func userEvent(author string, content string) *trpcEvent.Event {
	return &trpcEvent.Event{
		Author: author,
		Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleUser, Content: content}}},
		},
	}
}

// TestMemoryPlugin_ExpectedRootEcho_Skipped 已提交批次那条精确合并回显必须由管线跳过（事件循环已逐消息落库），净新增事实为零。
func TestMemoryPlugin_ExpectedRootEcho_Skipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 0 {
		t.Fatalf("expected root echo must NOT be re-stored (would double-write), got %d facts", got)
	}
}

// TestMemoryPlugin_DifferentUserContent_NotSkipped 内容不等于已提交合并消息的 user 事件（如同回合后续提问）必须走正常路径。
func TestMemoryPlugin_DifferentUserContent_NotSkipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), userEvent("user", "a completely different user message")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("non-matching user message must still be stored, got %d", got)
	}
}

// TestMemoryPlugin_AssistantOutput_StillStored 同一 durable 回合里的 assistant 输出必须入库，不得被当作回显跳过。
func TestMemoryPlugin_AssistantOutput_StillStored(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	assistantEvt := &trpcEvent.Event{
		Author:   "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	if _, err := mp.OnEvent(echoCtx(mergedInput), rootInv(), assistantEvt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("assistant output must still be stored, got %d", got)
	}
}

// TestMemoryPlugin_NonRootInvocation_NotSkipped 即使内容等于合并输入，只要不是根调用也不得跳过：跳过的资格只属于被正面确认的根回显。
func TestMemoryPlugin_NonRootInvocation_NotSkipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	if _, err := mp.OnEvent(echoCtx(mergedInput), nil, userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("non-root invocation must not trigger the echo skip, got %d", got)
	}
}

// TestMemoryPlugin_NoCredential_Unchanged 无凭据（非 durable 回合）时行为不变：消息照常入库。
func TestMemoryPlugin_NoCredential_Unchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)
	if _, err := mp.OnEvent(context.Background(), rootInv(), userEvent("user", "plain")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("plain path must still store, got %d", got)
	}
}

// TestMemoryPlugin_PluginPathPreservesContentParts 钉住 插件存储路径同样携带非文本部分：仅图输入经 onEvent 入库后 ContentParts 不丢，且按其分区可查回。
func TestMemoryPlugin_PluginPathPreservesContentParts(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	inv := &agent.Invocation{AgentName: "resident"}
	evt := &trpcEvent.Event{
		Author: "user",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleUser,
			ContentParts: []model.ContentPart{{
				Type:  model.ContentTypeImage,
				Image: &model.Image{URL: "https://example.com/pic.png"},
			}},
		}}}},
	}
	if _, err := mp.OnEvent(context.Background(), inv, evt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	require.Equal(t, 1, store.GetStats().TotalEvents, "the volatile image input must be stored")
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName("resident")}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, refs, 1, "the volatile image input must be queryable on its partition")
	fe, gerr := store.GetEvent(refs[0].EventKey)
	require.NoError(t, gerr)
	require.Len(t, fe.ContentParts, 1, "plugin store path must persist ContentParts (not drop the image)")
	require.NotNil(t, fe.ContentParts[0].Image)
	require.Equal(t, "https://example.com/pic.png", fe.ContentParts[0].Image.URL)
}

// TestMemoryPlugin_SwallowedStoreErrorDowngradesCredential 钉住 框架对插件错误只记日志并继续，因此被吞掉的 StoreEvent 失败必须把该凭据 MarkRejected，否则回合看似成功而凭据仍被当作已验证。
func TestMemoryPlugin_SwallowedStoreErrorDowngradesCredential(t *testing.T) {
	store := &storeErrStore{InMemoryStore: memory.NewInMemoryStore()}
	mp := NewMemoryPlugin(store)
	ctx := echoCtx(mergedInput)
	cred, ok := EchoCredentialFrom(ctx)
	require.True(t, ok)

	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("OnEvent echo: %v", err)
	}
	require.True(t, cred.Verified(), "a bound root echo makes the credential verified")

	assistantEvt := &trpcEvent.Event{
		Author:   "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	if _, err := mp.OnEvent(ctx, rootInv(), assistantEvt); err != nil {
		t.Fatalf("OnEvent assistant must not surface the swallowed store error: %v", err)
	}
	require.False(t, cred.Verified(), "a swallowed store error during a credentialed turn must MarkRejected the credential")
}

// TestMemoryPlugin_PreciseEchoIsolation_Threading 钉住 精确回显隔离的整体串演：带凭据的 durable 回合穿过输入回显、assistant 输出、工具与后续输入时，只有那一条根回显被跳过，其余全部入库。
func TestMemoryPlugin_PreciseEchoIsolation_Threading(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)
	cred := &EchoCredential{AttemptToken: "resident#attempt-1", Agent: "resident", Session: "sess-1", MergedMessage: mergedInput, CommittedKeys: []int64{111, 222}}
	ctx := WithEchoCredential(context.Background(), cred)

	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", mergedInput)); err != nil {
		t.Fatalf("echo: %v", err)
	}
	if _, err := mp.OnEvent(ctx, rootInv(), &trpcEvent.Event{Author: "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}}}); err != nil {
		t.Fatalf("assistant: %v", err)
	}
	if _, err := mp.OnEvent(ctx, rootInv(), &trpcEvent.Event{Author: "resident",
		Response: &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleTool, Content: "tool output", ToolID: "tc1"}}}}}); err != nil {
		t.Fatalf("tool: %v", err)
	}
	if _, err := mp.OnEvent(ctx, rootInv(), userEvent("user", "an entirely different later question")); err != nil {
		t.Fatalf("later-user: %v", err)
	}
	if _, err := mp.OnEvent(ctx, nil, userEvent("user", mergedInput)); err != nil {
		t.Fatalf("same-nonroot: %v", err)
	}

	require.Equal(t, 4, store.GetStats().TotalEvents,
		"exactly the one root echo is skipped; assistant/tool/subsequent-user/same-non-root all store")
	require.True(t, cred.Verified(), "the root echo Bind makes the credential verified")
}

// update 复刻生产写入路径：更新该因果键的末事件 key，超上界即淘汰。因果查找的承重契约（返回值只能是该键
// 最后写入的 key 或 0，绝不为他键；淘汰只能降级为无父，不得错接前驱）见文档。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#causal-chain
func update(p *MemoryPlugin, causalKey string, eventKey int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastEventKeys[causalKey] = eventKey
	if len(p.lastEventKeys) > maxLastEventKeys {
		p.evictOldestLastEventKeysLocked()
	}
}

// parentOf 复刻生产读取路径：键不存在时返回 0。
func parentOf(p *MemoryPlugin, causalKey string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastEventKeys[causalKey]
}

// TestLastEventKeys_EvictionCausalSemantics 钉住 淘汰后的因果查找语义：映射不超上限、保留项读回自己最后写入的键、不得串到别的会话父键上。
func TestLastEventKeys_EvictionCausalSemantics(t *testing.T) {
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}

	const sessions = maxLastEventKeys + 500
	key := int64(1_000_000)
	lastWritten := make(map[string]int64, sessions)
	for i := 0; i < sessions; i++ {
		ck := fmt.Sprintf("1:s%d", i)
		update(p, ck, key)
		lastWritten[ck] = key
		key++
	}

	if len(p.lastEventKeys) > maxLastEventKeys {
		t.Fatalf("map exceeded cap: %d > %d", len(p.lastEventKeys), maxLastEventKeys)
	}

	for k, v := range p.lastEventKeys {
		if lastWritten[k] != v {
			t.Fatalf("retained %q=%d but last written under it was %d (cross-link?!) ", k, v, lastWritten[k])
		}
	}

	evictedCount := 0
	retainedCount := 0
	for i := 0; i < sessions; i++ {
		ck := fmt.Sprintf("1:s%d", i)
		got := parentOf(p, ck)
		want := lastWritten[ck]
		switch {
		case got == 0:
			evictedCount++
			if _, ok := p.lastEventKeys[ck]; ok {
				t.Fatalf("session %q present in map but reads parent 0", ck)
			}
		case got == want:
			retainedCount++
		default:
			t.Fatalf("session %q read parent %d, want %d (its last key) or 0 (evicted)", ck, got, want)
		}
	}
	if evictedCount != sessions-maxLastEventKeys {
		t.Fatalf("evicted %d sessions, want %d", evictedCount, sessions-maxLastEventKeys)
	}
	if retainedCount != maxLastEventKeys {
		t.Fatalf("retained %d sessions, want %d", retainedCount, maxLastEventKeys)
	}

	dead := "1:s0"
	if parentOf(p, dead) != 0 {
		t.Fatalf("evicted session %q must read parent 0 before reuse, got %d", dead, parentOf(p, dead))
	}
	update(p, dead, key)
	if got := parentOf(p, dead); got != key {
		t.Fatalf("resurrected %q reads %d, want new key %d", dead, got, key)
	}
}

// TestLastEventKeys_BoundedFuzz drives randomized interleaved updates over a small session space with strictly increasing keys.
// - The invariant is asserted every step: the map stays capped, and each present key maps to the last value written under that key.
func TestLastEventKeys_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xcafe5))
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}
	const space = maxLastEventKeys * 3 / 2

	writtenUnder := make(map[string]map[int64]bool)
	var key int64
	for i := 0; i < 6000; i++ {
		ck := fmt.Sprintf("1:s%d", rng.Intn(space))
		key++
		if writtenUnder[ck] == nil {
			writtenUnder[ck] = map[int64]bool{}
		}
		writtenUnder[ck][key] = true
		update(p, ck, key)

		if len(p.lastEventKeys) > maxLastEventKeys {
			t.Fatalf("iter %d: cap violated: %d", i, len(p.lastEventKeys))
		}
		for k, v := range p.lastEventKeys {
			if !writtenUnder[k][v] {
				t.Fatalf("iter %d: key %q holds foreign value %d never written under it", i, k, v)
			}
		}
	}
}
