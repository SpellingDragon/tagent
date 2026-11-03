// The cross-session symptom this change exists for, read from the two anchors the
// delivery owes: a reflecting session that hands a body to a peer session drives that
// peer through one round, and the tool face of the reflecting agent carries exactly one
// communication tool.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package execution

import (
	"context"
	"encoding/json"
	"errors"
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

// crossSymptomEntryAgent names the material the cross-session symptom is written in: one
// entry agent, one peer agent whose session the delivery names, and one reflecting agent
// whose declared white list reaches both.
// - crossSymptomBody is the body one delivery carries, and the material the peer round is read back by: it reaches the peer's next model request
// - crossSymptomPeerSession is the peer session the delivery names, bound by a peer turn before the reflecting turn runs, so the address it names is live
const (
	crossSymptomEntryAgent     = "user"
	crossSymptomEntryModel     = "model-cross-entry"
	crossSymptomPeerAgent      = "peer"
	crossSymptomPeerModel      = "model-cross-peer"
	crossSymptomMeditatorAgent = "meditator"
	crossSymptomMeditatorModel = "model-cross-meditator"
	crossSymptomPeerSession    = "session-cross-symptom-peer"

	crossSymptomBindingRequest = "cross symptom: bind the peer session"
	crossSymptomPeerAnswer     = "cross symptom: the peer session answered"
	crossSymptomRequest        = "cross symptom: hand the note to the peer session"
	crossSymptomBody           = "cross symptom: the note one session hands a peer"
	crossSymptomTurnDone       = "cross symptom: the turn is done"

	crossSymptomToolSend = "send"
	crossSymptomCallSend = "call-cross-symptom-send"
)

// crossSymptomMeditationSession is the loop session of the reflecting agent.
const crossSymptomMeditationSession = "tagent-session:" + crossSymptomEntryAgent + ":meditation"

// crossSymptomWindow bounds every wait of the symptom anchors.
const crossSymptomWindow = 20 * time.Second

// TestSendSymptomCrossSessionDrivesTurn drives the cross-session symptom: a reflecting session hands one body to the live session of a peer agent.
// - positive: the call is answered with the bare ok receipt
// - positive: the body lands in the peer chain as an external input under the send lineage, addressed to the peer session
// - positive: the peer session is driven through exactly one round that reads the body
// - negative: the body never leaves through the outbound channel, because the call names a session and not a channel
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestSendSymptomCrossSessionDrivesTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newCrossSymptomProvider()
	sender := &symptomSender{}
	host := crossSymptomBoot(t, provider, sender)
	peer := event.Address{AgentID: crossSymptomPeerAgent, SessionID: crossSymptomPeerSession}

	bindPeerTurn(t, ctx, host, peer)

	meditate := event.Address{AgentID: crossSymptomMeditatorAgent, SessionID: crossSymptomMeditationSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-cross-symptom-meditation",
		Address:      meditate,
		Input:        []model.Message{{Role: model.RoleUser, Content: crossSymptomRequest}},
	}); err != nil {
		t.Fatalf("invoke the reflecting turn: %v", err)
	}

	receipt, found := crossSymptomAwaitReceipt(provider, crossSymptomCallSend, crossSymptomWindow)
	if !found {
		t.Fatalf("the reflecting turn never read a result beside its send call:\n%s", symptomDescribe(provider.requests()))
	}
	if receipt != symptomReceiptOK {
		t.Fatalf("send{target:{agent,session}} result = %q, want the bare %q of a delivered body:\n%s",
			receipt, symptomReceiptOK, symptomDescribe(provider.requests()))
	}

	rounds := crossSymptomAwaitPeerRounds(provider, crossSymptomBody, crossSymptomWindow)
	if len(rounds) != 1 {
		t.Fatalf("peer rounds carrying the body = %d, want the one round the delivery owed:\n%s",
			len(rounds), symptomDescribe(provider.requests()))
	}

	facts := crossSymptomBodyFacts(t, host, peer)
	if len(facts) != 1 {
		t.Fatalf("delivered facts in the peer chain = %d, want the one body the call carried:\n%s",
			len(facts), symptomDescribe(provider.requests()))
	}
	if facts[0].Type != event.TypeExternalInput {
		t.Fatalf("delivered fact type = %q, want the external input a delivery lands", facts[0].Type)
	}
	if got := facts[0].Meta[event.MetaKeyTriggerSource]; got != crossSymptomLineage {
		t.Fatalf("delivered fact trigger source = %q, want the %q lineage of a body a session sent", got, crossSymptomLineage)
	}
	if facts[0].Target != peer {
		t.Fatalf("delivered fact target = %+v, want the peer session %+v", facts[0].Target, peer)
	}
	for _, body := range sender.accepted() {
		if strings.Contains(body, crossSymptomBody) {
			t.Fatalf("the delivered body left through the outbound channel: %q", body)
		}
	}
}

// TestSendSymptomOnlyOneCommunicationTool reads the tool face the reflecting agent is offered.
// - the one communication tool the face declares is send, so a body reaches a peer session by one route only
// - the face carries no second delivery tool: a document declaring one is refused at boot by name
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestSendSymptomOnlyOneCommunicationTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newCrossSymptomProvider()
	host := crossSymptomBoot(t, provider, &symptomSender{})
	peer := event.Address{AgentID: crossSymptomPeerAgent, SessionID: crossSymptomPeerSession}

	bindPeerTurn(t, ctx, host, peer)

	meditate := event.Address{AgentID: crossSymptomMeditatorAgent, SessionID: crossSymptomMeditationSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-cross-symptom-meditation",
		Address:      meditate,
		Input:        []model.Message{{Role: model.RoleUser, Content: crossSymptomRequest}},
	}); err != nil {
		t.Fatalf("invoke the reflecting turn: %v", err)
	}
	request, found := crossSymptomAwaitMeditationRequest(provider, crossSymptomWindow)
	if !found {
		t.Fatalf("the reflecting agent's loop session served no model request:\n%s", symptomDescribe(provider.requests()))
	}
	declared := crossSymptomCommunicationTools(request)
	if len(declared) != 1 || declared[0] != crossSymptomToolSend {
		t.Fatalf("communication tools of the reflecting face = %v, want exactly %q:\n%s",
			declared, crossSymptomToolSend, symptomDescribe(provider.requests()))
	}

	if err := crossSymptomBootDeclaring(t, provider, crossSymptomDeletedTool); !errors.Is(err, assembly.ErrUnknownHostTool) {
		t.Fatalf("boot of a document declaring %q = %v, want the named refusal of a tool this host does not wire",
			crossSymptomDeletedTool, err)
	}
}

// crossSymptomLineage is the trigger source a delivered body is stamped with.
const crossSymptomLineage = "send"

// crossSymptomDeletedTool is the communication tool DEL-2 deleted: the id a document
// declaring a second delivery route would name.
const crossSymptomDeletedTool = "deliver_summary"

// crossSymptomBootDeclaring renders the symptom document beside the reflecting agent's
// tools and answers the result of booting it, so one check reads the host's refusal of
// a declared tool id without booting a host of its own.
func crossSymptomBootDeclaring(t *testing.T, provider *crossSymptomProvider, tool string) error {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	document := strings.Replace(crossSymptomHostDocument(root, provider.endpoint()),
		"      - { kind: tool, id: "+crossSymptomToolSend+" }\n",
		"      - { kind: tool, id: "+crossSymptomToolSend+" }\n      - { kind: tool, id: "+tool+" }\n", 1)
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the observed face"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(&symptomSender{}))
	if host != nil {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if closeErr := host.Close(closeCtx); closeErr != nil {
				t.Logf("the refusing host ended its close beside: %v", closeErr)
			}
		}()
	}
	return err
}

// crossSymptomCommunicationTools reports the declared tools of one request that belong to
// the communication family: every name a body could be delivered through.
func crossSymptomCommunicationTools(request intakeRequest) []string {
	out := make([]string, 0, len(request.Tools))
	for _, name := range request.Tools {
		switch name {
		case "send", "deliver_summary", "deliver_to":
			out = append(out, name)
		}
	}
	return out
}

// bindPeerTurn drives one peer turn, so the session the delivery names is a live binding
// before the reflecting turn asks for it.
func bindPeerTurn(t *testing.T, ctx context.Context, host *assembly.Host, peer event.Address) {
	t.Helper()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-cross-symptom-bind",
		Address:      peer,
		Input:        []model.Message{{Role: model.RoleUser, Content: crossSymptomBindingRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the peer turn: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("peer turn terminal = %v, want the completed turn", turn.Terminal)
	}
}

// crossSymptomBodyFacts reports the facts of the peer chain that carry the delivered body.
func crossSymptomBodyFacts(t *testing.T, host *assembly.Host, peer event.Address) []event.Event {
	t.Helper()
	var out []event.Event
	for _, fact := range host.Run().SessionFacts(peer) {
		if strings.Contains(fact.Message.Content, crossSymptomBody) {
			out = append(out, fact)
		}
	}
	return out
}

// crossSymptomAwaitReceipt waits for the result text the model read beside one call.
func crossSymptomAwaitReceipt(provider *crossSymptomProvider, callID string, window time.Duration) (string, bool) {
	deadline := time.Now().Add(window)
	for {
		if receipt, ok := symptomReceipt(provider.requests(), callID); ok {
			return receipt, true
		}
		if !time.Now().Before(deadline) {
			return "", false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// crossSymptomAwaitPeerRounds waits for the peer requests that carried the delivered body
// and answers them in request order.
func crossSymptomAwaitPeerRounds(provider *crossSymptomProvider, body string, window time.Duration) []intakeRequest {
	deadline := time.Now().Add(window)
	for {
		var out []intakeRequest
		for _, request := range provider.requests() {
			if request.Model == crossSymptomPeerModel && symptomCarries(request, body) {
				out = append(out, request)
			}
		}
		if len(out) > 0 || !time.Now().Before(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// crossSymptomAwaitMeditationRequest waits for the first request the reflecting agent's
// loop session served.
func crossSymptomAwaitMeditationRequest(provider *crossSymptomProvider, window time.Duration) (intakeRequest, bool) {
	deadline := time.Now().Add(window)
	for {
		for _, request := range provider.requests() {
			if request.Model == crossSymptomMeditatorModel {
				return request, true
			}
		}
		if !time.Now().Before(deadline) {
			return intakeRequest{}, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// crossSymptomProvider is the OpenAI-compatible endpoint the symptom anchors boot against:
// the peer turn answers its own rounds, and the reflecting turn issues the one call the
// anchor reads. Each scripted call is claimed once, so a drive that repeats a step reads
// the terminal instead of silently doubling the flow.
type crossSymptomProvider struct {
	server *httptest.Server

	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	issued map[string]bool
}

// newCrossSymptomProvider starts the endpoint and closes it with the test.
func newCrossSymptomProvider() *crossSymptomProvider {
	provider := &crossSymptomProvider{issued: map[string]bool{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	return provider
}

// close ends the endpoint the host was booted against.
func (p *crossSymptomProvider) close() { p.server.Close() }

// endpoint reports the base URL the host document's provider entry names.
func (p *crossSymptomProvider) endpoint() string { return p.server.URL + "/v1" }

// serve answers one chat completion and records the request it carried.
func (p *crossSymptomProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, p.answer(intakeDecodeRequest(index, modelName, body)))
}

// answer scripts one decision off the request: the reflecting turn issues its one send
// call in the decision that reads the input the anchor drove, and every other decision
// answers its own terminal.
func (p *crossSymptomProvider) answer(request intakeRequest) string {
	if request.Model != crossSymptomMeditatorModel {
		return intakeFinalAnswer(crossSymptomPeerAnswer)
	}
	if !symptomCarries(request, crossSymptomRequest) || p.claim(crossSymptomCallSend) == false {
		return intakeFinalAnswer(crossSymptomTurnDone)
	}
	return intakeToolCallAnswer(crossSymptomToolSend, crossSymptomCallSend,
		`{"target":{"agent":`+intakeQuoted(crossSymptomPeerAgent)+`,"session":`+intakeQuoted(crossSymptomPeerSession)+
			`},"content":`+intakeQuoted(crossSymptomBody)+`}`)
}

// claim reports whether the named call is this request's to issue, exactly once.
func (p *crossSymptomProvider) claim(call string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issued[call] {
		return false
	}
	p.issued[call] = true
	return true
}

// requests reports the requests the endpoint served, in request order.
func (p *crossSymptomProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// crossSymptomHostDocument renders the document the symptom anchors boot: one entry agent,
// one peer agent whose session a body is handed to, and one reflecting agent whose white
// list and observed face reach both.
func crossSymptomHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + crossSymptomEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + crossSymptomEntryModel + "\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + crossSymptomEntryAgent + ":\n")
	b.WriteString("    model: " + crossSymptomEntryModel + "\n")
	b.WriteString("  " + crossSymptomPeerAgent + ":\n")
	b.WriteString("    model: " + crossSymptomPeerModel + "\n")
	b.WriteString("  " + crossSymptomMeditatorAgent + ":\n")
	b.WriteString("    model: " + crossSymptomMeditatorModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + crossSymptomToolSend + " }\n")
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [" + crossSymptomEntryAgent + ", " + crossSymptomPeerAgent + "] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [" + crossSymptomEntryAgent + ", " + crossSymptomPeerAgent + "]\n")
	b.WriteString("      deliver_to: [" + crossSymptomEntryAgent + ", " + crossSymptomPeerAgent + "]\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// crossSymptomBoot boots the formal host the symptom anchors drive, so every decision
// below reaches the runtime through the public entry the deployment uses.
func crossSymptomBoot(t *testing.T, provider *crossSymptomProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Cleanup(provider.close)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(crossSymptomHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the observed face"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the cross-session symptom host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the cross-session symptom host ended its close beside: %v", err)
		}
	})
	return host
}
