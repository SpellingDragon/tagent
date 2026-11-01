package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// hostDriveEntryAgent names the material the cross-session delivery anchor is written in:
// one entry agent, one peer agent whose session a body is handed to, and one reflecting
// agent whose white list reaches both.
// - hostDriveBody is the body one delivery carries, and the material every check reads back
// - a delivered body lands in the target chain and reaches the target's next model request, while a body that never delivered appears in neither
// - hostDrivePeerSession is the peer session the delivery names, bound before the reflecting turn runs so the address is a live one
const (
	hostDriveEntryAgent     = "user"
	hostDriveEntryModel     = "model-drive-user"
	hostDrivePeerAgent      = "peer"
	hostDrivePeerModel      = "model-drive-peer"
	hostDriveMeditatorAgent = "meditator"
	hostDriveMeditatorModel = "model-drive-meditator"
	hostDrivePeerSession    = "session-drive-peer"

	hostDriveToolSend = "send"

	hostDriveBindingRequest = "drive: bind the peer session"
	hostDrivePeerAnswer     = "drive: the peer session answered"
	hostDriveRequest        = "drive: hand the note to the peer session"
	hostDriveBody           = "drive: the note one session hands a peer"
	hostDriveDone           = "drive: the turn is done"

	hostDriveSendCall = "call-drive-send"
	hostDriveDupCall  = "call-drive-dup-send"
)

// hostDriveMeditationSession is the loop session of the reflecting agent.
const hostDriveMeditationSession = "tagent-session:" + hostDriveEntryAgent + ":meditation"

// hostDriveWindow bounds every wait of the anchor.
const hostDriveWindow = 20 * time.Second

// TestSendDrivesTargetTurn drives the one delivery the cross-session face owes.
// - positive: the call is answered with the bare ok, and the body lands in the target chain under the send lineage
// - positive: the target session is driven through one round that reads the delivered body
// - negative duplicate_delivery_idempotent: the delivery repeated under the identity it already landed with adds no fact
// - negative duplicate_delivery_idempotent: it owes no second round, so a replay never doubles the delivery
// - the model face cannot present one call identity twice, so the anchor repeats the delivery through the cross-session write face it commits on
// - negative: a busy target answers the sender beside its own in-flight decision, so the one-slot gate never deadlocks the sender
// - the body a busy target accepts beside its in-flight decision is read at the target's next boundary, so the turn driven after the parked one carries it
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestSendDrivesTargetTurn(t *testing.T) {
	t.Run("delivery_drives_target_turn", func(t *testing.T) {
		scene := newHostDriveScene(t)
		scene.bindPeer(t)
		scene.invokeMeditation(t, hostDriveSendCall, hostDrivePlainSend(hostDriveSendCall))

		if receipt := scene.awaitReceipt(t, hostDriveSendCall); receipt != hostDriveReceiptOK {
			t.Fatalf("send{target:{agent,session}} result = %q, want the bare %q of a delivered body:\n%s",
				receipt, hostDriveReceiptOK, scene.describe())
		}
		scene.awaitPeerBody(t)
		facts := scene.bodyFacts(t)
		if len(facts) != 1 {
			t.Fatalf("delivered facts = %d, want the one body the call carried: %+v", len(facts), facts)
		}
		if facts[0].Type != event.TypeExternalInput {
			t.Fatalf("delivered fact type = %q, want the external input a delivery lands", facts[0].Type)
		}
		if got := facts[0].Meta[event.MetaKeyTriggerSource]; got != hostDriveLineage {
			t.Fatalf("delivered fact trigger source = %q, want the %q lineage of a body a peer sent", got, hostDriveLineage)
		}
		if facts[0].Target != scene.peer {
			t.Fatalf("delivered fact target = %+v, want the peer session %+v", facts[0].Target, scene.peer)
		}
		if rounds := scene.peerRoundsCarrying(t); len(rounds) != 1 {
			t.Fatalf("target rounds carrying the body = %d, want the one round the delivery owed:\n%s",
				len(rounds), scene.describe())
		}
		scene.refuteOutbound(t)
	})

	t.Run("duplicate_delivery_idempotent", func(t *testing.T) {
		scene := newHostDriveScene(t)
		scene.bindPeer(t)
		scene.invokeMeditation(t, hostDriveDupCall, hostDrivePlainSend(hostDriveDupCall))
		if receipt := scene.awaitReceipt(t, hostDriveDupCall); receipt != hostDriveReceiptOK {
			t.Fatalf("send{target:{agent,session}} result = %q, want the bare %q of a delivered body:\n%s",
				receipt, hostDriveReceiptOK, scene.describe())
		}
		scene.awaitPeerBody(t)
		facts := scene.bodyFacts(t)
		if len(facts) != 1 {
			t.Fatalf("delivered facts = %d, want the one body the call carried:\n%s", len(facts), scene.describe())
		}

		ctx, cancel := context.WithTimeout(context.Background(), hostDriveWindow)
		defer cancel()
		replayed, err := scene.host.Run().CommitToSession(ctx, scene.peer, facts[0])
		if err != nil {
			t.Fatalf("repeat the delivered identity: %v", err)
		}
		if replayed != facts[0].Key {
			t.Fatalf("repeated delivery identity = %q, want the identity it already landed under %q", replayed, facts[0].Key)
		}
		if again := scene.bodyFacts(t); len(again) != 1 {
			t.Fatalf("delivered facts beside the repeated identity = %d, want the single fact it already landed:\n%s",
				len(again), scene.describe())
		}
		if rounds := scene.peerRoundsCarrying(t); len(rounds) != 1 {
			t.Fatalf("target rounds carrying the body = %d, want the one round the first delivery owed:\n%s",
				len(rounds), scene.describe())
		}
		scene.refuteOutbound(t)
	})

	t.Run("busy_target_never_blocks_the_sender", func(t *testing.T) {
		scene := newHostDriveScene(t)
		scene.provider.parkModel(hostDrivePeerModel)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*hostDriveWindow)
			defer cancel()
			if _, err := scene.host.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-drive-bind",
				Address:      scene.peer,
				Input:        []model.Message{{Role: model.RoleUser, Content: hostDriveBindingRequest}},
			}); err != nil {
				t.Logf("the binding peer turn ended beside: %v", err)
			}
		}()
		scene.provider.awaitParked(t)
		scene.invokeMeditation(t, hostDriveSendCall, hostDrivePlainSend(hostDriveSendCall))

		if receipt := scene.awaitReceipt(t, hostDriveSendCall); receipt != hostDriveReceiptOK {
			t.Fatalf("send{target:{agent,session}} result beside a busy target = %q, want the bare %q:\n%s",
				receipt, hostDriveReceiptOK, scene.describe())
		}
		if parked := scene.provider.stillParked(); !parked {
			t.Fatal("the target finished its round before the sender read its receipt, want the delivery answered beside the in-flight decision")
		}
		if facts := scene.bodyFacts(t); len(facts) != 1 {
			t.Fatalf("facts of the busy target = %d, want the body committed at the running decision's boundary:\n%s",
				len(facts), scene.describe())
		}
		scene.provider.release()

		scene.drivePeerTurn(t)
		scene.awaitPeerBody(t)
		if rounds := scene.peerRoundsCarrying(t); len(rounds) != 1 {
			t.Fatalf("target rounds carrying the body = %d, want the one boundary read:\n%s", len(rounds), scene.describe())
		}
		scene.refuteOutbound(t)
	})
}

// hostDriveLineage and hostDriveReceiptOK are the own-lineage constant the delivery stamps
// on the fact it commits, and the bare receipt a delivered body is answered with.
const (
	hostDriveLineage   = "send"
	hostDriveReceiptOK = "ok"
)

// hostDrivePlainSend renders the one send call a reflecting turn issues under the
// call identity the check reads its receipt by.
func hostDrivePlainSend(callID string) string {
	return wiringToolCallsAnswer(wiringCall{
		id:        callID,
		name:      hostDriveToolSend,
		arguments: hostDriveSendArguments(),
	})
}

// hostDriveSendArguments renders the arguments a delivery names: the peer session
// and the body.
func hostDriveSendArguments() string {
	return `{"target":{"agent":` + hostDriveQuoted(hostDrivePeerAgent) +
		`,"session":` + hostDriveQuoted(hostDrivePeerSession) +
		`},"content":` + hostDriveQuoted(hostDriveBody) + `}`
}

// hostDriveQuoted encodes one argument string.
func hostDriveQuoted(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic("host: encode the scripted material: " + err.Error())
	}
	return string(encoded)
}

// hostDriveScene is one booted host beside the scripted endpoint, the peer address a
// body is handed to and the reflecting address that hands it.
type hostDriveScene struct {
	t        *testing.T
	host     *assembly.Host
	provider *hostDriveProvider
	sender   *deliveryRecorder
	peer     event.Address
	meditate event.Address
	mu       sync.Mutex
	turns    int
}

// newHostDriveScene boots the host of the delivery anchor.
func newHostDriveScene(t *testing.T) *hostDriveScene {
	return newHostDriveSceneWith(t, hostDriveDocument)
}

// newHostDriveSceneWith boots the scene of the document one check renders beside the
// assembly options it declares, so an anchor that needs another face of the same
// deployment boots through the very entry a deployment uses. A scene that holds a send
// leaves one obligation waiting for a ruling no check answers, so its close reads out the
// deadline it is given: the bound keeps the anchor fast instead of waiting out a longer one.
func newHostDriveSceneWith(t *testing.T, render func(root, endpoint string) string, options ...assembly.Option) *hostDriveScene {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Setenv("TAGENT_SESSION_ID", "")
	root := t.TempDir()
	provider := newHostDriveProvider(t)
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(render(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the observed face"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, append([]assembly.Option{assembly.WithSender(sender)}, options...)...)
	if err != nil {
		t.Fatalf("boot the delivery host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the delivery host ended its close beside: %v", err)
		}
	})
	return &hostDriveScene{
		t:        t,
		host:     host,
		provider: provider,
		sender:   sender,
		peer:     event.Address{AgentID: hostDrivePeerAgent, SessionID: hostDrivePeerSession},
		meditate: event.Address{AgentID: hostDriveMeditatorAgent, SessionID: hostDriveMeditationSession},
	}
}

// bindPeer drives one peer turn, so the session the delivery names is a live
// binding before the reflecting turn asks for it.
func (s *hostDriveScene) bindPeer(t *testing.T) {
	t.Helper()
	s.drivePeerTurn(t)
}

// drivePeerTurn drives one turn of the peer session and fails the check when the
// turn does not complete.
func (s *hostDriveScene) drivePeerTurn(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hostDriveWindow)
	defer cancel()
	turn, err := s.host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: event.InvocationID("inv-peer-" + strconv.Itoa(len(s.provider.servedRequests()))),
		Address:      s.peer,
		Input:        []model.Message{{Role: model.RoleUser, Content: hostDriveBindingRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the peer turn: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("peer turn terminal = %v, want the completed turn", turn.Terminal)
	}
}

// invokeMeditation drives one turn of the reflecting agent's loop session, the turn
// that issues the send call the check reads.
func (s *hostDriveScene) invokeMeditation(t *testing.T, callID, answer string) {
	t.Helper()
	s.mu.Lock()
	s.turns++
	turn := s.turns
	s.mu.Unlock()
	s.provider.scriptMeditation(callID, answer)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*hostDriveWindow)
		defer cancel()
		if _, err := s.host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: event.InvocationID("inv-drive-meditation-" + strconv.Itoa(turn)),
			Address:      s.meditate,
			Input:        []model.Message{{Role: model.RoleUser, Content: hostDriveRequest}},
		}); err != nil {
			t.Logf("the reflecting turn ended beside: %v", err)
		}
	}()
}

// awaitReceipt waits for the result text the reflecting turn read beside one call.
func (s *hostDriveScene) awaitReceipt(t *testing.T, callID string) string {
	t.Helper()
	deadline := time.Now().Add(hostDriveWindow)
	for {
		for _, request := range s.provider.servedRequests() {
			if receipt, ok := sendRigReceipt(request, callID); ok {
				return receipt
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision read the result of %q within %s:\n%s", callID, hostDriveWindow, s.describe())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitPeerBody waits for a peer request that carried the delivered body.
func (s *hostDriveScene) awaitPeerBody(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(hostDriveWindow)
	for {
		if len(s.peerRoundsCarrying(t)) > 0 {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no peer round carried %q within %s:\n%s", hostDriveBody, hostDriveWindow, s.describe())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// bodyFacts reports the facts of the peer chain that carry the delivered body.
func (s *hostDriveScene) bodyFacts(t *testing.T) []event.Event {
	t.Helper()
	var out []event.Event
	for _, fact := range s.host.Run().SessionFacts(s.peer) {
		if strings.Contains(fact.Message.Content, hostDriveBody) {
			out = append(out, fact)
		}
	}
	return out
}

// peerRoundsCarrying reports the peer requests that carried the delivered body.
func (s *hostDriveScene) peerRoundsCarrying(t *testing.T) []scriptedRequest {
	t.Helper()
	var out []scriptedRequest
	for _, request := range s.provider.servedRequests() {
		if request.Model == hostDrivePeerModel && sendRigCarries(request, hostDriveBody) {
			out = append(out, request)
		}
	}
	return out
}

// refuteOutbound states that the delivered body never left through the outbound
// channel: the session destination is an in-process chain, not a channel.
func (s *hostDriveScene) refuteOutbound(t *testing.T) {
	t.Helper()
	for _, body := range s.sender.bodies() {
		if strings.Contains(body, hostDriveBody) {
			t.Fatalf("the delivered body left through the outbound channel: %q", body)
		}
	}
}

// describe renders the requests the endpoint served, so a failure states what the
// models really read.
func (s *hostDriveScene) describe() string {
	described := make([]string, 0)
	for _, request := range s.provider.servedRequests() {
		messages := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			label := message.Role
			if message.ToolCallID != "" {
				label += "(" + message.ToolCallID + ")"
			}
			messages = append(messages, label+"="+hostDriveQuoted(message.Content))
		}
		described = append(described, "#"+strconv.Itoa(request.Index)+" model="+request.Model+" ["+strings.Join(messages, ", ")+"]")
	}
	return "[" + strings.Join(described, "\n") + "]"
}

// hostDriveProvider is the OpenAI-compatible endpoint the delivery anchor boots
// against: the peer turn answers its own rounds, and the reflecting turn issues the
// calls the check scripts. A model the check parks holds its decision until the check
// releases it, so a busy target is a real in-flight production.
type hostDriveProvider struct {
	*scriptedProvider
	mu       sync.Mutex
	scripts  []hostDriveScript
	issued   map[string]bool
	parkOn   string
	hold     chan struct{}
	parked   chan struct{}
	released bool
}

// hostDriveScript is one decision the reflecting turn is scripted to make: the call
// identity it issues and the answer that carries it, served once.
type hostDriveScript struct {
	callID string
	answer string
}

// newHostDriveProvider starts the endpoint beside the decision script.
func newHostDriveProvider(t *testing.T) *hostDriveProvider {
	t.Helper()
	provider := &hostDriveProvider{
		scriptedProvider: newScriptedProvider(t),
		issued:           map[string]bool{},
		parked:           make(chan struct{}, 8),
	}
	provider.decide = provider.decideRequest
	return provider
}

// scriptMeditation names the call the next reflecting decision issues and the answer
// that carries it. A script is served once, so the decision behind the call reads the
// terminal and never issues the same call identity a second time.
func (p *hostDriveProvider) scriptMeditation(callID, answer string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scripts = append(p.scripts, hostDriveScript{callID: callID, answer: answer})
	p.issued[callID] = false
}

// parkModel parks every decision of one model until the check releases it.
func (p *hostDriveProvider) parkModel(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parkOn = name
	p.hold = make(chan struct{})
}

// release lets a parked model answer.
func (p *hostDriveProvider) release() {
	p.mu.Lock()
	hold := p.hold
	p.released = true
	p.mu.Unlock()
	if hold != nil {
		close(hold)
	}
}

// stillParked reports whether no release happened yet.
func (p *hostDriveProvider) stillParked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.released
}

// awaitParked waits until one decision reached the park.
func (p *hostDriveProvider) awaitParked(t *testing.T) {
	t.Helper()
	select {
	case <-p.parked:
	case <-time.After(hostDriveWindow):
		t.Fatal("the parked model received no request, want the drive to reach its production")
	}
}

// decideRequest answers one request off the model it named and the material it
// carried.
func (p *hostDriveProvider) decideRequest(request scriptedRequest) (string, error) {
	p.mu.Lock()
	parkOn := p.parkOn
	hold := p.hold
	p.mu.Unlock()

	if parkOn != "" && request.Model == parkOn {
		select {
		case p.parked <- struct{}{}:
		default:
		}
		<-hold
		return scriptedAnswerOf(hostDrivePeerAnswer), nil
	}
	if request.Model != hostDriveMeditatorModel {
		return scriptedAnswerOf(hostDrivePeerAnswer), nil
	}
	if answer, ok := p.claimScript(request); ok {
		return answer, nil
	}
	return scriptedAnswerOf(hostDriveDone), nil
}

// claimScript answers the next scripted decision the request reads the input for, and
// reports false beside no script left: a script is served once.
func (p *hostDriveProvider) claimScript(request scriptedRequest) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !sendRigCarries(request, hostDriveRequest) {
		return "", false
	}
	for _, script := range p.scripts {
		if p.issued[script.callID] {
			continue
		}
		p.issued[script.callID] = true
		return script.answer, true
	}
	return "", false
}

// hostDriveDocument renders the document the delivery anchor boots: one entry
// agent, one peer agent whose session a body is handed to, and one reflecting agent
// whose white list reaches both.
func hostDriveDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + hostDriveEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + hostDriveEntryModel + "\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + hostDriveEntryAgent + ":\n")
	b.WriteString("    model: " + hostDriveEntryModel + "\n")
	b.WriteString("  " + hostDrivePeerAgent + ":\n")
	b.WriteString("    model: " + hostDrivePeerModel + "\n")
	b.WriteString("  " + hostDriveMeditatorAgent + ":\n")
	b.WriteString("    model: " + hostDriveMeditatorModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + hostDriveToolSend + " }\n")
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [" + hostDriveEntryAgent + ", " + hostDrivePeerAgent + "] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 2h\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [" + hostDriveEntryAgent + ", " + hostDrivePeerAgent + "]\n")
	b.WriteString("      deliver_to: [" + hostDriveEntryAgent + ", " + hostDrivePeerAgent + "]\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}
