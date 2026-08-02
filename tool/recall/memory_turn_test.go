package recall

import (
	"context"
	"encoding/json"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

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
	must(rs.SetParent(200, 100)) // thinking_plan ← external_input
	must(rs.SetParent(300, 200)) // action_command ← thinking_plan
	must(rs.SetParent(400, 300)) // agent_output ← action_command
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
	// Chronological order: external_input → thinking_plan → action_command → agent_output.
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
	// The dropped tool steps' execution detail is present (the "how").
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
	// A previous turn's tail (agent_output) chained BEFORE this turn's input.
	must(store.StoreEvent(50, memory.FullEvent{EventKey: 50, EventType: tagentevent.TypeAgentOutput, EventSummary: "上一轮", Timestamp: 1709999990000}))
	aoKey := buildChainedTurn(t, store)
	// Link this turn's external_input to the previous turn's agent_output.
	must(store.RelationStore().SetParent(100, 50))

	res := callMemoryTurn(t, store, tagentevent.FormatEventKey(aoKey))

	if !res.Complete {
		t.Errorf("walk must complete at this turn's external_input, got %+v", res)
	}
	// Must NOT include the previous turn's agent_output (key 50).
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
	// 200 (thinking_plan) is NOT stored — chain broken.
	must(store.StoreEvent(400, memory.FullEvent{EventKey: 400, EventType: tagentevent.TypeAgentOutput, EventSummary: "完成", Timestamp: 1710000030000}))
	must(store.RelationStore().SetParent(400, 200)) // parent points to a missing event

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
// recall key. ParseEventKey must tolerate these forms (code-review Nit).
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
