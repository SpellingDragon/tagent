package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
		case 2, 3:
			// Full-duplex consumption may take one closing decision after the
			// tool round when a settlement lands late; the scripted answer is
			// the same completed turn either way.
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
	// Full-duplex consumption may take one closing decision when a settlement
	// lands after the receipts round, so the scenario pins the closed roundtrip
	// and reads its observations from the decision that closes it: the last view
	// that carries both admission receipts. The closing view is the one the
	// model answered the turn from, and the one that presents receipts and
	// backgrounds side by side.
	if len(requests) < 2 || len(requests) > 3 || turn.DecisionCount < 2 || turn.DecisionCount > 3 || turn.Terminal != model.KindCompleted {
		t.Fatalf("requests = %d, decisions = %d, terminal = %v, want one admitted roundtrip beside its final decision", len(requests), turn.DecisionCount, turn.Terminal)
	}
	closing := scenarioClosingView(requests)
	if closing == nil {
		t.Fatalf("no view carries both admission receipts beside the settled backgrounds")
	}

	calls := []string{}
	receipts := []map[string]any{}
	for _, message := range closing {
		if message.Role != model.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			calls = append(calls, call.ID)
		}
	}
	observations["requested_calls"] = calls
	if len(calls) != 2 {
		t.Fatalf("the closing view carries %d native calls, want the scripted two", len(calls))
	}
	// The settled bodies may land in the closing decision itself or in the next
	// request the late settlement drives, and that request may present a body as
	// the result message of its call rather than as a second observation. Read
	// both bodies once, then pin the stable receipt round of the closing view:
	// the trailing observation count is delivery-order dependent, so the body
	// arrival below is the result assertion that carries it.
	bodies := map[string]string{calls[0]: toolA.output, calls[1]: toolB.output}
	backgroundsSeen := scenarioAwaitBackgrounds(t, provider, bodies)
	observations["model_message_roles"] = scenarioReceiptRoles(closing)
	t.Logf("S03 views: %s", scenarioViewDigest(provider.recorded()))
	for _, call := range calls {
		if _, held := scenarioBackgroundOf(closing, call, bodies[call]); !held {
			t.Logf("S03 full-duplex: the closing decision carried one settled body; the next request carried the other")
			break
		}
	}

	labels := newScenarioLabels()
	issuingDecision := event.DecisionID("")
	issuingCount := 0
	decisionIDs := []event.DecisionID{}
	// The call→task link is read from the chain, never from the receipt text:
	// the receipt is the minimal admission confirmation, and the result fact
	// under the native call identity is what names the task it settled. A
	// settlement the roundtrip answered before may still be landing, so the two
	// results are awaited inside a bound before the link is read.
	facts, taskOfCall := scenarioCallTasks(t, run, event.Address{AgentID: "agent-a", SessionID: "session-receipt"}, calls)
	for _, fact := range facts {
		if fact.InvocationID != turn.InvocationID || fact.DecisionID == "" {
			continue
		}
		if fact.Type == event.TypeThinkingPlan || fact.Type == event.TypeAgentOutput {
			decisionIDs = append(decisionIDs, fact.DecisionID)
		}
		if fact.Type == event.TypeThinkingPlan {
			issuingDecision = fact.DecisionID
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
		for at, message := range closing {
			if message.ToolID != call {
				continue
			}
			if message.Role == model.RoleTool {
				receiptsPerCall[call]++
				taskID := taskOfCall[call]
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
			if at < scenarioIndexOfReceipt(closing, call) {
				receiptBeforeTerminal = false
			}
		}
	}
	// The settled body reaches the model view that reads it: the closing decision
	// of the roundtrip when it is still in flight, or the next request the
	// settlement drives when the roundtrip answered first. The read is the body
	// under its own call identity in a request after the tool round, never a value
	// copied from the fixture and never one presentation role.
	captured["background_original"] = backgroundsSeen[calls[0]]
	observations["receipts"] = receipts
	observations["backgrounds"] = backgrounds
	observations["captured_content_markers"] = captured
	exactlyOnce := true
	for _, call := range calls {
		if receiptsPerCall[call] != 1 || taskOfCall[call] == "" {
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
	observations["background_result_in_next_model_view"] = len(backgroundsSeen) == len(bodies)
	observations["native_call_id_preserved"] = scenarioCallsPreserved(calls)
	observations["links_are_event_derived_one_to_one"] = exactlyOnce
	observations["multi_tool_call_boundary_preserved"] = scenarioBoundaryPreserved(provider.recorded(), calls, bodies)
	observations["request_standin_strictly_validated"] = len(violations) == 0
	for _, decisionID := range decisionIDs {
		refs = append(refs, string(decisionID))
	}
	for taskID := range seenTasks {
		refs = append(refs, taskID)
	}

	EmitScenarioRecord(t, scenario, refs, observations)
}

// scenarioCallTasks reports the chain facts and the call→task link the result
// facts under the two native call identities carry, waiting inside a bound for
// a settlement the roundtrip's late read may still be landing.
func scenarioCallTasks(t *testing.T, run *assembly.Run, addr event.Address, calls []string) ([]event.Event, map[string]string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		facts := run.SessionFacts(addr)
		tasks := map[string]string{}
		for _, fact := range facts {
			if fact.Type != event.TypeActionCommand || fact.TaskID == "" {
				continue
			}
			callID := fact.Message.ToolID
			if callID == "" {
				callID = fact.CallID
			}
			if callID != "" {
				tasks[callID] = string(fact.TaskID)
			}
		}
		complete := true
		for _, call := range calls {
			if tasks[call] == "" {
				complete = false
			}
		}
		if complete {
			return facts, tasks
		}
		if time.Now().After(deadline) {
			t.Fatalf("the two result facts did not land within the wait: %v", tasks)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// scenarioClosingView reports the first request view that carries both admission
// receipts, or nil beside none: the first view after the tool round is the
// closing decision of the roundtrip, while a later segment the settlement drives
// re-presents the same receipts but is not that decision. Reading forward keeps a
// late segment from being mistaken for the roundtrip's closing answer.
func scenarioClosingView(requests []model.Request) []model.Message {
	for at := 0; at < len(requests); at++ {
		receipts := map[string]bool{}
		for _, message := range requests[at].Messages {
			if message.Role == model.RoleTool && message.Content == "ok" && message.ToolID != "" {
				receipts[message.ToolID] = true
			}
		}
		if receipts["call-a"] && receipts["call-b"] {
			return requests[at].Messages
		}
	}
	return nil
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

// scenarioAwaitBackgrounds waits until every settled body reached a request
// after the tool round under its own call identity and reports the body each
// was read under. The wait is bounded because the segment that reads a late
// settlement may start after the driven call returned.
func scenarioAwaitBackgrounds(t *testing.T, provider *scenarioModel, bodies map[string]string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		requests := provider.recorded()
		seen := map[string]string{}
		for call, body := range bodies {
			for at := 1; at < len(requests); at++ {
				if content, found := scenarioBackgroundOf(requests[at].Messages, call, body); found {
					seen[call] = content
					break
				}
			}
		}
		if len(seen) == len(bodies) {
			return seen
		}
		if time.Now().After(deadline) {
			t.Fatalf("the settled bodies of %v did not reach a model view within the wait; recorded views: %s", bodies, scenarioViewDigest(requests))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// scenarioViewDigest renders the recorded requests as one line per view, so a
// wait that timed out names what the model actually saw instead of only what it
// did not: each entry is role/tool-identity/content-prefix.
func scenarioViewDigest(requests []model.Request) string {
	parts := make([]string, 0, len(requests))
	for at, request := range requests {
		entries := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			content := message.Content
			if len(content) > 24 {
				content = content[:24] + "..."
			}
			entries = append(entries, fmt.Sprintf("%s/%s/%q", message.Role, message.ToolID, content))
		}
		parts = append(parts, fmt.Sprintf("view %d [%s]", at, strings.Join(entries, "; ")))
	}
	return strings.Join(parts, " | ")
}

// scenarioBackgroundOf reports the body one settled call enters a request under
// its own call identity. The body may be presented as the observation message
// of the closing decision or as the result message of the next request the late
// settlement drives, so the read names the call identity and the body and never
// one presentation role.
func scenarioBackgroundOf(messages []model.Message, callID, body string) (string, bool) {
	for _, message := range messages {
		if message.ToolID == callID && message.Content == body {
			return message.Content, true
		}
	}
	return "", false
}

// scenarioReceiptRoles reports the closing view's role sequence through its last
// admission receipt: the decision and the two receipts are the stable process
// shape of the roundtrip, while the observation tail is delivery-order dependent
// (in the closing view, in the next self-driven request, or split between them).
func scenarioReceiptRoles(messages []model.Message) []string {
	roles := scenarioRoles(messages)
	lastReceipt := -1
	for at, message := range messages {
		if message.Role == model.RoleTool {
			lastReceipt = at
		}
	}
	if lastReceipt < 0 {
		return roles
	}
	return roles[:lastReceipt+1]
}

// scenarioCallsPreserved reports whether the native call identifiers the scripted
// answer carried reached the runtime unchanged.
func scenarioCallsPreserved(calls []string) bool {
	return len(calls) == 2 && calls[0] == "call-a" && calls[1] == "call-b"
}

// scenarioBoundaryPreserved reports whether each call's admission receipt and
// settled body reached a model view under its own call identity. A late
// settlement may land in a later request than its receipt, so the boundary is
// read across the whole recorded stream and never from one view's arrival order.
func scenarioBoundaryPreserved(requests []model.Request, calls []string, bodies map[string]string) bool {
	if len(calls) != 2 {
		return false
	}
	receipts := map[string]bool{}
	settled := map[string]bool{}
	for _, request := range requests {
		for _, message := range request.Messages {
			if message.ToolID == "" {
				continue
			}
			if body := bodies[message.ToolID]; body != "" && message.Content == body {
				settled[message.ToolID] = true
				continue
			}
			if message.Role == model.RoleTool {
				receipts[message.ToolID] = true
			}
		}
	}
	for _, call := range calls {
		if !receipts[call] || !settled[call] {
			return false
		}
	}
	return true
}
