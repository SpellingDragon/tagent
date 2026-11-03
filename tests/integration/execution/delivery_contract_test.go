// The two delivery-account symptoms this change exists for, read from the anchors the delivery contract owes:
//
//   - one event delivered to three sessions is accounted for per target: each target carries its own delivery identity and receipt
//   - a body an agent sends and a task settle the framework delivers to one target land in one account under the same four entries
//   - the fact type of each stays what it is, so "who is speaking" is read from the fact and "which delivery is this" from the account
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// deliveryEntryAgent, deliveryEntryModel, deliveryPeerAgent, deliveryPeerModel,
// deliveryReviewerAgent, deliveryReviewerModel and deliveryMeditatorModel name the
// material the delivery anchors are written in: one entry agent, one peer agent that
// holds the delegating tool, one reviewer whose settled task lands the framework's own
// delivery fact at the peer session, and one reflecting agent holding the one
// communication tool whose declared white list reaches the peer.
const (
	deliveryEntryAgent     = "user"
	deliveryEntryModel     = "model-delivery-entry"
	deliveryPeerAgent      = "peer"
	deliveryPeerModel      = "model-delivery-peer"
	deliveryReviewerAgent  = "reviewer"
	deliveryReviewerModel  = "model-delivery-reviewer"
	deliveryMeditatorAgent = "meditator"
	deliveryMeditatorModel = "model-delivery-meditator"

	deliveryMeditationSession = "tagent-session:" + deliveryEntryAgent + ":meditation"

	deliveryBindRequest      = "delivery ledger: bind the peer session"
	deliveryPeerAnswer       = "delivery ledger: the peer session answered"
	deliveryReviewRequest    = "delivery ledger: review the change under way"
	deliveryReviewAnswer     = "delivery ledger: the review settled"
	deliveryCallSubagent     = "call-delivery-subagent"
	deliveryMultiRequest     = "delivery ledger: hand the note to three peer sessions"
	deliveryMultiInvocation  = "inv-delivery-ledger-multi"
	deliveryMultiBody        = "delivery ledger: the note three sessions receive"
	deliveryTwoRequest       = "delivery ledger: hand the note to the one peer session"
	deliveryTwoInvocation    = "inv-delivery-ledger-two"
	deliveryTwoBody          = "delivery ledger: the note one send carried"
	deliveryDoubleRequest    = "delivery ledger: hand two notes to the one peer session"
	deliveryDoubleInvocation = "inv-delivery-ledger-double"
	deliveryTurnDone         = "delivery ledger: the turn is done"
	deliveryToolSend         = "send"
	deliveryToolSubagentTool = "call_subagent"
)

// deliveryPeerSessions are the three sessions one source event is delivered to.
var deliveryPeerSessions = []string{
	"session-delivery-peer-one",
	"session-delivery-peer-two",
	"session-delivery-peer-three",
}

// deliveryDoubleBodies are the two bodies one decision hands to the same peer session beside each
// other, so the double delivery of one idle target is read from what the run really carries.
var deliveryDoubleBodies = []string{
	"delivery ledger: the first of two notes one decision hands",
	"delivery ledger: the second of two notes one decision hands",
}

// deliveryWindow bounds every wait of the delivery anchors, so a run that commits no
// account answers the named gap instead of holding the test.
const deliveryWindow = 20 * time.Second

// TestInstancesSymptomMultiTargetLedger drives one source event to three sessions through three single-target calls.
// - positive: the account lists the source event three times, one record per target session
// - positive: every record carries its own delivery identity beside the receipt the landed body stands at
// - negative (each_target_exactly_once): no target of one source event is listed twice, and each peer chain holds exactly one delivered fact
// - negative (ledger_queryable_without_parsing_body): the account answers by source event although no message body carries that identity
// - consumer: the calls reach the runtime as one Host.Invoke over a host booted from a real document
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestInstancesSymptomMultiTargetLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newDeliveryProvider()
	host := deliveryBoot(t, provider)
	for _, session := range deliveryPeerSessions {
		deliveryBind(t, ctx, host, event.Address{AgentID: deliveryPeerAgent, SessionID: event.SessionID(session)})
	}

	meditate := event.Address{AgentID: deliveryMeditatorAgent, SessionID: deliveryMeditationSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: deliveryMultiInvocation,
		Address:      meditate,
		Input:        []model.Message{{Role: model.RoleUser, Content: deliveryMultiRequest}},
	}); err != nil {
		t.Fatalf("invoke the reflecting turn that delivers to three sessions: %v", err)
	}
	deliveryAwaitReceipts(t, provider, len(deliveryPeerSessions))

	ledger := host.Run().DeliveryLedger()
	if ledger == nil {
		t.Fatalf("the assembled run reports no delivery account, want the one it owes")
	}
	sources := map[event.EventID]bool{}
	targets := make([]event.Address, 0, len(deliveryPeerSessions))
	for _, session := range deliveryPeerSessions {
		targets = append(targets, event.Address{AgentID: deliveryPeerAgent, SessionID: event.SessionID(session)})
	}
	for _, target := range targets {
		records := ledger.ByTarget(target)
		if len(records) != 1 {
			t.Fatalf("deliveries the account lists for %q = %d, want the one body its call carried:\n%s",
				target.SessionID, len(records), deliveryDescribe(ledger.Records()))
		}
		sources[records[0].Source] = true
	}
	if len(sources) != 1 {
		t.Fatalf("source events the three deliveries are filed under = %d, want the one event that caused them:\n%s",
			len(sources), deliveryDescribe(ledger.Records()))
	}
	var source event.EventID
	for held := range sources {
		source = held
	}
	if !strings.Contains(string(source), deliveryMeditationSession) {
		t.Fatalf("source event of the three deliveries = %q, want the invocation of the reflecting turn %q", source, deliveryMeditationSession)
	}
	bySource := ledger.BySource(source)
	if len(bySource) != len(deliveryPeerSessions) {
		t.Fatalf("deliveries the account lists for %q = %d, want one per target session (%d):\n%s",
			source, len(bySource), len(deliveryPeerSessions), deliveryDescribe(ledger.Records()))
	}
	seenTargets := map[string]bool{}
	for _, record := range bySource {
		if record.Target.AgentID != deliveryPeerAgent {
			t.Fatalf("delivery target agent = %q, want the peer agent %q", record.Target.AgentID, deliveryPeerAgent)
		}
		if seenTargets[string(record.Target.SessionID)] {
			t.Fatalf("target session %q is listed twice in one source event, want every target exactly once:\n%s",
				record.Target.SessionID, deliveryDescribe(ledger.Records()))
		}
		seenTargets[string(record.Target.SessionID)] = true
		if record.Identity == "" {
			t.Fatalf("delivery record of %q carries no delivery identity", record.Target.SessionID)
		}
		if record.Receipt != assembly.DeliveryReceiptSent {
			t.Fatalf("receipt of the delivery to %q = %q, want %q", record.Target.SessionID, record.Receipt, assembly.DeliveryReceiptSent)
		}
		if !strings.Contains(record.Identity, string(record.Target.SessionID)) {
			t.Fatalf("delivery identity %q of %q does not name the target it was delivered to",
				record.Identity, record.Target.SessionID)
		}
		if record.FactType != event.TypeExternalInput {
			t.Fatalf("fact type of the delivery to %q = %q, want the %q an agent's own body lands as",
				record.Target.SessionID, record.FactType, event.TypeExternalInput)
		}
		if _, err := host.Run().CommittedEvent(ctx, event.EventID(record.Identity)); err != nil {
			t.Fatalf("the delivery identity %q names no committed fact: %v", record.Identity, err)
		}
		if sourceOf := record.Meta()[event.MetaKeyDeliverySource]; sourceOf != string(source) {
			t.Fatalf("source entry of the delivery to %q = %q, want the source event it is filed under %q",
				record.Target.SessionID, sourceOf, source)
		}
	}
	for _, session := range deliveryPeerSessions {
		peer := event.Address{AgentID: deliveryPeerAgent, SessionID: event.SessionID(session)}
		if got := len(deliveryFactsCarrying(host, peer, deliveryMultiBody)); got != 1 {
			t.Fatalf("delivered facts in the chain of %q = %d, want the one body its call carried", session, got)
		}
	}
	deliveryRequireBodyUnread(t, host, bySource, deliveryMultiBody)
}

// TestInstancesSymptomTwoEntriesOneAccount drives one sent body and one task settle to the same target.
// - positive: both deliveries are listed under one target in one account
// - positive: the two records carry the same four delivery entries, so a reader consumes both through one vocabulary
// - negative (fact_type_distinct): the records keep their own fact types, so the fact still says who was speaking
// - negative (dedup_rule_identical): every record's identity is the canonical identity of its own committed fact
// - consumer: the send call is one Host.Invoke over a booted host, and the settle fact is the one the framework commits for a settled delegated task
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestInstancesSymptomTwoEntriesOneAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newDeliveryProvider()
	host := deliveryBoot(t, provider)
	peer := event.Address{AgentID: deliveryPeerAgent, SessionID: event.SessionID(deliveryPeerSessions[0])}
	deliveryBind(t, ctx, host, peer)

	provider.watchSettledTask(func() event.TaskID { return deliverySettledTask(host.Run().SessionFacts(peer)) })
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-delivery-settle",
		Address:      peer,
		Input:        []model.Message{{Role: model.RoleUser, Content: deliveryReviewRequest}},
	}); err != nil {
		t.Fatalf("invoke the peer turn that delegates the review: %v", err)
	}
	if taskID := deliveryAwaitSettledTask(provider, deliveryWindow); taskID == "" {
		t.Fatalf("the delegated review settled no task the framework delivers a fact for:\n%s", deliveryDescribeRequests(provider.requests()))
	}

	meditate := event.Address{AgentID: deliveryMeditatorAgent, SessionID: deliveryMeditationSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: deliveryTwoInvocation,
		Address:      meditate,
		Input:        []model.Message{{Role: model.RoleUser, Content: deliveryTwoRequest}},
	}); err != nil {
		t.Fatalf("invoke the reflecting turn that delivers to the peer session: %v", err)
	}
	deliveryAwaitReceipts(t, provider, 1)

	ledger := host.Run().DeliveryLedger()
	byTarget := ledger.ByTarget(peer)
	if len(byTarget) != 2 {
		t.Fatalf("deliveries the account lists for %q = %d, want the body a session sent beside the settle the framework delivered:\n%s",
			peer.SessionID, len(byTarget), deliveryDescribe(byTarget))
	}
	sent, settled := deliverySplitFamilies(t, byTarget)
	if sent.FactType != event.TypeExternalInput {
		t.Fatalf("fact type of the sent body = %q, want %q", sent.FactType, event.TypeExternalInput)
	}
	if settled.FactType != event.TypeTaskSettled {
		t.Fatalf("fact type of the framework settle = %q, want %q", settled.FactType, event.TypeTaskSettled)
	}
	if sent.FactType == settled.FactType {
		t.Fatalf("both records report the fact type %q, want the two delivery families kept apart", sent.FactType)
	}
	if got, want := deliveryKeySet(sent.Meta()), deliveryKeySet(settled.Meta()); !deliverySameKeys(got, want) {
		t.Fatalf("delivery entry keys of the sent body = %v, of the framework settle = %v, want one isomorphic set under the frozen keys",
			got, want)
	}
	if sent.Meta()[event.MetaKeyDeliveryTarget] != settled.Meta()[event.MetaKeyDeliveryTarget] {
		t.Fatalf("target entry of the sent body = %q, of the framework settle = %q, want the one address both reached",
			sent.Meta()[event.MetaKeyDeliveryTarget], settled.Meta()[event.MetaKeyDeliveryTarget])
	}
	for _, record := range byTarget {
		fact, err := host.Run().CommittedEvent(ctx, event.EventID(record.Identity))
		if err != nil {
			t.Fatalf("the delivery identity %q names no committed fact: %v", record.Identity, err)
		}
		if string(fact.Key) != record.Identity || fact.Type != record.FactType {
			t.Fatalf("identity %q resolves to fact %q of type %q, want the fact the record was projected from",
				record.Identity, fact.Key, fact.Type)
		}
	}
	deliveryRequireBodyUnread(t, host, byTarget, deliveryTwoBody)
}

// deliverySplitFamilies reports the record of the sent body and the record of the
// framework settle, so each assertion names which family it reads.
func deliverySplitFamilies(t *testing.T, records []assembly.DeliveryRecord) (sent, settled assembly.DeliveryRecord) {
	t.Helper()
	for _, record := range records {
		switch record.FactType {
		case event.TypeExternalInput:
			sent = record
		case event.TypeTaskSettled:
			settled = record
		}
	}
	if sent.Identity == "" || settled.Identity == "" {
		t.Fatalf("the account of one target holds sent=%q settled=%q, want one record of each family:\n%s",
			sent.Identity, settled.Identity, deliveryDescribe(records))
	}
	return sent, settled
}

// deliveryKeySet reports the sorted entry keys of one record metadata map.
func deliveryKeySet(meta map[string]string) []string {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for at := i; at > 0 && keys[at] < keys[at-1]; at-- {
			keys[at], keys[at-1] = keys[at-1], keys[at]
		}
	}
	return keys
}

// deliverySameKeys reports whether two sorted key lists name the same entries.
func deliverySameKeys(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for at := range left {
		if left[at] != right[at] {
			return false
		}
	}
	return true
}

// deliveryRequireBodyUnread proves the account answers without the body: the identities
// the account reports appear in no message the run holds, so a query that answered them
// read the entries and never the text.
func deliveryRequireBodyUnread(t *testing.T, host *assembly.Host, records []assembly.DeliveryRecord, body string) {
	t.Helper()
	carried := deliveryFactsCarrying(host, event.Address{}, body)
	if len(carried) == 0 {
		t.Fatalf("the run holds no fact carrying %q, want the delivered bodies the account was read beside", body)
	}
	for _, record := range records {
		for _, fact := range carried {
			if strings.Contains(fact.Message.Content, record.Identity) {
				t.Fatalf("the body of %q carries the delivery identity %q, so the account was read out of the text instead of its entries",
					fact.Key, record.Identity)
			}
		}
	}
}

// deliveryFactsCarrying reports the facts of one address that carry the named material;
// an address naming no agent reads every session.
func deliveryFactsCarrying(host *assembly.Host, addr event.Address, material string) []event.Event {
	var out []event.Event
	if addr.SessionID != "" || addr.AgentID != "" {
		for _, fact := range host.Run().SessionFacts(addr) {
			if strings.Contains(fact.Message.Content, material) {
				out = append(out, fact)
			}
		}
		return out
	}
	for _, session := range deliveryAllSessions(host) {
		for _, fact := range host.Run().SessionFacts(event.Address{SessionID: session}) {
			if strings.Contains(fact.Message.Content, material) {
				out = append(out, fact)
			}
		}
	}
	return out
}

// deliveryAllSessions reports every session the run bound, so a read that names no
// address still reaches the facts of all of them.
func deliveryAllSessions(host *assembly.Host) []event.SessionID {
	out := make([]event.SessionID, 0, len(deliveryPeerSessions)+2)
	for _, session := range deliveryPeerSessions {
		out = append(out, event.SessionID(session))
	}
	out = append(out, event.SessionID(deliveryMeditationSession))
	return out
}

// deliveryBind drives one peer turn, so the session a delivery names is live before the
// reflecting turn asks for it.
func deliveryBind(t *testing.T, ctx context.Context, host *assembly.Host, peer event.Address) {
	t.Helper()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: event.InvocationID("inv-delivery-bind-" + string(peer.SessionID)),
		Address:      peer,
		Input:        []model.Message{{Role: model.RoleUser, Content: deliveryBindRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the peer turn of %q: %v", peer.SessionID, err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("peer turn terminal of %q = %v, want the completed turn", peer.SessionID, turn.Terminal)
	}
}

// deliverySettledTask reports the task the settle fact of the peer chain belongs to.
func deliverySettledTask(facts []event.Event) event.TaskID {
	for at := len(facts) - 1; at >= 0; at-- {
		if facts[at].Type == event.TypeTaskSettled && facts[at].TaskID != "" {
			return facts[at].TaskID
		}
	}
	return ""
}

// deliveryAwaitSettledTask waits for the settle fact the framework commits, bounded so a
// run that commits none reports the gap.
func deliveryAwaitSettledTask(provider *deliveryProvider, window time.Duration) event.TaskID {
	deadline := time.Now().Add(window)
	for {
		if taskID := provider.settled(); taskID != "" {
			return taskID
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// deliveryAwaitReceipts waits until the model read the bare ok by as many calls as the
// anchor delivered to, so the account is read beside the receipts and not before them.
func deliveryAwaitReceipts(t *testing.T, provider *deliveryProvider, calls int) {
	t.Helper()
	deadline := time.Now().Add(deliveryWindow)
	for {
		if got := provider.okReceipts(); got >= calls {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("send receipts the model read = %d, want %d:\n%s", provider.okReceipts(), calls, deliveryDescribeRequests(provider.requests()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// deliveryProvider is the OpenAI-compatible endpoint the anchors boot against: it
// scripts the three single-target deliveries, the one delivery and the peer's own
// delegating decision off the material each request carries.
type deliveryProvider struct {
	server *httptest.Server

	mu          sync.Mutex
	models      []string
	bodies      []map[string]json.RawMessage
	issued      map[string]bool
	settledTask func() event.TaskID
}

// newDeliveryProvider starts the endpoint and closes it with the test.
func newDeliveryProvider() *deliveryProvider {
	provider := &deliveryProvider{issued: map[string]bool{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	return provider
}

// close ends the endpoint the host was booted against.
func (p *deliveryProvider) close() { p.server.Close() }

// endpoint reports the base URL the host document's provider entry names.
func (p *deliveryProvider) endpoint() string { return p.server.URL + "/v1" }

// watchSettledTask attaches the read face the delegating decision takes the settled task
// from.
func (p *deliveryProvider) watchSettledTask(read func() event.TaskID) {
	p.mu.Lock()
	p.settledTask = read
	p.mu.Unlock()
}

// settled reports the task identity the watch face answers.
func (p *deliveryProvider) settled() event.TaskID {
	p.mu.Lock()
	read := p.settledTask
	p.mu.Unlock()
	if read == nil {
		return ""
	}
	return read()
}

// serve answers one chat completion and records the request it carried.
func (p *deliveryProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	var modelName string
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	p.mu.Lock()
	index := len(p.bodies)
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()

	payload := p.answer(index, intakeDecodeRequest(index, modelName, body))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
}

// answer scripts one decision off the request: the peer turn delegates once, an idle
// peer session answers, the reflecting turn hands the note to three sessions or to one,
// and the reviewing turn settles.
func (p *deliveryProvider) answer(_ int, request intakeRequest) string {
	switch request.Model {
	case deliveryReviewerModel:
		return intakeFinalAnswer(deliveryReviewAnswer)
	case deliveryPeerModel:
		if deliveryCarries(request, deliveryReviewRequest) && p.claim(deliveryCallSubagent) {
			return intakeToolCallAnswer(deliveryToolSubagentTool, deliveryCallSubagent,
				`{"name":`+intakeQuoted(deliveryReviewerAgent)+`,"model":`+intakeQuoted(deliveryReviewerModel)+
					`,"prompt":`+intakeQuoted(deliveryReviewRequest)+`}`)
		}
		return intakeFinalAnswer(deliveryPeerAnswer)
	}
	if deliveryCarries(request, deliveryDoubleRequest) && p.claim("double") {
		return deliveryToolCallsAnswer(deliveryDoubleCalls())
	}
	if deliveryCarries(request, deliveryMultiRequest) && p.claim("multi") {
		return deliveryToolCallsAnswer(deliveryMultiCalls())
	}
	if deliveryCarries(request, deliveryTwoRequest) && p.claim("two") {
		return intakeToolCallAnswer(deliveryToolSend, "call-delivery-two-send",
			`{"target":{"agent":`+intakeQuoted(deliveryPeerAgent)+`,"session":`+intakeQuoted(deliveryPeerSessions[0])+
				`},"content":`+intakeQuoted(deliveryTwoBody)+`}`)
	}
	return intakeFinalAnswer(deliveryTurnDone)
}

// deliveryMultiCalls renders the one delivery of one event to three sessions: three
// single-target calls, each naming its own session.
func deliveryMultiCalls() [][3]string {
	calls := make([][3]string, 0, len(deliveryPeerSessions))
	for at, session := range deliveryPeerSessions {
		callID := fmt.Sprintf("call-delivery-multi-%d", at)
		arguments := `{"target":{"agent":` + intakeQuoted(deliveryPeerAgent) + `,"session":` + intakeQuoted(session) +
			`},"content":` + intakeQuoted(deliveryMultiBody) + `}`
		calls = append(calls, [3]string{deliveryToolSend, callID, arguments})
	}
	return calls
}

// deliveryDoubleCalls renders the two calls one decision hands to the same idle peer session:
// each carries its own body under its own call identity, so the two deliveries land as two facts.
func deliveryDoubleCalls() [][3]string {
	calls := make([][3]string, 0, len(deliveryDoubleBodies))
	for at, body := range deliveryDoubleBodies {
		callID := fmt.Sprintf("call-delivery-double-%d", at)
		arguments := `{"target":{"agent":` + intakeQuoted(deliveryPeerAgent) + `,"session":` + intakeQuoted(deliveryPeerSessions[0]) +
			`},"content":` + intakeQuoted(body) + `}`
		calls = append(calls, [3]string{deliveryToolSend, callID, arguments})
	}
	return calls
}

// deliveryToolCallsAnswer renders the chat completion that issues several native calls
// from one decision.
func deliveryToolCallsAnswer(calls [][3]string) string {
	rendered := make([]string, 0, len(calls))
	for _, call := range calls {
		rendered = append(rendered, `{"id":"`+call[1]+`","type":"function","function":{"name":"`+call[0]+
			`","arguments":`+intakeQuoted(call[2])+`}}`)
	}
	return `{"id":"scripted-multi","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"calling","tool_calls":[` + strings.Join(rendered, ",") + `]},` +
		`"finish_reason":"tool_calls"}]}`
}

// claim reports whether the named scripted decision is this request's to issue, once.
func (p *deliveryProvider) claim(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issued[name] {
		return false
	}
	p.issued[name] = true
	return true
}

// requests reports the requests the endpoint served, in request order.
func (p *deliveryProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// okReceipts reports how many calls the model was answered the bare ok for.
func (p *deliveryProvider) okReceipts() int {
	counted := 0
	for _, request := range p.requests() {
		for index, message := range request.Messages {
			if message.ToolCallID == "" {
				continue
			}
			for _, later := range request.Messages[index+1:] {
				if later.Role == string(model.RoleUser) && strings.TrimSpace(later.Content) == "ok" {
					counted++
					break
				}
			}
		}
	}
	return counted
}

// deliveryCarries reports whether one request carries the named material in any message.
func deliveryCarries(request intakeRequest, material string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, material) {
			return true
		}
	}
	return false
}

// deliveryDescribe renders the delivery account the anchor read, so a failure states
// what the run really accounted for.
func deliveryDescribe(records []assembly.DeliveryRecord) string {
	described := make([]string, 0, len(records))
	for _, record := range records {
		described = append(described, fmt.Sprintf("{source=%q target=%s/%s identity=%q receipt=%q type=%q}",
			record.Source, record.Target.AgentID, record.Target.SessionID, record.Identity, record.Receipt, record.FactType))
	}
	return "[" + strings.Join(described, " ") + "]"
}

// deliveryDescribeRequests renders the scripted requests, so a failure states what the
// model really saw.
func deliveryDescribeRequests(requests []intakeRequest) string {
	return symptomDescribe(requests)
}

// deliveryHostDocument renders the document the anchors boot: one entry agent, one peer
// agent holding the delegating tool, one reviewer whose settled task the framework
// delivers, and one reflecting agent whose white list and observed face reach both.
func deliveryHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + deliveryEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + deliveryEntryModel + "\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + deliveryEntryAgent + ":\n")
	b.WriteString("    model: " + deliveryEntryModel + "\n")
	b.WriteString("  " + deliveryPeerAgent + ":\n")
	b.WriteString("    model: " + deliveryPeerModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + deliveryToolSubagentTool + " }\n")
	b.WriteString("  " + deliveryReviewerAgent + ":\n")
	b.WriteString("    model: " + deliveryReviewerModel + "\n")
	b.WriteString("  " + deliveryMeditatorAgent + ":\n")
	b.WriteString("    model: " + deliveryMeditatorModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + deliveryToolSend + " }\n")
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [" +
		deliveryEntryAgent + ", " + deliveryPeerAgent + "] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: delivery-meditation.md\n")
	b.WriteString("      observed_namespaces: [" + deliveryEntryAgent + ", " + deliveryPeerAgent + "]\n")
	b.WriteString("      deliver_to: [" + deliveryEntryAgent + ", " + deliveryPeerAgent + "]\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// deliveryBoot boots the formal host the anchors drive, so every delivery below reaches
// the runtime through the public entry the deployment uses.
func deliveryBoot(t *testing.T, provider *deliveryProvider) *assembly.Host {
	t.Helper()
	t.Cleanup(provider.close)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(deliveryHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "delivery-meditation.md"),
		[]byte("reflect on the observed face"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(&symptomSender{}))
	if err != nil {
		t.Fatalf("boot the delivery contract host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the delivery contract host ended its close beside: %v", err)
		}
	})
	return host
}

// TestSendSymptomDoubleDeliveryToOneSession drives the two deliveries one decision hands to the same idle peer session.
// - positive: both calls answer the bare ok, so both bodies reached the session their calls named
// - positive: the peer chain holds one delivered fact per body, so neither body is dropped behind the other
// - positive: the peer turn reads both bodies, whether the second arrived inside the round the first opened or at its own boundary
// - negative: the account lists the two deliveries under one target, each with its own identity, and none twice
// - consumer: the calls reach the runtime as one Host.Invoke over a host booted from a real document
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestSendSymptomDoubleDeliveryToOneSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newDeliveryProvider()
	host := deliveryBoot(t, provider)
	peer := event.Address{AgentID: deliveryPeerAgent, SessionID: event.SessionID(deliveryPeerSessions[0])}
	deliveryBind(t, ctx, host, peer)

	meditate := event.Address{AgentID: deliveryMeditatorAgent, SessionID: deliveryMeditationSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: deliveryDoubleInvocation,
		Address:      meditate,
		Input:        []model.Message{{Role: model.RoleUser, Content: deliveryDoubleRequest}},
	}); err != nil {
		t.Fatalf("invoke the reflecting turn that hands two notes to one session: %v", err)
	}
	deliveryAwaitReceipts(t, provider, len(deliveryDoubleBodies))

	for _, body := range deliveryDoubleBodies {
		if got := len(deliveryFactsCarrying(host, peer, body)); got != 1 {
			t.Fatalf("delivered facts carrying %q = %d, want the one body its call carried", body, got)
		}
		if deliveryAwaitPeerBody(provider, body) == 0 {
			t.Fatalf("the peer turn never read %q, so the body landed beside no turn:\n%s",
				body, deliveryDescribeRequests(provider.requests()))
		}
	}

	records := host.Run().DeliveryLedger().ByTarget(peer)
	if len(records) != len(deliveryDoubleBodies) {
		t.Fatalf("deliveries the account lists for %q = %d, want one per body (%d):\n%s",
			peer.SessionID, len(records), len(deliveryDoubleBodies), deliveryDescribe(records))
	}
	identities := map[string]bool{}
	for _, record := range records {
		if record.Receipt != assembly.DeliveryReceiptSent {
			t.Fatalf("receipt of the delivery to %q = %q, want %q", peer.SessionID, record.Receipt, assembly.DeliveryReceiptSent)
		}
		if identities[record.Identity] {
			t.Fatalf("identity %q of the delivery to %q is listed twice, want one record per body:\n%s",
				record.Identity, peer.SessionID, deliveryDescribe(records))
		}
		identities[record.Identity] = true
	}
}

// deliveryAwaitPeerBody waits until one request of the peer turn carries the named body and reports
// how many did: the second body lands inside the round the first opened or at the next boundary, so
// the check waits on what the turn really read instead of on an arrival order it cannot pin.
func deliveryAwaitPeerBody(provider *deliveryProvider, body string) int {
	deadline := time.Now().Add(deliveryWindow)
	for {
		counted := 0
		for _, request := range provider.requests() {
			if request.Model == deliveryPeerModel && deliveryCarries(request, body) {
				counted++
			}
		}
		if counted > 0 || !time.Now().Before(deadline) {
			return counted
		}
		time.Sleep(10 * time.Millisecond)
	}
}
