package evolution

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestMeditationYieldsBesideAnActiveSession drives the activity side of the rhythm gate through the production driver and loop.
// - It checks the structural judgments the gate reads beside the execution interval.
//   - the newest host-facing fact younger than the min gap holds the tick, and the same face fires once that fact has aged
//   - the decision seat of the session that fact belongs to holds the pass while a model decision is in flight there
//   - the next host-facing commit fires once that seat is released
//   - a self-managed round beside an older host-facing fact still fires on the rhythm
//   - a held tick leaves the watermark and the pending input untouched, so the reflection is postponed, never dropped
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationYieldsBesideAnActiveSession(t *testing.T) {
	t.Run("fresh_host_fact_holds_the_tick", func(t *testing.T) {
		ctx := context.Background()
		rig := newReflectionRig(t, reflectionConfig(), t.TempDir())
		owner := activitySession()
		at := time.Unix(0, 0).Add(2 * time.Minute)
		key := rig.ledger.record(ownedFact("activity-fresh", "the host material recorded a minute ago", at, owner), rig.partition)

		held := rig.reflection.Tick(ctx, at.Add(time.Minute))
		if held.Decision.Disposition != meditation.TickRhythm {
			t.Fatalf("tick beside the fresh host fact = (%q, %s), want the rhythm gate yielding to the session that just spoke", held.Decision.Disposition, held.Decision.Reason)
		}
		if held.Scan == nil || held.Scan.Verdict.Recent == nil || held.Scan.Verdict.Recent.Identity.Key != key {
			t.Fatalf("held face = %+v, want the fresh host fact inside the window", held.Scan)
		}
		if got := rig.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want none while the session speaks", got)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("the held tick left a reflection input pending, want the gate open for the next evaluation")
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the held tick = %s, want it unmoved", got)
		}

		aged := at.Add(2 * time.Hour)
		settled := rig.reflection.Tick(ctx, aged)
		if settled.Decision.Disposition != meditation.TickFired || settled.Injection == nil {
			t.Fatalf("tick after the host fact aged = (%q, %s), want the same face firing once the min gap has elapsed", settled.Decision.Disposition, settled.Decision.Reason)
		}
		if settled.Scan == nil || settled.Scan.Verdict.Recent == nil || settled.Scan.Verdict.Recent.Identity.Key != key {
			t.Fatalf("fired face = %+v, want the postponed material still inside the window", settled.Scan)
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want exactly the one postponed reflection", got)
		}
	})

	t.Run("held_decision_seat_holds_the_tick", func(t *testing.T) {
		ctx := context.Background()
		cfg := reflectionConfig()
		cfg.Cadence.MinGap = 0
		rig := newReflectionRig(t, cfg, t.TempDir())
		owner := activitySession()
		instance := event.InstanceID("instance-activity")
		seats := state.NewOccupancy()
		if err := seats.Occupy(owner, instance, "invocation-held", "decision-held"); err != nil {
			t.Fatalf("hold the decision seat: %v", err)
		}
		driver := meditation.NewDriver(rig.reflection, 0, 0, seats.Slot, instance)
		outcomes := make(chan meditation.TickOutcome, 4)
		driver.SetObserver(func(outcome meditation.TickOutcome) { outcomes <- outcome })
		t.Cleanup(driver.Stop)

		first := rig.ledger.record(ownedFact("activity-seat", "the material committed while the seat is held", time.Now().Add(-time.Minute), owner), rig.partition)
		if err := driver.ObserveCommit(ctx, first); err != nil {
			t.Fatalf("observe the commit: %v", err)
		}
		select {
		case outcome := <-outcomes:
			if outcome.Decision.Disposition != meditation.TickRhythm {
				t.Fatalf("pass beside the held seat = (%q, %s), want the reflection waiting for the seat", outcome.Decision.Disposition, outcome.Decision.Reason)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the driver reported no pass beside the held seat")
		}
		if got := rig.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want none while the seat is held", got)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("the held pass left a reflection input pending, want the gate open for the next commit")
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the held pass = %s, want it unmoved", got)
		}

		if err := seats.Vacate(owner, instance, "invocation-held"); err != nil {
			t.Fatalf("release the decision seat: %v", err)
		}
		second := rig.ledger.record(ownedFact("activity-seat-next", "the next host-facing commit of that session", time.Now(), owner), rig.partition)
		if err := driver.ObserveCommit(ctx, second); err != nil {
			t.Fatalf("observe the next commit: %v", err)
		}
		select {
		case outcome := <-outcomes:
			if outcome.Decision.Disposition != meditation.TickFired || outcome.Injection == nil {
				t.Fatalf("pass after the seat was released = (%q, %s), want the postponed reflection firing", outcome.Decision.Disposition, outcome.Decision.Reason)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the driver fired no pass after the seat was released")
		}
		if got := rig.input.injectedCount(); got != 1 {
			t.Fatalf("injected inputs = %d, want exactly the one postponed reflection", got)
		}
	})

	t.Run("self_managed_turns_hold_nothing", func(t *testing.T) {
		ctx := context.Background()
		cfg := reflectionConfig()
		cfg.Cadence.MinGap = 5 * time.Minute
		rig := newReflectionRig(t, cfg, t.TempDir())
		owner := activitySession()
		hostAt := time.Unix(0, 0).Add(10 * time.Minute)
		hostKey := rig.ledger.record(ownedFact("activity-host", "the host material the rhythm reflects on", hostAt, owner), rig.partition)
		ownKey := rig.ledger.record(lineageFact("activity-own", "the settlement summary the agent wrote for itself", meditation.LineageReflection, hostAt.Add(9*time.Minute)), rig.partition)

		tick := rig.reflection.Tick(ctx, hostAt.Add(10*time.Minute))
		if tick.Decision.Disposition != meditation.TickFired || tick.Injection == nil {
			t.Fatalf("tick beside the self-managed round = (%q, %s), want the rhythm reached at its own time", tick.Decision.Disposition, tick.Decision.Reason)
		}
		if tick.Scan == nil || tick.Scan.Verdict.Recent == nil || tick.Scan.Verdict.Recent.Identity.Key != hostKey {
			t.Fatalf("judged face = %+v, want the host material as the newest host-facing fact", tick.Scan)
		}
		selfManaged := false
		for _, fact := range tick.Scan.Facts {
			if fact.Identity.Key == ownKey {
				selfManaged = fact.Attribution.SelfManaged
			}
		}
		if !selfManaged {
			t.Fatalf("scanned facts = %+v, want the self-managed round read as such", tick.Scan.Facts)
		}
	})

	t.Run("next_host_commit_evaluates_again", func(t *testing.T) {
		ctx := context.Background()
		cfg := reflectionConfig()
		cfg.Cadence.MinGap = 5 * time.Minute
		rig := newReflectionRig(t, cfg, t.TempDir())
		owner := activitySession()
		firstAt := time.Unix(0, 0).Add(time.Minute)
		firstKey := rig.ledger.record(ownedFact("activity-first", "the host material committed while the seat was held", firstAt, owner), rig.partition)

		yielded := rig.reflection.Tick(ctx, firstAt.Add(time.Minute))
		if yielded.Decision.Disposition != meditation.TickRhythm {
			t.Fatalf("tick beside the fresh host fact = (%q, %s), want the reflection postponed to the next host-facing commit", yielded.Decision.Disposition, yielded.Decision.Reason)
		}
		if yielded.Scan == nil || yielded.Scan.Verdict.Recent == nil || yielded.Scan.Verdict.Recent.Identity.Key != firstKey {
			t.Fatalf("yielded face = %+v, want the fresh host fact inside the window", yielded.Scan)
		}
		if got := rig.input.injectedCount(); got != 0 {
			t.Fatalf("injected inputs = %d, want none beside the fresh host fact", got)
		}
		if _, pending := rig.reflection.Pending(); pending {
			t.Fatal("the postponed tick left a reflection input pending, want the next commit to evaluate the same material")
		}
		if got := rig.watermark(t); !got.IsZero() {
			t.Fatalf("watermark after the postponed tick = %s, want it unmoved", got)
		}

		nextAt := time.Unix(0, 0).Add(10 * time.Minute)
		nextKey := rig.ledger.record(ownedFact("activity-next", "the next host-facing commit of that session", nextAt, owner), rig.partition)
		fired := rig.reflection.Tick(ctx, nextAt.Add(cfg.Cadence.MinGap+time.Minute))
		if fired.Decision.Disposition != meditation.TickFired || fired.Injection == nil {
			t.Fatalf("tick on the next host-facing commit = (%q, %s), want the postponed reflection firing", fired.Decision.Disposition, fired.Decision.Reason)
		}
		if fired.Scan == nil || fired.Scan.Verdict.Recent == nil || fired.Scan.Verdict.Recent.Identity.Key != nextKey {
			t.Fatalf("fired face = %+v, want the next commit as the newest host-facing fact", fired.Scan)
		}
	})
}

// TestTheGateReevaluatesAfterTheWindow drives the held episode of the rhythm gate through the production driver.
// - the fact the commit itself filed is inside the min gap, so the first pass yields and injects nothing
// - the same episode re-evaluates on its standing wait without a second commit, and fires once the window is past
// - the held episode reported the rhythm once, so the wait is the window's remainder and never a spin
// - the fired pass reads the very material the held pass read, so the observation was postponed, never dropped
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestTheGateReevaluatesAfterTheWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := reflectionConfig()
	cfg.Cadence.Interval = 0
	cfg.Cadence.MinGap = 80 * time.Millisecond
	rig := newReflectionRig(t, cfg, t.TempDir())
	owner := activitySession()
	driver := meditation.NewDriver(rig.reflection, cfg.Cadence.Interval, cfg.Cadence.MinGap, nil, "")
	outcomes := make(chan meditation.TickOutcome, 64)
	driver.SetObserver(func(outcome meditation.TickOutcome) { outcomes <- outcome })
	t.Cleanup(driver.Stop)

	key := rig.ledger.record(ownedFact("window-commit", "the host material the commit itself filed", time.Now(), owner), rig.partition)
	if err := driver.ObserveCommit(ctx, key); err != nil {
		t.Fatalf("observe the commit: %v", err)
	}

	held := activityOutcome(t, outcomes, "held pass")
	if held.Decision.Disposition != meditation.TickRhythm {
		t.Fatalf("the pass beside the committing fact = (%q, %s), want the activity window holding it back", held.Decision.Disposition, held.Decision.Reason)
	}
	if held.Scan == nil || held.Scan.Verdict.Recent == nil || held.Scan.Verdict.Recent.Identity.Key != key {
		t.Fatalf("held face = %+v, want the committing fact as the newest host-facing fact", held.Scan)
	}
	if got := rig.input.injectedCount(); got != 0 {
		t.Fatalf("injected inputs = %d, want none while the window holds the pass back", got)
	}
	if _, pending := rig.reflection.Pending(); pending {
		t.Fatal("the held pass left a reflection input pending, want the gate open for the re-evaluation")
	}
	if got := rig.watermark(t); !got.IsZero() {
		t.Fatalf("watermark after the held pass = %s, want it unmoved", got)
	}

	heldPasses := 0
	var fired meditation.TickOutcome
	for fired.Decision.Disposition != meditation.TickFired {
		outcome := activityOutcome(t, outcomes, "re-evaluated pass")
		if outcome.Decision.Disposition == meditation.TickRhythm {
			heldPasses++
			continue
		}
		fired = outcome
	}
	if heldPasses != 0 {
		t.Fatalf("held passes after the first = %d, want the window waited out once: the standing wait is the window's remainder, not a re-pass every moment", heldPasses)
	}
	if fired.Injection == nil {
		t.Fatalf("the re-evaluated pass = (%q, %s), want the postponed reflection firing once the window is past", fired.Decision.Disposition, fired.Decision.Reason)
	}
	if fired.Scan == nil || fired.Scan.Verdict.Recent == nil || fired.Scan.Verdict.Recent.Identity.Key != key {
		t.Fatalf("fired face = %+v, want the very material the held pass read", fired.Scan)
	}
	if got := rig.input.injectedCount(); got != 1 {
		t.Fatalf("injected inputs = %d, want exactly the one postponed reflection", got)
	}
	if got := rig.watermark(t); got.IsZero() {
		t.Fatal("watermark after the fired pass is unset, want the consumed reflection advancing it")
	}
}

// activityOutcome waits for one pass reading of the driver under check.
func activityOutcome(t *testing.T, outcomes <-chan meditation.TickOutcome, what string) meditation.TickOutcome {
	t.Helper()
	select {
	case outcome := <-outcomes:
		return outcome
	case <-time.After(30 * time.Second):
		t.Fatalf("the driver reported no %s", what)
		return meditation.TickOutcome{}
	}
}

// activitySession is the address whose traffic the activity gate reads: the
// session every host-facing fact of these sub-cases belongs to.
func activitySession() event.Address {
	return event.Address{AgentID: "agent-a", SessionID: "session-activity"}
}

// ownedFact builds one host-facing fact recorded by the named session, so the
// judgment can read whose turn the material came from.
func ownedFact(key, content string, at time.Time, owner event.Address) event.Event {
	evt := externalFact(key, content, at)
	evt.Owner = owner
	evt.Source = owner
	return evt
}
