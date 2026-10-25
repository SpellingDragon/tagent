// The capability-gap wiring checks of the host: the counter face the face
// publishes, the OTel switch the document declares, the exec face the document
// grants, the risk table the governance section carries and the delegation id
// the host now contributes. Every check boots a real host document, so the
// declaration surface the model reads is the one the composition root built.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	osexec "os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// TestMetricsEndpointServesCounters reads the counter face the host publishes:
//   - GET /metrics answers the Prometheus text exposition under its counter types
//   - a freshly booted host reads no admitted turn
//   - the invoked turn moves tagent_turns_total beside the facts and delivery it committed
//   - a method the counter face does not serve is refused instead of read as a scrape
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestMetricsEndpointServesCounters(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if host.FaceAddr() == "" {
		t.Fatal("the host face listens on no address")
	}

	body := getMetrics(t, host.FaceAddr())
	if !strings.Contains(body, "# TYPE tagent_turns_total counter") {
		t.Fatalf("metrics body = %q, want the counter declaration of tagent_turns_total", body)
	}
	if turns, found := metricValue(body, "tagent_turns_total"); !found || turns != 0 {
		t.Fatalf("tagent_turns_total before any invocation = %v (found %v), want the zero of a fresh host", turns, found)
	}

	if turn := invokeScriptedTurn(t, ctx, host, "session-metrics"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
	}

	body = getMetrics(t, host.FaceAddr())
	if turns, found := metricValue(body, "tagent_turns_total"); !found || turns < 1 {
		t.Fatalf("tagent_turns_total = %v (found %v), want the one admitted turn: %q", turns, found, body)
	}
	if facts, found := metricValue(body, "tagent_facts_total"); !found || facts < 1 {
		t.Fatalf("tagent_facts_total = %v (found %v), want the facts the turn committed: %q", facts, found, body)
	}
	if deliveries, found := metricValue(body, "tagent_deliveries_total"); !found || deliveries < 1 {
		t.Fatalf("tagent_deliveries_total = %v (found %v), want the answer the delivery face accepted: %q", deliveries, found, body)
	}
	resp, err := http.Post("http://"+host.FaceAddr()+"/metrics", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("post to the counter face: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /metrics status = %d, want the method refusal of the read-only counter face", resp.StatusCode)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestCallSubagentGrantableByHostDocument grants the delegation id through the document:
//   - the document declaring {kind: tool, id: call_subagent} boots instead of being refused
//   - the declaration surface the model reads carries the call_subagent tool
//   - a document naming a tool the host does not contribute is still refused by name
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestCallSubagentGrantableByHostDocument(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	root := t.TempDir()
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: " + subagent.CallSubagentRef + " }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, ""))
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot a document granting %q: %v", subagent.CallSubagentRef, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-call-subagent"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
	}
	requests := provider.recorded()
	if len(requests) == 0 {
		t.Fatal("the provider saw no request, want the turn's decision")
	}
	names := requestToolNames(t, requests[0])
	if !wiringContains(names, subagent.CallSubagentRef) {
		t.Fatalf("request tools = %v, want the declared %q among them", names, subagent.CallSubagentRef)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}

	misspelledRoot := t.TempDir()
	misspelledPath := writeScriptedHostDocument(t, misspelledRoot, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, misspelledPath, wiringHostDocument(misspelledRoot, provider.endpoint(),
		"    model: model-a\n"+
			"    tools:\n"+
			"      - { kind: tool, id: call_subagents }\n", ""))
	if _, err := tagent.Boot(misspelledPath, tagent.WithSender(recordingSender{})); !errors.Is(err, tagent.ErrUnknownHostTool) {
		t.Fatalf("boot of the misspelled id = %v, want the named unknown-host-tool refusal", err)
	}
}

// TestRiskRulesFlowFromDocument reads the risk table the governance section declares:
//   - a declared send=critical rule holds the call instead of executing it
//   - the hold registers the approval request the declared grade was rendered at
//   - the model reads the approval wait in its next request
//   - the held call delivers nothing, so the counter face commits no side effect
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestRiskRulesFlowFromDocument(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		if index == 0 {
			return scriptedToolCallAnswer
		}
		return scriptedTerminalAnswer
	}
	root := t.TempDir()
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: send }\n"
	topBlock := "governance:\n" +
		"  risk_rules:\n" +
		"    - { tool: send, level: critical }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, topBlock))
	approvals := approval.NewApprovalManager("", time.Hour)
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender), tagent.WithApprovals(approvals))
	if err != nil {
		t.Fatalf("boot a document declaring a critical risk rule: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-risk-rules"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the turn to close after the held call", turn.Terminal)
	}

	pending := approvals.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending approvals = %+v, want the one request the critical call registered", pending)
	}
	held := pending[0]
	if held.ToolName != "send" || held.RiskLevel != plugin.RiskCritical || held.Status != approval.ApprovalPending {
		t.Fatalf("pending request = %+v, want the send call held at the critical grade", held)
	}
	if !strings.Contains(held.Reason, "critical risk awaits approval") {
		t.Fatalf("pending request reason = %q, want the wait the critical grade renders", held.Reason)
	}
	for _, body := range sender.bodies() {
		if body == "hi" {
			t.Fatalf("delivered bodies = %q, want the held send call to execute nothing", sender.bodies())
		}
	}

	requests := provider.recorded()
	if len(requests) < 2 {
		t.Fatalf("model requests = %d, want the decision beside the request that read the hold", len(requests))
	}
	waitSeen := false
	for _, body := range requests[1:] {
		for _, message := range settleRequestTexts(t, body) {
			if strings.Contains(message.content, "parked awaiting approval") {
				waitSeen = true
			}
		}
	}
	if !waitSeen {
		t.Fatalf("no request carried the approval wait, want the hold the declared rule rendered")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestRiskRulesAbsentKeepsTheSendFace proves the declared table is the only source of the risk face:
//   - a document beside no risk_rules boots and parks nothing
//   - the declared send tool delivers its content, so the default grade holds no call
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestRiskRulesAbsentKeepsTheSendFace(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		if index == 0 {
			return scriptedToolCallAnswer
		}
		return scriptedTerminalAnswer
	}
	root := t.TempDir()
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: send }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, ""))
	approvals := approval.NewApprovalManager("", time.Hour)
	sender := &deliveryRecorder{}
	host, err := tagent.Boot(configPath, tagent.WithSender(sender), tagent.WithApprovals(approvals))
	if err != nil {
		t.Fatalf("boot a document beside no risk rules: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-no-risk-rules"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the delivered send", turn.Terminal)
	}
	if pending := approvals.Pending(); len(pending) != 0 {
		t.Fatalf("pending approvals = %+v, want no hold beside no declared rule", pending)
	}
	delivered := false
	for _, body := range sender.bodies() {
		if body == "hi" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("delivered bodies = %q, want the declared send tool to deliver its content", sender.bodies())
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestExecFaceWiredFromDocument drives the exec face one document grants:
//   - a document declaring the exec id boots the run beside the release tool
//   - the run tool acquires and reports the frame the declared command wrote
//   - the release tool stops the very resource the run started
//   - no request answers ErrUnwired, so the boot wired the shared resource ledger
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecFaceWiredFromDocument(t *testing.T) {
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("tmux is not available on this runner")
	}
	isolateTmuxServer(t)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	resource := fmt.Sprintf("tagent-wiring-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = osexec.Command("tmux", "kill-session", "-t", resource).Run() })
	provider.answer = func(index int) string {
		switch index {
		case 0:
			return wiringToolCallsAnswer(wiringCall{
				id:   "call-exec-run",
				name: "exec_run",
				arguments: fmt.Sprintf(`{"command":"echo","args":["wiring-ok"],"kind":%q,"resource":%q}`,
					tagentexec.KindTmux, resource),
			})
		case 1:
			return wiringToolCallsAnswer(wiringCall{
				id:        "call-exec-release",
				name:      "exec_release",
				arguments: fmt.Sprintf(`{"kind":%q,"resource":%q}`, tagentexec.KindTmux, resource),
			})
		default:
			return scriptedTerminalAnswer
		}
	}
	root := t.TempDir()
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: " + tagentexec.PresetName + " }\n" +
		"    exec:\n" +
		"      monitor: { interval: 20ms, stable_duration: 40ms, interactive_stable_duration: 40ms, fake_dead_duration: 200ms }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, ""))
	host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
	if err != nil {
		t.Fatalf("boot a document granting %q: %v", tagentexec.PresetName, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-exec-wiring"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the run beside its release", turn.Terminal)
	}
	requests := provider.recorded()
	if len(requests) != 3 {
		t.Fatalf("model requests = %d, want the run decision, the release decision and the closing answer", len(requests))
	}
	runSeen := false
	for _, message := range settleRequestTexts(t, requests[1]) {
		if strings.Contains(message.content, "executed "+tagentexec.KindTmux+"/"+resource) &&
			strings.Contains(message.content, "wiring-ok") {
			runSeen = true
		}
	}
	if !runSeen {
		t.Fatalf("the release request carries no run result for %s/%s: %+v",
			tagentexec.KindTmux, resource, settleRequestTexts(t, requests[1]))
	}
	releaseSeen := false
	for _, message := range settleRequestTexts(t, requests[2]) {
		if strings.Contains(message.content, "released "+tagentexec.KindTmux+"/"+resource+" after confirmed stop") {
			releaseSeen = true
		}
	}
	if !releaseSeen {
		t.Fatalf("the closing request carries no confirmed release of %s/%s: %+v",
			tagentexec.KindTmux, resource, settleRequestTexts(t, requests[2]))
	}
	for _, body := range requests {
		for _, message := range settleRequestTexts(t, body) {
			if strings.Contains(message.content, tagentexec.ErrUnwired.Error()) {
				t.Fatalf("a request carried the unwired refusal %q, want the declared exec face wired",
					tagentexec.ErrUnwired)
			}
		}
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestExecFaceDeclaredBesideNoTmuxRefusesTheBoot proves the declared face is wired or refused, never run unwired:
//   - a document declaring the exec id on a host without tmux refuses the boot
//   - the refusal names the declared face instead of booting a run beside no ledger
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestExecFaceDeclaredBesideNoTmuxRefusesTheBoot(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	provider := newScriptedProvider(t)
	root := t.TempDir()
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: " + tagentexec.PresetName + " }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, ""))
	if _, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{})); err == nil {
		t.Fatal("boot of a declared exec face beside no tmux = nil, want the named refusal")
	} else if !strings.Contains(err.Error(), tagentexec.PresetName) {
		t.Fatalf("boot error = %v, want the declared %q face named", err, tagentexec.PresetName)
	}
}

// TestTelemetrySwitchFromDocument pins the telemetry section the document declares:
//   - otel_enabled boots the host beside the SDK observer and the turn completes
//   - a key the telemetry section does not name refuses the boot by name
//   - an endpoint beside the disabled switch refuses the boot instead of being read past
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestTelemetrySwitchFromDocument(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Run("otel_enabled boots beside the SDK observer", func(t *testing.T) {
		provider := newScriptedProvider(t)
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(),
			"    model: model-a\n", "telemetry:\n  otel_enabled: true\n"))
		host, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
		if err != nil {
			t.Fatalf("boot a document enabling OTel: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if turn := invokeScriptedTurn(t, ctx, host, "session-otel"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn beside the SDK observer", turn.Terminal)
		}
		if err := host.Close(ctx); err != nil {
			t.Fatalf("close the host: %v", err)
		}
	})
	t.Run("an unknown telemetry key refuses the boot", func(t *testing.T) {
		provider := newScriptedProvider(t)
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(),
			"    model: model-a\n", "telemetry:\n  otel_enable: true\n"))
		_, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
		if err == nil || !strings.Contains(err.Error(), "otel_enable") {
			t.Fatalf("boot of the misspelled telemetry key = %v, want the unknown key named", err)
		}
	})
	t.Run("an endpoint beside the disabled switch refuses the boot", func(t *testing.T) {
		provider := newScriptedProvider(t)
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(),
			"    model: model-a\n", "telemetry:\n  otel_enabled: false\n  endpoint: localhost:4317\n"))
		_, err := tagent.Boot(configPath, tagent.WithSender(recordingSender{}))
		if err == nil || !strings.Contains(err.Error(), "endpoint") {
			t.Fatalf("boot of an endpoint beside otel_enabled false = %v, want the endpoint named", err)
		}
	})
}

// wiringHostDocument renders one host document whose entry agent section carries
// the given block of agent fields and whose top level carries the given sections
// beside the storage tier.
func wiringHostDocument(root, endpoint, agentBlock, topBlock string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-a\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	if strings.TrimSpace(agentBlock) == "" {
		b.WriteString("  user: {}\n")
	} else {
		b.WriteString("  user:\n")
		b.WriteString(agentBlock)
	}
	b.WriteString(topBlock)
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// wiringCall is one tool call a scripted decision issues.
type wiringCall struct {
	id        string
	name      string
	arguments string
}

// wiringToolCallsAnswer renders the answer of one decision that issues the given
// tool calls in order.
func wiringToolCallsAnswer(calls ...wiringCall) string {
	encoded := make([]string, 0, len(calls))
	for _, call := range calls {
		args, err := json.Marshal(call.arguments)
		if err != nil {
			panic("host: encode the scripted tool arguments: " + err.Error())
		}
		encoded = append(encoded, `{"id":"`+call.id+`","type":"function","function":{"name":"`+call.name+
			`","arguments":`+string(args)+`}}`)
	}
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"working","tool_calls":[` +
		strings.Join(encoded, ",") +
		`]},"finish_reason":"tool_calls"}]}`
}

// getMetrics reads the counter face of one host and answers its body.
func getMetrics(t *testing.T, faceAddr string) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://" + faceAddr + "/metrics")
	if err != nil {
		t.Fatalf("get the metrics endpoint: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, hostBodyLimit))
	if err != nil {
		t.Fatalf("read the metrics answer: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200: %s", resp.StatusCode, body)
	}
	return string(body)
}

// metricValue reads the value one counter of the exposition carries.
func metricValue(body, name string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, name+" ") {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, name+" ")), 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

// requestToolNames reads the tool names one recorded request declared to the model.
func requestToolNames(t *testing.T, body map[string]json.RawMessage) []string {
	t.Helper()
	raw, ok := body["tools"]
	if !ok {
		return nil
	}
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("decode the request tools: %v", err)
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

// wiringContains reports whether names carries want.
func wiringContains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// isolateTmuxServer points this check at a tmux server of its own: another
// package finishing the shared server must not take the declared execution with
// it, and the socket directory stays short enough for the platform limit.
func isolateTmuxServer(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tagent-tmux-")
	if err != nil {
		t.Fatalf("create the private tmux directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
}
