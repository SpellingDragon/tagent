// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
//
// SettleDriver is the event-driven face of one unattended settle: it observes
// the delivery facts this run commits for a terminal task notification and
// drives one follow-up turn at the user session the task belongs to.
//
// The trigger surface is closed, and the anti-loop is semantic rather than a
// blacklist:
//
//   - Only the terminal kinds settled, failed and cancelled fire; accepted and progress do not.
//   - Only a delivery fact carries the settle-lineage meta key, so a backflow settle never fires.
//   - The fired turn commits no settle fact with a lineage, so it can never fire itself again.
//   - A notice that arrives while a follow-up is in flight is retained, in arrival order, and re-evaluated when that turn ends: it is never dropped.
package tagent

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// terminalSettleKinds is the closed table of notification kinds that end a
// task: the same three kinds the loop answers as terminal on its backflow. The
// full notice vocabulary is accepted, progress, settled, failed and cancelled;
// accepted and progress are kept out of this table on purpose, so a task that
// is still running never turns the runtime.
var terminalSettleKinds = map[task.NoticeKind]bool{
	task.NoticeSettled:   true,
	task.NoticeFailed:    true,
	task.NoticeCancelled: true,
}

// noticeKindOf reports the notification kind one fact reports, read from the
// mechanical key the committing path stamps. A fact committed before the key
// was spelled falls back to its settle lineage, so the two spellings of the same
// lineage never disagree about the kind.
func noticeKindOf(fact event.Event) task.NoticeKind {
	if kind := fact.Meta[event.MetaKeyNoticeKind]; kind != "" {
		return task.NoticeKind(kind)
	}
	return task.NoticeKind(fact.Meta[event.MetaKeySettleTriggerSource])
}

// noticeTaskIDOf reports the task one fact reports on: the typed identity the
// fact carries, and otherwise the mechanical key its metadata stamped.
func noticeTaskIDOf(fact event.Event) event.TaskID {
	if fact.TaskID != "" {
		return fact.TaskID
	}
	return event.TaskID(fact.Meta[event.MetaKeyTaskID])
}

// ErrSettleDriverUnwired reports an observation on a driver whose fact lookup,
// user-address memory or follow-up face was never wired: a gap by name instead
// of a silent no-op that reads as a delivered turn.
var ErrSettleDriverUnwired = errors.New("tagent: the settle driver is not wired")

// settleNotice is one observation this driver owes a turn: the address the
// follow-up runs at beside the fact that asked for it. A notice retained while
// a turn is in flight keeps its fact, so the turn that re-evaluates it reads the
// very fact the delivery committed instead of a reconstruction of it.
type settleNotice struct {
	addr event.Address
	fact event.Event
}

// SettleDriver is the fact->observation face of the one turn a terminal notice
// fires at the session it belongs to. It reads the fact one commit identity
// names, judges it against the terminal table above, and runs the injected
// follow-up face when the owning session carries a remembered user address.
type SettleDriver struct {
	// lookup reads the fact one commit identity names: the commit notice
	// carries an identity, and the fact itself is read back through the chain
	// the run already holds.
	lookup func(ctx context.Context, id event.EventID) (event.Event, error)

	// userAddressOf reports the user address the host remembered for one
	// session: the follow-up turn runs at that explicit address and never at a
	// most-recent one.
	userAddressOf func(session event.SessionID) (event.Address, bool)

	// fire drives the one follow-up turn the settle asks for.
	fire func(ctx context.Context, addr event.Address, fact event.Event) error

	stopCtx    context.Context
	stopCancel context.CancelFunc

	mu      sync.Mutex
	firing  bool
	stopped bool
	// retained holds the notices that arrived while a follow-up was in flight,
	// in arrival order: the rearm after that turn takes the next one, so a
	// notice is deferred by a turn and never lost to one.
	retained []settleNotice
	wg       sync.WaitGroup
}

var (
	_ plugin.Instance           = (*SettleDriver)(nil)
	_ plugin.PostCommitObserver = (*SettleDriver)(nil)
	_ interface{ Stop() }       = (*SettleDriver)(nil)
)

// NewSettleDriver assembles the settle face over the fact lookup of one run,
// the user-address memory of one host and the follow-up drive of that host.
func NewSettleDriver(
	lookup func(context.Context, event.EventID) (event.Event, error),
	userAddressOf func(event.SessionID) (event.Address, bool),
	fire func(context.Context, event.Address, event.Event) error,
) *SettleDriver {
	d := &SettleDriver{
		lookup:        lookup,
		userAddressOf: userAddressOf,
		fire:          fire,
	}
	d.stopCtx, d.stopCancel = context.WithCancel(context.Background())
	return d
}

// Name reports the stable name of this plugin instance.
func (d *SettleDriver) Name() string { return "settle-driver" }

// Port reports the consumption this driver serves: one committed fact in, one
// observation out. The observation is the follow-up turn the fact may fire.
func (d *SettleDriver) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// ObserveCommit reads the fact one commit wrote and fires the follow-up turn the
// terminal notice asks for.
//
// The turn itself runs detached from the committing delivery: a follow-up
// spends a model turn, and the flush that delivered the notification never waits
// for it. A notice that arrives while a turn is in flight joins the driver's
// retained queue and is re-evaluated as soon as that turn ends, so the busy
// window defers the turn instead of dropping the notice. A fact that is not a
// terminal delivery fact, or whose session carries no remembered user address,
// is ignored in silence: those are not settles nobody drives, they are settles
// somebody already read or addresses the host was never asked to turn. A failed
// read is reported back and never rolls the committed fact back.
func (d *SettleDriver) ObserveCommit(ctx context.Context, id event.EventID) error {
	if d == nil || !d.live() {
		return nil
	}
	if d.lookup == nil || d.userAddressOf == nil || d.fire == nil {
		return ErrSettleDriverUnwired
	}
	fact, err := d.lookup(ctx, id)
	if err != nil {
		return err
	}
	if fact.Type != event.TypeTaskSettled || noticeTaskIDOf(fact) == "" {
		return nil
	}
	if !terminalSettleKinds[noticeKindOf(fact)] {
		return nil
	}
	addr, ok := d.userAddressOf(fact.Owner.SessionID)
	if !ok || !addr.IsComplete() {
		return nil
	}
	notice := settleNotice{addr: addr, fact: fact}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	if d.firing {
		d.retained = append(d.retained, notice)
		d.mu.Unlock()
		return nil
	}
	d.firing = true
	d.wg.Add(1)
	d.mu.Unlock()

	go d.fireFact(ctx, notice)
	return nil
}

// fireFact runs the follow-up turns this driver owes, one after another, and
// settles the driver's firing state when the queue runs dry. A turn outlives
// the delivery that observed it but not the driver: a stop cancels it, and Stop
// waits for it to return. Every turn after the first is one the rearm took from
// the retained queue, so a notice that arrived while a turn was in flight is
// re-evaluated here instead of being dropped by it.
func (d *SettleDriver) fireFact(ctx context.Context, first settleNotice) {
	defer d.wg.Done()

	if ctx != nil {
		ctx = context.WithoutCancel(ctx)
	} else {
		ctx = context.Background()
	}
	for notice := first; ; {
		d.fireOne(ctx, notice)
		next, again := d.rearm()
		if !again {
			return
		}
		notice = next
	}
}

// fireOne drives one follow-up turn under its own cancellation: a stop ends the
// turn in flight, and the next retained notice is only taken after this one
// returned.
func (d *SettleDriver) fireOne(ctx context.Context, notice settleNotice) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(d.stopCtx, cancel)
	defer stop()

	if err := d.fire(ctx, notice.addr, notice.fact); err != nil {
		assemblyWarn("tagent: the settle follow-up of %s ended by error: %v", notice.addr.SessionID, err)
	}
}

// rearm reports the next retained notice of this driver, and whether one is
// owed at all. The queue is drained in arrival order; a stop or an empty queue
// ends the firing and clears what a stop left behind, so the driver rests until
// the next commit.
func (d *SettleDriver) rearm() (settleNotice, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || len(d.retained) == 0 {
		d.firing = false
		d.retained = nil
		return settleNotice{}, false
	}
	next := d.retained[0]
	d.retained = d.retained[1:]
	return next, true
}

// live reports whether the driver still accepts commits.
func (d *SettleDriver) live() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.stopped
}

// Stop ends the driver: no further commit fires a turn, a turn in flight is
// cancelled, and Stop waits for it to return. A repeated stop is a no-op.
func (d *SettleDriver) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	d.retained = nil
	d.mu.Unlock()
	d.stopCancel()
	d.wg.Wait()
}
