// The request-fidelity anchors of the host: every request the entry assembly
// sends must carry complete call-result pairing on the wire — every role=tool
// message answers a tool_call_id an assistant tool_calls message carries, and
// every tool call carries its id, function name and arguments. The three
// scenarios pin the three moments the replay path rebuilds a request from
// committed material: the turn after a tool call, the request after the age
// fold compressed the chain, and the turn after a cold boot re-read it. The
// protocol gate is the strict double of the check: a request that violates the
// pairing is refused with a 400 exactly as an OpenAI-compatible endpoint would.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// fidelityPairingViolations reports the call-result pairing violations of one
// request's messages, in message order: a role=tool message must carry a
// tool_call_id an earlier assistant tool_calls message answers, and every tool
// call must carry its id, function name and arguments. The check reads the wire
// shape the provider decoded, never the assembly's intent.
func fidelityPairingViolations(messages []scriptedMessage) []string {
	var violations []string
	answered := map[string]bool{}
	for index, message := range messages {
		if message.Role == string(model.RoleAssistant) {
			for _, call := range message.ToolCalls {
				if call.ID == "" || call.Name == "" || strings.TrimSpace(call.Arguments) == "" {
					violations = append(violations, fmt.Sprintf(
						"message %d: assistant tool call missing id/name/arguments: id=%q name=%q arguments=%q",
						index+1, call.ID, call.Name, call.Arguments))
					continue
				}
				answered[call.ID] = true
			}
		}
		if message.Role != string(model.RoleTool) {
			continue
		}
		if message.ToolCallID == "" {
			violations = append(violations, fmt.Sprintf(
				"message %d: role=tool carries no tool_call_id (content=%q)", index+1, message.Content))
			continue
		}
		if !answered[message.ToolCallID] {
			violations = append(violations, fmt.Sprintf(
				"message %d: role=tool names tool_call_id %q with no assistant tool_calls message answering it (content=%q)",
				index+1, message.ToolCallID, message.Content))
		}
	}
	return violations
}

// fidelityGate wraps one decision with the protocol-strict pairing gate: a
// request that violates the call-result pairing is refused by name, and the
// endpoint serves the refusal as a 400 the drive fails on instead of letting a
// broken request reach the model. A request that passes runs the script.
func fidelityGate(decide func(scriptedRequest) (string, error)) func(scriptedRequest) (string, error) {
	return func(request scriptedRequest) (string, error) {
		if violations := fidelityPairingViolations(request.Messages); len(violations) > 0 {
			return "", fmt.Errorf("protocol pairing: %s", strings.Join(violations, "; "))
		}
		return decide(request)
	}
}

// requireFidelityPairing requires every request the provider served to pass the
// call-result pairing check, naming every violation of every request on a red.
func requireFidelityPairing(t *testing.T, provider *scriptedProvider, stage string) {
	t.Helper()
	requests := provider.servedRequests()
	if len(requests) == 0 {
		t.Fatalf("%s: the provider served no request to check pairing on", stage)
	}
	var violations []string
	for _, request := range requests {
		for _, violation := range fidelityPairingViolations(request.Messages) {
			violations = append(violations, fmt.Sprintf("request %d (model %s): %s", request.Index+1, request.Model, violation))
		}
	}
	if len(violations) > 0 {
		t.Fatalf("%s: the request stream violates call-result pairing:\n%s", stage, strings.Join(violations, "\n"))
	}
}

// requireCallResultPair requires one request to carry the complete call-result
// pair of the named call: the assistant message whose tool_calls entry holds the
// call's id, function name and arguments, beside the role=tool answer that
// carries the same id.
func requireCallResultPair(t *testing.T, request scriptedRequest, callID, name, arguments string) {
	t.Helper()
	var answered, result bool
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if message.Role == string(model.RoleAssistant) && call.ID == callID {
				if call.Name != name || call.Arguments != arguments {
					t.Fatalf("the assistant tool call %q = (name=%q arguments=%q), want the complete call (name=%q arguments=%q)",
						callID, call.Name, call.Arguments, name, arguments)
				}
				answered = true
			}
		}
		if message.Role == string(model.RoleTool) && message.ToolCallID == callID {
			result = true
		}
	}
	if !answered || !result {
		t.Fatalf("request messages = %+v, want the assistant tool call %q (name=%q arguments=%q) beside its role=tool answer",
			request.Messages, callID, name, arguments)
	}
}

// fidelityCountToolMessages reports how many role=tool messages one request
// carries, so a scenario proves its pairing check ran over real material
// instead of passing over a request that carries no call at all.
func fidelityCountToolMessages(request scriptedRequest) int {
	count := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleTool) {
			count++
		}
	}
	return count
}

// fidelityEntrySections renders the entry agent section of one fidelity host:
// the entry agent under the named model window beside the declared send tool.
// The window is the replay window that registers the mapped sequence, so the
// requests of the session are rebuilt from the projection the commits record.
func fidelityEntrySections(window string) string {
	return "  user:\n" +
		"    model: model-entry\n" +
		window +
		"    tools:\n" +
		"      - { kind: tool, id: send }\n"
}

// bootFidelityHost boots the fidelity entry document over the given root and
// answers its configuration path, so a scenario re-boots the same document over
// the same storage root after a close.
func bootFidelityHost(t *testing.T, provider *scriptedProvider, sender recordingSender, root, window string) (*tagent.Host, string) {
	t.Helper()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", fidelityEntrySections(window)))
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the fidelity host: %v", err)
	}
	return host, configPath
}

// fidelityFillInput is one committed fill turn of the compression scenario: a
// short first line — the one-line read the projection keeps beside it — under a
// long second line that fills the window, so the fill material rides far over
// the fold threshold while every fold product stays small enough that the tool
// pair the scenario pins survives the folds whole.
func fidelityFillInput(turn int) string {
	return fmt.Sprintf("fidelity fill turn %02d\n%s", turn, strings.Repeat("material ", 95))
}

// TestRequestFidelity requires every request the replay path rebuilds to carry complete call-result pairing on the wire.
//   - positive: each scenario's request stream carries complete call-result pairs (tool_call_id / ToolCalls / function name and arguments)
//   - negative: TestRequestFidelityProtocolGate refuses a request whose tool_call_id was stripped, so a green is never a vacuous pass
//   - consumer: every scenario boots the formal assembly and drives real Host.Invoke turns over an OpenAI-compatible endpoint
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRequestFidelity(t *testing.T) {
	reproductionEnv(t)
	t.Run("tool_call_then_next_turn", runFidelityNextTurnAfterToolCall)
	t.Run("request_after_compression", runFidelityRequestAfterCompression)
	t.Run("request_after_cold_boot", runFidelityRequestAfterColdBoot)
}

// runFidelityNextTurnAfterToolCall drives one tool-call turn and then the next
// conversation turn of the same session, and requires the next turn's request
// — rebuilt from the projected chain — to carry the complete call-result pair
// of the call the turn before it issued.
func runFidelityNextTurnAfterToolCall(t *testing.T) {
	const (
		inputOne  = "fidelity turn one issues the call"
		note      = "the fidelity note"
		answerOne = "the fidelity turn one answer"
		inputTwo  = "fidelity turn two speaks from the chain"
		answerTwo = "the fidelity turn two answer"
		callID    = "call-fidelity-next-turn"
		arguments = `{"content":"the fidelity note"}`
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "turn one issues the tool call", requestTail(inputOne)).
		answers(settleToolCallAnswer("send", callID, arguments)).
		delivers(note)
	script.expect("model-entry", "turn one ends on its answer", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answerOne)).
		delivers(answerOne)
	script.expect("model-entry", "turn two speaks from the replayed chain", requestTail(inputTwo)).
		answers(scriptedAnswerOf(answerTwo)).
		delivers(answerTwo)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, _ := bootFidelityHost(t, provider, sender, t.TempDir(), "    max_tokens: 32000\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-request-fidelity-next-turn"}

	turn, err := driveIntegrityTurn(ctx, host, addr, inputOne)
	if err != nil {
		t.Fatalf("the tool-call turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("the tool-call turn = (%v, %d decisions), want the completed turn of its two decisions", turn.Terminal, len(turn.Decisions))
	}
	turnTwo, turnTwoErr := driveIntegrityTurn(ctx, host, addr, inputTwo)

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(inputTwo)(request) {
			target = request
		}
	}
	requireFidelityPairing(t, provider, "the turn after a tool call")
	if fidelityCountToolMessages(target) == 0 {
		t.Fatalf("the turn-after request carries no role=tool call-result material, so the pairing check proves nothing: %+v", target.Messages)
	}
	requireCallResultPair(t, target, callID, "send", arguments)
	if turnTwoErr != nil {
		t.Fatalf("the turn after a tool call ended by error: %v", turnTwoErr)
	}
	if turnTwo.Terminal != model.KindCompleted {
		t.Fatalf("the turn after a tool call = (%v, %d decisions), want the completed turn", turnTwo.Terminal, len(turnTwo.Decisions))
	}
	t.Logf("[fidelity 1] positive: the turn-after request carries the complete pair of %q (name=send, arguments=%q) beside its tool result", callID, arguments)
	closeIntegrityHost(t, ctx, host)
}

// runFidelityRequestAfterCompression fills one session chain past its window
// until the age fold rewrote its projection, then drives one tool-call turn and
// the turn after it, and requires the compressed request to carry the complete
// call-result pair the fold must not have degraded. The window is sized so the
// fill turns fold into history products while the tool turn stays whole: the
// fold judges every commit and consumes the process detail of a closed turn, so
// the compaction lands while the tool turn is still in progress — a segment in
// progress is never folded — and the request after the turn carries the pair
// beside the fold products the compression minted.
func runFidelityRequestAfterCompression(t *testing.T) {
	const (
		inputOne  = "fidelity compressed turn issues the call"
		note      = "the compressed fidelity note"
		answerOne = "the compressed fidelity turn answer"
		inputTwo  = "fidelity compressed turn speaks from the chain"
		answerTwo = "the compressed fidelity turn two answer"
		callID    = "call-fidelity-compressed"
		arguments = `{"content":"the compressed fidelity note"}`
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	for turn := 0; turn < 3; turn++ {
		fill := fidelityFillInput(turn)
		script.expect("model-entry", fmt.Sprintf("fill turn %d", turn+1), requestTail(fill)).
			answers(scriptedAnswerOf(fmt.Sprintf("answer for the fidelity fill turn %d", turn+1))).
			delivers(fmt.Sprintf("answer for the fidelity fill turn %d", turn+1))
	}
	script.expect("model-entry", "the compressed turn issues the tool call", requestTail(inputOne)).
		answers(settleToolCallAnswer("send", callID, arguments)).
		delivers(note)
	script.expect("model-entry", "the compressed turn ends on its answer", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answerOne)).
		delivers(answerOne)
	script.expect("model-entry", "the turn after compression speaks from the chain", requestTail(inputTwo)).
		answers(scriptedAnswerOf(answerTwo)).
		delivers(answerTwo)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, _ := bootFidelityHost(t, provider, sender, t.TempDir(),
		"    max_tokens: 375\n"+
			"    compress_threshold: 1.0\n"+
			"    keep_recent_tasks: 1\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-request-fidelity-compression"}

	for turn := 0; turn < 3; turn++ {
		if _, err := driveIntegrityTurn(ctx, host, addr, fidelityFillInput(turn)); err != nil {
			t.Fatalf("fill turn %d ended by error: %v", turn+1, err)
		}
	}
	entries, registered := host.Run().ReplayEntries(addr.SessionID)
	if !registered {
		t.Fatalf("the session chain of the compression scenario registered no projection, so no fold could run")
	}
	synthetic := 0
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			synthetic++
		}
	}
	if synthetic == 0 {
		t.Fatalf("the three fill turns folded nothing under the 256-token window: projection carries %d entries, none synthetic", len(entries))
	}

	if _, err := driveIntegrityTurn(ctx, host, addr, inputOne); err != nil {
		t.Fatalf("the tool-call turn after compression ended by error: %v", err)
	}
	turnTwo, turnTwoErr := driveIntegrityTurn(ctx, host, addr, inputTwo)

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(inputTwo)(request) {
			target = request
		}
	}
	requireFidelityPairing(t, provider, "the request after compression")
	if fidelityCountToolMessages(target) == 0 {
		t.Fatalf("the compressed request carries no role=tool call-result material, so the pairing check proves nothing: %+v", target.Messages)
	}
	requireCallResultPair(t, target, callID, "send", arguments)
	if turnTwoErr != nil {
		t.Fatalf("the turn after compression ended by error: %v", turnTwoErr)
	}
	if turnTwo.Terminal != model.KindCompleted {
		t.Fatalf("the turn after compression = (%v, %d decisions), want the completed turn", turnTwo.Terminal, len(turnTwo.Decisions))
	}
	t.Logf("[fidelity 2] positive: the compressed request (fold products=%d) carries the complete pair of %q beside its tool result", synthetic, callID)
	closeIntegrityHost(t, ctx, host)
}

// runFidelityRequestAfterColdBoot drives one tool-call turn, closes the host
// and re-boots the same document over the same storage root, and requires the
// first request the restarted run assembles from its replayed chain to carry
// the complete call-result pair of the call the run before it issued. The call
// fact the first boot committed beside its result is the pairing source the
// restart re-reads: the seeded ledger resolves both sides of the pair, so the
// request after the cold boot carries the call identity, function name and the
// arguments as the model issued them instead of a minted stand-in.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func runFidelityRequestAfterColdBoot(t *testing.T) {
	const (
		inputOne  = "fidelity cold boot turn issues the call"
		note      = "the cold boot fidelity note"
		answerOne = "the cold boot fidelity turn answer"
		inputTwo  = "fidelity cold boot turn speaks from the chain"
		answerTwo = "the cold boot fidelity turn two answer"
		callID    = "call-fidelity-cold-boot"
		arguments = `{"content":"the cold boot fidelity note"}`
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first boot issues the tool call", requestTail(inputOne)).
		answers(settleToolCallAnswer("send", callID, arguments)).
		delivers(note)
	script.expect("model-entry", "the first boot ends on its answer", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answerOne)).
		delivers(answerOne)
	script.expect("model-entry", "the restarted run speaks from the replayed chain", requestTail(inputTwo)).
		answers(scriptedAnswerOf(answerTwo)).
		delivers(answerTwo)
	provider.decide = fidelityGate(script.decide)

	sender := newRecordingSender()
	host, configPath := bootFidelityHost(t, provider, sender, t.TempDir(), "    max_tokens: 32000\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-request-fidelity-cold-boot"}

	turn, err := driveIntegrityTurn(ctx, host, addr, inputOne)
	if err != nil {
		t.Fatalf("the tool-call turn of the first boot ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("the tool-call turn of the first boot = (%v, %d decisions), want the completed turn of its two decisions", turn.Terminal, len(turn.Decisions))
	}
	closeIntegrityHost(t, ctx, host)

	rebooted, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("re-boot the fidelity host over the same storage root: %v", err)
	}
	keepIntegrityHost(t, script, rebooted)
	turnTwo, turnTwoErr := driveIntegrityTurn(ctx, rebooted, addr, inputTwo)

	var target scriptedRequest
	for _, request := range provider.servedRequests() {
		if requestTail(inputTwo)(request) {
			target = request
		}
	}
	requireFidelityPairing(t, provider, "the turn after a cold boot")
	if fidelityCountToolMessages(target) == 0 {
		t.Fatalf("the cold-boot request carries no role=tool call-result material, so the pairing check proves nothing: %+v", target.Messages)
	}
	requireCallResultPair(t, target, callID, "send", arguments)
	if turnTwoErr != nil {
		t.Fatalf("the turn after a cold boot ended by error: %v", turnTwoErr)
	}
	if turnTwo.Terminal != model.KindCompleted {
		t.Fatalf("the turn after a cold boot = (%v, %d decisions), want the completed turn", turnTwo.Terminal, len(turnTwo.Decisions))
	}
	t.Logf("[fidelity 3] positive: the cold-boot request carries the complete pair of %q beside its tool result", callID)
	closeIntegrityHost(t, ctx, rebooted)
}

// TestRequestFidelityProtocolGate requires a request whose call-result pairing was broken to be refused with a 400.
//   - a stripped tool_call_id and an orphan role=tool answer name the violation, so a green fidelity run is never a vacuous pass
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestRequestFidelityProtocolGate(t *testing.T) {
	t.Run("stripped_tool_call_id_is_refused", func(t *testing.T) {
		const broken = `{"model":"model-entry","messages":[` +
			`{"role":"user","content":"send the note"},` +
			`{"role":"assistant","content":"","tool_calls":[{"id":"call-strip","type":"function","function":{"name":"send","arguments":"{\"content\":\"the note\"}"}}]},` +
			`{"role":"tool","content":"the note"}]}`
		status, body := postFidelityRequest(t, broken)
		if status != http.StatusBadRequest {
			t.Fatalf("the endpoint answered a request whose tool_call_id was stripped with %d (%s), want 400", status, body)
		}
		if !strings.Contains(body, "tool_call_id") {
			t.Fatalf("the refusal = %s, want the pairing violation named (tool_call_id)", body)
		}
		t.Logf("[fidelity negative] stripped tool_call_id → %d: %s", status, body)
	})
	t.Run("unanswered_tool_call_id_is_refused", func(t *testing.T) {
		const broken = `{"model":"model-entry","messages":[` +
			`{"role":"user","content":"send the note"},` +
			`{"role":"tool","tool_call_id":"call-orphan","content":"the note"}]}`
		status, body := postFidelityRequest(t, broken)
		if status != http.StatusBadRequest {
			t.Fatalf("the endpoint answered a role=tool message no assistant tool_calls answered with %d (%s), want 400", status, body)
		}
		if !strings.Contains(body, "tool_call_id") {
			t.Fatalf("the refusal = %s, want the pairing violation named (tool_call_id)", body)
		}
		t.Logf("[fidelity negative] unpaired tool_call_id → %d: %s", status, body)
	})
}

// postFidelityRequest posts one raw chat completion body to the gated endpoint
// and answers the status and payload the endpoint served. The gate runs over
// the wire exactly as the scenarios' requests run over it.
func postFidelityRequest(t *testing.T, body string) (int, string) {
	t.Helper()
	provider := newScriptedProvider(t)
	provider.decide = fidelityGate(func(scriptedRequest) (string, error) {
		return scriptedTerminalAnswer, nil
	})
	response, err := http.Post(provider.server.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post the crafted request to the gated endpoint: %v", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read the gated endpoint's answer: %v", err)
	}
	return response.StatusCode, string(payload)
}
