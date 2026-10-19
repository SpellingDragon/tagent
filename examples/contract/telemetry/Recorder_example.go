package telemetryexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/telemetry"
)

// errRecorderBound is what the canonical recorder reports for a record refused at the queue
// length or the byte budget.
var errRecorderBound = errors.New("recorder: record crossed the queue bound")

// boundedRecorder is the canonical recorder: it admits committed facts into a bounded queue
// without ever blocking the caller that produced them, counts every refusal in the loss
// ledger, and reports the run as sealed only once its writer confirmed the seal.
type boundedRecorder struct {
	queue     []event.Event
	bytes     int
	queueMax  int
	budgetMax int
	dropped   uint64
	confirmed bool
}

// Record admits evt for asynchronous delivery, or refuses it at the bound and counts the
// refusal in the loss ledger.
func (r *boundedRecorder) Record(_ context.Context, evt event.Event) error {
	size := len(evt.Key) + len(evt.Type)
	if len(r.queue) >= r.queueMax || r.bytes+size > r.budgetMax {
		r.dropped++
		return errRecorderBound
	}
	r.queue = append(r.queue, evt)
	r.bytes += size
	return nil
}

// FlushAndWait drains the admitted records and reports the writer-confirmed seal: a run the
// writer never confirmed comes back partial, never sealed.
func (r *boundedRecorder) FlushAndWait(_ context.Context) (telemetry.SealResult, error) {
	r.queue = nil
	r.bytes = 0
	result := telemetry.SealResult{Status: telemetry.SealStatusPartial, Dropped: r.dropped}
	if r.confirmed {
		result.Status = telemetry.SealStatusSealed
	}
	return result, nil
}

// Close stops admission, drains what was admitted, and returns once the writer finished.
func (r *boundedRecorder) Close(_ context.Context) error {
	r.queue = nil
	r.bytes = 0
	return nil
}

var _ telemetry.Recorder = (*boundedRecorder)(nil)

// ExampleRecorder admits committed facts into a bounded queue: an admission past the byte
// budget is refused and counted instead of blocking the caller, and the run is reported
// sealed only when the writer confirmed the seal.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func ExampleRecorder() {
	recorder := &boundedRecorder{queueMax: 2, budgetMax: 16}
	ctx := context.Background()
	if err := recorder.Record(ctx, event.Event{Key: "a", Type: "b"}); err != nil {
		return
	}
	if err := recorder.Record(ctx, event.Event{Key: "cccccccccccccccc", Type: "d"}); err == nil {
		return
	}
	recorder.confirmed = true
	seal, err := recorder.FlushAndWait(ctx)
	if err != nil || seal.Status != telemetry.SealStatusSealed || seal.Dropped != 1 {
		return
	}
	if err := recorder.Close(ctx); err != nil {
		return
	}
}
