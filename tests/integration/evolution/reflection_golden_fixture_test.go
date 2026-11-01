// The fixture of the long-time reflection golden: a scripted model in the shape the
// reflection rig already uses, a compressed reflection cadence, a persisted watermark
// read, fact counting over the observed chain and the resource counters a long run is
// read against.
//
// The rounds of the golden run on an injected clock: every round hands the tick the
// moment it happens at, so a 30–50 round loop costs the wall clock of a few model
// stand-in calls and no wait at all. Nothing here asserts on the wall clock; both the
// cadence and the rounds are event counts and state readings.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package evolution

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// goldenRounds, goldenRestartAt, goldenStep and goldenFloor state the shape of one
// golden run once, so the rounds stay inside the band D11 declares and the restart
// lands in the middle of the line instead of at its edge.
const (
	// goldenRounds is the length of the long-time loop; D11 asks for 30–50 and the
	// golden runs the middle of that band.
	goldenRounds = 36
	// goldenRestartAt is the round the process is restarted before: the watermark the
	// first half advanced must be read back by the second half.
	goldenRestartAt = 18
	// goldenStep is how far one round's source fact is recorded after the previous
	// round's: the injected clock advances by this much per round.
	goldenStep = time.Second
	// goldenFloor is the rhythm floor of the golden, compressed from the production
	// hours to the window an injected clock walks in one step.
	goldenFloor = 100 * time.Millisecond
)

// goldenEpoch is the moment round zero of every golden fixture starts from: a fixed
// origin, so a restarted fixture lands on the same rounds the first half did.
var goldenEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// goldenMode is what the fixture's loop-session face reports back to the tick: the
// production reporting, beside the mutations that prove the golden's assertions bite.
type goldenMode int

const (
	// goldenConsumed is the production shape: the input commits the reflection fact,
	// drives the agent's own loop session and reports the terminal of that turn.
	goldenConsumed goldenMode = iota
	// goldenInFlight is the mutation of a loop that never reports an outcome: the
	// input commits and reports the turn still in flight, so the watermark never
	// advances and the pending guard holds every later round back.
	goldenInFlight
	// goldenDuplicate is the mutation of a loop that consumes one round twice: the
	// input commits the same reflection input under a second identity.
	goldenDuplicate
	// goldenNoDrive is the mutation of a fixture that never reached the model: the
	// input reports a consumed turn without driving the loop at all.
	goldenNoDrive
)

// goldenConfig is the reflection assembly the golden runs over: one agent's own loop
// session, its own partition as the observed face and the compressed cadence.
func goldenConfig() meditation.ReflectionConfig {
	return meditation.ReflectionConfig{
		Loop:    meditation.ReflectionLoop{Agent: "agent-a", Session: "session-meditate"},
		Cadence: meditation.ReflectionCadence{Interval: goldenFloor, MinGap: goldenFloor},
		Prompt:  "reflect on the observed face",
	}
}

// goldenSourceFact builds the host-facing fact one round of the loop reads: a user
// input recorded at the round's moment, carrying the round in its summary and in the
// identity it commits under, so the fact a restarted run reads back is the same fact
// and never a second record beside it.
func goldenSourceFact(round int, at time.Time) event.Event {
	summary := fmt.Sprintf("round %02d: work observed", round)
	return event.Event{
		Key:       event.EventID(fmt.Sprintf("golden-source-%02d", round)),
		Type:      event.TypeExternalInput,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyTriggerSource: "user", event.MetaKeyEventSummary: summary},
		Message:   model.Message{Role: model.RoleUser, Content: summary},
	}
}

// goldenReading is one watermark read of the run: the round it was read after and the
// moment the gate answered.
type goldenReading struct {
	round  int
	moment time.Time
}

// goldenRound is what one round of the loop did: the source fact it recorded, the
// moment it ticked at, the disposition it reached and the watermark it read back.
type goldenRound struct {
	index       int
	source      event.EventID
	at          time.Time
	now         time.Time
	disposition meditation.TickDisposition
	reason      string
	fired       bool
	key         event.EventID
	body        string
	reading     time.Time
	readError   error
}

// goldenFixture is one reflecting agent assembled over an anchor directory, the fact
// chain and the agent's own loop session, beside the readings one golden run leaves.
type goldenFixture struct {
	t    *testing.T
	dir  string
	cfg  meditation.ReflectionConfig
	mode goldenMode

	ledger     *reflectionLedger
	model      *reflectionModel
	store      *meditation.AnchorStore
	gate       meditation.WatermarkGate
	input      *goldenInput
	reflection *meditation.Reflection

	loop      *agent.Loop
	address   event.Address
	instance  event.InstanceID
	partition int

	readings []goldenReading
	rounds   []goldenRound
	scanned  map[event.EventID][]int
	firedOn  map[event.EventID][]int
	delivery *goldenDelivery
}

// newGoldenFixture assembles one reflecting agent over dir in the named mode. A
// non-nil wrap replaces the watermark gate the golden reads, which is how a mutation
// regresses the watermark through the production tick.
func newGoldenFixture(t *testing.T, dir string, cfg meditation.ReflectionConfig, mode goldenMode, wrap func(meditation.WatermarkGate) meditation.WatermarkGate) *goldenFixture {
	t.Helper()
	instance := event.InstanceID("instance-golden")
	address := event.Address{AgentID: event.AgentID(cfg.Loop.Agent), SessionID: event.SessionID(cfg.Loop.Session)}
	ledger := newReflectionLedger()
	provider := &reflectionModel{name: "model-golden"}

	decl := &config.Declaration{
		Entry:  cfg.Loop.Agent,
		Agents: []config.AgentSpec{{ID: cfg.Loop.Agent, Model: provider.name}},
	}
	binding := state.AgentBinding{
		Model:        provider,
		PromptSource: state.PromptSource{Version: "prompt-golden-v1"},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: decl}, map[event.AgentID]state.AgentBinding{event.AgentID(cfg.Loop.Agent): binding}, nil)
	if err != nil {
		t.Fatalf("build the golden state plan: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the golden state plan: %v", err)
	}
	loop := agent.NewLoop(bindings, state.NewOccupancy(), instance, nil)

	store, err := meditation.NewAnchorStore(goldenAnchorPath(dir))
	if err != nil {
		t.Fatalf("build the golden anchor store: %v", err)
	}
	var gate meditation.WatermarkGate = store
	if wrap != nil {
		gate = wrap(store)
	}
	fixture := &goldenFixture{
		t:         t,
		dir:       dir,
		cfg:       cfg,
		mode:      mode,
		ledger:    ledger,
		model:     provider,
		store:     store,
		gate:      gate,
		loop:      loop,
		address:   address,
		instance:  instance,
		partition: memory.PartitionIDFromName(cfg.Loop.Agent),
		scanned:   map[event.EventID][]int{},
		firedOn:   map[event.EventID][]int{},
	}
	fixture.input = &goldenInput{fixture: fixture, ledger: ledger, loop: loop, address: address, instance: instance, partition: fixture.partition}
	reflection, err := meditation.NewReflection(cfg, gate, ledger, fixture.input)
	if err != nil {
		t.Fatalf("assemble the golden reflection: %v", err)
	}
	fixture.reflection = reflection
	return fixture
}

// goldenAnchorPath is the persisted watermark record of one golden directory: the same
// file across a restart, which is what makes the continuation readable.
func goldenAnchorPath(dir string) string {
	return dir + string(os.PathSeparator) + "anchors.json"
}

// reassemble restarts the fixture: a new anchor store, a new reflection and a new loop
// session over the same directory, carrying the fact chain and the readings the run
// already made, the way a process restart carries a durable store and loses nothing it
// persisted.
func (f *goldenFixture) reassemble(mode goldenMode, wrap func(meditation.WatermarkGate) meditation.WatermarkGate) *goldenFixture {
	f.t.Helper()
	next := newGoldenFixture(f.t, f.dir, f.cfg, mode, wrap)
	next.ledger.restore(f.ledger)
	next.readings = append([]goldenReading(nil), f.readings...)
	next.rounds = append([]goldenRound(nil), f.rounds...)
	for key, rounds := range f.scanned {
		next.scanned[key] = append([]int(nil), rounds...)
	}
	for key, rounds := range f.firedOn {
		next.firedOn[key] = append([]int(nil), rounds...)
	}
	next.delivery = f.delivery
	if next.delivery != nil {
		next.delivery.ledger = next.ledger
	}
	return next
}

// persistedWatermark reads the watermark the record on disk holds, so a restart's
// continuation is read from the durable record rather than from the fixture's memory.
func (f *goldenFixture) persistedWatermark() (time.Time, error) {
	store, err := meditation.NewAnchorStore(goldenAnchorPath(f.dir))
	if err != nil {
		return time.Time{}, err
	}
	current, err := store.Current(context.Background())
	if err != nil {
		return time.Time{}, err
	}
	return current.Moment, nil
}

// tickRound records round r's source fact and runs one tick at that round's injected
// moment, then reads the watermark back. The pass it read is tallied so a fact
// consumed by two rounds is visible as exactly that.
func (f *goldenFixture) tickRound(ctx context.Context, round int) goldenRound {
	f.t.Helper()
	f.input.round = round
	at := goldenEpoch.Add(time.Duration(round) * goldenStep)
	source := f.ledger.record(goldenSourceFact(round, at), f.partition)
	now := at.Add(goldenFloor + time.Millisecond)

	outcome := f.reflection.Tick(ctx, now)
	record := goldenRound{
		index:       round,
		source:      source,
		at:          at,
		now:         now,
		disposition: outcome.Decision.Disposition,
		reason:      outcome.Decision.Reason,
	}
	if outcome.Injection != nil {
		record.fired = true
		record.key = outcome.Injection.Key
		record.body = outcome.Injection.Event.Message.Content
	}
	if outcome.Scan != nil {
		for _, fact := range outcome.Scan.Facts {
			f.scanned[fact.Identity.Key] = append(f.scanned[fact.Identity.Key], round)
		}
		if outcome.Scan.Verdict.Recent != nil {
			consumed := outcome.Scan.Verdict.Recent.Identity.Key
			f.firedOn[consumed] = append(f.firedOn[consumed], round)
		}
	}
	current, err := f.gate.Current(ctx)
	if err != nil {
		record.readError = err
	} else {
		record.reading = current.Moment
		f.readings = append(f.readings, goldenReading{round: round, moment: current.Moment})
	}
	f.rounds = append(f.rounds, record)
	return record
}

// quietTick ticks at the named moment without recording a new fact: a loop whose
// watermark carried across the restart reads nothing fresh, and a loop that lost it
// reaches back over a fact it already covered and consumes it a second time. The pass it
// reads is tallied exactly as a round's is, so the duplicate is visible where it happened.
func (f *goldenFixture) quietTick(ctx context.Context, index int, at time.Time) goldenRound {
	f.t.Helper()
	outcome := f.reflection.Tick(ctx, at)
	record := goldenRound{
		index:       index,
		at:          at,
		now:         at,
		disposition: outcome.Decision.Disposition,
		reason:      outcome.Decision.Reason,
	}
	if outcome.Injection != nil {
		record.fired = true
		record.key = outcome.Injection.Key
		record.body = outcome.Injection.Event.Message.Content
	}
	if outcome.Scan != nil {
		for _, fact := range outcome.Scan.Facts {
			f.scanned[fact.Identity.Key] = append(f.scanned[fact.Identity.Key], index)
		}
		if outcome.Scan.Verdict.Recent != nil {
			consumed := outcome.Scan.Verdict.Recent.Identity.Key
			f.firedOn[consumed] = append(f.firedOn[consumed], index)
		}
	}
	return record
}

// runRounds runs the rounds from `from` up to `to` (exclusive) on this fixture.
func (f *goldenFixture) runRounds(ctx context.Context, from, to int) {
	f.t.Helper()
	for round := from; round < to; round++ {
		f.tickRound(ctx, round)
	}
}

// requireModelCalls reports whether every round of the run reached the model: the
// scripted model records one request per reflection turn it drove, so a fixture that
// never drove the agent's own loop is red here instead of passing quietly.
func (f *goldenFixture) requireModelCalls(rounds int) error {
	f.t.Helper()
	if got := len(f.model.recorded()); got != rounds {
		return fmt.Errorf("model calls = %d after %d round(s), want one decision per reflection turn: the fixture never drove the model", got, rounds)
	}
	return nil
}

// goldenAssertEveryRoundFired reports the first round the loop did not fire and consume
// at. A round that stays pending, reads nothing fresh or meets the rhythm floor is the
// symptom this golden exists for: the watermark did not advance, so no later round can
// be trusted either.
func goldenAssertEveryRoundFired(rounds []goldenRound) error {
	for _, record := range rounds {
		if record.disposition != meditation.TickFired || !record.fired {
			return fmt.Errorf("round %02d ticked at %s reached %q (%s), want one reflection input injected and consumed: the loop reported no consumption, so the watermark never advanced",
				record.index, record.now.UTC().Format(time.RFC3339Nano), record.disposition, record.reason)
		}
	}
	return nil
}

// goldenAssertWatermarkMonotone reports the first watermark read that did not move
// forward: the execution watermark is advanced by consumption only, so a read that
// regresses or stands still is a guard that did not hold.
func goldenAssertWatermarkMonotone(readings []goldenReading) error {
	for i := 1; i < len(readings); i++ {
		previous, next := readings[i-1], readings[i]
		if next.moment.Before(previous.moment) {
			return fmt.Errorf("watermark regressed between round %02d (%s) and round %02d (%s): the execution watermark never moves backwards",
				previous.round, previous.moment.UTC().Format(time.RFC3339Nano), next.round, next.moment.UTC().Format(time.RFC3339Nano))
		}
		if next.moment.Equal(previous.moment) {
			return fmt.Errorf("watermark stood still between round %02d and round %02d at %s: a consumed reflection advances it to the injection moment",
				previous.round, next.round, next.moment.UTC().Format(time.RFC3339Nano))
		}
	}
	return nil
}

// goldenAssertSingleConsumption reports the first fact the run consumed twice and the
// first round that committed more than one reflection input: the novelty fact each fired
// round consumed must be consumed by that round alone, and one round injects exactly one
// reflection input.
func goldenAssertSingleConsumption(ledger *reflectionLedger, firedOn map[event.EventID][]int, scanned map[event.EventID][]int, rounds []goldenRound) error {
	for key, seen := range firedOn {
		if len(seen) > 1 {
			return fmt.Errorf("fact %s fired rounds %v, want each fact consumed once: a repeated consumption means the watermark did not advance past what it covered", key, seen)
		}
	}
	for key, seen := range scanned {
		if len(seen) > 1 {
			return fmt.Errorf("fact %s was read by rounds %v, want each fact inside one window: a repeated window means the watermark did not advance past what it covered", key, seen)
		}
	}
	for _, record := range rounds {
		committed := goldenCountPrefix(ledger, fmt.Sprintf("golden-reflection-%02d", record.index))
		if committed != 1 {
			return fmt.Errorf("round %02d committed %d reflection input(s), want exactly one: a duplicate consumption is the second report this golden forbids",
				record.index, committed)
		}
	}
	return nil
}

// goldenAssertRestartContinuation reports whether the second half of the run resumed
// from the watermark the first half persisted: the record read off disk must be the
// last consumed moment, and every round after the restart must read and advance past it
// instead of starting over from the epoch. The facts a resumed loop reads are checked
// beside this by goldenAssertSingleConsumption: a window that opened again over covered
// facts is a duplicate consumption.
func goldenAssertRestartContinuation(persisted time.Time, before []goldenReading, rounds []goldenRound, restartAt int) error {
	if len(before) == 0 {
		return fmt.Errorf("the first half read no watermark, so the restart has nothing to continue from")
	}
	last := before[len(before)-1]
	if !persisted.Equal(last.moment) {
		return fmt.Errorf("the persisted watermark reads %s after the restart, want the last consumed moment %s: the watermark did not carry across the restart",
			persisted.UTC().Format(time.RFC3339Nano), last.moment.UTC().Format(time.RFC3339Nano))
	}
	resumed := 0
	for _, record := range rounds {
		if record.index < restartAt {
			continue
		}
		resumed++
		if record.readError != nil {
			return fmt.Errorf("round %02d after the restart cannot read its watermark: %v", record.index, record.readError)
		}
		if !record.reading.After(persisted) {
			return fmt.Errorf("round %02d after the restart read the watermark %s, want a moment after the persisted %s: the resumed loop must start strictly after what it consumed",
				record.index, record.reading.UTC().Format(time.RFC3339Nano), persisted.UTC().Format(time.RFC3339Nano))
		}
	}
	if resumed == 0 {
		return fmt.Errorf("no round ran after the restart at round %02d, so the continuation was never read", restartAt)
	}
	return nil
}

// goldenResources is the resource reading of one moment of the run.
type goldenResources struct {
	goroutines int
	fds        int
}

// goldenMeasure reads the goroutine count and the open file descriptors of the process.
func goldenMeasure() goldenResources {
	fds := 0
	if entries, err := os.ReadDir("/dev/fd"); err == nil {
		fds = len(entries)
	}
	return goldenResources{goroutines: runtime.NumGoroutine(), fds: fds}
}

// goldenSettle waits, bounded, for the goroutine count to fall back to the baseline: a
// goroutine of the run may still be unwinding when the last round returns, and the wait
// is a settle rather than a verdict (no assertion here reads the wall clock).
func goldenSettle(before goldenResources) goldenResources {
	deadline := time.Now().Add(5 * time.Second)
	for {
		after := goldenMeasure()
		if after.goroutines <= before.goroutines || time.Now().After(deadline) {
			return after
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// goldenAssertNoLeak reports a run that left goroutines or file descriptors behind: a
// long-time loop that leaks one of either per round is the leak this golden forbids.
func goldenAssertNoLeak(before, after goldenResources) error {
	if after.goroutines > before.goroutines {
		return fmt.Errorf("goroutines grew from %d to %d across the run, want no goroutine left behind by the loop", before.goroutines, after.goroutines)
	}
	if after.fds > before.fds {
		return fmt.Errorf("open file descriptors grew from %d to %d across the run, want no descriptor left behind by the loop", before.fds, after.fds)
	}
	return nil
}

// goldenRegressingGate is the mutation of the watermark source: it answers the persisted
// watermark until the named read and an earlier moment after it, so the golden's monotone
// reading is exercised through the production tick rather than on a copied series.
type goldenRegressingGate struct {
	inner meditation.WatermarkGate
	after int
	step  time.Duration

	mu    sync.Mutex
	calls int
}

// Current answers the persisted watermark, moved back by step once the armed read passed.
func (g *goldenRegressingGate) Current(ctx context.Context) (meditation.Watermark, error) {
	current, err := g.inner.Current(ctx)
	if err != nil {
		return meditation.Watermark{}, err
	}
	g.mu.Lock()
	g.calls++
	regress := g.calls > g.after
	g.mu.Unlock()
	if regress && !current.Moment.IsZero() {
		current.Moment = current.Moment.Add(-g.step)
	}
	return current, nil
}

// Advance hands the advance through to the persisted record.
func (g *goldenRegressingGate) Advance(ctx context.Context, moment time.Time) error {
	return g.inner.Advance(ctx, moment)
}

var _ meditation.WatermarkGate = (*goldenRegressingGate)(nil)

// goldenCountPrefix counts the facts of the chain committed under the named key prefix.
func goldenCountPrefix(ledger *reflectionLedger, prefix string) int {
	count := 0
	for key := range ledger.snapshot() {
		if strings.HasPrefix(string(key), prefix) {
			count++
		}
	}
	return count
}

// goldenInput is the loop-session face of the golden: it commits the reflection input as
// a fact of the agent's own partition, drives the agent's own loop session with it and
// reports what became of that turn. A mutation arms the shape a loop without the
// consumption report would have.
type goldenInput struct {
	fixture   *goldenFixture
	ledger    *reflectionLedger
	loop      *agent.Loop
	address   event.Address
	instance  event.InstanceID
	partition int

	mu    sync.Mutex
	round int
}

// InjectReflection commits the input, drives the agent's own loop session and reports
// the outcome of that turn.
func (i *goldenInput) InjectReflection(ctx context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	i.mu.Lock()
	round := i.round
	i.mu.Unlock()
	key := event.EventID(fmt.Sprintf("golden-reflection-%02d", round))
	evt.Key = key
	i.ledger.record(evt, i.partition)

	if i.fixture.mode == goldenDuplicate {
		duplicate := evt
		duplicate.Key = event.EventID(fmt.Sprintf("%s-dup", key))
		i.ledger.record(duplicate, i.partition)
	}
	switch i.fixture.mode {
	case goldenInFlight:
		return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionInFlight, At: evt.Timestamp, Reason: "the mutation reports a turn that never reached an outcome"}, nil
	case goldenNoDrive:
		return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionConsumed, At: evt.Timestamp}, nil
	}

	turn, err := i.loop.Drive(ctx, agent.Invocation{
		InvocationID: event.InvocationID("golden-invocation-" + string(key)),
		Address:      i.address,
		Instance:     i.instance,
		Ready:        []event.Event{evt},
	})
	if err != nil {
		return meditation.ReflectionReceipt{}, err
	}
	if turn.Terminal != model.KindCompleted {
		return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionDeferred, At: evt.Timestamp, Reason: "the reflection turn did not reach a completed terminal"}, nil
	}
	return meditation.ReflectionReceipt{Key: key, Outcome: meditation.ReflectionConsumed, At: evt.Timestamp}, nil
}

var _ meditation.ReflectionInput = (*goldenInput)(nil)

// goldenSessions is the live-binding face a reflection resolves its declared deliver_to
// targets against.
type goldenSessions map[event.AgentID][]event.Address

// ActiveSessions reports the sessions one agent currently binds.
func (s goldenSessions) ActiveSessions(id event.AgentID) []event.Address {
	return append([]event.Address(nil), s[id]...)
}

var _ meditation.SessionQuery = goldenSessions(nil)

// goldenDelivery is the turn side of one reflection's delivery face: the production turn
// issues one send per live session of every declared target, and the fact identity is the
// delivery identity, so a re-presented body lands no second fact. The delivery it hands
// over is counted off the chain, not off its own tally.
type goldenDelivery struct {
	ledger  *reflectionLedger
	replay  bool
	targets []string
}

// deliverRound delivers round's reflection result to every declared target once, naming
// the live session the same way the reflection input lists it, and re-presents the same
// call once: a replay re-presents the identical call identity, so nothing lands twice
// unless the fixture is armed to duplicate the delivery.
func (d *goldenDelivery) deliverRound(round int, now time.Time) {
	presentations := 1
	if d.replay {
		presentations = 2
	}
	for _, target := range d.targets {
		for presentation := 0; presentation < presentations; presentation++ {
			call := fmt.Sprintf("golden-delivery-%02d-%s", round, target)
			if presentation > 0 {
				call = fmt.Sprintf("%s#%d", call, presentation)
			}
			evt := event.Event{
				Type:      event.TypeExternalInput,
				Timestamp: now,
				Key:       event.EventID(call),
				Meta:      map[string]string{event.MetaKeyTriggerSource: "send", event.MetaKeyEventSummary: fmt.Sprintf("round %02d reflection result", round)},
				Message:   model.Message{Role: model.RoleUser, Content: fmt.Sprintf("round %02d reflection result", round)},
			}
			addr := event.Address{AgentID: event.AgentID(target), SessionID: event.SessionID("session-" + target)}
			evt.Owner, evt.Source, evt.Target = addr, addr, addr
			d.ledger.record(evt, memory.PartitionIDFromName(target))
		}
	}
}

// goldenAssertDeliveryOncePerTarget reports the first round that delivered anything other
// than exactly one body to one of its declared targets: a target delivered twice would
// read the same reflection result twice, and a target delivered nothing was silently
// skipped.
func goldenAssertDeliveryOncePerTarget(ledger *reflectionLedger, targets []string, rounds int) error {
	for round := 0; round < rounds; round++ {
		for _, target := range targets {
			prefix := fmt.Sprintf("golden-delivery-%02d-%s", round, target)
			if got := goldenCountPrefix(ledger, prefix); got != 1 {
				return fmt.Errorf("round %02d delivered %d body(ies) to target %q, want exactly one", round, got, target)
			}
		}
	}
	return nil
}
