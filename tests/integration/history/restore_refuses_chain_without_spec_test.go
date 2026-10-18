package history

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// passPolicy is the deterministic stand-in of the real governance policy the
// formal assembly is configured with in these checks: the fixed passing
// dispositions are mechanism inputs carrying no quality conclusion, and the
// formal quality assessment lives in the structural evaluation gate.
type passPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (passPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       passingCheck("eval"),
		"benchmark":  passingCheck("benchmark"),
		"apply-gate": passingCheck("apply-gate"),
	}, nil
}

// passingCheck reports the passing disposition of one named mechanism check.
func passingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// TestRestoreRefusesChainWithoutSpec drives the durable chain only through the formal public entries and checks the admission-material contract of the chain.
//   - the chain the formal register lands is self-contained and a restart rebuilds the task beside the material it carries
//   - a chain whose links carry no admission material is refused by name and reports no recovery
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestRestoreRefusesChainWithoutSpec(t *testing.T) {
	t.Run("the formal register lands a self-contained chain a restart restores", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := memory.StorageConfig{Type: memory.StorageLocalFile, Root: t.TempDir()}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-chain"}
		provider := &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return chainToolTurn(chainCall("call-1", "probe", `{"x":1}`))
			}
			return chainFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, chainDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble the durable runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-chain",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); err != nil {
			t.Fatalf("invoke the durable runtime: %v", err)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		first, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the admitted task: %v", err)
		}
		if len(first.Details) != 1 {
			t.Fatalf("admitted tasks = %d, want the one call the turn admitted", len(first.Details))
		}
		admitted := first.Details[0].Ref
		if admitted.TaskID == "" || admitted.InstanceID == "" {
			t.Fatalf("admitted task ref = %+v, want the task beside its instance identity", admitted)
		}

		restarted, err := tagent.Assemble(ctx, chainDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithStorage(cfg))
		if err != nil {
			t.Fatalf("restart the durable runtime: %v", err)
		}
		report := restarted.Recovery()
		if len(report.Tasks) != 1 {
			t.Fatalf("recovery outcomes = %+v, want the one task the chain covers", report.Tasks)
		}
		outcome := report.Tasks[0]
		if outcome.TaskID != admitted.TaskID || !outcome.Recovered || outcome.Refused != nil {
			t.Fatalf("recovery outcome = %+v, want %s rebuilt without a refusal", outcome, admitted.TaskID)
		}
		if outcome.Owner != "agent-a" {
			t.Fatalf("recovery owner = %q, want the task restored at its own owner", outcome.Owner)
		}
		if outcome.Status != task.StatusSettled {
			t.Fatalf("restored status = %v, want the settled status the chain folds to", outcome.Status)
		}
		second, err := restarted.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the restored task: %v", err)
		}
		if len(second.Details) != 1 {
			t.Fatalf("restored tasks = %d, want the rebuilt task visible to the diagnostic read", len(second.Details))
		}
		restored := second.Details[0].Ref
		if restored.TaskID != admitted.TaskID || restored.InstanceID != admitted.InstanceID || restored.Address != admitted.Address {
			t.Fatalf("restored task ref = %+v, want the admission material of %+v restored as it was admitted", restored, admitted)
		}
	})

	t.Run("a chain carrying no admission material is refused by name and reports no recovery", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := memory.StorageConfig{Type: memory.StorageLocalFile, Root: t.TempDir()}
		tier, err := memory.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the fixture tier: %v", err)
		}
		fixture, err := state.NewDurableCommit(memory.TierFactLog(tier), nil)
		if err != nil {
			t.Fatalf("build the fixture chain: %v", err)
		}
		if result, err := fixture.Commit(ctx, task.CommitRecord{
			MutationID:       "admit:task-damaged",
			TaskID:           "task-damaged",
			ExpectedRevision: 0,
			Status:           task.StatusAccepted,
		}); err != nil || result != task.CommitCommitted {
			t.Fatalf("commit the fixture fact = %v, %v, want committed", result, err)
		}

		run, err := tagent.Assemble(ctx, chainDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble beside the damaged chain: %v", err)
		}
		report := run.Recovery()
		if len(report.Tasks) != 1 {
			t.Fatalf("recovery outcomes = %+v, want one outcome for the damaged chain", report.Tasks)
		}
		outcome := report.Tasks[0]
		if outcome.TaskID != "task-damaged" {
			t.Fatalf("recovery outcome = %+v, want the damaged task named", outcome)
		}
		if outcome.Recovered {
			t.Fatalf("recovery outcome = %+v, want no recovery reported for a chain carrying no admission material", outcome)
		}
		if !errors.Is(outcome.Refused, task.ErrSpecMissing) {
			t.Fatalf("recovery refusal = %v, want the named ErrSpecMissing refusal", outcome.Refused)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{{AgentID: "agent-a", SessionID: "session-damaged"}}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the damaged scene: %v", err)
		}
		if len(reading.Details) != 0 {
			t.Fatalf("task details = %+v, want nothing rebuilt from a chain carrying no admission material", reading.Details)
		}
	})
}

// chainDeclaration declares the single agent these checks assemble.
func chainDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
}

// chainModel is the scripted native model stand-in of these checks: it answers
// with the scripted response of its round and never reaches a real endpoint.
type chainModel struct {
	mu     sync.Mutex
	name   string
	answer func(req *model.Request) *model.Response
}

// GenerateContent streams the scripted answer of the round.
func (m *chainModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *chainModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// chainTool is the named native tool stand-in: it answers with its output.
type chainTool struct {
	name   string
	output string
}

// Declaration returns the declaration naming the tool.
func (t *chainTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the tool once and reports its output.
func (t *chainTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return t.output, nil
}

// chainCall builds one native tool call of a scripted assistant message.
func chainCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// chainToolTurn builds the scripted response carrying one native tool call.
func chainToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// chainFinalTurn builds the scripted response carrying the final answer.
func chainFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}
