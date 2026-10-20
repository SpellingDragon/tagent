package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
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

// TestCompressRequestObservation pins the observation the filing log writes for every judgment and retirement.
// - a filed judgment writes one DEBUG line carrying the producer, the reason, the request identity and the result
// - a repeated filing and a repeated retirement write their own line instead of staying silent
// - a logger that fails blocks neither the filing nor the retirement: the bypass drops the line and keeps the record
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#output-overflow
func TestCompressRequestObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("a filed judgment writes its line beside the record", func(t *testing.T) {
		observer := &filingObserver{}
		defer watchFilingLog(t, observer)()
		log := NewCompressRequests()
		request, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation))
		if err != nil {
			t.Fatalf("file the judgment: %v", err)
		}
		if !observer.mentions("debug", "compress request", "result=filed", "producer="+CompressProducerMeditation,
			"reason="+CompressReasonMeditation, "id="+string(request.ID), "pending=1") {
			t.Fatalf("debug lines = %v, want the filed judgment beside its producer and reason", observer.all())
		}
	})

	t.Run("a repeated filing and a repeated retirement write their own lines", func(t *testing.T) {
		observer := &filingObserver{}
		defer watchFilingLog(t, observer)()
		log := NewCompressRequests()
		request, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation))
		if err != nil {
			t.Fatalf("file the judgment: %v", err)
		}
		if _, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation)); err != nil {
			t.Fatalf("repeat the judgment: %v", err)
		}
		if !observer.mentions("debug", "compress request", "result=recorded", "id="+string(request.ID)) {
			t.Fatalf("debug lines = %v, want the repeated judgment told apart from the first filing", observer.all())
		}
		if err := log.Consume(ctx, request.ID); err != nil {
			t.Fatalf("retire the request: %v", err)
		}
		if !observer.mentions("debug", "compress request retired", "result=retired", "id="+string(request.ID), "pending=0") {
			t.Fatalf("debug lines = %v, want the retirement of the request", observer.all())
		}
		if err := log.Consume(ctx, request.ID); err == nil {
			t.Fatal("repeated retirement = nil, want the named refusal")
		}
		if !observer.mentions("debug", "compress request retired", "result=refused", "id="+string(request.ID)) {
			t.Fatalf("debug lines = %v, want the refused retirement", observer.all())
		}
	})

	t.Run("a failing logger blocks neither the filing nor the retirement", func(t *testing.T) {
		observer := &filingObserver{panicsLeft: 4}
		defer watchFilingLog(t, observer)()
		log := NewCompressRequests()
		request, err := log.Produce(ctx, event.CompressRequestFor(CompressProducerMeditation, "seq-1", CompressReasonMeditation))
		if err != nil {
			t.Fatalf("file the judgment beside a failing logger: %v", err)
		}
		if pending, err := log.Pending(ctx); err != nil || len(pending) != 1 {
			t.Fatalf("pending beside a failing logger = (%v, %v), want the one recorded request", pending, err)
		}
		if err := log.Consume(ctx, request.ID); err != nil {
			t.Fatalf("retire the request beside a failing logger: %v", err)
		}
		if pending, err := log.Pending(ctx); err != nil || len(pending) != 0 {
			t.Fatalf("pending after the retirement = (%v, %v), want none", pending, err)
		}
	})
}

// filingObserver is the framework logger the observation checks of this package
// read the compress request face through: it records every line beside its level
// and refuses the writes its counter still holds by panicking.
type filingObserver struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (o *filingObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.panicsLeft > 0 {
		o.panicsLeft--
		panic("the logger refused the write")
	}
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *filingObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *filingObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *filingObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *filingObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *filingObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *filingObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *filingObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *filingObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *filingObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *filingObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *filingObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *filingObserver) mentions(level string, fragments ...string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range o.lines {
		if !strings.HasPrefix(line, level+": ") {
			continue
		}
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
				break
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchFilingLog points the framework logger at the observer for the duration of
// one check and answers the restore that ends it, so a check reads the lines of
// its own face and leaves the package logger as it found it.
func watchFilingLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}
