package agent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
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
// observation never reconstructs the fact it observes. A commit that did not
// land reports no identity beside the named failure, so no caller ever answers
// a commitment over a fact the ledger refused.
//
// The redline list (3 rules):
// MUST store the whole event as it stands, minting an identity only for an event that carries none.
// MUST report the identity that fact committed under, so the observation reads that very fact back.
// MUST report a refused commit beside no identity, so the caller decides what a fact that never landed means.
type FactCommit func(evt event.Event) (event.EventID, error)

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

// NoticeView is one notification fact as the session chain's projection carries
// it: the canonical identity the fact committed under beside the input material
// it reads as.
type NoticeView struct {
	// Key is the canonical identity the notification fact committed under.
	Key event.EventID
	// Text is the input material the notification reads as.
	Text string
}

// NoticeFeed is the incremental read face of a session chain's notification
// facts: the boundary the ended material ends at and the notices the chain
// carries from that boundary on. A notification is a fact of the chain, so a
// drive reads the notices of its range as a projection increment rather than
// from a lane of its own; a chain beside no projection says so and the loop
// reads the cache it keeps beside the commit instead. The face crosses built-in
// and event types only, so the composition root attaches it with no package edge
// into the projection layer.
//
// - The boundary is re-resolved by identity each drive, not a kept position: a fold rewrite moves the sequence; remembered identities report each notice once.
//
// The redline list (3 rules):
// MUST report the ended-material boundary and the notices the chain carries from it on, in chain order.
// MUST report a chain that carries no projection as carrying none, never as an empty projection.
// MUST NOT answer an entry outside the identity prefix it was handed.
type NoticeFeed interface {
	// NoticeBoundary reports the position the ended material of the named chain
	// ends at, resolved from the fact identity the boundary records, and whether
	// that chain carries a projection at all.
	NoticeBoundary(chain RequestChain) (int, bool)
	// NoticesFrom reports the notification facts of the named chain from cursor
	// on under the given identity prefix, in chain order, beside the end the read
	// reached and whether the chain carries a projection. The cursor is resolved
	// against the sequence as it stands at the read; it is never a position the
	// caller may keep across a fold rewrite.
	NoticesFrom(chain RequestChain, prefix event.EventID, cursor int) ([]NoticeView, int, bool)
}

// factPipelineName is the stable name a refused fact pipeline assembly is
// reported under, so a fact that committed without its observation is readable
// as a gap by the face that failed to assemble.
const factPipelineName = "fact-pipeline"

// ErrFactPipelineUnwired reports a committed fact the loop could not observe
// because no pipeline face was attached: a gap by name, never a silently
// dropped observation.
var ErrFactPipelineUnwired = errors.New("agent: no fact pipeline is attached to observe through")

// Capture is the narrow trajectory capture face this loop fixes and records
// through: the request view is fixed before the request starts, every observed
// attempt is admitted under the capture's own bound, and the turn seals what it
// held when it ends. The observations cross as the loop observed them — the
// decision identity, the request view material, the reduced outcome — and never
// as a record of another package's shape, so the capture side attaches through
// this face with no package edge into the execution core. The attempt admission
// is the loop's only record-producing point: the episode identity travels on the
// ready input, the decision's model view material is what Snapshot fixed before
// the request started, and the stored input events are the chain the ledger
// keeps, so no second record kind is minted here to repeat what the run holds.
//
// The redline list (4 rules):
// MUST fix the request view before the request starts and never rebuild it from the options of a finished call.
// MUST admit every observed attempt beside the response that produced it and report a refused admission as a counted loss.
// MUST seal the turn once its productions stopped and report the seal failure as a counted loss.
// MUST NOT let a capture failure reach the model decision, and MUST NOT make admission wait on a writer.
type Capture interface {
	// Snapshot fixes the view one decision's request runs on before the request
	// starts: the model name, the prompt version, the tools visible to it and the
	// skills visible to it, beside the request as it stands. The request is
	// observed, never edited.
	Snapshot(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, visibleSkills []string, req model.Request) error
	// Record admits one observed attempt beside the response that produced it:
	// the outcome arrives reduced, and the record side shares that terminal
	// rather than deriving a second one.
	Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error
	// Seal closes the turn's capture and reports the manifest a real writer
	// confirmed; a seal that found records in flight is a named failure, never a
	// completed capture.
	Seal(ctx context.Context) error
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
	// Failure is the raw cause the latest decision's reduction recorded when its
	// terminal is KindFailed, and it stands empty otherwise. It is the text a
	// host delivers when a failed turn produced no deliverable body; a reduction
	// that observed no cause leaves it empty rather than inventing one.
	Failure string
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
// - The watchdog budget of a drive is read at the boundary between its rounds: the round that boundary finds the budget spent is the closing round, and the drive ends when that round returns. The budget is the one the drive's own agent declares, and the loop's default while that agent declares none.
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
	capture   Capture
	commit    FactCommit
	pipelines FactPipelines
	notices   NoticeFeed
	present   PresentationSink
	boundary  RequestFace
	watch     func(taskID event.TaskID, stopped func() bool) error
	final     FinalRoundPolicy
	finals    map[event.AgentID]FinalRoundPolicy
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
// - The drive reads its range's chain again at every decision boundary: input that landed in the range while a decision ran enters the next decision of that same range, and a decision that opened no obligation still ends the round while that input stands unread. The read starts behind the head the chain held when the drive began, so the material an earlier range already took is never this range's input.
// - The transcript of this drive is committed beside the terminal answer of its last decision, so the next invocation of this session reads the range's history as it actually ended.
// - The whole drive is watched as one in-flight production beside its invocation identity — the queueing waits included — and confirms its stop only when the drive really ended.
// - A progress notification of the range's own call commits as a fact of its chain and enters no round head: the report is audit and projection material, never input a decision speaks from.
// - A terminal notification of the range's own call enters the round head as input material: a settle, a failure or a cancellation is what an unattended follow-up turn speaks from, and the progress reports beside it are skipped by their own category.
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
	defer l.sealCapture(ctx)
	defer production.stop()
	l.intakeFor(inv.InvocationID).setAddress(inv.Address)
	l.markRangeHead(inv)
	l.observeReady(ctx, inv)
	l.markConsumed(inv.InvocationID, inv.Ready)
	history := l.sessionHistory(inv.Address)
	ready, err := aggregateReady(inv.Ready)
	if err != nil {
		return turn, err
	}
	transcript := append(append([]model.Message(nil), history...), ready...)
	policy := l.finalRoundFor(inv.Address.AgentID)
	started := time.Now()
	rounds := 0
	var landed []model.Message
	for {
		transcript = append(transcript, landed...)
		transcript = append(transcript, l.terminalNotes(inv)...)
		round := closingRound(policy, rounds, time.Since(started))
		decision, outcome, receipts, observations, decideErr := l.decide(ctx, inv, transcript, round)
		if decideErr != nil {
			return turn, decideErr
		}
		turn.Decisions = append(turn.Decisions, decision)
		turn.Terminal = outcome.Kind
		turn.Failure = ""
		if outcome.Kind == model.KindFailed {
			if cause := outcome.Cause(); cause != nil {
				turn.Failure = strings.TrimSpace(cause.Error())
			}
		}
		next, readErr := l.boundaryInput(ctx, inv)
		if readErr != nil {
			return turn, readErr
		}
		landed = next
		if round.final {
			turn.FinalRound = round.reason
			l.commitTranscript(inv.Address, append(transcript[len(history):], append([]model.Message{assistantOf(outcome)}, receipts...)...))
			if commitErr := l.commitOutput(ctx, inv, decision, outcome); commitErr != nil {
				return l.failCommit(turn, commitErr)
			}
			return turn, nil
		} else if len(receipts) == 0 && len(observations) == 0 && len(landed) == 0 {
			l.commitTranscript(inv.Address, append(transcript[len(history):], assistantOf(outcome)))
			if commitErr := l.commitOutput(ctx, inv, decision, outcome); commitErr != nil {
				return l.failCommit(turn, commitErr)
			}
			return turn, nil
		}
		transcript = append(transcript, assistantOf(outcome))
		transcript = append(transcript, receipts...)
		transcript = append(transcript, observations...)
		rounds++
	}
}

// failCommit reports one drive whose output fact did not commit: the turn ends
// beside the failed terminal and the named cause of the refusal, so a caller
// never reads a completed turn over a fact that never landed.
func (l *Loop) failCommit(turn Turn, cause error) (Turn, error) {
	turn.Terminal = model.KindFailed
	turn.Failure = strings.TrimSpace(cause.Error())
	return turn, fmt.Errorf("tagent: the output fact of the drive did not commit: %w", cause)
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

// consumerWarn renders one line of the consumer face at WARN beside a fact the
// commit face refused: a notice fact and a delivery fact answer no caller, so
// the refused commit is reported here rather than swallowed, and the bypass
// discipline of the consumer face applies to this line too.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed drive.
// MUST carry the named cause beside the identity of the fact family it belongs to.
// MUST NOT render transcript, message or tool-output content.
func consumerWarn(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Warnf(format, args...)
}

// Backflow admits one notification of a call into the range of the invocation
// that issued that call, so it drives that range's next decision and no other. A
// progress notification commits as a notice fact of that range and nothing
// more: the chain is the truth of the report, and a report of the pipeline's
// own rounds is never material a decision reads. A non-terminal notification
// that is not progress carries no result to pair and is ignored here. The
// settled delivery commits as the delivery fact of the range before it is handed
// on, so the delivery boundary of a call is observed as the fact it is; the
// After pass of that fact runs beside the notification rather than under the
// cancellation of the caller that delivered it, because observation is a bypass.
func (l *Loop) Backflow(invocationID event.InvocationID, notice task.Notice) {
	switch notice.Kind {
	case task.NoticeProgress:
		l.commitProgressFact(invocationID, notice)
		return
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

// commitProgressFact commits one progress notification as a notice fact of the
// range that issued the call, and parks nothing beside it: the fact is the audit
// and projection record of the report, and a progress report is never input
// material of a decision — the model is told what the user asked, never what the
// task pipeline is doing in its rounds.
func (l *Loop) commitProgressFact(invocationID event.InvocationID, notice task.Notice) {
	in := l.intakeFor(invocationID)
	if addr := in.rangeAddress(); addr.IsComplete() {
		if _, err := l.commitNotice(invocationID, addr, notice); err != nil {
			consumerWarn("tagent: the notice fact of task %s did not commit: %v", notice.TaskID, err)
		}
	}
}

// commitNotice commits one progress notification as the notice fact of its
// range: the stable identity of the notification beside the material it
// records, owned by the range address and scoped to the invocation that issued
// the call. The fact enters the mapped sequence of the session chain like every
// other committed fact of the range, and it answers no call: the report of
// in-flight work is audit and projection material, never an answer to the call
// and never input a decision speaks from. The notification's own four-tuple
// stays its identity: the fact reference it carries names the work it reports
// and never replaces that identity.
func (l *Loop) commitNotice(invocationID event.InvocationID, addr event.Address, notice task.Notice) (event.EventID, error) {
	return l.commitFactKey(context.Background(), addr.AgentID, event.Event{
		Key:       NoticeFactKey(invocationID, notice),
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyEventSummary: string(notice.Kind),
			event.MetaKeyNoticeKind:   string(notice.Kind),
			event.MetaKeyTaskID:       string(notice.TaskID),
		},
		Message:      progressNoteOf(notice),
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: invocationID,
		TaskID:       notice.TaskID,
		InstanceID:   l.instance,
	})
}

// NoticeFactPrefix reports the identity prefix every notice fact of one range
// carries: the range marker of the fact family, spelled as hex so the identity
// stays a canonical event key. A read face selects the notifications of that
// range alone through it.
func NoticeFactPrefix(invocationID event.InvocationID) event.EventID {
	return event.EventID(hex.EncodeToString([]byte(noticeKeyPrefix(string(invocationID)))))
}

// NoticeFactKey reports the canonical identity one notification commits under:
// the identity prefix of its range beside the notification's own stable
// four-tuple. A retried delivery of one notification keeps that identity, so the
// chain appends the notice once.
func NoticeFactKey(invocationID event.InvocationID, notice task.Notice) event.EventID {
	return event.EventID(string(NoticeFactPrefix(invocationID)) + hex.EncodeToString([]byte(notice.FactKey())))
}

// noticeKeyPrefix composes the range marker of the notice fact family: the
// length of the invocation identity beside the identity itself, so two ranges
// whose identities nest never share a prefix.
func noticeKeyPrefix(invocationID string) string {
	return "notice\x00" + strconv.Itoa(len(invocationID)) + "\x00" + invocationID + "\x00"
}

// observeReady runs the After pass of the ready input of one invocation: every
// ready event carries the identity it committed under at the entry of its
// range, so the turn-start boundary of this drive is observed as the fact the
// run already holds. A ready event that carries no identity names no committed
// fact and is not observed; the drive itself reads the event either way. The
// pass is idempotent per range: a fact one boundary already observed is not
// observed again by a later boundary of the same range.
func (l *Loop) observeReady(ctx context.Context, inv Invocation) {
	for _, ev := range inv.Ready {
		l.observeFact(ctx, inv, ev)
	}
}

// observeFact runs the After pass of one fact of one range, once: the identity
// the fact committed under is remembered by the range, so the fact a boundary
// meets again — the head input of the drive, or the input that landed in flight
// and is read at every boundary from then on — is observed exactly once.
func (l *Loop) observeFact(ctx context.Context, inv Invocation, evt event.Event) {
	if evt.Key == "" {
		return
	}
	in := l.intakeFor(inv.InvocationID)
	in.mu.Lock()
	observed := in.observed[evt.Key]
	in.observed[evt.Key] = true
	in.mu.Unlock()
	if observed {
		return
	}
	l.afterPass(ctx, inv.Address.AgentID, evt.Key)
}

// markConsumed records the keys of one batch of input as material its range has
// taken into the transcript, so the boundary read reports each fact once: the
// identity a fact committed under is the de-duplication, never the content the
// fact reads as.
func (l *Loop) markConsumed(invocationID event.InvocationID, events []event.Event) {
	in := l.intakeFor(invocationID)
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, evt := range events {
		if evt.Key != "" {
			in.read[evt.Key] = true
		}
	}
}

// markRangeHead records where the range of one drive begins on the chain of its session: the identity of the last fact the chain already held when the drive started, remembered by the range's intake. The boundary read of that drive starts behind it, so the material an earlier range committed under the same invocation identity is never read as input this range has not taken yet.
// - The marker is an identity, never a position: the age fold rewrites the sequence the commits record, so a position names other material after a rewrite.
// - The marker is recorded before the drive reads anything, so the head input of the drive itself stands behind it and is never re-read.
// - A chain that held no fact at the start names no head, and the read of that drive starts at the head of the chain as it stands.
func (l *Loop) markRangeHead(inv Invocation) {
	in := l.intakeFor(inv.InvocationID)
	in.mu.Lock()
	defer in.mu.Unlock()
	in.head = ""
	if n := len(inv.Ready); n > 0 {
		in.head = inv.Ready[n-1].Key
	} else if facts := l.sessionFacts(inv.Address); len(facts) > 0 {
		in.head = facts[len(facts)-1].Key
	}
}

// boundaryInput reads the range's own facts again at one decision boundary and answers the messages of the facts the range has not taken yet, in commit order: the input that landed in this range while the decision that just ended was running enters the next decision of that same range.
// - Only the facts of this range are read: a fact of another invocation of the session is that range's input and never this one's.
// - Only the facts that start a range are read: the reasoning, the calls, the output and the notices belong to the range in flight and are carried by the transcript.
// - Only the facts behind the head the drive recorded are read: a fact the chain already held when the drive began belongs to a range that already ran, even when it carries this range's own identity — a restarted process counts its invocation identities from the start again, so the facts of the earlier process are material that already ended.
// - The identity a fact committed under is what de-duplicates it, so the same text sent twice is two facts and stands in the request twice.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func (l *Loop) boundaryInput(ctx context.Context, inv Invocation) ([]model.Message, error) {
	facts := l.sessionFacts(inv.Address)
	if len(facts) == 0 {
		return nil, nil
	}
	fresh := make([]event.Event, 0, len(facts))
	in := l.intakeFor(inv.InvocationID)
	in.mu.Lock()
	for _, evt := range behindRangeHead(facts, in.head) {
		if evt.InvocationID != inv.InvocationID || evt.Key == "" || !startsRange(evt) {
			continue
		}
		if in.read[evt.Key] {
			continue
		}
		in.read[evt.Key] = true
		fresh = append(fresh, evt)
	}
	in.mu.Unlock()
	for _, evt := range fresh {
		l.observeFact(ctx, inv, evt)
	}
	return aggregateReady(fresh)
}

// behindRangeHead reports the facts of one chain that stand behind the head its drive recorded: the fact the head names, and every fact before it, belongs to a range that already ran, so no boundary of this drive reads them. The head names an identity rather than a position because a fold rewrites the sequence the commits record. A chain that does not carry the head is answered whole: the marker names material this read cannot locate, and a chain read as empty would silently drop the input that landed in flight.
func behindRangeHead(facts []event.Event, head event.EventID) []event.Event {
	if head == "" {
		return facts
	}
	for at, evt := range facts {
		if evt.Key == head {
			return facts[at+1:]
		}
	}
	return facts
}

// startsRange reports whether one committed fact starts the range it belongs to:
// the input an invocation aggregates, and the notification an unattended
// follow-up turn commits as the material its drive reads at the head. Every
// other fact — the reasoning, the calls and the output of a round, and the
// notice a running call admits — belongs to the range already in flight and
// never opens one. It is the criterion the session chain moves its
// ended-material boundary by (assembly admitsRange), so a fact of the range in
// flight never moves that boundary either; the loop keeps its own copy because
// the boundary read crosses the transcript face alone, and both copies name the
// same judgement.
func startsRange(evt event.Event) bool {
	if evt.Type == event.TypeExternalInput {
		return true
	}
	return evt.Type == event.TypeFeedback && evt.Meta[event.MetaKeyTriggerSource] == "task"
}

// commitOutput commits the assistant output one ended drive answers with as the
// turn-end fact of the range and runs its After pass: the message the drive
// commits to the transcript of its session beside the decision, invocation and
// address it belongs to, so the turn end is one committed fact of the run and
// not a line derived from the outcome afterwards. A commit the ledger refuses
// is reported to the caller of the drive beside a failed terminal, so a turn
// never answers completed over a fact that never landed.
func (l *Loop) commitOutput(ctx context.Context, inv Invocation, decision Decision, outcome model.Outcome) error {
	return l.commitFact(ctx, inv.Address.AgentID, event.Event{
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

// commitCalls commits the assistant message one decision issued its tool calls
// as the call fact of the range, before the calls it names are admitted: the
// message the model produced beside every call identity, function name and the
// arguments as the model issued them, landed as its own fact of the run. The
// call fact is what the projection restores the pairing a later request replays
// from — beside the result facts the calls settle into, the chain keeps both
// sides of every call-result pair and a restart recovers the pairing from the
// facts alone. A commit the ledger refuses ends the decision beside the named
// error before any call it names is admitted, so a call never executes over a
// fact that never landed.
func (l *Loop) commitCalls(ctx context.Context, inv Invocation, decision Decision, outcome model.Outcome) error {
	return l.commitFact(ctx, inv.Address.AgentID, event.Event{
		Type:         event.TypeThinkingPlan,
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
// no delivery fact: the fact of a delivery names the range it lands in. A
// commit the ledger refuses answers no caller, so it is reported at WARN and
// the delivery itself still settles into its range.
func (l *Loop) commitSettled(ctx context.Context, invocationID event.InvocationID, addr event.Address, notice task.Notice) {
	if addr.AgentID == "" || addr.SessionID == "" {
		return
	}
	if err := l.commitFact(ctx, addr.AgentID, event.Event{
		Type:         event.TypeTaskSettled,
		Timestamp:    time.Now(),
		TaskID:       notice.TaskID,
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: invocationID,
		InstanceID:   l.instance,
	}); err != nil {
		consumerWarn("tagent: the delivery fact of task %s did not commit: %v", notice.TaskID, err)
	}
}

// commitFact commits one fact of the run through the attached commit face and
// runs the After pass of that fact through the pipeline of the named agent. A
// loop beside no commit face commits nothing and observes nothing; a commit
// face beside no pipeline face still commits the fact but leaves its
// observation unnetted, so the missing face is a named gap beside a fact that
// stays committed as it stands. A refused commit is reported to the caller, so
// the loop never carries on as if a fact that never landed had landed.
func (l *Loop) commitFact(ctx context.Context, agentID event.AgentID, evt event.Event) error {
	_, err := l.commitFactKey(ctx, agentID, evt)
	return err
}

// commitFactKey commits one fact of the run through the attached commit face and
// runs the After pass of that fact, and reports the identity the fact committed
// under. An empty identity beside no error reports a loop that committed
// nothing: a loop beside no commit face commits nothing and observes nothing,
// while a commit face beside no pipeline face still commits the fact but leaves
// its observation unnetted, so the missing face is a named gap beside a fact
// that stays committed as it stands. A refused commit reports no identity
// beside the named failure and runs no After pass, because no committed fact
// stands there to observe.
func (l *Loop) commitFactKey(ctx context.Context, agentID event.AgentID, evt event.Event) (event.EventID, error) {
	l.mu.Lock()
	commit, pipelines := l.commit, l.pipelines
	l.mu.Unlock()
	if commit == nil {
		return "", nil
	}
	key, err := commit(evt)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", nil
	}
	if pipelines == nil {
		l.publish(view.Presentation{Fact: key, Gaps: []view.ObservationGap{{
			Plugin: factPipelineName,
			Fact:   key,
			Err:    ErrFactPipelineUnwired,
		}}})
		return key, nil
	}
	l.afterPass(ctx, agentID, key)
	return key, nil
}

// CommitFactFor commits one fact on behalf of the named agent and runs the
// After pass of that fact, and reports the identity the fact committed under.
// It is the same commit the loop's own production points use, exposed so a
// producer outside a dispatch — the configuration source judging a model
// replacement — lands its fact in the one ledger and is observed through the
// faces of that agent's published generation exactly as the loop's own facts
// are. An empty identity beside no error reports a loop beside no commit face,
// and a refused commit reports the failure beside no identity.
func (l *Loop) CommitFactFor(ctx context.Context, agentID event.AgentID, evt event.Event) (event.EventID, error) {
	return l.commitFactKey(ctx, agentID, evt)
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
	presentation := pipeline.After(ctx, key)
	l.publish(presentation)
	consumerDebug("tagent: consumer after-pass agent=%s fact=%s gaps=%d", agentID, key, len(presentation.Gaps))
}

// SetCapture attaches the trajectory capture face this loop fixes each
// decision's request view through, admits each observed attempt through and
// seals the turn through. A nil face captures nothing. A refused fix, record
// or seal is a counted recording loss and never reaches the model decision, and
// the setter is separate from the constructor so the declared construction
// shape of the loop stays as it is.
func (l *Loop) SetCapture(capture Capture) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.capture = capture
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

// SetNoticeFeed attaches the incremental read face the drive reads the progress
// notifications of its range through: the notices are facts of the session
// chain, so the read is a projection increment seeded at the ended-material
// boundary. A nil face keeps the read at the cache the loop keeps beside each
// commit. The setter is separate from the constructor so the declared
// construction shape of the loop stays as it is.
func (l *Loop) SetNoticeFeed(feed NoticeFeed) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notices = feed
}

// noticeReader reports the attached notice read face, or nil while none is
// attached.
func (l *Loop) noticeReader() NoticeFeed {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notices
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

// fixCapture fixes the request view of one decision through the attached
// capture before that request starts, or fixes nothing while no capture is
// attached. A fix the capture refused is counted as an explicit recording loss
// beside its decision identity and never carried into the model decision.
func (l *Loop) fixCapture(ctx context.Context, decisionID event.DecisionID, view modelView, req model.Request) {
	l.mu.Lock()
	capture := l.capture
	l.mu.Unlock()
	if capture == nil {
		return
	}
	if err := capture.Snapshot(ctx, decisionID, view.modelName, view.promptVersion, view.visibleTools, view.visibleSkills, req); err != nil {
		l.noteRecordingLoss("capture-snapshot", string(decisionID), err)
	}
}

// recordCaptureAttempt admits one observed attempt through the attached capture
// beside the response that produced it, or admits nothing while no capture is
// attached. A refused admission is counted as an explicit recording loss beside
// its attempt identity and never carried into the model decision.
func (l *Loop) recordCaptureAttempt(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) {
	l.mu.Lock()
	capture := l.capture
	l.mu.Unlock()
	if capture == nil {
		return
	}
	if err := capture.Record(ctx, attemptID, decisionID, episodeID, outcome); err != nil {
		l.noteRecordingLoss("capture-attempt", string(attemptID), err)
	}
}

// recordAttempt admits one observed attempt of a decision through the attached
// capture beside the response that produced it. It is the decision's one
// record-producing point: the episode identity is read from the ready input's
// own application boundary and never guessed, the response is the native one the
// attempt observed and the terminal is the one the reduction derived. A refused
// admission is counted as a recording loss and never carried into the model
// decision.
func (l *Loop) recordAttempt(ctx context.Context, inv Invocation, decisionID event.DecisionID, attemptID event.AttemptID, outcome model.Outcome) {
	l.recordCaptureAttempt(ctx, attemptID, decisionID, episodeOfReady(inv.Ready), outcome)
}

// sealCapture seals the capture of the turn the drive served, or seals nothing
// while no capture is attached. The seal runs once the drive's productions
// stopped, so it accounts exactly what the turn admitted; a refused seal is
// counted as an explicit recording loss and never as a completed capture.
func (l *Loop) sealCapture(ctx context.Context) {
	l.mu.Lock()
	capture := l.capture
	l.mu.Unlock()
	if capture == nil {
		return
	}
	if err := capture.Seal(ctx); err != nil {
		l.noteRecordingLoss("capture-seal", "", err)
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
	// "decision" or "attempt".
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
// - The safe request snapshot is fixed through the attached capture before the model entry is called and observes the request without editing it; a refused fix is counted as a recording loss.
// - The model entry error is presented with failure polarity through the same reduction: a synthesized failure response is reduced like any stream, so the terminal judgement stays in the model package alone.
// - The model decision reached its terminal: the session slot is free again while the admitted tools run as background tasks.
// - The handle is released once the work the decision set in motion has ended, never because a newer generation was published meanwhile.
// - The model production of one decision is watched beside its invocation identity and confirms its stop when the reduction really ended.
// - The closing round marks its request on the boundary, carries the closing instruction after the transcript, runs under the short closing timeout and admits none of the calls it issues.
// - The environment block the boundary rendered trails the request as its last message, behind the transcript and the closing instruction, so the dynamic material never enters the prefix.
func (l *Loop) decide(ctx context.Context, inv Invocation, transcript []model.Message, round decisionRound) (Decision, model.Outcome, []model.Message, []model.Message, error) {
	if round.final {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, round.timeout)
		defer cancel()
	}
	decisionID := l.nextDecisionID(inv)
	attemptID := event.AttemptID(fmt.Sprintf("attempt-%s", decisionID))
	if err := l.occupy(ctx, inv, decisionID); err != nil {
		return Decision{}, model.Outcome{}, nil, nil, err
	}
	handle, err := l.borrow.Borrow(ctx, inv.Address.AgentID)
	if err != nil {
		_ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID)
		return Decision{}, model.Outcome{}, nil, nil, err
	}

	tools, toolOrder := state.ToolFace(handle.Binding)
	callable := make(map[string]trpctool.CallableTool, len(tools))
	for name, tool := range tools {
		if native, ok := tool.(trpctool.CallableTool); ok {
			callable[name] = native
		}
	}
	view := modelViewOf(handle.Binding)
	boundary, err := l.requestBoundary(ctx, inv, handle.Binding, round.final)
	if err != nil {
		_ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID)
		_ = handle.Release(ctx)
		return Decision{}, model.Outcome{}, nil, nil, err
	}
	if boundary.PromptVersion != "" {
		view.promptVersion = boundary.PromptVersion
	}
	view.visibleSkills = boundary.VisibleSkills
	messages := make([]model.Message, 0, len(boundary.Messages)+len(transcript)+2)
	messages = append(messages, boundary.Messages...)
	messages = append(messages, transcript...)
	if round.final {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: round.prompt})
	}
	if boundary.DynamicSystem != "" {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: boundary.DynamicSystem})
	}
	req := model.Request{Messages: messages, Tools: tools, ToolOrder: toolOrder}
	l.fixCapture(ctx, decisionID, view, req)

	production := &inFlightProducer{}
	l.watchProducer(event.TaskID(inv.InvocationID), production.Stopped)
	stream, entryErr := handle.Binding.Model.GenerateContent(ctx, &req)
	if entryErr != nil {
		stream = failedStream(entryErr)
	}
	outcome := model.ReduceChannel(ctx, stream, nil)
	production.stop()
	l.recordAttempt(ctx, inv, decisionID, attemptID, outcome)
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
	if outcome.Kind == model.KindCompleted && outcome.Body != nil && outcome.Body.IsToolCallResponse() {
		if round.final {
			receipts = refuseClosingCalls(callsOf(outcome))
		} else {
			if commitErr := l.commitCalls(ctx, inv, decision, outcome); commitErr != nil {
				_ = handle.Release(ctx)
				return decision, outcome, nil, nil, commitErr
			}
			receipts, observations, err = l.dispatch(ctx, inv, decision, callsOf(outcome), callable)
			if err != nil {
				_ = handle.Release(ctx)
				return decision, outcome, nil, nil, err
			}
		}
	}
	_ = handle.Release(ctx)
	return decision, outcome, receipts, observations, nil
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
// - The pipeline is assembled from the instances of that generation: every declared port dispatches its instance exactly as NewFlow dispatches it, so the loop and the plugin set share one dispatch and no second assembly arises here.
// - The one Before pass travels on the context into the admission ruling beside the call frame, so the pre-execution ruling consumes the risk grade its risk faces rendered and never runs a second risk face of its own.
// - The transformed final call is the one both executed and recorded; the identity it is admitted under stays the resolved CallID.
// - The receipt is the synchronous answer of the call: the model reads the minimal confirmation that the call happened, the task identities stay out of the request, and the settled result never answers the call again.
// - Every call of this decision is already admitted and answered with its receipt before the first wait for a settled result begins.
// - The settled result fact is the stored event the next decision of this range reduces; a call that recorded no fact contributes none.
// - The After pass reads only a fact whose canonical commit already happened: a failed observation is a visible gap in the returned presentation and never rolls the committed fact back, and a dropped presentation drops the presentation only. The presentation is the read-out the inspection face consumes; the loop keeps the committed fact itself.
func (l *Loop) dispatch(ctx context.Context, inv Invocation, decision Decision, calls []model.ToolCall, tools map[string]trpctool.CallableTool) ([]model.Message, []model.Message, error) {
	pipeline, err := plugin.NewFlow(decision.Binding.Plugins)
	if err != nil {
		return nil, nil, err
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
			consumerDebug("tagent: consumer tool refused invocation=%s call=%s tool=%s by=plugin-pass", inv.InvocationID, call.ID, call.Function.Name)
			receipts = append(receipts, paired(call.ID, call.Function.Name, "tagent: the plugin pass refused the call: "+beforeErr.Error()))
			continue
		}
		final := pass.Final
		name := final.Function.Name
		native, ok := tools[name]
		if !ok {
			consumerDebug("tagent: consumer tool refused invocation=%s call=%s tool=%s by=unknown-tool", inv.InvocationID, call.ID, name)
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
		answer, callErr := native.Call(task.WithCallFrame(plugin.WithCallPass(ctx, pass), frame), final.Function.Arguments)
		switch out := answer.(type) {
		case task.AdmissionReceipt:
			if !out.Accepted {
				consumerDebug("tagent: consumer tool refused invocation=%s call=%s tool=%s by=admission", inv.InvocationID, call.ID, name)
				receipts = append(receipts, paired(call.ID, name, refusalText(callErr)))
				continue
			}
			consumerDebug("tagent: consumer tool admitted invocation=%s call=%s tool=%s task=%s", inv.InvocationID, call.ID, name, out.TaskID)
			receipts = append(receipts, paired(call.ID, name, admissionText(out)))
			running = append(running, admittedCall{callID: call.ID, tool: name, taskID: out.TaskID})
		case task.HoldReceipt:
			receipts = append(receipts, paired(call.ID, name, "tagent: the call is parked awaiting approval"))
		default:
			receipts = append(receipts, paired(call.ID, name, answerText(callErr, out)))
		}
	}
	observations := make([]model.Message, 0, len(running))
	for _, adm := range running {
		arrival, ok := in.await(ctx, adm.taskID)
		if !ok {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			observations = append(observations, observationOf(model.Message{Content: "tagent: the tool result did not arrive before the wait ended"}, adm.callID, adm.tool))
			continue
		}
		observations = append(observations, observationOf(arrival.msg, adm.callID, adm.tool))
		if arrival.fact != "" {
			l.publish(pipeline.After(ctx, arrival.fact))
		}
	}
	return receipts, observations, nil
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
// the cache of the settled tool messages that arrived before their wait began,
// the waits of the calls still running, the facts the range has already taken
// into its transcript and the facts it has already observed.
//
// The intake holds no notification lane of its own: a notification is a fact of
// the session chain, committed before it is admitted here, and a drive reads the
// notices of its range as a projection increment. The settled cache is the same
// shape for the delivery facts of the calls a decision admitted: a settled
// result is committed as the delivery fact of its range before it lands here,
// and the wait of its call pairs it through this cache. A notification of one
// invocation never reaches the intake of another.
// - The read set names the facts standing in this range's transcript: the head input of its drive, the notices it read, and the input a decision boundary reported.
// - The head names the fact the chain held when this range's drive began: the boundary reads of that drive start behind it, so the material an earlier range committed under this range's identity is never read again.
// - The observed set names the facts whose After pass this loop already ran, so neither set is exported and a boundary that meets the same fact again doubles nothing.
type intake struct {
	mu       sync.Mutex
	address  event.Address
	head     event.EventID
	waiters  map[event.TaskID][]chan delivered
	settled  map[event.TaskID][]delivered
	read     map[event.EventID]bool
	observed map[event.EventID]bool
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
		waiters:  map[event.TaskID][]chan delivered{},
		settled:  map[event.TaskID][]delivered{},
		read:     map[event.EventID]bool{},
		observed: map[event.EventID]bool{},
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
	in.settled[taskID] = append(in.settled[taskID], item)
}

// terminalNotes reads the terminal notifications of one invocation as input
// material, one user message per notification: a settle, failure or
// cancellation of the range's own call is the material an unattended follow-up
// turn speaks from, so the drive reads it beside the transcript. A progress
// report of the pipeline's own rounds is never material a decision speaks from:
// the report is a fact of the chain the audit and the projection read, so this
// read skips it. No producer commits progress material today, so the skip is a
// category guard for the shape a future producer would use; it keys on the round
// the notice carries rather than on the category (see pipelineProgress).
// A notification is a fact of the chain, so a read beside a chain is a
// projection increment from the ended-material boundary of the session; every
// read re-resolves that boundary from the fact identity the chain records
// instead of keeping the position an earlier read reached: the age fold rewrites
// the sequence the commits record, so a kept position names other material after
// a rewrite or falls past the shortened sequence. The identities a read reported
// are the state this range keeps, so reading from the boundary again reports
// each notification once.
func (l *Loop) terminalNotes(inv Invocation) []model.Message {
	in := l.intakeFor(inv.InvocationID)
	prefix := NoticeFactPrefix(inv.InvocationID)
	var views []NoticeView
	if feed := l.noticeReader(); feed != nil {
		chain := RequestChain{SessionID: inv.Address.SessionID}
		if boundary, held := feed.NoticeBoundary(chain); held {
			notices, _, _ := feed.NoticesFrom(chain, prefix, boundary)
			for _, view := range notices {
				if pipelineProgress(view.Key, prefix) {
					continue
				}
				views = append(views, view)
			}
		}
	}
	if len(views) == 0 {
		return nil
	}
	notes := make([]model.Message, 0, len(views))
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, view := range views {
		if view.Key != "" {
			if in.read[view.Key] {
				continue
			}
			in.read[view.Key] = true
		}
		notes = append(notes, model.Message{Role: model.RoleUser, Content: view.Text})
	}
	return notes
}

// noticeFields decodes the canonical identity one notice fact was committed
// under: NoticeFactKey spells the range prefix beside the hex of the notice's
// own four-tuple (task:round:kind:seq), so the identity is recovered from the
// key rather than guessed from the material the notice reads as.
func noticeFields(key event.EventID, prefix event.EventID) ([]string, bool) {
	spelled := string(key)
	if !strings.HasPrefix(spelled, string(prefix)) {
		return nil, false
	}
	raw, err := hex.DecodeString(spelled[len(prefix):])
	if err != nil {
		return nil, false
	}
	fields := strings.SplitN(string(raw), ":", 4)
	if len(fields) < 3 {
		return nil, false
	}
	return fields, true
}

// noticeKindOf reports the category one notice fact was committed under, read
// from the canonical identity it carries rather than guessed from the material
// the notice reads as.
func noticeKindOf(key event.EventID, prefix event.EventID) task.NoticeKind {
	fields, ok := noticeFields(key, prefix)
	if !ok {
		return ""
	}
	return task.NoticeKind(fields[2])
}

// pipelineProgress reports whether one notice fact is a progress report of the
// task pipeline's own rounds: the category says so and the notice names the
// execution round it reports in.
//
// The round test is a category guard, not progress semantics: no producer emits
// pipeline progress, so the Round>0 branch is exercised by tests only, while a
// zero-round fact keeps the material. An execution-resource observation is not a
// producer here: host.observeExecState records an audit fact with no notice keys,
// so the progress category carries no execution state. A malformed round keeps
// the material. The zero-round shape is pinned in TestQuietProgressNotice's
// zero_round_progress_is_material case; change this criterion only beside it.
func pipelineProgress(key event.EventID, prefix event.EventID) bool {
	if noticeKindOf(key, prefix) != task.NoticeProgress {
		return false
	}
	fields, ok := noticeFields(key, prefix)
	if !ok {
		return false
	}
	round, err := strconv.ParseInt(fields[1], 10, 64)
	return err == nil && round > 0
}

// noticeText renders the input material one progress notification reads as: the
// task it reports and the round it reports in.
func noticeText(notice task.Notice) string {
	return fmt.Sprintf("tagent: task %s reports progress in round %d", notice.TaskID, notice.Round)
}

// progressNoteOf renders one progress notification as the input material its
// range reads: a user message naming the task and the round it reports in, with
// no tool identity, because the report of in-flight work answers no call.
func progressNoteOf(notice task.Notice) model.Message {
	return model.Message{Role: model.RoleUser, Content: noticeText(notice)}
}

// await waits for the settled tool result of one task and reports whether one
// arrived before the context ended. The wait pairs the result with the call the
// decision that admitted it is about to reduce: the settled result is committed
// as the delivery fact of the range before it lands in this cache, so the lane is
// the cache of that fact — kept for the synchronous pairing a decision needs, and
// never a second truth beside the chain.
func (in *intake) await(ctx context.Context, taskID event.TaskID) (delivered, bool) {
	in.mu.Lock()
	if pending := in.settled[taskID]; len(pending) > 0 {
		item := pending[0]
		in.settled[taskID] = pending[1:]
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
// message carrying no role of its own is filled from the frozen event type, and
// an event type the frozen table does not declare refuses the aggregation by
// name instead of rendering under a fallback role.
func aggregateReady(ready []event.Event) ([]model.Message, error) {
	msgs := make([]model.Message, 0, len(ready))
	for _, ev := range ready {
		msg := ev.Message
		if msg.Role == "" {
			role, err := RoleOf(ev.Type)
			if err != nil {
				return nil, err
			}
			msg.Role = role
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
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

// modelView is the model view material one request actually runs on, carried
// across the capture face in the built-in shapes that face crosses: the model
// and prompt version of the borrowed generation, the names of the tools visible
// to the request and the names of the skills visible to it. A member that ran
// unversioned is named unversioned rather than assumed current: the record side
// pins each visible member by the material it is handed and invents no version
// of its own.
type modelView struct {
	modelName     string
	promptVersion string
	visibleTools  []string
	visibleSkills []string
}

// modelViewOf fixes the model view one request actually runs on before the
// request starts: the model and prompt version of the borrowed generation and
// the tools visible to the request. The skills visible to it are read off the
// request boundary view beside this, because the boundary assembly is the one
// home of the rule naming which skills a request makes visible. The view is
// never rebuilt from the options of a finished call.
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
	case task.AdmissionReceipt:
		return out.Text()
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

// admissionReceiptText is the whole content of one admission receipt: the
// minimal confirmation that the call happened. Nothing else of the admission
// travels to the model — not the task identity, not the running state, not the
// settlement promise. An admission is the system's own confirmation, and a
// system confirmation is never speaking material: the task id, the state word
// and the notification promise are exactly the fragments a model restates as
// an extra acknowledgement, so the confirmation is cut to this constant and
// the task keeps its own record of what it was admitted as.
const admissionReceiptText = task.AdmissionReceiptText

// admissionText reports the receipt text of one admitted call as that minimal
// confirmation. The answer still pairs with the call under the tool role and
// the call id, so the model can tell the call was admitted and that its result
// arrives later. The wording a model must act on stays outside this function:
// the approval hold keeps its "parked awaiting approval" and a refusal keeps
// its named reason.
func admissionText(receipt task.AdmissionReceipt) string {
	return receipt.Text()
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
// beside that history and never batches with a neighbour. A session whose
// transcript a mapped sequence carries needs no second face here: that
// sequence is the one history of the session, so an implementation may answer
// none of its own beside it — and the facts of that chain are what the drive
// reads its range's input again from at every decision boundary.
//
// The redline list (4 rules):
// MUST report the history the named session accumulated, as a copy the caller owns.
// MUST report the facts of the named session in commit order, as a copy the caller owns.
// MUST NOT batch the ready input of one invocation into another invocation's range.
// MUST NOT rewrite or drop the history a committed drive left behind.
type HistoryFace interface {
	// SessionHistory reports the messages the named session accumulated across
	// the drives that already ended on it.
	SessionHistory(addr event.Address) []model.Message
	// CommitHistory appends the transcript one ended drive accumulated to the
	// history of the named session.
	CommitHistory(addr event.Address, msgs []model.Message)
	// SessionFacts reports the facts the named session committed, in commit
	// order: the chain a drive reads the input that landed in its own range
	// from at a decision boundary rather than at the start of the drive alone.
	// The signature is the one the run's own chain read carries, so the drive
	// reads the very ledger it commits through instead of a lane beside it.
	SessionFacts(addr event.Address) []event.Event
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

// sessionFacts reports the facts the named session committed, as the chain read
// answers them: a loop beside no history face reads no chain and keeps every
// drive at the input it was handed.
func (l *Loop) sessionFacts(addr event.Address) []event.Event {
	l.mu.Lock()
	face := l.history
	l.mu.Unlock()
	if face == nil {
		return nil
	}
	return face.SessionFacts(addr)
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
