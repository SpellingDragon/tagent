package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegatedTaskCommitFailureLeavesNoSegment is the failure anchor of the
// delegated input batch: the instruction and the materials of a round commit
// ahead of the task, and a task commit the ledger refuses must leave the round
// unstarted — the facts that already landed are inert material that drives
// nothing, in this process and after a restart.
//
//   - the caller reads the named cause of the refusal beside the failed terminal
//   - the chain holds the instruction and the material facts and no task fact
//   - no segment of the site was driven: no model request was made and the
//     resident instance stands at rest
//   - a restarted loop over the same chain drives nothing either: the batch is
//     read only through the task fact that closes it
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedTaskCommitFailureLeavesNoSegment(t *testing.T) {
	refused := errors.New("agent: the ledger refused the task fact")
	site := event.Address{AgentID: "agent-reviewer", SessionID: "session-caller#sub:call-review"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-batch", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	ledger := &batchCommitFace{history: history, refuse: delegatedPartTask, cause: refused}
	loop.SetFactCommit(ledger.commit)

	script := newInvariantLoopModel("the round must not run", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}
	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
		Site:        site,
		Binding:     binding,
		Instruction: "record every finding beside the file it names",
		Request:     "the task whose commit the ledger refuses",
		Materials: []event.Event{{
			Key:     "ev-source",
			Type:    event.TypeExternalInput,
			Message: model.Message{Role: model.RoleUser, Content: "the material the batch projects"},
		}},
	})
	if err == nil {
		t.Fatalf("the refused task commit answered no error: the round ran beside a batch that never closed (answer=%+v)", answer)
	}
	if !errors.Is(err, refused) {
		t.Fatalf("the refused task commit answered %v, want the named cause %v", err, refused)
	}
	if answer.Status != task.StatusFailed || !strings.Contains(answer.Failure, refused.Error()) {
		t.Fatalf("the refused round = (%q, %q), want the failed terminal naming %q", answer.Failure, answer.Status, refused.Error())
	}

	facts := history.SessionFacts(site)
	parts := map[string]int{}
	for _, fact := range facts {
		parts[fact.Meta[delegatedPartMeta]]++
	}
	if parts[delegatedPartInstruction] != 1 || parts[delegatedPartMaterial] != 1 || parts[delegatedPartTask] != 0 {
		t.Fatalf("facts of the refused batch = %v, want the instruction and the material landed and no task: %+v", parts, facts)
	}

	if requests := script.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d beside a batch whose task never landed, want no segment driven: %+v", len(requests), requests)
	}
	resident := consumers.Ensure(site)
	if resident == nil {
		t.Fatalf("the resident instance of %s = nil, want the mounted instance the round would run on", site.SessionID)
	}
	invariantAwait(t, resident.Resting)

	// The restart half: a fresh loop over the very same chain mounts an instance,
	// reads the orphan facts and drives no segment — the batch is read through the
	// task fact that closes it, and that fact never landed.
	restarted := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-batch-restart", nil)
	restarted.SetHistory(history)
	restartedScript := newInvariantLoopModel("the restarted round must not run", nil)
	restartedBinding := state.AgentBinding{Model: restartedScript, PromptSource: state.PromptSource{Version: "v1"}}
	restartedScripts := NewConsumers(restarted, context.Background(), nil)
	restarted.SetConsumers(restartedScripts)
	t.Cleanup(restartedScripts.Stop)
	mounted := make(chan struct{}, 4)
	restartedScripts.SetProgress(func() {
		select {
		case mounted <- struct{}{}:
		default:
		}
	})
	instance := restartedScripts.mount(site, &restartedBinding)
	if instance == nil {
		t.Fatalf("the restarted loop mounted no instance for %s", site.SessionID)
	}
	instance.Wake()
	select {
	case <-mounted:
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted loop never finished its first read of the chain")
	}
	if requests := restartedScript.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d after the restart, want the orphan facts to drive nothing: %+v", len(requests), requests)
	}
}

// batchCommitFace is the commit face of one delegated batch that refuses the
// named part: every other fact lands into the chain face the way the run's
// ledger lands it, and the part it was fixed to refuse answers the named cause
// beside no record.
type batchCommitFace struct {
	mu      sync.Mutex
	history *invariantLoopHistory
	refuse  string
	cause   error
	seq     int
}

// commit keeps one fact of the batch, or answers the refusal the face was fixed
// with when the fact states the refused part.
func (f *batchCommitFace) commit(evt event.Event) (event.EventID, error) {
	if evt.Meta[delegatedPartMeta] == f.refuse {
		return "", f.cause
	}
	f.mu.Lock()
	if evt.Key == "" {
		f.seq++
		evt.Key = event.EventID(fmt.Sprintf("minted-batch-%d", f.seq))
	}
	f.mu.Unlock()
	f.history.land(evt)
	return evt.Key, nil
}
