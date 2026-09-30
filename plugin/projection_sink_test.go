package plugin

import (
	"context"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// recordingSink captures projected refs for assertions.
type recordingSink struct {
	refs []memory.EventReference
}

func (r *recordingSink) Append(ref memory.EventReference) { r.refs = append(r.refs, ref) }

func newResponseEvent(role model.Role, content string, toolCalls []model.ToolCall) *trpcEvent.Event {
	evt := trpcEvent.New("inv-1", "tagent")
	evt.Timestamp = time.Now()
	evt.Response = &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: role, Content: content, ToolCalls: toolCalls}}},
	}
	return evt
}

// TestPipelineStoreProjectsExactlyOnce 钉住「写入即投影」的同一同步点：入库事件恰好投影一次，
//
// 契约: docs/wiki/plugin/plugin-architecture.md#skip-set
func TestPipelineStoreProjectsExactlyOnce(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	evt := newResponseEvent(model.RoleAssistant, "hello there", nil)
	if _, err := p.OnEvent(ctx, nil, evt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

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
	if _, err := store.GetEvent(wantKey); err != nil {
		t.Errorf("stored event not retrievable: %v", err)
	}
}

// TestProjectionNoSinkNoPanic 钉住 ctx 无投影接收端时仍正常入库、只是不投影（独立运行场景）。
func TestProjectionNoSinkNoPanic(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	evt := newResponseEvent(model.RoleAssistant, "hi", nil)
	if _, err := p.OnEvent(context.Background(), nil, evt); err != nil {
		t.Fatalf("OnEvent without sink: %v", err)
	}
}

// TestSkippedEventsNotProjected 钉住被跳过的四类事件同样不得进投影——投影必须是存储的忠实索引。
func TestSkippedEventsNotProjected(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	bare := trpcEvent.New("inv-1", "tagent")
	if _, err := p.OnEvent(ctx, nil, bare); err != nil {
		t.Fatalf("OnEvent(bare): %v", err)
	}
	empty := newResponseEvent(model.RoleAssistant, "", nil)
	if _, err := p.OnEvent(ctx, nil, empty); err != nil {
		t.Fatalf("OnEvent(empty final): %v", err)
	}
	partial := newResponseEvent(model.RoleAssistant, "partial chunk", nil)
	partial.Response.IsPartial = true
	if _, err := p.OnEvent(ctx, nil, partial); err != nil {
		t.Fatalf("OnEvent(partial): %v", err)
	}

	if len(sink.refs) != 0 {
		t.Errorf("skipped events must not be projected, got %d refs", len(sink.refs))
	}
}

// TestSanitizeAssistantContent_StripsFabricatedPrefix pins that a model-imitated [evt_...] prefix is stripped at the storage boundary.
// - A fake key left in storage would poison prefixEventKey skipping and the retained-ref scan downstream.
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

// TestToolTurnProjectsAllSteps 钉住一个工具回合的三步（助手发起调用、工具结果、最终回答）各投影一条、顺序不变。
func TestToolTurnProjectsAllSteps(t *testing.T) {
	p := NewMemoryPlugin(memory.NewInMemoryStore())
	sink := &recordingSink{}
	ctx := WithProjectionSink(context.Background(), sink)

	steps := []*trpcEvent.Event{
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
