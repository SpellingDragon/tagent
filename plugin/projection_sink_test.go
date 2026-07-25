package plugin

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// recordingSink captures projected refs for assertions.
type recordingSink struct {
	refs []memory.EventReference
}

func (r *recordingSink) Append(ref memory.EventReference) { r.refs = append(r.refs, ref) }

func newResponseEvent(role model.Role, content string, toolCalls []model.ToolCall) *event.Event {
	evt := event.New("inv-1", "tagent")
	evt.Timestamp = time.Now()
	evt.Response = &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: role, Content: content, ToolCalls: toolCalls}}},
	}
	return evt
}

// TestI1_PipelineStoreProjectsExactlyOnce: a stored event is projected at the
// same synchronous point, exactly once, with the ref matching the StateDelta
// identifiers (write unification, unified-event-projection D1).
func TestI1_PipelineStoreProjectsExactlyOnce(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	evt := newResponseEvent(model.RoleAssistant, "hello there", nil)
	if _, err := p.OnEvent(ctx, nil, evt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	// I2 (synchronous append): the ref must be visible the moment OnEvent
	// returns — no goroutine hop, no timing dependence.
	if len(sink.refs) != 1 {
		t.Fatalf("expected exactly 1 projected ref, got %d", len(sink.refs))
	}
	ref := sink.refs[0]
	wantKey, err := tagentevent.ParseEventKey(string(evt.StateDelta["event_key"]))
	if err != nil || wantKey == 0 {
		t.Fatalf("StateDelta event_key missing/invalid: %v", err)
	}
	if ref.EventKey != wantKey {
		t.Errorf("projected key %d != StateDelta key %d", ref.EventKey, wantKey)
	}
	if ref.EventType != "agent_output" || ref.Role != "assistant" {
		t.Errorf("ref type/role mismatch: %+v", ref)
	}
	// stored ⟺ projected: the same key must be retrievable from the store.
	if _, err := store.GetEvent(wantKey); err != nil {
		t.Errorf("stored event not retrievable: %v", err)
	}
}

// TestI1_NoSinkNoPanic: absent a sink in ctx the plugin stores without
// projecting (standalone runner usage).
func TestI1_NoSinkNoPanic(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	evt := newResponseEvent(model.RoleAssistant, "hi", nil)
	if _, err := p.OnEvent(context.Background(), nil, evt); err != nil {
		t.Fatalf("OnEvent without sink: %v", err)
	}
}

// TestI1_SkippedEventsNotProjected: events the plugin skips (nil response,
// degenerate empty agent_output) must not reach the sink either — the
// projection is a faithful index of the store.
func TestI1_SkippedEventsNotProjected(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	// nil-response barrier event
	bare := event.New("inv-1", "tagent")
	if _, err := p.OnEvent(ctx, nil, bare); err != nil {
		t.Fatalf("OnEvent(bare): %v", err)
	}
	// degenerate empty final (H1)
	empty := newResponseEvent(model.RoleAssistant, "", nil)
	if _, err := p.OnEvent(ctx, nil, empty); err != nil {
		t.Fatalf("OnEvent(empty final): %v", err)
	}
	// streaming partial delta (D8)
	partial := newResponseEvent(model.RoleAssistant, "partial chunk", nil)
	partial.Response.IsPartial = true
	if _, err := p.OnEvent(ctx, nil, partial); err != nil {
		t.Fatalf("OnEvent(partial): %v", err)
	}

	if len(sink.refs) != 0 {
		t.Errorf("skipped events must not be projected, got %d refs", len(sink.refs))
	}
}

// TestSanitizeAssistantContent_StripsFabricatedPrefix: a model-imitated
// [evt_...] prefix must be stripped at the storage boundary — fake keys would
// poison prefixEventKey skipping and retained-ref scanning downstream.
func TestSanitizeAssistantContent_StripsFabricatedPrefix(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	evt := newResponseEvent(model.RoleAssistant, "[evt_1297376009205734912|thinking_plan] 我看到了知识库。", nil)
	if _, err := p.OnEvent(ctx, nil, evt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if len(sink.refs) != 1 {
		t.Fatalf("expected 1 projected ref, got %d", len(sink.refs))
	}
	stored, err := store.GetEvent(sink.refs[0].EventKey)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if stored.Content != "我看到了知识库。" {
		t.Errorf("fabricated prefix must be stripped at storage, got: %q", stored.Content)
	}
}

// TestI1_ToolTurnProjectsAllSteps: assistant tool_call, tool result, and final
// each project exactly one ref, in pipeline order.
func TestI1_ToolTurnProjectsAllSteps(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	steps := []*event.Event{
		newResponseEvent(model.RoleAssistant, "", []model.ToolCall{{ID: "c1"}}),
		newResponseEvent(model.RoleTool, `{"status":"completed"}`, nil),
		newResponseEvent(model.RoleAssistant, "done", nil),
	}
	for i, evt := range steps {
		if _, err := p.OnEvent(ctx, nil, evt); err != nil {
			t.Fatalf("OnEvent step %d: %v", i, err)
		}
	}

	if len(sink.refs) != 3 {
		t.Fatalf("expected 3 projected refs, got %d", len(sink.refs))
	}
	wantTypes := []string{"thinking_plan", "action_command", "agent_output"}
	for i, want := range wantTypes {
		if sink.refs[i].EventType != want {
			t.Errorf("ref[%d] type = %s, want %s", i, sink.refs[i].EventType, want)
		}
	}
}
