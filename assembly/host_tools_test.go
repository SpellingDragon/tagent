package assembly

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestHostSendAssemblyRules pins the two assembly rules the send tool obeys at boot.
// - boots_beside_no_sender: a document that grants it beside no outbound sender boots, because a send reaches no channel and demands no sender (design DEL-6)
// - refuses_white_list_beside_no_send: a reflection declaring a deliver_to white list carries that very tool, the delivery being its own call
// - a reflection declaring targets beside no send tool is refused by name, and no host comes back from it
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestHostSendAssemblyRules(t *testing.T) {
	t.Run("boots_beside_no_sender", runBootsBesideNoSender)
	t.Run("refuses_white_list_beside_no_send", runRefusesWhiteListBesideNoSend)
}

// runBootsBesideNoSender requires a document that grants the send tool to boot
// beside no sender: the tool reaches no channel, so the grant is no demand for
// one, and the boot that accepted the document stays usable.
func runBootsBesideNoSender(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	root := t.TempDir()
	host, err := Boot(sendRulesDocument(t, root, []string{hostSendToolID}, nil))
	if err != nil {
		t.Fatalf("boot the document that grants the %q tool beside no sender: %v", hostSendToolID, err)
	}
	if host == nil {
		t.Fatal("the boot accepted the document and returned no host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host booted beside no sender: %v", err)
	}
}

// runRefusesWhiteListBesideNoSend requires a reflection that declares a
// deliver_to white list beside no send tool to be refused by name before any
// host is published: the reflection turn writes its delivery as its own call, so
// a white list beside no tool is a delivery nothing could ever make.
func runRefusesWhiteListBesideNoSend(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	root := t.TempDir()
	host, err := Boot(sendRulesDocument(t, root, []string{hostSendToolID}, []string{}))
	if host != nil {
		_ = host.Close(context.Background())
		t.Fatal("a refused boot returned a host")
	}
	if !errors.Is(err, ErrHostMeditationToolMisplaced) {
		t.Fatalf("boot error = %v, want the misplaced tool refusal %v", err, ErrHostMeditationToolMisplaced)
	}
	if !strings.Contains(err.Error(), hostSendToolID) {
		t.Fatalf("boot error = %v, want it to name the missing %q grant", err, hostSendToolID)
	}
}

// sendRulesDocument writes one host document over the temp root, granting the
// named tools to the entry agent and, when the meditation session is asked for,
// the named tools to that session beside its reflection plan and its declared
// deliver_to white list. It answers the path of the document it wrote.
func sendRulesDocument(t *testing.T, root string, userTools, meditationTools []string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: scripted\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"http://127.0.0.1:1/v1\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    tools:\n")
	for _, name := range userTools {
		b.WriteString("      - { kind: tool, id: " + name + " }\n")
	}
	if meditationTools != nil {
		b.WriteString("  meditator:\n")
		if len(meditationTools) == 0 {
			b.WriteString("    tools: []\n")
		} else {
			b.WriteString("    tools:\n")
			for _, name := range meditationTools {
				b.WriteString("      - { kind: tool, id: " + name + " }\n")
			}
		}
		b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [user] }\n")
		b.WriteString("    meditation:\n")
		b.WriteString("      enabled: true\n")
		b.WriteString("      interval: 1h\n")
		b.WriteString("      min_gap: 2h\n")
		b.WriteString("      prompt_file: meditation.md\n")
		b.WriteString("      observed_namespaces: [user]\n")
		b.WriteString("      deliver_to: [user]\n")
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if meditationTools != nil {
		if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the session"), 0o644); err != nil {
			t.Fatalf("write the meditation prompt: %v", err)
		}
	}
	return configPath
}

// TestSendTargetConstants pins the frozen dialect of one send call to the spelling design D6 states.
// - a rename on either side of that contract is caught before the model reads it
// - the five argument keys carry the frozen spelling, and so do the three receipts
// - the argument structs declare exactly those keys as their JSON tags
// - a dialect that drifts on either side of the constant is named, so the check is sensitive to the rename it exists to refuse
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestSendTargetConstants(t *testing.T) {
	declared := map[string]string{
		"target":  "target",
		"content": "content",
		"task":    "task",
		"agent":   "agent",
		"session": "session",
	}
	inUse := map[string]string{
		"target":  hostSendTargetKey,
		"content": hostSendContentKey,
		"task":    hostSendTargetTaskKey,
		"agent":   hostSendTargetAgentKey,
		"session": hostSendTargetSessionKey,
	}
	if drift := sendDialectDrift(declared, inUse); len(drift) > 0 {
		t.Fatalf("argument keys drift from design D6 = %v", drift)
	}
	receipts := map[string]string{
		"hostSendReceiptOK":    hostSendReceiptOK,
		"hostSendDeniedPrefix": hostSendDeniedPrefix,
	}
	wantReceipts := map[string]string{
		"hostSendReceiptOK":    "ok",
		"hostSendDeniedPrefix": "[send_denied] ",
	}
	if drift := sendDialectDrift(wantReceipts, receipts); len(drift) > 0 {
		t.Fatalf("receipts drift from design D6 = %v", drift)
	}
	argsKeys := sendTagKeys(hostSendArgs{})
	if drift := sendDialectDrift(map[string]string{"target": "target", "content": "content"}, argsKeys); len(drift) > 0 {
		t.Fatalf("hostSendArgs tags = %v, want the target and content keys", argsKeys)
	}
	targetKeys := sendTagKeys(hostSendTarget{})
	wantTarget := map[string]string{"task": "task", "agent": "agent", "session": "session"}
	if drift := sendDialectDrift(wantTarget, targetKeys); len(drift) > 0 {
		t.Fatalf("hostSendTarget tags = %v, want the task, agent and session keys", targetKeys)
	}

	t.Run("rejects_dialect_drift", func(t *testing.T) {
		if drift := sendDialectDrift(declared, map[string]string{"target": "to", "content": "body"}); len(drift) == 0 {
			t.Fatal("a tag renamed beside its constant is reported as no drift")
		}
		if drift := sendDialectDrift(declared, map[string]string{
			"to":      "to",
			"body":    "body",
			"task":    "task",
			"agent":   "agent",
			"session": "session",
		}); len(drift) == 0 {
			t.Fatal("a constant renamed beside its tag is reported as no drift")
		}
		if drift := sendDialectDrift(map[string]string{"hostSendReceiptOK": "ok"}, map[string]string{"hostSendReceiptOK": "delivered"}); len(drift) == 0 {
			t.Fatal("a receipt renamed beside design D6 is reported as no drift")
		}
	})

	t.Run("held_word_never_returns", func(t *testing.T) {
		scanHeldWordAbsent(t)
	})
}

// heldReceiptWord is the retired third receipt of the send dialect: the word a body held for
// review used to be answered with, kept here as the one spelling a scan refuses. The spelling
// is assembled from two pieces so this anchor does not report itself.
const heldReceiptWord = "[send_" + "held] "

// scanHeldWordAbsent refuses any return of the retired hold receipt: the word is read over the
// product packages, the anchors that drive them and the specs that promise them, so a
// reintroduction of the third state is red wherever it is written and not only in the
// constant this package holds. Two scopes stay outside the scan on purpose: the archived
// changes keep what the dialect was, and rewriting them would falsify the history they
// record, and the deployment examples are prose of their own that the spec face does not
// own.
func scanHeldWordAbsent(t *testing.T) {
	t.Helper()
	roots := []string{"assembly", "tool", "runtime", "tests", filepath.Join("openspec", "specs")}
	root := ".."
	for _, scope := range roots {
		base := filepath.Join(root, scope)
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				if name := entry.Name(); name == ".git" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil || info.Size() > 2<<20 {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(string(body), heldReceiptWord) {
				t.Errorf("%s carries the retired hold receipt %q: the send dialect answers two states, and a body waiting on a review is held by the admission receipt beside this tool", path, heldReceiptWord)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", base, err)
		}
	}
}

// TestSendTargetResolution pins the destination surface of one send call: the one destination a call names.
// - every shape naming none or naming two is refused by name
// - a task destination resolves and the body reaches the re-entry face of that task
// - a session destination is refused by name beside no face carrying it, with no fallback to an outbound delivery
// - both destinations at once, one of the agent and session pair alone, an absent target and an outside field are each refused
// - an empty and an oversized body are refused, and the three dispatch outcomes answer their own receipt
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestSendTargetResolution(t *testing.T) {
	t.Run("resolves a task destination", func(t *testing.T) {
		reentry := &sendReentryStub{outcome: deliverySent}
		tool := &hostSendTool{reentry: reentry}
		if got := sendCall(t, tool, `{"target":{"task":" task-1 "},"content":"review it again"}`); got != hostSendReceiptOK {
			t.Fatalf("receipt = %q, want %q", got, hostSendReceiptOK)
		}
		if reentry.calls != 1 || reentry.task != "task-1" || reentry.content != "review it again" {
			t.Fatalf("re-entry = %d calls of %q with %q, want the one call of the trimmed task",
				reentry.calls, reentry.task, reentry.content)
		}
	})

	t.Run("passes the outcomes through", func(t *testing.T) {
		cases := []struct {
			name    string
			outcome deliveryOutcome
			cause   string
			want    string
		}{
			{"sent", deliverySent, "", hostSendReceiptOK},
			{"held", deliveryHeld, "parked for review", hostSendDeniedPrefix + "parked for review"},
			{"refused", deliveryRefused, "the task is not settled", hostSendDeniedPrefix + "the task is not settled"},
		}
		for _, one := range cases {
			tool := &hostSendTool{reentry: &sendReentryStub{outcome: one.outcome, cause: one.cause}}
			if got := sendCall(t, tool, `{"target":{"task":"task-1"},"content":"body"}`); got != one.want {
				t.Fatalf("%s receipt = %q, want %q", one.name, got, one.want)
			}
		}
	})

	t.Run("rejects_a_session_destination_without_a_face", func(t *testing.T) {
		reentry := &sendReentryStub{outcome: deliverySent}
		tool := &hostSendTool{reentry: reentry}
		got := sendCall(t, tool, `{"target":{"agent":"agent-a","session":"session-a"},"content":"body"}`)
		if !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "cross-session") {
			t.Fatalf("session destination receipt = %q, want the named refusal of a missing face", got)
		}
		if reentry.calls != 0 {
			t.Fatalf("re-entry calls = %d, want none for a session destination", reentry.calls)
		}
	})

	t.Run("rejects_both_targets", func(t *testing.T) {
		tool := &hostSendTool{reentry: &sendReentryStub{outcome: deliverySent}}
		got := sendCall(t, tool, `{"target":{"task":"task-1","agent":"agent-a","session":"session-a"},"content":"body"}`)
		if !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "exactly one destination") {
			t.Fatalf("both-target receipt = %q, want the named refusal of two destinations", got)
		}
		if got := sendCall(t, tool, `{"target":{"task":"task-1","session":"session-a"},"content":"body"}`); !strings.Contains(got, "exactly one destination") {
			t.Fatalf("task beside a session receipt = %q, want the named refusal of two destinations", got)
		}
	})

	t.Run("rejects_missing_target", func(t *testing.T) {
		tool := &hostSendTool{reentry: &sendReentryStub{outcome: deliverySent}}
		for _, args := range []string{
			`{"content":"body"}`,
			`{"target":null,"content":"body"}`,
			`{"target":{},"content":"body"}`,
			`{"target":{"task":"   "},"content":"body"}`,
		} {
			if got := sendCall(t, tool, args); !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "name no target") {
				t.Fatalf("%s receipt = %q, want the named refusal of an absent destination", args, got)
			}
		}
		if got := sendCall(t, tool, `{"target":{"agent":"agent-a"},"content":"body"}`); !strings.Contains(got, "names both") {
			t.Fatalf("half a session destination receipt = %q, want the named refusal of the incomplete pair", got)
		}
	})

	t.Run("rejects_unknown_fields", func(t *testing.T) {
		tool := &hostSendTool{reentry: &sendReentryStub{outcome: deliverySent}}
		got := sendCall(t, tool, `{"target":{"task":"task-1"},"content":"body","channel":"wechat"}`)
		if !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "'channel'") {
			t.Fatalf("unknown argument receipt = %q, want the named refusal of the extra field", got)
		}
		got = sendCall(t, tool, `{"target":{"task":"task-1","channel":"wechat"},"content":"body"}`)
		if !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "'channel'") {
			t.Fatalf("unknown target field receipt = %q, want the named refusal of the extra field", got)
		}
	})

	t.Run("rejects_an_empty_and_an_oversized_body", func(t *testing.T) {
		tool := &hostSendTool{reentry: &sendReentryStub{outcome: deliverySent}}
		if got := sendCall(t, tool, `{"target":{"task":"task-1"},"content":""}`); !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "empty") {
			t.Fatalf("empty body receipt = %q, want the named refusal", got)
		}
		oversized := strings.Repeat("x", hostSendContentMax+1)
		if got := sendCall(t, tool, `{"target":{"task":"task-1"},"content":"`+oversized+`"}`); !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "8KiB") {
			t.Fatalf("oversized body receipt = %q, want the named refusal", got)
		}
	})

	t.Run("rejects_no_reentry_face_beside_a_task", func(t *testing.T) {
		tool := &hostSendTool{}
		got := sendCall(t, tool, `{"target":{"task":"task-1"},"content":"body"}`)
		if !strings.HasPrefix(got, hostSendDeniedPrefix) || !strings.Contains(got, "task re-entry") {
			t.Fatalf("receipt beside no re-entry face = %q, want the named refusal", got)
		}
	})
}

// sendReentryStub records the one dispatch a call hands to the re-entry face and
// answers the outcome the check fixed.
type sendReentryStub struct {
	outcome deliveryOutcome
	cause   string
	calls   int
	task    event.TaskID
	content string
}

// ReenterTask implements the re-entry face the tool dispatches through.
func (s *sendReentryStub) ReenterTask(_ context.Context, taskID event.TaskID, content string) (deliveryOutcome, string) {
	s.calls++
	s.task = taskID
	s.content = content
	return s.outcome, s.cause
}

// sendCall drives the send tool with one argument body and reports its result text.
func sendCall(t *testing.T, tool *hostSendTool, args string) string {
	t.Helper()
	out, err := tool.Call(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("send call %s reports the protocol fault %v", args, err)
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("send call %s = %#v, want one result text", args, out)
	}
	return text
}

// sendTagKeys reports the JSON keys one argument struct declares, as the spelling in
// use, so the dialect constants are checked against the shape the model reaches.
func sendTagKeys(shape any) map[string]string {
	fields := reflect.TypeOf(shape)
	out := make(map[string]string, fields.NumField())
	for i := 0; i < fields.NumField(); i++ {
		key := strings.Split(fields.Field(i).Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		out[key] = key
	}
	return out
}

// TestSendDeclarationRewrite pins the declaration of the send tool: the one body it hands across a task or session boundary.
// - the target object names the destination, and the declaration promises no channel of its own
// - the description states the cross-task, cross-session communication and carries no channel wording
// - a text still carrying that promise is reported, so the check is sensitive to the wording it exists to refuse
// - the input schema requires the target object beside the body, and the target object declares the three destination keys as strings
// - the argument surface carries the two declared keys and nothing else
// 契约: docs/wiki/examples/wechat-bot-runtime.md#delivery-receipts
func TestSendDeclarationRewrite(t *testing.T) {
	declaration := (&hostSendTool{}).Declaration()
	if declaration.Name != hostSendToolID {
		t.Fatalf("declaration name = %q, want %q", declaration.Name, hostSendToolID)
	}
	description := declaration.Description
	if wording := sendChannelWording(description); len(wording) > 0 {
		t.Fatalf("the declaration still promises a channel = %v; the tool delivers across tasks and sessions only", wording)
	}
	for _, promise := range []string{"across tasks and sessions", hostSendTargetKey} {
		if !strings.Contains(description, promise) {
			t.Fatalf("the declaration = %q, want it to name %q", description, promise)
		}
	}

	schema := declaration.InputSchema
	if schema == nil || schema.Type != "object" {
		t.Fatalf("input schema = %+v, want the object the call is read against", schema)
	}
	if got := sendSchemaKeys(schema); !reflect.DeepEqual(got, []string{hostSendContentKey, hostSendTargetKey}) {
		t.Fatalf("input schema properties = %v, want the two declared arguments", got)
	}
	if got := append([]string(nil), schema.Required...); len(got) != 2 {
		t.Fatalf("required arguments = %v, want both declared arguments", got)
	}
	for _, key := range []string{hostSendTargetKey, hostSendContentKey} {
		if !sendSchemaRequires(schema, key) {
			t.Fatalf("required arguments = %v, want %q among them", schema.Required, key)
		}
	}

	target := schema.Properties[hostSendTargetKey]
	if target == nil || target.Type != "object" {
		t.Fatalf("the target argument = %+v, want the destination object", target)
	}
	if got := sendSchemaKeys(target); !reflect.DeepEqual(got, []string{hostSendTargetAgentKey, hostSendTargetSessionKey, hostSendTargetTaskKey}) {
		t.Fatalf("target properties = %v, want the three destination keys", got)
	}
	for name, property := range target.Properties {
		if property == nil || property.Type != "string" {
			t.Fatalf("target key %q = %+v, want one destination identity", name, property)
		}
	}
	if len(target.Required) > 0 {
		t.Fatalf("target required = %v, want no required key: a call names one destination or the other", target.Required)
	}

	t.Run("declaration_has_no_channel_wording", func(t *testing.T) {
		retired := []string{
			"Deliver one body to the user address of this call.",
			"Deliver to the user the body the call names.",
			"The target comes from the user address of the call's session.",
		}
		for _, text := range retired {
			if wording := sendChannelWording(text); len(wording) == 0 {
				t.Fatalf("the channel promise in %q was read as none", text)
			}
		}
		if wording := sendChannelWording(description); len(wording) != 0 {
			t.Fatalf("the declared description = %q still carries the channel wording %v", description, wording)
		}
	})
}

// sendChannelWording reports the channel promises one declaration text makes: every
// phrase stating that the tool reaches the user's own address, which the rewritten
// declaration must not say.
func sendChannelWording(text string) []string {
	lowered := strings.ToLower(text)
	var found []string
	for _, phrase := range []string{"user address", "deliver to the user"} {
		if strings.Contains(lowered, phrase) {
			found = append(found, "'"+phrase+"'")
		}
	}
	return found
}

// sendSchemaKeys reports the property names one schema declares, sorted so the report
// is reproducible.
func sendSchemaKeys(schema *trpctool.Schema) []string {
	out := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// sendSchemaRequires reports whether one schema requires the named argument.
func sendSchemaRequires(schema *trpctool.Schema, name string) bool {
	for _, required := range schema.Required {
		if required == name {
			return true
		}
	}
	return false
}

// sendDialectDrift reports the keys whose declared spelling and spelling in use
// disagree in either direction, sorted so the report is reproducible.
func sendDialectDrift(declared, inUse map[string]string) []string {
	var drift []string
	for key, spelling := range declared {
		used, ok := inUse[key]
		if !ok || used != spelling {
			drift = append(drift, "'"+key+"'")
			continue
		}
	}
	for key := range inUse {
		if _, ok := declared[key]; !ok {
			drift = append(drift, "'"+key+"'")
		}
	}
	sort.Strings(drift)
	return drift
}

// ledgerSource is the source event the account of the checks below files every
// delivery under: the invocation of the turn that issued the calls.
const ledgerSource = event.EventID("tagent-session:user:meditation#c1")

// ledgerTargets are the three sessions one source event reaches.
var ledgerTargets = []event.Address{
	{AgentID: "peer", SessionID: "session-ledger-one"},
	{AgentID: "peer", SessionID: "session-ledger-two"},
	{AgentID: "peer", SessionID: "session-ledger-three"},
}

// testLedgerRecords reads the delivery account for a check and refuses the check
// by name when the durable chain could not be read: a healthy chain answers no
// error, so a surfaced error is a defect the check reports rather than a partial
// view it reads as the whole account.
func testLedgerRecords(t *testing.T, l *DeliveryLedger) []DeliveryRecord {
	t.Helper()
	records, err := l.Records()
	if err != nil {
		t.Fatalf("the delivery account could not be read: %v", err)
	}
	return records
}

// testLedgerRefusals reads the refusal account for a check, refusing a read error.
func testLedgerRefusals(t *testing.T, l *DeliveryLedger) []DeliveryRefusal {
	t.Helper()
	refusals, err := l.Refusals()
	if err != nil {
		t.Fatalf("the delivery refusal account could not be read: %v", err)
	}
	return refusals
}

// testLedgerBySource reads one source event's deliveries for a check.
func testLedgerBySource(t *testing.T, l *DeliveryLedger, source event.EventID) []DeliveryRecord {
	t.Helper()
	records, err := l.BySource(source)
	if err != nil {
		t.Fatalf("the deliveries of source %s could not be read: %v", source, err)
	}
	return records
}

// testLedgerByTarget reads one target's deliveries for a check.
func testLedgerByTarget(t *testing.T, l *DeliveryLedger, target event.Address) []DeliveryRecord {
	t.Helper()
	records, err := l.ByTarget(target)
	if err != nil {
		t.Fatalf("the deliveries of target %+v could not be read: %v", target, err)
	}
	return records
}

// TestDeliveryLedgerEachTargetExactlyOnce pins the per-target account of one source event (design D9).
// - each_target_exactly_once: one source event delivered to three targets answers three records, no target twice
// - a read of one target answers only the record of that target
// - two reads of one chain answer the same records in the same order
func TestDeliveryLedgerEachTargetExactlyOnce(t *testing.T) {
	facts := make([]event.Event, 0, len(ledgerTargets))
	for _, target := range ledgerTargets {
		facts = append(facts, ledgerSent(event.EventID("ledger-sent-"+string(target.SessionID)), ledgerSource, target, "the note three sessions receive"))
	}
	ledger := ledgerTestRun(facts...).DeliveryLedger()

	records := testLedgerRecords(t, ledger)
	if len(records) != len(ledgerTargets) {
		t.Fatalf("deliveries the account lists = %d, want one per target (%d):\n%s", len(records), len(ledgerTargets), ledgerDescribe(records))
	}
	seen := map[event.Address]bool{}
	for _, record := range records {
		if seen[record.Target] {
			t.Fatalf("target %+v is listed twice, want every target exactly once:\n%s", record.Target, ledgerDescribe(records))
		}
		seen[record.Target] = true
		if record.Source != ledgerSource {
			t.Fatalf("source of the delivery to %+v = %q, want the one event that caused them %q", record.Target, record.Source, ledgerSource)
		}
		if record.Receipt != DeliveryReceiptSent {
			t.Fatalf("receipt of the delivery to %+v = %q, want %q", record.Target, record.Receipt, DeliveryReceiptSent)
		}
	}
	if got := testLedgerBySource(t, ledger, ledgerSource); len(got) != len(ledgerTargets) {
		t.Fatalf("deliveries answered by the source event = %d, want %d", len(got), len(ledgerTargets))
	}
	if got := testLedgerByTarget(t, ledger, ledgerTargets[1]); len(got) != 1 {
		t.Fatalf("deliveries answered by one target = %d, want the one body it received:\n%s", len(got), ledgerDescribe(got))
	}
	if first, second := ledgerDescribe(records), ledgerDescribe(testLedgerRecords(t, ledger)); first != second {
		t.Fatalf("two reads of one chain disagree:\n%s\n%s", first, second)
	}
}

// TestDeliveryLedgerFactTypeDistinct pins the one field the two delivery families keep apart (Q11=b, D7).
// - fact_type_distinct: a sent body stays an external input and a framework settle stays a task settle
// - the two records carry the same entry set under the frozen keys
func TestDeliveryLedgerFactTypeDistinct(t *testing.T) {
	target := ledgerTargets[0]
	run := ledgerTestRun(
		ledgerSent("ledger-sent", ledgerSource, target, "the note one session hands a peer"),
		ledgerSettled("ledger-settled", target, "the review settled"),
	)
	records := testLedgerByTarget(t, run.DeliveryLedger(), target)
	if len(records) != 2 {
		t.Fatalf("deliveries the account lists for %+v = %d, want the sent body beside the framework settle:\n%s",
			target, len(records), ledgerDescribe(records))
	}
	families := map[string]bool{}
	for _, record := range records {
		families[record.FactType] = true
	}
	if !families[event.TypeExternalInput] || !families[event.TypeTaskSettled] || len(families) != 2 {
		t.Fatalf("fact types of the two deliveries = %v, want %q and %q kept apart",
			ledgerKeysOf(families), event.TypeExternalInput, event.TypeTaskSettled)
	}
	left, right := ledgerMetaKeys(records[0].Meta()), ledgerMetaKeys(records[1].Meta())
	if strings.Join(left, ",") != strings.Join(right, ",") {
		t.Fatalf("delivery entries of the sent body = %v, of the framework settle = %v, want one isomorphic set", left, right)
	}
	if records[0].Meta()[event.MetaKeyDeliveryTarget] != records[1].Meta()[event.MetaKeyDeliveryTarget] {
		t.Fatalf("target entries = %q and %q, want the one address both reached",
			records[0].Meta()[event.MetaKeyDeliveryTarget], records[1].Meta()[event.MetaKeyDeliveryTarget])
	}
}

// TestDeliveryLedgerIdentityIsTheCommittedFact pins the dedup rule both delivery families share.
// - dedup_rule_identical: a record answers the fact key it came from, whichever family wrote it
// - a fact that repeats an identity the chain already holds lands no second record
func TestDeliveryLedgerIdentityIsTheCommittedFact(t *testing.T) {
	target := ledgerTargets[0]
	run := ledgerTestRun(
		ledgerSent("ledger-sent", ledgerSource, target, "the note one session hands a peer"),
		ledgerSettled("ledger-settled", target, "the review settled"),
	)
	for _, record := range testLedgerRecords(t, run.DeliveryLedger()) {
		if _, held := run.facts[event.EventID(record.Identity)]; !held {
			t.Fatalf("identity %q of the delivery to %+v names no fact of this run", record.Identity, record.Target)
		}
		if run.facts[event.EventID(record.Identity)].Type != record.FactType {
			t.Fatalf("fact type of %q = %q, want the %q the record states",
				record.Identity, run.facts[event.EventID(record.Identity)].Type, record.FactType)
		}
	}

	repeat := ledgerTestRun(
		ledgerSent("ledger-sent", ledgerSource, target, "the note one session hands a peer"),
		ledgerSent("ledger-sent", ledgerSource, target, "the same call presented again"),
	)
	if got := testLedgerRecords(t, repeat.DeliveryLedger()); len(got) != 1 {
		t.Fatalf("deliveries listed for one identity presented twice = %d, want the one the chain holds:\n%s",
			len(got), ledgerDescribe(got))
	}
}

// TestDeliveryLedgerQueryableWithoutParsingBody pins the read face of the account: entries, never body text.
// - ledger_queryable_without_parsing_body: a record is answered although no body carries its identity
// - a body-less delivery is accounted for too
// - a run that was never assembled answers no account instead of panicking
func TestDeliveryLedgerQueryableWithoutParsingBody(t *testing.T) {
	bodies := []string{"the note three sessions receive", "", "the review settled"}
	run := ledgerTestRun(
		ledgerSent("ledger-sent", ledgerSource, ledgerTargets[0], bodies[0]),
		ledgerSent("ledger-quiet", ledgerSource, ledgerTargets[1], bodies[1]),
		ledgerSettled("ledger-settled", ledgerTargets[2], bodies[2]),
	)
	records := testLedgerRecords(t, run.DeliveryLedger())
	if len(records) != len(bodies) {
		t.Fatalf("deliveries the account lists = %d, want one per delivered target (%d):\n%s", len(records), len(bodies), ledgerDescribe(records))
	}
	for _, record := range records {
		for _, body := range bodies {
			if strings.Contains(body, record.Identity) {
				t.Fatalf("body %q carries the identity %q, so the account was read out of the text instead of its entries", body, record.Identity)
			}
		}
	}
	var unassembled *Run
	if ledger := unassembled.DeliveryLedger(); ledger != nil {
		t.Fatalf("the account of a run that was never assembled = %+v, want none", ledger)
	}
	if got := testLedgerRecords(t, unassembled.DeliveryLedger()); len(got) != 0 {
		t.Fatalf("records of an unassembled account = %d, want none:\n%s", len(got), ledgerDescribe(got))
	}
}

// ledgerTestRun assembles a run holding the named facts in the order given, so the
// account is read off a chain the check states itself.
func ledgerTestRun(facts ...event.Event) *Run {
	run := &Run{facts: make(map[event.EventID]event.Event, len(facts))}
	for _, fact := range facts {
		run.facts[fact.Key] = fact
		run.factOrder = append(run.factOrder, fact.Key)
	}
	return run
}

// ledgerSent is one body an agent delivered: the external input the send route
// commits, stamped with the four delivery entries.
func ledgerSent(key event.EventID, source event.EventID, target event.Address, body string) event.Event {
	return event.Event{
		Key:     key,
		Type:    event.TypeExternalInput,
		Meta:    map[string]string{event.MetaKeyDeliveryIdentity: string(key), event.MetaKeyDeliverySource: string(source)},
		Message: model.Message{Role: model.RoleUser, Content: body},
		Target:  target,
	}
}

// ledgerSettled is one terminal notification the framework delivered: the task
// settle the settle route commits, which carries its own identity as the fact key.
func ledgerSettled(key event.EventID, target event.Address, body string) event.Event {
	return event.Event{
		Key:     key,
		Type:    event.TypeTaskSettled,
		Meta:    map[string]string{event.MetaKeyNoticeKind: "settled"},
		Message: model.Message{Role: model.RoleUser, Content: body},
		Target:  target,
		TaskID:  "task-ledger-settled",
	}
}

// ledgerDescribe renders records as one line each, so a failed read names what the
// account answered instead of the index that failed.
func ledgerDescribe(records []DeliveryRecord) string {
	if len(records) == 0 {
		return "  []"
	}
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, "  "+string(record.Source)+" -> "+string(record.Target.AgentID)+"/"+string(record.Target.SessionID)+
			" identity="+record.Identity+" receipt="+record.Receipt+" type="+record.FactType)
	}
	return strings.Join(lines, "\n")
}

// ledgerMetaKeys reports the sorted entry keys of one record metadata map.
func ledgerMetaKeys(meta map[string]string) []string {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ledgerKeysOf reports the sorted keys of one family set.
func ledgerKeysOf(held map[string]bool) []string {
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestReentrantSendOrdersItsCommits pins the two failure faces of a re-entrant send.
// - negative (reopen_failure_rolls_back_reactivation): a refused reopen leaves the task settled and re-entrant, so the next send reaches the round it opens
// - negative (reenter_occupied_round_not_reported_sent): an occupied round answers a refusal naming it, never the ok receipt of a body no round carries
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestReentrantSendOrdersItsCommits(t *testing.T) {
	t.Run("reopen_failure_rolls_back_reactivation", func(t *testing.T) {
		ctx := context.Background()
		registry := reentryRegistry(t)
		taskID := reentrySettled(t, registry)
		continuations := &reentryContinuationsStub{}
		ledger := &reentryLedgerStub{reopenErr: fmt.Errorf("%w: %s", errReentryNoLedgerEntry, taskID)}
		port := &hostSendReentryPort{tasks: registry, ledger: ledger, continuations: continuations}

		outcome, cause := port.ReenterTask(ctx, taskID, "review it again")
		if outcome == deliverySent {
			t.Fatalf("re-entry of a task whose reopen the ledger refused = %q (%q), want a refusal: the body reached no round", outcome, cause)
		}
		if !strings.Contains(cause, errReentryNoLedgerEntry.Error()) {
			t.Fatalf("refusal = %q, want it to carry the cause the ledger reported", cause)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != task.StatusSettled {
			t.Fatalf("status after the refused re-entry = (%v, %v), want the settled outcome it was re-entered from", status, err)
		}
		if got := continuations.abandonedCount(); got != 1 {
			t.Fatalf("claimed rounds given back = %d, want the one round the refused re-entry claimed", got)
		}
		if got := continuations.servedCount(); got != 0 {
			t.Fatalf("rounds served beside the refused reopen = %d, want none", got)
		}
		if err := registry.Reentrant(ctx, taskID); err != nil {
			t.Fatalf("re-entrant read after the rollback = %v, want a task the next send reaches", err)
		}

		ledger.reopen(true)
		outcome, cause = port.ReenterTask(ctx, taskID, "review it again")
		if outcome != deliverySent {
			t.Fatalf("re-entry after the rollback = %q (%q), want the round the next send opens", outcome, cause)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != task.StatusRunning {
			t.Fatalf("status after the second re-entry = (%v, %v), want the round it opened", status, err)
		}
		if got := reentryAwaitServed(continuations, 1); got != "review it again" {
			t.Fatalf("body served = %q, want the body the second re-entry carried", got)
		}
	})

	t.Run("reenter_occupied_round_not_reported_sent", func(t *testing.T) {
		ctx := context.Background()
		registry := reentryRegistry(t)
		taskID := reentrySettled(t, registry)
		continuations := &reentryContinuationsStub{occupied: true}
		ledger := &reentryLedgerStub{}
		port := &hostSendReentryPort{tasks: registry, ledger: ledger, continuations: continuations}

		outcome, cause := port.ReenterTask(ctx, taskID, "review it again")
		if outcome == deliverySent {
			t.Fatalf("re-entry of a task whose one round is in flight = %q (%q), want a refusal: no round owns the body", outcome, cause)
		}
		if !strings.Contains(cause, "in flight") || !strings.Contains(cause, string(taskID)) {
			t.Fatalf("refusal = %q, want it to name the round in flight and the task it belongs to", cause)
		}
		if got := continuations.claims(); got != 1 {
			t.Fatalf("claims = %d, want the one the occupied slot refused", got)
		}
		if got := continuations.abandonedCount(); got != 0 {
			t.Fatalf("claimed rounds given back = %d, want none: the refused claim held no slot", got)
		}
		if got := ledger.reopenedRounds(); len(got) != 0 {
			t.Fatalf("ledger rounds reopened = %v, want none: a body no round owns moves nothing", got)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != task.StatusSettled {
			t.Fatalf("status after the refused re-entry = (%v, %v), want the task left where it stood", status, err)
		}
	})
}

// TestReentrantSendFilesARoundItCouldNotPutBack pins the one failure the compensation cannot cover (F5).
// - negative (reenter_rollback_failure_files_a_broken_fact): a round the task was not put back from is filed as a broken attempt, queryable from the account
// - the broken attempt is filed beside the stage that could not put it back, under the identity the call and the task share
// - negative (reenter_rollback_answered_files_no_broken_fact): a round that was put back files no broken record
// - a refused attempt is no delivery: the broken record leaves the per-target account of the deliveries alone
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestReentrantSendFilesARoundItCouldNotPutBack(t *testing.T) {
	addr := event.Address{AgentID: "sender", SessionID: "session-send-rollback"}
	frame := task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-send-rollback",
			InvocationID: "invocation-send-rollback",
			DecisionID:   "decision-send-rollback",
			CallID:       "call-send-rollback",
		},
		Address: addr,
	}
	ctx := task.WithCallFrame(context.Background(), frame)
	taskID := event.TaskID("task-send-rollback")
	identity := string(frame.Identity.DedupKey()) + "\x00" + string(taskID)

	t.Run("rollback_failure_files_a_broken_fact", func(t *testing.T) {
		run := refusalAccount(addr)
		tasks := &reentryTasksStub{round: 4, rollbackErr: errReentryRollbackRefused}
		port := &hostSendReentryPort{
			run:           run,
			tasks:         tasks,
			ledger:        &reentryLedgerStub{},
			continuations: &reentryContinuationsStub{serveErr: errReentryRoundNeverLanded},
		}

		outcome, cause := port.ReenterTask(ctx, taskID, "review it again")
		if outcome == deliverySent {
			t.Fatalf("re-entry of a task that was not put back = %q (%q), want a refusal: the body reached no round", outcome, cause)
		}
		if !strings.Contains(cause, errReentryRoundNeverLanded.Error()) {
			t.Fatalf("refusal = %q, want it to carry the cause the round reported", cause)
		}
		if got := tasks.rolledBackRounds(); len(got) != 1 || got[0] != 4 {
			t.Fatalf("rounds the port put back = %v, want the one round it opened", got)
		}

		broken, held := reentryRefusalOf(t, run, DeliveryReceiptBroken, hostDenialRollback)
		if !held {
			t.Fatalf("refusals the account files = %s, want the one broken attempt of the round nothing could put back:\n%s",
				refusalDescribe(testLedgerRefusals(t, run.DeliveryLedger())), refusalDescribe(testLedgerRefusals(t, run.DeliveryLedger())))
		}
		if broken.Identity != identity {
			t.Fatalf("identity of the broken attempt = %q, want the one the call and the task share %q", broken.Identity, identity)
		}
		if broken.Target != addr {
			t.Fatalf("target of the broken attempt = %+v, want the one the call named %+v", broken.Target, addr)
		}
		if broken.Source != event.EventID(frame.Identity.InvocationID) {
			t.Fatalf("source of the broken attempt = %q, want the responsibility range it belongs to %q", broken.Source, frame.Identity.InvocationID)
		}
		if _, ok := reentryRefusalOf(t, run, DeliveryReceiptDenied, hostDenialStart); !ok {
			t.Fatalf("refusals the account files = %s, want the ordinary refusal of the same call beside the broken one",
				refusalDescribe(testLedgerRefusals(t, run.DeliveryLedger())))
		}
		if got := testLedgerRecords(t, run.DeliveryLedger()); len(got) != 0 {
			t.Fatalf("deliveries the account lists = %d, want none: an attempt the account turned away reached no target:\n%s",
				len(got), ledgerDescribe(got))
		}
	})

	t.Run("rollback_answered_files_no_broken_fact", func(t *testing.T) {
		run := refusalAccount(addr)
		tasks := &reentryTasksStub{round: 5}
		port := &hostSendReentryPort{
			run:           run,
			tasks:         tasks,
			ledger:        &reentryLedgerStub{},
			continuations: &reentryContinuationsStub{serveErr: errReentryRoundNeverLanded},
		}

		if outcome, cause := port.ReenterTask(ctx, taskID, "review it again"); outcome == deliverySent {
			t.Fatalf("re-entry of a task whose round never landed = %q (%q), want the refusal the round reported", outcome, cause)
		}
		if _, held := reentryRefusalOf(t, run, DeliveryReceiptBroken, hostDenialRollback); held {
			t.Fatalf("refusals the account files = %s, want none broken: the round was put back",
				refusalDescribe(testLedgerRefusals(t, run.DeliveryLedger())))
		}
		if _, ok := reentryRefusalOf(t, run, DeliveryReceiptDenied, hostDenialStart); !ok {
			t.Fatalf("refusals the account files = %s, want the refused attempt beside the put-back round",
				refusalDescribe(testLedgerRefusals(t, run.DeliveryLedger())))
		}
	})
}

// errReentryRollbackRefused is the cause the task stand-in refuses the compensation with: the round
// a re-entry opened could not be put back.
var errReentryRollbackRefused = fmt.Errorf("the reactivation of the round is not the one the task holds")

// errReentryRoundNeverLanded is the cause the continuation stand-in answers with: the round it was
// asked to serve never reached its input, so the receipt of the body is a refusal.
var errReentryRoundNeverLanded = fmt.Errorf("the round never reached its input")

// refusalAccount returns the account the refusal anchors file into: the address is already established,
// so a refusal fact commits without an assembly, and the account holds no other fact.
func refusalAccount(addr event.Address) *Run {
	return &Run{
		instance:         "instance-send-account",
		sites:            map[event.Address]bool{addr: true},
		facts:            map[event.EventID]event.Event{},
		sessionFactCount: map[event.SessionID]int{},
	}
}

// reentryTasksStub is the task-table stand-in the compensation anchors read: the round a reactivation
// opens, the rounds it was asked to put back and the failure it puts them back with.
type reentryTasksStub struct {
	round       int64
	rollbackErr error
	rolledBack  []int64
}

// Reentrant reports the task admit to a new round, as the settled one the anchors start from is.
func (s *reentryTasksStub) Reentrant(context.Context, event.TaskID) error { return nil }

// Lookup reports the delegation material a rebuilt entry is made of: the anchors here read a
// settled delegation, so the task answers the declaration and terminal it was admitted with.
func (s *reentryTasksStub) Lookup(context.Context, event.TaskID) (task.Spec, task.Status, error) {
	return task.Spec{TriggerSource: event.LineageDelegation}, task.StatusSettled, nil
}

// Reactivate reports the round the re-entry opened.
func (s *reentryTasksStub) Reactivate(context.Context, event.TaskID) (int64, error) {
	return s.round, nil
}

// RollbackReactivation records the round it was asked to put back and answers the failure it is
// fixed with, or puts it back when no failure is fixed.
func (s *reentryTasksStub) RollbackReactivation(_ context.Context, _ event.TaskID, round int64) error {
	s.rolledBack = append(s.rolledBack, round)
	return s.rollbackErr
}

// rolledBackRounds reports the rounds the port put back, in call order.
func (s *reentryTasksStub) rolledBackRounds() []int64 { return append([]int64(nil), s.rolledBack...) }

// reentryRefusalOf reports the one refusal the account files at the named receipt and stage.
func reentryRefusalOf(t *testing.T, run *Run, receipt, category string) (DeliveryRefusal, bool) {
	t.Helper()
	for _, refusal := range testLedgerRefusals(t, run.DeliveryLedger()) {
		if refusal.Receipt == receipt && refusal.Category == category {
			return refusal, true
		}
	}
	return DeliveryRefusal{}, false
}

// refusalDescribe renders the refusals one account answers as one line each, so a failed read names
// what the account holds instead of the receipt that was missing.
func refusalDescribe(refusals []DeliveryRefusal) string {
	if len(refusals) == 0 {
		return "  []"
	}
	lines := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		lines = append(lines, "  "+string(refusal.Source)+" -> "+string(refusal.Target.AgentID)+"/"+string(refusal.Target.SessionID)+
			" identity="+refusal.Identity+" receipt="+refusal.Receipt+" category="+refusal.Category)
	}
	return strings.Join(lines, "\n")
}

// errReentryNoLedgerEntry is the cause the ledger stand-in refuses the reopen with: a
// delegation ledger that holds no entry for the task it is asked to move.
var errReentryNoLedgerEntry = fmt.Errorf("agent: %w", errReentryUnknownDelegation)

// errReentryUnknownDelegation stands for the ledger's own unknown-delegation refusal,
// so the stand-in reports the case it models instead of a refusal invented here.
var errReentryUnknownDelegation = fmt.Errorf("no delegation is recorded for that task")

// reentryRegistry returns an empty task table over an in-memory commit carrier, so the
// anchors read a real task state machine instead of a stand-in for it.
func reentryRegistry(t *testing.T) *task.MemoryRegistry {
	t.Helper()
	return task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
}

// reentrySettled admits one task and settles it, so the re-entry starts from the settled
// outcome design D7 admits.
func reentrySettled(t *testing.T, registry *task.MemoryRegistry) event.TaskID {
	t.Helper()
	ctx := context.Background()
	addr := event.Address{AgentID: "sender", SessionID: "session-send-reentry"}
	receipt, err := registry.Register(ctx, task.Spec{
		CallID:       "call-reentry",
		DedupKey:     "dedup-reentry",
		InvocationID: "invocation-reentry",
		InstanceID:   "instance-reentry",
		Description:  "one delegated review",
		Source:       addr,
		Target:       addr,
	})
	if err != nil || !receipt.Accepted {
		t.Fatalf("admit the delegated review = (%+v, %v), want a fresh admission", receipt, err)
	}
	if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusRunning, "running", nil); err != nil || result != task.CommitCommitted {
		t.Fatalf("move the delegated review under way = (%v, %v), want a committed change", result, err)
	}
	if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, "settled", nil); err != nil || result != task.CommitCommitted {
		t.Fatalf("settle the delegated review = (%v, %v), want a committed change", result, err)
	}
	return receipt.TaskID
}

// reentryLedgerStub is the delegation-ledger stand-in the re-entry anchors read: the
// reopen it is fixed to refuse or accept, and the rounds it was asked to move or
// withdraw.
type reentryLedgerStub struct {
	mu        sync.Mutex
	reopenErr error
	rounds    []int64
	abandoned []int64
}

// CanReopenRound reports the terminal entry this stand-in always holds: the anchors here
// read the reopen that still fails, so the precheck is answered as a settled delegation.
func (s *reentryLedgerStub) CanReopenRound(event.TaskID) error { return nil }

// RestoreEntry accepts the rebuilt entry the precheck never asks for: this stand-in already
// holds the settled delegation, so the lazy rebuild is a no-op here.
func (s *reentryLedgerStub) RestoreEntry(event.TaskID, task.Spec, task.Status) error { return nil }

// ReopenRound records the round it was asked to move and answers the failure it is fixed
// with, or moves the entry when no failure is fixed.
func (s *reentryLedgerStub) ReopenRound(taskID event.TaskID, round int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reopenErr != nil {
		return s.reopenErr
	}
	s.rounds = append(s.rounds, round)
	return nil
}

// AbandonRound records the round the re-entry withdrew.
func (s *reentryLedgerStub) AbandonRound(_ event.TaskID, round int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abandoned = append(s.abandoned, round)
}

// reopen fixes the stand-in to accept or refuse the next reopen.
func (s *reentryLedgerStub) reopen(accept bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accept {
		s.reopenErr = nil
		return
	}
	s.reopenErr = errReentryNoLedgerEntry
}

// reopenedRounds reports the rounds the ledger was asked to move, in call order.
func (s *reentryLedgerStub) reopenedRounds() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.rounds...)
}

// reentryContinuationsStub is the continuation-table stand-in the re-entry anchors read:
// the one slot a body is claimed into, the slot a refused re-entry gives back, and the
// body the round was served with.
type reentryContinuationsStub struct {
	mu        sync.Mutex
	occupied  bool
	claimed   int
	abandoned int
	seeded    int
	served    []string
	afters    []func(error)
	serveErr  error
}

// Served reports the stub as already serving every task: the anchors here read the claim and
// the serve, so the lazy rebuild of a continuation this process never minted is not on their path.
func (s *reentryContinuationsStub) Served(event.TaskID) bool { return true }

// Seed accepts the continuation the lazy rebuild would hand over: the anchors here never reach
// it because the stub serves every task it is asked about.
func (s *reentryContinuationsStub) Seed(event.TaskID, []byte, event.SessionID, subagent.Builder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seeded++
	return nil
}

// Claim takes the one slot unless the stub is fixed as already held.
func (s *reentryContinuationsStub) Claim(taskID event.TaskID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed++
	if s.occupied {
		return fmt.Errorf("%w: %s", subagent.ErrContinuationBusy, taskID)
	}
	s.occupied = true
	return nil
}

// Abandon gives the slot back.
func (s *reentryContinuationsStub) Abandon(event.TaskID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.occupied = false
	s.abandoned++
}

// Serve records the body the round carried, records the report the port handed over and
// answers the failure it is fixed with. The stand-in serves the body the moment it returns, so
// the receipt the port answers with is the one of a body its round already carries.
func (s *reentryContinuationsStub) Serve(_ context.Context, _ event.TaskID, input string, after func(error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.served = append(s.served, input)
	if after != nil {
		s.afters = append(s.afters, after)
	}
	return s.serveErr
}

// endRound reports how the round the stub served ended, so an anchor reads the port's answer to a
// round that landed the body and then failed to record its terminal.
func (s *reentryContinuationsStub) endRound(err error) {
	s.mu.Lock()
	afters := append([]func(error){}, s.afters...)
	s.mu.Unlock()
	for _, after := range afters {
		after(err)
	}
}

// claims reports how many claims the port made, refused ones included.
func (s *reentryContinuationsStub) claims() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimed
}

// abandonedCount reports how often the port gave a claimed round back.
func (s *reentryContinuationsStub) abandonedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abandoned
}

// servedCount reports how many rounds were served.
func (s *reentryContinuationsStub) servedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.served)
}

// reentryAwaitServed waits for the nth body the round was served with and reports it: the
// round runs beside the receipt, so a check waits on the face it reports through.
func reentryAwaitServed(stub *reentryContinuationsStub, want int) string {
	deadline := time.Now().Add(5 * time.Second)
	for {
		stub.mu.Lock()
		served := append([]string(nil), stub.served...)
		stub.mu.Unlock()
		if len(served) >= want {
			return served[want-1]
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(time.Millisecond)
	}
}
