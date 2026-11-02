package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// TestSubagentServesSecondRound pins the round-typed continuation of one delegated task:
// - a task re-entered after it settled serves its next round on the assembly its first round ran
// - the second round settles the same task through the same delegation face, so no task of its own is admitted
// - a re-entry arriving inside the running round is refused by name, and the first round settles unchanged
// - a task this tool serves no round of is refused by name
// - a tool carrying no continuation serves one round and settles once, exactly as it did before the continuation existed
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentServesSecondRound(t *testing.T) {
	t.Run("the re-entered task serves its next round", func(t *testing.T) {
		delegation := &stubDelegation{receipt: task.AdmissionReceipt{
			CallID:   "call-review",
			TaskID:   "task-serve-second",
			Accepted: true,
			Kind:     task.AdmissionFresh,
		}}
		provider := &serveModel{name: "model-caller", answer: "review recorded"}
		continuations := NewContinuations()
		tool := newContinuationTool(t, delegation, provider, continuations)
		receipt, ok := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"}).(task.AdmissionReceipt)
		if !ok || receipt.TaskID != "task-serve-second" {
			t.Fatalf("call result = %#v, want the admission receipt of the served task", receipt)
		}
		awaitCompletions(t, delegation, 1)

		if err := continuations.Reenter(context.Background(), receipt.TaskID, "review the change again"); err != nil {
			t.Fatalf("re-enter the served task = %v, want the next round", err)
		}
		awaitCompletions(t, delegation, 2)
		if got := delegation.calls(); got != 1 {
			t.Fatalf("delegations = %d, want the one task the re-entry serves beside", got)
		}
		statuses := delegation.terminals()
		if len(statuses) != 2 || statuses[0] != task.StatusSettled || statuses[1] != task.StatusSettled {
			t.Fatalf("terminals = %v, want the settled outcome of both rounds", statuses)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("delegated requests = %d, want one per round", len(requests))
		}
		if !continuationCarries(requests[1], "review the change again") {
			t.Fatalf("second round request = %+v, want it to carry the re-entry input", requests[1].Messages)
		}
	})

	t.Run("rejects_reentry_in_flight", func(t *testing.T) {
		delegation := &stubDelegation{receipt: task.AdmissionReceipt{
			CallID:   "call-review",
			TaskID:   "task-serve-in-flight",
			Accepted: true,
			Kind:     task.AdmissionFresh,
		}}
		provider := newGatedModel("model-caller")
		continuations := NewContinuations()
		tool := newContinuationTool(t, delegation, provider, continuations)
		if _, ok := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"}).(task.AdmissionReceipt); !ok {
			t.Fatal("call the tool of the held round, want the admission receipt")
		}
		awaitStarted(t, provider)
		if err := continuations.Reenter(context.Background(), "task-serve-in-flight", "review it again"); !errors.Is(err, ErrContinuationBusy) {
			t.Fatalf("re-entry inside the running round = %v, want %v", err, ErrContinuationBusy)
		}
		provider.release()
		awaitCompletions(t, delegation, 1)
	})

	t.Run("rejects_reentry_of_a_task_not_served_here", func(t *testing.T) {
		continuations := NewContinuations()
		if err := continuations.Reenter(context.Background(), "task-elsewhere", "review it"); !errors.Is(err, ErrContinuationMissing) {
			t.Fatalf("re-entry of a task served here by no round = %v, want %v", err, ErrContinuationMissing)
		}
		if continuations.Served("task-elsewhere") {
			t.Fatal("a task no round was served for is reported as served")
		}
	})

	t.Run("first_round_settle_unchanged", func(t *testing.T) {
		delegation := &stubDelegation{receipt: task.AdmissionReceipt{
			CallID:   "call-review",
			TaskID:   "task-serve-one-round",
			Accepted: true,
			Kind:     task.AdmissionFresh,
		}}
		provider := &serveModel{name: "model-caller", answer: "review recorded"}
		tool := newContinuationTool(t, delegation, provider, nil)
		out := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"})
		receipt, ok := out.(task.AdmissionReceipt)
		if !ok || !receipt.Accepted || receipt.Kind != task.AdmissionFresh || receipt.TaskID != "task-serve-one-round" {
			t.Fatalf("call result = %#v, want the admission receipt it answers with beside no continuation", out)
		}
		awaitCompletions(t, delegation, 1)
		if got := delegation.calls(); got != 1 {
			t.Fatalf("delegations = %d, want the one task of the one-round contract", got)
		}
		if statuses := delegation.terminals(); len(statuses) != 1 || statuses[0] != task.StatusSettled {
			t.Fatalf("terminals = %v, want the single settled outcome of the one round", statuses)
		}
		if got := len(provider.recorded()); got != 1 {
			t.Fatalf("delegated requests = %d, want the one request of the one round", got)
		}
		if NewContinuations().Served("task-serve-one-round") {
			t.Fatal("a tool carrying no continuation reports a served task")
		}
	})
}

// newContinuationTool returns the call_subagent tool over the delegation stand-in, the
// model stand-in its builder resolves and the continuation table the test re-enters.
func newContinuationTool(t *testing.T, delegation *stubDelegation, provider trpcmodel.Model, continuations *Continuations) *Tool {
	t.Helper()
	bindings := state.NewBindings()
	holdBinding(t, bindings, event.Address{AgentID: "agent-caller", SessionID: "session-caller"})
	tool, err := NewTool(ToolOptions{
		Parent: &Assembly{Definition: Definition{Description: "the caller", Model: "model-caller"}},
		Builder: Builder{Model: func(context.Context, string) (trpcmodel.Model, error) {
			return provider, nil
		}},
		Delegation:    delegation,
		Bindings:      bindings,
		Continuations: continuations,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// awaitCompletions waits until the delegation stand-in reported the named number of
// terminals: the rounds run beside the admission, so a check waits on the face they
// report through.
func awaitCompletions(t *testing.T, delegation *stubDelegation, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if delegation.completions() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("terminals reported = %d, want the %d the served rounds answer with", delegation.completions(), want)
}

// awaitStarted waits until the held model was driven, so the round it serves is really in
// flight before the re-entry is read.
func awaitStarted(t *testing.T, model *gatedModel) {
	t.Helper()
	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the held round never reached the model it runs on")
	}
}

// continuationCarries reports whether one request carries the named material.
func continuationCarries(request trpcmodel.Request, material string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, material) {
			return true
		}
	}
	return false
}

// gatedModel is the model stand-in a round parks on: every request waits for the gate the
// test releases, so the round being served stays in flight while it is inspected.
type gatedModel struct {
	name    string
	started chan struct{}
	gate    chan struct{}
	once    sync.Once
	free    sync.Once
	mu      sync.Mutex
	driven  []trpcmodel.Request
}

// newGatedModel returns the held model of one round.
func newGatedModel(name string) *gatedModel {
	return &gatedModel{name: name, started: make(chan struct{}), gate: make(chan struct{})}
}

// GenerateContent parks until the gate opens, then streams the scripted final answer.
func (m *gatedModel) GenerateContent(_ context.Context, req *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	m.mu.Lock()
	m.driven = append(m.driven, *req)
	m.mu.Unlock()
	m.once.Do(func() { close(m.started) })
	<-m.gate
	ch := make(chan *trpcmodel.Response, 1)
	ch <- &trpcmodel.Response{
		Done:    true,
		Choices: []trpcmodel.Choice{{Message: trpcmodel.NewAssistantMessage("review recorded")}},
	}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *gatedModel) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// release opens the gate, exactly once.
func (m *gatedModel) release() { m.free.Do(func() { close(m.gate) }) }

// TestSubagentClaimsOneRoundBeforeServing pins the claim a re-entry takes before serving.
// - a claim of a task no round is served for, and a second claim of a taken round, are each refused by name
// - a served round no caller claimed is refused, so no body reaches a round a second caller believes it owns
// - a claim a caller does not serve gives the slot back, so the task stays re-entrant
// - a round that ends without recording its terminal keeps the whole input owed, and the next round carries it ahead of the body it was handed
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentClaimsOneRoundBeforeServing(t *testing.T) {
	t.Run("refuses_a_claim_of_a_task_served_here_by_no_round", func(t *testing.T) {
		if err := NewContinuations().Claim("task-nowhere"); !errors.Is(err, ErrContinuationMissing) {
			t.Fatalf("claim of a task no round is served for = %v, want %v", err, ErrContinuationMissing)
		}
	})

	t.Run("refuses_a_second_claim_of_one_round", func(t *testing.T) {
		delegation, _, continuations, taskID := continuationServedTask(t)
		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim the one round of the served task = %v", err)
		}
		if err := continuations.Claim(taskID); !errors.Is(err, ErrContinuationBusy) {
			t.Fatalf("second claim of the same round = %v, want %v", err, ErrContinuationBusy)
		}
		continuations.Abandon(taskID)
		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim after the slot was given back = %v, want the round it opens", err)
		}
		continuations.Abandon(taskID)
		if got := delegation.calls(); got != 1 {
			t.Fatalf("delegations = %d, want the one task the first round admitted", got)
		}
	})

	t.Run("refuses_a_round_no_caller_claimed", func(t *testing.T) {
		_, _, continuations, taskID := continuationServedTask(t)
		if err := continuations.Serve(context.Background(), taskID, "review it again"); !errors.Is(err, ErrContinuationUnclaimed) {
			t.Fatalf("serve of a round no caller claimed = %v, want %v", err, ErrContinuationUnclaimed)
		}
	})

	t.Run("keeps_the_body_owed_when_the_round_ends_beside", func(t *testing.T) {
		delegation, provider, continuations, taskID := continuationServedTask(t)
		delegation.mu.Lock()
		delegation.terminalErr = errors.New("the ledger refused the terminal of the round")
		delegation.mu.Unlock()

		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim the round of the served task = %v", err)
		}
		if err := continuations.Serve(context.Background(), taskID, "review it again"); err == nil {
			t.Fatal("serve of a round whose terminal the ledger refused reported no cause")
		}

		delegation.mu.Lock()
		delegation.terminalErr = nil
		delegation.mu.Unlock()
		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim the round after the failed one = %v, want the slot given back", err)
		}
		if err := continuations.Serve(context.Background(), taskID, "review the last change"); err != nil {
			t.Fatalf("serve the round after the failed one = %v", err)
		}

		requests := provider.recorded()
		if len(requests) != 3 {
			t.Fatalf("rounds served = %d, want the first round beside the failed one and the round after it", len(requests))
		}
		last := ""
		for _, message := range requests[len(requests)-1].Messages {
			last += message.Content + "\n"
		}
		owed, handed := strings.Index(last, "review it again"), strings.Index(last, "review the last change")
		if owed < 0 {
			t.Fatalf("the round after the failed one does not carry the body left owed: %q", last)
		}
		if handed < 0 || handed < owed {
			t.Fatalf("the round after the failed one does not carry the body it was handed after the owed one: %q", last)
		}
	})
}

// continuationServedTask drives one call through the delegation tool so the continuation
// table serves that task a round, and returns the table beside the model the rounds were
// served against and the task the table serves.
func continuationServedTask(t *testing.T) (*stubDelegation, *serveModel, *Continuations, event.TaskID) {
	t.Helper()
	delegation := &stubDelegation{receipt: task.AdmissionReceipt{
		CallID:   "call-review",
		TaskID:   "task-serve-claimed",
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}}
	provider := &serveModel{name: "model-caller", answer: "review recorded"}
	continuations := NewContinuations()
	tool := newContinuationTool(t, delegation, provider, continuations)
	receipt, ok := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"}).(task.AdmissionReceipt)
	if !ok || receipt.TaskID != "task-serve-claimed" {
		t.Fatalf("call result = %#v, want the admission receipt of the served task", receipt)
	}
	awaitCompletions(t, delegation, 1)
	return delegation, provider, continuations, receipt.TaskID
}
