// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// The scheduled reflection driver of one meditation loop: the plan it runs, the
// delivery port a fired reflection flows back through, one Tick per declared
// interval, and the named refusals a plan that cannot drive answers at assembly.
package meditation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
)

// DefaultInterval is the tick cadence of a driver whose plan names none.
const DefaultInterval = 30 * time.Minute

// ErrDriverCadence reports a plan whose interval or rhythm floor is negative: no
// ticker can run at such a cadence.
var ErrDriverCadence = errors.New("evolution: the meditation driver requires a non-negative cadence")

// ErrDriverUntargeted reports a plan that names no delivery target: a fired
// reflection whose result nobody receives is refused at assembly.
var ErrDriverUntargeted = errors.New("evolution: the meditation driver requires a declared delivery target")

// ErrDriverUnwired reports a driver assembled without the delivery port a fired
// reflection hands its result to.
var ErrDriverUnwired = errors.New("evolution: the meditation driver requires a delivery port")

// Deliver hands the output of one fired reflection to the declared targets: the
// source is the observing agent's own loop address, and the targets are the
// declared recipients in declaration order.
type Deliver func(ctx context.Context, source event.Address, targets []event.AgentID)

// DriverPlan is the one scheduled reflection a host declares: the reflection
// loop, face, cadence and prompt, beside the declared delivery targets.
type DriverPlan struct {
	ReflectionConfig
	// Targets is the declared delivery white list of one fired reflection.
	Targets []event.AgentID
}

// Driver drives one declared reflection loop: each tick reads the observed face
// through the reflection chain, injects the reflection input into the observing
// agent's own loop session, and hands a fired reflection's output to the
// delivery port. The zero value is not usable; build a driver with NewDriver.
type Driver struct {
	plan       DriverPlan
	source     event.Address
	reflection *Reflection
	deliver    Deliver

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewDriver assembles the scheduled reflection of one plan over its watermark
// gate, its observed fact-chain face, its loop-session injection face and its
// delivery port. A negative cadence, a plan naming no target and a missing port
// are refused by name; a plan leaving the interval unset runs at DefaultInterval.
func NewDriver(plan DriverPlan, gate WatermarkGate, facts ObservedFacts, input ReflectionInput, deliver Deliver) (*Driver, error) {
	if plan.Cadence.Interval < 0 || plan.Cadence.MinGap < 0 {
		return nil, fmt.Errorf("%w: interval %s, min_gap %s", ErrDriverCadence, plan.Cadence.Interval, plan.Cadence.MinGap)
	}
	if len(plan.Targets) == 0 {
		return nil, ErrDriverUntargeted
	}
	if deliver == nil {
		return nil, ErrDriverUnwired
	}
	if plan.Cadence.Interval == 0 {
		plan.Cadence.Interval = DefaultInterval
	}
	plan.Targets = append([]event.AgentID(nil), plan.Targets...)
	plan.Face.ObservedNamespaces = append([]string(nil), plan.Face.ObservedNamespaces...)
	plan.Face.ReadNamespaces = append([]string(nil), plan.Face.ReadNamespaces...)
	reflection, err := NewReflection(plan.ReflectionConfig, gate, facts, input)
	if err != nil {
		return nil, err
	}
	return &Driver{
		plan:       plan,
		source:     event.Address{AgentID: event.AgentID(plan.Loop.Agent), SessionID: event.SessionID(plan.Loop.Session)},
		reflection: reflection,
		deliver:    deliver,
	}, nil
}

// Scope reports the resolved observation face of this driver's reflection.
func (d *Driver) Scope() ReflectionScope {
	return d.reflection.Scope()
}

// Tick runs one scheduled reflection and, when it fired, hands what the
// reflection turn produced to the delivery port. A tick that decided not to fire
// delivers nothing, and a fired input the delivery policy does not read as
// host-visible is withheld.
func (d *Driver) Tick(ctx context.Context) TickOutcome {
	outcome := d.reflection.Tick(ctx, time.Now())
	if outcome.Decision.Disposition != TickFired {
		observeDriver("tagent: meditation driver %s tick %s (%s)",
			d.plan.Loop.Agent, outcome.Decision.Disposition, outcome.Decision.Reason)
		return outcome
	}
	delivery := DeliveryPolicy{}
	if outcome.Injection == nil || !delivery.Deliverable(outcome.Injection.Event) {
		observeDriver("tagent: meditation driver %s withheld reflection: the fired input carries no host-visible lineage", d.plan.Loop.Agent)
		return outcome
	}
	observeDriverFired("tagent: meditation driver %s fired reflection %s", d.plan.Loop.Agent, outcome.Injection.Key)
	d.deliver(ctx, d.source, d.plan.Targets)
	return outcome
}

// Start begins the tick loop: the first tick runs one interval after the start,
// so a restart never fires a reflection the previous process just consumed.
func (d *Driver) Start(ctx context.Context) {
	d.mu.Lock()
	if d.cancel != nil {
		d.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	d.cancel, d.done = cancel, done
	d.mu.Unlock()
	go d.run(loopCtx, done)
}

// run drives one tick per interval until the loop context ends.
func (d *Driver) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(d.plan.Cadence.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Tick(ctx)
		}
	}
}

// Stop ends the tick loop and waits for the tick in flight to return. A driver
// never started returns immediately.
func (d *Driver) Stop() {
	d.mu.Lock()
	cancel, done := d.cancel, d.done
	d.cancel, d.done = nil, nil
	d.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// observeDriver renders one driver line at DEBUG through the package logger: a
// failing logger is a missing line, never a refused tick.
func observeDriver(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}

// observeDriverFired renders the line of one fired reflection at INFO through the
// package logger: a failing logger is a missing line, never a refused tick.
func observeDriverFired(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Infof(format, args...)
}
