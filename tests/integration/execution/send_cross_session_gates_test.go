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
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// gateEntryAgent names the material the four-gate anchor is written in: one entry agent
// holding a live session, one reflecting agent declaring a deliver_to white list that
// reaches the entry agent and one target it never observed, and one declared agent the
// white list omits.
// - gateSession is the live session of the entry agent every accepted destination would name, so the refusal each subtest reads is the gate it names
// - gateBlindAgent is declared beside the white list and outside the observed face, the blind destination design D8 refuses by name
// - gateOutsiderAgent is declared and named nowhere in the white list
const (
	gateEntryAgent     = "user"
	gateEntryModel     = "model-gate-user"
	gateMeditatorAgent = "meditator"
	gateMeditatorModel = "model-gate-meditator"
	gateBlindAgent     = "blind"
	gateBlindModel     = "model-gate-blind"
	gateOutsiderAgent  = "outsider"
	gateOutsiderModel  = "model-gate-outsider"
	gateSession        = "session-send-gates"

	gateToolSend = "send"

	gateRequest = "gates: deliver one note to a peer session"
	gateBody    = "gates: the note one session hands another"
	gateDone    = "gates: the turn is done"
)

// gateMeditationSession is the loop session of the reflecting agent: the entry
// label the deployment names, then the entry agent, then the meditation suffix.
const gateMeditationSession = "tagent-session:" + gateEntryAgent + ":meditation"

// gateWindow bounds every wait of the anchor, so a run that never reaches the
// boundary answers the named gap instead of holding the check.
const gateWindow = 20 * time.Second

// TestSendCrossSessionGates drives the four gates the cross-session delivery face keeps through the formal boot entry.
// - negative denies_out_of_whitelist: a destination outside the declared deliver_to white list is refused by name and reaches no chain
// - negative denies_blind_target: a destination inside the white list but outside the observed face is refused by name
// - negative denies_unbound_session: a destination naming a session this run never bound is refused by name
// - negative denies_not_running: a destination aimed at a host whose close already began is refused by name, and no one-shot fallback opens a run for it
// - the turn is parked in its model production before the close, so the call the drive issues behind it is the one the gate turns away
// - the admission gate fronting every native tool answers that call first: the receipt names its refusal, and the body is delivered nowhere
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestSendCrossSessionGates(t *testing.T) {
	t.Run("denies_out_of_whitelist", func(t *testing.T) {
		scene := newGateScene(t, gateSendCall("call-gate-outsider",
			`{"target":{"agent":`+intakeQuoted(gateOutsiderAgent)+`,"session":`+intakeQuoted(gateSession)+`},"content":`+intakeQuoted(gateBody)+`}`))
		scene.invokeMeditation(t)
		receipt := scene.awaitMeditationReceipt(t, "call-gate-outsider")
		gateAssertDenied(t, receipt, "call-gate-outsider")
		if !strings.Contains(receipt, `the deliver_to white list of "`+gateMeditatorAgent+`" names no "`+gateOutsiderAgent+`"`) {
			t.Fatalf("receipt = %q, want the white list refusal naming %q", receipt, gateOutsiderAgent)
		}
		scene.refuteDelivered(t)
	})

	t.Run("denies_blind_target", func(t *testing.T) {
		scene := newGateScene(t, gateSendCall("call-gate-blind",
			`{"target":{"agent":`+intakeQuoted(gateBlindAgent)+`,"session":`+intakeQuoted(gateSession)+`},"content":`+intakeQuoted(gateBody)+`}`))
		scene.invokeMeditation(t)
		receipt := scene.awaitMeditationReceipt(t, "call-gate-blind")
		gateAssertDenied(t, receipt, "call-gate-blind")
		if !strings.Contains(receipt, `the observed face of "`+gateMeditatorAgent+`" does not reach "`+gateBlindAgent+`"`) {
			t.Fatalf("receipt = %q, want the blind-target refusal naming %q", receipt, gateBlindAgent)
		}
		scene.refuteDelivered(t)
	})

	t.Run("denies_unbound_session", func(t *testing.T) {
		scene := newGateScene(t, gateSendCall("call-gate-unbound",
			`{"target":{"agent":`+intakeQuoted(gateEntryAgent)+`,"session":"session-nowhere"},"content":`+intakeQuoted(gateBody)+`}`))
		scene.invokeMeditation(t)
		receipt := scene.awaitMeditationReceipt(t, "call-gate-unbound")
		gateAssertDenied(t, receipt, "call-gate-unbound")
		if !strings.Contains(receipt, `no session "session-nowhere" is bound to agent "`+gateEntryAgent+`"`) {
			t.Fatalf("receipt = %q, want the unknown-target refusal naming the unbound session", receipt)
		}
		scene.refuteDelivered(t)
	})

	t.Run("denies_not_running", func(t *testing.T) {
		scene := newGateScene(t, gateSendCall("call-gate-not-running",
			`{"target":{"agent":`+intakeQuoted(gateEntryAgent)+`,"session":`+intakeQuoted(gateSession)+`},"content":`+intakeQuoted(gateBody)+`}`))
		scene.provider.park()
		scene.invokeMeditation(t)
		scene.provider.awaitParked(t)
		closeCtx, cancel := context.WithTimeout(context.Background(), gateWindow)
		defer cancel()
		report, err := scene.host.Run().Close(closeCtx, agent.CloseRequest{
			Address:  scene.entry,
			Deadline: time.Now().Add(300 * time.Millisecond),
		})
		scene.provider.release()
		if err != nil {
			t.Fatalf("the close of the run beside the model in flight = %v, want Cut as a normal return", err)
		}
		if report.Outcome != agent.CloseCut || report.Drained {
			t.Fatalf("close report = (%+v), want Cut beside the call the close left running", report)
		}

		receipt := scene.awaitMeditationReceipt(t, "call-gate-not-running")
		if receipt == symptomReceiptOK {
			t.Fatalf("receipt of the call behind a begun close = %q, want the named refusal of a host with no running session", receipt)
		}
		if !strings.Contains(receipt, "refuses new calls") {
			t.Fatalf("receipt = %q, want the not-running refusal the admission gate answers beside a closing host", receipt)
		}
		scene.refuteDelivered(t)
		if got := len(scene.host.Run().SessionFacts(scene.entry)); got < 2 {
			t.Fatalf("facts of the target chain = %d, want the turn input and answer the bound session already committed and no delivered body", got)
		}
	})
}

// gateAssertDenied states that one receipt is the named refusal of the call it
// answers and never the bare ok of a delivery.
func gateAssertDenied(t *testing.T, receipt, callID string) {
	t.Helper()
	if receipt == symptomReceiptOK {
		t.Fatalf("receipt of %q = %q, want the named refusal of a destination the gate turned away", callID, receipt)
	}
	if !strings.HasPrefix(receipt, "[send_denied] ") {
		t.Fatalf("receipt of %q = %q, want it to open with [send_denied] ", callID, receipt)
	}
}

// gateSendCall renders the one send call a reflecting turn issues.
func gateSendCall(callID, arguments string) string {
	return intakeToolCallAnswer(gateToolSend, callID, arguments)
}

// gateScene is one booted host beside the scripted endpoint it serves: the entry
// session bound, the meditation turn the anchor drives and the requests the
// endpoint really served.
type gateScene struct {
	t        *testing.T
	host     *assembly.Host
	provider *gateProvider
	sender   *symptomSender
	entry    event.Address
	meditate event.Address
	call     string
}

// newGateScene boots the host of the four-gate anchor, binds the entry session
// and answers the scene the checks read.
func newGateScene(t *testing.T, call string) *gateScene {
	t.Helper()
	provider := newGateProvider(t)
	provider.call = call
	sender := &symptomSender{}
	host := gateBoot(t, provider, sender)
	scene := &gateScene{
		t:        t,
		host:     host,
		provider: provider,
		sender:   sender,
		entry:    event.Address{AgentID: gateEntryAgent, SessionID: gateSession},
		meditate: event.Address{AgentID: gateMeditatorAgent, SessionID: gateMeditationSession},
		call:     call,
	}
	scene.bindEntry(t)
	return scene
}

// bindEntry drives one entry turn, so the session every accepted destination
// would name is bound and idle before the reflecting turn asks for it.
func (s *gateScene) bindEntry(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), gateWindow)
	defer cancel()
	turn, err := s.host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-gate-entry",
		Address:      s.entry,
		Input:        []model.Message{{Role: model.RoleUser, Content: gateRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the entry turn that binds the target session: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("entry turn terminal = %v, want the completed turn", turn.Terminal)
	}
	if bound := s.host.Run().ActiveSessions(gateEntryAgent); len(bound) == 0 {
		t.Fatalf("active sessions of %q = %v, want the session the entry turn bound", gateEntryAgent, bound)
	}
}

// invokeMeditation drives one turn of the reflecting agent's loop session, the
// turn every gate is read from.
func (s *gateScene) invokeMeditation(t *testing.T) {
	t.Helper()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*gateWindow)
		defer cancel()
		if _, err := s.host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-gate-meditation",
			Address:      s.meditate,
			Input:        []model.Message{{Role: model.RoleUser, Content: gateRequest}},
		}); err != nil {
			t.Logf("the reflecting turn ended beside: %v", err)
		}
	}()
}

// awaitMeditationReceipt waits for the result text the reflecting turn read beside
// one call.
func (s *gateScene) awaitMeditationReceipt(t *testing.T, callID string) string {
	t.Helper()
	deadline := time.Now().Add(gateWindow)
	for {
		if receipt, ok := symptomReceipt(s.provider.requests(), callID); ok {
			return receipt
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision read the result of %q within %s:\n%s", callID, gateWindow, symptomDescribe(s.provider.requests()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// refuteDelivered states that the body never reached the target chain and never
// left through the outbound channel: a refused call delivers nothing.
func (s *gateScene) refuteDelivered(t *testing.T) {
	t.Helper()
	for _, fact := range s.host.Run().SessionFacts(s.entry) {
		if strings.Contains(fact.Message.Content, gateBody) {
			t.Fatalf("the target chain carries the refused body: %+v", fact)
		}
	}
	for _, body := range s.sender.accepted() {
		if strings.Contains(body, gateBody) {
			t.Fatalf("the refused body left through the outbound channel: %q", body)
		}
	}
}

// gateProvider is the OpenAI-compatible endpoint the four-gate anchor boots
// against: the entry turn answers a plain terminal, and the reflecting turn issues
// the one send call the subtest names.
type gateProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage

	call string
	// hold parks the reflecting decision until the anchor releases it, so a check
	// reads the gate beside a model production that is in flight.
	hold    chan struct{}
	parked  chan struct{}
	claimed bool
}

// newGateProvider starts the endpoint and closes it with the test.
func newGateProvider(t *testing.T) *gateProvider {
	t.Helper()
	provider := &gateProvider{parked: make(chan struct{}, 4)}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// endpoint reports the base URL the host document's provider entry names.
func (p *gateProvider) endpoint() string { return p.server.URL + "/v1" }

// park makes the next reflecting decision wait for the anchor's release.
func (p *gateProvider) park() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold == nil {
		p.hold = make(chan struct{})
	}
}

// release lets a parked reflecting decision answer.
func (p *gateProvider) release() {
	p.mu.Lock()
	hold := p.hold
	p.mu.Unlock()
	if hold != nil {
		close(hold)
	}
}

// awaitParked waits until one reflecting decision reached the park.
func (p *gateProvider) awaitParked(t *testing.T) {
	t.Helper()
	select {
	case <-p.parked:
	case <-time.After(gateWindow):
		t.Fatal("the reflecting decision never reached the park, want the drive to reach its model production")
	}
}

// serve answers one chat completion and records the request it carried.
func (p *gateProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	hold := p.hold
	call := p.call
	claimed := p.claimed
	if modelName == gateMeditatorModel {
		p.claimed = true
	}
	p.mu.Unlock()

	payload := intakeFinalAnswer(gateDone)
	if modelName == gateMeditatorModel && !claimed {
		if hold != nil {
			select {
			case p.parked <- struct{}{}:
			default:
			}
			<-hold
		}
		payload = call
	}
	if payload == "" {
		payload = intakeFinalAnswer(gateDone)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
	_ = index
}

// requests reports the requests the endpoint served, in request order.
func (p *gateProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// gateBoot boots the host of the four-gate anchor through the formal entry the
// deployment uses.
func gateBoot(t *testing.T, provider *gateProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Setenv("TAGENT_SESSION_ID", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(gateHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the observed face"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the four-gate host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the four-gate host ended its close beside: %v", err)
		}
	})
	return host
}

// gateHostDocument renders the document the four-gate anchor boots: one entry
// agent, one reflecting agent whose white list reaches the entry agent and one
// destination it never observed, and one declared agent the white list omits.
func gateHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + gateEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + gateEntryModel + "\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + gateEntryAgent + ":\n")
	b.WriteString("    model: " + gateEntryModel + "\n")
	b.WriteString("  " + gateMeditatorAgent + ":\n")
	b.WriteString("    model: " + gateMeditatorModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + gateToolSend + " }\n")
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [" + gateEntryAgent + ", " + gateBlindAgent + "] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [" + gateEntryAgent + "]\n")
	b.WriteString("      deliver_to: [" + gateEntryAgent + ", " + gateBlindAgent + "]\n")
	b.WriteString("  " + gateBlindAgent + ":\n")
	b.WriteString("    model: " + gateBlindModel + "\n")
	b.WriteString("  " + gateOutsiderAgent + ":\n")
	b.WriteString("    model: " + gateOutsiderModel + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}
