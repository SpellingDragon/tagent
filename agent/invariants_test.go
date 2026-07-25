package agent

import (
	"context"
	"fmt"
	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
)

// ============================================================================
// unified-event-projection invariant tests.
//
// I1 write-unification: stored ⟺ projected, exactly once (pipeline).
// I2 ordering: BeforeModel render contains all previously stored events.
// I3 render legality: no role=tool, no empty agent_output, no duplicate keys.
// I4 one-way boundary: assembly ignores the framework message tail.
// ============================================================================

// assertRenderLegality is the I3 assertion helper (D3 v2): the rendered
// sequence must be a LEGAL NATIVE conversation — every role=tool message has
// a prior assistant declaring its ToolID (no orphans, no duplicate answers),
// no empty pure-text assistant messages, no duplicate [evt_KEY prefixes.
func assertRenderLegality(t *testing.T, msgs []model.Message) {
	t.Helper()
	seenKeys := map[int64]bool{}
	declared := map[string]bool{}
	consumed := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case model.RoleAssistant:
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
			if len(m.ToolCalls) == 0 && strings.TrimSpace(tagentevent.StripEventKeyPrefix(m.Content)) == "" {
				t.Errorf("render legality: msg[%d] is an empty assistant output", i)
			}
		case model.RoleTool:
			if m.ToolID == "" || !declared[m.ToolID] {
				t.Errorf("render legality: msg[%d] is an orphan tool result (tool_id=%q has no prior declaring call)", i, m.ToolID)
			}
			if consumed[m.ToolID] {
				t.Errorf("render legality: msg[%d] duplicates an already-answered tool_id=%q", i, m.ToolID)
			}
			consumed[m.ToolID] = true
		}
		key, _, _ := tagentevent.ParseEventKeyAndType(m.Content)
		if key > 0 {
			if seenKeys[key] {
				t.Errorf("render legality: duplicate event key %d at msg[%d]", key, i)
			}
			seenKeys[key] = true
		}
	}
}

// seedProjectedToolTurn stores a thinking_plan + action_command pair through
// MemoryStore and the projection, mirroring a completed ReAct step.
func seedProjectedToolTurn(t *testing.T, cm *ContextManager) {
	t.Helper()
	now := time.Now().UnixMilli()
	// PartitionID left 0: StoreEvent derives it from the key, matching GetEvent's
	// PartitionIDFromEventKey lookup (mirrors production Snowflake keys).
	events := []memory.FullEvent{
		{
			EventKey: 1001, EventType: "external_input",
			EventSummary: "list files", Content: "list files", Timestamp: now,
		},
		{
			EventKey: 1002, EventType: "thinking_plan",
			EventSummary: "calling ls", Content: "",
			ToolCalls: []model.ToolCall{{ID: "call-1", Function: model.FunctionDefinitionParam{Name: "action", Arguments: []byte(`{"command":"ls"}`)}}},
			Timestamp: now + 1,
		},
		{
			EventKey: 1003, EventType: "action_command",
			EventSummary: "status=completed", Content: `{"command":"ls","status":"completed","output":"a.txt"}`,
			ToolID:    "call-1",
			Timestamp: now + 2,
		},
	}
	roles := []string{"user", "assistant", "tool"}
	for i, fe := range events {
		if err := cm.memStore.StoreEvent(fe.EventKey, fe); err != nil {
			t.Fatalf("store event %d: %v", fe.EventKey, err)
		}
		cm.projection.Append(memory.EventReference{
			EventKey: fe.EventKey, EventType: fe.EventType,
			EventSummary: fe.EventSummary, Timestamp: fe.Timestamp, Role: roles[i],
		})
	}
}

// newAssistantEvt builds a framework event carrying an assistant message,
// mirroring what the framework emits per model response.
func newAssistantEvt(content string) *event.Event {
	evt := event.New("inv-test", "tagent")
	evt.Timestamp = time.Now()
	evt.Response = &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
	}
	return evt
}

// TestProjectionSink_PerInvocationIsolation: concurrent invocations (main
// loop vs sub-agent) must each project only their own events — the ctx-bound
// sink can never cross-write (design.md D1 risk coverage).
func TestProjectionSink_PerInvocationIsolation(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := plugin.NewMemoryPlugin(store)
	mainProj := compress.NewSessionProjection()
	subProj := compress.NewSessionProjection()
	ctxMain := plugin.WithProjectionSink(context.Background(), mainProj)
	ctxSub := plugin.WithProjectionSink(context.Background(), subProj)

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if _, err := p.OnEvent(ctxMain, nil, newAssistantEvt(fmt.Sprintf("main-%d", i))); err != nil {
				t.Errorf("main OnEvent: %v", err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if _, err := p.OnEvent(ctxSub, nil, newAssistantEvt(fmt.Sprintf("sub-%d", i))); err != nil {
				t.Errorf("sub OnEvent: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if mainProj.Len() != n {
		t.Fatalf("main projection must hold %d refs, got %d", n, mainProj.Len())
	}
	if subProj.Len() != n {
		t.Fatalf("sub projection must hold %d refs, got %d", n, subProj.Len())
	}
	for _, ref := range mainProj.GetAll() {
		if !strings.HasPrefix(ref.EventSummary, "main-") {
			t.Errorf("cross-write: sub event leaked into main projection: %+v", ref)
		}
	}
	for _, ref := range subProj.GetAll() {
		if !strings.HasPrefix(ref.EventSummary, "sub-") {
			t.Errorf("cross-write: main event leaked into sub projection: %+v", ref)
		}
	}
}

// TestI3_RenderLegality_NativePairing: resolving a projection containing a
// thinking_plan + action_command pair must yield a legal NATIVE rendering —
// assistant carries native ToolCalls, the result is role=tool paired by id.
func TestI3_RenderLegality_NativePairing(t *testing.T) {
	cm := newTestContextManager("i3-agent", nil, nil, nil, nil)
	seedProjectedToolTurn(t, cm)

	result := cm.contextCompressor.Compress(context.Background(), cm.projection.GetAll())
	assertRenderLegality(t, result.Messages)

	// Native forms present: assistant with ToolCalls, tool result paired.
	var sawNativeCall, sawPairedResult bool
	for _, m := range result.Messages {
		if m.Role == model.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "call-1" {
			sawNativeCall = true
			if strings.Contains(m.Content, "call-1") {
				t.Errorf("assistant content must not contain textual call syntax, got: %q", m.Content)
			}
		}
		if m.Role == model.RoleTool && m.ToolID == "call-1" {
			sawPairedResult = true
		}
	}
	if !sawNativeCall || !sawPairedResult {
		t.Errorf("expected native call+result pair, got call=%v result=%v: %+v", sawNativeCall, sawPairedResult, result.Messages)
	}
}

// TestI4_AssemblyIgnoresFrameworkTail: garbage (or stale duplicates) in the
// framework's message tail must not affect the assembled request — assembly
// reads only the system message from args.
func TestI4_AssemblyIgnoresFrameworkTail(t *testing.T) {
	cm := newTestContextManager("i4-agent", nil, nil, nil, nil)
	seedProjectedToolTurn(t, cm)

	system := model.Message{Role: model.RoleSystem, Content: "sys"}

	clean := &model.BeforeModelArgs{Request: &model.Request{Messages: []model.Message{system}}}
	cm.assembleRequest(context.Background(), clean)

	polluted := &model.BeforeModelArgs{Request: &model.Request{Messages: []model.Message{
		system,
		{Role: model.RoleAssistant, Content: "", ToolCalls: []model.ToolCall{{ID: "stale-call"}}},
		{Role: model.RoleTool, Content: "stale result", ToolID: "stale-call"},
		{Role: model.RoleUser, Content: "garbage echo"},
	}}}
	cm.assembleRequest(context.Background(), polluted)

	if len(clean.Request.Messages) != len(polluted.Request.Messages) {
		t.Fatalf("assembly must ignore framework tail: clean=%d msgs, polluted=%d msgs",
			len(clean.Request.Messages), len(polluted.Request.Messages))
	}
	for i := range clean.Request.Messages {
		c, p := clean.Request.Messages[i], polluted.Request.Messages[i]
		if c.Role != p.Role || c.Content != p.Content {
			t.Errorf("assembly diverges at msg[%d]:\n  clean:    %s %q\n  polluted: %s %q",
				i, c.Role, c.Content, p.Role, p.Content)
		}
	}
}
