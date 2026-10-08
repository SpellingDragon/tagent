package tagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	trpcevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpcsession "trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/tool/recall"
)

// gateStore 是一台在真实提交点上注入故障的存储替身：写失败、写阻塞与父边失败都落在
// 存储与关系存储的入口，每次尝试的键都留痕，断言因此只能读已提交的事实。
//   - failWhen 命中：StoreEvent 返回错误且不落库，键记入 failed；
//   - blockWhen 命中：StoreEvent 停在 release 上，parked 先行关闭供测试确认已入闸；
//   - failEdges 打开时：每一条 SetParent 都失败，内容仍照常提交；
//   - 父边读取交给内层真实存储：没有关系证据就是 0，不做任何补偿。
//
// 契约: docs/wiki/memory/memory-architecture.md#causal-chain
type gateStore struct {
	*memory.InMemoryStore

	mu sync.Mutex

	failWhen  func(memory.FullEvent) bool
	blockWhen func(memory.FullEvent) bool
	parked    chan struct{}
	release   chan struct{}
	failEdges bool

	failed      []int64
	stored      []int64
	parentTried []gateEdge
	parkOnce    sync.Once
}

// gateEdge 是一次父边写入尝试的两个端点。
type gateEdge struct {
	child  int64
	parent int64
}

// errGateFault 是注入到提交点上的存储故障。
var errGateFault = errors.New("committed-facts gate: injected storage fault")

// newGateStore 返回一台包裹真实内存存储的故障替身。
func newGateStore() *gateStore {
	return &gateStore{InMemoryStore: memory.NewInMemoryStore()}
}

// StoreEvent 在写入口执行注入策略：阻塞、失败或照常落库，并记录尝试过的键。
func (s *gateStore) StoreEvent(key int64, evt memory.FullEvent) error {
	if s.blockWhen != nil && s.blockWhen(evt) {
		s.parkOnce.Do(func() { close(s.parked) })
		<-s.release
	}
	if s.failWhen != nil && s.failWhen(evt) {
		s.mu.Lock()
		s.failed = append(s.failed, key)
		s.mu.Unlock()
		return errGateFault
	}
	if err := s.InMemoryStore.StoreEvent(key, evt); err != nil {
		return err
	}
	s.mu.Lock()
	s.stored = append(s.stored, key)
	s.mu.Unlock()
	return nil
}

// RelationStore 交出带父边故障注入的关系面，父边尝试都留痕。
func (s *gateStore) RelationStore() memory.RelationStore {
	return gateRelations{RelationStore: s.InMemoryStore.RelationStore(), gate: s}
}

// gateRelations 包裹真实关系存储，只在 SetParent 入口记录尝试并按策略失败。
type gateRelations struct {
	memory.RelationStore
	gate *gateStore
}

// SetParent 记录这次父子边尝试；failEdges 打开时返回错误且不写边。
func (r gateRelations) SetParent(childKey, parentKey int64) error {
	r.gate.mu.Lock()
	r.gate.parentTried = append(r.gate.parentTried, gateEdge{child: childKey, parent: parentKey})
	fail := r.gate.failEdges
	r.gate.mu.Unlock()
	if fail {
		return errGateFault
	}
	return r.RelationStore.SetParent(childKey, parentKey)
}

// snapshotFailed 返回写入失败的键副本。
func (s *gateStore) snapshotFailed() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.failed...)
}

// snapshotStored 返回写入成功的键副本。
func (s *gateStore) snapshotStored() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.stored...)
}

// snapshotTried 返回父边尝试的副本。
func (s *gateStore) snapshotTried() []gateEdge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gateEdge(nil), s.parentTried...)
}

// failParentEdges 打开/关闭父边写入故障：内容照常提交，边一条也不留下。
func (s *gateStore) failParentEdges(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failEdges = on
}

// parentOf 读一条已提交事实的父键；没有关系证据时就是 0。
func parentOf(t *testing.T, s *gateStore, key int64) int64 {
	t.Helper()
	p, err := s.InMemoryStore.GetParent(key)
	require.NoErrorf(t, err, "读取父边不得报错 key=%d", key)
	return p
}

// gateSink 记录投影追加，使"失败键不进投影"成为可直接断言的面。
type gateSink struct {
	mu   sync.Mutex
	refs []memory.EventReference
}

// Append 实现 plugin.ProjectionSink。
func (k *gateSink) Append(ref memory.EventReference) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.refs = append(k.refs, ref)
}

// size 返回已追加的投影条数。
func (k *gateSink) size() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.refs)
}

// gateAssistantEvent 构造一条框架事件：终态 assistant 产出，带唯一的响应身份。
func gateAssistantEvent(content string) *trpcevent.Event {
	return &trpcevent.Event{
		Author:    "gate-agent",
		Timestamp: time.Now().UTC(),
		Response: &model.Response{
			ID:      "resp-" + content,
			Done:    true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		},
	}
}

// gateInvocation 构造一条带真实会话的框架调用；session 决定因果域的另一半。
func gateInvocation(agentName, sessionID string) *trpcagent.Invocation {
	return &trpcagent.Invocation{
		AgentName: agentName,
		Session:   &trpcsession.Session{ID: sessionID},
	}
}

// ticketOf 取回事件上已发布的持久票据；未发布时返回 (0,false)。
func ticketOf(evt *trpcevent.Event) (int64, bool) {
	raw, ok := evt.StateDelta[tagentevent.MetaKeyEventKey]
	if !ok {
		return 0, false
	}
	key, err := tagentevent.ParseEventKey(string(raw))
	if err != nil {
		return 0, false
	}
	return key, true
}

// gateResponse 是 mock 模型侧的一条终态 assistant 响应。
func gateResponse(content string) *model.Response {
	return &model.Response{
		ID:      "resp-" + content,
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
	}
}

// gateLoop 持有一次真实 runner 管线：循环启动一次，可注入多轮消息。
//   - 管线含框架 runner、memory 插件与注入的存储替身，只有模型是 mock；
//   - turn 排空到终态响应为止，返回该轮收到的全部框架事件。
type gateLoop struct {
	ag   *tagentagent.TagentAgent
	out  <-chan *trpcevent.Event
	name string
}

// newGateLoop 以注入的存储装配一台真实 TagentAgent 并启动事件循环。
func newGateLoop(t *testing.T, store memory.MemoryStore, name string, responses []*model.Response) *gateLoop {
	t.Helper()
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:        newSequenceModel(responses),
		MemoryStore:  store,
		Name:         name,
		SystemPrompt: "gate assistant",
	})
	require.NoError(t, err)
	out, err := ag.StartLoop("gate-user", "gate-session-"+name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ag.Close() })
	return &gateLoop{ag: ag, out: out, name: name}
}

// turn 注入一条用户消息并排干到终态响应，返回本轮收到的事件。
func (l *gateLoop) turn(t *testing.T, prompt string) []*trpcevent.Event {
	t.Helper()
	l.ag.InjectMessage(model.NewUserMessage(prompt))
	var got []*trpcevent.Event
	deadline := time.After(30 * time.Second)
	for {
		select {
		case evt, ok := <-l.out:
			if !ok {
				return got
			}
			got = append(got, evt)
			if evt.IsFinalResponse() {
				return got
			}
		case <-deadline:
			t.Fatalf("turn %q never reached a terminal response", prompt)
		}
	}
}

// finalAssistant 取一轮里最后一条带正文的 assistant 事件。
func finalAssistant(t *testing.T, events []*trpcevent.Event) *trpcevent.Event {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}
		msg := evt.Response.Choices[0].Message
		if msg.Role == model.RoleAssistant && msg.Content != "" {
			return evt
		}
	}
	require.FailNow(t, "no assistant event in the drained turn", "the pipeline produced no assistant output")
	return nil
}

// committedTurnShape 是召回工具 turn_key 形态的可观测回执。
type committedTurnShape struct {
	Mode     string `json:"mode"`
	Count    int    `json:"count"`
	Complete bool   `json:"complete"`
	Capped   bool   `json:"capped"`
	Reason   string `json:"reason"`
}

// recallTurn 经导出的召回工具入口按因果链重建一个回合。
func recallTurn(t *testing.T, store memory.MemoryStore, agentName string, key int64) committedTurnShape {
	t.Helper()
	tl, ok := recall.NewRecallTool(store, []int{memory.PartitionIDFromName(agentName)}).(trpctool.CallableTool)
	require.True(t, ok, "recall 入口必须是可调用工具")
	args, err := json.Marshal(map[string]any{"turn_key": tagentevent.FormatEventKey(key)})
	require.NoError(t, err)
	raw, err := tl.Call(context.Background(), args)
	require.NoError(t, err)
	body, err := json.Marshal(raw)
	require.NoError(t, err)
	var shape committedTurnShape
	require.NoErrorf(t, json.Unmarshal(body, &shape), "turn 回执形状: %s", string(body))
	return shape
}

// TestCommittedFacts_ProjectionAndRecall 端到端钉住「票据、投影与因果只随成功提交发布」：
//   - 真实管线里写失败的事件不发布票据、不推进因果游标，之后的成功条目也不得把它当父键；
//   - 未接存储同样不得发布伪持久票据；
//   - 写入失败把本轮尝试凭据从已确认降级为不可验证；
//   - 内容已提交而父边失败时事实照常可取，回合重建报不完整并具名断点；
//   - 同一 (partition,session) 的并发提交线性化成一条链，跨域提交互不阻塞；
//   - 重启的新实例在新段起点不猜父，旧链尾不会成为伪父。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#commit-gate
func TestCommittedFacts_ProjectionAndRecall(t *testing.T) {
	t.Run("pipeline store fault publishes no ticket and no pseudo parent", func(t *testing.T) {
		gate := newGateStore()
		gate.failWhen = func(e memory.FullEvent) bool {
			return strings.HasPrefix(e.Content, "FLAKY")
		}
		loop := newGateLoop(t, gate, "gate-flaky", []*model.Response{
			gateResponse("FLAKY first answer"),
			gateResponse("second answer"),
		})

		first := loop.turn(t, "ping one")
		failedEvt := finalAssistant(t, first)
		_, ticketed := ticketOf(failedEvt)
		require.False(t, ticketed,
			"写失败的 assistant 事件不得向下游发布 event_key 票据：下游按它召回必然落空")
		require.NotContains(t, failedEvt.StateDelta, tagentevent.MetaKeyPartitionID,
			"写失败也不得伪造分区归属")
		require.Equal(t, "FLAKY first answer", failedEvt.Response.Choices[0].Message.Content,
			"正文与分类不主张持久性，失败路径仍要原样返回")

		failed := gate.snapshotFailed()
		require.Len(t, failed, 1, "写失败必须恰好留痕一次尝试 key=%v", failed)
		_, err := gate.GetEvent(failed[0])
		require.Error(t, err, "失败键从未落库，绝不能按票据取回")

		second := loop.turn(t, "ping two")
		secondEvt := finalAssistant(t, second)
		key2, ok2 := ticketOf(secondEvt)
		require.True(t, ok2, "失败之后的成功提交必须照常发布票据")
		fetched, err := gate.GetEvent(key2)
		require.NoError(t, err, "已发布的票据必须可按它取回原文")
		require.Equal(t, "second answer", fetched.Content)

		committed := make(map[int64]bool)
		for _, k := range gate.snapshotStored() {
			committed[k] = true
		}
		for _, k := range gate.snapshotStored() {
			parent := parentOf(t, gate, k)
			if parent == 0 {
				continue
			}
			require.Truef(t, committed[parent], "父键 %d 必须是已提交键之一，失败键 %v 绝不能成为任何条目的父", parent, failed)
		}
	})

	t.Run("pipeline success publishes a recallable ticket", func(t *testing.T) {
		gate := newGateStore()
		loop := newGateLoop(t, gate, "gate-control", []*model.Response{gateResponse("healthy answer")})

		events := loop.turn(t, "ping")
		final := finalAssistant(t, events)
		key, ok := ticketOf(final)
		require.True(t, ok, "对照条件：成功提交在真实管线里确实发布可取回的票据，否则失败断言读的是一个不存在的面")
		fetched, err := gate.GetEvent(key)
		require.NoError(t, err)
		require.Equal(t, "healthy answer", fetched.Content)
		require.Equal(t, strconv.Itoa(memory.PartitionIDFromName("gate-control")),
			string(final.StateDelta[tagentevent.MetaKeyPartitionID]),
			"票据里的分区归属必须与 agent 名推导一致")
	})

	t.Run("credential is downgraded when the store faults", func(t *testing.T) {
		gate := newGateStore()
		gate.failWhen = func(memory.FullEvent) bool { return true }
		sink := &gateSink{}
		cred := &plugin.EchoCredential{
			AttemptToken:  "gate-agent#attempt-1",
			Agent:         "gate-agent",
			Session:       "sess-cred",
			MergedMessage: "hello",
		}
		cred.Bind("inv-cred")
		require.True(t, cred.Verified(), "前置条件：绑定后的凭据本身是可验证的")

		ctx := plugin.WithProjectionSink(context.Background(), sink)
		ctx = plugin.WithEchoCredential(ctx, cred)
		p := plugin.NewMemoryPlugin(gate)

		res, err := p.OnEvent(ctx, gateInvocation("gate-agent", "sess-cred"), gateAssistantEvent("answer"))
		require.NoError(t, err, "存储错误由插件吞掉，不改写成插件错误上抛")
		require.False(t, cred.Verified(),
			"写入失败的回合凭据必须被降级：装了凭据而不 Verified 的回合，模型入口与 ack 决策都要失败关闭")
		require.Zero(t, sink.size(), "写失败不得进入投影")
		require.NotContains(t, res.StateDelta, tagentevent.MetaKeyEventKey)
	})

	t.Run("absent store publishes no persistent ticket and projects nothing", func(t *testing.T) {
		sink := &gateSink{}
		ctx := plugin.WithProjectionSink(context.Background(), sink)
		p := plugin.NewMemoryPlugin(nil)

		res, err := p.OnEvent(ctx, gateInvocation("gate-agent", "sess-nil"), gateAssistantEvent("answer"))
		require.NoError(t, err)
		require.NotContains(t, res.StateDelta, tagentevent.MetaKeyEventKey,
			"未接存储时发布的票据是伪持久票据")
		require.NotContains(t, res.StateDelta, tagentevent.MetaKeyPartitionID)
		require.NotEmpty(t, res.StateDelta[tagentevent.MetaKeyEventType], "分类仍可返回")
		require.Zero(t, sink.size(), "未接存储不得投影")
	})

	t.Run("broken parent edge keeps facts valid while the turn walk names the break", func(t *testing.T) {
		gate := newGateStore()
		loop := newGateLoop(t, gate, "gate-relation", []*model.Response{
			gateResponse("first answer"),
			gateResponse("second answer"),
		})

		first := loop.turn(t, "ping one")
		key1, ok1 := ticketOf(finalAssistant(t, first))
		require.True(t, ok1, "前置条件：第一条已提交并拿到票据")

		require.NotZero(t, parentOf(t, gate, key1), "注入条件：故障打开之前父边是正常建立的")

		gate.failParentEdges(true)
		second := loop.turn(t, "ping two")
		key2, ok2 := ticketOf(finalAssistant(t, second))
		require.True(t, ok2, "故障只打在父边上：事实与票据照常有效")
		fetched, err := gate.GetEvent(key2)
		require.NoError(t, err, "已提交内容必须可按票据取回")
		require.Equal(t, "second answer", fetched.Content)

		tried := gate.snapshotTried()
		require.NotEmpty(t, tried, "关系失败的尝试必须留痕，供回溯端报断点")

		shape := recallTurn(t, gate, "gate-relation", key2)
		require.Equal(t, "turn", shape.Mode)
		require.False(t, shape.Complete, "父边写失败的那一段因果不完整，回合重建绝不能冒称因果完整")
		require.Equal(t, "no_parent_edge", shape.Reason, "断点必须具名到因果边缺失，不能读成一个恰好走空的回合")
		require.GreaterOrEqual(t, shape.Count, 1, "已提交的事实本身仍必须被重建出来")
	})

	t.Run("concurrent commits on one causal domain stay linear", func(t *testing.T) {
		gate := newGateStore()
		p := plugin.NewMemoryPlugin(gate)
		inv := gateInvocation("gate-concurrent", "sess-conc")

		const writers = 8
		keys := make([]int64, writers)
		var wg sync.WaitGroup
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				res, err := p.OnEvent(context.Background(), inv, gateAssistantEvent(fmt.Sprintf("c%d", i)))
				require.NoError(t, err)
				key, ok := ticketOf(res)
				require.Truef(t, ok, "并发提交的每一条都必须发布票据 c%d", i)
				keys[i] = key
			}(i)
		}
		wg.Wait()

		seen := make(map[int64]bool, writers)
		for _, k := range keys {
			require.Falsef(t, seen[k], "并发提交不得分配到重复的事件键 %d", k)
			seen[k] = true
		}
		parents := make(map[int64]int64, writers)
		inbound := make(map[int64]int, writers)
		roots := 0
		for _, k := range keys {
			parent := parentOf(t, gate, k)
			parents[k] = parent
			if parent == 0 {
				roots++
				continue
			}
			require.Truef(t, seen[parent], "并发域的父键 %d 必须是本域已提交键之一，不得指向虚事件", parent)
			inbound[parent]++
		}
		require.Equal(t, 1, roots, "同因果域的并发提交必须收成一条链：只有一个段起点没有父")
		for k, n := range inbound {
			require.Equalf(t, 1, n, "键 %d 不得被多个子条目认作父（分叉链）", k)
		}
		tail := keys[0]
		for _, k := range keys {
			if inbound[k] == 0 {
				tail = k
			}
		}
		walked := map[int64]bool{}
		for cur := tail; cur != 0; cur = parents[cur] {
			require.Falsef(t, walked[cur], "父边不得成环：%d 被走到第二次", cur)
			walked[cur] = true
		}
		require.Len(t, walked, writers, "从链尾回溯必须能走完全部 %d 条已提交事实", writers)
	})

	t.Run("a blocked causal domain does not stall another domain", func(t *testing.T) {
		gate := newGateStore()
		gate.blockWhen = func(e memory.FullEvent) bool { return strings.HasPrefix(e.Content, "BLOCKED") }
		gate.parked = make(chan struct{})
		gate.release = make(chan struct{})
		p := plugin.NewMemoryPlugin(gate)

		blockedDone := make(chan *trpcevent.Event, 1)
		go func() {
			res, err := p.OnEvent(context.Background(),
				gateInvocation("gate-blocked", "sess-slow"), gateAssistantEvent("BLOCKED slow commit"))
			require.NoError(t, err)
			blockedDone <- res
		}()

		<-gate.parked
		freeCtx := context.Background()
		freeEvt, err := p.OnEvent(freeCtx, gateInvocation("gate-free", "sess-quick"),
			gateAssistantEvent("FREE quick commit"))
		require.NoError(t, err, "另一条因果域的提交必须落在自己的锁段里，不等阻塞域")
		_, ok := ticketOf(freeEvt)
		require.True(t, ok, "未被阻塞的域照常发布票据")

		close(gate.release)
		var blocked *trpcevent.Event
		select {
		case blocked = <-blockedDone:
		case <-time.After(10 * time.Second):
			t.Fatal("the parked commit never resumed after release")
		}
		bkey, bok := ticketOf(blocked)
		require.True(t, bok, "解除阻塞后的提交照常发布票据")
		require.NotEqual(t, parentOf(t, gate, bkey),
			mustTicket(t, freeEvt), "不同因果域各自成链，绝不共享父键")
	})

	t.Run("a restarted instance starts a new segment without inventing a parent", func(t *testing.T) {
		gate := newGateStore()
		first := plugin.NewMemoryPlugin(gate)
		res1, err := first.OnEvent(context.Background(), gateInvocation("gate-restart", "sess-r"),
			gateAssistantEvent("segment one"))
		require.NoError(t, err)
		key1, ok1 := ticketOf(res1)
		require.Truef(t, ok1 && key1 != 0, "旧段的第一条同样拿到可取回的票据")

		second := plugin.NewMemoryPlugin(gate)
		res2, err := second.OnEvent(context.Background(), gateInvocation("gate-restart", "sess-r"),
			gateAssistantEvent("segment two"))
		require.NoError(t, err)
		key2, ok2 := ticketOf(res2)
		require.True(t, ok2, "新实例的提交同样要发布可取回的票据")
		fetched, err := gate.GetEvent(key2)
		require.NoError(t, err, "新段事实同样按票据可回")
		require.Equal(t, "segment two", fetched.Content)

		require.Zero(t, parentOf(t, gate, key2),
			"新实例没有旧链的因果证据：段起点必须停在这里，不猜旧链尾当父")
		require.Empty(t, gate.snapshotTried(), "没有证据就不写父边：一条伪父边也不得出现")
	})
}

// mustTicket 取回事件上已发布的票据，缺失即失败。
func mustTicket(t *testing.T, evt *trpcevent.Event) int64 {
	t.Helper()
	key, ok := ticketOf(evt)
	require.True(t, ok, "该事件必须已发布持久票据")
	return key
}
