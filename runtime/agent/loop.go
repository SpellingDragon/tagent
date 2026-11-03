package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Turn is the outcome one invocation's drive reports: the number of decisions
// it ran and the terminal its latest decision reduced to.
type Turn struct {
	// InvocationID is the range the turn belongs to.
	InvocationID event.InvocationID
	// DecisionCount is the number of model decisions the turn ran.
	DecisionCount int
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

	mu      sync.Mutex
	seq     uint64
	intakes map[event.InvocationID]*intake
	// ended names the ranges whose intake this loop may recycle, oldest first: a
	// range mints its intake once and a resident run mints one range per segment, so
	// the bookkeeping of the least recently ended range leaves the map once
	// intakeWindow ended ranges stand in it.
	ended     []event.InvocationID
	history   HistoryFace
	capture   Capture
	commit    FactCommit
	pipelines FactPipelines
	notices   NoticeFeed
	present   PresentationSink
	boundary  RequestFace
	// *writers is the ledger the close of this agent reads the real stop of every
	// production of this loop through: the loop builds it for itself, so the
	// registration stays private to the productions it names while a close reads
	// it through the loop's own Begin and Stopped.
	*writers
	final  FinalRoundPolicy
	finals map[event.AgentID]FinalRoundPolicy
	losses []RecordingLoss
	// consumers is the loop's only Consumer set. Once assigned it is never replaced;
	// its own stopped bit is the permanent terminal marker.
	consumers *Consumers
	// instances names the one consumption instance of each address this loop drives,
	// whoever mounted it. Uniqueness is read here rather than from a set's own map, so
	// a loop that carries two sets still drives one instance per address.
	instances map[event.Address]*Consumer
	flight    map[event.Address]modelFlight
	// fuses holds the execution-side storm fuses this loop admits under: the
	// per-response call cap and the long-context threshold a drive declares for
	// itself replaces. A member at zero is its fuse off, so a loop beside no
	// declaration admits exactly what it admitted before the fuses existed.
	fuses FuseLimits
	// book is the approval face a fuse parks a batch through. A loop beside no
	// book refuses a parked batch whole instead of truncating it, so a storm is
	// never cut down to a limit nobody approved.
	book ApprovalBook
}

// NewLoop returns the event loop over the state owner's borrowing face, the
// session decision-slot ledger and the fact resolver that turns a recorded
// result fact into the native tool message it carries.
func NewLoop(borrow state.Borrow, slots *state.Occupancy, instance event.InstanceID, facts FactResolver) *Loop {
	return &Loop{
		borrow:    borrow,
		slots:     slots,
		instance:  instance,
		facts:     facts,
		intakes:   map[event.InvocationID]*intake{},
		instances: map[event.Address]*Consumer{},
		flight:    map[event.Address]modelFlight{},
		fuses:     DefaultFuseLimits(),
		book:      DefaultApprovalBook(),
		writers:   newWriters(),
	}
}

// drive runs one explicit invocation on the Consumer of its address: the
// invocation's ready facts are handed to that consumer as the segment it opens
// on, the consumer flies the segment, and drive answers the turn the segment
// closed on. The loop keeps no second decision, harvest or seal path: every
// round, every paired result and every sealed turn belong to the Consumer, so
// this face is a submission beside a wait and nothing else.
// - The consumer is assembled for this invocation and never mounted, so no wake opens a second segment beside the one this call flies.
// - The turn is the closed segment's own immutable result, never a value re-derived from the chain after the segment ended.
func (l *Loop) drive(ctx context.Context, inv Invocation) (Turn, error) {
	consumer := newConsumer(inv.Address, l, nil)
	consumer.explicit = true
	return consumer.driveInvocation(ctx, inv)
}

// consumerDebug renders one line of the consumer face at DEBUG: the drive's start
// and end beside each decision the drive consumed. The consumer is the turn driver
// of one invocation, and a line carries identities, counts and the terminal, never
// transcript content.
//
// logSwapLock guards every access to the framework logger beside a running
// consumer: the observation tests swap the default while consumer goroutines
// read it, and an unsynchronized pair is a data race the detector names.
var logSwapLock sync.RWMutex

// frameworkLogger reports the framework logger under the swap lock.
func frameworkLogger() trpclog.Logger {
	logSwapLock.RLock()
	defer logSwapLock.RUnlock()
	return trpclog.Default
}

// swapFrameworkLogger points the framework logger at the observer and answers
// the restore that puts the default back, both under the swap lock.
func swapFrameworkLogger(observer trpclog.Logger) func() {
	logSwapLock.Lock()
	defer logSwapLock.Unlock()
	original := trpclog.Default
	trpclog.Default = observer
	return func() {
		logSwapLock.Lock()
		defer logSwapLock.Unlock()
		trpclog.Default = original
	}
}

// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed drive.
// MUST keep the INFO stream bounded by leaving every consumer line at DEBUG.
// MUST NOT render transcript, message or tool-output content.
func consumerDebug(format string, args ...any) {
	logger := frameworkLogger()
	if logger == nil {
		return
	}
	defer func() { _ = recover() }()
	logger.Debugf(format, args...)
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
	logger := frameworkLogger()
	if logger == nil {
		return
	}
	defer func() { _ = recover() }()
	logger.Warnf(format, args...)
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
//
// The committed delivery fact is named to the consumer of the range beside the
// wake: a result whose segment already closed stands behind the answer that
// segment landed, so no position of the chain opens a segment on it by itself,
// and the wake that names it is the account that says this fact still drives.
// The observation the result carries rides on the fact, so the segment the name
// opens reads the very result the notification delivered instead of a second
// reading of it.
//
// - The wake names the fact only while the task is not awaited by a flying segment and not already consumed: an awaited result is paired by that segment's harvest, and a consumed one drives nothing, so neither is read twice.
// - A loop that drives no set keeps its backflow parked in the intake; a set that carries no instance for the address wakes nothing and starts none.
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
	addr := in.rangeAddress()
	delivery := msg
	if l.resultFactOnChain(addr, notice.Event) {
		delivery = model.Message{Role: model.RoleTool}
	}
	key, commitErr := l.commitSettled(context.Background(), invocationID, addr, notice, delivery)
	if commitErr != nil {
		consumerWarn("tagent: the delivery fact of task %s did not commit: %v", notice.TaskID, commitErr)
	}
	in.deliver(notice.TaskID, delivered{msg: msg, fact: notice.Event})
	if consumers := l.drivingSet(); consumers != nil {
		consumers.wakeFact(addr, key)
	}
}

// WakeAddress ensures the resident Consumer of one address and signals that facts
// arrived. A committed body never needs a separate drive, whether its target was busy,
// idle, or already bound without a mounted Consumer.
func (l *Loop) WakeAddress(addr event.Address) *Consumer {
	if l == nil {
		return nil
	}
	consumer := l.residentOf().Ensure(addr)
	if consumer == nil {
		return nil
	}
	consumer.Wake()
	return consumer
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

// observeFact runs the After pass of one fact of one range, once: the identity
// the fact committed under is remembered by the range, so the fact a boundary
// meets again — the head input of the drive, or the input that landed in flight
// and is read at every boundary from then on — is observed exactly once.
//
// - The trigger of a range is the first source stamp observed before it takes a decision; a fact that lands in flight never rewrites it.
func (l *Loop) observeFact(ctx context.Context, inv Invocation, evt event.Event) {
	if evt.Key == "" {
		return
	}
	in := l.intakeFor(inv.InvocationID)
	in.mu.Lock()
	if in.decisions == 0 && in.trigger == "" {
		in.trigger = evt.Meta[event.MetaKeyTriggerSource]
	}
	observed := in.observed[evt.Key]
	in.observed[evt.Key] = true
	in.mu.Unlock()
	if observed {
		return
	}
	l.afterPass(ctx, inv.Address.AgentID, evt.Key)
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
//
// - The instruction and material facts of a delegated batch start no range; only the task that closes the batch does, so a batch whose task never landed drives nothing.
func startsRange(evt event.Event) bool {
	if evt.Type == event.TypeExternalInput {
		switch evt.Meta[delegatedPartMeta] {
		case delegatedPartInstruction, delegatedPartMaterial:
			return false
		}
		return true
	}
	return evt.Type == event.TypeFeedback && evt.Meta[event.MetaKeyTriggerSource] == "task"
}

// delegatedPartMeta, delegatedPartInstruction, delegatedPartMaterial,
// delegatedPartTask and delegatedSourceMeta stamp the parts of one delegated
// input batch: the instruction the round runs under, one projection per material
// the call handed over, and the task that closes the batch. The instruction and
// the material projections are the material of the range the task opens, and
// only the task starts it. The source key of one material rides its fact's meta
// as lineage — the material is the projection of that committed source event,
// never the source's identity reused as a key of the site's chain.
const (
	delegatedPartMeta        = "delegated_part"
	delegatedPartInstruction = "instruction"
	delegatedPartMaterial    = "material"
	delegatedPartTask        = "task"
	delegatedSourceMeta      = "delegated_source"
)

// delegatedSiteStamp renders the stable identity of one execution site inside
// the keys its instruction and material projections commit under: the complete
// address, never a stamp of the round itself, so two sites that share a caller
// session never share a projection key while a retried round of one site lands
// no second fact under the key its instruction already names.
func delegatedSiteStamp(site event.Address) string {
	return string(site.AgentID) + "@" + string(site.SessionID)
}

// outputMetaTerminal, outputMetaFailure, outputMetaFinalRound, outputMetaTrigger
// and outputMetaDecisionCount name the entries one agent_output records its closed
// turn under: the terminal the latest decision reduced to, the raw cause of a
// failed turn, the reason the closing round ran, the trigger lineage the range
// opened on, and the number of decisions the turn ran. They stand beside the input
// relation and never replace it, so a reader rebuilds the whole closed turn from
// the chain.
const (
	outputMetaTerminal      = "terminal"
	outputMetaFailure       = "failure"
	outputMetaFinalRound    = "final_round"
	outputMetaTrigger       = "trigger"
	outputMetaDecisionCount = "decision_count"
)

// commitOutput commits the assistant output one ended drive answers with as the
// turn-end fact of the range and runs its After pass: the message the drive
// commits to the transcript of its session beside the decision, invocation and
// address it belongs to, so the turn end is one committed fact of the run and
// not a line derived from the outcome afterwards. The fact records the stable
// fields of the closed turn beside the input relation it consumed — the
// terminal, the failure cause, the closing-round reason, the trigger lineage and
// the decision count — so a reader of the chain rebuilds the turn itself. A
// commit the ledger refuses is reported to the caller of the drive beside a
// failed terminal, so a turn never answers completed over a fact that never
// landed.
func (l *Loop) commitOutput(ctx context.Context, inv Invocation, decision Decision, outcome model.Outcome, inputs []event.EventID) error {
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("tagent: encode the output's input relations: %w", err)
	}
	failure := ""
	if outcome.Kind == model.KindFailed {
		if cause := outcome.Cause(); cause != nil {
			failure = strings.TrimSpace(cause.Error())
		}
	}
	decisionCount, finalRound, trigger := l.closingTurnOf(inv.InvocationID)
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
		Meta: map[string]string{
			event.MetaKeyInputEvents: string(encoded),
			outputMetaTerminal:       outcome.Kind.String(),
			outputMetaFailure:        failure,
			outputMetaFinalRound:     string(finalRound),
			outputMetaTrigger:        trigger,
			outputMetaDecisionCount:  strconv.Itoa(decisionCount),
		},
	})
}

// closingTurnOf reports the stable closing fields of one range's turn: the
// number of decisions it ran, the reason its closing round ran, and the trigger
// lineage its opening material carried.
func (l *Loop) closingTurnOf(invocationID event.InvocationID) (decisions int, finalRound FinalRoundReason, trigger string) {
	in := l.intakeFor(invocationID)
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.decisions, in.finalRound, in.trigger
}

// inputKeysOf returns the stable identities of the facts a direct drive consumed,
// in their original order and without duplicates.
func inputKeysOf(events []event.Event) []event.EventID {
	keys := make([]event.EventID, 0, len(events))
	seen := map[event.EventID]bool{}
	for _, evt := range events {
		if evt.Key == "" || seen[evt.Key] {
			continue
		}
		seen[evt.Key] = true
		keys = append(keys, evt.Key)
	}
	return keys
}

// appendInputKeys appends only identities not already consumed, preserving the
// order in which the facts actually entered the drive.
func appendInputKeys(keys []event.EventID, additions []event.EventID) []event.EventID {
	seen := make(map[event.EventID]bool, len(keys))
	for _, key := range keys {
		seen[key] = true
	}
	for _, key := range additions {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
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

// resultFactOnChain reports whether the chain of one address already carries the
// result fact a settled notification points at. The result fact is the one text
// of the tool result — the delivery fact names the delivery and never a second
// copy of the body — so a delivery that finds it on the chain commits the fact
// of the delivery alone and leaves the observation to the result fact the pull
// reads.
func (l *Loop) resultFactOnChain(addr event.Address, key event.EventID) bool {
	if key == "" {
		return false
	}
	for _, evt := range l.sessionFacts(addr) {
		if evt.Key == key {
			return true
		}
	}
	return false
}

// commitSettled commits one settled notification as the delivery fact of the
// range it settles into, beside the task it belongs to and the address the
// notification was accepted at, and answers the identity it landed under. The
// observation the notification delivered rides on the fact, so a segment that
// opens on that fact reads the result itself; a notification that carries no
// observation commits the fact of the delivery alone. A notification that
// arrived before the range recorded its own address commits no fact: the fact
// of a delivery names the range it lands in. A refused commit answers no
// identity beside the named cause.
//
// - The delivery fact carries the observation only when the chain holds no result fact to read it from, so the history reads each result once.
func (l *Loop) commitSettled(ctx context.Context, invocationID event.InvocationID, addr event.Address, notice task.Notice, msg model.Message) (event.EventID, error) {
	if addr.AgentID == "" || addr.SessionID == "" {
		return "", nil
	}
	evt := event.Event{
		Type:         event.TypeTaskSettled,
		Timestamp:    time.Now(),
		TaskID:       notice.TaskID,
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: invocationID,
		InstanceID:   l.instance,
	}
	if msg.Content != "" || msg.ToolID != "" || msg.ToolName != "" || len(msg.ToolCalls) > 0 {
		evt.Message = msg
	}
	return l.commitFactKey(ctx, addr.AgentID, evt)
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

// beginWriter registers one production of this loop that is starting now and
// answers the confirmation that closes its registration. The ledger is the
// loop's own: a production of a loop is not a task, so the registration names
// the production and mints no identity the task account would have to carry.
func (l *Loop) beginWriter(name string) func() {
	l.mu.Lock()
	writers := l.writers
	l.mu.Unlock()
	return writers.Begin(name)
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

// SetConsumers assigns the loop's only Consumer set during assembly and returns
// the actual current set. The first non-nil set wins; later replacement or clearing
// does not change it. A loop with no assigned set mounts one on first resident use,
// while a backflow beside no current set only parks its result and never mounts one.
func (l *Loop) SetConsumers(consumers *Consumers) *Consumers {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.consumers == nil && consumers != nil {
		l.consumers = consumers
	}
	return l.consumers
}

// drivingSet reports the consumption loops that drive the addresses of this loop's
// ranges: the set a host attached, or the set this loop mounted for itself, and none
// beside a loop that runs neither. The read mounts nothing — a settled backflow wakes
// the set that already drives its range and never starts one of its own to wake.
func (l *Loop) drivingSet() *Consumers {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.drivingSetLocked()
}

// drivingSetLocked reports the set that drives this loop's addresses under the lock
// the mount and the attach take.
func (l *Loop) drivingSetLocked() *Consumers {
	return l.consumers
}

// residentOf reports the consumption loops this loop drives delegated turns on:
// the set attached to it, or the set it mounts for itself when none is attached.
//
// The set mounted here is the loop's own, so a run assembled beside no host still
// drives every delegated execution site on a resident instance of that site. Once
// mounted, this same current set also receives the settled backflow of its ranges.
func (l *Loop) residentOf() *Consumers {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.residentLocked()
}

// residentLocked reports the set this loop drives its resident addresses through,
// mounting its own beside no attached one, under the lock the mount and the attach
// take.
func (l *Loop) residentLocked() *Consumers {
	if l.consumers == nil {
		l.consumers = NewConsumers(l, context.Background(), nil)
	}
	return l.consumers
}

// StopResidents ends the loop's one current Consumer set: no further instance mounts,
// each flying segment is answered by cancellation, and its inputs stay on the chain
// for restart. The current set remains as the stopped tombstone. No Loop lock is held
// while joining consumers, so a Consumer tool may synchronously re-enter WakeAddress.
func (l *Loop) StopResidents() {
	if l == nil {
		return
	}
	l.mu.Lock()
	resident := l.residentLocked()
	l.mu.Unlock()
	resident.Stop()
}

// StopResidentsWithin ends the loop's one current Consumer set inside the named
// deadline: the set is marked first — no further instance mounts, no context
// of a flying loop is cancelled by the mark itself, and each loop the set
// already holds ends its own life once the work it holds really ends — and
// the join that follows waits for those exits under the same deadline, so a
// close whose budget arrived returns beside the loops still flying instead of
// blocking on them. A loop the budget ended beside inside a model production
// is left flying and named in the answer; its production stays named by the
// writer ledger of the run, so the close that spent its budget reports it and
// a later drive finishes it for real. A loop the budget ended beside between
// turns is closed by its own loop (see Consumers.awaitStopped), so no entry
// waits on a driver that already spent its close. A zero deadline joins
// without one.
func (l *Loop) StopResidentsWithin(deadline time.Time) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	resident := l.residentLocked()
	l.mu.Unlock()
	resident.markStopped()
	return resident.awaitStopped(deadline)
}

// instanceFor answers the one consumption instance of one address, mounting it
// through the given set when this loop carries none: the registry of this loop, not
// the map of a set, is what answers "one instance per address".
//
// - An address this loop already drives answers the instance it carries, whatever set asks: a second set never starts a second driver of the same decisions.
// - A loop that already drives its addresses through one set mounts through that set alone: a second set answers no instance instead of opening a driver the close of this loop would not stop, and never takes an instance another set already mounted.
// - A loop beside no driving set mounts through whichever set asks first, exactly as a set assembled over a loop by a test or an embedder always could.
// - The chain is read for the cursor of a fresh instance outside the registry lock, so the mount never reads the chain under the lock it answers the registry by.
// - A quiet mount marks a fresh instance for a delegated batch: see Consumers.mount.
func (l *Loop) instanceFor(addr event.Address, cs *Consumers, quiet bool) *Consumer {
	l.mu.Lock()
	if cs == nil {
		l.mu.Unlock()
		return nil
	}
	if l.consumers == nil {
		l.consumers = cs
	}
	if l.currentStoppedLocked() {
		l.mu.Unlock()
		return nil
	}
	if instance, held := l.instances[addr]; held {
		l.mu.Unlock()
		return instance
	}
	if l.consumers != cs {
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()
	cursor := lastOutputOf(l.sessionFacts(addr))
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentStoppedLocked() {
		return nil
	}
	if instance, held := l.instances[addr]; held {
		return instance
	}
	if l.consumers != cs {
		return nil
	}
	instance := cs.mountLocked(l, addr, cursor, quiet)
	if instance == nil {
		return nil
	}
	l.instances[addr] = instance
	return instance
}

// currentStoppedLocked reads the one current set's terminal marker. Existing
// instances and fresh mounts both pass this check before any input is accepted.
func (l *Loop) currentStoppedLocked() bool {
	if l.consumers == nil {
		return false
	}
	l.consumers.mu.Lock()
	defer l.consumers.mu.Unlock()
	return l.consumers.stopped
}

// forgetInstance drops one ended instance from the registry and from the set that
// mounted it, so the address that mounts next is a new driver instead of the dead
// one this loop left behind.
func (l *Loop) forgetInstance(c *Consumer) {
	if c == nil {
		return
	}
	l.mu.Lock()
	if held, ok := l.instances[c.addr]; ok && held == c {
		delete(l.instances, c.addr)
	}
	l.mu.Unlock()
	c.set.forget(c)
}

// forgetInstancesOf drops every instance the named set mounted: the stop of a set
// answers no address of its own, so the registry never names a driver that ended.
func (l *Loop) forgetInstancesOf(cs *Consumers) {
	if cs == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for addr, instance := range l.instances {
		if instance.set == cs {
			delete(l.instances, addr)
		}
	}
}

// drivesAddress reports whether one address carries a consumption instance this loop
// drives, whoever mounted it.
func (l *Loop) drivesAddress(addr event.Address) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, held := l.instances[addr]
	return held
}

// DelegatedInput is what one delegated turn hands the resident instance of its
// execution site: the site, the binding its decisions run on, the instruction it runs
// under, the materials the task reads from and the request it starts from.
type DelegatedInput struct {
	// Site is the execution address the delegated turn runs in.
	Site event.Address
	// Binding is the binding the decisions of that site run on: the assembled
	// sub-agent's own material, never a generation held for a declared agent.
	Binding state.AgentBinding
	// Instruction is the instruction the turn runs under; empty declares none.
	Instruction string
	// Request is the task this event states: the direct content of the task
	// fact that closes the delegated input batch and drives the round.
	Request string
	// Materials are the source events the task reads from, resolved by the
	// caller: the framework projects each into the site's chain as the material
	// the round receives, and a source the call could not resolve stands here as
	// none. The consuming agent stays unaware of the resolution.
	Materials []event.Event
	// declaration is the resolved declaration of the turn as its caller rendered
	// it: opaque JSON stamped on the task fact that closes the input batch, so a
	// re-entry after a restart rebuilds the assembly from the site's own chain
	// instead of holding a second durable copy of it in process memory. It is
	// framework metadata, not a face of the input, so it stays unexported.
	declaration []byte
}

// WithDeclaration stamps the resolved declaration of one delegated turn onto the
// input, so the task fact that closes its batch carries the assembly a re-entry
// after a restart serves the next round on. The body stays opaque here: the
// caller that assembled the declaration owns its vocabulary.
func (in DelegatedInput) WithDeclaration(declaration []byte) DelegatedInput {
	in.declaration = append([]byte(nil), declaration...)
	return in
}

// DelegatedAnswer is the outcome of one delegated round as its execution site reported
// it: the body its last decision produced and the terminal it ended at.
type DelegatedAnswer struct {
	// Body is the assistant text the round ended on; empty when it produced none.
	Body string
	// Status is the terminal the delegated task reports.
	Status task.Status
	// Failure is the named cause a failed round ended at, empty otherwise.
	Failure string
}

// ErrDelegatedSiteIncomplete reports a delegated turn whose execution site names no
// complete agent session, so no resident instance could run it.
var ErrDelegatedSiteIncomplete = errors.New("agent: the delegated site names no complete agent session")

// ErrDelegatedInstanceUnavailable reports a delegated turn asked for while the loop
// carries no set of resident instances, or while the set already stopped.
var ErrDelegatedInstanceUnavailable = errors.New("agent: no resident instance is available for the delegated site")

// SubmitDelegated hands one delegated turn to the resident instance of its execution
// site and answers the segment that consumed it: the input commits at that site under
// the delegation lineage, the instance mounted with the binding the turn runs on is
// woken, and the round is the one that segment flies. The caller keeps the task face —
// admission, settle and notice stay where they are — and reads only what the turn
// produced.
// - The input commits as the site's own external input before the wake, so the segment the wake reads as its own material.
// - The answer is the chain the segment closed on: a body that never committed is a failed round rather than a copy kept beside the chain.
// - The body is the one the closing decision of that segment produced, never whatever the chain of the site ended at meanwhile: a second segment that closed beside this wait has no body of its own to lend.
// - A cancelled wait reports the cancelled terminal beside no error, so a closing run settles what it already admitted instead of leaving it owed. A wait the end of the loop answers is the same cancellation: the segment stays owed on the chain for the restart, and the round reports the terminal it really reached.
func (l *Loop) SubmitDelegated(ctx context.Context, in DelegatedInput) (DelegatedAnswer, error) {
	return l.submitDelegated(ctx, in, nil)
}

// SubmitDelegatedBeside hands one delegated turn to the resident instance of its site
// exactly as SubmitDelegated does, and reports the round's first commit beside it: the callback
// hears nil once the input has landed at the site, and the named cause of a turn that never
// landed. A caller that answers a delivery receipt uses it to answer only for a body that
// arrived, instead of answering before the input reached the chain the receipt claims.
//
// The report is the input's commit and nothing else: a round that lands its input and then
// fails still reported nil, because the body did arrive and what became of the round is its
// own terminal's to report.
func (l *Loop) SubmitDelegatedBeside(ctx context.Context, in DelegatedInput, started func(error)) (DelegatedAnswer, error) {
	return l.submitDelegated(ctx, in, started)
}

// submitDelegated is the one body of a delegated turn: the caller that reads only the answer
// and the caller that also has to hear the first commit share it, so a turn reports the same
// input, the same wake and the same terminal whichever way it was handed over.
//
// - The wake names every fact the batch committed beside the task, and the wait reads the task key alone, because the task is the identity of the round.
func (l *Loop) submitDelegated(ctx context.Context, in DelegatedInput, started func(error)) (DelegatedAnswer, error) {
	report := delegationStart(started)
	if !in.Site.IsComplete() {
		report(ErrDelegatedSiteIncomplete)
		return DelegatedAnswer{}, ErrDelegatedSiteIncomplete
	}
	consumers := l.residentOf()
	if consumers == nil {
		report(ErrDelegatedInstanceUnavailable)
		return DelegatedAnswer{}, ErrDelegatedInstanceUnavailable
	}
	consumer := consumers.mount(in.Site, &in.Binding)
	if consumer == nil {
		report(ErrDelegatedInstanceUnavailable)
		return DelegatedAnswer{}, ErrDelegatedInstanceUnavailable
	}
	drive, taskKey, err := l.delegatedInputOf(ctx, in)
	if err != nil {
		report(err)
		return DelegatedAnswer{Status: task.StatusFailed, Failure: err.Error()}, err
	}
	report(nil)
	consumer.Drive(drive...)
	consumer.Wake()
	answer, waitErr := consumer.WaitAnswer(ctx, delegationWaitKeys(taskKey))
	if waitErr != nil {
		if ctx.Err() != nil || errors.Is(waitErr, ErrSegmentCancelled) {
			return DelegatedAnswer{Status: task.StatusCancelled}, nil
		}
		return DelegatedAnswer{Status: task.StatusFailed, Failure: waitErr.Error()}, waitErr
	}
	status, failure := delegatedTerminalOf(answer)
	return DelegatedAnswer{Body: answer.Body, Status: status, Failure: failure}, nil
}

// delegationStart reports the first commit of one delegated round, at most once: the input's
// commit is the one report a started round makes, and every early refusal reports the cause
// it never started for. The callback is read at most once because a round that reports its
// start and then fails reports the start, never a second, later answer to the same question.
func delegationStart(started func(error)) func(error) {
	if started == nil {
		return func(error) {}
	}
	var once sync.Once
	return func(err error) {
		once.Do(func() { started(err) })
	}
}

// delegatedInputOf commits the input of one delegated turn at its execution site
// as a batch of ordinary input facts, in the order the round reads them: the
// instruction it runs under lands first as a system message of its own, the
// sources the call handed over land beside it as the material projections their
// keys name, and the task the caller stated lands last as the fact that closes
// the batch. The framework assembles and the agent consumes; no fact of the
// batch is a second structure beside the chain, and the facts the batch already
// committed stay readable by key after a restart.
//
// The task is the only fact of the batch that starts a range: the delegated tool
// names the whole batch when it wakes the site (see submitDelegated), so a batch
// whose task never landed leaves its instruction and materials inert instead of
// driving half a round. Each part commits under the key its execution site
// derives, so a retried round lands no second fact while the next round carries
// its own input and its own fact.
//
// - The instruction commits once per site under a stable identity; a later round of the same site re-commits that identity and lands no second fact.
// - A material is the projection of one committed source event; its key derives from the source key, so a retried round lands no second projection and the same source is one material.
// - The task key derives from the execution site and the request, so a retried round of one input lands no second fact.
func (l *Loop) delegatedInputOf(ctx context.Context, in DelegatedInput) ([]event.EventID, event.EventID, error) {
	drive := make([]event.EventID, 0, 2+len(in.Materials))
	if strings.TrimSpace(in.Instruction) != "" {
		key, err := l.commitFactKey(ctx, in.Site.AgentID, l.delegatedFactOf(in.Site,
			delegatedPartInstruction, "",
			event.EventID("delegated-"+delegatedSiteStamp(in.Site)+"-instruction"),
			model.Message{Role: model.RoleSystem, Content: in.Instruction}, nil))
		if err != nil {
			return nil, "", err
		}
		if key != "" {
			drive = append(drive, key)
		}
	}
	for _, material := range in.Materials {
		if material.Key == "" {
			continue
		}
		message := material.Message
		if message.Role == "" {
			role, err := RoleOf(material.Type)
			if err != nil {
				return nil, "", err
			}
			message.Role = role
		}
		key, err := l.commitFactKey(ctx, in.Site.AgentID, l.delegatedFactOf(in.Site,
			delegatedPartMaterial, string(material.Key),
			event.EventID("delegated-"+delegatedSiteStamp(in.Site)+"-material-"+string(material.Key)),
			message, nil))
		if err != nil {
			return nil, "", err
		}
		if key != "" {
			drive = append(drive, key)
		}
	}
	var key event.EventID
	if in.Site.SessionID != "" {
		digest := sha256.Sum256([]byte(in.Request))
		key = event.EventID(fmt.Sprintf("delegated-%s-%x", in.Site.SessionID, digest[:8]))
	}
	committed, err := l.commitFactKey(ctx, in.Site.AgentID, l.delegatedFactOf(in.Site,
		delegatedPartTask, "", key, model.Message{Role: model.RoleUser, Content: in.Request}, in.declaration))
	if err != nil {
		return nil, "", err
	}
	if committed != "" {
		drive = append(drive, committed)
	}
	return drive, committed, nil
}

// delegatedFactOf projects one part of a delegated input batch onto the fact the
// site's chain commits: the site as owner, source and target, the delegation
// lineage beside the part the fact states, and the instance the round runs on.
// The parts of a batch are the material of one delegated round and never a
// host-facing delivery or a freshness fact.
//
// - The declaration travels on the task fact alone; the instruction and material facts stay material of the round.
func (l *Loop) delegatedFactOf(site event.Address, part, source string, key event.EventID, message model.Message, declaration []byte) event.Event {
	meta := map[string]string{
		event.MetaKeyTriggerSource: event.LineageDelegation,
		delegatedPartMeta:          part,
	}
	if source != "" {
		meta[delegatedSourceMeta] = source
	}
	if part == delegatedPartTask && len(declaration) > 0 {
		meta[event.MetaKeyDelegation] = string(declaration)
	}
	return event.Event{
		Type:       event.TypeExternalInput,
		Timestamp:  time.Now(),
		Key:        key,
		Message:    message,
		Owner:      site,
		Source:     site,
		Target:     site,
		InstanceID: l.instance,
		Meta:       meta,
	}
}

// delegationWaitKeys names the fact one delegated round waits on: the task key
// that closes its batch, or no key beside a loop that committed nothing.
func delegationWaitKeys(taskKey event.EventID) []event.EventID {
	if taskKey == "" {
		return nil
	}
	return []event.EventID{taskKey}
}

// delegatedTerminalOf reports the terminal one closed segment answers with: the
// reduction's own terminal when the segment ended on one, the named cause when it ended
// beside an error, and the settled outcome only when neither stands.
func delegatedTerminalOf(answer SegmentAnswer) (task.Status, string) {
	switch {
	case answer.Err != nil || answer.Turn.Terminal == model.KindFailed:
		failure := answer.Turn.Failure
		if failure == "" && answer.Err != nil {
			failure = answer.Err.Error()
		}
		return task.StatusFailed, failure
	case answer.Turn.Terminal == model.KindCancelled:
		return task.StatusCancelled, answer.Turn.Failure
	default:
		return task.StatusSettled, ""
	}
}

// modelFlight is the model decision one address runs: the range it serves and
// the identity of the decision itself.
type modelFlight struct {
	invocation event.InvocationID
	decision   event.DecisionID
}

// InFlightModel reports the model decision the consumption state of one address
// carries, or that no model request of that address is executing.
//
// The redline list (3 rules):
// MUST report the decision this loop runs, never a decision another loop runs.
// MUST report no flight once the decision of that address ended.
// MUST NOT take a decision just to read it: the read starts nothing.
func (l *Loop) InFlightModel(addr event.Address) (event.InvocationID, event.DecisionID, bool) {
	if l == nil {
		return "", "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	flight, held := l.flight[addr]
	if !held {
		return "", "", false
	}
	return flight.invocation, flight.decision, true
}

// markModelInFlight records the decision one address is running and answers the
// release that clears it: the consumption state a reader sees is the decision
// the loop actually flies, and a released decision leaves no flight behind.
func (l *Loop) markModelInFlight(addr event.Address, invocation event.InvocationID, decision event.DecisionID) func() {
	l.mu.Lock()
	if l.flight == nil {
		l.flight = map[event.Address]modelFlight{}
	}
	l.flight[addr] = modelFlight{invocation: invocation, decision: decision}
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if flight, held := l.flight[addr]; held && flight.decision == decision {
			delete(l.flight, addr)
		}
	}
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

// decideOn runs one model decision over the range's transcript: it takes the
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
// - The fuses of one decision are read off the request as it stands before the dispatch: the usage of a response lands after the dispatch the fuses rule.
// - The environment block the boundary rendered trails the request as its last message, behind the transcript and the closing instruction, so the dynamic material never enters the prefix.
// decideOn runs one decision of an address whose material names the binding it decides
// on: the execution site of a delegated turn decides on the binding the caller resolved
// for it, and never on a generation a published declaration holds.
func (l *Loop) decideOn(ctx context.Context, inv Invocation, transcript []model.Message, round decisionRound, bound *state.AgentBinding) (Decision, model.Outcome, []model.Message, []admittedCall, error) {
	return l.decideWith(ctx, inv, transcript, round, bound)
}

// decideWith is the one decision body both entries run: the handle is the named binding
// when the material names one and the generation the borrowing face serves at this
// moment otherwise. A decision on a named binding is captured by that binding's own
// model beside the turn it serves, so the loop fixes no second view of the same calls
// and records no second attempt.
func (l *Loop) decideWith(ctx context.Context, inv Invocation, transcript []model.Message, round decisionRound, bound *state.AgentBinding) (Decision, model.Outcome, []model.Message, []admittedCall, error) {
	if round.final {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, round.timeout)
		defer cancel()
	}
	decisionID := l.nextDecisionID(inv)
	release := l.markModelInFlight(inv.Address, inv.InvocationID, decisionID)
	defer release()
	attemptID := event.AttemptID(fmt.Sprintf("attempt-%s", decisionID))
	if err := l.occupy(ctx, inv, decisionID); err != nil {
		return Decision{}, model.Outcome{}, nil, nil, err
	}
	handle, err := l.decisionHandle(ctx, inv, bound)
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
	if bound == nil {
		l.fixCapture(ctx, decisionID, view, req)
	}
	ruling := l.rulingOf(messages)

	production := l.beginWriter("model:" + string(decisionID))
	// Every resource a decision takes is released by an idempotent guard as
	// well as at its explicit point: a panic or a Goexit inside the model or
	// the reduction skips plain statements, and a writer, a decision slot or a
	// binding handle left held would strand every later drive of this address
	// (the slot waits a dead instance forever). The guard runs exactly once
	// whichever path reached it first.
	var productionOnce, vacateOnce, releaseOnce sync.Once
	stopProduction := func() { productionOnce.Do(production) }
	defer stopProduction()
	vacateSlot := func() { vacateOnce.Do(func() { _ = l.slots.Vacate(inv.Address, l.instance, inv.InvocationID) }) }
	defer vacateSlot()
	releaseHandle := func() { releaseOnce.Do(func() { _ = handle.Release(ctx) }) }
	defer releaseHandle()
	stream, entryErr := handle.Binding.Model.GenerateContent(ctx, &req)
	if entryErr != nil {
		stream = failedStream(entryErr)
	}
	outcome := model.ReduceChannel(ctx, stream, nil)
	stopProduction()
	if bound == nil {
		l.recordAttempt(ctx, inv, decisionID, attemptID, outcome)
	}
	consumerDebug("tagent: consumer decision invocation=%s decision=%s terminal=%s calls=%d closing=%t",
		inv.InvocationID, decisionID, outcome.Kind, len(callsOf(outcome)), round.final)
	if ruling.threshold > 0 && ruling.tokens > ruling.threshold {
		consumerDebug("tagent: consumer long-context fuse invocation=%s decision=%s approx_tokens=%d threshold=%d calls=%d",
			inv.InvocationID, decisionID, ruling.tokens, ruling.threshold, len(callsOf(outcome)))
	}

	decision := Decision{
		DecisionID:   decisionID,
		InvocationID: inv.InvocationID,
		Binding:      handle.Binding,
		Request:      req,
		Terminal:     outcome.Kind,
	}
	vacateSlot()

	var receipts []model.Message
	var running []admittedCall
	if outcome.Kind == model.KindCompleted && outcome.Body != nil && outcome.Body.IsToolCallResponse() {
		if round.final {
			receipts = refuseClosingCalls(callsOf(outcome))
		} else {
			if commitErr := l.commitCalls(ctx, inv, decision, outcome); commitErr != nil {
				releaseHandle()
				return decision, outcome, nil, nil, commitErr
			}
			receipts, running, err = l.dispatch(ctx, inv, decision, callsOf(outcome), callable, ruling)
			if err != nil {
				releaseHandle()
				return decision, outcome, nil, nil, err
			}
		}
	}
	releaseHandle()
	l.noteClosedDecision(inv.InvocationID, round)
	return decision, outcome, receipts, running, nil
}

// noteClosedDecision records one decision that ended successfully as a field of
// the turn its range closes on: the number of decisions the turn ran and, when
// this decision was the closing round, the reason that round ran. A decision
// that ended beside an error is recorded by nothing: the range it would have
// committed closes without an output fact.
func (l *Loop) noteClosedDecision(invocationID event.InvocationID, round decisionRound) {
	in := l.intakeFor(invocationID)
	in.mu.Lock()
	defer in.mu.Unlock()
	in.decisions++
	if round.final {
		in.finalRound = round.reason
	}
}

// decisionHandle reports the binding handle one decision runs on: the binding the
// material named for the address when it names one — held as it stands, for an address
// no published generation serves — and the generation the borrowing face serves at this
// moment otherwise.
func (l *Loop) decisionHandle(ctx context.Context, inv Invocation, bound *state.AgentBinding) (*state.BindingHandle, error) {
	if bound != nil {
		named := *bound
		return &state.BindingHandle{Binding: named}, nil
	}
	return l.borrow.Borrow(ctx, inv.Address.AgentID)
}

// dispatch admits every tool call of one decision through the task wrapper of
// the borrowed generation and answers each call with the single receipt of its
// admission as the paired tool message of that call. The admission loop admits
// every call first, and the drive ends beside the receipts it answered — the
// settled result of each admitted call returns as the event the follow-up
// decision the settle path fires reads, never as a synchronous response this
// dispatch waits for.
// - The plugin pipeline of the borrowed generation runs its one Before pass per call before that call is admitted, and its After pass only over facts whose canonical commit already happened.
// - The pipeline is assembled from the instances of that generation: every declared port dispatches its instance exactly as NewFlow dispatches it, so the loop and the plugin set share one dispatch and no second assembly arises here.
// - The one Before pass travels on the context into the admission ruling beside the call frame, so the pre-execution ruling consumes the risk grade its risk faces rendered and never runs a second risk face of its own.
// - The transformed final call is the one both executed and recorded; the identity it is admitted under stays the resolved CallID.
// - The receipt is the synchronous answer of the call: the model reads the minimal confirmation that the call happened, the task identities stay out of the request, and the settled result never answers the call again.
// - Every call of this decision is already admitted and answered with its receipt before the first wait for a settled result begins.
// - The settled result fact is the stored event the next decision of this range reduces; a call that recorded no fact contributes none.
// - The After pass reads only a fact whose canonical commit already happened: a failed observation is a visible gap in the returned presentation and never rolls the committed fact back, and a dropped presentation drops the presentation only. The presentation is the read-out the inspection face consumes; the loop keeps the committed fact itself.
// - The ruling of the response rules the calls before the first admission: a response the fuse holds is parked as one approval batch inside this dispatch, answered call by call when the batch does not run, and admitted whole in the order it was issued when the batch is approved. A call of a held response is never dispatched before the ruling that allows it, so the storm it belongs to leaves no task, no result fact and no degraded message behind.
// - The calls of an approved batch are read by one follow-up decision: each carries the flag its consumer reads to defer the decision that follows the dispatch until every call of the batch has settled, so the decision reads all their results as the batch the approver released.
// - The wait of a parked batch holds no model seat (the decision slot is vacated before this dispatch runs) and ends with the ruling, the expiry of the request or the cancellation of the invocation.
func (l *Loop) dispatch(ctx context.Context, inv Invocation, decision Decision, calls []model.ToolCall, tools map[string]trpctool.CallableTool, ruling callRuling) ([]model.Message, []admittedCall, error) {
	pipeline, err := plugin.NewFlow(decision.Binding.Plugins)
	if err != nil {
		return nil, nil, err
	}
	var notice []model.Message
	whole := false
	if fuse := ruling.fuseOver(len(calls)); fuse != "" {
		batch := parkedBatchOf(decision, calls, ruling, fuse)
		outcome, by := l.awaitBatch(ctx, batch)
		consumerDebug("tagent: consumer tool fuse invocation=%s decision=%s fuse=%s calls=%d cap=%d threshold=%d tokens=%d ruling=%s approval=%s",
			inv.InvocationID, decision.DecisionID, fuse, len(calls), ruling.cap, ruling.threshold, ruling.tokens, outcome, batch.id)
		if outcome != batchApproved {
			return batchReceipts(calls, batchRefusalText(outcome, batch, len(calls), ruling, by)), nil, nil
		}
		whole = true
		notice = append(notice, batchApprovalNotice(len(calls), batch, by))
	}
	receipts := make([]model.Message, 0, len(calls)+len(notice))
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
			running = append(running, admittedCall{callID: call.ID, tool: name, taskID: out.TaskID, batch: whole})
		case task.HoldReceipt:
			receipts = append(receipts, paired(call.ID, name, "tagent: the call is parked awaiting approval"))
		default:
			receipts = append(receipts, paired(call.ID, name, answerText(callErr, out)))
		}
	}
	receipts = append(receipts, notice...)
	return receipts, running, nil
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

// intakeWindow bounds how many ended ranges keep their backflow bookkeeping in this
// loop: a range is minted per drive and per consumption segment, so a resident run
// mints one per segment, and the bookkeeping of the least recently ended range leaves
// the map once this many ended ranges stand in it. A range that has not ended is never
// recycled, so a drive in flight keeps the read set it accumulated.
const intakeWindow = 4096

// intakeFor returns the backflow intake of one invocation, creating it on first
// use.
//
// - A range that ended long enough ago mints an empty intake again: the address it ran
// at is not kept beside the recycled entry, so a notification landing after that many
// further ranges of the session commits no notice fact and is reported by its task
// account alone.
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

// retireIntake records that one range ended, so the window may recycle its
// bookkeeping: the settled cache, the waits and the read/observed sets of the range
// are read by nothing once the range ended, and a resident run mints a range per
// segment.
func (l *Loop) retireIntake(invocationID event.InvocationID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.retireIntakeLocked(invocationID)
}

// retireIntakeLocked appends one ended range to the recycle order and drops the
// least recently ended ranges past the window. The read is taken under the lock the
// creation of every intake takes, so a recycled entry is never one a drive reopens.
func (l *Loop) retireIntakeLocked(invocationID event.InvocationID) {
	l.ended = append(l.ended, invocationID)
	for len(l.ended) > intakeWindow {
		oldest := l.ended[0]
		l.ended = l.ended[1:]
		delete(l.intakes, oldest)
	}
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
// - The read set names the facts standing in this range's transcript: the notices it read and the input a decision boundary reported.
// - The observed set names the facts whose After pass this loop already ran, so neither set is exported and a boundary that meets the same fact again doubles nothing.
type intake struct {
	mu       sync.Mutex
	address  event.Address
	waiters  map[event.TaskID][]chan delivered
	settled  map[event.TaskID][]delivered
	read     map[event.EventID]bool
	observed map[event.EventID]bool
	// decisions, finalRound and trigger are the stable fields of the turn this
	// range closes on: the decisions it ran successfully, the reason its closing
	// round ran, and the trigger lineage its opening material carried. The output
	// fact commits them beside its input relation, so the chain states the closed
	// turn itself rather than a word derived after the fact landed.
	decisions  int
	finalRound FinalRoundReason
	trigger    string
}

// setAddress records the address the range of this intake runs at, so the
// delivery fact of a settled call names the range it lands in. Opening a range
// also starts the closing fields of its turn: a range identity a later
// consumption loop reuses begins a turn of its own instead of inheriting the
// count, closing round and trigger of the range that ended before it.
func (in *intake) setAddress(addr event.Address) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.address = addr
	in.decisions = 0
	in.finalRound = FinalRoundNone
	in.trigger = ""
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
	_, order := state.ToolFace(binding)
	view.visibleTools = append(view.visibleTools, order...)
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
// reads its range's input again from at every decision boundary. The face may
// also answer the presentation capability: an implementation whose
// chain already knows which facts the request prefix of its session presents
// reports them through SessionPresented(addr event.Address) map[event.EventID]bool,
// and the resident consumption loop reads that capability from this same face
// rather than from a second seam. A face that keeps no prefix answers no
// presentation, and the pull keeps the chain's own boundary. The redline list (4 rules):
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
// the loop stays as it is. The face also carries the optional presentation
// capability: a chain that knows which facts its request prefix presents is
// read through this same attachment, so the loop has no second presentation
// seam to inject.
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

// sessionPresented reports the facts of the named session whose text the request
// prefix presents, and whether the attached chain face answered the presentation
// capability at all: a loop beside no face, or beside a chain that keeps no
// prefix, assembles no presentation and the caller keeps its own boundary. The
// read is derived from the chain attachment itself — the very ledger the pull
// already reads its facts from — so presentation is never a second seam that
// could disagree with the chain.
func (l *Loop) sessionPresented(addr event.Address) (map[event.EventID]bool, bool) {
	l.mu.Lock()
	face := l.history
	l.mu.Unlock()
	if face == nil {
		return nil, false
	}
	presented, ok := face.(interface {
		SessionPresented(addr event.Address) map[event.EventID]bool
	})
	if !ok {
		return nil, false
	}
	return presented.SessionPresented(addr), true
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
