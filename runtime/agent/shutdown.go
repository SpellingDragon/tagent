package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// shutdownPollInterval is the pause between two settlement passes of one drain.
// The close polls real settlement instead of assuming it, so the interval only
// shapes how often it looks.
const shutdownPollInterval = 2 * time.Millisecond

// reasonProducerNotStopped is the named reason beside a producer that has not
// confirmed its stop when a close reports what stayed open.
const reasonProducerNotStopped = "the producer has not confirmed its stop"

// ErrShutdownRefused, ErrDrainUnsettled, ErrProducerIdentityMissing and
// ErrProducerMissing are the named refusals of the close: each one names the
// case it refuses instead of answering a silent default.
var (
	// ErrShutdownRefused reports new input offered to an agent whose close has
	// begun. The close refuses new input first and admits nothing beside the
	// drain.
	ErrShutdownRefused = errors.New("agent: the close of this agent refuses new input")
	// ErrDrainUnsettled reports a close that ended with obligations still open.
	// An honest close reports this named unsettled state beside the reasons of
	// what stayed open instead of a clean end.
	ErrDrainUnsettled = errors.New("agent: the close did not reach real termination")
	// ErrProducerIdentityMissing reports a producer watched under no task
	// identity, so an unsettled report could never name the work it runs behind.
	ErrProducerIdentityMissing = errors.New("agent: the producer is watched under no task identity")
	// ErrProducerMissing reports a watch that names no producer.
	ErrProducerMissing = errors.New("agent: the watch names no producer")
)

// Reconciler is the delegation ledger the close lands the terminals of admitted
// calls through: a terminal whose obligations have cleared is reported and the
// booking beside it closes, while an obligation still open keeps the terminal
// withheld exactly as it does beside a live turn.
//
// The redline list (3 rules):
// MUST land a pending terminal only once the obligations of that call are cleared.
// MUST report every booking it still holds, so a close never reports a drain beside an unlanded terminal.
// MUST NOT hold a terminal past the obligations it waits on, and MUST NOT report one before them.
type Reconciler interface {
	// Reconcile attempts the landing of one admitted call's terminal again and
	// reports the status the record stands at afterwards.
	Reconcile(ctx context.Context, taskID event.TaskID) (task.Status, error)
	// Pending reports how many fresh admissions the ledger still books; each
	// booking closes only when its terminal really lands.
	Pending() int
}

// Shutdown is one agent's close face: the gate new work is admitted through,
// the ledger of the obligations that gate accepted, and the real stop
// confirmations of the producers behind that work.
//
// It fronts the task face the agent settles through as a DelegationTasks of its
// own, so the delegation ledger admits through the gate and a close refuses new
// input at the very point a new obligation would open. Everything already
// accepted keeps its obligation until real settlement: the close offers the
// notifications a task still owes beside their frozen routing, lands the
// terminals whose obligations cleared, and polls the producers until each one
// confirms its stop or the deadline arrives.
// The redline list (4 rules):
// MUST refuse new input at the gate before the first settlement pass of the close.
// MUST settle an accepted obligation only by real settlement, and report a drain only beside real termination.
// MUST NOT read a cancelled request, an emptied queue or an elapsed timeout as a settled obligation.
// MUST NOT report a clean drain beside an obligation or a production that stayed open.
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
type Shutdown struct {
	face DelegationTasks

	mu         sync.Mutex
	refusing   bool
	inflight   int
	admitted   map[event.TaskID]bool
	producers  map[event.TaskID][]func() bool
	reconciler Reconciler
	address    event.Address
	closing    bool
	finished   bool
	done       chan struct{}
	report     DrainReport
	err        error
}

// The close face settles its delegations through the very task face it fronts.
var _ DelegationTasks = (*Shutdown)(nil)

// NewShutdown returns the close face of one agent over the task face its work
// is admitted and settled through. The returned face is the one the agent's
// delegation ledger admits through, so the close can refuse new input at the
// gate where new obligations are opened and settle exactly what it accepted.
func NewShutdown(face DelegationTasks) *Shutdown {
	return &Shutdown{
		face:      face,
		admitted:  map[event.TaskID]bool{},
		producers: map[event.TaskID][]func() bool{},
	}
}

// SetReconciler attaches the delegation ledger the close lands the terminals of
// admitted calls through. The setter is separate from the constructor because
// the ledger admits through this very face: the face exists first and the
// ledger is built on it.
func (s *Shutdown) SetReconciler(reconciler Reconciler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconciler = reconciler
}

// Watch registers one in-flight producer behind the named task's execution: its
// real stop is one of the confirmations the close waits for, and a producer that
// never confirms keeps the drain open beside the task it runs behind. Several
// producers may stand behind one task and each confirms its own stop.
//
// The stopped answer is polled and only a true is taken as an end: a producer
// answering false has not stopped, and neither a cancelled request nor an
// emptied queue stands in for the confirmation. The producer answers for itself
// alone, and the registration carries the identity of the work it runs behind.
// The redline list (4 rules):
// MUST poll the stop confirmation until it answers true or the deadline arrives.
// MUST refuse a registration that names no work identity or carries no stop confirmation, by name.
// MUST NOT take a cancelled request, an emptied queue or an elapsed timeout as a stop.
// MUST NOT report a drain while any registered production has not confirmed its stop.
func (s *Shutdown) Watch(taskID event.TaskID, stopped func() bool) error {
	if taskID == "" {
		return ErrProducerIdentityMissing
	}
	if stopped == nil {
		return ErrProducerMissing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.producers[taskID] = append(s.producers[taskID], stopped)
	return nil
}

// Track records one already admitted task beside the close face, so the drain
// settles the obligations of that task and names them beside the deadline when
// they stay open. A call admitted through the task face of the run is recorded
// here at its admission exactly as a call admitted through this gate is, and
// recording is no admission of its own: a task recorded twice is the one task.
func (s *Shutdown) Track(taskID event.TaskID) {
	if taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admitted[taskID] = true
}

// Admit admits one new call as a task and returns its admission receipt, or
// refuses it by name once the close of this agent has begun. A fresh admission
// is recorded beside the close face so its obligation stays in the drain until
// real settlement; a dedup or a refusal added no obligation and settles none.
func (s *Shutdown) Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	s.mu.Lock()
	if s.refusing {
		address := s.address
		s.mu.Unlock()
		return task.AdmissionReceipt{CallID: spec.CallID, Accepted: false, Kind: task.AdmissionRejected},
			fmt.Errorf("%w: %s", ErrShutdownRefused, address)
	}
	s.inflight++
	s.mu.Unlock()

	receipt, err := s.face.Admit(ctx, spec)

	s.mu.Lock()
	s.inflight--
	if err == nil && receipt.Accepted && receipt.Kind == task.AdmissionFresh {
		s.admitted[receipt.TaskID] = true
	}
	s.mu.Unlock()
	return receipt, err
}

// Advance moves one task to the next status beside its pending notifications.
// A transition of work already accepted is settlement, so the gate never
// refuses it.
func (s *Shutdown) Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error) {
	return s.face.Advance(ctx, taskID, to, mutation, outbox)
}

// Obligations reports the obligations the task face still holds for one task,
// each classified by the wait it is in.
func (s *Shutdown) Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error) {
	return s.face.Obligations(ctx, taskID)
}

// Flush delivers the notifications of one task whose obligations are still
// open, beside the routing each envelope froze. Only an acceptance the target
// states explicitly clears one.
func (s *Shutdown) Flush(ctx context.Context, taskID event.TaskID) error {
	return s.face.Flush(ctx, taskID)
}

// Close refuses new input first, settles the obligations that were already
// accepted, and reports whether the drain reached real termination.
//
// The gate closes before the first settlement pass, so no new obligation opens
// beside the drain. An accepted obligation is settled only by real settlement —
// a notification its target explicitly accepted, a terminal that landed beside
// a cleared booking — and every watched producer must confirm its stop. A
// producer answering false has not stopped: the close keeps polling it until
// the confirmation arrives or the deadline does, and a deadline that arrives
// first returns the named unsettled state beside the reason of every
// obligation that stayed open. A second close returns the first result and runs
// no second settlement.
func (s *Shutdown) Close(ctx context.Context, req CloseRequest) (DrainReport, error) {
	s.mu.Lock()
	if s.finished {
		report, err := s.report, s.err
		s.mu.Unlock()
		return report, err
	}
	if s.closing {
		done := s.done
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		report, err := s.report, s.err
		s.mu.Unlock()
		return report, err
	}
	s.closing = true
	s.done = make(chan struct{})
	s.refusing = true
	s.address = req.Address
	s.mu.Unlock()

	report, err := s.drain(ctx, req)

	s.mu.Lock()
	s.report, s.err = report, err
	s.finished = true
	s.closing = false
	close(s.done)
	s.mu.Unlock()
	return report, err
}

// drain drives the settlement of every accepted obligation until real
// termination or the deadline, and reports the outcome beside the named
// unsettled state of anything that stayed open.
func (s *Shutdown) drain(ctx context.Context, req CloseRequest) (DrainReport, error) {
	for {
		if s.settlePass(ctx) {
			return DrainReport{Drained: true}, nil
		}
		if s.deadlineReached(req.Deadline) || ctx.Err() != nil {
			return s.unsettled(ctx), ErrDrainUnsettled
		}
		select {
		case <-ctx.Done():
			return s.unsettled(ctx), ErrDrainUnsettled
		case <-time.After(shutdownPollInterval):
		}
	}
}

// deadlineReached reports whether the deadline the close may spend has arrived.
// A zero deadline waits without one.
func (s *Shutdown) deadlineReached(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}

// settlePass drives one settlement attempt over everything the close accepted
// and reports whether it all really settled this time: the notifications a task
// still owes are offered beside their frozen routing, the terminals whose
// obligations cleared land beside their bookings, and every watched producer is
// polled for its real stop. An admission still in flight keeps the drain open,
// because its obligation is not settled before it is opened.
func (s *Shutdown) settlePass(ctx context.Context) bool {
	s.mu.Lock()
	if s.inflight > 0 {
		s.mu.Unlock()
		return false
	}
	tasks, producers, reconciler := s.snapshotLocked()
	s.mu.Unlock()

	settled := true
	for _, taskID := range tasks {
		_ = s.face.Flush(ctx, taskID)
		if reconciler != nil {
			_, _ = reconciler.Reconcile(ctx, taskID)
		}
		open, err := s.face.Obligations(ctx, taskID)
		if err != nil || open.Count > 0 {
			settled = false
		}
	}
	for _, list := range producers {
		for _, stopped := range list {
			if stopped != nil && !stopped() {
				settled = false
			}
		}
	}
	if reconciler != nil && reconciler.Pending() > 0 {
		settled = false
	}
	return settled
}

// snapshotLocked copies the settled-against view of the close: the tasks whose
// obligations it holds, the producers it confirms stops of, and the ledger it
// lands terminals through.
func (s *Shutdown) snapshotLocked() ([]event.TaskID, map[event.TaskID][]func() bool, Reconciler) {
	tasks := make([]event.TaskID, 0, len(s.admitted))
	for taskID := range s.admitted {
		tasks = append(tasks, taskID)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i] < tasks[j] })
	producers := make(map[event.TaskID][]func() bool, len(s.producers))
	for taskID, list := range s.producers {
		producers[taskID] = append([]func() bool(nil), list...)
	}
	return tasks, producers, s.reconciler
}

// unsettled reports what the close could not settle when it stopped waiting,
// each open obligation beside its named reason and each producer that has not
// confirmed its stop beside the task it runs behind.
func (s *Shutdown) unsettled(ctx context.Context) DrainReport {
	s.mu.Lock()
	tasks, producers, _ := s.snapshotLocked()
	s.mu.Unlock()

	report := DrainReport{Unsettled: []UnsettledObligation{}}
	for _, taskID := range tasks {
		open, err := s.face.Obligations(ctx, taskID)
		if err != nil {
			report.Unsettled = append(report.Unsettled, UnsettledObligation{TaskID: taskID, Reason: err.Error()})
			continue
		}
		for _, obligation := range open.Open {
			report.Unsettled = append(report.Unsettled, UnsettledObligation{
				TaskID: taskID,
				Reason: obligationReason(obligation, open.Blocked),
			})
		}
	}
	taskIDs := make([]event.TaskID, 0, len(producers))
	for taskID := range producers {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })
	for _, taskID := range taskIDs {
		for _, stopped := range producers[taskID] {
			if stopped != nil && !stopped() {
				report.Unsettled = append(report.Unsettled, UnsettledObligation{
					TaskID: taskID,
					Reason: reasonProducerNotStopped,
				})
			}
		}
	}
	sort.Slice(report.Unsettled, func(i, j int) bool {
		if report.Unsettled[i].TaskID != report.Unsettled[j].TaskID {
			return report.Unsettled[i].TaskID < report.Unsettled[j].TaskID
		}
		return report.Unsettled[i].Reason < report.Unsettled[j].Reason
	})
	return report
}

// obligationReason reports the named reason one open obligation stayed open:
// the reason its delivery was refused by when there is one, and the named wait
// it is still in otherwise.
func obligationReason(obligation task.Obligation, blocked []task.BlockedObligation) string {
	for _, entry := range blocked {
		if entry.Notice == obligation.Notice {
			return entry.Reason
		}
	}
	return fmt.Sprintf("the %s obligation has not settled", obligation.Kind)
}
