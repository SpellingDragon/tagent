package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// layeredReading is one post-commit face owing nothing of the fact it reads: it
// records every fact it was dispatched and answers the failure it was built with.
type layeredReading struct {
	invariantPlugin
	seen *[]event.EventID
	err  error
}

// ObserveCommit records the dispatched fact and answers the configured failure.
func (p *layeredReading) ObserveCommit(_ context.Context, id event.EventID) error {
	*p.seen = append(*p.seen, id)
	return p.err
}

// layeredDriver is one post-commit face carrying the driver duty: the obligation
// its observation starts stays owed here, so a failed observation keeps it owed
// and a delivered one clears it.
type layeredDriver struct {
	layeredReading
	owed *bool
}

// DrivesObligation marks the face's driver duty.
func (p *layeredDriver) DrivesObligation() {}

// ObserveCommit records the dispatch, keeps the obligation owed exactly while the
// observation failed, and answers that failure.
func (p *layeredDriver) ObserveCommit(ctx context.Context, id event.EventID) error {
	err := p.layeredReading.ObserveCommit(ctx, id)
	*p.owed = err != nil
	return err
}

// TestObserverLayering pins the two duties one fact->observation face may carry.
// - an observer-only failure is a named gap and the pass dispatches the later faces
// - an observer-only failure never reads as a retained obligation
// - a driver failure is reported beside the obligation its own account keeps
// - one pass answers both duties by their own semantics, in declared order
// - a driver failure is never a log line alone: the pass answers the gap it leaves
// - the pass retries nothing and clears nothing a driver owes
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestObserverLayering(t *testing.T) {
	ctx := context.Background()

	t.Run("an observer-only failure is a gap and the pass dispatches the later faces", func(t *testing.T) {
		var seen []event.EventID
		failure := errors.New("the reading did not land")
		broken := &layeredReading{
			invariantPlugin: invariantPlugin{name: "reading-broken", port: PortFactObservation},
			seen:            &seen,
			err:             failure,
		}
		sound := &layeredReading{
			invariantPlugin: invariantPlugin{name: "reading-sound", port: PortFactObservation},
			seen:            &seen,
		}
		pipeline, err := NewFlow([]Instance{broken, sound})
		if err != nil {
			t.Fatalf("register the two readings: %v", err)
		}
		presentation := pipeline.After(ctx, "fact-1")
		if len(presentation.Gaps) != 1 {
			t.Fatalf("gaps = %+v, want the one failed reading recorded", presentation.Gaps)
		}
		gap := presentation.Gaps[0]
		if gap.Plugin != "reading-broken" || gap.Fact != "fact-1" || !errors.Is(gap.Err, failure) {
			t.Fatalf("gap = %+v, want the plugin, fact and cause named", gap)
		}
		if errors.Is(gap.Err, ErrObservationObligationRetained) {
			t.Fatalf("gap error = %v, want a reading gap: an observer-only face owes no work", gap.Err)
		}
		if len(seen) != 2 || seen[1] != "fact-1" {
			t.Fatalf("dispatched facts = %v, want the later reading dispatched after the failed one", seen)
		}
	})

	t.Run("a driver failure keeps its obligation beside the cause", func(t *testing.T) {
		var seen []event.EventID
		owed := false
		failure := errors.New("the settle fact could not be read")
		driver := &layeredDriver{
			layeredReading: layeredReading{
				invariantPlugin: invariantPlugin{name: "driver", port: PortFactObservation},
				seen:            &seen,
				err:             failure,
			},
			owed: &owed,
		}
		pipeline, err := NewFlow([]Instance{driver})
		if err != nil {
			t.Fatalf("register the driver: %v", err)
		}
		presentation := pipeline.After(ctx, "fact-2")
		if len(presentation.Gaps) != 1 {
			t.Fatalf("gaps = %+v, want the failed driver recorded", presentation.Gaps)
		}
		gap := presentation.Gaps[0]
		if gap.Plugin != "driver" || gap.Fact != "fact-2" {
			t.Fatalf("gap = %+v, want the driver and the fact named", gap)
		}
		if !errors.Is(gap.Err, ErrObservationObligationRetained) {
			t.Fatalf("gap error = %v, want the retained obligation beside it: a driver failure is not a missing reading", gap.Err)
		}
		if !errors.Is(gap.Err, failure) {
			t.Fatalf("gap error = %v, want the cause still readable through it", gap.Err)
		}
		if !owed {
			t.Fatalf("the obligation was cleared by the pass: a driver failure keeps the work owed in its own account")
		}
		if len(seen) != 1 {
			t.Fatalf("driver dispatched %d time(s), want exactly one: the pass retries nothing", len(seen))
		}
	})

	t.Run("one pass answers both duties by their own semantics", func(t *testing.T) {
		var seen []event.EventID
		owed := false
		readingFailure := errors.New("the reading did not land")
		driverFailure := errors.New("the settle fact could not be read")
		reading := &layeredReading{
			invariantPlugin: invariantPlugin{name: "reading", port: PortFactObservation},
			seen:            &seen,
			err:             readingFailure,
		}
		driver := &layeredDriver{
			layeredReading: layeredReading{
				invariantPlugin: invariantPlugin{name: "driver", port: PortFactObservation},
				seen:            &seen,
				err:             driverFailure,
			},
			owed: &owed,
		}
		pipeline, err := NewFlow([]Instance{reading, driver})
		if err != nil {
			t.Fatalf("register both duties on one port: %v", err)
		}
		presentation := pipeline.After(ctx, "fact-3")
		if len(presentation.Gaps) != 2 {
			t.Fatalf("gaps = %+v, want one gap per failed face", presentation.Gaps)
		}
		if errors.Is(presentation.Gaps[0].Err, ErrObservationObligationRetained) {
			t.Fatalf("first gap = %v, want the reading answered without a duty", presentation.Gaps[0].Err)
		}
		if !errors.Is(presentation.Gaps[1].Err, ErrObservationObligationRetained) {
			t.Fatalf("second gap = %v, want the driver answered beside its obligation", presentation.Gaps[1].Err)
		}
		if !owed {
			t.Fatalf("the driver obligation was cleared: both duties share the port and never the failure semantics")
		}
	})

	t.Run("a driver failure is never a log line alone", func(t *testing.T) {
		var seen []event.EventID
		var order []string
		owed := true
		driver := &layeredDriver{
			layeredReading: layeredReading{
				invariantPlugin: invariantPlugin{name: "driver", port: PortFactObservation},
				seen:            &seen,
				err:             errors.New("the settle fact could not be read"),
			},
			owed: &owed,
		}
		presenter := invariantPresenter{
			invariantPlugin: invariantPlugin{name: "presenter", port: PortFactView},
			order:           &order,
			text:            "rendered",
		}
		pipeline, err := NewFlow([]Instance{driver, presenter})
		if err != nil {
			t.Fatalf("register the failing driver beside the presenter: %v", err)
		}
		presentation := pipeline.After(ctx, "fact-4")
		if len(presentation.Gaps) != 1 {
			t.Fatalf("gaps = %+v, want the driver failure answered on the presentation: a pass that logged it and carried on answers none", presentation.Gaps)
		}
		if len(presentation.Views) != 1 || presentation.Views[0].Text != "rendered" {
			t.Fatalf("views = %+v, want the fact still presented after the driver failed", presentation.Views)
		}
	})
}
