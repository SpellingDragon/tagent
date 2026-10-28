package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// scriptedToolCallAnswer is the answer of one decision that issues the send
// tool call the closing-round check drives.
const scriptedToolCallAnswer = `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"sending","tool_calls":[{"id":"call-1","type":"function","function":{"name":"send","arguments":"{\"content\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`

// TestBootPassesModelParameters checks the sampling the provider actually received:
//   - a declared temperature, thinking switch and reasoning effort reach the request
//   - a declared zero is written as the zero the document named
//   - an undeclared sampling stays absent from the request
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestBootPassesModelParameters(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	cases := []struct {
		name       string
		agentBlock string
		want       map[string]string
		absent     []string
	}{
		{
			name: "declared sampling reaches the request",
			agentBlock: "    temperature: 0.25\n" +
				"    thinking_enabled: true\n" +
				"    reasoning_effort: high\n",
			want: map[string]string{"temperature": "0.25", "thinking_enabled": "true", "reasoning_effort": "high"},
		},
		{
			name: "a declared zero is written as the zero the document named",
			agentBlock: "    temperature: 0\n" +
				"    thinking_enabled: false\n",
			want:   map[string]string{"temperature": "0", "thinking_enabled": "false"},
			absent: []string{"reasoning_effort"},
		},
		{
			name:   "an undeclared sampling stays absent",
			absent: []string{"temperature", "thinking_enabled", "reasoning_effort"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := newScriptedProvider(t)
			root := t.TempDir()
			document := scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-a", tc.agentBlock)
			configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
			writeHostDocumentFile(t, configPath, document)
			host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
			if err != nil {
				t.Fatalf("boot the host: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if turn := invokeScriptedTurn(t, ctx, host, "session-params"); turn.Terminal != model.KindCompleted {
				t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
			}
			bodies := provider.recorded()
			if len(bodies) != 1 {
				t.Fatalf("model requests = %d, want the one request of the turn's decision", len(bodies))
			}
			body := bodies[0]
			for key, want := range tc.want {
				raw, ok := body[key]
				if !ok {
					t.Fatalf("request carries no %q, want the declared value %s", key, want)
				}
				if got := scriptedScalar(t, raw); got != want {
					t.Fatalf("request %q = %s, want %s", key, got, want)
				}
			}
			for _, key := range tc.absent {
				if raw, ok := body[key]; ok {
					t.Fatalf("request %q = %s, want the parameter absent beside no declaration", key, raw)
				}
			}
			if err := host.Close(ctx); err != nil {
				t.Fatalf("close the host: %v", err)
			}
		})
	}
}

// TestBootAppliesMaxToolIterations budgets a drive through max_tool_iterations:
//   - the declared bound becomes the round budget of the drive
//   - the drive spends it, runs the closing round and ends on two decisions
//   - the closing round admits no tool call of its own
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestBootAppliesMaxToolIterations(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		if index < 2 {
			return scriptedToolCallAnswer
		}
		return scriptedTerminalAnswer
	}
	root := t.TempDir()
	agentBlock := "    max_tool_iterations: 1\n" +
		"    tools:\n" +
		"      - { kind: tool, id: send }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-a", agentBlock))
	host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	turn := invokeScriptedTurn(t, ctx, host, "session-iterations")
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the closing round", turn.Terminal)
	}
	if turn.FinalRound != agent.FinalRoundRounds {
		t.Fatalf("final round reason = %q, want the spent round budget of max_tool_iterations", turn.FinalRound)
	}
	if len(turn.Decisions) != 2 {
		t.Fatalf("turn decisions = %d, want the one ordinary round beside its closing round", len(turn.Decisions))
	}
	if models := provider.servedModels(); len(models) != 2 {
		t.Fatalf("model requests = %d, want one per round of the budgeted drive", len(models))
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// declaredSampling is the sampling the declared-agent cases pin on the second
// agent: the three parameters a declaration may name for its model requests.
var declaredSampling = map[string]string{
	"temperature":      "0.3",
	"thinking_enabled": "true",
	"reasoning_effort": "high",
}

// TestSubAgentDeclaredParametersReachRequest pins the sampling a declared agent's turns run under:
// - the declared agent's own turn reaches the provider with its three declared parameters
// - the entry agent's turn beside them carries none of the three
// - a delegated turn of the declared agent carries them too, though that turn runs through the sub-agent builder
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestSubAgentDeclaredParametersReachRequest(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	t.Run("the declared agent's own turn carries its declared sampling", func(t *testing.T) {
		provider := newScriptedProvider(t)
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-a",
			"  user: {}\n"+
				"  knowledge:\n"+
				"    temperature: 0.3\n"+
				"    thinking_enabled: true\n"+
				"    reasoning_effort: high\n"))
		host, err := assembly.Boot(configPath, assembly.WithSender(recordingSender{}))
		if err != nil {
			t.Fatalf("boot the host: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		for _, addr := range []event.Address{
			{AgentID: "user", SessionID: "session-entry-params"},
			{AgentID: "knowledge", SessionID: "session-knowledge-params"},
		} {
			turn, err := host.Invoke(ctx, assembly.InvokeRequest{
				Address: addr,
				Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
			})
			if err != nil {
				t.Fatalf("invoke %q: %v", addr.AgentID, err)
			}
			if turn.Terminal != model.KindCompleted {
				t.Fatalf("turn terminal of %q = %v, want the completed turn of the scripted answer", addr.AgentID, turn.Terminal)
			}
		}
		bodies := provider.recorded()
		if len(bodies) != 2 {
			t.Fatalf("model requests = %d, want the one request of each agent's turn", len(bodies))
		}
		for key := range declaredSampling {
			if raw, ok := bodies[0][key]; ok {
				t.Fatalf("entry request %q = %s, want the parameter absent beside no declaration", key, raw)
			}
		}
		for key, want := range declaredSampling {
			raw, ok := bodies[1][key]
			if !ok {
				t.Fatalf("declared agent request carries no %q, want the declared %s", key, want)
			}
			if got := scriptedScalar(t, raw); got != want {
				t.Fatalf("declared agent request %q = %s, want %s", key, got, want)
			}
		}
		if err := host.Close(ctx); err != nil {
			t.Fatalf("close the host: %v", err)
		}
	})

	t.Run("a delegated turn of the declared agent carries them too", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		temperature := 0.3
		thinking := true
		settled := make(chan struct{})
		knowledge := &entryModel{name: "model-knowledge", answer: func(*model.Request) *model.Response {
			close(settled)
			return entryFinalTurn("knowledge answered")
		}}
		var callerMu sync.Mutex
		callerRounds := 0
		caller := &entryModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			callerMu.Lock()
			callerRounds++
			round := callerRounds
			callerMu.Unlock()
			if round == 1 {
				return entryToolTurn(entryCall("call-knowledge", "knowledge", `{"input":"probe"}`))
			}
			return entryFinalTurn("delegated")
		}}
		decl := &config.Declaration{
			Entry: "caller",
			Agents: []config.AgentSpec{
				{
					ID:    "caller",
					Model: "model-caller",
					Tools: []config.ToolRef{{
						Name:   "knowledge",
						Params: map[string]string{subagent.ParamAgent: "knowledge"},
					}},
				},
				{
					ID:              "knowledge",
					Model:           "model-knowledge",
					Temperature:     &temperature,
					ThinkingEnabled: &thinking,
					ReasoningEffort: "high",
				},
			},
		}
		run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
			"caller":    {Model: caller},
			"knowledge": {Model: knowledge},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		addr := event.Address{AgentID: "caller", SessionID: "session-declared-sampling"}
		if _, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-declared-sampling",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "delegate the probe"}},
		}); err != nil {
			t.Fatalf("invoke the caller: %v", err)
		}
		select {
		case <-settled:
		case <-time.After(10 * time.Second):
			t.Fatal("the delegated turn of the declared agent never reached its request")
		}
		requests := knowledge.recorded()
		if len(requests) != 1 {
			t.Fatalf("delegated requests = %d, want the one decision of the target", len(requests))
		}
		if got := requests[0].Temperature; got == nil || *got != temperature {
			t.Fatalf("delegated request temperature = %v, want the declared %v", got, temperature)
		}
		if got := requests[0].ThinkingEnabled; got == nil || !*got {
			t.Fatalf("delegated request thinking switch = %v, want the declared true", got)
		}
		if got := requests[0].ReasoningEffort; got == nil || *got != "high" {
			t.Fatalf("delegated request reasoning effort = %v, want the declared high", got)
		}
		for _, request := range caller.recorded() {
			if request.Temperature != nil || request.ThinkingEnabled != nil || request.ReasoningEffort != nil {
				t.Fatalf("caller request carries sampling its declaration never named: %+v", request)
			}
		}
	})
}

// scriptedHostDocumentWithAgentSections renders one host document over the
// agent sections given verbatim, so a case declares as many agents as it reads.
func scriptedHostDocumentWithAgentSections(root, endpoint, modelName, sections string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + modelName + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString(sections)
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// scriptedScalar renders one recorded JSON scalar as the text the check compares:
// numbers verbatim, strings quoted as their value, booleans as their literal.
func scriptedScalar(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode the recorded scalar %s: %v", raw, err)
	}
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return fmt.Sprintf("%t", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	default:
		t.Fatalf("recorded value %s is not a JSON scalar", raw)
		return ""
	}
}
