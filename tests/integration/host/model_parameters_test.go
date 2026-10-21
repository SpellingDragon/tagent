package host

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
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
			host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
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
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
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
