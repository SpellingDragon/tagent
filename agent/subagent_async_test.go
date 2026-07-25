package agent

import (
	"context"
	"encoding/json"
	"github.com/SpellingDragon/tagent/agent/task"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// progAgent is a programmable agent.Agent whose Run emits a single final-output
// event after a configurable delay — used to exercise sub-agent async spawning.
type progAgent struct {
	name   string
	delay  time.Duration
	output string
}

func (m *progAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	go func() {
		defer close(ch)
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return
		}
		ch <- &event.Event{Response: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: m.output}}},
		}}
	}()
	return ch, nil
}

func (m *progAgent) Tools() []trpctool.Tool          { return nil }
func (m *progAgent) Info() agent.Info                { return agent.Info{Name: m.name, Description: "prog"} }
func (m *progAgent) SubAgents() []agent.Agent        { return nil }
func (m *progAgent) FindSubAgent(string) agent.Agent { return nil }

func subagentCallArgs(t *testing.T) []byte {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"request": "do the thing"})
	return b
}

// TestSubagentAsync_FastInline: a sub-agent that finishes within the sync-wait
// window returns its output inline (equivalent to synchronous behavior).
func TestSubagentAsync_FastInline(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "knowledge", delay: 20 * time.Millisecond, output: "FAST_RESULT"}, "t", nil, nil)
	w.SetAsyncDenseDuration(500 * time.Millisecond)
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	out, err := w.Call(ctx, subagentCallArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if out.(string) != "FAST_RESULT" {
		t.Errorf("expected inline result, got %q", out)
	}
}

// TestSubagentAsync_SlowAck: a sub-agent that exceeds the sync-wait window
// returns an ack (background-tracked).
func TestSubagentAsync_SlowAck(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 300 * time.Millisecond, output: "LATE"}, "t", nil, nil)
	w.SetAsyncDenseDuration(40 * time.Millisecond)
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	out, err := w.Call(ctx, subagentCallArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "后台运行") {
		t.Errorf("expected background ack, got %q", out)
	}
}

// TestSubagentAsync_NoSpawnerSync: without a spawner in context, the sub-agent
// runs synchronously (returns the result), preserving prior behavior.
func TestSubagentAsync_NoSpawnerSync(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "knowledge", delay: 20 * time.Millisecond, output: "SYNC_RESULT"}, "t", nil, nil)

	out, err := w.Call(context.Background(), subagentCallArgs(t)) // no spawner
	if err != nil {
		t.Fatal(err)
	}
	if out.(string) != "SYNC_RESULT" {
		t.Errorf("expected sync result, got %q", out)
	}
}

// TestSubagentAsync_DisabledSync: with async disabled, the sub-agent runs
// synchronously even when a spawner is present.
func TestSubagentAsync_DisabledSync(t *testing.T) {
	w := NewAgentToolWrapper(&progAgent{name: "plan", delay: 20 * time.Millisecond, output: "DISABLED_SYNC"}, "t", nil, nil)
	w.SetAsyncDisabled(true)
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), tm)

	out, err := w.Call(ctx, subagentCallArgs(t))
	if err != nil {
		t.Fatal(err)
	}
	if out.(string) != "DISABLED_SYNC" {
		t.Errorf("expected sync result with async disabled, got %q", out)
	}
}
