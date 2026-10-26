package evolutionmeditationexample

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/evolution/meditation"
)

// heldWatermark is the canonical freshness gate over the execution watermark: it reports
// the watermark it holds and advances it to the moment a consumed reflection turn was
// injected at.
type heldWatermark struct {
	moment time.Time
	known  bool
}

// Current returns the current execution watermark; a watermark that cannot be
// established is reported as an error and the gate stays closed.
func (w *heldWatermark) Current(context.Context) (meditation.Watermark, error) {
	if !w.known {
		return meditation.Watermark{}, errors.New("evolution: the execution watermark is not established")
	}
	return meditation.Watermark{Moment: w.moment}, nil
}

// Advance moves the watermark to the moment a consumed reflection turn was injected at.
func (w *heldWatermark) Advance(_ context.Context, moment time.Time) error {
	if !w.known {
		return errors.New("evolution: the execution watermark is not established")
	}
	w.moment = moment
	return nil
}

var _ meditation.WatermarkGate = (*heldWatermark)(nil)

// ExampleWatermarkGate reads the watermark, advances it past a consumed turn and reports
// that a gate without an established watermark stays closed.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func ExampleWatermarkGate() {
	ctx := context.Background()
	gate := &heldWatermark{known: true, moment: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	if _, err := gate.Current(ctx); err != nil {
		return
	}
	if err := gate.Advance(ctx, gate.moment.Add(time.Minute)); err != nil {
		return
	}
	advanced, err := gate.Current(ctx)
	if err != nil {
		return
	}

	closed := &heldWatermark{}
	if _, err := closed.Current(ctx); err == nil {
		return
	}
	_ = advanced.Moment
}
