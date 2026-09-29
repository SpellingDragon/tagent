package recall

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func callMemoryRecall(t *testing.T, tl tool.Tool, args string) string {
	t.Helper()
	ct, ok := tl.(tool.CallableTool)
	if !ok {
		t.Fatalf("memory_recall must be callable")
	}
	out, err := ct.Call(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// seedPartitions 返回种子事件所在的显式授权分区——隔离契约（2.7）下无分区
// 查询返回空，查询必须显式授权（生产中宿主恒注入 agent 自身分区）。
func seedPartitions() []int {
	return []int{memory.PartitionIDFromEventKey(100)}
}

func seedStore(t *testing.T) memory.MemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	must := func(err error) {
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	must(store.StoreEvent(100, memory.FullEvent{EventKey: 100, EventType: "external_input", EventSummary: "部署请求", Content: "请部署服务到测试环境", Timestamp: 1710000000000}))
	must(store.StoreEvent(200, memory.FullEvent{EventKey: 200, EventType: "agent_output", EventSummary: "部署完成", Content: "服务已部署,健康检查通过", Timestamp: 1710000100000}))
	return store
}

// TestMemoryRecall_ItemsPrecise 钉住 一批票据按给定顺序精确回补，命中不掺水。
// - 两条票据各自取回自己的全文，输出顺序与入参一致；
// - 随行的 hint 原样带回，一条未命中都不许出现。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_ItemsPrecise(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+tagentevent.FormatEventKey(200)+`","hint":"部署完成卡片"},{"key":"`+tagentevent.FormatEventKey(100)+`"}]}`)

	for _, want := range []string{`"mode":"items"`, "服务已部署", "请部署服务", "部署完成卡片"} {
		if !strings.Contains(out, want) {
			t.Errorf("items recall must contain %q, got: %s", want, out)
		}
	}
	if strings.Contains(out, `"miss":true`) {
		t.Errorf("no miss expected, got: %s", out)
	}
}

// TestMemoryRecall_MissReported 钉住 认不出的票据逐条标为未命中，绝不静默省略。
// - 未知 key 带 miss 标记，且未命中计数为 1；
// - 同批里那条真票据照常命中——一条失败不得把整批说空。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_MissReported(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"items":[{"key":"dead"},{"key":"`+tagentevent.FormatEventKey(100)+`"}]}`)

	if !strings.Contains(out, `"miss":true`) || !strings.Contains(out, `"misses":1`) {
		t.Errorf("miss must be reported explicitly, got: %s", out)
	}
	if !strings.Contains(out, "请部署服务") {
		t.Errorf("hit entry must still resolve, got: %s", out)
	}
}

// TestMemoryRecall_QuerySemantic 钉住 自由文本走检索层，按关键词命中。
// - 模式标记为 query，摘要含"部署"的事件被带回。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_QuerySemantic(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"query":"部署"}`)

	if !strings.Contains(out, `"mode":"query"`) || !strings.Contains(out, "部署完成") {
		t.Errorf("query recall must match by keyword, got: %s", out)
	}
}

// TestMemoryRecall_QueryZeroResultHonesty 钉住 零结果不得伪装成"后端没有历史"。
// - 计数为 0 时消息里必须写明检索范围（无可读分区内的匹配事件）；
// - 同时给出下一步该走 items，免得模型拿空列表推断全局。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_QueryZeroResultHonesty(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"query":"不存在的关键词"}`)

	if !strings.Contains(out, `"count":0`) {
		t.Fatalf("expected zero-result query, got: %s", out)
	}
	for _, want := range []string{"无可读分区内的匹配事件", "items"} {
		if !strings.Contains(out, want) {
			t.Errorf("zero-result message must mention %q, got: %s", want, out)
		}
	}
}

// TestMemoryRecall_ItemsPrecedence 钉住 items 与 query 同时给出时 items 取胜。
// - 结果模式必须是 items——落进 query 分支就是协议被改。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_ItemsPrecedence(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+tagentevent.FormatEventKey(100)+`"}],"query":"部署"}`)

	if !strings.Contains(out, `"mode":"items"`) {
		t.Errorf("items must take precedence over query, got: %s", out)
	}
}

// TestMemoryRecall_TicketLosslessness 钉住 从索引卡行里切出的字符串原样就能当票据。
// - 卡片行 `[hex]` 内那一段不加任何清洗直接提交，必须命中且不带 miss。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryRecall_TicketLosslessness(t *testing.T) {
	cardLine := "- 03-09 17:20 [" + tagentevent.FormatEventKey(200) + "] 部署完成"
	start := strings.IndexByte(cardLine, '[')
	end := strings.IndexByte(cardLine, ']')
	ticket := cardLine[start+1 : end]

	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+ticket+`"}]}`)
	if strings.Contains(out, `"miss":true`) || !strings.Contains(out, "服务已部署") {
		t.Errorf("card-line ticket must resolve losslessly, got: %s", out)
	}
}

// buildChainedTurn stores one task turn with a causal chain:
// external_input → thinking_plan → action_command → agent_output,
// each event's parent = the previous event. Returns the agent_output key.
func buildChainedTurn(t *testing.T, store *memory.InMemoryStore) int64 {
	t.Helper()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.StoreEvent(100, memory.FullEvent{EventKey: 100, EventType: tagentevent.TypeExternalInput, EventSummary: "部署请求", Content: "请部署服务", Timestamp: 1710000000000}))
	must(store.StoreEvent(200, memory.FullEvent{EventKey: 200, EventType: tagentevent.TypeThinkingPlan, EventSummary: "思考", Content: "计划先构建再部署", Timestamp: 1710000010000}))
	must(store.StoreEvent(300, memory.FullEvent{EventKey: 300, EventType: tagentevent.TypeActionCommand, EventSummary: "执行", Content: "go build && ./deploy.sh", Timestamp: 1710000020000}))
	must(store.StoreEvent(400, memory.FullEvent{EventKey: 400, EventType: tagentevent.TypeAgentOutput, EventSummary: "部署完成", Content: "服务已部署", Timestamp: 1710000030000}))

	rs := store.RelationStore()
	must(rs.SetParent(200, 100))
	must(rs.SetParent(300, 200))
	must(rs.SetParent(400, 300))
	return 400
}

func callMemoryTurn(t *testing.T, store *memory.InMemoryStore, key string) memoryTurnResult {
	t.Helper()
	tl := NewMemoryTurnTool(store).(tool.CallableTool)
	args, _ := json.Marshal(memoryTurnArgs{Key: key})
	out, err := tl.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("memory_turn call: %v", err)
	}
	raw, _ := json.Marshal(out)
	var res memoryTurnResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return res
}

// TestMemoryTurn_ReconstructsWholeTurn 钉住 锚在回合末尾也能重建整轮，并按时间正序交回。
// - 从 agent_output 回溯到 external_input，四件事一条不落，complete 为真；
// - 顺序 oldest→newest，压缩会丢的工具步骤（action_command 全文）照样带回。
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestMemoryTurn_ReconstructsWholeTurn(t *testing.T) {
	store := memory.NewInMemoryStore()
	aoKey := buildChainedTurn(t, store)

	res := callMemoryTurn(t, store, tagentevent.FormatEventKey(aoKey))

	if !res.Complete {
		t.Errorf("walk must reach the turn's external_input (complete=true), got %+v", res)
	}
	if res.Count != 4 {
		t.Fatalf("expected 4 events in the turn, got %d: %+v", res.Count, res.Events)
	}
	wantTypes := []string{
		tagentevent.TypeExternalInput,
		tagentevent.TypeThinkingPlan,
		tagentevent.TypeActionCommand,
		tagentevent.TypeAgentOutput,
	}
	for i, want := range wantTypes {
		if res.Events[i].Type != want {
			t.Errorf("event[%d] type = %q, want %q", i, res.Events[i].Type, want)
		}
	}
	if res.Events[2].Content != "go build && ./deploy.sh" {
		t.Errorf("action_command content must be recovered, got %q", res.Events[2].Content)
	}
}

// TestMemoryTurn_StopsAtExternalInput 钉住 回溯止于本回合的 external_input，不渗进上一回合。
// - 前一回合的 agent_output 已挂在本回合起点之下，却不得出现在结果里；
// - 首条必须是本轮的 external_input。
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestMemoryTurn_StopsAtExternalInput(t *testing.T) {
	store := memory.NewInMemoryStore()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.StoreEvent(50, memory.FullEvent{EventKey: 50, EventType: tagentevent.TypeAgentOutput, EventSummary: "上一轮", Timestamp: 1709999990000}))
	aoKey := buildChainedTurn(t, store)
	must(store.RelationStore().SetParent(100, 50))

	res := callMemoryTurn(t, store, tagentevent.FormatEventKey(aoKey))

	if !res.Complete {
		t.Errorf("walk must complete at this turn's external_input, got %+v", res)
	}
	for _, e := range res.Events {
		if e.Key == tagentevent.FormatEventKey(50) {
			t.Errorf("walk must stop at external_input, must not cross into previous turn: %+v", res.Events)
		}
	}
	if res.Events[0].Type != tagentevent.TypeExternalInput {
		t.Errorf("first event must be this turn's external_input, got %q", res.Events[0].Type)
	}
}

// TestMemoryTurn_Capped 钉住 上界截断与"完整"分家：被切断的回溯绝不报 complete。
// - 只给一步且尚未触及起点时 capped 为真、complete 为假；
// - 结果只含锚点事件那一条。
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestMemoryTurn_Capped(t *testing.T) {
	store := memory.NewInMemoryStore()
	aoKey := buildChainedTurn(t, store)

	tl := NewMemoryTurnTool(store).(tool.CallableTool)
	args, _ := json.Marshal(memoryTurnArgs{Key: tagentevent.FormatEventKey(aoKey), MaxSteps: 1})
	out, err := tl.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	raw, _ := json.Marshal(out)
	var res memoryTurnResult
	_ = json.Unmarshal(raw, &res)

	if !res.Capped {
		t.Errorf("MaxSteps=1 before external_input must set Capped=true, got %+v", res)
	}
	if res.Complete {
		t.Errorf("Complete must be false when capped before external_input")
	}
	if res.Count != 1 {
		t.Errorf("capped walk must return only the anchored event, got %d", res.Count)
	}
}

// TestMemoryTurn_BrokenChainHonest 钉住 断链时如实交回找到的部分，不报错也不假装完整。
// - 父事件不在库里：complete 必为假；
// - 只返回锚点事件，绝不跨过断点去猜。
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestMemoryTurn_BrokenChainHonest(t *testing.T) {
	store := memory.NewInMemoryStore()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.StoreEvent(100, memory.FullEvent{EventKey: 100, EventType: tagentevent.TypeExternalInput, EventSummary: "请求", Timestamp: 1710000000000}))
	must(store.StoreEvent(400, memory.FullEvent{EventKey: 400, EventType: tagentevent.TypeAgentOutput, EventSummary: "完成", Timestamp: 1710000030000}))
	must(store.RelationStore().SetParent(400, 200))

	res := callMemoryTurn(t, store, tagentevent.FormatEventKey(400))

	if res.Complete {
		t.Errorf("broken chain must not report Complete=true")
	}
	if res.Count != 1 || res.Events[0].Type != tagentevent.TypeAgentOutput {
		t.Errorf("broken chain must return only the anchored event, got %+v", res.Events)
	}
}

// TestMemoryTurn_EvtPrefixTolerance 钉住 三种回显形态都落到同一个事件。
// - 裸 hex、带 `evt_` 前缀、时间线上的 `[evt_hex|type]` 整段——任何一种都不许查空。
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func TestMemoryTurn_EvtPrefixTolerance(t *testing.T) {
	store := memory.NewInMemoryStore()
	aoKey := buildChainedTurn(t, store)
	hexKey := tagentevent.FormatEventKey(aoKey)

	for _, form := range []string{
		hexKey,
		"evt_" + hexKey,
		"[evt_" + hexKey + "|agent_output]",
	} {
		res := callMemoryTurn(t, store, form)
		if res.Count == 0 {
			t.Errorf("key form %q must resolve, got empty", form)
		}
	}
}
