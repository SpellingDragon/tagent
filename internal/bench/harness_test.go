// Package bench measures the published runtime over its public entry only: one
// formal assembly, one invocation turn and one durable restore, each driven by a
// mock model that reaches no endpoint. The numbers are baselines of this module
// on one machine, not service-level objectives.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package bench

import (
	"context"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// benchInput is the one user input a turn is driven with.
const benchInput = "hello"

// benchAgent is the single declared agent every benchmark assembles.
const benchAgent = event.AgentID("agent-a")

// benchmarkSessions is the number of session addresses a benchmark rotates
// through when it reuses one address.
const benchmarkSessions = 8

// scriptedModel answers the first decision of a turn with one tool call and the
// next decision with a final answer, so one turn admits exactly one native call.
type scriptedModel struct {
	name string
}

// GenerateContent streams the scripted answer for the request it is driven with:
// the benchmark input admits the call and every other request is answered finally.
func (m *scriptedModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	if last := req.Messages[len(req.Messages)-1]; last.Role == model.RoleUser && last.Content == benchInput {
		ch <- &model.Response{
			Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: model.FunctionDefinitionParam{
						Name:      "echo",
						Arguments: []byte(`{"probe":1}`),
					},
				}},
			}}},
			Done: true,
		}
	} else {
		ch <- &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}
	close(ch)
	return ch, nil
}

// Info reports the scripted model descriptor.
func (m *scriptedModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// echoTool is the one native call every timed turn executes exactly once.
type echoTool struct {
	name   string
	output string
}

// Declaration returns the declaration naming the echo tool.
func (t *echoTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call answers the call with the fixed output of the tool.
func (t *echoTool) Call(context.Context, []byte) (any, error) {
	return t.output, nil
}

// passGovernance answers one passing disposition per required publication check.
type passGovernance struct{}

// Check reports the passing dispositions the formal publication requires.
func (passGovernance) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	answer := func(name state.CheckName) state.CheckAnswer {
		return state.CheckAnswer{
			Name:     name,
			Verdict:  state.CheckPass,
			Reason:   "benchmark stand-in",
			Scope:    "assembly",
			Evidence: []string{"deterministic stand-in"},
		}
	}
	return map[state.CheckName]state.CheckAnswer{
		"eval":       answer("eval"),
		"benchmark":  answer("benchmark"),
		"apply-gate": answer("apply-gate"),
	}, nil
}

// benchDeclaration is the one-agent declaration every benchmark publishes.
func benchDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry:  string(benchAgent),
		Agents: []config.AgentSpec{{ID: string(benchAgent), Model: "model-a"}},
	}
}

// benchAgents is the native agent set the declaration resolves against.
func benchAgents() map[event.AgentID]assembly.NativeAgent {
	return map[event.AgentID]assembly.NativeAgent{
		benchAgent: {
			Model: &scriptedModel{name: "model-a"},
			Tools: []trpctool.CallableTool{&echoTool{name: "echo", output: "out-a"}},
		},
	}
}

// assemble builds one formal assembly over the benchmark declaration.
func assemble(b *testing.B, opts ...assembly.Option) *assembly.Run {
	b.Helper()
	opts = append([]assembly.Option{assembly.WithGovernance(passGovernance{})}, opts...)
	run, err := assembly.Assemble(context.Background(), benchDeclaration(), benchAgents(), opts...)
	if err != nil {
		b.Fatalf("assemble the benchmark runtime: %v", err)
	}
	return run
}

// drive runs one invocation turn on the named session and checks its terminal.
func drive(b *testing.B, run *assembly.Run, sequence int, session event.SessionID) {
	b.Helper()
	turn, err := run.Invoke(context.Background(), assembly.InvokeRequest{
		InvocationID: event.InvocationID(fmt.Sprintf("inv-%d", sequence)),
		Address:      event.Address{AgentID: benchAgent, SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: benchInput}},
	})
	if err != nil {
		b.Fatalf("invoke the benchmark runtime: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		b.Fatalf("turn terminal = %v, want one completed turn", turn.Terminal)
	}
}

// rotatingSession names the session of one turn in a bounded rotation.
func rotatingSession(sequence int) event.SessionID {
	return event.SessionID(fmt.Sprintf("session-%d", sequence%benchmarkSessions))
}

// uniqueSession names a session no other invocation shares.
func uniqueSession(sequence int) event.SessionID {
	return event.SessionID(fmt.Sprintf("session-%d", sequence))
}
