// The long-time reflection golden: the reflection loop of one agent run over a
// configurable number of rounds, read on the five faces D11 freezes — a watermark that
// never regresses, no fact consumed twice, no resource left behind, one delivery per
// declared target and a watermark that carries across a restart.
//
// Every round runs on an injected clock and a compressed cadence, so the loop costs the
// wall clock of the scripted model calls and nothing else. No assertion below reads the
// wall clock (R5): the reads are event counts and state readings.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
package evolution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
)

// TestFixtureReflectionLoop pins the golden's own fixtures before the long run leans on them.
//
//   - the fixture reports one reflection turn per round and the model records one decision per turn
//   - the rhythm floor the fixture injects is the compressed floor, not the production hours
//   - the fixture is red when the model is never driven, so the guard is not decorative
func TestFixtureReflectionLoop(t *testing.T) {
	const rounds = 6
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("the fixture drives one reflection turn per round", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenConsumed, nil)
		if got := fixture.reflection.MinGap(); got != goldenFloor {
			t.Fatalf("rhythm floor = %s, want the injected compressed floor %s", got, goldenFloor)
		}
		before := goldenMeasure()
		if before.fds == 0 {
			t.Fatalf("the descriptor counter read none on this platform, so the leak face would be read off an empty number")
		}
		fixture.runRounds(ctx, 0, rounds)
		after := goldenSettle(before)

		if err := goldenAssertEveryRoundFired(fixture.rounds); err != nil {
			t.Fatalf("the fixture reached no reflection turn: %v", err)
		}
		if err := fixture.requireModelCalls(rounds); err != nil {
			t.Fatalf("the fixture never reached the model: %v", err)
		}
		if err := goldenAssertWatermarkMonotone(fixture.readings); err != nil {
			t.Fatalf("watermark readings = %v: %v", fixture.readings, err)
		}
		if err := goldenAssertSingleConsumption(fixture.ledger, fixture.firedOn, fixture.scanned, fixture.rounds); err != nil {
			t.Fatalf("consumption of %d round(s): %v", rounds, err)
		}
		if err := goldenAssertNoLeak(before, after); err != nil {
			t.Fatalf("resources across %d round(s): %v", rounds, err)
		}
		if got := len(fixture.rounds); got != rounds {
			t.Fatalf("rounds = %d, want %d", got, rounds)
		}
	})

	t.Run("fixture_without_model_calls_fails", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenNoDrive, nil)
		fixture.runRounds(ctx, 0, 4)
		err := fixture.requireModelCalls(4)
		if err == nil {
			t.Fatal("a fixture that never drove the model passed its model-call guard, and the golden it backs would be green with no reflection turn behind it")
		}
		if !strings.Contains(err.Error(), "never drove the model") {
			t.Fatalf("guard = %v, want the missing model calls named", err)
		}
		if got := len(fixture.model.recorded()); got != 0 {
			t.Fatalf("model calls = %d beside a fixture that never drove the loop, want none", got)
		}
	})
}

// TestInstancesSymptomReflectionGolden is the I4 anchor: a long loop holds its watermark, consumption, resources and restart.
//
//   - 36 rounds each fire and consume exactly one reflection input, and the watermark advances to every injection moment
//   - no fact is consumed by two rounds and no round consumes its input twice
//   - the run is restarted in the middle, and the resumed loop consumes nothing the first half covered
//   - the loop leaves no goroutine and no file descriptor behind
//   - bite proofs: a regressing watermark source and a duplicated consumption are each red
func TestInstancesSymptomReflectionGolden(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Run("a long reflection loop holds its watermark, consumption and resources", func(t *testing.T) {
		if goldenRounds < 30 || goldenRounds > 50 {
			t.Fatalf("golden rounds = %d, want the 30–50 band D11 declares", goldenRounds)
		}
		before := goldenMeasure()
		dir := t.TempDir()
		first := newGoldenFixture(t, dir, goldenConfig(), goldenConsumed, nil)
		first.runRounds(ctx, 0, goldenRestartAt)

		persisted, err := first.persistedWatermark()
		if err != nil {
			t.Fatalf("read the persisted watermark before the restart: %v", err)
		}
		second := first.reassemble(goldenConsumed, nil)
		quiet := second.quietTick(ctx, goldenRestartAt, goldenEpoch.Add(time.Duration(goldenRestartAt)*goldenStep-goldenStep/2))
		if quiet.disposition != meditation.TickNoNovelty || quiet.fired {
			t.Fatalf("the first tick after the restart reached %q (%s) beside no new fact, want nothing fresh: the resumed loop consumed a fact the half before it already covered",
				quiet.disposition, quiet.reason)
		}
		second.runRounds(ctx, goldenRestartAt, goldenRounds)
		after := goldenSettle(before)

		if got := len(second.rounds); got != goldenRounds {
			t.Fatalf("rounds = %d, want %d", got, goldenRounds)
		}
		if err := goldenAssertEveryRoundFired(second.rounds); err != nil {
			t.Fatalf("the long reflection loop stopped firing: %v", err)
		}
		if err := goldenAssertWatermarkMonotone(second.readings); err != nil {
			t.Fatalf("watermark readings across %d round(s): %v", len(second.readings), err)
		}
		if err := goldenAssertSingleConsumption(second.ledger, second.firedOn, second.scanned, second.rounds); err != nil {
			t.Fatalf("consumption across %d round(s): %v", goldenRounds, err)
		}
		if err := goldenAssertRestartContinuation(persisted, first.readings, second.rounds, goldenRestartAt); err != nil {
			t.Fatalf("the restart of round %02d: %v", goldenRestartAt, err)
		}
		if err := goldenAssertNoLeak(before, after); err != nil {
			t.Fatalf("resources across %d round(s): %v", goldenRounds, err)
		}
		reread := newGoldenFixture(t, dir, goldenConfig(), goldenConsumed, nil)
		continued, err := reread.persistedWatermark()
		if err != nil {
			t.Fatalf("read the watermark the run left on disk: %v", err)
		}
		if continued.IsZero() || !continued.Equal(second.readings[len(second.readings)-1].moment) {
			t.Fatalf("persisted watermark = %s, want the last consumed moment %s",
				continued.UTC().Format(time.RFC3339Nano), second.readings[len(second.readings)-1].moment.UTC().Format(time.RFC3339Nano))
		}
		t.Logf("golden covered %d rounds across one restart, watermark %s -> %s; resources goroutines %d->%d, fds %d->%d; facts scanned %d, injected %d",
			goldenRounds, second.readings[0].moment.UTC().Format(time.RFC3339Nano), continued.UTC().Format(time.RFC3339Nano),
			before.goroutines, after.goroutines, before.fds, after.fds, len(second.scanned), len(second.rounds))
	})

	t.Run("restart_without_persisted_watermark_is_red", func(t *testing.T) {
		first := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenConsumed, nil)
		first.runRounds(ctx, 0, 6)
		stray := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenConsumed, nil)
		stray.ledger.restore(first.ledger)
		stray.readings = append([]goldenReading(nil), first.readings...)
		stray.rounds = append([]goldenRound(nil), first.rounds...)
		for key, rounds := range first.scanned {
			stray.scanned[key] = append([]int(nil), rounds...)
		}
		for key, rounds := range first.firedOn {
			stray.firedOn[key] = append([]int(nil), rounds...)
		}
		persisted, err := stray.persistedWatermark()
		if err != nil {
			t.Fatalf("read the stray anchor record: %v", err)
		}
		quiet := stray.quietTick(ctx, 6, goldenEpoch.Add(6*goldenStep-goldenStep/2))
		if quiet.disposition != meditation.TickFired {
			t.Fatalf("the mutation did not reach back over a covered fact: quiet tick = %q (%s), so it proves nothing", quiet.disposition, quiet.reason)
		}
		stray.runRounds(ctx, 6, 7)
		bite := goldenAssertRestartContinuation(persisted, first.readings, stray.rounds, 6)
		if bite == nil {
			t.Fatal("a restart that lost the persisted watermark was not caught")
		}
		if !strings.Contains(bite.Error(), "did not carry across the restart") {
			t.Fatalf("bite = %v, want the lost watermark named", bite)
		}
		replay := goldenAssertSingleConsumption(stray.ledger, stray.firedOn, stray.scanned, stray.rounds)
		if replay == nil {
			t.Fatal("a resumed loop that consumed a covered fact a second time was not caught as a duplicate consumption")
		}
		t.Logf("the golden bites on a restart that lost its watermark: %v / %v", bite, replay)
	})

	t.Run("watermark_regresses_is_red", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenConsumed, func(inner meditation.WatermarkGate) meditation.WatermarkGate {
			return &goldenRegressingGate{inner: inner, after: 4, step: 4 * goldenStep}
		})
		fixture.runRounds(ctx, 0, 8)
		if len(fixture.rounds) < 8 {
			t.Fatalf("rounds = %d, want the 8 the mutation was armed for", len(fixture.rounds))
		}
		err := goldenAssertWatermarkMonotone(fixture.readings)
		if err == nil {
			t.Fatalf("a watermark source that answers an earlier moment was not caught: readings = %v", fixture.readings)
		}
		if !strings.Contains(err.Error(), "regressed") {
			t.Fatalf("bite = %v, want the regression named", err)
		}
		t.Logf("the golden bites on a regressed watermark: %v", err)
	})

	t.Run("duplicate_consumption_is_red", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenDuplicate, nil)
		fixture.runRounds(ctx, 0, 6)
		err := goldenAssertSingleConsumption(fixture.ledger, fixture.firedOn, fixture.scanned, fixture.rounds)
		if err == nil {
			t.Fatal("a loop that consumed one round twice was not caught")
		}
		if !strings.Contains(err.Error(), "duplicate consumption") {
			t.Fatalf("bite = %v, want the duplicate consumption named", err)
		}
		t.Logf("the golden bites on a duplicated consumption: %v", err)
	})

	t.Run("no_consumption_report_is_red", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), goldenConfig(), goldenInFlight, nil)
		fixture.runRounds(ctx, 0, 4)
		err := goldenAssertEveryRoundFired(fixture.rounds)
		if err == nil {
			t.Fatalf("a loop that never reported an outcome passed the round guard: %v", fixture.rounds)
		}
		if !strings.Contains(err.Error(), "watermark never advanced") {
			t.Fatalf("bite = %v, want the missing consumption report named", err)
		}
		t.Logf("the golden bites on a loop that reports no consumption: %v", err)
	})
}

// TestReflectionGoldenMultiTarget pins the one delivery face of the long loop.
//
//   - the reflection input lists every declared target's live session exactly once, which is the list the turn sends against
//   - every round delivers exactly one body per declared target, counted off the chain
//   - bite proof: a turn that re-presents the same body under a second call identity is red
func TestReflectionGoldenMultiTarget(t *testing.T) {
	const rounds = 8
	targets := []string{"peer-a", "peer-b", "peer-c"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sessions := goldenSessions{}
	for _, target := range targets {
		sessions[event.AgentID(target)] = []event.Address{
			{AgentID: event.AgentID(target), SessionID: event.SessionID("session-" + target)},
		}
	}
	cfg := goldenConfig()
	cfg.Face.ObservedNamespaces = append([]string{"agent-a"}, targets...)
	cfg.Face.ReadNamespaces = append([]string{"agent-a"}, targets...)
	cfg.Delivery = meditation.ReflectionDelivery{Targets: targets, Sessions: sessions}

	t.Run("every declared target receives the result exactly once", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), cfg, goldenConsumed, nil)
		fixture.delivery = &goldenDelivery{ledger: fixture.ledger, targets: targets}
		for round := 0; round < rounds; round++ {
			record := fixture.tickRound(ctx, round)
			if record.disposition != meditation.TickFired {
				t.Fatalf("round %02d = %q (%s), want the reflection firing", round, record.disposition, record.reason)
			}
			for _, target := range targets {
				listed := fmt.Sprintf("- agent %q, session %q", target, "session-"+target)
				if got := strings.Count(record.body, listed); got != 1 {
					t.Fatalf("round %02d reflection input lists %q %d time(s), want the live session listed exactly once:\n%s", round, listed, got, record.body)
				}
			}
			fixture.delivery.deliverRound(round, record.now)
		}
		if err := goldenAssertDeliveryOncePerTarget(fixture.ledger, targets, rounds); err != nil {
			t.Fatalf("delivery across %d round(s): %v", rounds, err)
		}
		for _, target := range targets {
			if got := goldenCountPrefix(fixture.ledger, fmt.Sprintf("golden-delivery-%02d-%s", 0, target)); got != 1 {
				t.Fatalf("round 00 delivery to %q = %d fact(s), want one", target, got)
			}
			if got := goldenCountPrefix(fixture.ledger, "golden-delivery-"); got != rounds*len(targets) {
				t.Fatalf("delivery facts = %d, want %d: one per target per round", got, rounds*len(targets))
			}
		}
		if err := fixture.requireModelCalls(rounds); err != nil {
			t.Fatalf("the delivery rounds never reached the model: %v", err)
		}
	})

	t.Run("duplicate_target_delivery_is_red", func(t *testing.T) {
		fixture := newGoldenFixture(t, t.TempDir(), cfg, goldenConsumed, nil)
		fixture.delivery = &goldenDelivery{ledger: fixture.ledger, targets: targets, replay: true}
		for round := 0; round < 3; round++ {
			record := fixture.tickRound(ctx, round)
			if record.disposition != meditation.TickFired {
				t.Fatalf("round %02d = %q (%s), want the reflection firing", round, record.disposition, record.reason)
			}
			fixture.delivery.deliverRound(round, record.now)
		}
		err := goldenAssertDeliveryOncePerTarget(fixture.ledger, targets, 3)
		if err == nil {
			t.Fatal("a turn that delivered the same round's body twice to one target was not caught")
		}
		if !strings.Contains(err.Error(), "want exactly one") {
			t.Fatalf("bite = %v, want the duplicated delivery named", err)
		}
		t.Logf("the golden bites on a duplicated target delivery: %v", err)
	})
}
