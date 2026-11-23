package history

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
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
//   - the chain the formal register lands is self-contained: a restart reads the settled task back by identity, and the recovery report states nothing of it
//   - a chain whose links carry no admission material is refused by name: recovery reports no rebuild and the diagnostic read refuses the scene by name
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestRestoreRefusesChainWithoutSpec(t *testing.T) {
	t.Run("the formal register lands a self-contained chain a restart restores", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-chain"}
		provider := &chainModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return chainToolTurn(chainCall("call-1", "probe", `{"x":1}`))
			}
			return chainFinalTurn("settled")
		}}
		run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble the durable runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
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
		if first.Details[0].State.TaskStatus != task.StatusSettled {
			t.Fatalf("the admitted task stands at %v, want the settled state the turn commits", first.Details[0].State.TaskStatus)
		}

		if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
			t.Fatalf("close the first runtime: %v; the restart opens the same root, so the first runtime ends first, since the write rights of a view that is still open refuse every later writer", err)
		}
		restarted, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
		if err != nil {
			t.Fatalf("restart the durable runtime: %v", err)
		}
		report := restarted.Recovery()
		if len(report.Tasks) != 0 {
			t.Fatalf("recovery outcomes = %+v, want nothing stated of the task settled before the cut", report.Tasks)
		}
		second, err := restarted.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the restored task: %v", err)
		}
		if len(second.Details) != 1 {
			t.Fatalf("restored tasks = %d, want the settled task read back by identity through the diagnostic read", len(second.Details))
		}
		restored := second.Details[0].Ref
		if restored.TaskID != admitted.TaskID || restored.InstanceID != admitted.InstanceID || restored.Address != admitted.Address {
			t.Fatalf("restored task ref = %+v, want the admission material of %+v restored as it was admitted", restored, admitted)
		}
		if second.Details[0].State.TaskStatus != task.StatusSettled {
			t.Fatalf("restored status = %v, want the settled status the chain reads back", second.Details[0].State.TaskStatus)
		}
	})

	t.Run("a chain carrying no admission material is refused by name and reports no recovery", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the fixture tier: %v", err)
		}
		fixture := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
		if err := fixture.Seat(ctx, nil, nil); err != nil {
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
		if err := tier.Close(); err != nil {
			t.Fatalf("close the fixture writer: %v; the assembly opens the same root, so the fixture writer ends first, since one root carries one writer", err)
		}

		run, err := assembly.Assemble(ctx, chainDeclaration(), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: &chainModel{name: "model-a", answer: func(*model.Request) *model.Response {
				return chainFinalTurn("settled")
			}}, Tools: []trpctool.CallableTool{&chainTool{name: "probe", output: "out-a"}}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(cfg))
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
		if err == nil {
			t.Fatalf("query the damaged scene answered %+v, want the named refusal of a chain carrying no admission material", reading.Details)
		}
		if !errors.Is(err, task.ErrSpecMissing) || !errors.Is(err, query.ErrSourceUnreadable) {
			t.Fatalf("query refusal = %v, want the named ErrSpecMissing refusal under the unreadable-source class", err)
		}
		dims := refusedDimensions(err)
		for _, dim := range []query.ObjectKind{query.ObjectInvocation, query.ObjectTask, query.ObjectNotice} {
			if !slices.Contains(dims, dim) {
				t.Fatalf("refused dimensions = %v, want %s named beside the dimensions the task reading feeds", dims, dim)
			}
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

// refusedDimensions collects the dimension every SourceError of one refusal names, walking the joined causes of a multi-dimension refusal.
func refusedDimensions(err error) []query.ObjectKind {
	var dims []query.ObjectKind
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if source, ok := e.(*query.SourceError); ok {
			dims = append(dims, source.Dimension)
		}
		switch joined := e.(type) {
		case interface{ Unwrap() []error }:
			for _, part := range joined.Unwrap() {
				walk(part)
			}
		case interface{ Unwrap() error }:
			walk(joined.Unwrap())
		}
	}
	walk(err)
	return dims
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
