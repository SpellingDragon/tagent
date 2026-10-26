package meditation

import (
	"context"
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
// never a clock that fires on its own.
type Driver struct {
	reflection *Reflection
	interval   time.Duration
	minGap     time.Duration

	// lookup reads the fact one commit identity names: the commit notice carries
	// an identity, and the fact itself is read back through the observed chain.
	lookup func(ctx context.Context, id event.EventID) (event.Event, error)

	stopCtx    context.Context
	stopCancel context.CancelFunc

	mu        sync.Mutex
	lastFired time.Time
	firing    bool
	retained  bool
	stopped   bool
	wg        sync.WaitGroup

	observer func(TickOutcome)
}

var (
	_ plugin.Instance           = (*Driver)(nil)
	_ plugin.PostCommitObserver = (*Driver)(nil)
	_ interface{ Stop() }       = (*Driver)(nil)
)

// NewDriver assembles the event-driven reflection face over one reflection loop,
// the floor between two fires and the floor between two consumed reflections.
//
// The interval is measured from the last fire; the first fire is never held
// back, so the first event of a process is answered at once, and a non-positive
// interval disables that floor. The
// min gap stays the reflection's own rhythm floor, read from the execution
// watermark, so a fired but not yet consumed reflection is never re-fired early.
func NewDriver(reflection *Reflection, interval, minGap time.Duration) *Driver {
	d := &Driver{
		reflection: reflection,
		interval:   interval,
		minGap:     minGap,
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
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// SetObserver attaches the reading of each pass this driver runs. It is the
// host's log face; a nil observer drops the readings.
func (d *Driver) SetObserver(observer func(TickOutcome)) {
	d.mu.Lock()
	d.observer = observer
	d.mu.Unlock()
}

// ObserveCommit reads the fact one commit wrote and advances the driver's timer
// state when that fact names an interaction someone awaits.
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

// pass runs one reflection pass and reports the reading to the host observer.
func (d *Driver) pass(ctx context.Context) {
	if d.minGap > 0 {
		if watermark, err := d.reflection.Watermark(ctx); err == nil {
			if !watermark.Moment.IsZero() && time.Since(watermark.Moment) < d.minGap {
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
	d.notify(outcome)
}

// rearm settles one pass: it releases the firing state when no observation was
// retained during the pass, and otherwise keeps the episode open and reports how
// long the rhythm floor still holds the retained evaluation back.
func (d *Driver) rearm() (time.Duration, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || !d.retained {
		d.firing = false
		return 0, false
	}
	d.retained = false
	if d.interval > 0 && !d.lastFired.IsZero() {
		if wait := d.interval - time.Since(d.lastFired); wait > 0 {
			return wait, true
		}
	}
	return 0, true
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
