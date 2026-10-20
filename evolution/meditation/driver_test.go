// driver_test 钉住 定时反射驱动的红线：可驱动的计划在装配期拒绝负节奏、无目标与无投递端口，
// 未设间隔回落到核心默认；一次 Tick 走同一条水位/观察/注入链，只有 fired 才把结果交给投递端口；
// 循环按声明的间隔起停，停止后没有投递。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package meditation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestNewDriverRefusesUnusablePlans pins each named refusal of a plan the driver cannot run.
func TestNewDriverRefusesUnusablePlans(t *testing.T) {
	port := func(context.Context, event.Address, []event.AgentID) {}
	cases := []struct {
		name     string
		mutate   func(*DriverPlan)
		dropPort bool
		want     error
	}{
		{name: "negative interval", mutate: func(p *DriverPlan) { p.Cadence.Interval = -time.Minute }, want: ErrDriverCadence},
		{name: "negative rhythm floor", mutate: func(p *DriverPlan) { p.Cadence.MinGap = -time.Minute }, want: ErrDriverCadence},
		{name: "no target", mutate: func(p *DriverPlan) { p.Targets = nil }, want: ErrDriverUntargeted},
		{name: "no port", mutate: func(*DriverPlan) {}, dropPort: true, want: ErrDriverUnwired},
		{name: "face outside the read scope", mutate: func(p *DriverPlan) { p.Face.ObservedNamespaces = []string{"ops"} }, want: ErrObservationScopeOutOfBounds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := driverPlan()
			tc.mutate(&plan)
			deliver := port
			if tc.dropPort {
				deliver = nil
			}
			if _, err := NewDriver(plan, &driverGate{}, &driverFacts{}, &driverInput{}, deliver); !errors.Is(err, tc.want) {
				t.Fatalf("assemble the plan: error %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNewDriverDefaultsAnUnsetInterval pins the cadence and observation face of a plan that names no interval.
func TestNewDriverDefaultsAnUnsetInterval(t *testing.T) {
	plan := driverPlan()
	plan.Cadence.Interval = 0
	driver, err := NewDriver(plan, &driverGate{}, &driverFacts{}, &driverInput{},
		func(context.Context, event.Address, []event.AgentID) {})
	if err != nil {
		t.Fatalf("assemble the plan: %v", err)
	}
	if got := driver.plan.Cadence.Interval; got != DefaultInterval {
		t.Fatalf("interval = %s, want the default %s", got, DefaultInterval)
	}
	scope := driver.Scope()
	if scope.Own != "meditator" || len(scope.Names) != 1 || scope.Names[0] != "tagent" {
		t.Fatalf("scope = %+v, want the meditator observing tagent", scope)
	}
}

// TestTickFiresOnceAndDelivers pins one fired tick, one delivery and the advanced watermark.
func TestTickFiresOnceAndDelivers(t *testing.T) {
	ctx := context.Background()
	gate := &driverGate{}
	input := &driverInput{}
	deliveries := &driverDeliveries{}
	facts := &driverFacts{fact: event.Event{
		Key:       "fact-1",
		Type:      "answer",
		Timestamp: time.Now().Add(-time.Minute),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: "user",
			event.MetaKeyEventSummary:  "a business answer",
		},
	}}
	driver, err := NewDriver(driverPlan(), gate, facts, input, deliveries.deliver)
	if err != nil {
		t.Fatalf("assemble the plan: %v", err)
	}

	outcome := driver.Tick(ctx)
	if outcome.Decision.Disposition != TickFired {
		t.Fatalf("tick disposition = %q (%s), want %q", outcome.Decision.Disposition, outcome.Decision.Reason, TickFired)
	}
	calls := deliveries.taken()
	if len(calls) != 1 {
		t.Fatalf("deliveries = %d, want one", len(calls))
	}
	wantSource := event.Address{AgentID: "meditator", SessionID: "meditator#meditation"}
	if calls[0].source != wantSource {
		t.Fatalf("delivery source = %+v, want %+v", calls[0].source, wantSource)
	}
	if len(calls[0].targets) != 1 || calls[0].targets[0] != "tagent" {
		t.Fatalf("delivery targets = %v, want the declared tagent target", calls[0].targets)
	}
	watermark, err := gate.Current(ctx)
	if err != nil {
		t.Fatalf("read the watermark: %v", err)
	}
	if watermark.Moment.IsZero() || !watermark.Moment.Equal(outcome.Injection.At) {
		t.Fatalf("watermark = %s, want the injection moment %s", watermark.Moment, outcome.Injection.At)
	}
	if got := input.count(); got != 1 {
		t.Fatalf("injections = %d, want one", got)
	}

	again := driver.Tick(ctx)
	if again.Decision.Disposition != TickNoNovelty {
		t.Fatalf("second tick disposition = %q (%s), want %q", again.Decision.Disposition, again.Decision.Reason, TickNoNovelty)
	}
	if got := len(deliveries.taken()); got != 1 {
		t.Fatalf("deliveries after the second tick = %d, want one", got)
	}
}

// TestTickDeliversNothingBesideAClosedGate pins a closed gate that delivers nothing.
func TestTickDeliversNothingBesideAClosedGate(t *testing.T) {
	deliveries := &driverDeliveries{}
	driver, err := NewDriver(driverPlan(), closedGate{}, &driverFacts{}, &driverInput{}, deliveries.deliver)
	if err != nil {
		t.Fatalf("assemble the plan: %v", err)
	}
	outcome := driver.Tick(context.Background())
	if outcome.Decision.Disposition != TickClosed {
		t.Fatalf("tick disposition = %q (%s), want %q", outcome.Decision.Disposition, outcome.Decision.Reason, TickClosed)
	}
	if got := len(deliveries.taken()); got != 0 {
		t.Fatalf("deliveries = %d, want none beside a closed gate", got)
	}
}

// TestTickWithholdsTheOutputOfAnUnreadableInput pins the delivery judgment over a fired input the policy cannot read.
// - the tick still reports the injection it made
// - the delivery port is not called
func TestTickWithholdsTheOutputOfAnUnreadableInput(t *testing.T) {
	deliveries := &driverDeliveries{}
	driver, err := NewDriver(driverPlan(), &driverGate{}, driverFreshFacts(), &driverUnstampedInput{}, deliveries.deliver)
	if err != nil {
		t.Fatalf("assemble the plan: %v", err)
	}
	outcome := driver.Tick(context.Background())
	if outcome.Decision.Disposition != TickFired {
		t.Fatalf("tick disposition = %q (%s), want %q", outcome.Decision.Disposition, outcome.Decision.Reason, TickFired)
	}
	if got := deliveries.count(); got != 0 {
		t.Fatalf("deliveries = %d, want none beside an input the policy cannot read", got)
	}
}

// TestStartTicksUntilStopped pins the loop's interval, delivery and Stop behavior.
func TestStartTicksUntilStopped(t *testing.T) {
	plan := driverPlan()
	plan.Cadence.Interval = 5 * time.Millisecond
	plan.Cadence.MinGap = 0
	deliveries := &driverDeliveries{}
	input := &driverInput{outcome: ReflectionDeferred}
	driver, err := NewDriver(plan, &driverGate{}, driverFreshFacts(), input, deliveries.deliver)
	if err != nil {
		t.Fatalf("assemble the plan: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	driver.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for deliveries.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := deliveries.count(); got == 0 {
		t.Fatal("the loop delivered no reflection within two seconds")
	}
	stopped := make(chan struct{})
	go func() {
		driver.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within two seconds")
	}
	before := deliveries.count()
	time.Sleep(20 * time.Millisecond)
	if got := deliveries.count(); got != before {
		t.Fatalf("deliveries after Stop = %d, want the %d the loop left", got, before)
	}
}

// driverPlan is the baseline plan the driver tests mutate: one meditator
// observing the tagent partition at a one-minute cadence.
func driverPlan() DriverPlan {
	return DriverPlan{
		ReflectionConfig: ReflectionConfig{
			Loop:    ReflectionLoop{Agent: "meditator", Session: "meditator#meditation"},
			Face:    ReflectionFace{ObservedNamespaces: []string{"tagent"}, ReadNamespaces: []string{"tagent"}},
			Cadence: ReflectionCadence{Interval: time.Minute},
			Prompt:  "reflect on the observed partition",
		},
		Targets: []event.AgentID{"tagent"},
	}
}

// driverFreshFacts returns one fact the loop reads as fresh on every tick.
func driverFreshFacts() *driverFacts {
	return &driverFacts{fact: event.Event{
		Key:       "fact-1",
		Type:      "answer",
		Timestamp: time.Now().Add(-time.Minute),
		Meta:      map[string]string{event.MetaKeyTriggerSource: "user"},
	}}
}

// driverGate is the in-memory watermark gate of the driver tests.
type driverGate struct {
	mu     sync.Mutex
	moment time.Time
}

// Current reports the stored watermark.
func (g *driverGate) Current(context.Context) (Watermark, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Watermark{Moment: g.moment}, nil
}

// Advance moves the watermark forward and never backwards.
func (g *driverGate) Advance(_ context.Context, moment time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if moment.After(g.moment) {
		g.moment = moment
	}
	return nil
}

// closedGate is a watermark gate that cannot establish its watermark.
type closedGate struct{}

// Current refuses the watermark by name.
func (closedGate) Current(context.Context) (Watermark, error) {
	return Watermark{}, ErrWatermarkSourceUnwired
}

// Advance keeps the watermark where it stands.
func (closedGate) Advance(context.Context, time.Time) error { return nil }

// driverFacts is the fact-chain face of the driver tests, holding one fact.
type driverFacts struct {
	fact event.Event
}

// QueryEvents answers one reference to the held fact.
func (f *driverFacts) QueryEvents(context.Context, memory.QueryOptions) ([]memory.EventReference, error) {
	return []memory.EventReference{{
		Key:         f.fact.Key,
		PartitionID: memory.PartitionIDFromName("tagent"),
		Timestamp:   f.fact.Timestamp.UnixMilli(),
	}}, nil
}

// CommittedFact answers the held fact.
func (f *driverFacts) CommittedFact(context.Context, event.EventID) (event.Event, error) {
	return f.fact, nil
}

// driverInput is the loop-session injection face of the driver tests.
type driverInput struct {
	mu      sync.Mutex
	calls   int
	outcome ReflectionOutcome
}

// InjectReflection reports the configured outcome for every injected input.
func (i *driverInput) InjectReflection(_ context.Context, evt event.Event) (ReflectionReceipt, error) {
	i.mu.Lock()
	i.calls++
	i.mu.Unlock()
	outcome := i.outcome
	if outcome == "" {
		outcome = ReflectionConsumed
	}
	return ReflectionReceipt{Key: "reflection-key", Outcome: outcome, At: evt.Timestamp}, nil
}

// count reports how many reflection inputs the face injected.
func (i *driverInput) count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.calls
}

// driverUnstampedInput is the loop-session injection face of a driver whose committed
// input carries no trigger source, so the delivery judgment cannot read it as visible.
type driverUnstampedInput struct{}

// InjectReflection drops the trigger source from the input it commits and reports a
// consumed turn.
func (*driverUnstampedInput) InjectReflection(_ context.Context, evt event.Event) (ReflectionReceipt, error) {
	delete(evt.Meta, event.MetaKeyTriggerSource)
	return ReflectionReceipt{Key: "reflection-key", Outcome: ReflectionConsumed, At: evt.Timestamp}, nil
}

// driverDeliveries records the calls the delivery port received.
type driverDeliveries struct {
	mu  sync.Mutex
	got []driverDelivery
}

// driverDelivery is one call the delivery port received.
type driverDelivery struct {
	source  event.Address
	targets []event.AgentID
}

// deliver records one delivery.
func (d *driverDeliveries) deliver(_ context.Context, source event.Address, targets []event.AgentID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, driverDelivery{source: source, targets: append([]event.AgentID(nil), targets...)})
}

// taken returns the deliveries recorded so far.
func (d *driverDeliveries) taken() []driverDelivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]driverDelivery(nil), d.got...)
}

// count reports how many deliveries were recorded.
func (d *driverDeliveries) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.got)
}
