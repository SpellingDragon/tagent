// 契约: docs/wiki/agent/event-flow.md#runner-flow
package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// closeResidentsSession is the session the delegated round of the close check
// runs in, so its resident loop is one the run mounted for itself.
const closeResidentsSession = "session-close-residents"

// closeResidentsAdmit answers the admission receipt of the call it is handed and
// no result: the segment that issued it parks awaiting a settled result this
// check never lands, so the loop of that segment stands between turns while the
// close reads its criterion.
type closeResidentsAdmit struct {
	taskID   event.TaskID
	admitted chan struct{}
}

// Declaration returns the declaration naming the admitting tool.
func (a *closeResidentsAdmit) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "admit-close"}
}

// Call answers the admission receipt of the call it is handed.
func (a *closeResidentsAdmit) Call(ctx context.Context, _ []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, task.ErrCallFrameMissing
	}
	select {
	case a.admitted <- struct{}{}:
	default:
	}
	return task.AdmissionReceipt{
		CallID:   frame.Identity.CallID,
		TaskID:   a.taskID,
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}, nil
}

// TestCloseStopsTheResidentsTheRunMounted pins the stop the close of a run owes the resident loops it mounted:
// - negative: the close never returns beside a segment a resident loop still flies, because a run at rest over a segment that lands afterwards is refused
// - positive: the stop reaches the segment the loop left flying, so the delegated round waiting on it answers the cancelled terminal instead of hanging
// - the run reports the drained it really reached, and no turn lands on the chain of the site after that answer
// - a restart reading that chain reads the input it still owes and no turn the close wrote behind its own answer
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestCloseStopsTheResidentsTheRunMounted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admit := &closeResidentsAdmit{taskID: "task-close-residents", admitted: make(chan struct{}, 4)}
	round := 0
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		if round == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-close-residents",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      "admit-close",
							Arguments: []byte(`{}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the worker answered"}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{admit}},
	}
	run, err := Assemble(ctx, decl, natives, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	site := event.Address{AgentID: "agent-a", SessionID: closeResidentsSession}
	if err := run.establish(ctx, site); err != nil {
		t.Fatalf("establish the site of the delegated round: %v", err)
	}

	submitted := make(chan agent.DelegatedAnswer, 1)
	go func() {
		answer, submitErr := run.delegations.Submit(ctx, agent.DelegatedInput{
			Site:        site,
			Binding:     state.AgentBinding{Model: provider, Tools: []trpctool.Tool{admit}, PromptSource: state.PromptSource{Version: "v1"}},
			Instruction: "run the delegated round",
		})
		if submitErr != nil {
			submitted <- agent.DelegatedAnswer{Failure: submitErr.Error()}
			return
		}
		submitted <- answer
	}()
	select {
	case <-admit.admitted:
	case <-time.After(30 * time.Second):
		t.Fatal("the delegated round never reached its admission, want the resident loop to drive it to the call it parks on")
	}

	report, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("close the run beside the resident loop it mounted: %v", err)
	}
	if !report.Drained {
		t.Fatalf("the close of the run reported unsettled %+v, want the drained run it really reached", report.Unsettled)
	}
	select {
	case answer := <-submitted:
		if answer.Status != task.StatusCancelled {
			t.Fatalf("the delegated round beside the close = (%+v, %q), want the cancelled terminal: its segment is closed by the stop, not left flying",
				answer, answer.Failure)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the close returned beside a resident loop it never stopped: the delegated round still waits on a segment nobody closes")
	}

	settled := run.SessionFacts(site)
	time.Sleep(200 * time.Millisecond)
	if after := run.SessionFacts(site); len(after) != len(settled) {
		t.Fatalf("facts of %s beside the close = %d, want the %d that stood when the close answered: a segment wrote behind its own answer",
			site.SessionID, len(after), len(settled))
	}
	for _, fe := range settled {
		if fe.Type == event.TypeAgentOutput {
			t.Fatalf("agent_output facts of %s beside the close = %+v, want none: the abandoned segment committed no turn",
				site.SessionID, fe)
		}
	}
}
