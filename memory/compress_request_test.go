package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// requestNow is the filing moment the request-log invariants stamp judgments
// with, so a repeated filing is judged against one recorded moment.
var requestNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// TestCompressionRequestLogHoldsJudgments pins the filing log of the compress request family: one request kept on record per judgment.
// - a judgment naming no producer, subject or reason is refused by name
// - a repeated filing answers the recorded request and keeps its first filing moment
// - a retirement of a request that does not await consumption is refused by name
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressionRequestLogHoldsJudgments(t *testing.T) {
	ctx := context.Background()

	t.Run("a judgment naming nothing is refused by name", func(t *testing.T) {
		log := NewCompressRequests()
		for name, request := range map[string]event.CompressRequest{
			"no producer": {Subject: "seq-1", Reason: "a ground"},
			"no subject":  {Producer: CompressProducerMeditation, Reason: "a ground"},
			"no reason":   {Producer: CompressProducerMeditation, Subject: "seq-1"},
		} {
			if _, err := log.Produce(ctx, request); !errors.Is(err, ErrCompressRequestUnnamed) {
				t.Fatalf("%s = %v, want %v", name, err, ErrCompressRequestUnnamed)
			}
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending after refused judgments = (%v, %v), want none", pending, err)
		}
	})

	t.Run("a repeated filing keeps the recorded request", func(t *testing.T) {
		log := NewCompressRequests()
		first, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation))
		if err != nil {
			t.Fatalf("file the first judgment: %v", err)
		}
		if first.At.IsZero() {
			t.Fatal("the filed request carries no filing moment, want the moment it was filed")
		}
		repeat := event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation)
		repeat.At = requestNow
		second, err := log.Produce(ctx, repeat)
		if err != nil {
			t.Fatalf("file the repeated judgment: %v", err)
		}
		if second.ID != first.ID || !second.At.Equal(first.At) {
			t.Fatalf("repeated filing = (%q, %s), want the recorded (%q, %s): one judgment keeps one filing", second.ID, second.At, first.ID, first.At)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending after a repeated filing = (%v, %v), want the one request", pending, err)
		}
	})

	t.Run("a retirement of a request that does not await consumption is refused", func(t *testing.T) {
		log := NewCompressRequests()
		request, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation))
		if err != nil {
			t.Fatalf("file the judgment: %v", err)
		}
		if err := log.Consume(ctx, request.ID); err != nil {
			t.Fatalf("retire the pending request: %v", err)
		}
		if err := log.Consume(ctx, request.ID); !errors.Is(err, ErrCompressRequestNotPending) {
			t.Fatalf("repeated retirement = %v, want %v", err, ErrCompressRequestNotPending)
		}
	})
}
