// The projection-assembly checks of the host: the one assembler the request
// boundary builds its prefix through serves the three uses — the main context,
// the environment block and the delegated refs — under the same budget
// assembly, and an agent that declares no environment block grows no message.
package host

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// assemblyBudget is the token budget the three uses are held to: the full text
// of the material below stays out of the assembler's limit of four characters
// per token, while the ticket of the same text fits it.
const assemblyBudget = 120

// environmentBlockOpen and environmentBlockClose are the markers the dynamic
// environment block is rendered under, so a request states its block by the
// markers alone.
const (
	environmentBlockOpen  = "<environment>"
	environmentBlockClose = "</environment>"
)

// assemblyFullText is the material the three uses carry: past the budget, so
// every use degrades it the same way instead of presenting it in full.
var assemblyFullText = strings.Repeat("a", 500)

// assemblyPreview is the leading text a ticket carries: the rune bound of the
// budget assembly, marked at the cut.
var assemblyPreview = strings.Repeat("a", 128) + "…"

// TestProjectionAssemblyServesTheThreeUses checks the one assembler behind the three uses.
//   - the main context degrades to a ticket under the registration budget
//   - the environment block stays whole beside a budget that skips the projection
//   - the delegated refs degrade to the ticket of the same preview under the same budget
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionAssemblyServesTheThreeUses(t *testing.T) {
	t.Run("main context degrades under the assembly budget", func(t *testing.T) {
		provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
			return entryFinalTurn("回答第1轮")
		}}
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{{
				ID:               "agent-a",
				Model:            "model-a",
				MaxTokens:        assemblyBudget,
				EnvironmentBlock: true,
			}},
		}
		run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-assembly-budget"}
		invokeTurn(t, run, "inv-assembly-1", addr, assemblyFullText)
		invokeTurn(t, run, "inv-assembly-2", addr, "第二轮输入")

		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
		}
		got := requests[1].Messages
		if len(got) != 4 {
			t.Fatalf("second request carries %d messages, want the two mapped facts, the input and the block: %v", len(got), got)
		}
		main := got[0]
		if main.Role != model.RoleUser {
			t.Fatalf("message 0 role = %q, want the mapped input as a user message", main.Role)
		}
		if !strings.Contains(main.Content, assemblyPreview) {
			t.Fatalf("main context carries no %d-rune preview under budget %d: %q", 128, assemblyBudget, main.Content)
		}
		if !strings.Contains(main.Content, "full text retrievable by segment") {
			t.Fatalf("main context carries no ticket line: %q", main.Content)
		}
		if strings.Contains(main.Content, assemblyFullText) {
			t.Fatalf("main context carries the full text past the budget: %q", main.Content)
		}
		if got[1].Role != model.RoleAssistant || got[1].Content != "回答第1轮" {
			t.Fatalf("message 1 = (%q, %q), want the mapped answer in full", got[1].Role, got[1].Content)
		}
		block, at := environmentBlockMessage(t, got)
		if at != 3 {
			t.Fatalf("environment block stands at message %d, want it trailing the request", at)
		}
		requireEnvironmentFields(t, block)
		if got[2].Role != model.RoleUser || got[2].Content != "第二轮输入" {
			t.Fatalf("message 2 = (%q, %q), want the invocation input ahead of the block", got[2].Role, got[2].Content)
		}
	})

	t.Run("the environment block stays whole beside a budget that skips the projection", func(t *testing.T) {
		tight := assembleWithBudget(t, "session-assembly-tight", 20)
		roomy := assembleWithBudget(t, "session-assembly-roomy", 1200)

		tightBlock := environmentBlockOf(t, tight)
		roomyBlock := environmentBlockOf(t, roomy)
		if tightBlock != roomyBlock {
			t.Fatalf("environment block under the tight budget = %q, want the block under the roomy budget %q", tightBlock, roomyBlock)
		}
		requireEnvironmentFields(t, tightBlock)
		if strings.Contains(strings.Join(messageContents(tight), "\n"), assemblyFullText) {
			t.Fatalf("the tight budget presented the full text: %v", tight)
		}
		if !strings.Contains(strings.Join(messageContents(roomy), "\n"), assemblyFullText) {
			t.Fatalf("the roomy budget presented no full text: %v", roomy)
		}
	})

	t.Run("delegated refs degrade to the same ticket", func(t *testing.T) {
		provider := &assemblyRefModel{name: "model-reviewer"}
		handoff := &agent.HandoffEnvelope{
			Intent:      "review the material",
			Constraints: []string{"cite the evidence"},
			Refs:        []event.EventID{"ev-same"},
		}
		builder := subagent.Builder{
			Model: func(context.Context, string) (trpcmodel.Model, error) { return provider, nil },
			Facts: func(context.Context, event.EventID) (event.Event, error) {
				return event.Event{
					Key:     "ev-same",
					Type:    event.TypeExternalInput,
					Message: model.Message{Role: model.RoleUser, Content: assemblyFullText},
					Meta:    map[string]string{event.MetaKeyEventSummary: "the stored summary"},
				}, nil
			},
			RefBudget: assemblyBudget,
		}
		assembly := &subagent.Assembly{Definition: subagent.Definition{Name: "reviewer", Model: "model-reviewer"}}
		if _, err := builder.Serve(context.Background(), assembly, subagent.Turn{
			UserID:    "tenant-review",
			SessionID: "session-delegated-refs",
			Input:     "the change under review",
			Handoff:   handoff,
		}); err != nil {
			t.Fatalf("serve one turn carrying refs = %v, want the assembled agent to answer", err)
		}
		requests := provider.recorded()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want exactly one turn", len(requests))
		}
		got := requestContent(requests[0])
		if !strings.Contains(got, assemblyPreview) {
			t.Fatalf("delegated refs carry no %d-rune preview under budget %d: %q", 128, assemblyBudget, got)
		}
		if !strings.Contains(got, "full text retrievable by segment") {
			t.Fatalf("delegated refs carry no ticket line: %q", got)
		}
		if strings.Contains(got, assemblyFullText) {
			t.Fatalf("delegated refs carry the full text past the budget: %q", got)
		}
		if strings.Contains(got, "the stored summary") {
			t.Fatalf("delegated refs skipped the ticket for the summary: %q", got)
		}
	})
}

// TestUndeclaredEnvironmentBlockKeepsTheAssemblyUnchanged is the negative anchor of the assembly segment.
//   - an agent declaring no environment block carries no block and grows no message
//   - the generation counter stays still, so no segment ran unseen
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestUndeclaredEnvironmentBlockKeepsTheAssemblyUnchanged(t *testing.T) {
	before := assembly.EnvironmentBlockGenerations()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("回答第1轮")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: assemblyBudget,
		}},
	}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-no-assembly-block"}
	invokeTurn(t, run, "inv-no-assembly-1", addr, assemblyFullText)
	invokeTurn(t, run, "inv-no-assembly-2", addr, "第二轮输入")

	if got := assembly.EnvironmentBlockGenerations() - before; got != 0 {
		t.Fatalf("environment block generations = %d, want none for the undeclared agent", got)
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
	}
	got := requests[1].Messages
	if len(got) != 3 {
		t.Fatalf("second request carries %d messages, want the two mapped facts and the input alone: %v", len(got), got)
	}
	for _, message := range got {
		if strings.Contains(message.Content, environmentBlockOpen) {
			t.Fatalf("request carries an environment block the agent never declared: %v", got)
		}
	}
}

// assembleWithBudget drives one two-invocation session of a declaring agent
// under the given replay budget and returns the second request's messages.
func assembleWithBudget(t *testing.T, session string, budget int) []model.Message {
	t.Helper()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("回答第1轮")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:               "agent-a",
			Model:            "model-a",
			MaxTokens:        budget,
			EnvironmentBlock: true,
		}},
	}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)}
	invokeTurn(t, run, "inv-"+session+"-1", addr, assemblyFullText)
	invokeTurn(t, run, "inv-"+session+"-2", addr, "第二轮输入")
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
	}
	return requests[1].Messages
}

// environmentBlockMessage reports the environment block a request closes with
// and the message index it stands at: the block trails the request under its
// declared markers, so it is the last message and carries the block alone.
func environmentBlockMessage(t *testing.T, messages []model.Message) (string, int) {
	t.Helper()
	if len(messages) == 0 {
		t.Fatalf("request carries no message, want the environment block trailing it")
	}
	last := messages[len(messages)-1]
	if !strings.HasPrefix(last.Content, environmentBlockOpen+"\n") || !strings.HasSuffix(last.Content, environmentBlockClose) {
		t.Fatalf("last message = (%q, %q), want the marked environment block trailing the request: %v", last.Role, last.Content, messages)
	}
	return last.Content, len(messages) - 1
}

// environmentBlockOf reports the environment block of one request.
func environmentBlockOf(t *testing.T, messages []model.Message) string {
	t.Helper()
	block, _ := environmentBlockMessage(t, messages)
	return block
}

// requireEnvironmentFields checks the block names every face a request closes on.
func requireEnvironmentFields(t *testing.T, block string) {
	t.Helper()
	for _, want := range []string{
		"- agent: agent-a",
		"- model: model-a",
		"- tools: (none)",
		"- skills: (none)",
		"- mcp: (none)",
		"- tasks: 0 open",
		"  - (none)",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("environment block = %q, want it to name %q", block, want)
		}
	}
}

// messageContents reports the content of every message of one request.
func messageContents(messages []model.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.Content)
	}
	return out
}

// assemblyRefModel is the native model stand-in of the delegated refs check: it
// records every request and streams one scripted final answer.
type assemblyRefModel struct {
	mu       sync.Mutex
	name     string
	requests []trpcmodel.Request
}

// GenerateContent records the request and streams the scripted final answer.
func (m *assemblyRefModel) GenerateContent(_ context.Context, req *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	m.mu.Unlock()
	ch := make(chan *trpcmodel.Response, 1)
	ch <- &trpcmodel.Response{
		Done:    true,
		Choices: []trpcmodel.Choice{{Message: trpcmodel.NewAssistantMessage("review recorded")}},
	}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *assemblyRefModel) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// recorded reports the requests the model was driven with.
func (m *assemblyRefModel) recorded() []trpcmodel.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]trpcmodel.Request(nil), m.requests...)
}

// requestContent reports the joined content of every message one request carries.
func requestContent(req trpcmodel.Request) string {
	parts := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}
