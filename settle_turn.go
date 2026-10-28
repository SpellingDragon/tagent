// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
//
// SettleDriver is the event-driven face of one unattended settle: it observes
// the delivery facts this run commits for a terminal task notification and
// drives one follow-up turn at the address the notification froze its routing
// to.
//
// The trigger surface is closed, and the anti-loop is semantic rather than a
// blacklist:
//
//   - Only the terminal kinds settled, failed and cancelled fire; accepted and progress do not.
//   - Only a delivery fact carries the settle-lineage meta key, so a backflow settle never fires.
//   - The fired turn commits no settle fact with a lineage, so it can never fire itself again.
//   - The mailbox is the one account that answers whether work waits: a notification waits there until a turn claims it, drives it and consumes it on that same account, so one notification starts one turn however often it is observed.
//   - The attempt record is the second fence a restart reads: a turn that already landed its terminal is closed with its consumption receipt instead of driven again, so a stop between the terminal fact and the receipt replays no second turn.
package tagent

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/state"
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

// settleCompletedTerminal is the one terminal a follow-up turn reports when its
// notification was delivered: the settled, failed and cancelled notifications
// all ask the same question of the turn, and a turn that ended on any other
// terminal never delivered anything.
var settleCompletedTerminal = model.KindCompleted.String()

// settleAttempts is the attempt face one mailbox may serve beside the seats it
// owes: the durable record of the one follow-up turn a notification started,
// beside the terminal that turn ended on. A mailbox that serves it lets the pump
// tell a turn that already landed from one that never reached its terminal, so a
// restart closes the landed turn with its receipt instead of driving a second
// one. A mailbox that serves no attempt face keeps the bare at-least-once
// replay: a restart drives the owed entry again.
type settleAttempts interface {
	// Attempt reports the attempt record of one notification and whether the
	// account holds one at all.
	Attempt(ctx context.Context, notice task.Notice) (state.SettleAttempt, bool, error)
	// RecordAttempt records one attempt of a notification's follow-up turn: the
	// empty terminal marks the turn in flight, a terminal the one it ended on.
	RecordAttempt(ctx context.Context, notice task.Notice, attempt state.SettleAttempt) error
}

// settleBarrier is the account position one follow-up turn closes, carried on the
// context the driver hands its fire face: the terminal the turn ended on and the
// consumption receipt of its entry are written in the same barrier the terminal
// fact landed behind, before the drive returns and before the answer is
// delivered. A fire face that reads no barrier still settles its seat through the
// pump's own fallback beside the returned turn.
type settleBarrier struct {
	// notice is the notification the driven turn answers, in its own four-part
	// identity: task, round, kind and sequence.
	notice task.Notice
	// settle records the terminal the turn ended on as the attempt of its
	// notification.
	settle func(ctx context.Context, terminal model.Kind) error
	// consume writes the consumption receipt of the entry the turn drove.
	consume func(ctx context.Context) error
}

// settleBarrierKey is the context key the barrier of one follow-up drive travels
// under.
type settleBarrierKey struct{}

// withSettleBarrier stamps the barrier of one follow-up drive on the context its
// fire face reads.
func withSettleBarrier(ctx context.Context, barrier settleBarrier) context.Context {
	return context.WithValue(ctx, settleBarrierKey{}, barrier)
}

// settleBarrierFrom reports the barrier one drive carries and whether one is
// stamped: a user turn carries none.
func settleBarrierFrom(ctx context.Context) (settleBarrier, bool) {
	barrier, ok := ctx.Value(settleBarrierKey{}).(settleBarrier)
	return barrier, ok
}

// ErrSettleDriverUnwired reports an observation on a driver whose fact lookup,
// mailbox account or follow-up face was never wired: a gap by name instead of
// a silent no-op that reads as a delivered turn.
var ErrSettleDriverUnwired = errors.New("tagent: the settle driver is not wired")

// ErrNoticeTurnIncomplete reports a follow-up turn that ended without the
// completed delivery its notification owed: a stopped or failed turn hands its
// mailbox entry back unconsumed, so the work stays owed and the next wake drives
// it again, while only a turn that really delivered closes the entry beside its
// consumption receipt.
var ErrNoticeTurnIncomplete = errors.New("tagent: the follow-up turn did not complete its delivery")

// SettleWake is the mailbox account the settle pump drives its turns from: one
// face that answers whether work waits and moves one entry through claim,
// release and consumption on the same ledger. The state store behind one run
// satisfies it.
//
// The redline list (4 rules):
// MUST start exactly one follow-up turn per claimed entry and close it beside its consumption receipt.
// MUST close an entry whose recorded attempt already landed its terminal with that turn's receipt and drive no second turn.
// MUST NOT replay an entry whose consumption receipt is already recorded.
// MUST NOT hold a claimed entry beside a turn that stopped before its delivery.
type SettleWake interface {
	// RestoreOwed folds the unconsumed work a durable account still owes back
	// into the mailbox, so a stopped run's notices wait for the next decision
	// instead of dying with the process that accepted them.
	RestoreOwed(ctx context.Context) error
	// Unconsumed reports every mailbox entry whose consumption has not
	// completed, in the one order the account answers in.
	Unconsumed() []state.WorkSeat
	// Claim takes one pending entry for this executor alone, so one
	// notification starts one turn however often it is observed.
	Claim(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
	// Release hands one claimed entry back to the pending pool, so an executor
	// that stopped beside a claim keeps the work owed instead of holding it.
	Release(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
	// Consume records the completed consumption of one claimed entry, on the
	// same account the claim was taken from.
	Consume(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
}

// SettleDriver is the fact->observation face of the one turn a terminal notice
// fires at the address its routing froze. An observation wakes the pump, and
// the pump reads the mailbox account: every entry there is claimed, driven
// beside the committed fact its notification points at, and consumed on that
// same account once the turn returned. The driver keeps no queue of its own,
// so nothing it holds can be lost by a stop or a restart.
type SettleDriver struct {
	// lookup reads the fact one commit identity names: the commit notice
	// carries an identity, and the fact itself is read back through the chain
	// the run already holds.
	lookup func(ctx context.Context, id event.EventID) (event.Event, error)

	// wake resolves the mailbox account the pump drives: the store one run
	// assembles, resolved when the turn is driven and never before.
	wake func() SettleWake

	// fire drives the one follow-up turn the settle asks for.
	fire func(ctx context.Context, addr event.Address, fact event.Event) error

	stopCtx    context.Context
	stopCancel context.CancelFunc

	mu      sync.Mutex
	stopped bool
	// wakeCh carries the wake signals the observations leave behind; one slot
	// is enough, because a wake only asks the pump to read the account again
	// and the account itself never lies about the work it holds.
	wakeCh chan struct{}
	wg     sync.WaitGroup
}

var (
	_ plugin.Instance           = (*SettleDriver)(nil)
	_ plugin.PostCommitObserver = (*SettleDriver)(nil)
	_ interface{ Stop() }       = (*SettleDriver)(nil)

	// The settle face declares the driver duty: a fact that fires a settle leaves
	// the follow-up turn owed in the mailbox account, so a failed observation of
	// this face is an owed obligation rather than a missing reading.
	_ interface{ DrivesObligation() } = (*SettleDriver)(nil)
)

// NewSettleDriver assembles the settle face over the fact lookup of one run,
// the mailbox account of that run and the follow-up drive of its host. The
// pump starts beside the driver and rests until a wake or an observation asks
// it to read the account.
func NewSettleDriver(
	lookup func(context.Context, event.EventID) (event.Event, error),
	wake func() SettleWake,
	fire func(context.Context, event.Address, event.Event) error,
) *SettleDriver {
	d := &SettleDriver{
		lookup: lookup,
		wake:   wake,
		fire:   fire,
		wakeCh: make(chan struct{}, 1),
	}
	d.stopCtx, d.stopCancel = context.WithCancel(context.Background())
	d.wg.Add(1)
	go d.pump()
	return d
}

// Name reports the stable name of this plugin instance.
func (d *SettleDriver) Name() string { return "settle-driver" }

// Port reports the consumption this driver serves: one committed fact in, one
// observation out. The observation is the follow-up turn the fact may fire.
func (d *SettleDriver) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// DrivesObligation marks this face's duty: the observation wakes the pump, and the
// work it woke is owed on the mailbox account, so a failed observation leaves the
// follow-up turn owed and the account hands it to the next decision.
func (d *SettleDriver) DrivesObligation() {}

// ObserveCommit reads the fact one commit wrote and wakes the pump when the
// fact is one the settle trigger table fires on. The observation itself starts
// nothing: the mailbox is the one account that answers whether work waits, so
// a repeated observation of one delivered notification claims no second turn
// — the entry is either consumed already or claimed by the turn it started. A
// fact that is not a terminal delivery fact is ignored in silence: those are
// not settles nobody drives, they are settles somebody already read. A failed
// read is reported back and never rolls the committed fact back.
func (d *SettleDriver) ObserveCommit(ctx context.Context, id event.EventID) error {
	if d == nil || d.stoppedNow() {
		return nil
	}
	if d.lookup == nil || d.wake == nil || d.fire == nil {
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
	d.Wake()
	return nil
}

// Wake asks the pump to read the mailbox account again. The boot calls it once
// the restored account is in place, and any number of wakes collapse into the
// work the account already carries: a wake starts no turn of its own.
func (d *SettleDriver) Wake() {
	if d == nil {
		return
	}
	select {
	case d.wakeCh <- struct{}{}:
	default:
	}
}

// pump is the one goroutine that drives the follow-up turns of this driver,
// one after another: a wake signal asks it to read the mailbox account, and
// every entry it finds there is driven on the account itself. A stop ends the
// pump and the turn in flight beside it, and the account keeps everything the
// turns did not consume.
func (d *SettleDriver) pump() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stopCtx.Done():
			return
		case <-d.wakeCh:
		}
		d.drain()
	}
}

// seatKey is the account position one drive takes its entry from: the site and
// the acceptance sequence, which name one entry and nothing else.
type seatKey struct {
	addr     event.Address
	instance event.InstanceID
	seq      state.AcceptSeq
}

// drain drives every turn the mailbox owes until it answers no more work. Each
// entry is claimed for this executor alone, driven beside the fact its
// notification points at, and consumed on the same account once the turn
// returned. A claim another consumer holds is skipped rather than stolen, so
// one notification starts one turn however often it is observed, and an entry
// a failed turn handed back is left to the next wake instead of retried in a
// tight loop beside the failure it just reported.
func (d *SettleDriver) drain() {
	ctx := d.stopCtx
	tried := map[seatKey]bool{}
	for {
		if ctx.Err() != nil {
			return
		}
		wake := d.wake()
		if wake == nil {
			return
		}
		if err := wake.RestoreOwed(ctx); err != nil {
			assemblyWarn("tagent: the settle restore of the mailbox reported: %v", err)
		}
		seats := wake.Unconsumed()
		worked := false
		for _, seat := range seats {
			if ctx.Err() != nil {
				return
			}
			if !terminalSettleKinds[seat.Notice.Kind] || seat.Claim != state.ClaimPending {
				continue
			}
			key := seatKey{addr: seat.Address, instance: seat.Instance, seq: seat.AcceptSeq}
			if tried[key] {
				continue
			}
			if err := wake.Claim(ctx, seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
				continue
			}
			tried[key] = true
			worked = true
			d.driveSeat(ctx, wake, seat)
		}
		if !worked {
			return
		}
	}
}

// driveSeat drives the one follow-up turn one mailbox seat owes and settles the
// entry on the same account its claim was taken from: before any turn is driven
// the in-flight marker of the attempt lands durably, so a process that starts
// over the same account reads the attempt rather than a blank seat, and the turn
// runs beside the committed fact the notification points at. The entry is closed
// beside the terminal the turn landed through the barrier this drive stamps on
// the context its fire face reads. A turn that returned settles the entry as
// consumed, and a turn that failed hands the entry back so the next wake retries
// it. A seat whose recorded attempt already landed a completed terminal is closed
// with its receipt and no turn is driven for it: the receipt the stopped process
// never wrote is written here, so one notification starts one turn however many
// processes over the same account read it. A stop ends the turn in flight and
// leaves the entry unconsumed, so the work survives the stop instead of dying with
// it; beside a turn that completed, the consumption receipt is written without
// the pump's cancellation and closes the entry even as the driver stops.
func (d *SettleDriver) driveSeat(ctx context.Context, wake SettleWake, seat state.WorkSeat) {
	fact, err := d.lookup(ctx, settleDeliveryKey(seat.Notice))
	if err != nil {
		assemblyWarn("tagent: the settle fact of %s could not be read: %v", seat.Notice.FactKey(), err)
		d.releaseSeat(wake, seat)
		return
	}
	if fact.Type != event.TypeTaskSettled || noticeTaskIDOf(fact) == "" || !terminalSettleKinds[noticeKindOf(fact)] {
		d.releaseSeat(wake, seat)
		return
	}

	attempts, _ := wake.(settleAttempts)
	if attempts != nil {
		landed, held, err := attempts.Attempt(ctx, seat.Notice)
		switch {
		case err != nil:
			assemblyWarn("tagent: the settle attempt of %s could not be read: %v", seat.Notice.FactKey(), err)
			d.releaseSeat(wake, seat)
			return
		case held && landed.Terminal == settleCompletedTerminal:
			if err := wake.Consume(context.WithoutCancel(ctx), seat.Address, seat.Instance, seat.AcceptSeq); err != nil && !errors.Is(err, state.ErrClaimSettled) {
				assemblyWarn("tagent: the settle consumption of %s beside its landed attempt reported: %v", seat.Notice.FactKey(), err)
			}
			return
		}
		if err := attempts.RecordAttempt(ctx, seat.Notice, state.SettleAttempt{}); err != nil {
			assemblyWarn("tagent: the settle attempt of %s was not recorded: %v", seat.Notice.FactKey(), err)
			d.releaseSeat(wake, seat)
			return
		}
	}

	fireCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(d.stopCtx, cancel)
	defer stop()

	barrier := settleBarrier{
		notice: seat.Notice,
		settle: func(bctx context.Context, terminal model.Kind) error {
			if attempts == nil {
				return nil
			}
			return attempts.RecordAttempt(context.WithoutCancel(bctx), seat.Notice, state.SettleAttempt{Terminal: terminal.String()})
		},
		consume: func(bctx context.Context) error {
			return wake.Consume(context.WithoutCancel(bctx), seat.Address, seat.Instance, seat.AcceptSeq)
		},
	}
	if err := d.fire(withSettleBarrier(fireCtx, barrier), seat.Address, fact); err != nil {
		assemblyWarn("tagent: the settle follow-up of %s ended by error: %v", seat.Address.SessionID, err)
		d.releaseSeat(wake, seat)
		return
	}
	if err := wake.Consume(context.WithoutCancel(ctx), seat.Address, seat.Instance, seat.AcceptSeq); err != nil && !errors.Is(err, state.ErrClaimSettled) {
		assemblyWarn("tagent: the settle consumption of %s beside the finished turn reported: %v", seat.Notice.FactKey(), err)
	}
}

// releaseSeat hands one entry this drive claimed back to the pending pool, so
// the work stays owed and the next wake picks it up again.
func (d *SettleDriver) releaseSeat(wake SettleWake, seat state.WorkSeat) {
	if err := wake.Release(context.WithoutCancel(context.Background()), seat.Address, seat.Instance, seat.AcceptSeq); err != nil {
		assemblyWarn("tagent: the settle release of %s reported: %v", seat.Notice.FactKey(), err)
	}
}

// stoppedNow reports whether the driver has been stopped.
func (d *SettleDriver) stoppedNow() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped
}

// Stop ends the driver: no further commit wakes the pump, the turn in flight
// is cancelled, and Stop waits for the pump to return. What the mailbox still
// holds is kept as it stands — an unconsumed notice outlives the stop, so the
// next process resumes the turn it owes. A repeated stop is a no-op.
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
	d.mu.Unlock()
	d.stopCancel()
	d.wg.Wait()
}
