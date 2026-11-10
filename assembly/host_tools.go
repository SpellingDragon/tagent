// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The host-owned tools and ports: the send tool the model calls across tasks and
// sessions, the scheduled reflection plan and the port that commits its body to a
// session, and the request shape an inbound channel posts to the task endpoint. Each
// one is a face the host contributes to the assembly, so the host builds it here
// rather than leaving the wiring to every deployment.
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// hostSendToolID is the declaration-side name of the outbound message tool.
const hostSendToolID = "send"

// hostSendTargetKey opens the frozen argument dialect of one send call (design D6):
// a target object beside the body, and the three keys that object may carry. The
// target names exactly one destination — a task, or an agent beside its session —
// and every key below is the spelling the declaration and the refusal text share, so
// a rename here is a rename of the contract the model reads.
const (
	// hostSendTargetKey is the one argument beside the body: the destination object.
	hostSendTargetKey = "target"
	// hostSendContentKey is the body argument.
	hostSendContentKey = "content"
	// hostSendTargetTaskKey names the task destination inside the target object.
	hostSendTargetTaskKey = "task"
	// hostSendTargetAgentKey names the agent of a session destination.
	hostSendTargetAgentKey = "agent"
	// hostSendTargetSessionKey names the session of a session destination.
	hostSendTargetSessionKey = "session"
)

// hostSendReceiptOK opens the frozen receipt dialect of one send call (design D6): a
// delivered body answers the bare ok, and every body that was not delivered opens with
// the refusal prefix, so a caller reads the two states apart from the text alone.
//
// The dialect carries two states because only two are reachable: a body handed to a task
// is answered after the round's own input has landed, a body handed to a peer session is
// answered after its fact committed, and nothing the send face can reach leaves the body
// waiting. The governance hold is not a third state of this dialect — it is the admission
// receipt's own shape (task.HoldReceipt), rendered before the tool runs, so a model that
// reads a held call never reads a send receipt at all.
const (
	// hostSendReceiptOK is the whole receipt of a body the call delivered.
	hostSendReceiptOK = "ok"
	// hostSendDeniedPrefix opens a refusal receipt: nothing was delivered.
	hostSendDeniedPrefix = "[send_denied] "
)

// hostSendContentMax is the byte ceiling of one body the send tool delivers.
const hostSendContentMax = 8 * 1024

// hostSendTool is the model-visible face of one body handed to one destination: a
// task this run serves a round of, or the session of a peer agent. The tool carries
// no channel of its own, because the body a turn delivers leaves through the turn
// body alone (design DEL-1).
type hostSendTool struct {
	// reentry is the task re-entry face the {task} destination dispatches to. A host
	// that carries none refuses the call by name instead of delivering the body
	// outbound, because the body names a task and no task can be reached that way.
	reentry hostSendReentry
	// session is the cross-session delivery face the {agent, session} destination
	// dispatches to. A host that carries none refuses the call by name rather than
	// falling back to the outbound channel, because the body names a peer session
	// and the channel would answer a different question than the model asked.
	session hostSendSession
}

var _ trpctool.CallableTool = (*hostSendTool)(nil)

// hostSendReentry is the task re-entry face one send call dispatches to: the task the
// call names is re-entered with the body, and the outcome is reported in the same two
// receipts every delivery reports through. The face is the seam between the tool and the
// task state machine, so the tool itself holds no registry.
type hostSendReentry interface {
	// ReenterTask hands one body to the named task and reports the outcome of that
	// re-entry beside the cause of a refusal.
	ReenterTask(ctx context.Context, taskID event.TaskID, content string) (deliveryOutcome, string)
}

// hostSendReentryPort is the host's binding of the re-entry face one send call
// dispatches to: the task table the named task is reactivated in, the delegation
// ledger whose entry is moved to the round that reactivation opened, and the
// continuation table the round itself is served from.
//
// The port is built before the run and bound after Assemble, so the tool every
// agent carries reads the live scene the run holds instead of a copy taken at
// construction. A port bound to no run refuses the call by name rather than
// reporting a re-entry no task can answer.
type hostSendReentryPort struct {
	run           *Run
	tasks         hostReentryTasks
	ledger        hostReentryLedger
	continuations hostReentryContinuations
	// rebuild is the second face of the continuation account: whether it already
	// serves a task and the seed that rebuilds a round from the declaration its
	// delegated input fact carries. It is kept apart from the claim-and-serve face
	// so each seam stays a narrow contract of its own.
	rebuild hostReentryRebuild
}

var _ hostSendReentry = (*hostSendReentryPort)(nil)

// hostReentryTasks is the task-table face one re-entry moves, and nothing more: the
// settled status design D7 admits read without moving the task, the reactivation that
// opens the round the body is owed to, and the compensation that puts the task back
// when the round it opened is never served. The face is the seam between the tool and
// the task state machine, so the order the port settles its preconditions in is
// readable here instead of being spread over the registry.
type hostReentryTasks interface {
	// Reentrant reports whether the task may be re-entered, without moving it, and
	// names an identity the table holds no record of.
	Reentrant(ctx context.Context, taskID event.TaskID) error
	// Reactivate re-enters a settled task as a new round and reports the round it
	// opened.
	Reactivate(ctx context.Context, taskID event.TaskID) (int64, error)
	// RollbackReactivation puts the task back to the settled outcome that round
	// re-entered it from, because nothing served it.
	RollbackReactivation(ctx context.Context, taskID event.TaskID, round int64) error
}

// hostReentryLedger is the delegation-ledger face one re-entry moves: the terminal
// entry the reopen needs read without moving it, the reopen that moves the entry to
// the round the task opened, and the withdrawal of a round nothing served.
type hostReentryLedger interface {
	// CanReopenRound reports whether the ledger would reopen the entry of one
	// delegation, without moving it.
	CanReopenRound(taskID event.TaskID) error
	// ReopenRound moves the ledger entry to the round the reactivation opened.
	ReopenRound(taskID event.TaskID, round int64) error
	// AbandonRound withdraws the reopening of one round nothing served.
	AbandonRound(taskID event.TaskID, round int64)
}

// hostReentryContinuations is the continuation-table face one re-entry claims: the
// one round that will carry the body, the slot a refused re-entry gives back, and
// the round the body is served into.
type hostReentryContinuations interface {
	// Claim takes the one round slot of one task and reports the claim it could not
	// make by name.
	Claim(taskID event.TaskID) error
	// Abandon gives a claimed slot back without serving a round for it.
	Abandon(taskID event.TaskID)
	// Serve serves the next round of the claimed task with the body, answers once the
	// body is in that round, and reports how the round itself ended to after.
	Serve(ctx context.Context, taskID event.TaskID, input string, after func(error)) error
}

// hostReentryRebuild is the continuation account's rebuild face: whether it already
// serves one task a round, and the seed that rebuilds a continuation from the
// declaration a delegated input fact carries. A re-entry after a restart reads this
// face before it claims, so the round it serves runs on the assembly the chain
// recorded instead of one the stopped process held in memory.
type hostReentryRebuild interface {
	// Served reports whether the table still serves the named task a round.
	Served(taskID event.TaskID) bool
	// Seed rebuilds the continuation of one task from the declaration its delegated
	// input fact carries.
	Seed(taskID event.TaskID, declaration []byte, siteSession event.SessionID, builder subagent.Builder) error
}

// Bind attaches the assembled run the port re-enters tasks through.
func (p *hostSendReentryPort) Bind(run *Run) {
	p.run, p.tasks, p.ledger, p.continuations, p.rebuild = nil, nil, nil, nil, nil
	if run == nil {
		return
	}
	p.run = run
	if run.registry != nil {
		p.tasks = run.registry
	}
	if run.delegations != nil {
		p.ledger = run.delegations
	}
	if run.continuations != nil {
		p.continuations = run.continuations
		p.rebuild, _ = run.continuations.(hostReentryRebuild)
	}
}

// ReenterTask hands one body to the named task and reports the outcome of that
// re-entry: a settled task of this host is reactivated into a new round, the
// ledger entry of the task is moved to that round, and the round body is handed to
// the delegated turn the task was first served in.
//
// The answer is the delivery and not the acceptance: ok is returned only once the round
// serving the body has landed its own input, so a caller that reads ok reads a body that
// arrived. A round that never reaches its input is refused by name here, and the attempt is
// filed in the account of the call beside the refusal, so a body that reached nothing is a
// queryable refusal rather than a sentence with nothing behind it. Every read the round
// depends on is settled before the first mutation, and the one round that will carry the
// body is in this call's hand before it answers:
// - The settled status design D7 admits and the terminal ledger entry the reopen needs are both read first, so a refusal moves nothing at all.
// - A delegation entry and a continuation this process does not hold are rebuilt from the task account and the declaration the delegated input fact carries before the round is claimed, so a restart re-enters the round the record names instead of refusing a round process memory lost.
// - The claim is what makes the receipt true. Without it the call could answer "ok" for a body a second re-entry also took, or for a round that never opened, and the body would be lost behind a receipt that said otherwise.
// - The round is opened last, and the round a later step refuses is read against the one step that still had to race: the round is put back rather than leaving the task in flight with no turn to serve it.
// - The receipt waits for the round's first commit: a body handed to a round that never landed it is refused here, and the cause is filed with it.
// - A round that lands the body and then ends without recording its terminal keeps the round it holds and leaves the accepted body owed to the next round. The failure is that round's own: the ok already answered for the committed input stands, and no denial or broken receipt is filed beside it.
// - Only a task this host serves a round of is re-entered: an identity the table holds no record of, a task whose delegated round this host never served, and every status design D7 refuses are each answered as a named refusal rather than reactivated into a round nobody asked for.
// - A delegation the ledger holds no entry of is rebuilt from the task the record already carries: the process that admitted it is gone, so the settled terminal and the return site of that delegation live on the task account alone. A task of another family, or one whose admission was not a delegation, stays refused by name.
// - A continuation this process never minted is rebuilt from the declaration the delegated input fact travels with: the round a restart re-enters is served on the assembly the chain recorded, not on a copy process memory lost.
func (p *hostSendReentryPort) ReenterTask(ctx context.Context, taskID event.TaskID, content string) (deliveryOutcome, string) {
	if p.tasks == nil || p.ledger == nil {
		return deliveryRefused, "the host is not assembled"
	}
	continuations := p.continuations
	if continuations == nil {
		return p.refuseReentry(ctx, taskID, hostDenialClaim, subagent.ErrContinuationMissing)
	}
	if err := p.tasks.Reentrant(ctx, taskID); err != nil {
		return p.refuseReentry(ctx, taskID, hostDenialTask, err)
	}
	if err := p.ledger.CanReopenRound(taskID); err != nil {
		if !errors.Is(err, agent.ErrDelegationUnknown) {
			return p.refuseReentry(ctx, taskID, hostDenialDelegation, err)
		}
		if p.run == nil || p.run.registry == nil || p.run.delegations == nil {
			return p.refuseReentry(ctx, taskID, hostDenialDelegation, errHostReentryUnassembled)
		}
		spec, status, lookupErr := p.run.registry.Lookup(ctx, taskID)
		if lookupErr != nil {
			return p.refuseReentry(ctx, taskID, hostDenialDelegation, lookupErr)
		}
		if restoreErr := p.run.delegations.RestoreEntry(taskID, spec, status); restoreErr != nil {
			return p.refuseReentry(ctx, taskID, hostDenialDelegation, restoreErr)
		}
		if err := p.ledger.CanReopenRound(taskID); err != nil {
			return p.refuseReentry(ctx, taskID, hostDenialDelegation, err)
		}
	}
	if p.rebuild != nil && !p.rebuild.Served(taskID) {
		if err := p.rebuildContinuation(ctx, taskID); err != nil {
			return p.refuseReentry(ctx, taskID, hostDenialClaim, err)
		}
	}
	if err := continuations.Claim(taskID); err != nil {
		return p.refuseReentry(ctx, taskID, hostDenialClaim, err)
	}
	round, err := p.tasks.Reactivate(ctx, taskID)
	if err != nil {
		continuations.Abandon(taskID)
		return p.refuseReentry(ctx, taskID, hostDenialTask, err)
	}
	if err := p.ledger.ReopenRound(taskID, round); err != nil {
		continuations.Abandon(taskID)
		p.unwindReentry(ctx, taskID, round, err)
		return p.refuseReentry(ctx, taskID, hostDenialDelegation, err)
	}
	if err := continuations.Serve(ctx, taskID, content, func(roundErr error) {
		if roundErr != nil {
			p.roundEndedBeside(taskID, round, roundErr)
		}
	}); err != nil {
		p.unwindReentry(ctx, taskID, round, err)
		return p.refuseReentry(ctx, taskID, hostDenialStart, err)
	}
	return deliverySent, ""
}

// errHostReentryUnassembled reports a re-entry whose continuation must be rebuilt beside no
// assembled run, so no declaration or builder can be resolved for the round.
var errHostReentryUnassembled = errors.New("assembly: the re-entry carries no assembled run")

// errDelegationDeclarationMissing reports a re-entry whose task carries no declaration on the
// delegated input fact the chain holds, so the round it would serve has no assembly to rebuild.
var errDelegationDeclarationMissing = errors.New("assembly: the delegated input of that task carries no declaration")

// rebuildContinuation seeds the continuation of one task from the facts the chain already
// holds: the declaration the delegated input fact travels with, and the caller whose assembled
// surface resolves the round's tools. The entry serves the next round of the task the state
// machine opens; this method moves no task and claims no slot of its own.
func (p *hostSendReentryPort) rebuildContinuation(ctx context.Context, taskID event.TaskID) error {
	if p.run == nil || p.run.registry == nil || p.rebuild == nil {
		return errHostReentryUnassembled
	}
	spec, _, err := p.run.registry.Lookup(ctx, taskID)
	if err != nil {
		return err
	}
	declaration, err := p.run.delegationDeclaration(spec)
	if err != nil {
		return err
	}
	builder, err := p.run.delegatedBuilder(ctx, spec.Source.AgentID)
	if err != nil {
		return err
	}
	return p.rebuild.Seed(taskID, declaration, subagent.DelegatedSiteSession(spec.Source.SessionID, spec.CallID), builder)
}

// delegationDeclaration reports the resolved declaration one delegated task's input fact
// carries. The chain is the durable account when the run has a tier and the live ledger
// beside none, so a restart reads the declaration back even when the site chain does not fit
// the hot window. The site session locates the chain: its agent is the assembled name the
// declaration carries, while the session is the caller session beside the native call.
func (r *Run) delegationDeclaration(spec task.Spec) ([]byte, error) {
	session := subagent.DelegatedSiteSession(spec.Source.SessionID, spec.CallID)
	if session == "" {
		return nil, fmt.Errorf("%w: the task names no complete site", errDelegationDeclarationMissing)
	}
	var found []byte
	if err := r.eachDeliveryFact(func(fact event.Event) bool {
		if fact.Target.SessionID != session {
			return true
		}
		if declaration := fact.Meta[event.MetaKeyDelegation]; declaration != "" {
			found = []byte(declaration)
			return false
		}
		return true
	}); err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("%w: site %s", errDelegationDeclarationMissing, session)
}

// refuseReentry answers one refused re-entry and files the attempt: the model reads the named
// cause, and the account of the call holds the refusal under the stage that turned it away. A
// call carrying no frame names no session to file the attempt in, so the refusal is answered
// alone rather than invented into a chain of its own.
func (p *hostSendReentryPort) refuseReentry(ctx context.Context, taskID event.TaskID, category string, err error) (deliveryOutcome, string) {
	cause := hostReentryCause(taskID, err)
	p.refuse(ctx, taskID, category, cause)
	return deliveryRefused, cause
}

// refuse files one refused re-entry in the account of the session that asked for it, under the
// identity the call and the task share: the attempt names the task it named and the stage that
// turned it away, so a retry files one record and a different stage files its own.
func (p *hostSendReentryPort) refuse(ctx context.Context, taskID event.TaskID, category, cause string) {
	p.fileRefusal(ctx, taskID, DeliveryReceiptDenied, category, cause)
}

// breakReentry files the one re-entry whose compensation failed, under its own receipt: a round the
// task was not put back from is not a body that was turned away and reached nothing, it is a task left
// in a round no turn serves. The two read apart in the account, so an operator querying the broken
// attempts never reads them as ordinary refusals.
func (p *hostSendReentryPort) breakReentry(ctx context.Context, taskID event.TaskID, category, cause string) {
	p.fileRefusal(ctx, taskID, DeliveryReceiptBroken, category, cause)
}

// fileRefusal lands one attempt of one re-entry in the account at the receipt and the stage it stands
// at. A call carrying no frame names no session to file the attempt in, so the record is left to the
// cause the caller is answered with rather than invented into a chain of its own.
func (p *hostSendReentryPort) fileRefusal(ctx context.Context, taskID event.TaskID, receipt, category, cause string) {
	run := p.run
	if run == nil {
		return
	}
	frame, framed := task.CallFrameFrom(ctx)
	if !framed {
		return
	}
	identity := string(frame.Identity.DedupKey()) + "\x00" + string(taskID)
	run.recordRefusal(ctx, frame, taskID, frame.Address, identity, receipt, category, cause)
}

// roundEndedBeside reports a round that landed its input and then ended without recording the
// terminal of its own round. The delivery already answered ok for the input that committed, so
// the failure belongs to the round: the task keeps the round it holds, the ledger keeps its
// reopen, and the accepted body stays owed to the next round. Nothing is put back and no denied
// or broken receipt is filed beside the ok, because a failure after the input committed may not
// be answered as a denial of an input that already arrived.
func (p *hostSendReentryPort) roundEndedBeside(taskID event.TaskID, round int64, cause error) {
	assemblyWarn("tagent: the round %d of %s landed its input and ended without recording its terminal: %v; the task keeps the round it holds and the accepted body stays owed to its next round", round, taskID, cause)
}

// unwindReentry puts a round nothing served back: the task returns to the settled
// outcome the reactivation re-entered it from, the ledger entry returns to the
// terminal it reopened from, and the cause is reported rather than left in the log
// of a body the caller was told had been taken. The body itself stays owed to the
// next round of the task, so the re-entry loses the round and never the input.
//
// A task that cannot be put back is the one failure the compensation cannot cover: the round stays
// open with no turn to serve it, so the account files the attempt as broken and the assembly reports
// it at ERROR — a task stuck in a round nobody serves is a warning an operator has to see, not a line
// in a debug stream. Nothing reaps it: the terminal TTL reclaims tasks that reached a terminal outcome,
// and a task left in a round it was not put back from reached none, so it refuses every re-entry of
// the round it is stuck in until the task table itself is reset.
func (p *hostSendReentryPort) unwindReentry(ctx context.Context, taskID event.TaskID, round int64, cause error) {
	ctx = context.WithoutCancel(ctx)
	if err := p.tasks.RollbackReactivation(ctx, taskID, round); err != nil {
		p.breakReentry(ctx, taskID, hostDenialRollback, err.Error())
		assemblyError("tagent: the round %d of %s was opened and never served, and the task was not put back: %v; the task stays in a round no turn serves and refuses every re-entry of it until the task table is reset", round, taskID, err)
	}
	p.ledger.AbandonRound(taskID, round)
	assemblyWarn("tagent: the round %d of %s was withdrawn beside: %v; the body it was handed stays owed to the next re-entry of the task", round, taskID, cause)
}

// hostReentryCause renders the cause one refused re-entry is answered with: the
// named refusal the task table, the delegation ledger or the continuation table
// reported, so the model reads which status it named and what answers it instead.
func hostReentryCause(taskID event.TaskID, err error) string {
	switch {
	case errors.Is(err, task.ErrTaskNotFound):
		return fmt.Sprintf("the task table holds no record of %s, and a body names a task of this host", taskID)
	case errors.Is(err, task.ErrReactivationFailed):
		return "the task is failed: " + task.ErrReactivationFailed.Error()
	case errors.Is(err, task.ErrReactivationCancelled):
		return "the task is cancelled: " + task.ErrReactivationCancelled.Error()
	case errors.Is(err, task.ErrRoundInFlight), errors.Is(err, subagent.ErrContinuationBusy):
		return fmt.Sprintf("a round of %s is in flight, so the body waits for its terminal: %v", taskID, task.ErrRoundInFlight)
	case errors.Is(err, agent.ErrDelegationNotSettled):
		return "the delegation has not settled, so its next round is not open yet: " + agent.ErrDelegationNotSettled.Error()
	case errors.Is(err, subagent.ErrContinuationMissing):
		return fmt.Sprintf("no delegated round of %s is served here: the task was never delegated, or the round it was served in already left", taskID)
	default:
		return err.Error()
	}
}

// hostSendSession is the cross-session delivery face one send call dispatches to:
// one body handed to the session the call names, judged by the four gates the
// delivery face keeps and answered in the two receipts every delivery reports
// through. The face is the seam between the tool and the live scene, so the tool
// itself resolves no session and holds no white list.
type hostSendSession interface {
	// DeliverToSession hands one body to the session the call names and reports the
	// outcome of that delivery beside the cause of a refusal.
	DeliverToSession(ctx context.Context, agent, session, content string) (deliveryOutcome, string)
}

// hostSendSessionLineage is the trigger source one cross-session delivery stamps on
// the fact it commits, so the target turn reads a body a peer sent rather than input
// from its own user (design D8).
const hostSendSessionLineage = "send"

// hostSendSessionReceipt is the outcome one delivery identity answered, so a replay
// of that identity answers the same text and writes nothing a second time.
type hostSendSessionReceipt struct {
	outcome deliveryOutcome
	cause   string
}

// DeliverToSession hands one body to the session the call names: the four gates the
// delivery face keeps judge the destination first, the body then commits into the
// target chain under the call's own identity, and an idle target is driven through
// one round while a busy one reads the fact at its own next decision boundary. The
// sender is never blocked on the target's turn, so a busy session never deadlocks the
// call that delivered to it.
func (p *hostMeditationPort) DeliverToSession(ctx context.Context, agent, session, content string) (deliveryOutcome, string) {
	if p.run == nil {
		return deliveryRefused, "the host is not assembled"
	}
	frame, framed := task.CallFrameFrom(ctx)
	if !framed {
		return deliveryRefused, "the call carries no frame, so the delivery names no sender"
	}
	addr := event.Address{AgentID: event.AgentID(agent), SessionID: event.SessionID(session)}
	if outcome, cause := p.gate(ctx, frame, addr); outcome != deliverySent {
		p.refuseDelivery(ctx, frame, addr, hostDenialGate, cause)
		return outcome, cause
	}

	key := hostSendSessionKey(frame, addr)
	p.mu.Lock()
	defer p.mu.Unlock()
	if receipt, seen := p.receipts[key]; seen {
		return receipt.outcome, receipt.cause
	}
	receipt := p.deliver(ctx, frame, addr, content)
	if p.receipts == nil {
		p.receipts = make(map[string]hostSendSessionReceipt)
	}
	p.receipts[key] = receipt
	return receipt.outcome, receipt.cause
}

// gate judges one session destination against the four gates the delivery face keeps,
// in the order the delivery spec freezes: the deliver_to white list, the observed
// face, the live binding of the named session and the liveness of its loop. Every gate
// refuses by name, so the model reads which one turned the body away.
// - The deliver_to white list is the one the document declared, so a host that declared none delivers nothing: an absent plan, a sender outside it and a target outside it are each refused.
// - A destination outside the observed face is blind: the reflection cannot see what it writes there, so the delivery is refused instead of being guessed at.
// - The named session is resolved against the sessions this run already bound, so establishing one nobody asked for is never this seam's answer.
// - A host shutting down, and a site its store does not serve, are each refused by name instead of being established a second time.
func (p *hostMeditationPort) gate(ctx context.Context, frame task.CallFrame, addr event.Address) (deliveryOutcome, string) {
	if p.plan == nil {
		return deliveryRefused, "this host declares no deliver_to white list, and a session destination is refused closed beside none"
	}
	if string(frame.Address.AgentID) != p.plan.Agent {
		return deliveryRefused, fmt.Sprintf("agent %q carries no cross-session delivery face: the white list this host declares belongs to %q",
			frame.Address.AgentID, p.plan.Agent)
	}
	if !hostSendNames(p.plan.Targets, string(addr.AgentID)) {
		return deliveryRefused, fmt.Sprintf("the deliver_to white list of %q names no %q, and a body delivered outside it would reach a session the reflection never declared",
			p.plan.Agent, addr.AgentID)
	}
	if !hostSendNames(p.plan.Observed, string(addr.AgentID)) {
		return deliveryRefused, fmt.Sprintf("the observed face of %q does not reach %q, so a body delivered there would be blind",
			p.plan.Agent, addr.AgentID)
	}
	if !addr.IsComplete() {
		return deliveryRefused, "the target names an incomplete session address"
	}
	if !hostSendBound(p.run, addr) {
		return deliveryRefused, fmt.Sprintf("no session %q is bound to agent %q", addr.SessionID, addr.AgentID)
	}
	if p.run.refusing() {
		return deliveryRefused, "the host is shutting down, so no session is running"
	}
	if p.run.store != nil {
		if _, err := p.run.store.Establish(ctx, addr, p.run.instance); err != nil {
			return deliveryRefused, fmt.Sprintf("the session %q of agent %q is not running: %v", addr.SessionID, addr.AgentID, err)
		}
	}
	return deliverySent, ""
}

// deliver commits one body into the target chain and wakes its resident Consumer.
// Target business does not select the driver or the receipt boundary: ok means the
// input fact committed, while the target's answer remains with its own Consumer.
// The fact identity is the delivery identity, so a replay lands no second fact.
func (p *hostMeditationPort) deliver(ctx context.Context, frame task.CallFrame, addr event.Address, content string) hostSendSessionReceipt {
	if p.run.loop == nil {
		cause := "this host drives no loop, so the body would land beside no turn"
		p.refuseDelivery(ctx, frame, addr, hostDenialRoute, cause)
		return hostSendSessionReceipt{outcome: deliveryRefused, cause: cause}
	}
	deliveryKey := event.EventID(hostSendSessionKey(frame, addr))
	evt := event.Event{
		Type:         event.TypeExternalInput,
		Timestamp:    time.Now(),
		Key:          deliveryKey,
		Meta:         hostDeliveryMeta(frame, addr, string(deliveryKey)),
		Message:      model.Message{Role: model.RoleUser, Content: content},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: hostSendSessionInvocation(frame, addr),
	}
	if _, err := p.run.commitOnly(ctx, addr, evt); err != nil {
		cause := fmt.Sprintf("the session %q of agent %q did not take the body: %v", addr.SessionID, addr.AgentID, err)
		p.refuseDelivery(ctx, frame, addr, hostDenialCommit, cause)
		return hostSendSessionReceipt{outcome: deliveryRefused, cause: cause}
	}
	p.run.loop.WakeAddress(addr)
	return hostSendSessionReceipt{outcome: deliverySent}
}

// hostSendBound reports whether this run already bound the named address, so a
// destination nobody established is refused instead of being opened by the delivery.
func hostSendBound(run *Run, addr event.Address) bool {
	for _, live := range run.ActiveSessions(addr.AgentID) {
		if live == addr {
			return true
		}
	}
	return false
}

// hostSendSessionKey is the identity one delivery is deduplicated under: the call the
// body came from beside the address it names, so a replay of the same call answers the
// same receipt and a second body from another call still lands.
func hostSendSessionKey(frame task.CallFrame, addr event.Address) string {
	return string(frame.Identity.DedupKey()) + "\x00" + string(addr.AgentID) + "\x00" + string(addr.SessionID)
}

// hostSendSessionInvocation identifies the input fact one delivery commits under:
// derived from the call itself, so a replay names the same input. The resident Consumer
// mints its own segment range for the answer instead of driving under this send identity.
func hostSendSessionInvocation(frame task.CallFrame, addr event.Address) event.InvocationID {
	return event.InvocationID(fmt.Sprintf("%s#sent-%s", addr.SessionID, frame.Identity.DedupKey()))
}

// hostSendNames reports whether one name appears in a resolved name list.
func hostSendNames(names []string, name string) bool {
	for _, held := range names {
		if held == name {
			return true
		}
	}
	return false
}

// DeliveryReceiptSent is the one receipt a body that reached its target stands at: the body
// landed in the chain of the destination it named. It is the frozen word of the delivery
// receipt entry, so both delivery families read the same receipt for a body that landed and a
// receipt no fact states reads as this one.
const DeliveryReceiptSent = "sent"

// DeliveryReceiptDenied and DeliveryReceiptBroken are the two receipts an attempt that reached
// nothing stands at: denied for a delivery the face turned away — a gate, a task table or a
// round that never landed the body — and broken for one whose round nothing served could not be
// put back, so the account of the attempt is the only place that failure survives.
//
// Both are receipts of an account, never result text: the model reads the two-state dialect of
// the send face, and the record is what makes the refusal queryable afterwards rather than
// living only in the sentence the caller was answered with.
const (
	// DeliveryReceiptDenied is the receipt of an attempt that reached no target.
	DeliveryReceiptDenied = "denied"
	// DeliveryReceiptBroken is the receipt of an attempt whose round could not be withdrawn.
	DeliveryReceiptBroken = "broken"
)

// hostDenialGate names the four-gate judgement of a cross-session destination; the categories
// beside it name every other stage that turns a body away, so a reader of the account tells a
// gate refusal from a round that never landed.
const (
	// hostDenialGate is the four-gate judgement of a cross-session destination.
	hostDenialGate = "gate"
	// hostDenialRoute is a destination this run drives no loop of.
	hostDenialRoute = "route"
	// hostDenialCommit is the commit of a body into the target chain.
	hostDenialCommit = "commit"
	// hostDenialTask is the task table refusing a re-entry.
	hostDenialTask = "task"
	// hostDenialDelegation is the delegation ledger refusing the reopen of a round.
	hostDenialDelegation = "delegation"
	// hostDenialClaim is the continuation table refusing the claim of a round.
	hostDenialClaim = "claim"
	// hostDenialStart is a round that never landed the input it was handed.
	hostDenialStart = "start"
	// hostDenialRollback is a round nothing served that could not be put back.
	hostDenialRollback = "rollback"
)

// hostDeliveryMeta renders the delivery entries one committed delivery carries
// (design D7): the target it reached, the identity it is deduplicated under, the
// receipt it stands at and the source event it is filed under. The keys are the
// frozen MetaKeyDelivery entries, so the account of one delivery is read from the
// same four entries whichever route carried the body.
// - The identity is the canonical identity of the committed fact, so a replayed delivery resolves against the fact it already landed instead of writing beside it.
// - The source is the invocation of the call that issued the delivery, so every delivery of one turn is read back under the one event that caused them.
func hostDeliveryMeta(frame task.CallFrame, addr event.Address, identity string) map[string]string {
	return map[string]string{
		event.MetaKeyTriggerSource:    hostSendSessionLineage,
		event.MetaKeyDeliveryTarget:   hostDeliveryTarget(addr),
		event.MetaKeyDeliveryIdentity: identity,
		event.MetaKeyDeliveryReceipt:  DeliveryReceiptSent,
		event.MetaKeyDeliverySource:   string(frame.Identity.InvocationID),
	}
}

// hostDeliveryTarget renders one address as the value of a delivery entry: the
// agent and the session it names, so an entry read back names both.
func hostDeliveryTarget(addr event.Address) string {
	return string(addr.AgentID) + "/" + string(addr.SessionID)
}

// hostDeliveryRefusalMeta renders the delivery entries of one attempt that reached nothing: the
// target the call named, the identity the attempt is filed under, the receipt it stands at, the
// source event it belongs to and the stage that turned it away, with the cause kept as the
// one-line summary of the fact so a reader of the account reads what was refused it. The keys
// are the frozen MetaKeyDelivery entries, so an attempt and a landing are read by the same
// vocabulary.
func hostDeliveryRefusalMeta(frame task.CallFrame, target event.Address, identity, receipt, category, cause string) map[string]string {
	meta := map[string]string{
		event.MetaKeyTriggerSource:    hostSendSessionLineage,
		event.MetaKeyDeliveryTarget:   hostDeliveryTarget(target),
		event.MetaKeyDeliveryIdentity: identity,
		event.MetaKeyDeliveryReceipt:  receipt,
		event.MetaKeyDeliverySource:   string(frame.Identity.InvocationID),
		event.MetaKeyDeliveryDenial:   category,
	}
	if cause != "" {
		meta[event.MetaKeyEventSummary] = cause
	}
	return meta
}

// recordRefusal lands one refused attempt in the account of the session that asked for it: a body
// that reached nothing is still an attempt the account reads back, by the source event it belongs
// to and by the target it named, so a refusal is queryable afterwards instead of living only in
// the text the caller was answered with.
//
// The fact carries no model payload: it is an audit fact of the host, and a fact with no content
// enters no mapped sequence, so the chain of the sender reads the account without its transcript
// ever learning of it. An identity the account already holds is not written beside itself, so a
// retry of the same refused attempt files one record.
func (r *Run) recordRefusal(ctx context.Context, frame task.CallFrame, taskID event.TaskID, target event.Address, identity, receipt, category, cause string) {
	if r == nil {
		return
	}
	key := event.EventID(identity + "#" + receipt + ":" + category)
	if _, err := r.commitOnly(context.WithoutCancel(ctx), frame.Address, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Key:       key,
		TaskID:    taskID,
		Target:    target,
		Meta:      hostDeliveryRefusalMeta(frame, target, identity, receipt, category, cause),
	}); err != nil {
		assemblyError("tagent: the refused delivery %s is not in the account err=%v", identity, err)
	}
}

// refuseDelivery files one refused session delivery in the account of the session that asked for
// it, under the identity the call and the destination share: a body that reached no session is
// still an attempt the account reads back, so a reader tells the refusal of one destination from
// the refusal of another and a replay of the same call files one record.
func (p *hostMeditationPort) refuseDelivery(ctx context.Context, frame task.CallFrame, addr event.Address, category, cause string) {
	p.run.recordRefusal(ctx, frame, "", addr, hostSendSessionKey(frame, addr), DeliveryReceiptDenied, category, cause)
}

// DeliveryRecord is one single-target delivery of one run: the source event it is
// filed under, the target it reached, the identity it is deduplicated under, the
// receipt it stands at and the type of the fact that carried it. The fact type
// keeps the two delivery families apart — an agent's own body is an external input
// and a framework state change is a task settle — while every other field is read
// through the same four entries for both.
type DeliveryRecord struct {
	// Source is the event the delivery is filed under.
	Source event.EventID
	// Target is the address the delivery reached.
	Target event.Address
	// Identity is the canonical identity the delivery is deduplicated under.
	Identity string
	// Receipt is the receipt the delivery stands at.
	Receipt string
	// FactType is the frozen type of the fact that carried the delivery.
	FactType string
}

// Meta reports the four delivery entries of one record under the frozen keys, so
// two records of different families are compared and consumed by the same
// vocabulary instead of by the shape of the fact behind them.
func (r DeliveryRecord) Meta() map[string]string {
	return map[string]string{
		event.MetaKeyDeliveryTarget:   hostDeliveryTarget(r.Target),
		event.MetaKeyDeliveryIdentity: r.Identity,
		event.MetaKeyDeliveryReceipt:  r.Receipt,
		event.MetaKeyDeliverySource:   string(r.Source),
	}
}

// DeliveryLedger is the delivery ledger of one assembled run (design D9): the
// read-back face of the delivery account, projecting by source event and by
// target the delivery facts the run committed. The account is the derived state
// the checkpoint states; the ledger reads it back and holds no state of its own,
// and the tier the run keeps makes the facts durable — so a query never parses a
// body: the four entries, the identity and the type of the fact are all it reads.
type DeliveryLedger struct {
	run *Run
}

// DeliveryLedger reports the delivery ledger of this run, or nil beside no run.
func (r *Run) DeliveryLedger() *DeliveryLedger {
	if r == nil {
		return nil
	}
	return &DeliveryLedger{run: r}
}

// Records reports every delivery the run accounts for, ordered by source event,
// then by target and identity, so two reads of one chain answer the same order.
// One identity answers one record: a repeated delivery of the identity the chain
// already holds lands no second fact and therefore no second record. A durable
// chain that could not be read answers a named error instead of a partial view.
func (l *DeliveryLedger) Records() ([]DeliveryRecord, error) {
	if l == nil || l.run == nil {
		return nil, nil
	}
	records := make([]DeliveryRecord, 0, 16)
	seen := map[string]struct{}{}
	if err := l.run.eachDeliveryFact(func(fact event.Event) bool {
		record, held := hostDeliveryRecordOf(fact)
		if !held {
			return true
		}
		if _, repeated := seen[record.Identity]; repeated {
			return true
		}
		seen[record.Identity] = struct{}{}
		records = append(records, record)
		return true
	}); err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Source != records[j].Source {
			return records[i].Source < records[j].Source
		}
		if records[i].Target.AgentID != records[j].Target.AgentID {
			return records[i].Target.AgentID < records[j].Target.AgentID
		}
		if records[i].Target.SessionID != records[j].Target.SessionID {
			return records[i].Target.SessionID < records[j].Target.SessionID
		}
		return records[i].Identity < records[j].Identity
	})
	return records, nil
}

// BySource reports the deliveries one source event is accounted for: every target
// the event reached, each beside its own identity and receipt.
func (l *DeliveryLedger) BySource(source event.EventID) ([]DeliveryRecord, error) {
	records, err := l.Records()
	if err != nil {
		return nil, err
	}
	var out []DeliveryRecord
	for _, record := range records {
		if record.Source == source {
			out = append(out, record)
		}
	}
	return out, nil
}

// ByTarget reports the deliveries one address received, whichever family carried
// them.
func (l *DeliveryLedger) ByTarget(addr event.Address) ([]DeliveryRecord, error) {
	records, err := l.Records()
	if err != nil {
		return nil, err
	}
	var out []DeliveryRecord
	for _, record := range records {
		if record.Target == addr {
			out = append(out, record)
		}
	}
	return out, nil
}

// DeliveryRefusal is one attempt the delivery face turned away: the source event it is filed
// under, the target the call named, the identity it is deduplicated under, the receipt it stands
// at and the stage that turned the body away.
type DeliveryRefusal struct {
	// Source is the event the attempt is filed under.
	Source event.EventID
	// Target is the address the call named, which the body never reached.
	Target event.Address
	// Identity is the identity the attempt is deduplicated under.
	Identity string
	// Receipt is the receipt the attempt stands at: denied, or broken for a round that
	// could not be put back.
	Receipt string
	// Category is the stage that turned the body away.
	Category string
}

// Refusals reports every attempt the run turned away, ordered by source event, then target,
// then identity and category, so two reads of one chain answer the same order. The attempts
// are read from the same account as the landings, so a refusal is queryable after the fact
// rather than only from the sentence the caller was answered with.
func (l *DeliveryLedger) Refusals() ([]DeliveryRefusal, error) {
	if l == nil || l.run == nil {
		return nil, nil
	}
	refusals := make([]DeliveryRefusal, 0, 16)
	seen := map[string]struct{}{}
	if err := l.run.eachDeliveryFact(func(fact event.Event) bool {
		receipt := fact.Meta[event.MetaKeyDeliveryReceipt]
		if receipt != DeliveryReceiptDenied && receipt != DeliveryReceiptBroken {
			return true
		}
		identity := fact.Meta[event.MetaKeyDeliveryIdentity]
		category := fact.Meta[event.MetaKeyDeliveryDenial]
		dedup := identity + "\x00" + receipt + "\x00" + category
		if _, repeated := seen[dedup]; repeated {
			return true
		}
		seen[dedup] = struct{}{}
		refusals = append(refusals, DeliveryRefusal{
			Source:   event.EventID(fact.Meta[event.MetaKeyDeliverySource]),
			Target:   fact.Target,
			Identity: identity,
			Receipt:  receipt,
			Category: category,
		})
		return true
	}); err != nil {
		return nil, err
	}
	sort.Slice(refusals, func(i, j int) bool {
		if refusals[i].Source != refusals[j].Source {
			return refusals[i].Source < refusals[j].Source
		}
		if refusals[i].Target.AgentID != refusals[j].Target.AgentID {
			return refusals[i].Target.AgentID < refusals[j].Target.AgentID
		}
		if refusals[i].Target.SessionID != refusals[j].Target.SessionID {
			return refusals[i].Target.SessionID < refusals[j].Target.SessionID
		}
		if refusals[i].Identity != refusals[j].Identity {
			return refusals[i].Identity < refusals[j].Identity
		}
		return refusals[i].Category < refusals[j].Category
	})
	return refusals, nil
}

// errDeliveryScanDone ends a delivery enumeration early: the caller read the
// fact it named and no later record can change the answer.
var errDeliveryScanDone = errors.New("tagent: delivery scan complete")

// eachDeliveryFact walks every fact the delivery account reads, one at a time.
// The durable chain is the account; the live ledger is a hot window over it
// (facts.go, noWindowResidentFacts), so the walk streams the chain instead of
// materializing every body, and only a run beside no storage tier reads the
// window alone. The visit answers false to stop the walk after the fact it
// needed, so a lookup that names one fact never reads the rest of the chain.
//
// A durable walk that failed answers a named error instead of degrading to the
// live window: a partial window read beside an unreadable chain would present
// the deliveries it still holds as the whole account, hiding the ones the chain
// could not serve. The caller surfaces the error rather than answering a silent
// partial view.
func (r *Run) eachDeliveryFact(visit func(event.Event) bool) error {
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if tier != nil && tier.Segments != nil {
		err := tier.Segments.EachEvent(context.Background(), func(stored ledger.StoredEvent) error {
			fact, held := hostDeliveryFactOf(stored)
			if !held {
				return nil
			}
			if !visit(fact) {
				return errDeliveryScanDone
			}
			return nil
		})
		if errors.Is(err, errDeliveryScanDone) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tagent: the delivery account of this run could not read its durable chain: %w", err)
		}
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.factOrder {
		evt, held := r.facts[key]
		if !held {
			continue
		}
		if !visit(evt) {
			return nil
		}
	}
	return nil
}

// hostDeliveryFactOf decodes one stored record into the fact the delivery
// account projects: the type, the target, the metadata and the identity of each
// committed fact, which is every field a delivery record reads. A body this
// build cannot decode is skipped with the record named: an account that refused
// to answer beside one unreadable record would hide the deliveries it can still
// read.
func hostDeliveryFactOf(record ledger.StoredEvent) (event.Event, bool) {
	body, err := memory.DecodeEventBody(record.Body)
	if err != nil {
		assemblyWarn("tagent: the committed record %s is not read as a delivery err=%v", record.Key, err)
		return event.Event{}, false
	}
	return event.Event{
		Key:    body.Key,
		Type:   body.Type,
		Target: body.Target,
		Meta:   body.Meta,
	}, true
}

// hostDeliveryRecordOf projects one committed fact into its delivery record and
// reports whether the fact is a delivery at all.
// - A fact stamped with a delivery identity is one, whichever family wrote it: the entries it carries are read back exactly as the committing path stated them.
// - A fact whose receipt stands at a refusal is no delivery: an attempt the face turned away reached no target, so it is read by the refusal account (Refusals) and never counted as a body some target received.
// - A task settle fact is one too: it is the fact a terminal notification lands as, its identity is the notification identity the follow-up turn drives from, and a fact committed before the delivery entries existed carries none of them, so its record states the receipt the landed fact is.
// - Every other fact is no delivery: an agent output, a tool result and an attention fact are not bodies handed to a target.
func hostDeliveryRecordOf(fact event.Event) (DeliveryRecord, bool) {
	if identity := fact.Meta[event.MetaKeyDeliveryIdentity]; identity != "" {
		receipt := fact.Meta[event.MetaKeyDeliveryReceipt]
		if receipt == "" {
			receipt = DeliveryReceiptSent
		}
		if receipt != DeliveryReceiptSent {
			return DeliveryRecord{}, false
		}
		return DeliveryRecord{
			Source:   event.EventID(fact.Meta[event.MetaKeyDeliverySource]),
			Target:   fact.Target,
			Identity: identity,
			Receipt:  receipt,
			FactType: fact.Type,
		}, true
	}
	if fact.Type != event.TypeTaskSettled || noticeTaskIDOf(fact) == "" {
		return DeliveryRecord{}, false
	}
	if !terminalSettleKinds[noticeKindOf(fact)] {
		return DeliveryRecord{}, false
	}
	return DeliveryRecord{
		Source:   fact.Key,
		Target:   fact.Target,
		Identity: string(fact.Key),
		Receipt:  DeliveryReceiptSent,
		FactType: fact.Type,
	}, true
}

// hostSendArgs is the argument shape the model sees: the destination and the body, and
// nothing else.
type hostSendArgs struct {
	Target  *hostSendTarget `json:"target"`
	Content string          `json:"content"`
}

// hostSendTarget is the one destination a call names, as the object of mutually
// exclusive subfields the declaration describes: the task to re-enter, or the agent
// beside the session a body is delivered to. A target naming both, naming one of the
// pair alone, or naming neither is refused by name.
type hostSendTarget struct {
	Task    string `json:"task"`
	Agent   string `json:"agent"`
	Session string `json:"session"`
}

// Declaration implements tool.CallableTool: the one body a call hands across a task
// or a session boundary, and the destination argument that names which.
//
// The description promises the communication the tool is and no channel of its
// own: the body reaches the destination the call names, and the outbound channel
// stays with the turn body the host already delivers (design DEL-1). The schema states
// the two required arguments the call is read against — the target object and the body
// — so a shape the declaration does not describe is refused by name in Call.
func (t *hostSendTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        hostSendToolID,
		Description: "Deliver one body across tasks and sessions: name the destination in the target argument. The bare ok says the body reached that destination, and a [send_denied] result text names why it did not; the turn continues either way.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				hostSendTargetKey: {
					Type:        "object",
					Description: "The one destination the body is handed to: the task to re-enter, or the agent beside the session that receives the body.",
					Properties: map[string]*trpctool.Schema{
						hostSendTargetTaskKey:    {Type: "string", Description: "The task to re-enter with the body."},
						hostSendTargetAgentKey:   {Type: "string", Description: "The agent of a session destination."},
						hostSendTargetSessionKey: {Type: "string", Description: "The session of a session destination."},
					},
				},
				hostSendContentKey: {Type: "string", Description: "The body to deliver, UTF-8, at most 8KiB"},
			},
			Required: []string{hostSendTargetKey, hostSendContentKey},
		},
	}
}

// Call implements tool.CallableTool: every refusal and every dispatch outcome is
// answered as result text, and an error is reserved for a malformed argument shape.
//
// The call is read in the order design D6 freezes: the argument surface, then the one
// destination it names, then the body. A body the host delivered answers the bare ok, and
// every body that reached nothing answers the named refusal — the two states are the whole
// dialect, because a call this face can reach never leaves a body waiting. A destination this host
// carries no face for is refused by name and never falls back to an outbound delivery:
// the body named a task or a session, so sending it to the address of the call would
// answer a different question than the model asked.
func (t *hostSendTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if unknown := hostUnknownFields(probe, hostSendTargetKey, hostSendContentKey); len(unknown) > 0 {
		return t.denied("the arguments name fields outside the declaration " + strings.Join(unknown, ", ") +
			"; send accepts " + hostSendTargetKey + " and " + hostSendContentKey + " only"), nil
	}
	var args hostSendArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	target, reason := hostSendTargetOf(probe[hostSendTargetKey])
	if target == nil {
		return t.denied(reason), nil
	}
	if args.Content == "" {
		return t.denied("content is empty"), nil
	}
	if len(args.Content) > hostSendContentMax {
		return t.denied(fmt.Sprintf("content exceeds the 8KiB ceiling (%d bytes)", len(args.Content))), nil
	}
	return t.dispatch(ctx, target, args.Content), nil
}

// hostSendTargetOf resolves the one destination a call names and reports the reason it
// was refused beside no target: an absent object, a key outside the three the object
// declares, both destinations at once, one of the agent and session pair alone, and an
// object naming no destination at all are each refused by name.
func hostSendTargetOf(raw json.RawMessage) (*hostSendTarget, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, hostSendTargetMissing()
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, "the " + hostSendTargetKey + " is not the object the declaration describes"
	}
	if unknown := hostUnknownFields(probe, hostSendTargetTaskKey, hostSendTargetAgentKey, hostSendTargetSessionKey); len(unknown) > 0 {
		return nil, "the " + hostSendTargetKey + " names fields outside the declaration " + strings.Join(unknown, ", ") +
			"; it accepts " + hostSendTargetTaskKey + ", " + hostSendTargetAgentKey + " and " + hostSendTargetSessionKey + " only"
	}
	var named hostSendTarget
	if err := json.Unmarshal(raw, &named); err != nil {
		return nil, "the " + hostSendTargetKey + " is not the object the declaration describes"
	}
	task := strings.TrimSpace(named.Task)
	agent := strings.TrimSpace(named.Agent)
	session := strings.TrimSpace(named.Session)
	switch {
	case task != "" && (agent != "" || session != ""):
		return nil, "the " + hostSendTargetKey + " names a " + hostSendTargetTaskKey + " and a session at once, and a send names exactly one destination"
	case task != "":
		return &hostSendTarget{Task: task}, ""
	case agent != "" && session != "":
		return &hostSendTarget{Agent: agent, Session: session}, ""
	case agent != "" || session != "":
		return nil, "the " + hostSendTargetKey + " names only one of " + hostSendTargetAgentKey + " and " + hostSendTargetSessionKey +
			", and a session destination names both"
	}
	return nil, hostSendTargetMissing()
}

// hostSendTargetMissing is the refusal of a call that names no destination, shared by the
// absent object and the object carrying no value.
func hostSendTargetMissing() string {
	return "the arguments name no " + hostSendTargetKey + ": name the " + hostSendTargetTaskKey + " to re-enter, or the " +
		hostSendTargetAgentKey + " and " + hostSendTargetSessionKey + " to deliver to"
}

// dispatch hands one body to the destination the call named and answers its receipt: a
// task destination re-enters that task through the run's re-entry face, and a session
// destination delivers into that peer session through the run's cross-session face.
// A destination this host carries no face for is refused by name and never falls back
// to the other face, because each one answers a different question.
func (t *hostSendTool) dispatch(ctx context.Context, target *hostSendTarget, content string) string {
	if target.Task != "" {
		if t.reentry == nil {
			return t.denied("this host carries no task re-entry face")
		}
		return t.receipt(t.reentry.ReenterTask(ctx, event.TaskID(target.Task), content))
	}
	if t.session == nil {
		return t.denied("this host carries no cross-session delivery face")
	}
	return t.receipt(t.session.DeliverToSession(ctx, target.Agent, target.Session, content))
}

// receipt renders the two states one dispatch reports through: a delivered body answers
// the bare ok, and one that reached no target answers the named refusal. An outcome this
// dialect cannot render — a body still waiting on a review — is answered as the refusal it
// is, because a receipt that read as a delivery for a body nothing delivered would be the
// one lie the dialect exists to prevent.
func (t *hostSendTool) receipt(outcome deliveryOutcome, cause string) string {
	if outcome == deliverySent {
		return hostSendReceiptOK
	}
	return t.denied(cause)
}

// denied renders one refusal as result text.
func (t *hostSendTool) denied(reason string) string {
	return hostSendDeniedPrefix + reason
}

// hostUnknownFields names the argument fields outside the named declaration, sorted so
// the text is reproducible.
func hostUnknownFields(probe map[string]json.RawMessage, declared ...string) []string {
	allowed := make(map[string]bool, len(declared))
	for _, key := range declared {
		allowed[key] = true
	}
	var out []string
	for key := range probe {
		if !allowed[key] {
			out = append(out, "'"+key+"'")
		}
	}
	sort.Strings(out)
	return out
}

// hostMeditationPort is the host's binding of the two faces a reflection reads and
// writes its delivery through: the live-session query it resolves its declared
// deliver_to targets against, and the cross-session delivery face the one
// communication tool dispatches a `{agent, session}` destination to.
//
// The port is built before the run and bound after Assemble, so the tool every agent
// carries and the reflection itself read the live scene the run holds instead of a
// copy made at construction. The meditation loop session is never a delivery target:
// it is the observing session itself, not a binding of the agent.
type hostMeditationPort struct {
	plan *meditation.Plan
	run  *Run

	// mu brackets one delivery: its four gates, the fact it commits and the drive it
	// decides to owe are resolved while no other delivery interleaves, so two bodies
	// aimed at one idle session never open two turns beside each other. It also guards
	// receipts, the outcome one call identity already answered.
	mu       sync.Mutex
	receipts map[string]hostSendSessionReceipt

	// reflection brackets the reflection loop of the generation in effect: the
	// plan its face is built from beside the driver the commits of this run are
	// dispatched to. The faces one driver is assembled over are bound once, when
	// the run is assembled, because they are the run's own reads and writes.
	reflection sync.RWMutex
	face       meditationReflection
	installed  bool
	faces      meditation.Faces
	facesHeld  bool
	observe    func(meditation.TickOutcome)
}

var (
	_ meditation.SessionQuery = (*hostMeditationPort)(nil)
	_ hostSendSession         = (*hostMeditationPort)(nil)
)

// Bind attaches the assembled run the port reads and delivers through.
func (p *hostMeditationPort) Bind(run *Run) { p.run = run }

// meditationReflection is the reflection loop one generation serves: the plan
// the document declares beside the driver that fires its passes and the
// watermark gate that loop installed. A document that enables no meditation
// serves a nil plan beside no driver and no gate.
type meditationReflection struct {
	plan   *meditation.Plan
	driver *meditation.Driver
	gate   meditation.WatermarkGate
}

// bindFaces attaches the run faces every reflection driver of this port is
// assembled over, and the reading of each pass a driver runs.
func (p *hostMeditationPort) bindFaces(faces meditation.Faces, observe func(meditation.TickOutcome)) {
	p.reflection.Lock()
	defer p.reflection.Unlock()
	p.faces, p.facesHeld, p.observe = faces, true, observe
}

// mountReflection assembles the reflection loop of one plan over the run faces
// this port bound and installs it as the loop in effect. A nil plan installs the
// loop of a document that enables no meditation, and answers nil.
func (p *hostMeditationPort) mountReflection(plan *meditation.Plan) (*meditation.Driver, error) {
	p.reflection.Lock()
	faces, held, observe := p.faces, p.facesHeld, p.observe
	p.reflection.Unlock()
	if plan == nil {
		p.installReflection(meditationReflection{})
		return nil, nil
	}
	if !held {
		return nil, fmt.Errorf("tagent: the reflection loop of agent %q was built before the run bound its faces", plan.Agent)
	}
	gate, err := p.anchorGate(plan)
	if err != nil {
		return nil, err
	}
	faces.Gate = gate
	driver, err := plan.Instances(faces, observe)
	if err != nil {
		return nil, err
	}
	p.installWatermark(gate)
	p.installReflection(meditationReflection{plan: plan, driver: driver, gate: gate})
	return driver, nil
}

// anchorGate returns the watermark gate of one generation: the anchor store over
// the record its plan names, which is the record a loop rebuilt after a
// generation switch or a restart continues from. The store the loop in effect
// already reads is reused when it names the same record, so one file keeps one
// in-process writer while the superseded loop is still winding down.
func (p *hostMeditationPort) anchorGate(plan *meditation.Plan) (meditation.WatermarkGate, error) {
	if held, ok := p.reflectionInEffect().gate.(*meditation.AnchorStore); ok && held.Path() == plan.Anchors {
		return held, nil
	}
	return meditation.NewAnchorStore(plan.Anchors)
}

// installWatermark points the run's anchor store at the record this loop reads,
// so the watermark the diagnostics report is the watermark the driver advances. A
// loop that installed no store of its own — a document that enables no
// meditation, or a mount that runs before the port is bound to its run — leaves
// the store the run holds as it stands.
func (p *hostMeditationPort) installWatermark(gate meditation.WatermarkGate) {
	if p.run == nil || gate == nil {
		return
	}
	p.run.installWatermark(gate)
}

// installReflection installs one reflection loop as the loop in effect and
// answers the loop it replaced beside whether a loop was in effect at all.
func (p *hostMeditationPort) installReflection(next meditationReflection) (meditationReflection, bool) {
	p.reflection.Lock()
	defer p.reflection.Unlock()
	previous, held := p.face, p.installed
	p.face, p.installed = next, true
	return previous, held
}

// restoreReflection puts one reflection loop back as the loop in effect, together
// with the anchor store it read, so a refused candidate leaves the watermark of
// the loop in service in place. A previous loop that held no gate (a boot without
// meditation) clears the watermark the refused candidate installed: install
// directly so a nil gate resets rather than early-returning.
func (p *hostMeditationPort) restoreReflection(previous meditationReflection) {
	p.reflection.Lock()
	p.face = previous
	p.reflection.Unlock()
	if p.run != nil {
		p.run.installWatermark(previous.gate)
	}
}

// reflectionInEffect reports the reflection loop of the generation in effect.
func (p *hostMeditationPort) reflectionInEffect() meditationReflection {
	if p == nil {
		return meditationReflection{}
	}
	p.reflection.RLock()
	defer p.reflection.RUnlock()
	return p.face
}

// reflectionInstalled reports whether a generation installed a reflection loop
// on this port at all.
func (p *hostMeditationPort) reflectionInstalled() bool {
	if p == nil {
		return false
	}
	p.reflection.RLock()
	defer p.reflection.RUnlock()
	return p.installed
}

// driverInEffect reports the driver the commits of this run are dispatched to.
// A port whose document enables no meditation answers nil, and a commit is then
// observed by no reflection.
func (p *hostMeditationPort) driverInEffect() *meditation.Driver {
	return p.reflectionInEffect().driver
}

// planInEffect reports the meditation plan the generation in effect declares:
// the plan the native face of one generation is built from beside the delivery
// face of the port. A port built beside no enabled meditation answers nil.
func (p *hostMeditationPort) planInEffect() *meditation.Plan {
	if p == nil {
		return nil
	}
	p.reflection.RLock()
	defer p.reflection.RUnlock()
	return p.face.plan
}

// stopReflection ends the reflection loop in effect, once: the driver stops
// observing commits, a pass already in flight is cancelled and joined, and a
// second stop finds nothing to stop.
func (p *hostMeditationPort) stopReflection() {
	if p == nil {
		return
	}
	if driver := p.reflectionInEffect().driver; driver != nil {
		driver.Stop()
	}
}

// generationReflection is the plugin instance this run observes commits through:
// it forwards every commit to the reflection driver of the generation in effect,
// so a reload that moves the meditation section replaces the driver behind an
// instance the run already holds. It is the instance a host stops on close.
type generationReflection struct {
	port *hostMeditationPort
}

var (
	_ plugin.Instance           = (*generationReflection)(nil)
	_ plugin.PostCommitObserver = (*generationReflection)(nil)
)

// newGenerationReflection returns the stable reflection instance of one port.
func newGenerationReflection(port *hostMeditationPort) *generationReflection {
	return &generationReflection{port: port}
}

// Name reports the stable name of the reflection instance: the name every
// reflection driver answers, because the instance serves the driver's port.
func (g *generationReflection) Name() string { return "meditation-driver" }

// Port reports the consumption the reflection serves: one committed fact in,
// one observation out.
func (g *generationReflection) Port() plugin.Port { return plugin.PortFactObservation }

// Material declares what this reflection reads, when it reads it and what a failed
// reading leaves behind: one committed fact read after its commit, where a failed
// reflection is a named gap and the fact keeps standing.
func (g *generationReflection) Material() plugin.Material {
	return plugin.Material{
		Reads:   plugin.PortFactObservation,
		Timing:  plugin.TimingPostCommit,
		Failure: plugin.RecordGap,
	}
}

// ObserveCommit forwards one commit to the driver of the generation in effect.
func (g *generationReflection) ObserveCommit(ctx context.Context, id event.EventID) error {
	if g == nil || g.port == nil {
		return nil
	}
	driver := g.port.driverInEffect()
	if driver == nil {
		return nil
	}
	return driver.ObserveCommit(ctx, id)
}

// Stop ends the reflection loop in effect.
func (g *generationReflection) Stop() {
	if g == nil || g.port == nil {
		return
	}
	g.port.stopReflection()
}

// ActiveSessions reports the run's live bindings of one agent, without the
// observing session of the reflection itself.
func (p *hostMeditationPort) ActiveSessions(agentID event.AgentID) []event.Address {
	if p.run == nil {
		return nil
	}
	sessions := p.run.ActiveSessions(agentID)
	out := make([]event.Address, 0, len(sessions))
	for _, addr := range sessions {
		if p.plan != nil && addr.AgentID == event.AgentID(p.plan.Agent) && addr.SessionID == event.SessionID(p.plan.Session) {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// hostInjection is the body an inbound channel posts to the task endpoint: the
// messages the turn reads and the lineage the channel declares for it. A
// non-empty Session spells an inject-only session of its own instead of
// reusing the chat session the host admitted last.
type hostInjection struct {
	Messages      []hostInjectionMessage `json:"messages"`
	TriggerSource string                 `json:"trigger_source"`
	Session       string                 `json:"session"`
}

// hostInjectionMessage is one message of an injection: the role it speaks for
// and its content.
type hostInjectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// userText reports the user content the injection carries: the last non-empty
// user message is the one a turn reads.
func (in hostInjection) userText() string {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		msg := in.Messages[i]
		if msg.Role != string(model.RoleUser) {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return msg.Content
		}
	}
	return ""
}
