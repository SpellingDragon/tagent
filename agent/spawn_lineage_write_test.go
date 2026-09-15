package agent

import (
	"context"
	"sync"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

// ctxCaptureModel records the ctx handed to GenerateContent so tests can
// assert on context-injected values (e.g. the task spawner lineage).
type ctxCaptureModel struct {
	mu   sync.Mutex
	ctx  context.Context
	info model.Info
}

func (m *ctxCaptureModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{ID: "t", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}
	close(ch)
	return ch, nil
}

func (m *ctxCaptureModel) Info() model.Info { return m.info }

func (m *ctxCaptureModel) captured() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ctx
}

// TestRunFlow_SpawnCarriesTriggerSourceLineage: write-side lineage — RunFlow
// must inject the current turn's trigger_source into the OriginSpawner so
// tasks spawned during a meditation turn carry meditation lineage on their
// settle events (fixes the leak where settle triggers resolved as bare
// "task" and their outputs leaked to lastActiveChat).
func TestRunFlow_SpawnCarriesTriggerSourceLineage(t *testing.T) {
	m := &ctxCaptureModel{info: model.Info{Name: "mock"}}
	cm := newTestContextManager("lineage-cm", m, nil, make(chan *event.Event, 16), NewEventBus())
	cm.taskController = task.NewTaskManager(task.TaskManagerConfig{})
	cm.triggerSource = "meditation"

	if err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}); err != nil {
		t.Fatalf("RunFlow: %v", err)
	}

	sp, ok := task.TaskSpawnerFromContext(m.captured())
	if !ok {
		t.Fatal("no TaskSpawner in RunFlow ctx")
	}
	os, ok := sp.(*task.OriginSpawner)
	if !ok {
		t.Fatalf("spawner = %T, want *task.OriginSpawner", sp)
	}
	if got := os.Origin[tagentevent.MetaKeyTriggerSource]; got != "meditation" {
		t.Fatalf("Origin[trigger_source] = %q, want %q", got, "meditation")
	}
}

// Control: an empty trigger source keeps the bare TaskController spawner
// (zero-behavior change for turns without a resolved trigger source).
func TestRunFlow_SpawnBareControllerWithoutTriggerSource(t *testing.T) {
	m := &ctxCaptureModel{info: model.Info{Name: "mock"}}
	cm := newTestContextManager("bare-cm", m, nil, make(chan *event.Event, 16), NewEventBus())
	cm.taskController = task.NewTaskManager(task.TaskManagerConfig{})
	cm.triggerSource = ""

	if err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}); err != nil {
		t.Fatalf("RunFlow: %v", err)
	}

	sp, ok := task.TaskSpawnerFromContext(m.captured())
	if !ok {
		t.Fatal("no TaskSpawner in RunFlow ctx")
	}
	if _, isOrigin := sp.(*task.OriginSpawner); isOrigin {
		t.Fatal("empty trigger source must keep the bare TaskController spawner")
	}
}
