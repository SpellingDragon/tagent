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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
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
// call names is re-entered with the body, and the outcome is reported in the same three
// states every delivery reports through. The face is the seam between the tool and the
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
	tasks         hostReentryTasks
	ledger        hostReentryLedger
	continuations hostReentryContinuations
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
	// Serve serves the next round of the claimed task with the body.
	Serve(ctx context.Context, taskID event.TaskID, input string) error
}

// Bind attaches the assembled run the port re-enters tasks through.
func (p *hostSendReentryPort) Bind(run *Run) {
	p.tasks, p.ledger, p.continuations = nil, nil, nil
	if run == nil {
		return
	}
	if run.registry != nil {
		p.tasks = run.registry
	}
	if run.delegations != nil {
		p.ledger = run.delegations
	}
	if run.continuations != nil {
		p.continuations = run.continuations
	}
}

// ReenterTask hands one body to the named task and reports the outcome of that
// re-entry: a settled task of this host is reactivated into a new round, the
// ledger entry of the task is moved to that round, and the round body is handed to
// the delegated turn the task was first served in. The call is answered as
// delivered once that round owns the input; the round itself runs beside the
// receipt exactly as the round of a fresh admission does, so the turn that issued
// the call keeps deciding while the task it re-entered runs.
//
// Every read the round depends on is settled before the first mutation, and the one
// round that will carry the body is in this call's hand before it answers:
// - The settled status design D7 admits and the terminal ledger entry the reopen needs are both read first, so a refusal moves nothing at all.
// - The claim is what makes the receipt true. Without it the call could answer "ok" for a body a second re-entry also took, or for a round that never opened, and the body would be lost behind a receipt that said otherwise.
// - The round is opened last, and the round a later step refuses is read against the one step that still had to race: the round is put back rather than leaving the task in flight with no turn to serve it.
// - A round that ends without recording its terminal puts the task and the ledger back and leaves the body owed to the next round, so a body the receipt accepted is never dropped.
// - Only a task this host serves a round of is re-entered: an identity the table holds no record of, a task whose delegated round this host never served, and every status design D7 refuses are each answered as a named refusal rather than reactivated into a round nobody asked for.
func (p *hostSendReentryPort) ReenterTask(ctx context.Context, taskID event.TaskID, content string) (deliveryOutcome, string) {
	if p.tasks == nil || p.ledger == nil {
		return deliveryRefused, "the host is not assembled"
	}
	continuations := p.continuations
	if continuations == nil {
		return deliveryRefused, hostReentryCause(taskID, subagent.ErrContinuationMissing)
	}
	if err := p.tasks.Reentrant(ctx, taskID); err != nil {
		return deliveryRefused, hostReentryCause(taskID, err)
	}
	if err := p.ledger.CanReopenRound(taskID); err != nil {
		return deliveryRefused, hostReentryCause(taskID, err)
	}
	if err := continuations.Claim(taskID); err != nil {
		return deliveryRefused, hostReentryCause(taskID, err)
	}
	round, err := p.tasks.Reactivate(ctx, taskID)
	if err != nil {
		continuations.Abandon(taskID)
		return deliveryRefused, hostReentryCause(taskID, err)
	}
	if err := p.ledger.ReopenRound(taskID, round); err != nil {
		continuations.Abandon(taskID)
		p.unwindReentry(ctx, taskID, round, err)
		return deliveryRefused, hostReentryCause(taskID, err)
	}
	go func() {
		if err := continuations.Serve(context.WithoutCancel(ctx), taskID, content); err != nil {
			p.unwindReentry(ctx, taskID, round, err)
		}
	}()
	return deliverySent, ""
}

// unwindReentry puts a round nothing served back: the task returns to the settled
// outcome the reactivation re-entered it from, the ledger entry returns to the
// terminal it reopened from, and the cause is reported rather than left in the log
// of a body the caller was told had been taken. The body itself stays owed to the
// next round of the task, so the re-entry loses the round and never the input.
func (p *hostSendReentryPort) unwindReentry(ctx context.Context, taskID event.TaskID, round int64, cause error) {
	ctx = context.WithoutCancel(ctx)
	if err := p.tasks.RollbackReactivation(ctx, taskID, round); err != nil {
		assemblyWarn("tagent: the round %d of %s was opened and never served, and the task was not put back: %v", round, taskID, err)
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
// delivery face keeps and answered in the three states every delivery reports
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
	if string(frame.Address.AgentID) != p.plan.agent {
		return deliveryRefused, fmt.Sprintf("agent %q carries no cross-session delivery face: the white list this host declares belongs to %q",
			frame.Address.AgentID, p.plan.agent)
	}
	if !hostSendNames(p.plan.targets, string(addr.AgentID)) {
		return deliveryRefused, fmt.Sprintf("the deliver_to white list of %q names no %q, and a body delivered outside it would reach a session the reflection never declared",
			p.plan.agent, addr.AgentID)
	}
	if !hostSendNames(p.plan.observed, string(addr.AgentID)) {
		return deliveryRefused, fmt.Sprintf("the observed face of %q does not reach %q, so a body delivered there would be blind",
			p.plan.agent, addr.AgentID)
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

// deliver commits one body into the target chain and owes the target a round: an idle
// session is driven through one turn beside the receipt, and a busy one reads the fact
// at its own next decision boundary because the drive already in flight owns the range.
// - The fact identity is the delivery identity, so the same body re-presented under the same call identity lands no second fact: the commit answers the identity it already holds instead of writing beside it.
func (p *hostMeditationPort) deliver(ctx context.Context, frame task.CallFrame, addr event.Address, content string) hostSendSessionReceipt {
	loop := p.run.loop
	if loop == nil {
		return hostSendSessionReceipt{outcome: deliveryRefused, cause: "this host drives no loop, so the body would land beside no turn"}
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
	busy := hostSendSessionBusy(p.run, addr)
	key, err := p.run.commitOnly(ctx, addr, evt)
	if err != nil {
		return hostSendSessionReceipt{outcome: deliveryRefused,
			cause: fmt.Sprintf("the session %q of agent %q did not take the body: %v", addr.SessionID, addr.AgentID, err)}
	}
	if busy {
		return hostSendSessionReceipt{outcome: deliverySent}
	}
	evt.Key = key
	instance := p.run.instance
	go func() {
		if _, err := loop.Drive(context.WithoutCancel(ctx), agent.Invocation{
			InvocationID: evt.InvocationID,
			Address:      addr,
			Instance:     instance,
			Ready:        []event.Event{evt},
		}); err != nil {
			assemblyWarn("tagent: the sent body %s of %s ended beside: %v", evt.InvocationID, addr.SessionID, err)
		}
	}()
	return hostSendSessionReceipt{outcome: deliverySent}
}

// hostSendSessionBusy reports whether a model decision of the target session is in
// flight, which is the one-slot gate the delivery reads: a busy target adopts the body
// at its own boundary, and an idle target is driven through a round of its own.
func hostSendSessionBusy(run *Run, addr event.Address) bool {
	_, _, busy := run.ModelInFlight(addr, run.instance)
	return busy
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

// hostSendSessionInvocation is the invocation identity one delivery commits and drives
// under: derived from the call itself, so the same delivery always names the same range
// and a replay never opens a second one.
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

// DeliveryReceiptSent is the one receipt the delivery account records: the body
// reached the chain of the target it named. It is the frozen word of the delivery
// receipt entry, so both delivery families read the same receipt for a body that
// landed and a receipt no fact states reads as this one.
const DeliveryReceiptSent = "sent"

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

// DeliveryLedger is the delivery account of one assembled run (design D9): the
// projections of the delivery facts the run committed, read back by source event
// and by target. It holds no state of its own — the facts are the account, and the
// tier the run keeps makes them durable — so a query never parses a body: the four
// entries, the identity and the type of the fact are all it reads.
type DeliveryLedger struct {
	run *Run
}

// DeliveryLedger reports the delivery account of this run, or nil beside no run.
func (r *Run) DeliveryLedger() *DeliveryLedger {
	if r == nil {
		return nil
	}
	return &DeliveryLedger{run: r}
}

// Records reports every delivery the run accounts for, ordered by source event,
// then by target and identity, so two reads of one chain answer the same order.
// One identity answers one record: a repeated delivery of the identity the chain
// already holds lands no second fact and therefore no second record.
func (l *DeliveryLedger) Records() []DeliveryRecord {
	if l == nil || l.run == nil {
		return nil
	}
	facts := l.run.hostDeliveryFacts()
	records := make([]DeliveryRecord, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		record, held := hostDeliveryRecordOf(fact)
		if !held {
			continue
		}
		if _, repeated := seen[record.Identity]; repeated {
			continue
		}
		seen[record.Identity] = struct{}{}
		records = append(records, record)
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
	return records
}

// BySource reports the deliveries one source event is accounted for: every target
// the event reached, each beside its own identity and receipt.
func (l *DeliveryLedger) BySource(source event.EventID) []DeliveryRecord {
	var out []DeliveryRecord
	for _, record := range l.Records() {
		if record.Source == source {
			out = append(out, record)
		}
	}
	return out
}

// ByTarget reports the deliveries one address received, whichever family carried
// them.
func (l *DeliveryLedger) ByTarget(addr event.Address) []DeliveryRecord {
	var out []DeliveryRecord
	for _, record := range l.Records() {
		if record.Target == addr {
			out = append(out, record)
		}
	}
	return out
}

// hostDeliveryFacts reports the facts this run holds, in commit order, as a copy
// the caller owns.
func (r *Run) hostDeliveryFacts() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.Event, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		evt, held := r.facts[key]
		if !held {
			continue
		}
		out = append(out, evt)
	}
	return out
}

// hostDeliveryRecordOf projects one committed fact into its delivery record and
// reports whether the fact is a delivery at all.
// - A fact stamped with a delivery identity is one, whichever family wrote it: the entries it carries are read back exactly as the committing path stated them.
// - A task settle fact is one too: it is the fact a terminal notification lands as, its identity is the notification identity the follow-up turn drives from, and a fact committed before the delivery entries existed carries none of them, so its record states the receipt the landed fact is.
// - Every other fact is no delivery: an agent output, a tool result and an attention fact are not bodies handed to a target.
func hostDeliveryRecordOf(fact event.Event) (DeliveryRecord, bool) {
	if identity := fact.Meta[event.MetaKeyDeliveryIdentity]; identity != "" {
		receipt := fact.Meta[event.MetaKeyDeliveryReceipt]
		if receipt == "" {
			receipt = DeliveryReceiptSent
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

// hostMeditationPlan is the one reflection section a document enables: the
// observing agent, the loop session, the cadence and floor, the static prompt,
// the observed and read namespaces, the declared delivery targets and the
// anchor record the watermark persists through.
type hostMeditationPlan struct {
	agent    string
	session  string
	interval time.Duration
	minGap   time.Duration
	prompt   string
	observed []string
	reads    []string
	targets  []string
	anchors  string
}

// hostMeditationDefaultInterval is the tick cadence of a declaration that names
// none.
const hostMeditationDefaultInterval = 30 * time.Minute

// resolveHostMeditation resolves the one meditation section a document enables:
// the entry agent first and the remaining agents in declaration order, so one
// document resolves to one plan across restarts. A document enabling none
// reports a nil plan, and every inconsistent section is refused by name.
func resolveHostMeditation(doc document.Document, promptDir, storageRoot string) (*hostMeditationPlan, error) {
	var plan *hostMeditationPlan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec, ok := doc.Agents[id]
		if !ok || !spec.Meditation.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("tagent: agents %q and %q both enable meditation, and one host drives one reflection loop",
				declaredBy, id)
		}
		entry := spec.Meditation
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation interval %s", id, interval)
		}
		if interval == 0 {
			interval = hostMeditationDefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation min_gap %s", id, entry.MinGap)
		}
		prompt, err := hostMeditationPrompt(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := meditation.ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("tagent: %w", err)
		}
		targets, err := hostMeditationTargets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("tagent: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist", id)
		}
		plan = &hostMeditationPlan{
			agent:    id,
			session:  hostSessionPrefix(doc.Entry) + "meditation",
			interval: interval,
			minGap:   entry.MinGap,
			prompt:   prompt,
			observed: scope.Names,
			reads:    reads,
			targets:  targets,
			anchors:  filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// hostMeditationPrompt reads the static prompt one enabled meditation carries.
// A relative path resolves beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly.
func hostMeditationPrompt(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("tagent: agent %q enables meditation but names no prompt_file", id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("tagent: agent %q cannot read the meditation prompt %s: %w", id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("tagent: agent %q declares an empty meditation prompt %s", id, path)
	}
	return string(body), nil
}

// hostMeditationTargets resolves the delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and a section that names no target is refused by name.
func hostMeditationTargets(doc document.Document, id string, declared []string) ([]string, error) {
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("tagent: agent %q delivers its meditation to the undeclared agent %q", id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, trimmed)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("tagent: agent %q enables meditation but names no deliver_to target", id)
	}
	return targets, nil
}

// hostSessionPrefix reports the session identity prefix of one entry agent: the
// label the deployment names, and the host default beside none.
func hostSessionPrefix(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "tagent-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
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
	plan *hostMeditationPlan
	run  *Run

	// mu brackets one delivery: its four gates, the fact it commits and the drive it
	// decides to owe are resolved while no other delivery interleaves, so two bodies
	// aimed at one idle session never open two turns beside each other. It also guards
	// receipts, the outcome one call identity already answered.
	mu       sync.Mutex
	receipts map[string]hostSendSessionReceipt
}

var (
	_ meditation.SessionQuery = (*hostMeditationPort)(nil)
	_ hostSendSession         = (*hostMeditationPort)(nil)
)

// Bind attaches the assembled run the port reads and delivers through.
func (p *hostMeditationPort) Bind(run *Run) { p.run = run }

// ActiveSessions reports the run's live bindings of one agent, without the
// observing session of the reflection itself.
func (p *hostMeditationPort) ActiveSessions(agentID event.AgentID) []event.Address {
	if p.run == nil {
		return nil
	}
	sessions := p.run.ActiveSessions(agentID)
	out := make([]event.Address, 0, len(sessions))
	for _, addr := range sessions {
		if p.plan != nil && addr.AgentID == event.AgentID(p.plan.agent) && addr.SessionID == event.SessionID(p.plan.session) {
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
