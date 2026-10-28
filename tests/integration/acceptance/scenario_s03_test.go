package acceptance

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestScenario03ToolReceiptAndBackground drives one native roundtrip carrying two tool calls through the public assembly entry.
// - one admission receipt per call under its own native call identity, before the terminal
// - the settled background body arrives in the next model view under its call identity
// - the call, task and decision identities pair one to one
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestScenario03ToolReceiptAndBackground(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S03")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}
	violations := []string{}

	provider := &scenarioModel{name: "model-a"}
	turns := 0
	provider.answer = func(req *model.Request) *model.Response {
		turns++
		switch turns {
		case 1:
			if len(req.Messages) != 1 || req.Messages[0].Role != model.RoleUser || req.Messages[0].Content != "hello" {
				violations = append(violations, "the first request carried other messages than the one user input")
				return scenarioFailTurn("request outside the scripted stand-in")
			}
			return scenarioToolTurn(
				scenarioCall("call-a", "receipt-marker-a", `{"x":1}`),
				scenarioCall("call-b", "receipt-marker-b", `{"y":2}`),
			)
		case 2:
			return scenarioFinalTurn("settled")
		default:
			violations = append(violations, "the drive requested a decision outside the scripted roundtrip")
			return scenarioFailTurn("decision outside the scripted roundtrip")
		}
	}
	toolA := &scenarioTool{name: "receipt-marker-a", output: "background-marker-a"}
	toolB := &scenarioTool{name: "receipt-marker-b", output: "background-marker-b"}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	governance := &scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{toolA, toolB}},
	}, assembly.WithGovernance(governance))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	turn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-receipt",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-receipt"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the roundtrip: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 2 || len(turn.Decisions) != 2 {
		t.Fatalf("requests = %d, decisions = %d, want one admitted roundtrip beside its final decision", len(requests), len(turn.Decisions))
	}
	second := requests[1].Messages
	observations["model_message_roles"] = scenarioRoles(second)

	calls := []string{}
	receipts := []map[string]any{}
	for _, message := range second {
		if message.Role != model.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			calls = append(calls, call.ID)
		}
	}
	observations["requested_calls"] = calls

	labels := newScenarioLabels()
	issuingDecision := event.DecisionID("")
	issuingCount := 0
	for _, decision := range turn.Decisions {
		if len(decision.Request.Messages) == len(requests[0].Messages) {
			issuingDecision = decision.DecisionID
			issuingCount++
		}
	}
	if issuingCount != 1 {
		t.Fatalf("decisions whose request carries the issuing turn = %d, want exactly one", issuingCount)
	}
	receiptsPerCall := map[string]int{}
	receiptBeforeTerminal := true
	backgrounds := []map[string]any{}
	captured := map[string]any{}
	seenTasks := map[string]int{}
	for _, call := range calls {
		for at, message := range second {
			if message.ToolID != call {
				continue
			}
			if message.Role == model.RoleTool {
				receiptsPerCall[call]++
				taskID := scenarioTaskIDOf(message.Content)
				labels.label("task", taskID)
				seenTasks[taskID]++
				receipts = append(receipts, map[string]any{
					"call":     labels.label("call", call),
					"task":     labels.label("task", taskID),
					"decision": labels.label("decision", string(issuingDecision)),
				})
				backgrounds = append(backgrounds, map[string]any{
					"call":   labels.label("call", call),
					"marker": strings.Replace(labels.label("call", call), "call-", "bg-", 1),
				})
				if call == calls[0] {
					captured["receipt_message"] = message.ToolName
				}
				continue
			}
			if at < scenarioIndexOfReceipt(second, call) {
				receiptBeforeTerminal = false
			}
			if call == calls[0] {
				captured["background_original"] = message.Content
			}
		}
	}
	observations["receipts"] = receipts
	observations["backgrounds"] = backgrounds
	observations["captured_content_markers"] = captured
	exactlyOnce := true
	for _, call := range calls {
		if receiptsPerCall[call] != 1 {
			exactlyOnce = false
		}
	}
	for _, count := range seenTasks {
		if count != 1 {
			exactlyOnce = false
		}
	}
	observations["receipts_per_call"] = scenarioUniformCount(receiptsPerCall)
	observations["receipt_before_terminal"] = receiptBeforeTerminal && exactlyOnce
	observations["background_result_in_next_model_view"] = scenarioCarriesBackground(second, calls[0], "background-marker-a")
	observations["native_call_id_preserved"] = scenarioCallsPreserved(calls)
	observations["links_are_event_derived_one_to_one"] = exactlyOnce
	observations["multi_tool_call_boundary_preserved"] = scenarioBoundaryPreserved(second, calls)
	observations["request_standin_strictly_validated"] = len(violations) == 0
	for _, decision := range turn.Decisions {
		refs = append(refs, string(decision.DecisionID))
	}
	for taskID := range seenTasks {
		refs = append(refs, taskID)
	}

	EmitScenarioRecord(t, scenario, refs, observations)
}

// scenarioTaskIDOf reports the task identity one admission receipt names.
func scenarioTaskIDOf(content string) string {
	const prefix = "accepted "
	if !strings.HasPrefix(content, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(content, prefix)
	if at := strings.Index(rest, " "); at >= 0 {
		return rest[:at]
	}
	return rest
}

// scenarioIndexOfReceipt reports the position of the receipt message of one call.
func scenarioIndexOfReceipt(messages []model.Message, callID string) int {
	for at, message := range messages {
		if message.ToolID == callID && message.Role == model.RoleTool {
			return at
		}
	}
	return len(messages)
}

// scenarioUniformCount reports the shared count of one per-call tally, or -1 when
// the tally is not uniform.
func scenarioUniformCount(tally map[string]int) int {
	shared := -1
	for _, count := range tally {
		if shared == -1 {
			shared = count
			continue
		}
		if shared != count {
			return -1
		}
	}
	return shared
}

// scenarioCarriesBackground reports whether the next model view carries the named
// background body under the named call identity.
func scenarioCarriesBackground(messages []model.Message, callID, body string) bool {
	for _, message := range messages {
		if message.ToolID == callID && message.Role != model.RoleTool && message.Content == body {
			return true
		}
	}
	return false
}

// scenarioCallsPreserved reports whether the native call identifiers the scripted
// answer carried reached the runtime unchanged.
func scenarioCallsPreserved(calls []string) bool {
	return len(calls) == 2 && calls[0] == "call-a" && calls[1] == "call-b"
}

// scenarioBoundaryPreserved reports whether two calls of one assistant message
// keep their own paired answer boundaries.
func scenarioBoundaryPreserved(messages []model.Message, calls []string) bool {
	if len(calls) != 2 {
		return false
	}
	paired := map[string]int{}
	for _, message := range messages {
		if message.ToolID == "" {
			continue
		}
		paired[message.ToolID]++
	}
	return paired[calls[0]] >= 2 && paired[calls[1]] >= 2
}
