package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// executionSerialization names the serialized execution a dispatched tool call
// is admitted under: at most one effective model decision per session at a
// time, the rest queued.
const executionSerialization task.Serialization = "session-serialized"

// FactResolver reports the native tool message one recorded result fact
// carries. The fact identity comes from the settled notification of a call, so
// the loop pairs the tool message with the call it answers.
type FactResolver func(id event.EventID) (model.Message, bool)

// FactCommit is the one commit face of the facts the loop produces: it stores
// the whole event as the ledger of the run keeps it and reports the identity
// that fact committed under, minting one for an event that arrives without an
// identity. The identity it answers is the one the After pass reads back, so an
// observation never reconstructs the fact it observes.
//
// The redline list (2 rules):
// MUST store the whole event as it stands, minting an identity only for an event that carries none.
// MUST report the identity that fact committed under, so the observation reads that very fact back.
type FactCommit func(evt event.Event) event.EventID

// FactPipelines assembles the ordered plugin pipeline of one agent, so the
// After pass of a fact the loop commits outside a dispatch — the ready input of
// a range, the output of a drive, the settled delivery of a call — runs through
// the very faces the published generation of that agent carries. A refused
// assembly stays a named gap beside the committed fact: observation is a
// bypass, never part of the commit.
//
// The redline list (2 rules):
// MUST assemble the faces of the published generation of the named agent, in its declared order.
// MUST NOT run a second observation beside the pipeline it answers.
type FactPipelines func(agentID event.AgentID) (plugin.Pipeline, error)

// factPipelineName is the stable name a refused fact pipeline assembly is
// reported under, so a fact that committed without its observation is readable
// as a gap by the face that failed to assemble.
const factPipelineName = "fact-pipeline"

// ErrFactPipelineUnwired reports a committed fact the loop could not observe
// because no pipeline face was attached: a gap by name, never a silently
// dropped observation.
var ErrFactPipelineUnwired = errors.New("agent: no fact pipeline is attached to observe through")

// Recorder is the narrow training-record production face this loop writes its
// episode, decision and attempt records through. The methods receive the
// observations scattered exactly as the production point observed them — the
// record identities, the stored input events, the model view material and the
// reduced outcome — and never a record of another package's shape: the
// signatures cross built-in, event and model types only, so the record
// production side attaches through this face with no package edge into the
// execution core, the same stitching the projected request assembly uses.
//
// The production points of this loop write one record per real decision and per
// observable attempt; a nil recorder keeps the loop recording nothing, and a
// recorder error never reaches the model decision: recording is observation,
// not execution.
// The redline list (4 rules):
// MUST write at the observation point, carrying the identities and the values as that point observed them.
// MUST report every record it could not write as a counted loss beside the record identity.
// MUST NOT derive a record of its own: the outcome arrives reduced, never re-judged here.
// MUST NOT let a write failure reach the model decision: recording is observation, not execution.
type Recorder interface {
	// RecordEpisode writes the episode record of one business process at the
	// point that process actually starts: the application's own episode
	// boundary the ready input names, the session the invocation runs in and
	// the time the record was written.
	RecordEpisode(ctx context.Context, episodeID event.EpisodeID, sessionID event.SessionID, at time.Time) error
	// RecordDecision writes the decision record of one model decision at the
	// point that decision produced its terminal: the identities it belongs to,
	// the stored input events it reduced and the model view material its
	// request actually ran on, each visible tool named exactly as it ran.
	RecordDecision(ctx context.Context, decisionID event.DecisionID, invocationID event.InvocationID, episodeID event.EpisodeID, inputs []event.EventID, modelName, promptVersion string, visibleTools []string, at time.Time) error
	// RecordAttempt writes the attempt record of one observable model attempt at
	// the point that attempt produced its response: the outcome exactly as the
	// single model reduction derived it, so the record side shares that terminal
	// and derives none of its own.
	RecordAttempt(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error
}

// Turn is the outcome one invocation's drive reports: the decisions it ran in
// order and the terminal its latest decision reduced to.
type Turn struct {
	// InvocationID is the range the turn belongs to.
	InvocationID event.InvocationID
	// Decisions lists the model decisions the turn ran, in execution order.
	Decisions []Decision
	// Terminal is the terminal the latest decision reduced to, or KindUnknown
	// while no decision reached one.
	Terminal model.Kind
	// FinalRound names why the closing round of this drive ran: the round budget
	// or the time budget was spent, so the latest decision delivers instead of
	// continuing. It stands empty for a drive that ended on its own terminal.
	FinalRound FinalRoundReason
}

// Loop drives the persistent event loop of one invocation: borrow the binding
// generation effective at execution start, aggregate the range's own input,
// assemble one native model request, reduce that request through the single
// terminal entry point, admit every tool call through the task wrapper, and
// let the settled notification backflow drive the next decision of the same
// invocation. One invocation runs its own decisions over its own transcript,
// so two invocations of one session are never batched together: each
// aggregates only its own ready input and each keeps its own decisions
// independent of the other's terminal.
// - This loop is the runtime's only tool-call loop: no framework Runner or LLMAgent drives a second loop beside it. The terminal of a decision is read from the reduction outcome and never re-derived here: this file does not classify the six states and does not judge the terminal on stream mechanics.
// - A decision borrows the binding generation effective when it starts executing and keeps that handle until the work it set in motion has ended, while a publication during the decision never changes the generation it runs with.
// - A settled task notification backflows to the invocation that issued the call and drives that invocation's next decision, which borrows the generation effective at its own start — never one chosen in advance.
// - The watchdog budget of a drive is read at the boundary between its rounds: the round that boundary finds the budget spent is the closing round, and the drive ends when that round returns.
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
type Loop struct {
	borrow   state.Borrow
	slots    *state.Occupancy
	instance event.InstanceID
	facts    FactResolver

	mu        sync.Mutex
	seq       uint64
	intakes   map[event.InvocationID]*intake
	history   HistoryFace
	recorder  Recorder
	commit    FactCommit
	pipelines FactPipelines
	present   PresentationSink
	boundary  RequestFace
	snapshot  RequestSnapshotFace
	watch     func(taskID event.TaskID, stopped func() bool) error
	final     FinalRoundPolicy
	losses    []RecordingLoss
}

// NewLoop returns the event loop over the state owner's borrowing face, the
// session decision-slot ledger and the fact resolver that turns a recorded
// result fact into the native tool message it carries.
func NewLoop(borrow state.Borrow, slots *state.Occupancy, instance event.InstanceID, facts FactResolver) *Loop {
	return &Loop{
		borrow:   borrow,
		slots:    slots,
		instance: instance,
		facts:    facts,
		intakes:  map[event.InvocationID]*intake{},
	}
}

// Drive runs one invocation until its latest decision reaches a terminal that
// dispatches no tool: every decision aggregates the transcript the range has
// accumulated beside the session history the drives that already ended on this
// session committed, and the receipts and settled observations of the tools one
// decision admitted enter that transcript before the next decision begins. The
// ready input of the range is never mixed with the input of another invocation.
// - The episode record is written where the business process actually starts: at the first drive of the invocation's own ready input.
// - The settled result facts one decision paired are the stored input the next decision reduces.
// - The transcript of this drive is committed beside the terminal answer of its last decision, so the next invocation of this session reads the range's history as it actually ended.
// - The whole drive is watched as one in-flight production beside its invocation identity — the queueing waits included — and confirms its stop only when the drive really ended.
// - A drive whose budget is spent runs one closing round and ends there: the closing round delivers what the range already produced, its transcript is committed the same way, and the obligation it opens is none.
// - The drive is observed as one consumer run: its start and its end are DEBUG lines carrying identities, counts and the terminal, never transcript content.
func (l *Loop) Drive(ctx context.Context, inv Invocation) (turn Turn, err error) {
	turn = Turn{InvocationID: inv.InvocationID, Terminal: model.KindUnknown}
	consumerStarted := time.Now()
	consumerDebug("tagent: consumer drive start agent=%s session=%s invocation=%s ready=%d",
		inv.Address.AgentID, inv.Address.SessionID, inv.InvocationID, len(inv.Ready))
	defer func() {
		consumerDebug("tagent: consumer drive end invocation=%s decisions=%d terminal=%s elapsed=%s failed=%t",
			inv.InvocationID, len(turn.Decisions), turn.Terminal, time.Since(consumerStarted).Round(time.Microsecond), err != nil)
	}()
	production := &inFlightProducer{}
	l.watchProducer(event.TaskID(inv.InvocationID), production.Stopped)
	defer production.stop()
	l.intakeFor(inv.InvocationID).setAddress(inv.Address)
	l.observeReady(ctx, inv)
	history := l.sessionHistory(inv.Address)
	transcript := append(append([]model.Message(nil), history...), aggregateReady(inv.Ready)...)
	l.recordEpisode(ctx, inv)
	inputs := readyEventIDs(inv.Ready)
	policy := l.finalRound()
	started := time.Now()
	rounds := 0
	for {
		round := closingRound(policy, rounds, time.Since(started))
		decision, outcome, receipts, observations, facts, decideErr := l.decide(ctx, inv, transcript, inputs, round)
		if decideErr != nil {
			return turn, decideErr
		}
		turn.Decisions = append(turn.Decisions, decision)
		turn.Terminal = outcome.Kind
		inputs = append(inputs, facts...)
		if round.final {
			turn.FinalRound = round.reason
			l.commitTranscript(inv.Address, append(transcript[len(history):], append([]model.Message{assistantOf(outcome)}, receipts...)...))
			l.commitOutput(ctx, inv, decision, outcome)
			return turn, nil
		}
		if len(receipts) == 0 && len(observations) == 0 {
			l.commitTranscript(inv.Address, append(transcript[len(history):], assistantOf(outcome)))
			l.commitOutput(ctx, inv, decision, outcome)
			return turn, nil
		}
		transcript = append(transcript, assistantOf(outcome))
		transcript = append(transcript, receipts...)
		transcript = append(transcript, observations...)
		rounds++
	}
}

// consumerDebug renders one line of the consumer face at DEBUG: the drive's start
// and end beside each decision the drive consumed. The consumer is the turn driver
// of one invocation, and a line carries identities, counts and the terminal, never
// transcript content.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed drive.
// MUST keep the INFO stream bounded by leaving every consumer line at DEBUG.
// MUST NOT render transcript, message or tool-output content.
func consumerDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}

// Backflow admits one settled notification of a call into the intake of the
// invocation that issued that call, so it drives that invocation's next
// decision and no other. A non-terminal notification carries no result to pair
// and is ignored here. The settled delivery commits as the delivery fact of the
// range before it is handed on, so the delivery boundary of a call is observed
// as the fact it is; the After pass of that fact runs beside the notification
// rather than under the cancellation of the caller that delivered it, because
// observation is a bypass.
func (l *Loop) Backflow(invocationID event.InvocationID, notice task.Notice) {
	switch notice.Kind {
	case task.NoticeSettled, task.NoticeFailed, task.NoticeCancelled:
	default:
		return
	}
	msg := model.Message{Role: model.RoleTool}
	if l.facts != nil {
		if got, ok := l.facts(notice.Event); ok {
			msg = got
		}
	}
	in := l.intakeFor(invocationID)
	l.commitSettled(context.Background(), invocationID, in.rangeAddress(), notice)
	in.deliver(notice.TaskID, delivered{msg: msg, fact: notice.Event})
}

// observeReady runs the After pass of the ready input of one invocation: every
// ready event carries the identity it committed under at the entry of its
// range, so the turn-start boundary of this drive is observed as the fact the
// run already holds. A ready event that carries no identity names no committed
// fact and is not observed; the drive itself reads the event either way.
func (l *Loop) observeReady(ctx context.Context, inv Invocation) {
	for _, ev := range inv.Ready {
		if ev.Key == "" {
			continue
		}
		l.afterPass(ctx, inv.Address.AgentID, ev.Key)
	}
}

// commitOutput commits the assistant output one ended drive answers with as the
// turn-end fact of the range and runs its After pass: the message the drive
// commits to the transcript of its session beside the decision, invocation and
// address it belongs to, so the turn end is one committed fact of the run and
// not a line derived from the outcome afterwards.
func (l *Loop) commitOutput(ctx context.Context, inv Invocation, decision Decision, outcome model.Outcome) {
	l.commitFact(ctx, inv.Address.AgentID, event.Event{
		Type:         event.TypeAgentOutput,
		Timestamp:    time.Now(),
		Message:      assistantOf(outcome),
		Owner:        inv.Address,
		Source:       inv.Address,
		Target:       inv.Address,
		InvocationID: inv.InvocationID,
		InstanceID:   inv.Instance,
		DecisionID:   decision.DecisionID,
		EpisodeID:    episodeOfReady(inv.Ready),
	})
}

// commitSettled commits one settled notification as the delivery fact of the
// range it settles into, beside the task it belongs to and the address the
// notification was accepted at, and runs the After pass of that fact. A
// notification that arrived before the range recorded its own address commits
// no delivery fact: the fact of a delivery names the range it lands in.
func (l *Loop) commitSettled(ctx context.Context, invocationID event.InvocationID, addr event.Address, notice task.Notice) {
	if addr.AgentID == "" || addr.SessionID == "" {
		return
	}
	l.commitFact(ctx, addr.AgentID, event.Event{
		Type:         event.TypeTaskSettled,
		Timestamp:    time.Now(),
		TaskID:       notice.TaskID,
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: invocationID,
		InstanceID:   l.instance,
	})
}

// commitFact commits one fact of the run through the attached commit face and
// runs the After pass of that fact through the pipeline of the named agent. A
// loop beside no commit face commits nothing and observes nothing; a commit
// face beside no pipeline face still commits the fact but leaves its
// observation unnetted, so the missing face is a named gap beside a fact that
// stays committed as it stands.
func (l *Loop) commitFact(ctx context.Context, agentID event.AgentID, evt event.Event) {
	l.mu.Lock()
	commit, pipelines := l.commit, l.pipelines
	l.mu.Unlock()
	if commit == nil {
		return
	}
	key := commit(evt)
	if key == "" {
		return
	}
	if pipelines == nil {
		l.publish(view.Presentation{Fact: key, Gaps: []view.ObservationGap{{
			Plugin: factPipelineName,
			Fact:   key,
			Err:    ErrFactPipelineUnwired,
		}}})
		return
	}
	l.afterPass(ctx, agentID, key)
}

// afterPass runs the After pass of one committed fact through the pipeline of
// the named agent, and records a refused assembly as a named gap beside that
// fact: the fact stays committed exactly as it committed and the drive moves on.
func (l *Loop) afterPass(ctx context.Context, agentID event.AgentID, key event.EventID) {
	l.mu.Lock()
	pipelines := l.pipelines
	l.mu.Unlock()
	if pipelines == nil {
		return
	}
	pipeline, err := pipelines(agentID)
	if err != nil {
		l.publish(view.Presentation{Fact: key, Gaps: []view.ObservationGap{{
			Plugin: factPipelineName,
			Fact:   key,
			Err:    err,
		}}})
		return
	}
	l.publish(pipeline.After(ctx, key))
}

// SetRecorder attaches the training record production face this loop writes its
// episode, decision and attempt records through. The production points of this
// loop write one record per real decision and per observable attempt; a nil
// recorder keeps the loop recording nothing. The setter is separate from the
// constructor so the declared construction shape of the loop stays as it is.
func (l *Loop) SetRecorder(recorder Recorder) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recorder = recorder
}

// SetFactCommit attaches the commit face the loop commits the facts it produces
// through — the ready input of a range, the output of a drive and the settled
// delivery of a call — so every one of them lands in the one ledger of the run
// and is observed back as it committed. A nil face keeps the loop committing
// nothing of its own. The setter is separate from the constructor so the
// declared construction shape of the loop stays as it is.
func (l *Loop) SetFactCommit(commit FactCommit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.commit = commit
}

// SetFactPipelines attaches the pipeline face the After pass of a fact
// committed outside a dispatch is assembled through, so that pass runs over the
// faces of the published generation rather than a pipeline of the loop's own. A
// nil face beside a commit face is a wiring gap reported on the first committed
// fact. The setter is separate from the constructor so the declared
// construction shape of the loop stays as it is.
func (l *Loop) SetFactPipelines(pipelines FactPipelines) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pipelines = pipelines
}

// SetRequestFace attaches the request-boundary face this loop assembles each
// decision's request prefix through: the fixed file contents, the loaded skill
// bodies and the replayed projection of the session chain. A nil face keeps
// every request at its transcript alone. The setter is separate from the
// constructor so the declared construction shape of the loop stays as it is.
func (l *Loop) SetRequestFace(face RequestFace) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.boundary = face
}

// RequestSnapshotFace fixes the safe request snapshot of one decision's
// request at the formal request boundary: the face is handed the request as it
// stands before the model entry is called — the full message set the model
// will see and the tools visible to it — beside the model view material the
// request runs on and the borrowed binding its visible skill set is read from.
// The face keeps a projected copy of the request and never edits the request
// itself, and a face error never reaches the model decision: capture is
// observation, not execution, and the refused fix is counted as an explicit
// recording loss beside its decision identity. The face crosses built-in,
// event, model and state types only, so the capture side attaches through it
// with no package edge into the execution core.
//
// The redline list (3 rules):
// MUST keep a projected copy of the request and say whether the fix was kept.
// MUST NOT edit the request it was handed, and MUST NOT hold it past the fix.
// MUST NOT let a refused fix reach the model decision: it is a counted recording loss.
type RequestSnapshotFace func(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools []string, binding state.AgentBinding, req model.Request) error

// SetRequestSnapshotFace attaches the safe request snapshot face this loop
// fixes each decision's request boundary snapshot through before that request
// starts. A nil face fixes nothing. The setter is separate from the constructor
// so the declared construction shape of the loop stays as it is.
func (l *Loop) SetRequestSnapshotFace(face RequestSnapshotFace) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.snapshot = face
}

// fixRequestSnapshot fixes the safe request snapshot of one decision's request
// through the attached face before that request starts, or fixes nothing while
// no face is attached. A fix the face refused is counted as an explicit
// recording loss beside its decision identity and never carried into the model
// decision.
func (l *Loop) fixRequestSnapshot(ctx context.Context, decisionID event.DecisionID, view modelView, binding state.AgentBinding, req model.Request) {
	l.mu.Lock()
	face := l.snapshot
	l.mu.Unlock()
	if face == nil {
		return
	}
	if err := face(ctx, decisionID, view.modelName, view.promptVersion, view.visibleTools, binding, req); err != nil {
		l.noteRecordingLoss("snapshot", string(decisionID), err)
	}
}

// SetWatch attaches the registration the close of this agent confirms the real
// stop of every in-flight production through: the whole drive of one invocation
// and the model stream of each of its decisions, each registered beside the work
// identity it runs behind, with the stop confirmation that work is polled
// through. A nil registration keeps the loop registering nothing. The setter is
// separate from the constructor so the declared construction shape of the loop
// stays as it is.
//
// The redline list (2 rules):
// MUST register a production when it really starts and confirm its stop only when it really ended.
// MUST NOT read a cancelled request, an emptied queue or an elapsed timeout as a stop.
func (l *Loop) SetWatch(watch func(taskID event.TaskID, stopped func() bool) error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.watch = watch
}

// watchProducer registers one in-flight production behind the named work
// identity through the attached face, or registers nothing while no face is
// attached.
func (l *Loop) watchProducer(taskID event.TaskID, stopped func() bool) {
	l.mu.Lock()
	watch := l.watch
	l.mu.Unlock()
	if watch == nil {
		return
	}
	_ = watch(taskID, stopped)
}

// inFlightProducer is the stop confirmation of one in-flight production of
// this loop: the model stream one decision reduces, or the whole drive of one
// invocation with the queueing waits it stands in. The production confirms its
// stop only when the work really ended: a cancelled request is never read as
// one.
type inFlightProducer struct {
	mu      sync.Mutex
	stopped bool
}

// Stopped reports whether this production really stopped.
func (p *inFlightProducer) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// stop confirms the real stop of this production.
func (p *inFlightProducer) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// requestBoundary assembles the request-boundary material of one decision
// through the attached face, or returns an empty view while no face is
// attached: the request then runs on its transcript alone. The boundary of a
// closing round is marked as such, so the face reads the round a request is.
func (l *Loop) requestBoundary(ctx context.Context, inv Invocation, binding state.AgentBinding, final bool) (BoundaryView, error) {
	l.mu.Lock()
	face := l.boundary
	l.mu.Unlock()
	if face == nil {
		return BoundaryView{}, nil
	}
	return face(ctx, RequestBoundary{
		Agent:      inv.Address.AgentID,
		Chain:      RequestChain{SessionID: inv.Address.SessionID},
		Binding:    binding,
		FinalRound: final,
	})
}

// RecordingLoss is one training record a production point could not write:
// the record kind and the identity it belonged to beside the named failure
// that stopped the write. A loss is counted here — never silently dropped —
// and never carried into the model decision: recording is observation, not
// execution.
type RecordingLoss struct {
	// Kind names the record kind the failed write belonged to: "episode",
	// "decision", "attempt" or "snapshot".
	Kind string
	// ID is the identity of the record the failed write belonged to.
	ID string
	// Err is the named failure that stopped the write.
	Err error
}

// noteRecordingLoss counts one record the production point could not write,
// beside the named failure that stopped the write.
func (l *Loop) noteRecordingLoss(kind, id string, cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.losses = append(l.losses, RecordingLoss{Kind: kind, ID: id, Err: cause})
}

// RecordingLosses reports the training records the production points could not
// write, in arrival order, each beside its record identity and the named
// failure that stopped the write. The account is the explicit gap ledger of
// the run: nothing the recorder refused is silently forgotten here.
func (l *Loop) RecordingLosses() []RecordingLoss {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]RecordingLoss(nil), l.losses...)
}

// currentRecorder reports the attached recorder, or nil while none is attached.
func (l *Loop) currentRecorder() Recorder {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.recorder
}

// PresentationSink consumes the After pass presentation of one committed fact:
// the views its presenters rendered beside the visible gaps its failed
// observers left. The sink is the read-out face the presentation is handed to —
// the inspection face of the run — while the loop keeps the committed fact
// itself and never retracts it for anything this presentation carries.
type PresentationSink func(presentation view.Presentation)

// SetPresentations attaches the presentation sink this loop hands each After
// pass outcome to. The After pass itself runs either way — exactly once per
// committed fact and only after that fact's canonical commit — and a nil sink
// drops the presentation only, never the fact it was rendered from. The setter
// is separate from the constructor so the declared construction shape of the
// loop stays as it is.
func (l *Loop) SetPresentations(sink PresentationSink) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.present = sink
}

// publish hands one After pass presentation to the attached sink, or drops the
// presentation while no sink is attached: the drop is the presentation's own,
// and the committed fact it was rendered from stays untouched.
func (l *Loop) publish(presentation view.Presentation) {
	l.mu.Lock()
	sink := l.present
	l.mu.Unlock()
	if sink != nil {
		sink(presentation)
	}
}

// decide runs one model decision over the range's transcript: it takes the
// session decision slot, borrows the generation effective at this moment,
// assembles the native request, reduces the model stream to its single terminal
// through the model package's only reduction entry, and then admits every tool
// call of that terminal through the task wrapper. The decision slot is held
// only while the model decision is in flight; a tool call admitted here runs as
// a background task and holds no slot.
// - One decision boundary is one observable model attempt: a retry hidden below this boundary never mints a second attempt identity.
// - The model view is fixed before the request starts and is never rebuilt from the options of a finished call.
// - The safe request snapshot is fixed before the model entry is called and observes the request without editing it; a refused fix is counted as a recording loss.
// - The model entry error is presented with failure polarity through the same reduction: a synthesized failure response is reduced like any stream, so the terminal judgement stays in the model package alone.
// - The model decision reached its terminal: the session slot is free again while the admitted tools run as background tasks.
// - The handle is released once the work the decision set in motion has ended, never because a newer generation was published meanwhile.
// - The model production of one decision is watched beside its invocation identity and confirms its stop when the reduction really ended.
// - The closing round marks its request on the boundary, carries the closing instruction after the transcript, runs under the short closing timeout and admits none of the calls it issues.
func (l *Loop) decide(ctx context.Context, inv Invocation, transcript []model.Message, inputs []event.EventID, round decisionRound) (Decision, model.Outcome, []model.Message, []model.Message, []event.EventID, error) {
	if round.final {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, round.timeout)
		defer cancel()
	}
	decisionID := l.nextDecisionID(inv)
	attemptID := event.AttemptID(fmt.Sprintf("attempt-%s", decisionID))
	if err := l.occupy(ctx, inv, decisionID); err != nil {
		return Decision{}, model.Outcome{}, nil, nil, nil, err
	}
	handle, err := l.borrow.Borrow(ctx, inv.Address.AgentID)
	if err != nil {
		_ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID)
		return Decision{}, model.Outcome{}, nil, nil, nil, err
	}

	tools := make(map[string]trpctool.Tool, len(handle.Binding.Tools))
	callable := make(map[string]trpctool.CallableTool, len(handle.Binding.Tools))
	for _, tool := range handle.Binding.Tools {
		if tool == nil || tool.Declaration() == nil {
			continue
		}
		name := tool.Declaration().Name
		tools[name] = tool
		if native, ok := tool.(trpctool.CallableTool); ok {
			callable[name] = native
		}
	}
	view := modelViewOf(handle.Binding)
	boundary, err := l.requestBoundary(ctx, inv, handle.Binding, round.final)
	if err != nil {
		_ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID)
		_ = handle.Release(ctx)
		return Decision{}, model.Outcome{}, nil, nil, nil, err
	}
	if boundary.PromptVersion != "" {
		view.promptVersion = boundary.PromptVersion
	}
	messages := make([]model.Message, 0, len(boundary.Messages)+len(transcript)+1)
	messages = append(messages, boundary.Messages...)
	messages = append(messages, transcript...)
	if round.final {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: round.prompt})
	}
	req := model.Request{Messages: messages, Tools: tools}
	l.fixRequestSnapshot(ctx, decisionID, view, handle.Binding, req)

	production := &inFlightProducer{}
	l.watchProducer(event.TaskID(inv.InvocationID), production.Stopped)
	stream, entryErr := handle.Binding.Model.GenerateContent(ctx, &req)
	if entryErr != nil {
		stream = failedStream(entryErr)
	}
	outcome := model.ReduceChannel(ctx, stream, nil)
	production.stop()
	l.recordDecisionAttempt(ctx, inv, decisionID, attemptID, inputs, view, outcome)
	consumerDebug("tagent: consumer decision invocation=%s decision=%s terminal=%s calls=%d closing=%t",
		inv.InvocationID, decisionID, outcome.Kind, len(callsOf(outcome)), round.final)

	decision := Decision{
		DecisionID:   decisionID,
		InvocationID: inv.InvocationID,
		Binding:      handle.Binding,
		Request:      req,
		Terminal:     outcome.Kind,
	}
	_ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID)

	var receipts []model.Message
	var observations []model.Message
	var facts []event.EventID
	if outcome.Kind == model.KindCompleted && outcome.Body != nil && outcome.Body.IsToolCallResponse() {
		if round.final {
			receipts = refuseClosingCalls(callsOf(outcome))
		} else {
			receipts, observations, facts, err = l.dispatch(ctx, inv, decision, callsOf(outcome), callable)
			if err != nil {
				_ = handle.Release(ctx)
				return decision, outcome, nil, nil, nil, err
			}
		}
	}
	_ = handle.Release(ctx)
	return decision, outcome, receipts, observations, facts, nil
}

// dispatch admits every tool call of one decision through the task wrapper of
// the borrowed generation and answers each call with the single receipt of its
// admission as the paired tool message of that call. The admission loop admits
// every call first — one call's wait for its settlement never blocks the next
// call's admission — and the wait for the settled results happens outside that
// loop. Each settled result returns as the observation of its call beside the
// receipts, entering the next decision as input material rather than as a
// second synchronous response under the same call identifier.
// - The plugin pipeline of the borrowed generation runs its one Before pass per call before that call is admitted, and its After pass only over facts whose canonical commit already happened.
// - The one Before pass travels on the context into the admission ruling beside the call frame, so the pre-execution ruling consumes the risk grade its risk faces rendered and never runs a second risk face of its own.
// - The transformed final call is the one both executed and recorded; the identity it is admitted under stays the resolved CallID.
// - The receipt is the synchronous answer of the call: the model reads the admission and the task identity it was registered under, and the settled result never answers the call again.
// - Every call of this decision is already admitted and answered with its receipt before the first wait for a settled result begins.
// - The settled result fact is the stored event the next decision of this range reduces; a call that recorded no fact contributes none.
// - The After pass reads only a fact whose canonical commit already happened: a failed observation is a visible gap in the returned presentation and never rolls the committed fact back, and a dropped presentation drops the presentation only. The presentation is the read-out the inspection face consumes; the loop keeps the committed fact itself.
func (l *Loop) dispatch(ctx context.Context, inv Invocation, decision Decision, calls []model.ToolCall, tools map[string]trpctool.CallableTool) ([]model.Message, []model.Message, []event.EventID, error) {
	pipeline, err := plugin.NewPipeline(decision.Binding.Plugins)
	if err != nil {
		return nil, nil, nil, err
	}
	in := l.intakeFor(inv.InvocationID)
	receipts := make([]model.Message, 0, len(calls))
	type admittedCall struct {
		callID string
		tool   string
		taskID event.TaskID
	}
	var running []admittedCall
	for _, call := range calls {
		pass, beforeErr := pipeline.Before(ctx, call)
		if beforeErr != nil {
			receipts = append(receipts, paired(call.ID, call.Function.Name, "tagent: the plugin pass refused the call: "+beforeErr.Error()))
			continue
		}
		final := pass.Final
		name := final.Function.Name
		native, ok := tools[name]
		if !ok {
			receipts = append(receipts, paired(call.ID, name, "tagent: no tool named "+name))
			continue
		}
		frame := task.CallFrame{
			Identity: task.CallIdentity{
				InstanceID:   l.instance,
				InvocationID: inv.InvocationID,
				DecisionID:   decision.DecisionID,
				CallID:       call.ID,
			},
			Address:   inv.Address,
			Execution: executionSerialization,
		}
		answer, callErr := native.Call(task.WithCallFrame(governance.WithCallPass(ctx, pass), frame), final.Function.Arguments)
		switch out := answer.(type) {
		case task.AdmissionReceipt:
			if !out.Accepted {
				receipts = append(receipts, paired(call.ID, name, refusalText(callErr)))
				continue
			}
			receipts = append(receipts, paired(call.ID, name, admissionText(out)))
			running = append(running, admittedCall{callID: call.ID, tool: name, taskID: out.TaskID})
		case task.HoldReceipt:
			receipts = append(receipts, paired(call.ID, name, "tagent: the call is parked awaiting approval"))
		default:
			receipts = append(receipts, paired(call.ID, name, answerText(callErr, out)))
		}
	}
	observations := make([]model.Message, 0, len(running))
	var facts []event.EventID
	for _, adm := range running {
		arrival, ok := in.await(ctx, adm.taskID)
		if !ok {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
			observations = append(observations, observationOf(model.Message{Content: "tagent: the tool result did not arrive before the wait ended"}, adm.callID, adm.tool))
			continue
		}
		observations = append(observations, observationOf(arrival.msg, adm.callID, adm.tool))
		if arrival.fact != "" {
			facts = append(facts, arrival.fact)
			l.publish(pipeline.After(ctx, arrival.fact))
		}
	}
	return receipts, observations, facts, nil
}

// occupy takes the session decision slot for the named decision, waiting for
// the invocation already holding it to end its model decision instead of
// running a second one beside it.
func (l *Loop) occupy(ctx context.Context, inv Invocation, decisionID event.DecisionID) error {
	for {
		err := l.slots.Occupy(inv.Address, l.instance, inv.InvocationID, decisionID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, state.ErrSlotHeld) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

// nextDecisionID mints the identity of one decision of the named range.
func (l *Loop) nextDecisionID(inv Invocation) event.DecisionID {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	return event.DecisionID(fmt.Sprintf("decision-%s-%d", inv.InvocationID, l.seq))
}

// intakeFor returns the backflow intake of one invocation, creating it on first
// use.
func (l *Loop) intakeFor(invocationID event.InvocationID) *intake {
	l.mu.Lock()
	defer l.mu.Unlock()
	in, ok := l.intakes[invocationID]
	if !ok {
		in = newIntake()
		l.intakes[invocationID] = in
	}
	return in
}

// intake is one invocation's backflow intake: the address its range runs at,
// the settled tool messages that arrived before their wait began, and the waits
// of the calls still running. A notification of one invocation never reaches the
// intake of another.
type intake struct {
	mu      sync.Mutex
	address event.Address
	waiters map[event.TaskID][]chan delivered
	arrived map[event.TaskID][]delivered
}

// setAddress records the address the range of this intake runs at, so the
// delivery fact of a settled call names the range it lands in.
func (in *intake) setAddress(addr event.Address) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.address = addr
}

// rangeAddress reports the address the range of this intake runs at, or the
// zero address while no drive of it recorded one.
func (in *intake) rangeAddress() event.Address {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.address
}

// delivered is one settled tool result as it arrives through the backflow: the
// native tool message it pairs and the stored fact identity that message was
// recorded under.
type delivered struct {
	msg  model.Message
	fact event.EventID
}

// newIntake returns an empty backflow intake.
func newIntake() *intake {
	return &intake{
		waiters: map[event.TaskID][]chan delivered{},
		arrived: map[event.TaskID][]delivered{},
	}
}

// deliver hands one settled tool result to the wait of its task, or parks it
// until that wait begins.
func (in *intake) deliver(taskID event.TaskID, item delivered) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if waiters := in.waiters[taskID]; len(waiters) > 0 {
		waiters[0] <- item
		in.waiters[taskID] = waiters[1:]
		return
	}
	in.arrived[taskID] = append(in.arrived[taskID], item)
}

// await waits for the settled tool result of one task and reports whether one
// arrived before the context ended.
func (in *intake) await(ctx context.Context, taskID event.TaskID) (delivered, bool) {
	in.mu.Lock()
	if pending := in.arrived[taskID]; len(pending) > 0 {
		item := pending[0]
		in.arrived[taskID] = pending[1:]
		in.mu.Unlock()
		return item, true
	}
	ch := make(chan delivered, 1)
	in.waiters[taskID] = append(in.waiters[taskID], ch)
	in.mu.Unlock()
	select {
	case item := <-ch:
		return item, true
	case <-ctx.Done():
		return delivered{}, false
	}
}

// aggregateReady turns the ready input of one invocation into the native
// messages of its next request, in the order the session admitted it: the
// native message of each event is kept as it stands, so a multi-tool assistant
// message and its per-call paired tool messages survive untouched. Only a
// message carrying no role of its own is filled from the frozen event type.
func aggregateReady(ready []event.Event) []model.Message {
	msgs := make([]model.Message, 0, len(ready))
	for _, ev := range ready {
		msg := ev.Message
		if msg.Role == "" {
			msg.Role = RoleOf(ev.Type)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// assistantOf projects the assistant message one reduced body carries, keeping
// the native tool calls of that message for the transcript that the next
// request replays.
func assistantOf(outcome model.Outcome) model.Message {
	if outcome.Body == nil || len(outcome.Body.Choices) == 0 {
		return model.Message{Role: model.RoleAssistant}
	}
	choice := outcome.Body.Choices[0]
	msg := choice.Message
	if len(msg.ToolCalls) == 0 {
		msg.ToolCalls = choice.Delta.ToolCalls
	}
	if msg.Role == "" {
		msg.Role = model.RoleAssistant
	}
	return msg
}

// callsOf reports the native tool calls one reduced body carries.
func callsOf(outcome model.Outcome) []model.ToolCall {
	if outcome.Body == nil || len(outcome.Body.Choices) == 0 {
		return nil
	}
	choice := outcome.Body.Choices[0]
	if len(choice.Message.ToolCalls) > 0 {
		return choice.Message.ToolCalls
	}
	return choice.Delta.ToolCalls
}

// failedStream synthesizes the failure polarity of a model entry error as a
// response stream, so the entry error reaches the same single reduction every
// stream does instead of standing a terminal judgement of its own.
func failedStream(cause error) <-chan *model.Response {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Done:  true,
		Error: &model.ResponseError{Message: cause.Error(), Type: model.ErrorTypeStreamError},
	}
	close(ch)
	return ch
}

// paired builds the native tool message that answers one call: the output or
// the refusal text, kept byte-identical under the call identifier it pairs with.
func paired(callID, tool, content string) model.Message {
	return model.Message{Role: model.RoleTool, ToolID: callID, ToolName: tool, Content: content}
}

// episodeOfReady reports the application's own episode boundary the ready input
// of one invocation names, or an empty identity when none of its events names
// one: the episode of a record is read from the application boundary and never
// guessed from a session or a trace.
func episodeOfReady(ready []event.Event) event.EpisodeID {
	for _, ev := range ready {
		if ev.EpisodeID != "" {
			return ev.EpisodeID
		}
	}
	return ""
}

// readyEventIDs reports the stored event identities the ready input of one
// invocation carries, in the order the session admitted it.
func readyEventIDs(ready []event.Event) []event.EventID {
	ids := make([]event.EventID, 0, len(ready))
	for _, ev := range ready {
		if ev.Key != "" {
			ids = append(ids, ev.Key)
		}
	}
	return ids
}

// recordEpisode writes the episode record of one invocation's business process
// at the point that process actually starts. The episode identity is read from
// the ready input's own application boundary and never guessed, and no episode
// record is written for an input that names no episode at all.
// - Recording is observation, not execution: a recorder error is never carried into the model decision and never blocks it; the write it stopped is counted as an explicit recording loss instead of being dropped silently.
func (l *Loop) recordEpisode(ctx context.Context, inv Invocation) {
	recorder := l.currentRecorder()
	if recorder == nil {
		return
	}
	episodeID := episodeOfReady(inv.Ready)
	if episodeID == "" {
		return
	}
	if err := recorder.RecordEpisode(ctx, episodeID, inv.Address.SessionID, time.Now()); err != nil {
		l.noteRecordingLoss("episode", string(episodeID), err)
	}
}

// recordDecisionAttempt writes the decision record and the attempt record at
// the point the decision actually produced them: one decision boundary beside
// one observable model attempt, the stored input events the decision reduced,
// the native response the attempt observed and the single terminal the model
// reduction derived. A retry hidden below the decision boundary writes no
// second attempt here, and no terminal is re-derived at this production point.
// - The attempt record is assembled on the record side from the outcome exactly as the reduction produced it: the terminal it records is the one the model reduction derived.
// - A write the recorder refused is counted as an explicit recording loss beside its record identity and never dropped silently.
func (l *Loop) recordDecisionAttempt(ctx context.Context, inv Invocation, decisionID event.DecisionID, attemptID event.AttemptID, inputs []event.EventID, view modelView, outcome model.Outcome) {
	recorder := l.currentRecorder()
	if recorder == nil {
		return
	}
	episodeID := episodeOfReady(inv.Ready)
	now := time.Now()
	if err := recorder.RecordDecision(ctx, decisionID, inv.InvocationID, episodeID, inputs, view.modelName, view.promptVersion, view.visibleTools, now); err != nil {
		l.noteRecordingLoss("decision", string(decisionID), err)
	}
	if err := recorder.RecordAttempt(ctx, attemptID, decisionID, episodeID, outcome); err != nil {
		l.noteRecordingLoss("attempt", string(attemptID), err)
	}
}

// modelView is the model view material one request actually runs on, carried
// across the recording face in the built-in shapes that face crosses: the model
// and prompt version of the borrowed generation and the names of the tools
// visible to the request. A member that ran unversioned is named unversioned
// rather than assumed current: the record side pins each visible member by the
// material it is handed and invents no version of its own.
type modelView struct {
	modelName     string
	promptVersion string
	visibleTools  []string
}

// modelViewOf fixes the model view one request actually runs on before the
// request starts: the model and prompt version of the borrowed generation and
// the tools visible to the request. The view is never rebuilt from the options
// of a finished call.
func modelViewOf(binding state.AgentBinding) modelView {
	view := modelView{
		modelName:     binding.Model.Info().Name,
		promptVersion: binding.PromptSource.Version,
	}
	for _, tool := range binding.Tools {
		if tool == nil || tool.Declaration() == nil {
			continue
		}
		view.visibleTools = append(view.visibleTools, tool.Declaration().Name)
	}
	return view
}

// answerText reports the answer text of a call kept in the shape the call
// returned it in: the tool output byte-identical beside a call that executed
// synchronously, or the named error of a call that never reached execution. The
// text carries no wrapper prefix of its own, so the content the model reads is
// the content the call produced.
func answerText(cause error, answer any) string {
	if cause != nil {
		return cause.Error()
	}
	switch out := answer.(type) {
	case nil:
		return ""
	case string:
		return out
	default:
		return fmt.Sprintf("%v", out)
	}
}

// refusalText reports the native tool error of a call that was refused before
// execution: the named error kept as it stands beside a refusal that carries a
// reason of its own, or the plain refusal beside one that carries none.
func refusalText(cause error) string {
	if cause != nil {
		return cause.Error()
	}
	return "tagent: the call was refused before execution"
}

// admissionText reports the receipt text of one admitted call: the task it was
// registered as beside its running state, and the settlement-then-notify
// promise the model can end its turn on. The text names no status query and no
// waiting step, so it never invites polling, and the delivery promise it keeps
// stays fully dependable.
func admissionText(receipt task.AdmissionReceipt) string {
	return fmt.Sprintf("accepted %s (running): the call runs as a background task and its result will arrive as a notification when it settles", receipt.TaskID)
}

// observationOf turns one settled result into the observation its call enters
// the next decision of the invocation with: the settled body kept
// byte-identical under the call identifier it belongs to, entered as input
// material of the range rather than as a second synchronous tool response under
// the same call identifier. This live observation renders as user input
// material deliberately — the settled body is fresh input of the range, not a
// replayed tool answer — while the replay path renders a settled fact under
// the tool role it originally answered a call with (see RoleOf).
func observationOf(msg model.Message, callID, tool string) model.Message {
	return model.Message{Role: model.RoleUser, ToolID: callID, ToolName: tool, Content: msg.Content}
}

// HistoryFace is the session transcript face the loop reads the accumulated
// history of a session from and writes one ended drive's transcript back to:
// the invocations of one session share the history the drives that already
// ended on it committed, while each drive aggregates only its own ready input
// beside that history and never batches with a neighbour.
//
// The redline list (3 rules):
// MUST report the history the named session accumulated, as a copy the caller owns.
// MUST NOT batch the ready input of one invocation into another invocation's range.
// MUST NOT rewrite or drop the history a committed drive left behind.
type HistoryFace interface {
	// SessionHistory reports the messages the named session accumulated across
	// the drives that already ended on it.
	SessionHistory(addr event.Address) []model.Message
	// CommitHistory appends the transcript one ended drive accumulated to the
	// history of the named session.
	CommitHistory(addr event.Address, msgs []model.Message)
}

// SetHistory attaches the session transcript face this loop reads the shared
// session history from and commits one ended drive's transcript back to. The
// setter is separate from the constructor so the declared construction shape of
// the loop stays as it is.
func (l *Loop) SetHistory(face HistoryFace) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.history = face
}

// sessionHistory reports the transcript the named session accumulated across
// the drives that already ended on it, as a copy the drive owns: a loop beside
// no history face shares no history and keeps every drive at its own input.
func (l *Loop) sessionHistory(addr event.Address) []model.Message {
	l.mu.Lock()
	face := l.history
	l.mu.Unlock()
	if face == nil {
		return nil
	}
	return append([]model.Message(nil), face.SessionHistory(addr)...)
}

// commitTranscript appends the transcript one ended drive accumulated to the
// history of its session, so the next invocation of the session reads the
// range's history as it actually ended. A loop beside no history face commits
// nothing.
func (l *Loop) commitTranscript(addr event.Address, msgs []model.Message) {
	l.mu.Lock()
	face := l.history
	l.mu.Unlock()
	if face == nil || len(msgs) == 0 {
		return
	}
	face.CommitHistory(addr, msgs)
}
