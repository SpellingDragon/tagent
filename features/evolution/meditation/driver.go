package meditation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Driver is the event-driven face of one reflection loop: it observes the facts
// the agent's own pipeline commits and runs a reflection pass only when one of
// them names an interaction someone awaits and the rhythm has elapsed.
//
// There is no ticker: a reflection is the answer to a new event, so a process
// with nothing new never fires one. The interval is a floor between two fires,
// never a clock that fires on its own. A pass the rhythm floor holds back keeps
// its episode open through the standing re-arm and evaluates again once the
// window opens, so a floor above zero still fires instead of waiting for a
// commit the held session may never send.
type Driver struct {
	reflection *Reflection
	interval   time.Duration
	minGap     time.Duration

	// modelInFlight reads the consumption state of one observed address: whether
	// a model request of that address is executing right now. It is the
	// consumption loop's own read, handed over as a function, so the driver
	// reports the request the loop actually flies rather than a copy of it.
	modelInFlight func(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool)
	// instance is the runtime instance the observed session sits in.
	instance event.InstanceID

	// lookup reads the fact one commit identity names: the commit notice carries
	// an identity, and the fact itself is read back through the observed chain.
	lookup func(ctx context.Context, id event.EventID) (event.Event, error)

	stopCtx    context.Context
	stopCancel context.CancelFunc

	mu        sync.Mutex
	lastFired time.Time
	// holdUntil is the moment the rhythm floor that held the latest pass back
	// opens the window: the standing re-arm measures its wait against it, so a
	// held episode re-evaluates when the floor elapses instead of spinning.
	holdUntil time.Time
	// owner is the address the latest host-facing commit names: the session whose
	// model request the next pass waits on.
	owner    event.Address
	firing   bool
	retained bool
	stopped  bool
	wg       sync.WaitGroup

	observer func(TickOutcome)
}

var (
	_ plugin.Instance           = (*Driver)(nil)
	_ plugin.PostCommitObserver = (*Driver)(nil)
	_ interface{ Stop() }       = (*Driver)(nil)
)

// NewDriver assembles the event-driven reflection face over one reflection loop,
// the floor between two fires, the floor between two consumed reflections, the
// read of whether a model request of the observed session is executing and the
// instance that request runs in.
//
// The interval is measured from the last fire; the first fire is never held
// back, so the first event of a process is answered at once, and a non-positive
// interval disables that floor. The
// min gap stays the reflection's own rhythm floor, read from the execution
// watermark, so a fired but not yet consumed reflection is never re-fired early.
// A nil consumption-state read or an empty instance leaves the pass its rhythm
// alone.
func NewDriver(reflection *Reflection, interval, minGap time.Duration,
	modelInFlight func(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool),
	instance event.InstanceID) *Driver {
	d := &Driver{
		reflection:    reflection,
		interval:      interval,
		minGap:        minGap,
		modelInFlight: modelInFlight,
		instance:      instance,
	}
	d.stopCtx, d.stopCancel = context.WithCancel(context.Background())
	if reflection != nil {
		d.lookup = reflection.CommittedFact
	}
	return d
}

// Name reports the stable name of this plugin instance.
func (d *Driver) Name() string { return "meditation-driver" }

// Port reports the consumption the driver serves: one committed fact in, one
// observation out. The observation is the reflection pass the fact may fire.
func (d *Driver) Port() plugin.Port {
	return plugin.PortFactObservation
}

// SetObserver attaches the reading of each pass this driver runs. It is the
// host's log face; a nil observer drops the readings.
func (d *Driver) SetObserver(observer func(TickOutcome)) {
	d.mu.Lock()
	d.observer = observer
	d.mu.Unlock()
}

// ObserveCommit reads the fact one commit wrote and advances the driver's timer
// state when that fact names an interaction someone awaits, recording the session
// that material belongs to beside it: the pass reflects on that session's model
// request.
//
// The pass itself runs detached from the committing turn: a reflection spends a
// model call, and a user turn never waits for it. A failed read is reported and
// never rolls the commit back, and a stamp outside the host-facing vocabulary
// leaves the timer untouched, so an agent's own reflection never reflects again.
func (d *Driver) ObserveCommit(ctx context.Context, id event.EventID) error {
	if d == nil || d.reflection == nil || !d.live() {
		return nil
	}
	if d.lookup == nil {
		return ErrObservedFaceUnwired
	}
	fact, err := d.lookup(ctx, id)
	if err != nil {
		return err
	}
	if !hostFacingStamp(fact.Meta[event.MetaKeyTriggerSource]) {
		return nil
	}

	now := time.Now()
	d.mu.Lock()
	d.owner = fact.Owner
	if d.stopped {
		d.mu.Unlock()
		return nil
	}
	if d.firing {
		d.retained = true
		d.mu.Unlock()
		return nil
	}
	if d.interval > 0 && !d.lastFired.IsZero() && now.Sub(d.lastFired) < d.interval {
		d.mu.Unlock()
		return nil
	}
	d.firing = true
	d.wg.Add(1)
	d.mu.Unlock()

	go d.fire(ctx)
	return nil
}

// fire runs the reflection passes one firing episode owes and settles the
// driver's firing state. An observation that lands while a pass is in flight is
// retained for the evaluation that settles it, so a busy driver never drops a
// commit; the episode outlives the committing turn but not the driver: a stop
// cancels it, and Stop waits for it to return.
func (d *Driver) fire(ctx context.Context) {
	defer d.wg.Done()
	defer func() {
		d.mu.Lock()
		if d.stopped {
			d.firing = false
			d.retained = false
		}
		d.mu.Unlock()
	}()

	if ctx != nil {
		ctx = context.WithoutCancel(ctx)
	} else {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(d.stopCtx, cancel)
	defer stop()

	for {
		d.pass(ctx)
		wait, again := d.rearm()
		if !again {
			return
		}
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// pass runs one reflection pass and reports the reading to the host observer. A
// pass the observed session is running a model request for keeps the episode
// open on the standing rearm rhythm: the watermark stays where it stands, no
// reflection input is pending, and the window is evaluated again once the
// request may have ended. A pass the rhythm floor holds back keeps the episode
// open until the window opens, so the same observation is evaluated again
// without a new commit.
func (d *Driver) pass(ctx context.Context) {
	if held, reason := d.modelInFlightOf(); held {
		wait := d.interval
		if f := d.rhythmFloor(); f > 0 && (wait <= 0 || f < wait) {
			wait = f
		}
		d.hold(time.Now().Add(wait))
		d.notify(TickOutcome{Decision: TickDecision{Disposition: TickRhythm, Reason: reason}})
		return
	}
	floor := d.rhythmFloor()
	if floor > 0 {
		if watermark, err := d.reflection.Watermark(ctx); err == nil {
			if !watermark.Moment.IsZero() && time.Since(watermark.Moment) < floor {
				d.hold(watermark.Moment.Add(floor))
				d.notify(TickOutcome{Decision: TickDecision{
					Disposition: TickRhythm,
					Reason:      "the min gap since the last consumed reflection has not elapsed",
				}})
				return
			}
		}
	}

	outcome := d.reflection.Tick(ctx, time.Now())
	if outcome.Decision.Disposition == TickFired {
		d.mu.Lock()
		d.lastFired = time.Now()
		d.mu.Unlock()
	}
	if outcome.Decision.Disposition == TickRhythm {
		d.hold(d.rhythmWindow(outcome, floor))
	}
	d.notify(outcome)
}

// rhythmFloor is the floor this pass waits out: the wider of the driver's own
// interval-side floor and the reflection's cadence floor, which name the same
// declared value in production and are never narrower than the window that held
// the pass back.
func (d *Driver) rhythmFloor() time.Duration {
	floor := d.minGap
	if d.reflection != nil {
		if cadence := d.reflection.MinGap(); cadence > floor {
			floor = cadence
		}
	}
	return floor
}

// rhythmWindow names the moment the rhythm floor that held a pass back opens the
// window again: the moment the newest host-facing fact leaves the floor, or one
// floor from now when the pass carried no such fact.
func (d *Driver) rhythmWindow(outcome TickOutcome, floor time.Duration) time.Time {
	if scan := outcome.Scan; scan != nil && scan.Verdict.Recent != nil {
		if at := scan.Verdict.Recent.Content.Timestamp; !at.IsZero() {
			return at.Add(floor)
		}
	}
	return time.Now().Add(floor)
}

// hold keeps the firing episode open until the moment the window opens again: the
// observation that settled the pass is postponed, never dropped, and the standing
// re-arm schedules the re-evaluation the window allows.
func (d *Driver) hold(until time.Time) {
	d.mu.Lock()
	d.retained = true
	d.holdUntil = until
	d.mu.Unlock()
}

// modelInFlightOf reports whether the session the latest host-facing commit names
// is running a model request, beside the reading that names the decision it
// runs. A driver assembled without a consumption-state read or in no instance
// has no request to wait on: an empty instance is answered here, once, and never
// handed to the read.
func (d *Driver) modelInFlightOf() (bool, string) {
	d.mu.Lock()
	modelInFlight, instance, owner := d.modelInFlight, d.instance, d.owner
	d.mu.Unlock()
	if modelInFlight == nil || instance == "" || owner.IsZero() {
		return false, ""
	}
	invocation, decision, held := modelInFlight(owner, instance)
	if !held {
		return false, ""
	}
	return true, fmt.Sprintf("the model request of %s/%s is executing as the decision %s of %s", owner.AgentID, owner.SessionID, decision, invocation)
}

// rearm settles one pass: it releases the firing state when no observation was
// retained during the pass, and otherwise keeps the episode open and reports how
// long the rhythm floor still holds the retained evaluation back. A first
// suppression has no fire to measure the interval from, and a held pass has
// nothing left of it: both wait for the window the floor closes, never shorter
// than the single millisecond that still lets the clock advance and never
// longer than the interval floor when one is declared.
func (d *Driver) rearm() (time.Duration, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || !d.retained {
		d.firing = false
		return 0, false
	}
	d.retained = false
	now := time.Now()
	wait := time.Duration(0)
	if d.interval > 0 && !d.lastFired.IsZero() {
		wait = d.interval - now.Sub(d.lastFired)
	}
	if wait <= 0 {
		if wait = d.holdUntil.Sub(now); wait < time.Millisecond {
			wait = time.Millisecond
		}
		if d.interval > 0 && wait > d.interval {
			wait = d.interval
		}
	}
	return wait, true
}

// live reports whether the driver still accepts commits.
func (d *Driver) live() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.stopped
}

// notify hands one pass reading to the host observer, if one is attached.
func (d *Driver) notify(outcome TickOutcome) {
	d.mu.Lock()
	observer := d.observer
	d.mu.Unlock()
	if observer != nil {
		observer(outcome)
	}
}

// Stop ends the driver: no further commit fires a pass, a pass in flight is
// cancelled, and Stop waits for it to return. A repeated stop is a no-op.
func (d *Driver) Stop() {
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
