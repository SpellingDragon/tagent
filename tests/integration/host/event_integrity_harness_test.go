// The integrity harness of the host: one declared multi-decision script drives
// the formal assembly, and the checks read back the requests the provider
// actually served beside the actions the sender actually answered. A request
// outside the script, a declared step never reached and a step reached twice
// are recorded as named violations, never swallowed by a default answer.
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package host

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// integrityUnexpected, integrityDuplicate and integrityMissing are the violation kinds the integrity script records.
const (
	integrityUnexpected = "unexpected"
	integrityDuplicate  = "duplicate"
	integrityMissing    = "missing"
)

// integrityViolation is one way a drive left the script it declared.
type integrityViolation struct {
	kind   string
	detail string
}

// String renders the violation as the line a failed check reads.
func (v integrityViolation) String() string { return v.kind + ": " + v.detail }

// integrityScript is the declared decision script of one drive: the steps of
// each model identity in the order the script expects them, beside the
// violations the drive produced against it.
type integrityScript struct {
	mu         sync.Mutex
	streams    []*integrityStream
	byIdentity map[string]*integrityStream
	violations []integrityViolation
	violated   chan struct{}
	once       sync.Once
}

// integrityStream is the ordered step list of one model identity.
type integrityStream struct {
	identity string
	steps    []*integrityStep
	next     int
}

// integrityStep is one declared decision: the request it matches, the answer it
// serves, the outbound body it declares beside an optional park the check
// releases.
type integrityStep struct {
	name        string
	identity    string
	match       func(scriptedRequest) bool
	answering   string
	body        string
	arrived     chan struct{}
	arrivedOnce sync.Once
	hold        chan struct{}
}

// newIntegrityScript returns the script a drive declares its steps over.
func newIntegrityScript() *integrityScript {
	return &integrityScript{byIdentity: map[string]*integrityStream{}, violated: make(chan struct{})}
}

// expect declares the next step of one model identity: the request the step
// matches and the answer it serves.
func (s *integrityScript) expect(identity, name string, match func(scriptedRequest) bool) *integrityStep {
	stream := s.byIdentity[identity]
	if stream == nil {
		stream = &integrityStream{identity: identity}
		s.byIdentity[identity] = stream
		s.streams = append(s.streams, stream)
	}
	step := &integrityStep{name: name, identity: identity, match: match, arrived: make(chan struct{})}
	stream.steps = append(stream.steps, step)
	return step
}

// answers declares the payload the provider serves on this step.
func (s *integrityStep) answers(payload string) *integrityStep {
	s.answering = payload
	return s
}

// delivers declares the outbound body this step's decision causes, so the
// checks map every recorded action back to the step that produced it.
func (s *integrityStep) delivers(body string) *integrityStep {
	s.body = body
	return s
}

// park declares that the provider waits on this step until the check releases
// it, the model barrier a check reads an in-flight drive from.
func (s *integrityStep) park() *integrityStep {
	s.hold = make(chan struct{})
	return s
}

// stepsOf reports the declared steps of one identity in declaration order.
func (s *integrityScript) stepsOf(identity string) []*integrityStep {
	stream := s.byIdentity[identity]
	if stream == nil {
		return nil
	}
	return append([]*integrityStep(nil), stream.steps...)
}

// actionBodies reports the outbound bodies the script declares, in declaration
// order, so the checks compare them with the actions the sender recorded.
func (s *integrityScript) actionBodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, stream := range s.streams {
		for _, step := range stream.steps {
			if step.body != "" {
				out = append(out, step.body)
			}
		}
	}
	return out
}

// decide answers one request the way the script declares it: the next step of
// the stream the request's identity names, and nothing else. A request outside
// the script, a step reached twice and a step reached after the script ran out
// are refused, so the drive fails instead of reading a default answer.
func (s *integrityScript) decide(request scriptedRequest) (string, error) {
	s.mu.Lock()
	stream := s.byIdentity[request.Model]
	if stream == nil {
		return s.refuseLocked(integrityViolation{
			kind:   integrityUnexpected,
			detail: fmt.Sprintf("request %d named model %q, which the script never declares", request.Index+1, request.Model),
		})
	}
	if stream.next < len(stream.steps) {
		step := stream.steps[stream.next]
		if step.match(request) {
			stream.next++
			step.arrivedOnce.Do(func() { close(step.arrived) })
			hold, payload := step.hold, step.answering
			s.mu.Unlock()
			if hold != nil {
				<-hold
			}
			return payload, nil
		}
	}
	if step, ok := stream.reachedLocked(request); ok {
		return s.refuseLocked(integrityViolation{
			kind: integrityDuplicate,
			detail: fmt.Sprintf("step %q of model %q was reached twice (request %d)",
				step.name, step.identity, request.Index+1),
		})
	}
	expected := "no further step of this model"
	if stream.next < len(stream.steps) {
		expected = fmt.Sprintf("step %q of model %q", stream.steps[stream.next].name, stream.identity)
	}
	return s.refuseLocked(integrityViolation{
		kind: integrityUnexpected,
		detail: fmt.Sprintf("request %d of model %q matched no step; the script expected %s",
			request.Index+1, request.Model, expected),
	})
}

// reachedLocked reports whether one consumed step of the stream matches the
// request again, the shape a repeated step arrives as.
func (st *integrityStream) reachedLocked(request scriptedRequest) (*integrityStep, bool) {
	for _, step := range st.steps[:st.next] {
		if step.match(request) {
			return step, true
		}
	}
	return nil, false
}

// refuseLocked records one violation and answers the refusal the provider
// serves, under the script lock the caller already holds.
func (s *integrityScript) refuseLocked(violation integrityViolation) (string, error) {
	s.violations = append(s.violations, violation)
	s.once.Do(func() { close(s.violated) })
	s.mu.Unlock()
	return "", fmt.Errorf("integrity script: %s", violation)
}

// audit reports every violation the drive left: the ones it recorded while
// serving, beside every declared step the drive never reached.
func (s *integrityScript) audit() []integrityViolation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]integrityViolation(nil), s.violations...)
	for _, stream := range s.streams {
		for _, step := range stream.steps[stream.next:] {
			out = append(out, integrityViolation{
				kind:   integrityMissing,
				detail: fmt.Sprintf("step %q of model %q was declared but never reached", step.name, step.identity),
			})
		}
	}
	return out
}

// await waits until the drive reaches one declared step, failing on the first
// violation the script records instead of waiting out the timeout.
func (s *integrityScript) await(t *testing.T, step *integrityStep) {
	t.Helper()
	select {
	case <-step.arrived:
	case <-s.violated:
		t.Fatalf("the script was violated before step %q arrived: %v", step.name, s.audit())
	case <-time.After(60 * time.Second):
		t.Fatalf("the drive never reached step %q of model %q", step.name, step.identity)
	}
}

// release lets one parked step answer.
func (s *integrityScript) release(step *integrityStep) {
	s.mu.Lock()
	defer s.mu.Unlock()
	releaseStepLocked(step)
}

// releaseAll lets every parked step answer, the cleanup a failed check owes the
// server it leaves behind.
func (s *integrityScript) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stream := range s.streams {
		for _, step := range stream.steps {
			releaseStepLocked(step)
		}
	}
}

// releaseStepLocked closes the park of one step once.
func releaseStepLocked(step *integrityStep) {
	if step.hold == nil {
		return
	}
	select {
	case <-step.hold:
	default:
		close(step.hold)
	}
}

// integrityViolationsOf reports the violations of one kind.
func integrityViolationsOf(violations []integrityViolation, kind string) []integrityViolation {
	out := make([]integrityViolation, 0, len(violations))
	for _, violation := range violations {
		if violation.kind == kind {
			out = append(out, violation)
		}
	}
	return out
}

// requestTail matches the request whose last message is the user input given.
func requestTail(input string) func(scriptedRequest) bool {
	return func(request scriptedRequest) bool {
		if len(request.Messages) == 0 {
			return false
		}
		last := request.Messages[len(request.Messages)-1]
		return last.Role == string(model.RoleUser) && last.Content == input
	}
}

// requestCarriesToolResult matches the request that carries the result of the
// named tool call, the shape a continued decision arrives as. The protocol
// renders the running notice as a tool message and the settled result as the
// user material beside the same call id, so the match reads the call id.
func requestCarriesToolResult(callID string) func(scriptedRequest) bool {
	return func(request scriptedRequest) bool {
		for _, message := range request.Messages {
			if message.ToolCallID == callID {
				return true
			}
		}
		return false
	}
}

// integrityOutcome is one drive's turn beside the error it ended on.
type integrityOutcome struct {
	turn agent.Turn
	err  error
}

// awaitIntegrityDrive waits for one drive to return.
func awaitIntegrityDrive(t *testing.T, driven <-chan integrityOutcome, what string) integrityOutcome {
	t.Helper()
	select {
	case got := <-driven:
		return got
	case <-time.After(60 * time.Second):
		t.Fatalf("the drive of %s never returned", what)
		return integrityOutcome{}
	}
}

// driveIntegrityTurn drives one turn at the given address over the host.
func driveIntegrityTurn(ctx context.Context, host *tagent.Host, addr event.Address, input string) (agent.Turn, error) {
	return host.Invoke(ctx, tagent.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: input}},
	})
}

// bootIntegrityHost boots the scripted entry document the integrity checks
// drive: the entry agent beside the declared send tool, resolved at the
// scripted provider.
func bootIntegrityHost(t *testing.T, provider *scriptedProvider, sender recordingSender) *tagent.Host {
	t.Helper()
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-entry",
		"    tools:\n"+
			"      - { kind: tool, id: send }\n"))
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the integrity host: %v", err)
	}
	return host
}

// bootIntegrityHostWithAgents boots one host document whose agent sections are
// given verbatim, so a check declares the identities its script names.
func bootIntegrityHostWithAgents(t *testing.T, provider *scriptedProvider, sender recordingSender, sections string) *tagent.Host {
	t.Helper()
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", sections))
	host, err := tagent.Boot(configPath, tagent.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the integrity host: %v", err)
	}
	return host
}

// keepIntegrityHost releases every park and stops the host when the check ends,
// so a failed check leaves neither a parked provider nor a listening face.
func keepIntegrityHost(t *testing.T, script *integrityScript, host *tagent.Host) {
	t.Helper()
	t.Cleanup(func() {
		script.releaseAll()
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the integrity host ended its close beside: %v", err)
		}
	})
}

// closeIntegrityHost drains the run the host drives and requires the formal
// obligations to converge before the host closes: the confirmable drain point
// of a check, never a sleep beside a still-open obligation.
func closeIntegrityHost(t *testing.T, ctx context.Context, host *tagent.Host) {
	t.Helper()
	report, err := host.Run().Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(20 * time.Second)})
	if err != nil {
		t.Fatalf("drain the run the host drives: %v", err)
	}
	if !report.Drained || len(report.Unsettled) != 0 {
		t.Fatalf("drain report = %+v, want every formal obligation settled before the close", report)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// runIntegrityPositive drives one declared multi-decision script over the boot
// assembly: the input reaches the entry, the send tool answers through the
// recording sender, the tool result drives the second decision, and the final
// answer closes the turn. Every model step is parked, so the check reads the
// outbound actions the drive produced before it lets the decision continue.
func runIntegrityPositive(t *testing.T) {
	t.Helper()
	const (
		input      = "report the two notes"
		firstNote  = "the first note"
		secondNote = "the second note"
		settled    = "the entry settled both notes"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry asks for the first note", requestTail(input)).
		answers(settleToolCallAnswer("send", "call-note-1", `{"content":"the first note"}`)).
		delivers(firstNote).
		park()
	script.expect("model-entry", "the entry asks for the second note", requestCarriesToolResult("call-note-1")).
		answers(settleToolCallAnswer("send", "call-note-2", `{"content":"the second note"}`)).
		delivers(secondNote).
		park()
	script.expect("model-entry", "the entry ends on its final answer", requestCarriesToolResult("call-note-2")).
		answers(scriptedAnswerOf(settled)).
		delivers(settled).
		park()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootIntegrityHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-event-integrity-positive"}
	steps := script.stepsOf("model-entry")
	if len(steps) != 3 {
		t.Fatalf("declared steps = %d, want the three decisions of the script", len(steps))
	}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, input)
		driven <- integrityOutcome{turn: turn, err: err}
	}()

	script.await(t, steps[0])
	if actions := sender.actions(); len(actions) != 0 {
		t.Fatalf("outbound actions before the first decision answered = %+v, want none", actions)
	}
	script.release(steps[0])

	script.await(t, steps[1])
	if actions := sender.actions(); len(actions) != 1 || actions[0].body != firstNote {
		t.Fatalf("outbound actions beside the first tool result = %+v, want the first note alone", actions)
	}
	script.release(steps[1])

	script.await(t, steps[2])
	if actions := sender.actions(); len(actions) != 2 || actions[1].body != secondNote {
		t.Fatalf("outbound actions beside the second tool result = %+v, want both notes and no final answer yet", actions)
	}
	script.release(steps[2])

	got := awaitIntegrityDrive(t, driven, "the scripted positive turn")
	if got.err != nil {
		t.Fatalf("the scripted turn ended by error: %v", got.err)
	}
	if got.turn.Terminal != model.KindCompleted || len(got.turn.Decisions) != 3 {
		t.Fatalf("turn = (%v, %d decisions), want the completed turn of the three scripted decisions",
			got.turn.Terminal, len(got.turn.Decisions))
	}

	requests := provider.servedRequests()
	if len(requests) != 3 {
		t.Fatalf("model requests = %d, want one per scripted decision", len(requests))
	}
	for index, request := range requests {
		if request.Model != "model-entry" {
			t.Fatalf("request %d named model %q, want the entry identity of its step", index+1, request.Model)
		}
	}
	if !requestCarriesToolResult("call-note-1")(requests[1]) || !requestCarriesToolResult("call-note-2")(requests[2]) {
		t.Fatalf("requests = %+v, want the tool result of each step in the request that follows it", requests)
	}

	actions := sender.actions()
	want := []string{firstNote, secondNote, settled}
	if len(actions) != len(want) {
		t.Fatalf("outbound actions = %+v, want the %d actions the script declares", actions, len(want))
	}
	for index, action := range actions {
		if action.body != want[index] || action.target != addr || !action.confirmed() {
			t.Fatalf("action %d = %+v, want %q addressed to %+v and confirmed", index+1, action, want[index], addr)
		}
	}
	if declared := script.actionBodies(); !reflect.DeepEqual(declared, want) {
		t.Fatalf("declared action bodies = %q, want the observed %q", declared, want)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the positive script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runIntegrityNegative drives three scripts that each leave the declared steps,
// and requires every departure to fail the drive by name: a request outside the
// script, a declared step the drive never reached, and a step reached twice.
func runIntegrityNegative(t *testing.T) {
	t.Helper()
	checkIntegrityUnexpectedRequestFails(t)
	checkIntegrityMissingStepFails(t)
	checkIntegrityDuplicateStepFails(t)
}

// checkIntegrityUnexpectedRequestFails drives a script that declares one
// decision beside a drive that needs two: the second request matches no step
// and must fail instead of reading a default answer.
func checkIntegrityUnexpectedRequestFails(t *testing.T) {
	t.Helper()
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the only declared decision", requestTail("one request only")).
		answers(settleToolCallAnswer("send", "call-only", `{"content":"only note"}`)).
		delivers("only note")
	provider.decide = script.decide
	sender := newRecordingSender()
	host := bootIntegrityHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-event-integrity-unexpected"}

	turn, err := driveIntegrityTurn(ctx, host, addr, "one request only")
	if err == nil && turn.Terminal != model.KindFailed {
		t.Fatalf("a model request outside the script did not fail the turn: turn = (%v, %d decisions), delivered = %q",
			turn.Terminal, len(turn.Decisions), sender.bodies())
	}
	if requests := provider.servedRequests(); len(requests) != 2 {
		t.Fatalf("model requests = %d, want the declared decision beside the refused one", len(requests))
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != "only note" {
		t.Fatalf("delivered bodies = %q, want the one action the declared step caused", bodies)
	}
	violations := script.audit()
	if got := integrityViolationsOf(violations, integrityUnexpected); len(got) != 1 {
		t.Fatalf("violations = %v, want the one unexpected request named", violations)
	}
	if got := integrityViolationsOf(violations, integrityDuplicate); len(got) != 0 {
		t.Fatalf("violations = %v, want no duplicate step beside the unexpected request", violations)
	}
	if got := integrityViolationsOf(violations, integrityMissing); len(got) != 0 {
		t.Fatalf("violations = %v, want no missing step beside the unexpected request", violations)
	}
}

// checkIntegrityMissingStepFails declares a tool-result step the drive never
// reaches, because the decision before it ends the turn: the declared step must
// fail as missing at the drain point.
func checkIntegrityMissingStepFails(t *testing.T) {
	t.Helper()
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry answers before any tool result", requestTail("answer before the tool")).
		answers(scriptedAnswerOf("answered early")).
		delivers("answered early")
	script.expect("model-entry", "the tool result of a send call the drive never makes", requestCarriesToolResult("call-never")).
		answers(scriptedAnswerOf("never reached"))
	provider.decide = script.decide
	sender := newRecordingSender()
	host := bootIntegrityHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-event-integrity-missing"}

	turn, err := driveIntegrityTurn(ctx, host, addr, "answer before the tool")
	if err != nil {
		t.Fatalf("the scripted turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the declared answer", turn.Terminal)
	}
	violations := script.audit()
	missing := integrityViolationsOf(violations, integrityMissing)
	if len(missing) != 1 || !strings.Contains(missing[0].detail, "send call the drive never makes") {
		t.Fatalf("violations = %v, want the one declared tool-result step named missing", violations)
	}
	if got := integrityViolationsOf(violations, integrityUnexpected); len(got) != 0 {
		t.Fatalf("violations = %v, want no unexpected request beside the missing step", violations)
	}
	if got := integrityViolationsOf(violations, integrityDuplicate); len(got) != 0 {
		t.Fatalf("violations = %v, want no duplicate step beside the missing step", violations)
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != "answered early" {
		t.Fatalf("delivered bodies = %q, want the answer the reached step caused", bodies)
	}
}

// checkIntegrityDuplicateStepFails drives a step that answers with another send
// call, so the same request shape reaches the script a second time: the repeat
// must fail as a duplicate step, and the repeated action must be readable on
// the outbound face.
func checkIntegrityDuplicateStepFails(t *testing.T) {
	t.Helper()
	const note = "the same note"
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry asks for the note", requestTail("send the same note twice")).
		answers(settleToolCallAnswer("send", "call-dup-1", `{"content":"the same note"}`)).
		delivers(note)
	script.expect("model-entry", "the entry asks for the same note again", requestCarriesToolResult("call-dup-1")).
		answers(settleToolCallAnswer("send", "call-dup-2", `{"content":"the same note"}`)).
		delivers(note)
	provider.decide = script.decide
	sender := newRecordingSender()
	host := bootIntegrityHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-event-integrity-duplicate"}

	turn, err := driveIntegrityTurn(ctx, host, addr, "send the same note twice")
	if err == nil && turn.Terminal != model.KindFailed {
		t.Fatalf("a step reached twice did not fail the turn: turn = (%v, %d decisions), delivered = %q",
			turn.Terminal, len(turn.Decisions), sender.bodies())
	}
	if requests := provider.servedRequests(); len(requests) != 3 {
		t.Fatalf("model requests = %d, want the two declared steps beside the repeated one", len(requests))
	}
	violations := script.audit()
	duplicate := integrityViolationsOf(violations, integrityDuplicate)
	if len(duplicate) != 1 || !strings.Contains(duplicate[0].detail, "the entry asks for the same note again") {
		t.Fatalf("violations = %v, want the repeated step named duplicate", violations)
	}
	if got := integrityViolationsOf(violations, integrityUnexpected); len(got) != 0 {
		t.Fatalf("violations = %v, want no unexpected request beside the duplicate step", violations)
	}
	if got := integrityViolationsOf(violations, integrityMissing); len(got) != 0 {
		t.Fatalf("violations = %v, want no missing step beside the duplicate step", violations)
	}
	bodies := sender.bodies()
	if len(bodies) != 2 || bodies[0] != note || bodies[1] != note {
		t.Fatalf("delivered bodies = %q, want the repeated action readable twice", bodies)
	}
}

// runIntegrityConsumer drives the boot assembly the way the product is entered:
// one Host.Invoke turn at the entry address beside one turn at a declared
// second agent. The sender is gated, so the check reads the send tool parked on
// the outbound face before the model continues, and then maps every request the
// provider served back to its declared identity and every action back to the
// step that caused it.
func runIntegrityConsumer(t *testing.T) {
	t.Helper()
	const (
		entryInput  = "report through the entry tool"
		entryNote   = "the entry note"
		entryFinal  = "the entry settled"
		workerInput = "answer the audit"
		workerFinal = "the worker answered"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends its note", requestTail(entryInput)).
		answers(settleToolCallAnswer("send", "call-entry-note", `{"content":"the entry note"}`)).
		delivers(entryNote)
	script.expect("model-entry", "the entry ends on its final answer", requestCarriesToolResult("call-entry-note")).
		answers(scriptedAnswerOf(entryFinal)).
		delivers(entryFinal)
	script.expect("model-worker", "the worker answers at its own address", requestTail(workerInput)).
		answers(scriptedAnswerOf(workerFinal))
	provider.decide = script.decide

	sender, gate := newGatedRecordingSender()
	host := bootIntegrityHostWithAgents(t, provider, sender,
		"  user:\n"+
			"    model: model-entry\n"+
			"    tools:\n"+
			"      - { kind: tool, id: send }\n"+
			"  worker:\n"+
			"    model: model-worker\n")
	keepIntegrityHost(t, script, host)
	t.Cleanup(gate.releaseAll)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-event-integrity-consumer"}
	workerAddr := event.Address{AgentID: "worker", SessionID: "session-event-integrity-consumer-worker"}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, entryInput)
		driven <- integrityOutcome{turn: turn, err: err}
	}()

	gate.wait(t, "the entry note")
	if requests := provider.servedRequests(); len(requests) != 1 {
		t.Fatalf("model requests beside the parked tool = %d, want the one decision that issued it", len(requests))
	}
	actions := sender.actions()
	if len(actions) != 1 || actions[0].body != entryNote || actions[0].target != addr || actions[0].confirmed() {
		t.Fatalf("parked action = %+v, want the entry note addressed to %+v and still unconfirmed", actions, addr)
	}
	gate.releaseAll()

	got := awaitIntegrityDrive(t, driven, "the entry turn")
	if got.err != nil {
		t.Fatalf("the entry turn ended by error: %v", got.err)
	}
	if got.turn.Terminal != model.KindCompleted || len(got.turn.Decisions) != 2 {
		t.Fatalf("entry turn = (%v, %d decisions), want the completed turn of its two decisions",
			got.turn.Terminal, len(got.turn.Decisions))
	}
	workerTurn, err := driveIntegrityTurn(ctx, host, workerAddr, workerInput)
	if err != nil {
		t.Fatalf("the worker turn ended by error: %v", err)
	}
	if workerTurn.Terminal != model.KindCompleted || len(workerTurn.Decisions) != 1 {
		t.Fatalf("worker turn = (%v, %d decisions), want the completed turn of its own decision",
			workerTurn.Terminal, len(workerTurn.Decisions))
	}

	requests := provider.servedRequests()
	if len(requests) != 3 {
		t.Fatalf("model requests = %d, want the two entry decisions beside the worker decision", len(requests))
	}
	wantIdentities := []string{"model-entry", "model-entry", "model-worker"}
	for index, request := range requests {
		if request.Model != wantIdentities[index] {
			t.Fatalf("request %d named model %q, want the identity %q of its step", index+1, request.Model, wantIdentities[index])
		}
	}
	if !requestCarriesToolResult("call-entry-note")(requests[1]) {
		t.Fatalf("entry requests = %+v, want the tool result of the entry note in the decision that follows it", requests[:2])
	}
	if !requestTail(workerInput)(requests[2]) {
		t.Fatalf("worker request = %+v, want the input its own address carried", requests[2])
	}

	actions = sender.actions()
	if declared := script.actionBodies(); !reflect.DeepEqual(declared, sender.bodies()) {
		t.Fatalf("declared action bodies = %q, want the recorded %q", declared, sender.bodies())
	}
	if len(actions) != 2 {
		t.Fatalf("outbound actions = %+v, want the two actions the entry steps declare beside none of the worker", actions)
	}
	for index, want := range []string{entryNote, entryFinal} {
		if actions[index].body != want || actions[index].target != addr || !actions[index].confirmed() {
			t.Fatalf("action %d = %+v, want %q addressed to %+v and confirmed", index+1, actions[index], want, addr)
		}
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the consumer script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}
