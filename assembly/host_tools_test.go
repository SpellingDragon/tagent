package assembly

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"

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
		"hostSendHeldPrefix":   hostSendHeldPrefix,
	}
	wantReceipts := map[string]string{
		"hostSendReceiptOK":    "ok",
		"hostSendDeniedPrefix": "[send_denied] ",
		"hostSendHeldPrefix":   "[send_held] ",
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
			{"held", deliveryHeld, "parked for review", hostSendHeldPrefix + "parked for review"},
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

	records := ledger.Records()
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
	if got := ledger.BySource(ledgerSource); len(got) != len(ledgerTargets) {
		t.Fatalf("deliveries answered by the source event = %d, want %d", len(got), len(ledgerTargets))
	}
	if got := ledger.ByTarget(ledgerTargets[1]); len(got) != 1 {
		t.Fatalf("deliveries answered by one target = %d, want the one body it received:\n%s", len(got), ledgerDescribe(got))
	}
	if first, second := ledgerDescribe(records), ledgerDescribe(ledger.Records()); first != second {
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
	records := run.DeliveryLedger().ByTarget(target)
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
	for _, record := range run.DeliveryLedger().Records() {
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
	if got := repeat.DeliveryLedger().Records(); len(got) != 1 {
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
	records := run.DeliveryLedger().Records()
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
	if got := unassembled.DeliveryLedger().Records(); len(got) != 0 {
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
