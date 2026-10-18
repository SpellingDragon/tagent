package acceptance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

// scenarioRecorder records the tool results committed through the task wrapper
// and reports one fact identity per recorded result.
// 契约: docs/wiki/agent/agent-architecture.md#test-support
type scenarioRecorder struct {
	mu       sync.Mutex
	recorded []task.Result
	facts    []event.EventID
}

// record stores one result beside the identity of the fact it recorded.
func (r *scenarioRecorder) record(ctx context.Context, result task.Result) (event.EventID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, result)
	id := event.EventID(fmt.Sprintf("fact-%d", len(r.facts)+1))
	r.facts = append(r.facts, id)
	return id, nil
}

// results reports the results recorded so far.
func (r *scenarioRecorder) results() []task.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]task.Result(nil), r.recorded...)
}

// factID reports the fact identity of the recorded result at one position.
func (r *scenarioRecorder) factID(at int) event.EventID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at < 0 || at >= len(r.facts) {
		return ""
	}
	return r.facts[at]
}

// scenarioMailbox records the notifications delivered to the target site.
type scenarioMailbox struct {
	mu      sync.Mutex
	notes   []task.Notice
	arrived chan task.Notice
}

// accept stores one notification and signals its arrival.
func (m *scenarioMailbox) accept(ctx context.Context, notice task.Notice) error {
	m.mu.Lock()
	m.notes = append(m.notes, notice)
	m.mu.Unlock()
	if m.arrived != nil {
		select {
		case m.arrived <- notice:
		default:
		}
	}
	return nil
}

// snapshot reports the notifications accepted so far.
func (m *scenarioMailbox) snapshot() []task.Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]task.Notice(nil), m.notes...)
}

// scenarioTaskRig is the task face one ownership drive runs over: the registry a
// task is looked up in and the manager its obligations are held by.
type scenarioTaskRig struct {
	registry *task.MemoryRegistry
	manager  *task.MemoryManager
}

// newScenarioTaskRig reports the task face the ownership drives admit through.
func newScenarioTaskRig() scenarioTaskRig {
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	return scenarioTaskRig{registry: registry, manager: task.NewMemoryManager(registry, nil)}
}

// scenarioFrame reports the call frame one wrapped call is admitted under.
func scenarioFrame(invocation, callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-scenario",
			InvocationID: event.InvocationID(invocation),
			DecisionID:   event.DecisionID("decision-" + callID),
			CallID:       callID,
		},
		Address:   event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Execution: "serial",
	}
}

// awaitIdle waits until the task holds no open obligation, so a settlement the
// drive observed is the settlement the manager committed.
func awaitIdle(t *testing.T, manager *task.MemoryManager, taskID event.TaskID) {
	t.Helper()
	deadline := time.Now().Add(scenarioTimeout)
	for time.Now().Before(deadline) {
		held, err := manager.Obligations(context.Background(), taskID)
		if err == nil && held.Count == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s kept its obligations open", taskID)
}
