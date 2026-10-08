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

// commitRecord 是一次因果提交：child 键与它当时认定的 parent 键（parent 为 0 表示无父）。
type commitRecord struct {
	child  int64
	parent int64
}

// spyCommitStore 记录真实提交顺序与 SetParent 尝试（失败尝试同样留痕），并按注入的
// 写失败/关系失败/阻塞门行事。记录走带缓冲 channel，替身自身不引入额外锁，
// 因此可安全用于并发与 -race。failOn/calls 只服务单线程子测（第 N 次 StoreEvent 失败）。
type spyCommitStore struct {
	*memory.InMemoryStore
	edges     chan commitRecord
	committed chan int64
	failOn    int
	calls     int
	relErr    error
	blockOn   string
	entered   chan struct{}
	release   chan struct{}
}

func newSpyStore() *spyCommitStore {
	return &spyCommitStore{
		InMemoryStore: memory.NewInMemoryStore(),
		edges:         make(chan commitRecord, 512),
		committed:     make(chan int64, 512),
		entered:       make(chan struct{}, 8),
		release:       make(chan struct{}),
	}
}

// StoreEvent 按注入门决定失败/阻塞，成功后才把键交给 committed 记录。
func (s *spyCommitStore) StoreEvent(key int64, evt memory.FullEvent) error {
	if s.failOn > 0 {
		s.calls++
		if s.calls == s.failOn {
			return errors.New("segment write rejected")
		}
	}
	if s.blockOn != "" && evt.Content == s.blockOn {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		<-s.release
	}
	if err := s.InMemoryStore.StoreEvent(key, evt); err != nil {
		return err
	}
	s.committed <- key
	return nil
}

// RelationStore 返回记录 SetParent 尝试的替身；其余关系操作透传内层存储。
func (s *spyCommitStore) RelationStore() memory.RelationStore {
	return edgeSpyRelation{RelationStore: s.InMemoryStore.RelationStore(), owner: s}
}

type edgeSpyRelation struct {
	memory.RelationStore
	owner *spyCommitStore
}

func (r edgeSpyRelation) SetParent(childKey, parentKey int64) error {
	if r.owner.relErr != nil {
		r.owner.edges <- commitRecord{child: childKey, parent: parentKey}
		return r.owner.relErr
	}
	if err := r.RelationStore.SetParent(childKey, parentKey); err != nil {
		return err
	}
	r.owner.edges <- commitRecord{child: childKey, parent: parentKey}
	return nil
}

// drainCommitted 取出全部已排队提交键（OnEvent 返回时记录必已入 channel）。
func (s *spyCommitStore) drainCommitted() []int64 {
	out := make([]int64, 0, len(s.committed))
	for {
		select {
		case k := <-s.committed:
			out = append(out, k)
		default:
			return out
		}
	}
}

// drainEdges 取出全部 SetParent 尝试记录。
func (s *spyCommitStore) drainEdges() []commitRecord {
	out := make([]commitRecord, 0, len(s.edges))
	for {
		select {
		case e := <-s.edges:
			out = append(out, e)
		default:
			return out
		}
	}
}

// ticketKey 取回事件 StateDelta 中的持久票据并解析成键；插件未发布票据时返回 (0,false)。
func ticketKey(t *testing.T, evt *trpcEvent.Event) (int64, bool) {
	t.Helper()
	raw, ok := evt.StateDelta[tagentevent.MetaKeyEventKey]
	if !ok {
		return 0, false
	}
	key, err := tagentevent.ParseEventKey(string(raw))
	require.NoError(t, err, "已发布的票据必须是可解析的 hex 事件键")
	return key, true
}

// causalKeyOf 复刻插件的因果域标识：partition 由 agent 名推导，session 为空（替身不挂会话）。
func causalKeyOf(agentName string) string {
	return fmt.Sprintf("%d:%s", memory.PartitionIDFromName(agentName), "")
}

// TestMemoryPlugin_CommitVisibility 钉住「提交成功才发布可召回票据」。
//   - 写失败或未接存储：下游拿不到取不回的 event_key/partition_id，失败键不进因果游标
//   - 内容已提交而关系失败：事实、票据与游标照常有效，只有因果完整性退让
//
// 契约: docs/wiki/plugin/plugin-architecture.md#memory-plugin
func TestMemoryPlugin_CommitVisibility(t *testing.T) {
	assistant := func(content string) *trpcEvent.Event {
		return newResponseEvent(model.RoleAssistant, content, nil)
	}

	t.Run("store failure publishes no ticket and no cursor advance", func(t *testing.T) {
		spy := newSpyStore()
		spy.failOn = 2
		sink := &recordingSink{}
		ctx := WithProjectionSink(context.Background(), sink)
		p := NewMemoryPlugin(spy)
		inv := &agent.Invocation{AgentName: "resident"}

		res1, err := p.OnEvent(ctx, inv, assistant("first"))
		require.NoError(t, err)
		key1, ok := ticketKey(t, res1)
		require.True(t, ok, "成功提交必须发布票据")

		res2, err := p.OnEvent(ctx, inv, assistant("second"))
		require.NoError(t, err, "被吞掉的存储错误不得改写成插件错误上抛")
		assert.NotContains(t, res2.StateDelta, tagentevent.MetaKeyEventKey,
			"写失败不得向下游提供取不回的持久票据")
		assert.NotContains(t, res2.StateDelta, tagentevent.MetaKeyPartitionID,
			"写失败不得伪造分区归属")
		assert.NotEmpty(t, res2.StateDelta[tagentevent.MetaKeyEventType],
			"分类不主张持久性，必须继续返回")
		assert.Equal(t, "second", res2.Response.Choices[0].Message.Content, "正文必须原样透传")
		assert.Equal(t, key1, parentOf(p, causalKeyOf("resident")), "写失败不得推进因果游标")

		res3, err := p.OnEvent(ctx, inv, assistant("third"))
		require.NoError(t, err)
		key3, ok := ticketKey(t, res3)
		require.True(t, ok, "写失败之后的成功提交仍要发布票据")

		parent, gerr := spy.GetParent(key3)
		require.NoError(t, gerr)
		assert.Equal(t, key1, parent, "失败键绝不能成为下一条的父键")
		assert.Equal(t, key3, parentOf(p, causalKeyOf("resident")), "游标只推进到最后已存键")

		refs := make([]int64, 0, len(sink.refs))
		for _, r := range sink.refs {
			refs = append(refs, r.EventKey)
		}
		assert.Equal(t, []int64{key1, key3}, refs, "失败键不得进入投影")

		fe, err := spy.GetEvent(key3)
		require.NoError(t, err, "成功票据必须可 GetEvent 取回")
		assert.Equal(t, "third", fe.Content)
	})

	t.Run("nil store publishes no pseudo-persistent ticket", func(t *testing.T) {
		sink := &recordingSink{}
		ctx := WithProjectionSink(context.Background(), sink)
		p := NewMemoryPlugin(nil)

		res, err := p.OnEvent(ctx, &agent.Invocation{AgentName: "resident"}, assistant("hello"))
		require.NoError(t, err)
		assert.NotContains(t, res.StateDelta, tagentevent.MetaKeyEventKey,
			"未接存储时发布的票据是伪持久票据，下游按它召回必然落空")
		assert.NotContains(t, res.StateDelta, tagentevent.MetaKeyPartitionID)
		assert.NotEmpty(t, res.StateDelta[tagentevent.MetaKeyEventType], "分类仍可返回")
		assert.Equal(t, "hello", res.Response.Choices[0].Message.Content, "正文仍可返回")
		assert.Empty(t, sink.refs, "未接存储不得投影")
		assert.Empty(t, p.lastEventKeys, "未接存储不得推进因果游标")
	})

	t.Run("relation failure keeps fact ticket and cursor", func(t *testing.T) {
		spy := newSpyStore()
		spy.relErr = errors.New("relation journal busy")
		sink := &recordingSink{}
		ctx := WithProjectionSink(context.Background(), sink)
		cred := &EchoCredential{AttemptToken: "resident#attempt-1", Agent: "resident", Session: "sess-1", MergedMessage: mergedInput}
		ctx = WithEchoCredential(ctx, cred)
		p := NewMemoryPlugin(spy)
		inv := &agent.Invocation{AgentName: "resident"}

		res1, err := p.OnEvent(ctx, inv, assistant("first"))
		require.NoError(t, err)
		key1, ok := ticketKey(t, res1)
		require.True(t, ok)

		res2, err := p.OnEvent(ctx, inv, assistant("second"))
		require.NoError(t, err)
		key2, ok := ticketKey(t, res2)
		require.True(t, ok, "内容已提交而关系失败时，事实与票据必须保持有效")

		res3, err := p.OnEvent(ctx, inv, assistant("third"))
		require.NoError(t, err)
		key3, ok := ticketKey(t, res3)
		require.True(t, ok)

		edges := spy.drainEdges()
		require.Len(t, edges, 2, "关系失败的尝试必须留痕，供回溯报 partial")
		assert.Equal(t, commitRecord{child: key2, parent: key1}, edges[0])
		assert.Equal(t, commitRecord{child: key3, parent: key2}, edges[1],
			"下一条仍接最后已存键，不得因关系失败而回滚内容")

		assert.Len(t, sink.refs, 3, "内容已提交即应投影")
		assert.Equal(t, key3, parentOf(p, causalKeyOf("resident")), "游标推进只取决于内容提交")
		assert.False(t, cred.rejected, "内容已提交不是凭据降级；关系失败另有 partial 表达")
	})
}

// jitterSpyStore 在存储内停留，把「读父 → 提交 → 推进游标」之间的交错窗口放大到毫秒级；
// 未线性化的实现必然在多条并发提交里造出同父/断链。
type jitterSpyStore struct{ *spyCommitStore }

func (s jitterSpyStore) StoreEvent(key int64, evt memory.FullEvent) error {
	time.Sleep(2 * time.Millisecond)
	return s.spyCommitStore.StoreEvent(key, evt)
}

// TestMemoryPlugin_CausalOrdering 钉住同一因果域的提交段是一条线性化顺序。
//   - 读父、分配键、StoreEvent、关系、游标推进之间不容同域并发插队
//   - 不同因果域必须并行推进；在途或排队的键锁记录不得被游标上界淘汰
//
// 契约: docs/wiki/plugin/plugin-architecture.md#causal-chain
func TestMemoryPlugin_CausalOrdering(t *testing.T) {
	t.Run("same causal key commits form one linear chain", func(t *testing.T) {
		spy := newSpyStore()
		p := NewMemoryPlugin(jitterSpyStore{spy})
		inv := &agent.Invocation{AgentName: "resident"}

		const n = 24
		done := make(chan error, n)
		for i := 0; i < n; i++ {
			go func(i int) {
				_, err := p.OnEvent(context.Background(), inv,
					newResponseEvent(model.RoleAssistant, fmt.Sprintf("concurrent-%d", i), nil))
				done <- err
			}(i)
		}
		for i := 0; i < n; i++ {
			require.NoError(t, <-done)
		}

		commits := spy.drainCommitted()
		require.Len(t, commits, n, "每个事件都必须真实提交一次，不多不少")

		edges := spy.drainEdges()
		byChild := make(map[int64]int64, len(edges))
		children := make(map[int64]int, len(edges))
		for _, e := range edges {
			if prev, dup := byChild[e.child]; dup {
				t.Fatalf("子键 %d 被挂了两个父键（%d 与 %d）：同域提交互相覆盖", e.child, prev, e.parent)
			}
			byChild[e.child] = e.parent
			children[e.parent]++
		}
		for parent, cnt := range children {
			assert.Equal(t, 1, cnt, "父键 %d 挂了 %d 个子键：同域并发读到了同一个游标（分叉）", parent, cnt)
		}

		for i, k := range commits {
			if i == 0 {
				_, hasParent := byChild[k]
				assert.False(t, hasParent, "段起点没有可信锚时必须具名断点，不得猜父")
				continue
			}
			parent, hasParent := byChild[k]
			require.True(t, hasParent, "提交 %d 没有挂父键，因果链在此分叉", i)
			assert.Equal(t, commits[i-1], parent,
				"第 %d 条提交的父必须是它前面一条已提交键（同因果键的提交段未串行）", i)
			_, err := spy.GetEvent(parent)
			require.NoError(t, err, "父键 %d 必须是已落库的事实，不能指向虚事件", parent)
		}

		assert.Equal(t, commits[n-1], parentOf(p, causalKeyOf("resident")), "游标停在最后一次提交上")
	})

	t.Run("different causal keys are not serialized", func(t *testing.T) {
		blockedName, otherName := "resident", "worker"
		require.NotEqual(t, memory.PartitionIDFromName(blockedName), memory.PartitionIDFromName(otherName),
			"两个 agent 必须落在不同 partition，才能代表两个因果域")

		spy := newSpyStore()
		spy.blockOn = "blocked"
		p := NewMemoryPlugin(spy)

		blockedDone := make(chan error, 1)
		go func() {
			_, err := p.OnEvent(context.Background(), &agent.Invocation{AgentName: blockedName},
				newResponseEvent(model.RoleAssistant, "blocked", nil))
			blockedDone <- err
		}()
		select {
		case <-spy.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("阻塞写入从未进入 StoreEvent，用例前提不成立")
		}

		otherDone := make(chan error, 1)
		go func() {
			_, err := p.OnEvent(context.Background(), &agent.Invocation{AgentName: otherName},
				newResponseEvent(model.RoleAssistant, "other", nil))
			otherDone <- err
		}()
		select {
		case err := <-otherDone:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("另一因果域被同一个存储写入阻塞：提交段不得共用全局锁")
		}

		close(spy.release)
		require.NoError(t, <-blockedDone)
		assert.Len(t, spy.drainCommitted(), 2, "两个域各自完成提交")
	})

	t.Run("active causal lock survives the cursor cap", func(t *testing.T) {
		p := NewMemoryPlugin(nil)
		active := causalKeyOf("resident")
		fill := func() {
			for i := 0; i < maxLastEventKeys+9; i++ {
				p.lastEventKeys[fmt.Sprintf("9:s%d", i)] = int64(1000 + i)
			}
		}

		p.mu.Lock()
		p.lastEventKeys[active] = 1
		fill()
		p.evictOldestLastEventKeysLocked()
		_, idleGone := p.lastEventKeys[active]
		p.mu.Unlock()
		require.False(t, idleGone, "对照：无活跃引用时它作为最旧键照常被淘汰")

		guard := p.acquireCausal(active)
		p.mu.Lock()
		p.lastEventKeys[active] = 1
		fill()
		p.evictOldestLastEventKeysLocked()
		_, held := p.lastEventKeys[active]
		p.mu.Unlock()
		require.True(t, held, "正在提交或排队等待的因果键不得被游标上界淘汰：那等于抽掉在途链的锚")

		p.releaseCausal(guard)
		p.mu.Lock()
		p.lastEventKeys["9:extra"] = 2
		p.evictOldestLastEventKeysLocked()
		_, released := p.lastEventKeys[active]
		assert.False(t, released, "引用归零后回到正常回收")
		assert.Len(t, p.lastEventKeys, maxLastEventKeys)
		p.mu.Unlock()
	})
}

// TestMemoryPlugin_RecoveryBoundary 钉住跳过闸的位置与因果锚缺口后的语义。
//   - 屏障、流式、退化空终态、本次输入回显都在任何键分配与写入之前返回
//   - 游标缺口之后新段起点一律不猜父：事实仍可召回，因果链具名断开并由回溯端报 partial
//
// 契约: docs/wiki/plugin/plugin-architecture.md#skip-set
func TestMemoryPlugin_RecoveryBoundary(t *testing.T) {
	inv := &agent.Invocation{AgentName: "resident"}

	skippedClean := func(t *testing.T, spy *spyCommitStore, p *MemoryPlugin, ctx context.Context, evt *trpcEvent.Event) {
		t.Helper()
		res, err := p.OnEvent(ctx, inv, evt)
		require.NoError(t, err)
		assert.Empty(t, spy.drainCommitted(), "被跳过的事件不得入库")
		assert.Nil(t, res.StateDelta, "被跳过的事件不得拿到任何 StateDelta 票据")
		assert.Empty(t, p.lastEventKeys, "被跳过的事件不得推进因果游标")
	}

	t.Run("all four skip gates stay ahead of allocation", func(t *testing.T) {
		spy := newSpyStore()
		p := NewMemoryPlugin(spy)

		barrier := newResponseEvent(model.RoleAssistant, "sync barrier", nil)
		barrier.Response = nil
		skippedClean(t, spy, p, context.Background(), barrier)

		streaming := newResponseEvent(model.RoleAssistant, "partial chunk", nil)
		streaming.Response.IsPartial = true
		skippedClean(t, spy, p, context.Background(), streaming)

		emptyFinal := newResponseEvent(model.RoleAssistant, "", nil)
		skippedClean(t, spy, p, context.Background(), emptyFinal)

		cred := &EchoCredential{AttemptToken: "resident#attempt-1", Agent: "resident", Session: "sess-1", MergedMessage: mergedInput}
		skippedClean(t, spy, p, WithEchoCredential(context.Background(), cred), userEvent("user", mergedInput))
		assert.True(t, cred.boundSet, "回显仍由本插件绑定：跳过不等于降级")
	})

	t.Run("no trusted anchor after restart starts a named break", func(t *testing.T) {
		spy := newSpyStore()
		ctx := context.Background()

		before, err := NewMemoryPlugin(spy).OnEvent(ctx, inv, newResponseEvent(model.RoleAssistant, "before restart", nil))
		require.NoError(t, err)
		key1, ok := ticketKey(t, before)
		require.True(t, ok)
		require.Empty(t, spy.drainEdges(), "第一条本就无父")

		_, err = spy.GetEvent(key1)
		require.NoError(t, err, "重启前的键仍在库里——正因如此，不猜父才是有代价的选择")

		restarted := NewMemoryPlugin(spy)
		after, err := restarted.OnEvent(ctx, inv, newResponseEvent(model.RoleAssistant, "after restart", nil))
		require.NoError(t, err)
		key2, ok := ticketKey(t, after)
		require.True(t, ok, "接不上父链不改变它是一条已提交事实")

		_, err = spy.GetEvent(key2)
		require.NoError(t, err, "新段起点必须可按票据取回")
		assert.Empty(t, spy.drainEdges(), "无可信锚时不得建立任何父子边")

		parent, gerr := spy.GetParent(key2)
		require.NoError(t, gerr)
		assert.Zero(t, parent, "新段起点停在断点上，不得接回重启前的键")
	})

	t.Run("evicted cursor starts a named break", func(t *testing.T) {
		spy := newSpyStore()
		p := NewMemoryPlugin(spy)
		ctx := context.Background()

		res1, err := p.OnEvent(ctx, inv, newResponseEvent(model.RoleAssistant, "first", nil))
		require.NoError(t, err)
		key1, ok := ticketKey(t, res1)
		require.True(t, ok)

		res2, err := p.OnEvent(ctx, inv, newResponseEvent(model.RoleAssistant, "second", nil))
		require.NoError(t, err)
		key2, ok := ticketKey(t, res2)
		require.True(t, ok)
		edges := spy.drainEdges()
		require.Len(t, edges, 1, "有锚时第二条必须挂上第一条")
		assert.Equal(t, commitRecord{child: key2, parent: key1}, edges[0])

		p.mu.Lock()
		delete(p.lastEventKeys, causalKeyOf("resident"))
		p.mu.Unlock()

		res3, err := p.OnEvent(ctx, inv, newResponseEvent(model.RoleAssistant, "third", nil))
		require.NoError(t, err)
		key3, ok := ticketKey(t, res3)
		require.True(t, ok, "游标淘汰只降级为无父，不撤销已提交事实")

		assert.Empty(t, spy.drainEdges(), "被淘汰后不得猜父：新段起点是具名断点")
		parent, gerr := spy.GetParent(key3)
		require.NoError(t, gerr)
		assert.Zero(t, parent)
		assert.Equal(t, key3, parentOf(p, causalKeyOf("resident")), "新段起点成为新的游标，后续继续接链")
	})
}

// typeCtxKey 用来证明解析器拿到的是事件自己那条 ctx（scope 就挂在那里），不是新建的空白 ctx。
type typeCtxKey struct{}

func callIDAssistantEvt(id string) *trpcEvent.Event {
	return &trpcEvent.Event{
		InvocationID: "inv-callid",
		Author:       "tagent",
		Response: &model.Response{
			ID:      id,
			Done:    true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "answer"}}},
		},
	}
}

// storedEvents reads back what actually landed in the fact chain.
func storedEvents(t *testing.T, store *memory.InMemoryStore) []memory.FullEvent {
	t.Helper()
	return store.AllEvents()
}

func TestMemoryPlugin_CallID(t *testing.T) {
	t.Run("exact hit stamps the key on the committed fact", func(t *testing.T) {
		store := memory.NewInMemoryStore()
		var gotID string
		var gotCtx context.Context
		p := NewMemoryPlugin(store, WithCallIDResolver(func(ctx context.Context, responseID string) (string, bool) {
			gotCtx, gotID = ctx, responseID
			return "call-7", responseID == "resp-42"
		}))

		ctx := context.WithValue(context.Background(), typeCtxKey{}, "carried")
		evt, err := p.OnEvent(ctx, rootInv(), callIDAssistantEvt("resp-42"))
		require.NoError(t, err)
		require.NotNil(t, evt)

		require.Equal(t, "resp-42", gotID, "查询键就是响应自己的 ID，不是别的")
		require.NotNil(t, gotCtx)
		require.Equal(t, "carried", gotCtx.Value(typeCtxKey{}), "解析器必须看见事件自己的 ctx（scope 挂在上面）")

		all := storedEvents(t, store)
		require.Len(t, all, 1)
		require.Equal(t, "call-7", all[0].Metadata[tagentevent.MetaKeyCallID],
			"命中的关联键随事实落库，导出侧才有 rawMetadata 可读")
		require.NotContains(t, evt.StateDelta, tagentevent.MetaKeyCallID,
			"它不是运行票据：只进事实元数据，不进 StateDelta 票据面")
	})

	t.Run("a miss stamps nothing (no nearest-call guess)", func(t *testing.T) {
		store := memory.NewInMemoryStore()
		calls := 0
		p := NewMemoryPlugin(store, WithCallIDResolver(func(ctx context.Context, responseID string) (string, bool) {
			calls++
			return "", false
		}))

		_, err := p.OnEvent(context.Background(), rootInv(), callIDAssistantEvt("resp-unbound"))
		require.NoError(t, err)

		require.Equal(t, 1, calls, "解析器被问过，且只问过那一个精确键")
		all := storedEvents(t, store)
		require.Len(t, all, 1, "未命中照常提交事实（关联是可选增益，不是提交前置）")
		require.NotContains(t, all[0].Metadata, tagentevent.MetaKeyCallID,
			"S2: 未命中必须具名 unbound，绝不拿「最近调用」凑一个")
		require.NotEmpty(t, all[0].Metadata[tagentevent.MetaKeyAgentName], "既有归因不受影响")
	})

	t.Run("no response id never asks the resolver", func(t *testing.T) {
		store := memory.NewInMemoryStore()
		asked := 0
		p := NewMemoryPlugin(store, WithCallIDResolver(func(ctx context.Context, responseID string) (string, bool) {
			asked++
			return "should-not-be-used", true
		}))

		_, err := p.OnEvent(context.Background(), rootInv(), callIDAssistantEvt(""))
		require.NoError(t, err)
		require.Zero(t, asked, "没有身份就不查询：一次空 ID 的查询本身就是一种猜测")
		all := storedEvents(t, store)
		require.Len(t, all, 1)
		require.NotContains(t, all[0].Metadata, tagentevent.MetaKeyCallID)
	})

	t.Run("no resolver installed is byte-identical to the wiring before it", func(t *testing.T) {
		store := memory.NewInMemoryStore()
		p := NewMemoryPlugin(store)
		_, err := p.OnEvent(context.Background(), rootInv(), callIDAssistantEvt("resp-1"))
		require.NoError(t, err)
		all := storedEvents(t, store)
		require.Len(t, all, 1)
		require.NotContains(t, all[0].Metadata, tagentevent.MetaKeyCallID)
	})

	t.Run("a fact that did not commit publishes no attribution either", func(t *testing.T) {
		store := &storeErrStore{InMemoryStore: memory.NewInMemoryStore()}
		p := NewMemoryPlugin(store, WithCallIDResolver(func(context.Context, string) (string, bool) {
			return "call-x", true
		}))
		evt, err := p.OnEvent(context.Background(), rootInv(), callIDAssistantEvt("resp-9"))
		require.NoError(t, err)
		require.NotContains(t, evt.StateDelta, tagentevent.MetaKeyCallID,
			"提交闸：写失败不发布任何可按它取回的标识")
		require.NotEmpty(t, evt.StateDelta[tagentevent.MetaKeyEventType], "分类照旧发布")
	})

	t.Run("the four skip gates stay in front of the stamp", func(t *testing.T) {
		store := memory.NewInMemoryStore()
		asked := 0
		p := NewMemoryPlugin(store, WithCallIDResolver(func(context.Context, string) (string, bool) {
			asked++
			return "call-y", true
		}))

		_, err := p.OnEvent(context.Background(), rootInv(), &trpcEvent.Event{
			InvocationID: "inv-x", Author: "tagent",
			Response: &model.Response{ID: "r-partial", IsPartial: true,
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "chunk"}}}},
		})
		require.NoError(t, err)
		require.Zero(t, asked, "流式分片在任何写入/查询之前就被跳过")

		_, err = p.OnEvent(context.Background(), rootInv(), &trpcEvent.Event{InvocationID: "inv-y"})
		require.NoError(t, err)
		require.Zero(t, asked, "无 choices 的屏障事件同样先被跳过")

		require.Empty(t, store.GetStats().TotalEvents, "被跳过的事件一条都不落库")
	})
}

// TestMemoryPlugin_ConstructorStaysBackwardCompatible 钉住 变参构造：单参调用仍是合法形态。
func TestMemoryPlugin_ConstructorStaysBackwardCompatible(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)
	require.NotNil(t, p)
	require.Equal(t, "memory", p.Name())

	p2 := NewMemoryPlugin(store, WithCallIDResolver(nil))
	require.NotNil(t, p2)
	_, err := p2.OnEvent(context.Background(), rootInv(), callIDAssistantEvt("resp-bc"))
	require.NoError(t, err)
}
