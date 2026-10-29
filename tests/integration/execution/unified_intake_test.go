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
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// intakeEntryAgent names the material this anchor is written in, so every message is
// material the endpoint served and every invocation reaches an address the document
// really declares.
//
// - intakeSlowBody is the body the in-flight call delivers, and the outbound face parks on it
// - intakeSlowCallID is the call identity of that call, so a decision is read from the tool message that pairs it
const (
	intakeEntryAgent       = "user"
	intakeEntryModel       = "model-entry"
	intakeSession          = "session-intake"
	intakeFirstInvocation  = "inv-intake-first"
	intakeSecondInvocation = "inv-intake-second"
	intakeSlowCallID       = "call-intake-slow"
	intakeSlowBody         = "intake: the slow call is running"
	intakeFirstBody        = "intake: the message that opens the turn"
	intakeSecondBody       = "intake: the message that arrives in flight"
	intakeRepeatBody       = "intake: the same text sent twice"
	intakeFinalBody        = "intake: the slow call finished"
)

// intakeAcceptWindow bounds how long an in-flight invocation may take to answer (the
// anchor's red assertion is that it answers nothing at all), intakeBootWindow bounds
// the wait for the call to park, intakeSettleWindow guards a harness that broke, and
// intakeParkCeiling bounds one parked delivery so a call nothing releases fails by
// name instead of hanging the package.
const (
	intakeAcceptWindow = 2 * time.Second
	intakeBootWindow   = 20 * time.Second
	intakeSettleWindow = 20 * time.Second
	intakeParkCeiling  = 30 * time.Second
)

// TestMessageReachesTheNextDecisionOfAInFlightTurn checks that a message arriving inside a slow call reaches the next decision of the same turn.
//
// - positive: the invocation of that message is answered as accepted and the message stands in the next decision once
// - negative: the decision already running reads nothing of it, and the turn in flight ends by its own terminal
// - negative: on the gate-shaped intake the message opens a turn of its own, which is the shape this anchor measures against
// - negative: a fact landing beside unfinished material only appends, so the projection prefix that stood stays an exact prefix
// - negative: a progress notice enters no decision input, in this window too
// - negative: two identical texts sent in one turn stand in the input twice, since the consumed identity de-duplicates
// - consumer: every invocation is one Host.Invoke over a host booted from a real document, read back from the requests served
//
// 契约: docs/wiki/agent/event-flow.md
func TestMessageReachesTheNextDecisionOfAInFlightTurn(t *testing.T) {
	t.Run("the message lands in the next decision of the in-flight turn", func(t *testing.T) {
		scene := intakeDrive(t, intakeSecondBody)

		t.Run("positive: the in-flight invocation is accepted and the message reaches the next decision once", func(t *testing.T) {
			accepted := scene.accepted[0]
			if accepted.waited {
				t.Fatalf("the invocation of the in-flight message returned nothing within %s: it parked on the session gate, "+
					"so the message waits for the turn to end instead of reaching the decision boundary of the turn in flight "+
					"(requests served beside it = %d)", intakeAcceptWindow, len(scene.requests))
			}
			if accepted.err != nil {
				t.Fatalf("invoke beside the in-flight turn = %v, want the message accepted beside no error", accepted.err)
			}
			if accepted.turn.Terminal != model.KindUnknown || len(accepted.turn.Decisions) != 0 {
				t.Fatalf("accepted turn = (%v, %d decisions), want the receipt the host answers an in-flight message with: "+
					"no decision reached and no error", accepted.turn.Terminal, len(accepted.turn.Decisions))
			}
			at, ok := intakeDecisionAfterCall(scene.requests)
			if !ok {
				t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
			}
			if copies := intakeUserCopies(scene.requests[at], intakeSecondBody); copies != 1 {
				t.Fatalf("copies of the in-flight message in the next decision = %d, want exactly 1 (%s)",
					copies, intakeDescribe(scene.requests))
			}
			if landed := intakeFactsCarrying(scene.facts, intakeSecondBody); len(landed) != 1 {
				t.Fatalf("facts carrying the in-flight message = %d, want the one fact the message lands as: "+
					"an accepted message is committed once and no later turn commits it again", len(landed))
			}
		})

		t.Run("negative: the in-flight turn reads the message from no other lane", func(t *testing.T) {
			if scene.firstErr != nil || scene.first.Terminal != model.KindCompleted {
				t.Fatalf("the turn in flight = (%v, %v), want it to end by its own terminal beside the message that arrived in it",
					scene.first.Terminal, scene.firstErr)
			}
			if copies := intakeUserCopies(scene.requests[0], intakeSecondBody); copies != 0 {
				t.Fatalf("copies of the in-flight message in the decision already running = %d, want 0: "+
					"a running decision is never fed the message that arrives beside it (%s)",
					copies, intakeDescribe(scene.requests))
			}
			accepted := scene.accepted[0]
			at, ok := intakeDecisionAfterCall(scene.requests)
			if !ok {
				t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
			}
			copies := intakeUserCopies(scene.requests[at], intakeSecondBody)
			if accepted.waited {
				// TODO(A5): migrate this branch onto the added requirement when the anchor set moves
				if copies != 0 {
					t.Fatalf("copies of the in-flight message in the next decision = %d, want the baseline shape of 0 "+
						"beside an invocation that never returned (%s)", copies, intakeDescribe(scene.requests))
				}
				if carriers := intakeRequestsCarryingUser(scene.requests, intakeSecondBody); len(carriers) == 0 {
					t.Fatalf("requests = %s, want the baseline to read the message in a turn of its own after the gate freed it",
						intakeDescribe(scene.requests))
				}
				return
			}
			if len(scene.requests) != 2 {
				t.Fatalf("model requests = %d (%s), want the two decisions of the one in-flight turn: "+
					"an in-flight message drives no turn of its own", len(scene.requests), intakeDescribe(scene.requests))
			}
		})

		t.Run("negative: the projection prefix of the unfinished material is unchanged", func(t *testing.T) {
			if !scene.beforeCarried || !scene.afterCarried {
				t.Fatalf("projection carried = (%t, %t), want the session chain to register the mapped sequence the prefix is read from",
					scene.beforeCarried, scene.afterCarried)
			}
			if len(scene.after) < len(scene.before) {
				t.Fatalf("projection entries = %d after, %d before, want a landed fact to append and never to shorten the chain",
					len(scene.after), len(scene.before))
			}
			for at := range scene.before {
				if scene.after[at].Key != scene.before[at].Key ||
					scene.after[at].Type != scene.before[at].Type ||
					scene.after[at].Content != scene.before[at].Content {
					t.Fatalf("projection entry %d = %+v, want the entry %+v that stood when the message arrived: "+
						"material that is not finished is never rewritten by a fact landing beside it",
						at, scene.after[at], scene.before[at])
				}
			}
			// TODO(A2): pin the ended-material boundary itself beside this prefix, the position the next request presents
		})

		t.Run("negative: no decision input carries progress material", func(t *testing.T) {
			if len(scene.requests) == 0 {
				t.Fatalf("model requests = 0, want the decisions of the in-flight turn to be served")
			}
			for _, request := range scene.requests {
				for _, message := range request.Messages {
					if strings.Contains(message.Content, progressNoteFragment) {
						t.Fatalf("request messages = %+v, want no progress report in the material a decision reads: "+
							"the in-flight window is no second lane for notices", request.Messages)
					}
				}
			}
		})
	})

	t.Run("the same text sent twice in one turn stands in the input twice", func(t *testing.T) {
		scene := intakeDrive(t, intakeRepeatBody, intakeRepeatBody)
		at, ok := intakeDecisionAfterCall(scene.requests)
		if !ok {
			t.Fatalf("requests = %s, want the decision that reads the result of %s", intakeDescribe(scene.requests), intakeSlowCallID)
		}
		if copies := intakeUserCopies(scene.requests[at], intakeRepeatBody); copies != 2 {
			t.Fatalf("copies of the same text in the next decision = %d, want 2: the consumed identity de-duplicates "+
				"and the content does not (%s)", copies, intakeDescribe(scene.requests))
		}
	})
}

// intakeScene is one run of the anchor: the invocation that opens the turn, the
// invocations that arrive beside it, and the material read back from the model
// endpoint and the run itself.
type intakeScene struct {
	addr     event.Address
	first    agent.Turn
	firstErr error
	// accepted carries one entry per message the run sent in flight, in issue
	// order; waited marks the entry whose invocation answered nothing inside
	// the acceptance window and was read after the parked call was released.
	accepted      []intakeAcceptance
	requests      []intakeRequest
	before        []replay.ProjectionEntry
	beforeCarried bool
	after         []replay.ProjectionEntry
	afterCarried  bool
	facts         []event.Event
}

// intakeAcceptance is the receipt one in-flight invocation answered with, or the
// record that it answered nothing before the acceptance window ended.
type intakeAcceptance struct {
	turn   agent.Turn
	err    error
	waited bool
}

// intakeDrive runs the anchor once: it opens a turn whose first decision issues
// the slow call, waits until that call is really parked inside the outbound
// face, reads the projection prefix that stands at that moment, sends the given
// messages in the same session, releases the call, and reads back what the
// model and the run recorded.
func intakeDrive(t *testing.T, inFlight ...string) *intakeScene {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	scene := &intakeScene{addr: event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession}}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		scene.first, scene.firstErr = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeFirstInvocation,
			Address:      scene.addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: intakeFirstBody}},
		})
	}()
	sender.awaitParked(t)
	scene.before, scene.beforeCarried = host.Run().ReplayEntries(scene.addr.SessionID)

	type arrival struct {
		turn agent.Turn
		err  error
	}
	arrivals := make([]chan arrival, len(inFlight))
	for at, body := range inFlight {
		arrivals[at] = make(chan arrival, 1)
		invocationID := event.InvocationID(fmt.Sprintf("%s-%d", intakeSecondInvocation, at))
		go func(ch chan arrival, id event.InvocationID, content string) {
			turn, err := host.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: id,
				Address:      scene.addr,
				Input:        []model.Message{{Role: model.RoleUser, Content: content}},
			})
			ch <- arrival{turn: turn, err: err}
		}(arrivals[at], invocationID, body)
	}
	for at := range inFlight {
		select {
		case answer := <-arrivals[at]:
			scene.accepted = append(scene.accepted, intakeAcceptance{turn: answer.turn, err: answer.err})
		case <-time.After(intakeAcceptWindow):
			scene.accepted = append(scene.accepted, intakeAcceptance{waited: true})
		}
	}

	sender.release()
	select {
	case <-firstDone:
	case <-time.After(intakeSettleWindow):
		t.Fatalf("the turn in flight never ended after its call was released")
	}
	for at := range inFlight {
		if !scene.accepted[at].waited {
			continue
		}
		select {
		case answer := <-arrivals[at]:
			scene.accepted[at] = intakeAcceptance{turn: answer.turn, err: answer.err, waited: true}
		case <-time.After(intakeSettleWindow):
			t.Fatalf("the in-flight invocation %d never returned beside a released call", at)
		}
	}
	scene.requests = provider.requests()
	scene.after, scene.afterCarried = host.Run().ReplayEntries(scene.addr.SessionID)
	scene.facts = host.Run().SessionFacts(scene.addr)
	return scene
}

// intakeProvider is the OpenAI-compatible endpoint the anchor boots against: the
// first request of the run issues the slow call, every later request ends the
// turn that reads it, and every request is recorded in the shape the anchor
// reads its evidence from.
type intakeProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
}

// newIntakeProvider starts the endpoint and closes it with the test.
func newIntakeProvider(t *testing.T) *intakeProvider {
	t.Helper()
	provider := &intakeProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the request it carried.
func (p *intakeProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	payload := intakeFinalAnswer(intakeFinalBody)
	if index == 0 {
		payload = intakeToolCallAnswer("send", intakeSlowCallID, `{"content":`+intakeQuoted(intakeSlowBody)+`}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
}

// requests reports the requests the endpoint served, in request order.
func (p *intakeProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// endpoint reports the base URL the host document's provider entry names.
func (p *intakeProvider) endpoint() string { return p.server.URL + "/v1" }

// intakeRequest is one chat completion request as the endpoint decoded it.
type intakeRequest struct {
	Index    int
	Model    string
	Messages []intakeMessage
	Tools    []string
}

// intakeMessage is one message a request carried.
type intakeMessage struct {
	Role       string
	Content    string
	ToolCallID string
}

// intakeDecodeRequest reads one recorded body into the shape the anchor reads.
func intakeDecodeRequest(index int, modelName string, body map[string]json.RawMessage) intakeRequest {
	request := intakeRequest{Index: index, Model: modelName}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				request.Messages = append(request.Messages, intakeMessage{
					Role:       message.Role,
					Content:    intakeContent(message.Content),
					ToolCallID: message.ToolCallID,
				})
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// intakeContent reads one message content the protocol may render as a plain
// string or as text parts.
func intakeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "")
}

// intakeQuoted renders one string as the JSON literal a scripted answer carries.
func intakeQuoted(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic("execution: encode the scripted material: " + err.Error())
	}
	return string(encoded)
}

// intakeToolCallAnswer renders the chat completion that issues one native call.
func intakeToolCallAnswer(name, callID, arguments string) string {
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"calling","tool_calls":[{"id":"` + callID +
		`","type":"function","function":{"name":"` + name + `","arguments":` + intakeQuoted(arguments) + `}}]},` +
		`"finish_reason":"tool_calls"}]}`
}

// intakeFinalAnswer renders the chat completion that ends the turn.
func intakeFinalAnswer(content string) string {
	return `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":` + intakeQuoted(content) + `},"finish_reason":"stop"}]}`
}

// intakeSender is the outbound face of the booted host: it records every body it
// accepted, and it parks the one body the slow call delivers until the anchor
// releases it, so the in-flight window is a real tool execution rather than an
// elapsed timeout.
type intakeSender struct {
	mu      sync.Mutex
	targets []event.Address
	bodies  []string
	parked  chan struct{}
	freed   chan struct{}
	once    sync.Once
	opened  sync.Once
}

// newIntakeSender returns the recording outbound face of one run.
func newIntakeSender() *intakeSender {
	return &intakeSender{parked: make(chan struct{}), freed: make(chan struct{})}
}

// Send records one accepted body and parks the slow call's body until release.
func (s *intakeSender) Send(ctx context.Context, target event.Address, content string) error {
	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.bodies = append(s.bodies, content)
	s.mu.Unlock()
	if content != intakeSlowBody {
		return nil
	}
	s.opened.Do(func() { close(s.parked) })
	select {
	case <-s.freed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(intakeParkCeiling):
		return fmt.Errorf("execution: the slow call was never released")
	}
}

// awaitParked waits until the slow call really reached the outbound face.
func (s *intakeSender) awaitParked(t *testing.T) {
	t.Helper()
	select {
	case <-s.parked:
	case <-time.After(intakeBootWindow):
		t.Fatalf("no decision reached the slow call within %s, so the in-flight window was never opened", intakeBootWindow)
	}
}

// release frees the parked call, exactly once.
func (s *intakeSender) release() {
	s.once.Do(func() { close(s.freed) })
}

// intakeHostDocument renders the one document the anchor boots: the entry agent
// keeps the declared send tool, whose outbound face the anchor supplies, so one
// real decision can be parked inside a real tool execution.
func intakeHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + intakeEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + intakeEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + intakeEntryAgent + ":\n")
	b.WriteString("    model: " + intakeEntryModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: send }\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// intakeBoot boots the formal host the anchor drives, so every invocation below
// reaches the runtime through the public entry the deployment uses.
func intakeBoot(t *testing.T, provider *intakeProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(intakeHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the intake host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the intake host ended its close beside: %v", err)
		}
	})
	return host
}

// intakeDecisionAfterCall reports the first request that reads the result of the
// scripted call: the decision boundary the in-flight message has to reach.
func intakeDecisionAfterCall(requests []intakeRequest) (int, bool) {
	for at, request := range requests {
		for _, message := range request.Messages {
			if message.Role == string(model.RoleTool) && message.ToolCallID == intakeSlowCallID {
				return at, true
			}
		}
	}
	return 0, false
}

// intakeUserCopies counts the user messages of one request that carry the named
// material.
func intakeUserCopies(request intakeRequest, material string) int {
	count := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.Contains(message.Content, material) {
			count++
		}
	}
	return count
}

// intakeFactsCarrying reports the committed facts of the session whose message
// carries the named material, so a check reads the chain the run really holds
// rather than the shape it meant to write.
func intakeFactsCarrying(facts []event.Event, material string) []event.Event {
	out := make([]event.Event, 0, len(facts))
	for _, fact := range facts {
		if strings.Contains(fact.Message.Content, material) && fact.Type == event.TypeExternalInput {
			out = append(out, fact)
		}
	}
	return out
}

// intakeRequestsCarryingUser reports the requests that carry the named material
// as user input.
func intakeRequestsCarryingUser(requests []intakeRequest, material string) []int {
	out := make([]int, 0, len(requests))
	for at, request := range requests {
		if intakeUserCopies(request, material) > 0 {
			out = append(out, at)
		}
	}
	return out
}

// intakeDescribe renders the requests the anchor reads its evidence from: the
// identity each one named, and the role and call pairing of every message, so a
// failure states what the model really saw.
func intakeDescribe(requests []intakeRequest) string {
	described := make([]string, 0, len(requests))
	for _, request := range requests {
		roles := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			role := message.Role
			if message.ToolCallID != "" {
				role += "(" + message.ToolCallID + ")"
			}
			roles = append(roles, role)
		}
		described = append(described, fmt.Sprintf("#%d model=%q messages=%v tools=%v", request.Index, request.Model, roles, request.Tools))
	}
	return "[" + strings.Join(described, "; ") + "]"
}
