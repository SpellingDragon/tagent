package agent

import (
	"context"
	"errors"
	"fmt"
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
)

// invariantLoopAddress is the session address the invariants' drives run in.
var invariantLoopAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// invariantLoopOtherAddress is the neighbouring session address the invariants' drives never mix with.
var invariantLoopOtherAddress = event.Address{AgentID: "agent-a", SessionID: "session-b"}

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
			turn, err := loop.Drive(ctx, Invocation{
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
				if out.turn.Terminal != model.KindCompleted || len(out.turn.Decisions) != 1 {
					t.Fatalf("turn %d = (%v, %d decisions), want one completed decision", i, out.turn.Terminal, len(out.turn.Decisions))
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
		turn, err := loop.Drive(ctx, Invocation{
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
		if len(turn.Decisions) != 1 {
			t.Fatalf("decisions beside a refused model entry = %d, want the one attempt", len(turn.Decisions))
		}
		admitted := capture.admissions()
		if len(admitted) != 1 || len(capture.views()) != 1 {
			t.Fatalf("admissions beside a refused model entry = (%d attempts, %d views), want one of each", len(admitted), len(capture.views()))
		}
		if got := admitted[0].outcome.Kind; got != model.KindFailed {
			t.Fatalf("admitted attempt outcome = %v, want the reduced terminal itself", got)
		}
		if admitted[0].episodeID != "episode-1" || admitted[0].decisionID != turn.Decisions[0].DecisionID {
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
		turn, err := loop.Drive(ctx, Invocation{
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
		if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the tool round beside the answering one", turn.Terminal, len(turn.Decisions))
		}
		admitted := capture.admissions()
		if len(admitted) != 2 {
			t.Fatalf("admitted attempts = %d, want one per decision of the drive", len(admitted))
		}
		for i, admission := range admitted {
			if admission.decisionID != turn.Decisions[i].DecisionID {
				t.Fatalf("admitted attempt %d names decision %q, want %q", i, admission.decisionID, turn.Decisions[i].DecisionID)
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

	t.Run("a production registered for a drive confirms its stop only after that drive ended", func(t *testing.T) {
		behaviour := &invariantLoopModel{gate: make(chan struct{})}
		loop, _, _ := invariantLoop(t, behaviour)
		watch := &invariantLoopWatch{}
		loop.SetWatch(watch.register)
		done := make(chan invariantLoopResult, 1)
		go func() {
			turn, err := loop.Drive(ctx, Invocation{
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
		registered := watch.list()
		if len(registered) == 0 {
			t.Fatal("a drive in flight registered no production")
		}
		for _, stopped := range registered {
			if stopped() {
				t.Fatal("a production still in flight confirmed its stop")
			}
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
		for _, stopped := range watch.list() {
			if !stopped() {
				t.Fatal("a production of an ended drive never confirmed its stop")
			}
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
		turn, err := loop.Drive(ctx, Invocation{
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

// invariantLoopHistory keeps one transcript per session address.
type invariantLoopHistory struct {
	mu        sync.Mutex
	bySession map[event.SessionID][]model.Message
}

// newInvariantLoopHistory returns an empty session transcript face.
func newInvariantLoopHistory() *invariantLoopHistory {
	return &invariantLoopHistory{bySession: map[event.SessionID][]model.Message{}}
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

// invariantLoopWatch records the in-flight productions a drive registers.
type invariantLoopWatch struct {
	mu    sync.Mutex
	stops []func() bool
}

// register records one production stop confirmation.
func (w *invariantLoopWatch) register(_ event.TaskID, stopped func() bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stops = append(w.stops, stopped)
	return nil
}

// list reports the stop confirmations registered so far.
func (w *invariantLoopWatch) list() []func() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]func() bool(nil), w.stops...)
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
	original := trpclog.Default
	trpclog.Default = logger
	tb.Cleanup(func() { trpclog.Default = original })
}

// TestConsumerObservation checks the consumer face of the observation bypass.
// - one drive renders a start line carrying the address and the ready count, and an end line carrying the decision count and the terminal
// - every decision the drive consumes renders one line carrying its terminal and the number of calls the decision admitted
// - the consumer face writes nothing at INFO, so the bounded stream stays with turn boundaries and model requests
// - a logger that fails does not end the drive
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestConsumerObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("one drive renders its start, its decision and its end", func(t *testing.T) {
		logger := &consumerLog{}
		watchConsumerLog(t, logger)
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-a", nil))

		turn, err := loop.Drive(ctx, Invocation{
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
		if !logger.mentions("debug", "consumer drive end", "invocation=invocation-1", "decisions=1", "terminal=completed", "failed=false") {
			t.Fatalf("debug lines = %v, want the end line of the drive", logger.at("debug"))
		}
		if lines := logger.at("debug"); len(lines) != 3 {
			t.Fatalf("debug lines = %v, want one start, one decision and one end", lines)
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

		if _, err := loop.Drive(ctx, Invocation{
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

		turn, err := loop.Drive(ctx, Invocation{
			InvocationID: "invocation-3",
			Address:      invariantLoopAddress,
			Instance:     "instance-a",
			Ready: []event.Event{{
				Key:     "event-3",
				Message: model.Message{Role: model.RoleUser, Content: "hello"},
			}},
		})

		if err != nil || turn.Terminal != model.KindCompleted || len(turn.Decisions) != 1 {
			t.Fatalf("drive through a failing logger = (%v, %d decisions, %v), want the one completed decision", turn.Terminal, len(turn.Decisions), err)
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

		turn, err := loop.Drive(ctx, Invocation{
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

		if _, err := loop.Drive(ctx, Invocation{
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

		if _, err := loop.Drive(ctx, Invocation{
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
		turn, err := loop.Drive(ctx, Invocation{
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
		turn, err := loop.Drive(ctx, Invocation{
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

		turn, err := loop.Drive(ctx, Invocation{
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
