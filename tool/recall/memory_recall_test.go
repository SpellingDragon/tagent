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

// TestMemoryRecall_ItemsPrecise: tickets are resolved precisely, in order,
//
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

// TestMemoryRecall_MissReported: unknown keys are explicitly marked miss,
// never silently omitted.
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

// TestMemoryRecall_QuerySemantic: free-text query goes through the retrieval
// layer (keyword match).
func TestMemoryRecall_QuerySemantic(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"query":"部署"}`)

	if !strings.Contains(out, `"mode":"query"`) || !strings.Contains(out, "部署完成") {
		t.Errorf("query recall must match by keyword, got: %s", out)
	}
}

// TestMemoryRecall_QueryZeroResultHonesty: a zero-result query must NOT return
// a bare empty list (observed in production: the model concluded "the backend
// has no history at all" from a silent count:0). It must carry a message
// stating what was searched and steering toward items/turn_key.
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

// TestMemoryRecall_ItemsPrecedence: when both items and query are provided,
// items win (protocol rule).
func TestMemoryRecall_ItemsPrecedence(t *testing.T) {
	tl := NewMemoryRecallTool(seedStore(t), seedPartitions())
	out := callMemoryRecall(t, tl, `{"items":[{"key":"`+tagentevent.FormatEventKey(100)+`"}],"query":"部署"}`)

	if !strings.Contains(out, `"mode":"items"`) {
		t.Errorf("items must take precedence over query, got: %s", out)
	}
}

// TestMemoryRecall_TicketLosslessness: a rendered index-card line's [hex] key
// can be cut out verbatim and used as a ticket.
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

// TestMemoryTurn_ReconstructsWholeTurn: anchoring on the agent_output key walks
// the causal chain back to the external_input and returns the whole turn
// (including the tool steps compression would drop), oldest → newest, stopping
// at external_input.
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

// TestMemoryTurn_StopsAtExternalInput: the walk must not cross into the
// previous turn — it stops at the first external_input reached.
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

// TestMemoryTurn_Capped: when MaxSteps is hit before reaching external_input,
// Capped is true and Complete is false — the model is told the walk was cut.
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

// TestMemoryTurn_BrokenChainHonest: when the chain breaks mid-walk (a parent
// event is missing from the store), the tool returns what it found with
// Complete=false — honest degradation, not an error or a misleading full turn.
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

// TestMemoryTurn_EvtPrefixTolerance: models see keys rendered as [evt_HEX|type]
// in the timeline and will echo "evt_HEX" or the bracketed form back as a
// recall key. ParseEventKey must tolerate these forms.
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
