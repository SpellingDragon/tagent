package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
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
func newContinuationTool(t *testing.T, delegation Delegation, provider trpcmodel.Model, continuations *Continuations) *Tool {
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
func awaitCompletions(t *testing.T, delegation interface{ completions() int }, want int) {
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
		if err := continuations.Serve(context.Background(), taskID, "review it again", nil); !errors.Is(err, ErrContinuationUnclaimed) {
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
		ended := make(chan error, 1)
		if err := continuations.Serve(context.Background(), taskID, "review it again", func(err error) { ended <- err }); err != nil {
			t.Fatalf("serve the round that lands its input = %v, want the receipt of a body its round carries", err)
		}
		select {
		case err := <-ended:
			if err == nil {
				t.Fatal("the round whose terminal the ledger refused reported a settled round")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the round whose terminal the ledger refused never reported how it ended")
		}

		delegation.mu.Lock()
		delegation.terminalErr = nil
		delegation.mu.Unlock()
		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim the round after the failed one = %v, want the slot given back", err)
		}
		if err := continuations.Serve(context.Background(), taskID, "review the last change", nil); err != nil {
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

// TestSubagentAnswersTheReceiptOfItsRound pins what the receipt of a re-entry stands for.
// - negative (receipt_before_the_round_lands): nothing is answered before the round it opened landed its own input
// - positive: the receipt is answered once that input landed, while the round it opened is still running
// - negative (receipt_of_a_round_that_never_landed): a round that never landed its input answers its cause, records no terminal, and keeps the body owed
// - negative (panic_gives_the_round_back): a round that dies beside a panic reports the cause, gives the slot back and keeps the whole input owed
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentAnswersTheReceiptOfItsRound(t *testing.T) {
	t.Run("receipt_waits_for_the_rounds_input", func(t *testing.T) {
		delegation := &besideDelegation{
			stubDelegation: stubDelegation{receipt: task.AdmissionReceipt{
				CallID:   "call-beside",
				TaskID:   "task-serve-beside",
				Accepted: true,
				Kind:     task.AdmissionFresh,
			}},
			mayLand:   make(chan struct{}),
			mayAnswer: make(chan struct{}),
		}
		continuations := NewContinuations()
		tool := newContinuationTool(t, delegation, &serveModel{name: "model-caller", answer: "review recorded"}, continuations)
		if _, ok := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"}).(task.AdmissionReceipt); !ok {
			t.Fatal("call the tool of the served task, want the admission receipt")
		}
		awaitCompletions(t, delegation, 1)

		answered := make(chan error, 1)
		go func() {
			answered <- continuations.Reenter(context.Background(), "task-serve-beside", "review it again")
		}()
		select {
		case err := <-answered:
			t.Fatalf("the re-entry answered %v before the round it opened landed its input", err)
		case <-time.After(100 * time.Millisecond):
		}

		close(delegation.mayLand)
		select {
		case err := <-answered:
			if err != nil {
				t.Fatalf("the re-entry answered %v beside the round whose input landed, want the receipt of a body that arrived", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the re-entry never answered beside the round whose input landed")
		}

		close(delegation.mayAnswer)
		awaitCompletions(t, delegation, 2)
	})

	t.Run("receipt_of_a_round_that_never_landed", func(t *testing.T) {
		cause := errors.New("the site never took the input of the round")
		delegation := &besideDelegation{
			stubDelegation: stubDelegation{receipt: task.AdmissionReceipt{
				CallID:   "call-unlanded",
				TaskID:   "task-serve-unlanded",
				Accepted: true,
				Kind:     task.AdmissionFresh,
			}},
			landErr: cause,
		}
		continuations := NewContinuations()
		tool := newContinuationTool(t, delegation, &serveModel{name: "model-caller", answer: "review recorded"}, continuations)
		if _, ok := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"}).(task.AdmissionReceipt); !ok {
			t.Fatal("call the tool of the served task, want the admission receipt")
		}
		awaitCompletions(t, delegation, 1)

		if err := continuations.Reenter(context.Background(), "task-serve-unlanded", "review it again"); !errors.Is(err, cause) {
			t.Fatalf("re-entry of a round that never landed its input = %v, want the cause the site reported", err)
		}
		if got := delegation.completions(); got != 1 {
			t.Fatalf("terminals recorded = %d, want the first round alone: a round whose input never landed has no terminal to record", got)
		}

		continuationIdle(t, continuations, "task-serve-unlanded")
		delegation.fixLandErr(nil)
		if err := continuations.Reenter(context.Background(), "task-serve-unlanded", "review the last change"); err != nil {
			t.Fatalf("re-entry after the round that never landed = %v, want the round the body is still owed to", err)
		}
		awaitCompletions(t, delegation, 2)
		requests := delegation.roundRequests()
		if len(requests) != 3 {
			t.Fatalf("rounds the stand-in served = %d, want the first round beside the refused one and the round after it", len(requests))
		}
		last := requests[len(requests)-1]
		if !strings.Contains(last, "review it again") || !strings.Contains(last, "review the last change") {
			t.Fatalf("the round after the refused one was handed %q, want the body that never landed ahead of the body it carried", last)
		}
	})

	t.Run("panic_gives_the_round_back", func(t *testing.T) {
		_, _, continuations, taskID := continuationServedTask(t)
		continuations.mu.Lock()
		entry := continuations.served[taskID]
		entry.faces.delegation = &panicDelegation{}
		continuations.mu.Unlock()

		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim the round of the served task = %v", err)
		}
		ended := make(chan error, 1)
		err := continuations.Serve(context.Background(), taskID, "review it again", func(err error) { ended <- err })
		if err == nil || !strings.Contains(err.Error(), "panic") {
			t.Fatalf("serve of a round that died = %v, want the cause the panic left", err)
		}
		select {
		case err := <-ended:
			if err == nil {
				t.Fatal("the round that died beside a panic reported a settled round")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the round that died beside a panic never reported how it ended")
		}

		continuations.mu.Lock()
		running, owed := entry.running, append([]string(nil), entry.owed...)
		continuations.mu.Unlock()
		if running {
			t.Fatal("the round that died beside a panic still holds the slot of its task")
		}
		if len(owed) != 1 || !strings.Contains(owed[0], "review it again") {
			t.Fatalf("input left owed = %v, want the body the died round was handed", owed)
		}
		if err := continuations.Claim(taskID); err != nil {
			t.Fatalf("claim after the round that died beside a panic = %v, want the slot given back", err)
		}
		continuations.Abandon(taskID)
	})
}

// besideDelegation is the delegation stand-in whose round lands its input on the anchor's word:
// the round parks before the commit and again before its answer, so the receipt of a re-entry is
// read at both ends of the one fact it stands for. A round fixed with a landErr never lands its
// input at all.
type besideDelegation struct {
	stubDelegation
	mayLand   chan struct{}
	mayAnswer chan struct{}
	landErr   error
}

// SubmitBeside records the round it is handed, parks on the anchor's word, reports the commit
// of the round and answers the terminal the round reached.
func (s *besideDelegation) SubmitBeside(_ context.Context, in agent.DelegatedInput, started func(error)) (agent.DelegatedAnswer, error) {
	s.mu.Lock()
	s.rounds = append(s.rounds, in)
	landErr := s.landErr
	s.mu.Unlock()
	if s.mayLand != nil {
		<-s.mayLand
	}
	if landErr != nil {
		started(landErr)
		return agent.DelegatedAnswer{Status: task.StatusFailed, Failure: landErr.Error()}, landErr
	}
	started(nil)
	if s.mayAnswer != nil {
		<-s.mayAnswer
	}
	return agent.DelegatedAnswer{Status: task.StatusSettled, Body: "review recorded"}, nil
}

// fixLandErr fixes whether the rounds of this stand-in land their input.
func (s *besideDelegation) fixLandErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landErr = err
}

// roundRequests reports the input each round was handed, in round order.
func (s *besideDelegation) roundRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.rounds))
	for _, round := range s.rounds {
		out = append(out, round.Request)
	}
	return out
}

// panicDelegation is the delegation stand-in whose round dies inside the pipeline it runs on:
// the panic is what an instance failing in the middle of a turn raises, and the continuation
// table is what has to survive it.
type panicDelegation struct{ stubDelegation }

// Submit panics, so the round it would have decided leaves nothing behind.
func (s *panicDelegation) Submit(context.Context, agent.DelegatedInput) (agent.DelegatedAnswer, error) {
	panic("the delegated round died")
}

// continuationIdle waits until the one round of a served task left its slot, so the next
// re-entry is read against a table that already gave the round back.
func continuationIdle(t *testing.T, continuations *Continuations, taskID event.TaskID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		continuations.mu.Lock()
		entry := continuations.served[taskID]
		idle := entry == nil || !entry.running
		continuations.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the round of %s never gave its slot back", taskID)
}
