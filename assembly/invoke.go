package assembly

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Invoke drives one invocation through the persistent event loop: the input is
// aggregated into the range's own transcript, every decision borrows the
// binding generation effective at its own start, and the settled tool
// notifications backflow to this invocation to drive its next decision. The
// turn reports the decisions it ran and the terminal the latest one reduced to.
// One address Consumer may batch inputs while preserving each source invocation;
// Run.Invoke waits for the segment that actually consumed its own input facts.
//
// The composition root establishes the session site of the explicit address on
// this instance and grants the diagnostic read face of the run a read of it, so
// the site is owned and read back from the state owner rather than mirrored
// beside it. Every input message commits as an entry fact of the range before
// the drive begins, so the turn-start boundary is one committed fact the
// observation reads back.
// The redline list (3 rules):
// MUST aggregate the input into the range's own transcript and never batch two invocations of one session.
// MUST borrow the binding generation effective at each decision's own start.
// MUST NOT let an admission receipt stand for the result of the call.
func (r *Run) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error) {
	if req.InvocationID == "" {
		return agent.Turn{}, errors.New("tagent: the invocation carries no identity")
	}
	if err := r.establish(ctx, req.Address); err != nil {
		return agent.Turn{}, err
	}
	keys := make([]event.EventID, 0, len(req.Input))
	now := time.Now()
	for index, msg := range req.Input {
		evt := r.inputEventOf(req, msg, index, req.InvocationID, now)
		key, err := r.commitFactChecked(ctx, evt)
		if err != nil {
			return agent.Turn{}, fmt.Errorf("tagent: commit the input fact: %w", err)
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(req.Input) == 0 {
		evt := r.inputEventOf(req, model.Message{Role: model.RoleUser}, 0, req.InvocationID, now)
		key, err := r.commitFactChecked(ctx, evt)
		if err != nil {
			return agent.Turn{}, fmt.Errorf("tagent: commit the empty invocation fact: %w", err)
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return r.waitResident(ctx, req.Address, req.InvocationID, keys)
}

// waitResident lets the address Consumer answer the committed inputs. Run.Invoke
// and Run.InjectEvent share this one wait instead of driving a second loop.
func (r *Run) waitResident(ctx context.Context, addr event.Address, invocationID event.InvocationID, keys []event.EventID) (agent.Turn, error) {
	consumer := r.loop.WakeAddress(addr)
	if consumer == nil {
		return agent.Turn{InvocationID: invocationID, Terminal: model.KindCancelled}, agent.ErrSegmentCancelled
	}
	answer, err := consumer.WaitAnswer(ctx, keys)
	return answer.Turn, err
}

// Close closes the runtime's own side for the named session address: the request
// is judged first, new input is refused next, the run quiesces to its fixed
// point, and only then does the session side close. The close reports one of
// three states: a run at rest, a run that spent its budget with work still on
// the chain, or a run it could not judge. The session side closes after the
// quiesce, so the mailbox bookkeeping an in-flight tool result still lands
// through stays open exactly as long as the quiesce may need it.
//
// The one absolute budget of the request covers every join and teardown the
// close takes: the resident consumption loops are marked first — without
// cancelling anything they fly, so the obligations they hold are finished for
// real inside the budget or stay named on the chain — and joined under the
// same deadline, so a close beside a producer that never stops answers Cut
// beside that producer's name instead of waiting on it without an end. A
// repeated close retries the quiesce the same way until the run reaches rest.
// The redline list (5 rules):
// MUST judge the timeout of the close before any side effect it would take, so a close naming none is refused with the run untouched.
// MUST refuse new input before the first quiesce of the close.
// MUST mark the resident consumption loops the loop mounted for itself before the first read of the rest criterion, and join them inside the budget the request named, so a segment those loops still fly is waited out or named — never unbounded before Cut and never cancelled by the close itself.
// MUST judge the run by the one rest criterion, never by an emptied queue or an elapsed wait.
// MUST NOT report a run at rest while a writer has not stopped.
func (r *Run) Close(ctx context.Context, req agent.CloseRequest) (agent.DrainReport, error) {
	timeout, timeoutErr := req.Resolve()
	if timeoutErr != nil {
		return agent.DrainReport{Outcome: agent.CloseBroken}, timeoutErr
	}
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	defer r.markCloseReturned()
	if flying := r.loop.StopResidentsWithin(timeout); len(flying) > 0 {
		assemblyWarn("tagent: the close budget ended beside %d resident loop(s) still inside a model production: %s", len(flying), strings.Join(flying, ", "))
	}
	judge := context.WithoutCancel(ctx)
	read := r.quiesce(ctx, timeout)
	cut := r.cutApprovalWaits(judge, read)
	if len(cut) > 0 {
		read = r.Unsettled(judge)
	}
	stopped, writers := r.writers.Stopped()
	landing := r.landingOwed()
	drained := len(cut) == 0 && read.Resting() && stopped && !landing
	report := agent.DrainReport{
		Unsettled: mergeUnsettled(cut, r.unsettledObligations(read), agent.WriterObligations(writers), landingObligations(landing)),
		Drained:   drained,
		Outcome:   closeOutcome(read, drained),
		Failed:    r.failedAnswers(),
	}
	if req.Address.IsComplete() {
		if closeErr := r.store.CloseSession(ctx, req.Address); closeErr != nil && !errors.Is(closeErr, state.ErrUnknownTarget) {
			return agent.DrainReport{}, closeErr
		}
	}
	if report.Outcome == agent.CloseBroken {
		return report, brokenCloseReason(read)
	}
	return report, nil
}

// closeGate is the admission gate of the delegation ledger: a delegated call is
// turned away by name once the close of this run has begun, exactly as a native
// call through the tool gate is, so no new obligation opens beside the quiesce.
// Every other face is the task face of the run unchanged.
type closeGate struct {
	run  *Run
	face agent.DelegationTasks
}

// The gate fronts the task face of one run.
var _ agent.DelegationTasks = closeGate{}

// Admit admits one delegated call through the task face, or refuses it by name
// once the close of this run has begun.
func (g closeGate) Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	if g.run.refusing() {
		return task.AdmissionReceipt{CallID: spec.CallID, Accepted: false, Kind: task.AdmissionRejected},
			fmt.Errorf("%w: the close of this run refuses new calls", agent.ErrShutdownRefused)
	}
	return g.face.Admit(ctx, spec)
}

// Advance moves one task to its next status through the task face.
func (g closeGate) Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error) {
	return g.face.Advance(ctx, taskID, to, mutation, outbox)
}

// Obligations reports the obligations one task still owes through the task face.
func (g closeGate) Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error) {
	return g.face.Obligations(ctx, taskID)
}

// Flush delivers the notifications one task still owes through the task face.
func (g closeGate) Flush(ctx context.Context, taskID event.TaskID) error {
	return g.face.Flush(ctx, taskID)
}

// brokenCloseReason names why one close could not judge the run: the criterion
// that could not be read.
func brokenCloseReason(read task.Unsettled) error {
	return fmt.Errorf("%w: %s", agent.ErrDrainUnsettled, read.Unjudged)
}

// quiesce runs one close to its fixed point: while the rest criterion answers
// work outstanding, a writer of the run has not confirmed its stop, or the
// delegation ledger still holds a terminal it has not landed, the owed
// notifications are offered again, the terminals that cleared are landed and the
// drivers of the run are woken when the outstanding set changed, so the derived
// turns of the run converge without a new input. The writer barrier is read here
// beside the criterion, so a drive or a model in flight is waited out by name
// instead of being taken for an ended run. The loop re-reads the account on the
// progress pulse of the run and returns when its own budget arrives, so the wait
// is an event and never a fixed-interval poll. A run that reached rest is
// answered at once, and a run that spent its budget is answered as it stands.
// - The criterion is read beside a context that does not carry the caller's cancellation: a cancelled close is out of budget and answers Cut where the run stands, never Broken.
func (r *Run) quiesce(ctx context.Context, timeout time.Time) task.Unsettled {
	judge := context.WithoutCancel(ctx)
	var budget <-chan time.Time
	if !timeout.IsZero() {
		timer := time.NewTimer(time.Until(timeout))
		defer timer.Stop()
		budget = timer.C
	}
	woken := ""
	for {
		read := r.Unsettled(judge)
		stopped, _ := r.writers.Stopped()
		if read.Resting() && stopped && !r.landingOwed() {
			return read
		}
		if ctx.Err() != nil || budgetSpent(timeout) {
			return read
		}
		r.offerOwed(ctx)
		if digest := settleDigest(read); digest != woken {
			woken = digest
			r.wakeOwedDrivers()
		}
		select {
		case <-ctx.Done():
			return r.Unsettled(judge)
		case <-budget:
			return r.Unsettled(judge)
		case <-r.Progress():
		}
	}
}

// cutApprovalWaits cancels the tasks one close leaves waiting on an approval no
// restart can rejoin, and answers the items the close itself landed for them. A
// hold waits on an approval this process holds in memory alone: the task names an
// approval identity and a digest, never the call and its arguments, so a wait none
// of this process's holds carries is a wait no restart can rejoin nor put to a
// decision, and a task left in it is work no later process can finish. The close
// therefore cancels it and lands the fact, so the account ends the wait in a state
// a restart reads as an outcome instead of reopening a wait nobody can settle.
//
// - Only a run that did not reach rest is judged: a quiesce that reached rest leaves no wait behind, so an approval still open is only cut beside work that stayed open.
// - A wait one of this process's holds still carries is left standing: a ruling here can resume it, so the close reports it and the question keeps its place in the presence.
// - The mutation identity is derived from the task, so a cancelled task that was already cut is answered as the fact the chain holds instead of a second cancel.
// - The item is reported beside the run's own, naming the task and the address, so the cancel is never a silent edit of the account.
func (r *Run) cutApprovalWaits(ctx context.Context, read task.Unsettled) []agent.UnsettledObligation {
	if read.Unjudged != "" {
		return nil
	}
	face := closeFace{manager: r.manager, registry: r.registry}
	var cut []agent.UnsettledObligation
	for _, work := range read.Work {
		if work.Status != task.StatusAwaitingApproval {
			continue
		}
		if r.waitsOn(work.TaskID) {
			continue
		}
		mutation := task.MutationID("cut-approval:" + string(work.TaskID))
		if _, err := face.Advance(ctx, work.TaskID, task.StatusCancelled, mutation, nil); err != nil {
			assemblyWarn("tagent: the approval wait of task %s was not cancelled beside the close: %v", work.TaskID, err)
			continue
		}
		assemblyWarn("tagent: the close cancelled task %s, whose approval wait no restart can rejoin", work.TaskID)
		cut = append(cut, agent.UnsettledObligation{
			TaskID:  work.TaskID,
			Address: work.Address,
			Reason:  reasonApprovalCut,
		})
	}
	return cut
}

// waitsOn reports whether a hold of this process still waits on the task named.
// A wait a hold carries is one a ruling can resume in this process, so the close
// leaves it standing for that ruling; a wait no hold carries is a wait only the
// chain states, and no ruling of this process or the next can resume it.
func (r *Run) waitsOn(taskID event.TaskID) bool {
	r.mu.Lock()
	books := append([]*task.HoldBook(nil), r.holds...)
	r.mu.Unlock()
	for _, book := range books {
		if _, waiting := book.Waiting(taskID); waiting {
			return true
		}
	}
	return false
}

// reasonApprovalCut names the outcome of a task whose approval wait the close
// ended because no restart can rejoin that wait.
const reasonApprovalCut = "the approval this task waited on cannot be rejoined by a restart, so the close cancelled it"

// budgetSpent reports whether the budget the close may spend has already
// arrived. A zero budget waits without one.
func budgetSpent(timeout time.Time) bool {
	return !timeout.IsZero() && !time.Now().Before(timeout)
}

// noteProgress reports that the account behind this run moved. The report
// merges: a pulse that lands while a close reads the account keeps the close
// turning once more instead of queueing a second look at the same movement.
func (r *Run) noteProgress() {
	if r == nil || r.progress == nil {
		return
	}
	select {
	case r.progress <- struct{}{}:
	default:
	}
}

// Progress reports the channel a close waits on: one pulse per movement of the
// account, merged so a close never misses the movement it has not read yet.
func (r *Run) Progress() <-chan struct{} {
	if r == nil {
		return nil
	}
	return r.progress
}

// settleDigest names the outstanding set as one string, so a quiesce wakes the
// drivers when that set changed and never in a loop beside work that keeps
// failing: a seat handed back to the pool is the same outstanding set it was.
func settleDigest(read task.Unsettled) string {
	var digest strings.Builder
	for _, work := range read.Work {
		digest.WriteString("work:" + string(work.TaskID) + ";")
	}
	for _, notice := range read.Notices {
		digest.WriteString("notice:" + notice.Notice.FactKey() + ";")
	}
	for _, input := range read.Inputs {
		digest.WriteString("input:" + string(input.Event) + ";")
	}
	return digest.String()
}

// Unsettled reports the rest criterion of this run: the work it accepted and has
// not finished beside the notifications no consumption confirmed. The close, the
// restart and the report read it and no other, so the task chain and the
// consumption ledger are judged once.
func (r *Run) Unsettled(ctx context.Context) task.Unsettled {
	return task.ReadUnsettled(ctx, runUnsettled{run: r}, r.fence)
}

// fence reports the movement of this run, so the sets of one evaluation of the
// criterion belong to one instant of every account the criterion reads. The
// sequence is advanced at the write point of each account — the task chain and
// its obligations, the mailbox, the session chains — so a judgment that unions
// them is fenced over the accounts that actually hold them, and not over one
// account's own sequence read as if the others moved with it. A read that spans
// a write is re-read; a sequence that never holds under a read is answered
// unjudged rather than as sets of different instants.
func (r *Run) fence(context.Context) uint64 {
	return r.movement.Read()
}

// Rejections reports the feedback fact of every notification a target refused,
// as the state account of this run holds it: the notification each refusal
// answers beside the routing it was offered to and the reason the target gave. A
// refusal is a consumption result and never an obligation the criterion waits on,
// so the read is the diagnostic face of what the account already answered.
func (r *Run) Rejections() []state.Rejection {
	if r == nil || r.store == nil {
		return nil
	}
	return r.store.Rejections()
}

// runUnsettled is the rest criterion source of one run: the chain the registry
// holds, the notifications no consumption confirmed beside the mailbox the store
// accepted, and the input rounds the session chains of the run hold with no
// answer.
type runUnsettled struct {
	run *Run
}

// Unfinished reports the tasks this run accepted and has not finished.
func (s runUnsettled) Unfinished(ctx context.Context) ([]task.UnsettledWork, error) {
	return s.run.registry.Unfinished(ctx)
}

// Unconsumed reports the notifications no consumption confirmed, from the two
// accounts this run owns: the notifications its tasks still owe a delivery for,
// leaving out the ones a target already refused, and the ones a mailbox accepted
// and nobody consumed. A refusal is the consumption result of its notification and
// the account keeps it as the feedback fact it is, so the rest criterion reads
// that fact instead of waiting on a delivery no target will ever accept.
func (s runUnsettled) Unconsumed(ctx context.Context) ([]task.UnsettledNotice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owed, err := s.run.manager.Owed(ctx)
	if err != nil {
		return nil, err
	}
	if s.run.store == nil {
		return owed, nil
	}
	unconsumed := make([]task.UnsettledNotice, 0, len(owed))
	for _, notice := range owed {
		if s.run.store.Refused(notice.Notice) {
			continue
		}
		unconsumed = append(unconsumed, notice)
	}
	for _, seat := range s.run.store.Unconsumed() {
		unconsumed = append(unconsumed, task.UnsettledNotice{
			Notice:   seat.Notice,
			Address:  seat.Address,
			Instance: seat.Instance,
			Reason:   reasonMailboxUnconsumed,
		})
	}
	return unconsumed, nil
}

// Unanswered reports the input rounds the session chains of this run hold with no
// answer: every driving fact the chain committed that stands past the round its
// last answer closed. The position is read back from the chain itself, so an
// input this process accepted and never drove is owed a drive rather than lost
// beside a cursor kept in memory — and a restart over the same chain reads the
// very same rounds, because the boundary is the one fact the chain records.
//
// The rounds split by the cut of the newest checkpoint: the rounds it covered
// come back from the carrier by identity and are read out of the durable chain
// the run holds, and the rounds that landed past it are the resident window of
// each chain. A run beside no storage tier restores no cut and reads the window
// alone.
func (s runUnsettled) Unanswered(ctx context.Context) ([]task.UnsettledInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var unanswered []task.UnsettledInput
	for _, addr := range s.run.ActiveSessions("") {
		facts := s.run.restoredInputFacts(ctx, addr)
		facts = append(facts, s.run.SessionFacts(addr)...)
		pending := agent.PendingInputs(facts)
		if len(pending) == 0 {
			continue
		}
		var instance event.InstanceID
		if s.run.store != nil {
			instance = s.run.store.InstanceOf(addr)
		}
		for _, evt := range pending {
			unanswered = append(unanswered, task.UnsettledInput{
				Address:  addr,
				Instance: instance,
				Event:    evt.Key,
				Source:   evt.Source,
				Reason:   reasonInputUnanswered,
			})
		}
	}
	return unanswered, nil
}

// reasonInputUnanswered names the wait an input round stands in while the chain
// holds it and no answer closed it.
const reasonInputUnanswered = "the session chain holds this input round with no answer"

// reasonMailboxUnconsumed names the wait a notification stands in while it sits
// in a mailbox entry no consumption confirmed.
const reasonMailboxUnconsumed = "the mailbox holds this notification unconsumed"

// failedAnswers reports the failed answers the close settled as notification
// consumption results, each by the invocation whose turn ended failed, in the
// order the chain committed them. The failure observation is the one fact the
// settle face lands beside a failed answer, so the read names exactly the
// answers a receipt closed: a turn that failed for another reason carries the
// audit fact of its failure but no observation, and the report never names it as
// a settlement. The chain is the one source, so a restart over the same log names
// the same failures the first process settled instead of forgetting them.
func (r *Run) failedAnswers() []event.InvocationID {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[event.InvocationID]bool{}
	var failed []event.InvocationID
	for _, key := range r.factOrder {
		fact, held := r.facts[key]
		if !held || fact.Type != event.TypeFeedback || fact.Meta[event.MetaKeyEventSummary] != failureObservationSummary {
			continue
		}
		invocation := event.InvocationID(fact.Meta[event.MetaPrefix+"invocation"])
		if invocation == "" {
			invocation = fact.InvocationID
		}
		if invocation == "" || seen[invocation] {
			continue
		}
		seen[invocation] = true
		failed = append(failed, invocation)
	}
	return failed
}

// unsettledObligations renders the rest criterion as the per-address report of
// one close: every piece of unfinished work and every unconsumed notification
// beside the address it stands at and the reason it stayed open.
func (r *Run) unsettledObligations(read task.Unsettled) []agent.UnsettledObligation {
	out := make([]agent.UnsettledObligation, 0, len(read.Work)+len(read.Notices)+len(read.Inputs))
	if read.Unjudged != "" {
		out = append(out, agent.UnsettledObligation{Reason: read.Unjudged})
	}
	for _, work := range read.Work {
		out = append(out, agent.UnsettledObligation{
			TaskID:  work.TaskID,
			Address: work.Address,
			Reason:  "the task stands at " + statusWordOf(work.Status),
		})
	}
	for _, notice := range read.Notices {
		out = append(out, agent.UnsettledObligation{
			TaskID:  notice.Notice.TaskID,
			Address: notice.Address,
			Source:  notice.Source,
			FactKey: notice.Notice.FactKey(),
			Reason:  notice.Reason,
		})
	}
	for _, input := range read.Inputs {
		out = append(out, agent.UnsettledObligation{
			Address: input.Address,
			Source:  input.Source,
			FactKey: string(input.Event),
			Reason:  input.Reason,
		})
	}
	return out
}

// statusWordOf renders the status one unfinished task holds as the word a close
// report prints beside it.
func statusWordOf(status task.Status) string {
	switch status {
	case task.StatusAccepted:
		return "accepted"
	case task.StatusRunning:
		return "running"
	case task.StatusAwaitingApproval:
		return "awaiting-approval"
	case task.StatusSettled:
		return "settled"
	case task.StatusFailed:
		return "failed"
	case task.StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// mergeUnsettled unions the per-address items one close reports, each item once:
// one address, task and reason name one piece of unsettled work however many
// faces of the close saw it, so a report answers the work that stands open and
// never one entry per face that noticed it. The reason is part of the identity: a
// task that stands open for two different waits is two items, and one face that
// named a wait another did not is the one that has to be read.
func mergeUnsettled(sets ...[]agent.UnsettledObligation) []agent.UnsettledObligation {
	merged := make([]agent.UnsettledObligation, 0, len(sets[0]))
	seen := make(map[string]bool, len(sets[0]))
	for _, set := range sets {
		for _, entry := range set {
			key := string(entry.Address.AgentID) + "\x00" + string(entry.Address.SessionID) + "\x00" +
				string(entry.TaskID) + "\x00" + entry.Reason
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, entry)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		left, right := merged[i], merged[j]
		if left.Address.AgentID != right.Address.AgentID {
			return left.Address.AgentID < right.Address.AgentID
		}
		if left.Address.SessionID != right.Address.SessionID {
			return left.Address.SessionID < right.Address.SessionID
		}
		if left.TaskID != right.TaskID {
			return left.TaskID < right.TaskID
		}
		if left.FactKey != right.FactKey {
			return left.FactKey < right.FactKey
		}
		return left.Reason < right.Reason
	})
	return merged
}

// closeOutcome names the state one close ended in: a run at rest is quiesced, a
// run whose criterion answered outstanding work is cut, and a run whose
// criterion could not be read is broken.
func closeOutcome(read task.Unsettled, drained bool) agent.CloseOutcome {
	switch {
	case read.Unjudged != "":
		return agent.CloseBroken
	case read.Resting() && drained:
		return agent.CloseQuiesced
	default:
		return agent.CloseCut
	}
}

// refusing reports whether the close of this run has begun, so a new call is
// turned away at the admission gate before any obligation opens beside the
// drain.
func (r *Run) refusing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closing
}

// markCloseReturned records that a close of this run has returned to its caller.
// It is the completion state the sink reads: work that lands while the close still
// runs is handed to the consumers the quiesce is finishing, and work that lands
// after the close returned is kept owed instead of being handed to a stopped
// current.
func (r *Run) markCloseReturned() {
	r.mu.Lock()
	r.closeReturned = true
	r.mu.Unlock()
}

// closeFinished reports whether a close of this run has already returned to its
// caller, so a late result cannot drive a range of this process.
func (r *Run) closeFinished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeReturned
}

// offerOwed offers the notifications the tasks of this run still owe beside
// their frozen routing and lands the terminals whose obligations cleared: a
// flush delivers them, only an explicit acceptance clears one, and a pending
// terminal lands only beside a cleared booking. It reads and settles through the
// task faces alone and keeps no bookkeeping of its own. The tasks it walks are
// every identity this run held beside every task the registry still answers
// unfinished, so a delegated task the commit face never listed is offered too.
func (r *Run) offerOwed(ctx context.Context) {
	for _, id := range r.closeTasks(ctx) {
		_ = r.manager.Flush(ctx, id)
		if r.delegations != nil {
			_, _ = r.delegations.Reconcile(ctx, id)
		}
	}
}

// closeTasks lists the task identities one close settles through, in identity
// order: the ones this run held, beside the ones the registry still answers
// unfinished.
func (r *Run) closeTasks(ctx context.Context) []event.TaskID {
	ids := map[event.TaskID]bool{}
	for _, id := range r.commit.TaskIDs() {
		ids[id] = true
	}
	if work, err := r.registry.Unfinished(ctx); err == nil {
		for _, entry := range work {
			ids[entry.TaskID] = true
		}
	}
	out := make([]event.TaskID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// landingOwed reports whether the delegation ledger still holds a booking whose
// terminal has not landed: the landing is part of the criterion, so a close
// never reports a run at rest beside an unlanded terminal.
func (r *Run) landingOwed() bool {
	return r.delegations != nil && r.delegations.Pending() > 0
}

// landingObligations renders the pending booking of the delegation ledger as the
// one report entry a close names when that booking is what kept it from rest.
func landingObligations(pending bool) []agent.UnsettledObligation {
	if !pending {
		return nil
	}
	return []agent.UnsettledObligation{{Reason: reasonTerminalNotLanded}}
}

// reasonTerminalNotLanded names the wait a delegation booking stands in while the
// terminal it holds has not landed.
const reasonTerminalNotLanded = "the delegation ledger has not landed this terminal"

// wakeOwedDrivers asks every driver of this run whose work the quiesce waits on
// to read its account again, so a derived turn owed by an accepted obligation
// starts without a new input driving it.
func (r *Run) wakeOwedDrivers() {
	for _, instance := range r.plugins {
		if driver, ok := instance.(interface {
			DrivesObligation()
			Wake()
		}); ok {
			driver.Wake()
		}
	}
}

// typeOf reports the frozen event type one native input message renders as.
func typeOf(msg model.Message) string {
	switch msg.Role {
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return event.TypeThinkingPlan
		}
		return event.TypeAgentOutput
	case model.RoleTool:
		return event.TypeActionCommand
	default:
		return event.TypeExternalInput
	}
}

// inputEventOf renders one native input message as the entry fact of a range:
// the frozen type of the message beside the address, identity and instance the
// range stamps on it, and the metadata of that message when the caller names
// one. The identity is the range the message is input of, so a message admitted
// beside a range already running commits under that range and is read at that
// drive's next decision boundary.
func (r *Run) inputEventOf(req InvokeRequest, msg model.Message, index int, invocationID event.InvocationID, now time.Time) event.Event {
	evt := event.Event{
		Type:         typeOf(msg),
		Timestamp:    now,
		Message:      msg,
		Owner:        req.Address,
		Source:       req.Address,
		Target:       req.Address,
		InvocationID: invocationID,
		InstanceID:   r.instance,
		EpisodeID:    req.Episode,
	}
	evt.Meta = req.inputMetaOf(index)
	return evt
}

// inputMetaOf merges the per-message metadata of one input beside the
// round-level trigger source: the message keeps the entries that name where it
// came from, and the round stamp is written last so no per-message entry can
// overwrite the lineage the caller declared. A message that names no entry of
// its own carries the round stamp alone, and a range that declares neither
// carries no metadata at all, so an assembly that names neither behaves exactly
// as it did before the face existed.
func (req InvokeRequest) inputMetaOf(index int) map[string]string {
	var named map[string]string
	if index >= 0 && index < len(req.InputMeta) {
		named = req.InputMeta[index]
	}
	var meta map[string]string
	for key, value := range named {
		if key == event.MetaKeyTriggerSource {
			continue
		}
		if meta == nil {
			meta = make(map[string]string, len(named)+1)
		}
		meta[key] = value
	}
	if req.TriggerSource != "" {
		if meta == nil {
			meta = make(map[string]string, 1)
		}
		meta[event.MetaKeyTriggerSource] = req.TriggerSource
	}
	return meta
}
