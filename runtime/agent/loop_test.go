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
		recorder := &invariantLoopRecorder{}
		loop.SetRecorder(recorder)
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
		if got := recorder.counts(); got != (invariantLoopCounts{episodes: 1, decisions: 1, attempts: 1}) {
			t.Fatalf("records beside a refused model entry = %+v, want one of each production point", got)
		}
		if got := recorder.outcome(); got != model.KindFailed {
			t.Fatalf("recorded attempt outcome = %v, want the reduced terminal itself", got)
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

// invariantLoopCounts is the number of records one recorder was asked to write.
type invariantLoopCounts struct {
	episodes  int
	decisions int
	attempts  int
}

// invariantLoopRecorder counts the records written through it and keeps the last attempt outcome.
type invariantLoopRecorder struct {
	mu       sync.Mutex
	counted  invariantLoopCounts
	outcomes []model.Outcome
}

// RecordEpisode counts one episode record.
func (r *invariantLoopRecorder) RecordEpisode(context.Context, event.EpisodeID, event.SessionID, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counted.episodes++
	return nil
}

// RecordDecision counts one decision record.
func (r *invariantLoopRecorder) RecordDecision(context.Context, event.DecisionID, event.InvocationID, event.EpisodeID, []event.EventID, string, string, []string, time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counted.decisions++
	return nil
}

// RecordAttempt counts one attempt record beside the outcome it was handed.
func (r *invariantLoopRecorder) RecordAttempt(_ context.Context, _ event.AttemptID, _ event.DecisionID, _ event.EpisodeID, outcome model.Outcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counted.attempts++
	r.outcomes = append(r.outcomes, outcome)
	return nil
}

// counts reports the records written so far.
func (r *invariantLoopRecorder) counts() invariantLoopCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counted
}

// outcome reports the kind of the last attempt outcome handed to the recorder.
func (r *invariantLoopRecorder) outcome() model.Kind {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.outcomes) == 0 {
		return model.KindUnknown
	}
	return r.outcomes[len(r.outcomes)-1].Kind
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
func (c *invariantCommitFace) commit(evt event.Event) event.EventID {
	c.mu.Lock()
	defer c.mu.Unlock()
	if evt.Key == "" {
		evt.Key = event.EventID(fmt.Sprintf("minted-%d", len(c.events)+1))
	}
	c.events = append(c.events, evt)
	if c.feed != nil && evt.Type == event.TypeFeedback {
		c.feed.append(NoticeView{Key: evt.Key, Text: evt.Message.Content})
	}
	return evt.Key
}

// list reports the facts committed so far.
func (c *invariantCommitFace) list() []event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.Event(nil), c.events...)
}

// TestBackflowNotificationLanes pins the lanes one notification of a call is admitted into.
// - a progress notification commits as a notice fact of its range before it is read, and answers no call
// - the range reads the notice as a projection increment from the ended boundary, as input material, and reads it once
// - a range beside no chain reads the cached notification once and settles nothing
// - a settled notification still pairs its result with the call and parks no notice
// - a notification that is neither progress nor terminal leaves the range exactly as it stands
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestBackflowNotificationLanes(t *testing.T) {
	ctx := context.Background()

	t.Run("progress commits as a notice fact and enters the range's next decision", func(t *testing.T) {
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
			t.Fatalf("notice fact message = %+v, want the input material the range reads", fact.Message)
		}
		if fact.Owner != invariantLoopAddress || fact.Target != invariantLoopAddress || fact.Source != invariantLoopAddress {
			t.Fatalf("notice fact addresses = (%+v, %+v, %+v), want the range address", fact.Owner, fact.Target, fact.Source)
		}

		in := loop.intakeFor("invocation-progress")
		in.mu.Lock()
		parked := len(in.settled[notice.TaskID])
		waiters := len(in.waiters[notice.TaskID])
		cached := len(in.notices)
		in.mu.Unlock()
		if parked != 0 || waiters != 0 {
			t.Fatalf("intake beside a progress notification = (%d parked, %d waits), want the settled wait left open", parked, waiters)
		}
		if cached != 1 {
			t.Fatalf("notice cache = %d, want the one admission of the progress", cached)
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
		if !invariantCarries(requests[0], note.Content) {
			t.Fatalf("first request messages = %+v, want the progress of the call as input material", requests[0])
		}
		if note.Role != model.RoleUser || note.ToolID != "" || note.ToolName != "" {
			t.Fatalf("progress note = %+v, want input material that answers no call", note)
		}
		if cursors := feed.readCursors(); len(cursors) != 1 || cursors[0] != 0 {
			t.Fatalf("notice reads = %v, want the one read from the ended boundary", cursors)
		}
		if notes := loop.progressNotes(Invocation{InvocationID: "invocation-progress", Address: invariantLoopAddress}); len(notes) != 0 {
			t.Fatalf("progress read twice = %+v, want the notification reported once", notes)
		}
	})

	t.Run("a range beside no chain reads the cached notification once", func(t *testing.T) {
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-cache", nil))
		notice := task.Notice{TaskID: "task-cache", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-cache"}
		loop.Backflow("invocation-cache", notice)

		in := loop.intakeFor("invocation-cache")
		in.mu.Lock()
		cached := len(in.notices)
		key := event.EventID("")
		if cached == 1 {
			key = in.notices[0].key
		}
		in.mu.Unlock()
		if cached != 1 || key != "" {
			t.Fatalf("notice cache = (%d, %q), want the one cached admission beside no commit face", cached, key)
		}
		invocation := Invocation{InvocationID: "invocation-cache", Address: invariantLoopAddress}
		notes := loop.progressNotes(invocation)
		if len(notes) != 1 || notes[0].Content != progressNoteOf(notice).Content {
			t.Fatalf("progress notes = %+v, want the cached report read once", notes)
		}
		if again := loop.progressNotes(invocation); len(again) != 0 {
			t.Fatalf("progress read twice = %+v, want the notification reported once", again)
		}
	})

	t.Run("a settled notification still pairs its result with the call", func(t *testing.T) {
		loop, _, _ := invariantLoop(t, newInvariantLoopModel("answer-settled", nil))
		notice := task.Notice{TaskID: "task-settled", Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "fact-settled"}
		loop.Backflow("invocation-settled", notice)

		in := loop.intakeFor("invocation-settled")
		in.mu.Lock()
		parked := len(in.settled["task-settled"])
		cached := len(in.notices)
		in.mu.Unlock()
		if parked != 1 || cached != 0 {
			t.Fatalf("intake beside a settled notification = (%d parked, %d notices), want the paired result alone", parked, cached)
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
		cached := len(in.notices)
		in.mu.Unlock()
		if parked != 0 || waits != 0 || cached != 0 {
			t.Fatalf("intake beside an accepted notification = (%d parked, %d waits, %d notices), want the intake untouched", parked, waits, cached)
		}
		if notes := loop.progressNotes(Invocation{InvocationID: "invocation-accepted", Address: invariantLoopAddress}); len(notes) != 0 {
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
