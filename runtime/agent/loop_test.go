package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// invariantLoopAddress is the session address the invariants' drives run in.
var invariantLoopAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// invariantLoopOtherAddress is the neighbouring session address the invariants' drives never mix with.
var invariantLoopOtherAddress = event.Address{AgentID: "agent-a", SessionID: "session-b"}

// TestLoopDriveNotExported pins that no caller outside this package can start a turn beside the consumption loop.
func TestLoopDriveNotExported(t *testing.T) {
	if method, ok := reflect.TypeOf((*Loop)(nil)).MethodByName("Drive"); ok {
		t.Fatalf("Loop.%s is exported, want the drive folded into the unexported consumption face", method.Name)
	}
}

// TestLoopTurnInvariants pins the decision slot, the single reduction and the session-scoped request chain of one drive.
// - one session runs one effective model decision at a time, and the slot is free again once both drives ended
// - a model entry error reaches the same single reduction every stream does, and no second decision is minted
// - a production registered for a drive confirms its stop only after that drive really ended
// - one session's history and ready input never enter another session's request, and no drive writes into another session
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestLoopTurnInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("one session runs one effective model decision at a time", func(t *testing.T) {
		behaviour := &invariantLoopModel{gate: make(chan struct{}), answer: "answer-a"}
		loop, _, slots := invariantLoop(t, behaviour)
		results := make(chan invariantLoopResult, 2)
		drive := func(invocation event.InvocationID) {
			turn, err := loop.drive(ctx, Invocation{
				InvocationID: invocation,
				Address:      invariantLoopAddress,
				Instance:     "instance-a",
				Ready: []event.Event{{
					Key:     event.EventID(string(invocation) + "-event"),
					Message: model.Message{Role: model.RoleUser, Content: string(invocation)},
				}},
			})
			results <- invariantLoopResult{turn: turn, err: err}
		}
		go drive("invocation-1")
		invariantAwait(t, func() bool { return behaviour.inFlightCount() == 1 })
		held, decision, ok := slots.Slot(invariantLoopAddress, "instance-a")
		if !ok || held != event.InvocationID("invocation-1") || decision == "" {
			t.Fatalf("decision slot while one drive is in flight = (%q, %q, %v), want the first invocation's own decision", held, decision, ok)
		}
		go drive("invocation-2")
		time.Sleep(20 * time.Millisecond)
		if got := behaviour.peakCount(); got != 1 {
			t.Fatalf("model decisions in flight at once = %d, want the session's single slot respected", got)
		}
		close(behaviour.gate)
		for i := 0; i < 2; i++ {
			select {
			case out := <-results:
				if out.err != nil {
					t.Fatalf("drive %d ended with %v, want a completed turn", i, out.err)
				}
				if out.turn.Terminal != model.KindCompleted || out.turn.DecisionCount != 1 {
					t.Fatalf("turn %d = (%v, %d decisions), want one completed decision", i, out.turn.Terminal, out.turn.DecisionCount)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("a drive did not end after the queued decision was let through")
			}
		}
		if got := behaviour.peakCount(); got != 1 {
			t.Fatalf("model decisions in flight at once = %d, want never two beside one slot", got)
		}
		if _, _, ok := slots.Slot(invariantLoopAddress, "instance-a"); ok {
			t.Fatal("the decision slot is still held after both drives ended")
		}
	})

	t.Run("a model entry error reaches the same single reduction every stream does", func(t *testing.T) {
		behaviour := newInvariantLoopModel("", errors.New("agent: the model entry refused the request"))
		loop, _, _ := invariantLoop(t, behaviour)
		capture := &invariantLoopCapture{}
		loop.SetCapture(capture)
		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-1",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:       "event-1",
				EpisodeID: "episode-1",
				Message:   model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})
		if err != nil {
			t.Fatalf("drive beside a refused model entry = %v, want the refusal reduced", err)
		}
		if turn.Terminal != model.KindFailed {
			t.Fatalf("terminal beside a refused model entry = %v, want %v", turn.Terminal, model.KindFailed)
		}
		if turn.DecisionCount != 1 {
			t.Fatalf("decisions beside a refused model entry = %d, want the one attempt", turn.DecisionCount)
		}
		admitted := capture.admissions()
		views := capture.views()
		if len(admitted) != 1 || len(views) != 1 {
			t.Fatalf("admissions beside a refused model entry = (%d attempts, %d views), want one of each", len(admitted), len(capture.views()))
		}
		if got := admitted[0].outcome.Kind; got != model.KindFailed {
			t.Fatalf("admitted attempt outcome = %v, want the reduced terminal itself", got)
		}
		if admitted[0].episodeID != "episode-1" || admitted[0].decisionID != views[0].decisionID {
			t.Fatalf("admitted attempt = (%s, %s), want the episode the ready input named beside the decision it belongs to", admitted[0].episodeID, admitted[0].decisionID)
		}
	})

	t.Run("an intermediate decision's outcome stays replayable through the capture", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{answer: "delivered"},
		)
		loop, history, _ := closingRoundLoop(t, script)
		capture := &invariantLoopCapture{}
		loop.SetCapture(capture)
		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-1",
			Address:      closingRoundAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:       "event-1",
				EpisodeID: "episode-1",
				Message:   model.Message{Role: model.RoleUser, Content: "start"},
			}},
		})
		if err != nil {
			t.Fatalf("drive over a tool round and its answer = %v, want both decisions run", err)
		}
		if turn.DecisionCount != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the tool round beside the answering one", turn.Terminal, turn.DecisionCount)
		}
		admitted := capture.admissions()
		views := capture.views()
		if len(admitted) != 2 || len(views) != 2 {
			t.Fatalf("capture = (%d attempts, %d views), want one per decision of the drive", len(admitted), len(views))
		}
		for i, admission := range admitted {
			if admission.decisionID != views[i].decisionID {
				t.Fatalf("admitted attempt %d names decision %q, want %q", i, admission.decisionID, views[i].decisionID)
			}
			if admission.episodeID != "episode-1" {
				t.Fatalf("admitted attempt %d names episode %q, want the ready input's own", i, admission.episodeID)
			}
		}
		intermediate := admitted[0].outcome
		if intermediate.Body == nil || !intermediate.Body.IsToolCallResponse() {
			t.Fatalf("admitted intermediate outcome = %+v, want the tool call the reduction kept", intermediate)
		}
		last := admitted[1].outcome
		if last.Kind != model.KindCompleted || last.Body == nil || last.Body.IsToolCallResponse() {
			t.Fatalf("admitted last outcome = %+v, want the answer the reduction kept", last)
		}
		if committed := history.SessionHistory(closingRoundAddress); !invariantCarries(committed, "delivered") {
			t.Fatalf("committed transcript = %+v, want the last decision's answer kept as the drive's word", committed)
		}
	})

	t.Run("a drive in flight keeps its writer registered until that drive ended", func(t *testing.T) {
		behaviour := &invariantLoopModel{gate: make(chan struct{})}
		loop, _, _ := invariantLoop(t, behaviour)
		done := make(chan invariantLoopResult, 1)
		go func() {
			turn, err := loop.drive(ctx, Invocation{
				InvocationID: "invocation-1",
				Address:      invariantLoopAddress,
				Instance:     "instance-a",
				Ready: []event.Event{{
					Key:     "event-1",
					Message: model.Message{Role: model.RoleUser, Content: "hello"},
				}},
			})
			done <- invariantLoopResult{turn: turn, err: err}
		}()
		invariantAwait(t, func() bool { return behaviour.inFlightCount() == 1 })
		if stopped, open := loop.Stopped(); stopped || len(open) == 0 {
			t.Fatalf("writers beside a drive in flight = (%v, %v), want the drive registered as one that has not stopped", stopped, open)
		}
		close(behaviour.gate)
		select {
		case out := <-done:
			if out.err != nil {
				t.Fatalf("drive ended with %v, want a completed turn", out.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the drive did not end after the model stream was let through")
		}
		if stopped, open := loop.Stopped(); !stopped {
			t.Fatalf("writers left open after the drive ended = %v, want every writer stopped", open)
		}
	})

	t.Run("one session's history and ready input never enter another session's request", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-b", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		history := newInvariantLoopHistory()
		history.CommitHistory(invariantLoopAddress, []model.Message{{Role: model.RoleUser, Content: "alpha"}})
		loop.SetHistory(history)
		chains := &invariantLoopChains{}
		loop.SetRequestFace(chains.face)
		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-b",
			Address:      invariantLoopOtherAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-b",
				Message: model.Message{Role: model.RoleUser, Content: "beta"},
			}},
		})
		if err != nil || turn.Terminal != model.KindCompleted {
			t.Fatalf("drive of the neighbouring session = (%v, %v), want a completed turn", turn.Terminal, err)
		}
		if got := chains.recorded(); len(got) != 1 || got[0] != "session-b" {
			t.Fatalf("request chains of the drive = %v, want the one session it runs in", got)
		}
		sent := behaviour.requestMessages()
		if len(sent) != 1 {
			t.Fatalf("requests sent by the drive = %d, want one", len(sent))
		}
		if invariantCarries(sent[0], "alpha") {
			t.Fatalf("request of the neighbouring session carries another session's history: %+v", sent[0])
		}
		if !invariantCarries(sent[0], "beta") {
			t.Fatalf("request of the neighbouring session carries no ready input: %+v", sent[0])
		}
		if !invariantCarries(sent[0], "prefix-session-b") {
			t.Fatalf("request of the neighbouring session carries no prefix of its own chain: %+v", sent[0])
		}
		if got := history.SessionHistory(invariantLoopAddress); len(got) != 1 || invariantCarries(got, "answer-b") {
			t.Fatalf("history of the first session = %+v, want its own message alone", got)
		}
		if got := history.SessionHistory(invariantLoopOtherAddress); len(got) != 2 {
			t.Fatalf("history of the driven session = %+v, want the ready input beside the answer it committed", got)
		}
	})
}

// invariantLoopResult is one finished drive as the goroutine hands it back.
type invariantLoopResult struct {
	turn Turn
	err  error
}

// invariantLoopModel is the model of the invariants: it streams one answer and may hold every call at a gate.
type invariantLoopModel struct {
	mu       sync.Mutex
	gate     chan struct{}
	fail     error
	answer   string
	inFlight int
	peak     int
	requests [][]model.Message
}

// newInvariantLoopModel returns a model answering at once with the answer named, or refusing the entry.
func newInvariantLoopModel(answer string, fail error) *invariantLoopModel {
	gate := make(chan struct{})
	close(gate)
	return &invariantLoopModel{gate: gate, answer: answer, fail: fail}
}

// GenerateContent holds the call at the gate and then streams its one answer.
func (m *invariantLoopModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.inFlight++
	if m.inFlight > m.peak {
		m.peak = m.inFlight
	}
	m.requests = append(m.requests, append([]model.Message(nil), req.Messages...))
	m.mu.Unlock()
	select {
	case <-m.gate:
	case <-ctx.Done():
	}
	m.mu.Lock()
	m.inFlight--
	m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: m.answer}}},
		Done:    true,
	}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (m *invariantLoopModel) Info() model.Info { return model.Info{Name: "loop-model"} }

// inFlightCount reports how many calls are inside the model right now.
func (m *invariantLoopModel) inFlightCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inFlight
}

// peakCount reports the largest number of calls the model saw at once.
func (m *invariantLoopModel) peakCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peak
}

// requestMessages reports the message set of every request the model was handed.
func (m *invariantLoopModel) requestMessages() [][]model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]model.Message(nil), m.requests...)
}

// invariantLoopChains records the request chain each assembled request ran under.
type invariantLoopChains struct {
	mu     sync.Mutex
	chains []event.SessionID
}

// face assembles one prefix naming the chain it was asked for.
func (c *invariantLoopChains) face(_ context.Context, boundary RequestBoundary) (BoundaryView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chains = append(c.chains, boundary.Chain.SessionID)
	return BoundaryView{Messages: []model.Message{{
		Role:    model.RoleSystem,
		Content: "prefix-" + string(boundary.Chain.SessionID),
	}}}, nil
}

// recorded reports the chains the assembled requests ran under, in order.
func (c *invariantLoopChains) recorded() []event.SessionID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.SessionID(nil), c.chains...)
}

// invariantLoopHistory keeps one transcript and one fact chain per session address.
type invariantLoopHistory struct {
	mu        sync.Mutex
	bySession map[event.SessionID][]model.Message
	facts     map[event.SessionID][]event.Event
}

// newInvariantLoopHistory returns an empty session transcript face.
func newInvariantLoopHistory() *invariantLoopHistory {
	return &invariantLoopHistory{
		bySession: map[event.SessionID][]model.Message{},
		facts:     map[event.SessionID][]event.Event{},
	}
}

// SessionHistory reports the transcript accumulated for one session.
func (h *invariantLoopHistory) SessionHistory(addr event.Address) []model.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]model.Message(nil), h.bySession[addr.SessionID]...)
}

// CommitHistory appends one ended drive's transcript to its own session.
func (h *invariantLoopHistory) CommitHistory(addr event.Address, msgs []model.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bySession[addr.SessionID] = append(h.bySession[addr.SessionID], msgs...)
}

// SessionFacts reports the fact chain accumulated for one session, as a copy.
func (h *invariantLoopHistory) SessionFacts(addr event.Address) []event.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]event.Event(nil), h.facts[addr.SessionID]...)
}

// land commits one fact into the chain of its session, the way a producer outside the
// drive lands material a decision boundary has to read again.
func (h *invariantLoopHistory) land(evt event.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.facts[evt.Owner.SessionID] = append(h.facts[evt.Owner.SessionID], evt)
}

// invariantLoopView is the request view material one decision was admitted with.
type invariantLoopView struct {
	decisionID    event.DecisionID
	modelName     string
	promptVersion string
	visibleTools  []string
}

// invariantLoopAdmission is one attempt the loop admitted through its evidence face.
type invariantLoopAdmission struct {
	attemptID  event.AttemptID
	decisionID event.DecisionID
	episodeID  event.EpisodeID
	outcome    model.Outcome
}

// invariantLoopCapture is the loop's evidence face in a test: it keeps the view
// material each decision was fixed with and the outcome each admission carried,
// so one attempt of one decision is readable after the drive ended.
type invariantLoopCapture struct {
	mu       sync.Mutex
	fixed    []invariantLoopView
	recorded []invariantLoopAdmission
	sealed   int
}

// Snapshot keeps the view material one decision's request was fixed on.
func (c *invariantLoopCapture) Snapshot(_ context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, _ []string, _ model.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fixed = append(c.fixed, invariantLoopView{decisionID: decisionID, modelName: modelName, promptVersion: promptVersion, visibleTools: append([]string(nil), visibleTools...)})
	return nil
}

// Record admits one observed attempt beside the outcome the reduction produced.
func (c *invariantLoopCapture) Record(_ context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recorded = append(c.recorded, invariantLoopAdmission{attemptID: attemptID, decisionID: decisionID, episodeID: episodeID, outcome: outcome})
	return nil
}

// Seal counts one closed turn.
func (c *invariantLoopCapture) Seal(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sealed++
	return nil
}

// views reports the request views fixed so far.
func (c *invariantLoopCapture) views() []invariantLoopView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]invariantLoopView(nil), c.fixed...)
}

// admissions reports the attempts admitted so far.
func (c *invariantLoopCapture) admissions() []invariantLoopAdmission {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]invariantLoopAdmission(nil), c.recorded...)
}

// invariantLoop returns one loop over one held generation and one empty occupancy ledger.
func invariantLoop(t *testing.T, behaviour *invariantLoopModel) (*Loop, *state.Bindings, *state.Occupancy) {
	t.Helper()
	declaration := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a"}}}
	plan, err := state.NewStatePlan(
		state.OrgPlan{Declaration: declaration, Fingerprint: "fp-a"},
		map[event.AgentID]state.AgentBinding{
			"agent-a": {Model: behaviour, PromptSource: state.PromptSource{Version: "v1"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("build the binding plan of the loop: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the binding generation of the loop: %v", err)
	}
	slots := state.NewOccupancy()
	return NewLoop(bindings, slots, "instance-a", nil), bindings, slots
}

// invariantAwait waits for one expected state to arrive before a bounded deadline.
func invariantAwait(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the expected state did not arrive before the deadline")
}

// consumerLog records the consumer lines one drive rendered: the DEBUG and INFO
// levels the observation writes, and no other level.
type consumerLog struct {
	trpclog.Logger
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// take records one rendered line or refuses it by panicking.
func (l *consumerLog) take(level, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.panicsLeft > 0 {
		l.panicsLeft--
		panic("the logger refused the write")
	}
	l.lines = append(l.lines, level+": "+text)
}

// Debugf records one formatted debug line.
func (l *consumerLog) Debugf(format string, args ...any) {
	l.take("debug", fmt.Sprintf(format, args...))
}

// Infof records one formatted info line.
func (l *consumerLog) Infof(format string, args ...any) {
	l.take("info", fmt.Sprintf(format, args...))
}

// at reports the lines taken at the named level.
func (l *consumerLog) at(level string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.HasPrefix(line, level+": ") {
			out = append(out, line)
		}
	}
	return out
}

// mentions reports whether one line at the level carries every fragment.
func (l *consumerLog) mentions(level string, fragments ...string) bool {
	for _, line := range l.at(level) {
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchConsumerLog points the consumer observation at the given logger for the
// duration of one check.
func watchConsumerLog(tb testing.TB, logger trpclog.Logger) {
	tb.Helper()
	restore := SwapFrameworkLogger(logger)
	tb.Cleanup(restore)
}

// TestConsumerObservation checks the consumer face of the observation bypass.
// - one drive renders a start line carrying the address and the ready count, and an end line carrying the decision count and the terminal
// - every decision the drive consumes renders one line carrying its terminal and the number of calls the decision admitted
// - a declaration that names no provider window renders one gate-closed fact, so the untrimmed request is observable
// - the consumer face writes nothing at INFO, so the bounded stream stays with turn boundaries and model requests
// - a logger that fails does not end the drive
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestConsumerObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("one drive renders its start, its decision and its end", func(t *testing.T) {
		logger := &consumerLog{}
		watchConsumerLog(t, logger)
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-a", nil))

		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-1",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-1",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})

		if err != nil {
			t.Fatalf("drive = %v, want a completed turn", err)
		}
		if !logger.mentions("debug", "consumer drive start", "agent=agent-a", "session=session-a", "invocation=invocation-1", "ready=1") {
			t.Fatalf("debug lines = %v, want the start line of the drive's own invocation", logger.at("debug"))
		}
		if !logger.mentions("debug", "consumer decision", "invocation=invocation-1", "terminal=completed", "calls=0", "closing=false") {
			t.Fatalf("debug lines = %v, want the decision line of the one answer", logger.at("debug"))
		}
		if !logger.mentions("debug", "request budget gate closed", "invocation=invocation-1", "max_tokens=0", "provider_window=0") {
			t.Fatalf("debug lines = %v, want the gate-closed fact of a declaration naming no budget", logger.at("debug"))
		}
		if !logger.mentions("debug", "consumer drive end", "invocation=invocation-1", "decisions=1", "terminal=completed", "failed=false") {
			t.Fatalf("debug lines = %v, want the end line of the drive", logger.at("debug"))
		}
		if lines := logger.at("debug"); len(lines) != 4 {
			t.Fatalf("debug lines = %v, want one start, one gate fact, one decision and one end", lines)
		}
		if lines := logger.at("info"); len(lines) != 1 || !logger.mentions("info", "model response", "terminal=completed") {
			t.Fatalf("info lines = %v, want the one bounded model-response line and no consumer line above DEBUG", lines)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("terminal = %v, want the completed turn of the one answer", turn.Terminal)
		}
	})

	t.Run("a failed decision is named in the lines of its drive", func(t *testing.T) {
		logger := &consumerLog{}
		watchConsumerLog(t, logger)
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("", errors.New("agent: the model entry refused the request")))

		if _, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-2",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-2",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		}); err != nil {
			t.Fatalf("drive beside a refused model entry = %v, want the refusal reduced", err)
		}

		if !logger.mentions("debug", "consumer decision", "invocation=invocation-2", "terminal=failed") {
			t.Fatalf("debug lines = %v, want the failed terminal named in the decision line", logger.at("debug"))
		}
		if !logger.mentions("debug", "consumer drive end", "invocation=invocation-2", "terminal=failed", "failed=false") {
			t.Fatalf("debug lines = %v, want the reduced terminal beside a drive that itself ended", logger.at("debug"))
		}
	})

	t.Run("a logger that fails does not end the drive", func(t *testing.T) {
		watchConsumerLog(t, &consumerLog{panicsLeft: 3})
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-c", nil))

		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-3",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-3",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})

		if err != nil || turn.Terminal != model.KindCompleted || turn.DecisionCount != 1 {
			t.Fatalf("drive through a failing logger = (%v, %d decisions, %v), want the one completed decision", turn.Terminal, turn.DecisionCount, err)
		}
	})
}

// invariantCarries reports whether one message set carries the content named.
func invariantCarries(msgs []model.Message, content string) bool {
	for _, msg := range msgs {
		if msg.Content == content {
			return true
		}
	}
	return false
}

// invariantNoticeFeed is the notice read face of one session chain: the notice
// facts the chain committed in chain order, the ended-material boundary the reads
// start from, and the cursors the reads reached.
type invariantNoticeFeed struct {
	mu       sync.Mutex
	boundary int
	entries  []NoticeView
	cursors  []int
}

// newInvariantNoticeFeed returns a chain carrying no notice and ending at the
// given boundary.
func newInvariantNoticeFeed(boundary int) *invariantNoticeFeed {
	return &invariantNoticeFeed{boundary: boundary}
}

// NoticeBoundary reports the fixed ended-material boundary of this chain.
func (f *invariantNoticeFeed) NoticeBoundary(RequestChain) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boundary, true
}

// NoticesFrom reports the notice facts appended after the cursor under the given
// identity prefix, beside the position the read reached.
func (f *invariantNoticeFeed) NoticesFrom(_ RequestChain, prefix event.EventID, cursor int) ([]NoticeView, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(f.entries) {
		cursor = len(f.entries)
	}
	f.cursors = append(f.cursors, cursor)
	var out []NoticeView
	for _, entry := range f.entries[cursor:] {
		if strings.HasPrefix(string(entry.Key), string(prefix)) {
			out = append(out, entry)
		}
	}
	return out, len(f.entries), true
}

// append records one notice fact this chain committed.
func (f *invariantNoticeFeed) append(view NoticeView) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, view)
}

// readCursors reports the cursors the reads of this chain started at.
func (f *invariantNoticeFeed) readCursors() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.cursors...)
}

// invariantCommitFace records the facts a loop commits and appends the material
// of a committed notice to the chain beside it, so the chain a range reads is the
// chain it committed.
type invariantCommitFace struct {
	mu     sync.Mutex
	events []event.Event
	feed   *invariantNoticeFeed
}

// commit records one committed fact and reports the identity it committed under.
func (c *invariantCommitFace) commit(evt event.Event) (event.EventID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if evt.Key == "" {
		evt.Key = event.EventID(fmt.Sprintf("minted-%d", len(c.events)+1))
	}
	c.events = append(c.events, evt)
	if c.feed != nil && evt.Type == event.TypeFeedback {
		c.feed.append(NoticeView{Key: evt.Key, Text: evt.Message.Content})
	}
	return evt.Key, nil
}

// list reports the facts committed so far.
func (c *invariantCommitFace) list() []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.Event(nil), c.events...)
}

// TestBackflowNotificationLanes pins the lanes one notification of a call is admitted into.
// - a progress notification commits as a notice fact of its range, answers no call, and no decision reads it
// - the range reads no progress projection at its round head, and nothing lands in the intake beside the fact
// - a range beside no chain commits nothing for a report it cannot address, and settles nothing
// - a settled notification still pairs its result with the call
// - a notification that is neither progress nor terminal leaves the range exactly as it stands
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestBackflowNotificationLanes(t *testing.T) {
	ctx := context.Background()

	t.Run("progress_commits_fact_without_model_material", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-progress", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		feed := newInvariantNoticeFeed(0)
		commits := &invariantCommitFace{feed: feed}
		loop.SetNoticeFeed(feed)
		loop.SetFactCommit(commits.commit)
		loop.intakeFor("invocation-progress").setAddress(invariantLoopAddress)
		notice := task.Notice{TaskID: "task-progress", Round: 2, Kind: task.NoticeProgress, Seq: 0, Event: "fact-progress"}
		loop.Backflow("invocation-progress", notice)

		facts := commits.list()
		if len(facts) != 1 {
			t.Fatalf("committed facts = %d, want the one notice fact of the progress", len(facts))
		}
		fact := facts[0]
		if fact.Type != event.TypeFeedback {
			t.Fatalf("notice fact type = %q, want the feedback type of input material", fact.Type)
		}
		if fact.Key != NoticeFactKey("invocation-progress", notice) {
			t.Fatalf("notice fact key = %q, want the stable identity of the notification", fact.Key)
		}
		if fact.InvocationID != "invocation-progress" || fact.TaskID != notice.TaskID || fact.InstanceID != "instance-a" {
			t.Fatalf("notice fact = %+v, want it scoped to the range and the instance that admitted it", fact)
		}
		if fact.Message.Content != progressNoteOf(notice).Content || fact.Message.Role != model.RoleUser {
			t.Fatalf("notice fact message = %+v, want the report recorded as the input material it is", fact.Message)
		}
		if fact.Meta[event.MetaKeyEventSummary] != string(notice.Kind) ||
			fact.Meta[event.MetaKeyNoticeKind] != string(notice.Kind) ||
			fact.Meta[event.MetaKeyTaskID] != string(notice.TaskID) {
			t.Fatalf("notice fact meta = %v, want the kind and the task as mechanical keys", fact.Meta)
		}
		if fact.Owner != invariantLoopAddress || fact.Target != invariantLoopAddress || fact.Source != invariantLoopAddress {
			t.Fatalf("notice fact addresses = (%+v, %+v, %+v), want the range address", fact.Owner, fact.Target, fact.Source)
		}

		in := loop.intakeFor("invocation-progress")
		in.mu.Lock()
		parked := len(in.settled[notice.TaskID])
		waiters := len(in.waiters[notice.TaskID])
		in.mu.Unlock()
		if parked != 0 || waiters != 0 {
			t.Fatalf("intake beside a progress notification = (%d parked, %d waits), want the settled wait left open", parked, waiters)
		}

		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-progress",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-progress",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})
		if err != nil || turn.Terminal != model.KindCompleted {
			t.Fatalf("drive beside a progress notification = (%v, %v), want the one completed turn", turn.Terminal, err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
		note := progressNoteOf(notice)
		if invariantCarries(requests[0], note.Content) {
			t.Fatalf("first request messages = %+v, want no progress report in the material a decision reads", requests[0])
		}
		if note.Role != model.RoleUser || note.ToolID != "" || note.ToolName != "" {
			t.Fatalf("progress note = %+v, want the recorded report that answers no call", note)
		}
		if cursors := feed.readCursors(); len(cursors) == 0 {
			t.Fatalf("notice reads = %v, want the terminal read face to run beside the progress report", cursors)
		}
	})

	t.Run("unaddressed_progress_commits_nothing", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-cache", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		commits := &invariantCommitFace{feed: newInvariantNoticeFeed(0)}
		loop.SetFactCommit(commits.commit)
		notice := task.Notice{TaskID: "task-cache", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-cache"}
		loop.Backflow("invocation-cache", notice)

		if facts := commits.list(); len(facts) != 0 {
			t.Fatalf("committed facts = %d, want no fact for a report beside no addressed range", len(facts))
		}
		if notes := loop.terminalNotes(Invocation{InvocationID: "invocation-cache", Address: invariantLoopAddress}); len(notes) != 0 {
			t.Fatalf("terminal notes = %+v, want nothing to read beside no fact", notes)
		}

		if _, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-cache",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-cache",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		}); err != nil {
			t.Fatalf("drive beside an unaddressed progress notification = %v, want the one completed turn", err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 || len(requests[0]) != 1 || requests[0][0].Content != "hello" {
			t.Fatalf("first request messages = %+v, want the ready input alone", requests)
		}
	})

	t.Run("a settled notification still pairs its result with the call", func(t *testing.T) {
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-settled", nil))
		notice := task.Notice{TaskID: "task-settled", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "fact-settled"}
		loop.Backflow("invocation-settled", notice)

		in := loop.intakeFor("invocation-settled")
		in.mu.Lock()
		parked := len(in.settled["task-settled"])
		in.mu.Unlock()
		if parked != 1 {
			t.Fatalf("intake beside a settled notification = %d parked, want the paired result", parked)
		}
		if item, ok := in.await(ctx, "task-settled"); !ok || item.fact != notice.Event {
			t.Fatalf("await = (%+v, %v), want the settled result the call waits for", item, ok)
		}
	})

	t.Run("a notification that is not progress leaves the range as it stands", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-accepted", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		loop.Backflow("invocation-accepted", task.Notice{TaskID: "task-accepted", Round: 1, Kind: task.NoticeAccepted, Seq: 0, Event: "fact-accepted"})

		in := loop.intakeFor("invocation-accepted")
		in.mu.Lock()
		parked := len(in.settled["task-accepted"])
		waits := len(in.waiters["task-accepted"])
		in.mu.Unlock()
		if parked != 0 || waits != 0 {
			t.Fatalf("intake beside an accepted notification = (%d parked, %d waits), want the intake untouched", parked, waits)
		}
		if notes := loop.terminalNotes(Invocation{InvocationID: "invocation-accepted", Address: invariantLoopAddress}); len(notes) != 0 {
			t.Fatalf("notification cache beside an accepted notification = %+v, want it empty", notes)
		}

		if _, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-accepted",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-accepted",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		}); err != nil {
			t.Fatalf("drive beside an accepted notification = %v, want the one completed turn", err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 || len(requests[0]) != 1 {
			t.Fatalf("first request messages = %+v, want the ready input alone", requests)
		}
		if requests[0][0].Content != "hello" {
			t.Fatalf("first request message = %+v, want the accepted notification carried nowhere", requests[0][0])
		}
	})
}

// TestQuietProgressNotice pins that a progress report commits as a fact of its range and enters no decision.
// - progress_fact_committed: the report commits under its stable notice identity with its kind and task, and a repeated delivery keeps that one record
// - progress_absent_from_next_request: the drive commits the fact, parks nothing, and its request carries the ready input alone
// - zero_round_progress_is_material: a zero-round notice under the progress category is execution-resource material and reaches the next request
// - terminal_notification_reaches_the_next_request: the terminal notification of the same range stays at the read face and enters the next request
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestQuietProgressNotice(t *testing.T) {
	ctx := context.Background()

	t.Run("progress_fact_committed", func(t *testing.T) {
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-quiet-fact", nil))
		ledger := newQuietNoticeLedger(nil)
		loop.SetFactCommit(ledger.commit)
		loop.intakeFor("invocation-quiet-fact").setAddress(invariantLoopAddress)

		first := task.Notice{TaskID: "task-quiet", Round: 2, Kind: task.NoticeProgress, Seq: 0, Event: "fact-quiet-0"}
		second := task.Notice{TaskID: "task-quiet", Round: 2, Kind: task.NoticeProgress, Seq: 1, Event: "fact-quiet-1"}
		loop.Backflow("invocation-quiet-fact", first)
		loop.Backflow("invocation-quiet-fact", first)
		loop.Backflow("invocation-quiet-fact", second)

		if got := ledger.deliveries(); got != 3 {
			t.Fatalf("notice deliveries = %d, want the retried report beside the two reports", got)
		}
		facts := ledger.list()
		if len(facts) != 2 {
			t.Fatalf("committed notice facts = %d, want one fact per report beside the retried delivery", len(facts))
		}
		for i, notice := range []task.Notice{first, second} {
			fact := facts[i]
			if fact.Key != NoticeFactKey("invocation-quiet-fact", notice) {
				t.Fatalf("notice fact %d key = %q, want the stable identity of the report in seq %d", i, fact.Key, notice.Seq)
			}
			if fact.Type != event.TypeFeedback {
				t.Fatalf("notice fact %d type = %q, want the feedback type of input material", i, fact.Type)
			}
			if fact.Meta[event.MetaKeyNoticeKind] != string(task.NoticeProgress) || fact.Meta[event.MetaKeyTaskID] != string(notice.TaskID) {
				t.Fatalf("notice fact %d meta = %v, want the progress kind and the reported task", i, fact.Meta)
			}
			if fact.Message.Role != model.RoleUser || fact.Message.Content != progressNoteOf(notice).Content || fact.Message.ToolID != "" {
				t.Fatalf("notice fact %d message = %+v, want the report recorded as input material that answers no call", i, fact.Message)
			}
			if fact.InvocationID != "invocation-quiet-fact" || fact.TaskID != notice.TaskID || fact.InstanceID != "instance-a" {
				t.Fatalf("notice fact %d = %+v, want it scoped to the range that admitted it", i, fact)
			}
			if fact.Owner != invariantLoopAddress || fact.Target != invariantLoopAddress || fact.Source != invariantLoopAddress {
				t.Fatalf("notice fact %d addresses = (%+v, %+v, %+v), want the range address", i, fact.Owner, fact.Target, fact.Source)
			}
		}
	})

	t.Run("progress_absent_from_next_request", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-quiet-absent", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		feed := newInvariantNoticeFeed(0)
		ledger := newQuietNoticeLedger(feed)
		loop.SetNoticeFeed(feed)
		loop.SetFactCommit(ledger.commit)
		in := loop.intakeFor("invocation-quiet-absent")
		in.setAddress(invariantLoopAddress)
		notice := task.Notice{TaskID: "task-quiet-absent", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-quiet-absent"}
		loop.Backflow("invocation-quiet-absent", notice)

		if facts := ledger.list(); len(facts) != 1 {
			t.Fatalf("committed notice facts = %d, want the report committed as its fact beside the silence", len(facts))
		}
		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-quiet-absent",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-quiet-absent",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})
		if err != nil || turn.Terminal != model.KindCompleted {
			t.Fatalf("drive beside a progress report = (%v, %v), want the one completed turn", turn.Terminal, err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
		if note := progressNoteOf(notice); invariantCarries(requests[0], note.Content) {
			t.Fatalf("first request messages = %+v, want no progress report in the material a decision reads", requests[0])
		}
		if len(requests[0]) != 1 || requests[0][0].Content != "hello" {
			t.Fatalf("first request messages = %+v, want the ready input alone beside the fact the report committed", requests[0])
		}
		if cursors := feed.readCursors(); len(cursors) == 0 {
			t.Fatalf("notice reads = %v, want the terminal read face to run beside the progress report", cursors)
		}
		in.mu.Lock()
		parked, waits := len(in.settled[notice.TaskID]), len(in.waiters[notice.TaskID])
		in.mu.Unlock()
		if parked != 0 || waits != 0 {
			t.Fatalf("intake beside a progress report = (%d parked, %d waits), want nothing parked beside the fact", parked, waits)
		}
	})

	t.Run("zero_round_progress_is_material", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-quiet-observation", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		feed := newInvariantNoticeFeed(0)
		ledger := newQuietNoticeLedger(feed)
		loop.SetNoticeFeed(feed)
		loop.SetFactCommit(ledger.commit)
		in := loop.intakeFor("invocation-quiet-observation")
		in.setAddress(invariantLoopAddress)
		observation := task.Notice{TaskID: "task-quiet-observation", Round: 0, Kind: task.NoticeProgress, Seq: 0, Event: "fact-quiet-observation"}
		loop.Backflow("invocation-quiet-observation", observation)

		if facts := ledger.list(); len(facts) != 1 {
			t.Fatalf("committed notice facts = %d, want the observation committed as its fact", len(facts))
		}
		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-quiet-observation",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-quiet-observation",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})
		if err != nil || turn.Terminal != model.KindCompleted {
			t.Fatalf("drive beside a zero-round progress report = (%v, %v), want the one completed turn", turn.Terminal, err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
		if material := progressNoteOf(observation).Content; !invariantCarries(requests[0], material) {
			t.Fatalf("first request messages = %+v, want the material a zero-round progress fact carries, which is not a pipeline round report", requests[0])
		}
	})

	t.Run("terminal_notification_reaches_the_next_request", func(t *testing.T) {
		behaviour := newInvariantLoopModel("answer-quiet-terminal", nil)
		loop, _, _ := invariantLoop(t, behaviour)
		feed := newInvariantNoticeFeed(0)
		ledger := newQuietNoticeLedger(feed)
		loop.SetNoticeFeed(feed)
		loop.SetFactCommit(ledger.commit)
		in := loop.intakeFor("invocation-quiet-terminal")
		in.setAddress(invariantLoopAddress)
		progress := task.Notice{TaskID: "task-quiet-terminal", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-quiet-terminal"}
		loop.Backflow("invocation-quiet-terminal", progress)
		settled := task.Notice{TaskID: "task-quiet-terminal", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "fact-quiet-terminal-settled"}
		material := "tagent: task task-quiet-terminal settled\nresult:\nworker result"
		ledger.commit(event.Event{
			Key:     NoticeFactKey("invocation-quiet-terminal", settled),
			Type:    event.TypeFeedback,
			Message: model.Message{Role: model.RoleUser, Content: material},
		})

		turn, err := loop.drive(ctx, Invocation{
			InvocationID: "invocation-quiet-terminal",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-quiet-terminal",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})
		if err != nil || turn.Terminal != model.KindCompleted {
			t.Fatalf("drive beside a terminal notification = (%v, %v), want the one completed turn", turn.Terminal, err)
		}
		requests := behaviour.requestMessages()
		if len(requests) != 1 {
			t.Fatalf("model requests = %d, want the one decision of the range", len(requests))
		}
		if invariantCarries(requests[0], progressNoteOf(progress).Content) {
			t.Fatalf("first request messages = %+v, want the progress report skipped by its category", requests[0])
		}
		if !invariantCarries(requests[0], material) {
			t.Fatalf("first request messages = %+v, want the terminal notification of the range as input material", requests[0])
		}
	})
}

// quietNoticeLedger is the commit face the quiet notice cases commit through: one
// record per identity — an event already published is not committed twice — beside
// the deliveries it was asked to commit and the chain it appends the material of
// a committed notice to.
type quietNoticeLedger struct {
	mu       sync.Mutex
	attempts []event.EventID
	order    []event.EventID
	events   map[event.EventID]event.Event
	feed     *invariantNoticeFeed
}

// newQuietNoticeLedger returns an empty ledger beside the chain it appends the
// material of a committed notice to.
func newQuietNoticeLedger(feed *invariantNoticeFeed) *quietNoticeLedger {
	return &quietNoticeLedger{events: map[event.EventID]event.Event{}, feed: feed}
}

// commit records one fact under its identity and reports it back: a delivery of an
// identity the ledger already holds keeps the one record and reports that same
// identity.
func (l *quietNoticeLedger) commit(evt event.Event) (event.EventID, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if evt.Key == "" {
		evt.Key = event.EventID(fmt.Sprintf("minted-quiet-%d", len(l.attempts)+1))
	}
	l.attempts = append(l.attempts, evt.Key)
	if _, held := l.events[evt.Key]; held {
		return evt.Key, nil
	}
	l.events[evt.Key] = evt
	l.order = append(l.order, evt.Key)
	if l.feed != nil && evt.Type == event.TypeFeedback {
		l.feed.append(NoticeView{Key: evt.Key, Text: evt.Message.Content})
	}
	return evt.Key, nil
}

// list reports the facts the ledger holds in commit order.
func (l *quietNoticeLedger) list() []event.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]event.Event, 0, len(l.order))
	for _, key := range l.order {
		out = append(out, l.events[key])
	}
	return out
}

// deliveries reports how many deliveries the ledger was asked to commit.
func (l *quietNoticeLedger) deliveries() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.attempts)
}

// backflowNestedSite is the delegated execution site a nested call runs in: the
// address whose resident instance issues a call of its own.
var backflowNestedSite = event.Address{AgentID: "agent-nested", SessionID: "session-nested"}

// backflowNestedNeighbour is the declared address beside the delegated site: the
// wake of one range stays on its own address and never drives the neighbour.
var backflowNestedNeighbour = event.Address{AgentID: "agent-nested", SessionID: "session-neighbour"}

// backflowNestedAnswer is the body the delegated turn closes on.
const backflowNestedAnswer = "the body the nested call produced"

// backflowNestedTask is the task the nested call is admitted as.
const backflowNestedTask = event.TaskID("task-nested")

// backflowNestedBudget bounds the delegated turn itself: the wait the calling entry
// spends on the segment that consumed its input.
const backflowNestedBudget = 2 * time.Second

// backflowNestedWindow bounds the checks' own waits, wider than the delegated turn's
// budget so a wake that never came is read as the terminal the wait fell back on
// instead of as the check hanging.
const backflowNestedWindow = 5 * backflowNestedBudget

// TestBackflowWakesSelfRegisteredResidents pins the wake a settled call leaves beside no host.
// - backflow_wakes_self_registered_residents: a settled notification wakes the loop the loop mounted for itself
// - the delegated turn answers the settled terminal beside the body its own chain ended on, never a cancelled one
// - the set that mounted the instance keeps one instance for the address, and the result lands on that chain alone
// - a loop beside no set at all mounts none: its backflow stays parked in the intake
func TestBackflowWakesSelfRegisteredResidents(t *testing.T) {
	t.Run("backflow_wakes_self_registered_residents", func(t *testing.T) {
		tool := &backflowNestedCallTool{settle: make(chan struct{}), backflowed: make(chan struct{})}
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-nested", backflowNestedCallToolName)}},
			closingRoundStep{answer: backflowNestedAnswer},
		)
		loop, history := newBackflowNestedLoop(t, script, tool)
		binding := state.AgentBinding{Model: script, Tools: []trpctool.Tool{tool}, PromptSource: state.PromptSource{Version: "v1"}}

		ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
		defer cancel()
		type submission struct {
			answer DelegatedAnswer
			err    error
		}
		submitted := make(chan submission, 1)
		go func() {
			answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
				Site:        backflowNestedSite,
				Binding:     binding,
				Instruction: "run the delegated turn",
				Request:     "write the nested answer",
			})
			submitted <- submission{answer: answer, err: err}
		}()

		resident := loop.residentOf().Ensure(backflowNestedSite)
		if resident == nil {
			t.Fatalf("the set the loop mounted for itself holds no instance of %s", backflowNestedSite.SessionID)
		}
		invariantAwait(t, func() bool { return resident.TaskState(backflowNestedTask) == TaskAwaited })
		close(tool.settle)
		select {
		case <-tool.backflowed:
		case <-time.After(backflowNestedWindow):
			t.Fatal("the settled notification never reached the loop")
		}

		select {
		case got := <-submitted:
			if got.err != nil {
				t.Fatalf("the delegated turn ended beside %v, want the settled answer of its own chain", got.err)
			}
			if got.answer.Status != task.StatusSettled {
				t.Fatalf("the delegated turn ended as status %d, want the settled status %d (the cancelled status is %d): a result that woke no resident loop leaves the segment to a cancellation",
					got.answer.Status, task.StatusSettled, task.StatusCancelled)
			}
			if got.answer.Body != backflowNestedAnswer {
				t.Fatalf("the body of the delegated turn = %q, want %q", got.answer.Body, backflowNestedAnswer)
			}
		case <-time.After(backflowNestedWindow):
			t.Fatal("the delegated turn never answered: the settled result woke no resident loop")
		}

		if again := loop.residentOf().Ensure(backflowNestedSite); again != resident {
			t.Fatalf("the instance of %s = %p, want the one instance %p the address runs on", backflowNestedSite.SessionID, again, resident)
		}
		if outputs := backflowNestedOutputs(history, backflowNestedSite); len(outputs) != 1 || outputs[0].Message.Content != backflowNestedAnswer {
			t.Fatalf("agent_output facts of %s = %+v, want the one answer the delegated turn closed on", backflowNestedSite.SessionID, outputs)
		}
		if outputs := backflowNestedOutputs(history, backflowNestedNeighbour); len(outputs) != 0 {
			t.Fatalf("agent_output facts of %s = %+v, want the result on the range that issued the call alone", backflowNestedNeighbour.SessionID, outputs)
		}
	})

	t.Run("a_loop_beside_no_consumers_mounts_nothing", func(t *testing.T) {
		tool := &backflowNestedCallTool{settle: make(chan struct{}), backflowed: make(chan struct{})}
		script := newClosingRoundScript(closingRoundStep{answer: backflowNestedAnswer})
		loop, _ := newBackflowNestedLoop(t, script, tool)

		loop.Backflow("invocation-idle", task.Notice{TaskID: "task-idle", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "fact-idle"})

		loop.mu.Lock()
		consumers := loop.consumers
		loop.mu.Unlock()
		if consumers != nil {
			t.Fatal("a settled backflow beside no current set mounted one, want none")
		}
		in := loop.intakeFor("invocation-idle")
		if item, ok := in.await(context.Background(), "task-idle"); !ok || item.fact != "fact-idle" {
			t.Fatalf("parked result = (%+v, %v), want the settled result kept in the intake", item, ok)
		}
	})
}

// TestStoppedResidentsRejectDelegatedWork proves that stopping residents is terminal for new delegated work.
// - no replacement set or Consumer may appear beside the stopped one
func TestStoppedResidentsRejectDelegatedWork(t *testing.T) {
	for _, test := range []struct {
		name       string
		transition func(*testing.T, *Loop)
	}{
		{name: "immediately_after_stop", transition: func(*testing.T, *Loop) {}},
		{name: "after_clearing_the_set", transition: func(_ *testing.T, loop *Loop) {
			loop.SetConsumers(nil)
		}},
		{name: "after_replacing_the_set", transition: func(t *testing.T, loop *Loop) {
			replacement := NewConsumers(loop, context.Background(), nil)
			t.Cleanup(replacement.Stop)
			loop.SetConsumers(replacement)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := &backflowNestedCallTool{settle: make(chan struct{}), backflowed: make(chan struct{})}
			script := newClosingRoundScript(closingRoundStep{answer: backflowNestedAnswer})
			loop, _ := newBackflowNestedLoop(t, script, tool)
			loop.StopResidents()
			test.transition(t, loop)

			ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
			defer cancel()
			_, err := loop.SubmitDelegated(ctx, DelegatedInput{
				Site:        backflowNestedSite,
				Binding:     state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}},
				Instruction: "run only if the loop is still open",
				Request:     "this input must not revive a stopped Consumer",
			})
			if !errors.Is(err, ErrDelegatedInstanceUnavailable) {
				t.Fatalf("delegated work after StopResidents returned %v, want ErrDelegatedInstanceUnavailable", err)
			}
		})
	}
}

// gatedHistoryFace holds the first chain read so a set-attachment attempt can be
// ordered against a public WakeAddress without inspecting Loop's private maps.
type gatedHistoryFace struct {
	HistoryFace
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *gatedHistoryFace) SessionFacts(addr event.Address) []event.Event {
	h.once.Do(func() {
		close(h.started)
		<-h.release
	})
	return h.HistoryFace.SessionFacts(addr)
}

// TestConsumerSetDoesNotReplaceMountedAddress proves that a mounted address cannot be silently taken over by another set.
// - stopping the current set makes public delegated work unavailable
func TestConsumerSetDoesNotReplaceMountedAddress(t *testing.T) {
	tool := &backflowNestedCallTool{settle: make(chan struct{}), backflowed: make(chan struct{})}
	script := newClosingRoundScript(closingRoundStep{answer: backflowNestedAnswer})
	loop, history := newBackflowNestedLoop(t, script, tool)
	gated := &gatedHistoryFace{HistoryFace: history, started: make(chan struct{}), release: make(chan struct{})}
	loop.SetHistory(gated)
	first := NewConsumers(loop, context.Background(), nil)
	second := NewConsumers(loop, context.Background(), nil)
	t.Cleanup(first.Stop)
	t.Cleanup(second.Stop)
	loop.SetConsumers(first)

	woke := make(chan struct{})
	go func() {
		loop.WakeAddress(backflowNestedSite)
		close(woke)
	}()
	select {
	case <-gated.started:
	case <-time.After(backflowNestedWindow):
		t.Fatal("WakeAddress did not read the address chain")
	}
	attached := make(chan struct{})
	go func() {
		loop.SetConsumers(second)
		close(attached)
	}()
	close(gated.release)
	for _, done := range []chan struct{}{woke, attached} {
		select {
		case <-done:
		case <-time.After(backflowNestedWindow):
			t.Fatal("the address mount/attach interleaving did not finish")
		}
	}

	first.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	_, err := loop.SubmitDelegated(ctx, DelegatedInput{
		Site:        backflowNestedSite,
		Binding:     state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}},
		Instruction: "check which set owns the mounted address",
		Request:     "a stopped current set must not be replaced by another runtime",
	})
	if !errors.Is(err, ErrDelegatedInstanceUnavailable) {
		t.Fatalf("stopping the mounted address's set returned %v, want ErrDelegatedInstanceUnavailable", err)
	}
}

// wakeFromConsumerToolName is the tool that re-enters WakeAddress from a Consumer.
const wakeFromConsumerToolName = "wake_from_consumer"

// wakeFromConsumerTool waits for StopResidents to cancel its decision, then re-enters
// the public wake path. No private lock or registry is inspected by the test.
type wakeFromConsumerTool struct {
	loop      *Loop
	site      event.Address
	entered   chan struct{}
	cancelled chan struct{}
	hold      chan struct{}
}

func (t *wakeFromConsumerTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: wakeFromConsumerToolName, Description: "wake the current address during cancellation"}
}

func (t *wakeFromConsumerTool) Call(ctx context.Context, _ []byte) (any, error) {
	close(t.entered)
	<-ctx.Done()
	if t.cancelled != nil {
		close(t.cancelled)
	}
	if t.hold != nil {
		<-t.hold
	}
	t.loop.WakeAddress(t.site)
	return "woke", nil
}

// TestStopResidentsAllowsWakeFromConsumerTool proves that stop holds no state lock while joining a re-entering Consumer.
func TestStopResidentsAllowsWakeFromConsumerTool(t *testing.T) {
	tool := &wakeFromConsumerTool{site: backflowNestedSite, entered: make(chan struct{})}
	script := newClosingRoundScript(closingRoundStep{calls: []model.ToolCall{
		closingRoundCall("call-wake-from-consumer", wakeFromConsumerToolName),
	}})
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-wake", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	var mu sync.Mutex
	seq := 0
	loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		seq++
		key := event.EventID(fmt.Sprintf("%s#wake-fact-%d", evt.Owner.SessionID, seq))
		mu.Unlock()
		evt.Key = key
		history.land(evt)
		return key, nil
	})
	tool.loop = loop
	binding := state.AgentBinding{Model: script, Tools: []trpctool.Tool{tool}, PromptSource: state.PromptSource{Version: "v1"}}

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedWindow)
	defer cancel()
	submitted := make(chan error, 1)
	go func() {
		_, err := loop.SubmitDelegated(ctx, DelegatedInput{
			Site:        backflowNestedSite,
			Binding:     binding,
			Instruction: "exercise the consumer re-entry boundary",
			Request:     "stop while this tool is inside the consumer",
		})
		submitted <- err
	}()
	select {
	case <-tool.entered:
	case <-time.After(backflowNestedWindow):
		t.Fatal("the delegated Consumer never entered the wake tool")
	}
	stopped := make(chan struct{})
	go func() {
		loop.StopResidents()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(backflowNestedBudget):
		t.Fatal("StopResidents is blocked beside a Consumer tool re-entering WakeAddress")
	}
	select {
	case err := <-submitted:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("the stopped delegated call returned %v, want a cancellation or named unavailability", err)
		}
	case <-time.After(backflowNestedBudget):
		t.Fatal("the delegated call did not return after StopResidents")
	}
}

// TestStoppedSetRejectsSubmitBeforeExistingInstanceJoin proves that a stopped set rejects submit before its old Consumer joins.
func TestStoppedSetRejectsSubmitBeforeExistingInstanceJoin(t *testing.T) {
	tool := &wakeFromConsumerTool{
		site:      backflowNestedSite,
		entered:   make(chan struct{}),
		cancelled: make(chan struct{}),
		hold:      make(chan struct{}),
	}
	script := newClosingRoundScript(closingRoundStep{calls: []model.ToolCall{
		closingRoundCall("call-stopped-existing", wakeFromConsumerToolName),
	}})
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-stopped-existing", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	var mu sync.Mutex
	seq := 0
	loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		seq++
		key := event.EventID(fmt.Sprintf("%s#stopped-existing-%d", evt.Owner.SessionID, seq))
		mu.Unlock()
		evt.Key = key
		history.land(evt)
		return key, nil
	})
	tool.loop = loop
	binding := state.AgentBinding{Model: script, Tools: []trpctool.Tool{tool}, PromptSource: state.PromptSource{Version: "v1"}}
	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedWindow)
	defer cancel()
	first := make(chan error, 1)
	go func() {
		_, err := loop.SubmitDelegated(ctx, DelegatedInput{Site: backflowNestedSite, Binding: binding, Request: "hold the first consumer"})
		first <- err
	}()
	select {
	case <-tool.entered:
	case <-time.After(backflowNestedWindow):
		t.Fatal("the first delegated Consumer never entered its tool")
	}
	stopped := make(chan struct{})
	go func() {
		loop.StopResidents()
		close(stopped)
	}()
	select {
	case <-tool.cancelled:
	case <-time.After(backflowNestedWindow):
		t.Fatal("StopResidents did not cancel the existing Consumer")
	}

	before := len(history.SessionFacts(backflowNestedSite))
	secondCtx, secondCancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer secondCancel()
	second := make(chan error, 1)
	go func() {
		_, err := loop.SubmitDelegated(secondCtx, DelegatedInput{
			Site:    backflowNestedSite,
			Binding: binding,
			Request: "this second input must be refused before commit",
		})
		second <- err
	}()
	var secondErr error
	select {
	case secondErr = <-second:
	case <-time.After(backflowNestedBudget):
		secondErr = errors.New("the second Submit did not return")
	}
	close(tool.hold)
	select {
	case <-stopped:
	case <-time.After(backflowNestedWindow):
		t.Fatal("StopResidents did not finish after releasing the existing Consumer")
	}
	if !errors.Is(secondErr, ErrDelegatedInstanceUnavailable) {
		t.Fatalf("Submit beside stopped existing Consumer returned %v, want ErrDelegatedInstanceUnavailable", secondErr)
	}
	if after := len(history.SessionFacts(backflowNestedSite)); after != before {
		t.Fatalf("facts after the refused Submit = %d, want the unchanged %d", after, before)
	}
}

// backflowNestedCallToolName is the name the scripted round calls.
const backflowNestedCallToolName = "nested_call"

// backflowNestedCallTool is the tool one round of the delegated site calls: it admits
// the call as a task and hands that task's settled notification to the loop's own
// backflow path, which is the delivery a task pipeline makes from outside the loop.
type backflowNestedCallTool struct {
	loop *Loop
	// settle lets the check fire the settled notification of the admitted task.
	settle chan struct{}
	// backflowed closes once the notification reached the loop.
	backflowed chan struct{}
}

var _ trpctool.CallableTool = (*backflowNestedCallTool)(nil)

// Declaration names the tool the scripted round calls.
func (t *backflowNestedCallTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: backflowNestedCallToolName, Description: "admits one nested call as a task"}
}

// Call admits the call and delivers its settled notification once the check fires it.
func (t *backflowNestedCallTool) Call(ctx context.Context, _ []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, errors.New("agent: the nested call carries no frame")
	}
	go func() {
		<-t.settle
		t.loop.Backflow(frame.Identity.InvocationID, task.Notice{
			TaskID: backflowNestedTask,
			Round:  1,
			Kind:   task.NoticeSettled,
			Seq:    0,
			Event:  "fact-nested-settled",
		})
		close(t.backflowed)
	}()
	return task.AdmissionReceipt{CallID: frame.Identity.CallID, TaskID: backflowNestedTask, Accepted: true, Kind: task.AdmissionFresh}, nil
}

// newBackflowNestedLoop assembles one loop beside no host: no consumption set is
// attached through SetConsumers, so the delegated site's resident instance belongs to
// the set the loop mounts for itself, which is the shape an embedder or a test drives
// the assembled runtime in.
func newBackflowNestedLoop(t *testing.T, m model.Model, tool *backflowNestedCallTool) (*Loop, *invariantLoopHistory) {
	t.Helper()
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-a", nil)
	history := newInvariantLoopHistory()
	var mu sync.Mutex
	seq := 0
	loop.SetHistory(history)
	loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		seq++
		key := event.EventID(fmt.Sprintf("%s#fact-%d", evt.Owner.SessionID, seq))
		mu.Unlock()
		evt.Key = key
		history.land(evt)
		return key, nil
	})
	tool.loop = loop
	t.Cleanup(loop.StopResidents)
	return loop, history
}

// backflowNestedOutputs reports the agent_output facts one address's chain carries.
func backflowNestedOutputs(history *invariantLoopHistory, addr event.Address) []event.Event {
	var out []event.Event
	for _, evt := range history.SessionFacts(addr) {
		if evt.Type == event.TypeAgentOutput {
			out = append(out, evt)
		}
	}
	return out
}

// TestOutputFactCarriesStableTurnMetadata pins the closing fields of one segment's agent_output fact to its delivered answer.
// - a completed segment records its terminal, the empty failure and closing round, its trigger lineage and its decision count
// - a failed segment records the failed terminal beside the trimmed cause
// - the input relation the fact commits stays beside the new entries
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestOutputFactCarriesStableTurnMetadata(t *testing.T) {
	t.Run("a completed segment commits its turn fields", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the instance of %s/%s = nil, want one instance to drive the material", addr.AgentID, addr.SessionID)
		}
		rig.landInput(addr, residentInstanceInput)
		consumer.Wake()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil {
			t.Fatalf("the segment of %s closed beside %v, want a completed turn", addr.SessionID, answer.Err)
		}
		outputs := rig.outputs(addr)
		if len(outputs) != 1 {
			t.Fatalf("agent_output facts of %s = %d, want the one the closed segment committed", addr.SessionID, len(outputs))
		}
		if outputs[0].InvocationID != answer.Turn.InvocationID {
			t.Fatalf("output fact invocation = %q, want the closed segment %q", outputs[0].InvocationID, answer.Turn.InvocationID)
		}
		expectOutputTurnMetadata(t, outputs[0], answer)
		if _, held := outputs[0].Meta[event.MetaKeyInputEvents]; !held {
			t.Fatalf("output fact carries no %q entry: the turn metadata stands beside the input relation", event.MetaKeyInputEvents)
		}
	})

	t.Run("a failed segment commits the cause its turn reported", func(t *testing.T) {
		failure := errors.New("agent: the model entry refused the request")
		rig := newResidentInstanceRigWithModel(t, newInvariantLoopModel("", failure))
		addr := rig.otherAddress()
		consumer := rig.consumers.Ensure(addr)
		if consumer == nil {
			t.Fatalf("the instance of %s/%s = nil, want one instance to drive the material", addr.AgentID, addr.SessionID)
		}
		rig.landNotice(addr, residentInstanceNotice)
		consumer.Wake()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil {
			t.Fatalf("the failed segment of %s ended beside %v, want the turn's failed terminal alone", addr.SessionID, answer.Err)
		}
		if answer.Turn.Terminal != model.KindFailed {
			t.Fatalf("the segment of %s closed on %v, want the failed terminal", addr.SessionID, answer.Turn.Terminal)
		}
		if strings.TrimSpace(answer.Turn.Failure) == "" {
			t.Fatalf("the failed segment of %s reports no cause, want the model's refusal", addr.SessionID)
		}
		if answer.Trigger != "task" {
			t.Fatalf("the segment of %s opened on trigger %q, want the notice's %q lineage", addr.SessionID, answer.Trigger, "task")
		}
		outputs := rig.outputs(addr)
		if len(outputs) != 1 {
			t.Fatalf("agent_output facts of %s = %d, want the one the failed segment committed", addr.SessionID, len(outputs))
		}
		expectOutputTurnMetadata(t, outputs[0], answer)
	})
}

// expectOutputTurnMetadata pins the five stable entries one agent_output fact
// commits to the turn the same segment delivered.
func expectOutputTurnMetadata(t *testing.T, fact event.Event, answer SegmentAnswer) {
	t.Helper()
	wants := []struct {
		key  string
		want string
	}{
		{key: "terminal", want: answer.Turn.Terminal.String()},
		{key: "failure", want: answer.Turn.Failure},
		{key: "final_round", want: string(answer.Turn.FinalRound)},
		{key: "trigger", want: answer.Trigger},
		{key: "decision_count", want: fmt.Sprint(answer.Turn.DecisionCount)},
	}
	for _, want := range wants {
		got, held := fact.Meta[want.key]
		if !held {
			t.Fatalf("agent_output metadata carries no %q entry: got %v, want the closed turn's %q", want.key, fact.Meta, want.want)
		}
		if got != want.want {
			t.Fatalf("agent_output metadata %q = %q, want the closed turn's %q", want.key, got, want.want)
		}
	}
}
