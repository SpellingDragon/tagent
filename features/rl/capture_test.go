package rl

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestCaptureSinkAccountsEveryBound pins the capture loss ledger: each bound counts its own loss, and a seal without a writer seal is partial.
// - a negative bound, a missing capture id and a missing writer are refused at construction
// - a record above the per-record bound is counted under that bound
// - a record refused because the in-flight bound is reached is counted under that bound
// - a record refused because the byte budget is spent is counted under that bound
// - the seal's accounted records are the ones written, and the frozen account is not rewritten by a later refusal
// - a record offered to a sealed capture is counted closed and refuses by name
// - a writer that reports no sync leaves the capture partial with its reason
// - a seal the writer did not confirm in the deadline is abandoned, keeping the in-flight count
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestCaptureSinkAccountsEveryBound(t *testing.T) {
	ctx := context.Background()

	t.Run("construction refusals", func(t *testing.T) {
		if _, err := NewCaptureSink("", SinkConfig{}, &recordingWriter{synced: true}); !errors.Is(err, ErrCaptureIDMissing) {
			t.Fatalf("sink without a capture id = %v, want ErrCaptureIDMissing", err)
		}
		if _, err := NewCaptureSink("cap:1", SinkConfig{}, nil); !errors.Is(err, ErrWriterMissing) {
			t.Fatalf("sink without a writer = %v, want ErrWriterMissing", err)
		}
		if _, err := NewCaptureSink("cap:1", SinkConfig{MaxInFlight: -1}, &recordingWriter{synced: true}); !errors.Is(err, ErrNegativeBound) {
			t.Fatalf("sink with a negative bound = %v, want ErrNegativeBound", err)
		}
	})

	t.Run("the per-record bound counts its own loss", func(t *testing.T) {
		writer := &recordingWriter{synced: true}
		sink, err := NewCaptureSink("cap:size", SinkConfig{MaxRecordBytes: 8}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		admission, err := sink.Record(ctx, sinkAttempt("at:size"))
		if err != nil {
			t.Fatalf("record above the per-record bound: %v", err)
		}
		if admission.Admitted || admission.Bound != BoundRecordSize {
			t.Fatalf("admission = %+v, want a refusal under the per-record bound", admission)
		}
		ledger := sink.Ledger()
		if ledger.DroppedRecordSize != 1 || ledger.Admitted != 0 || ledger.Dropped() != 1 {
			t.Fatalf("ledger = %+v, want one loss under the per-record bound", ledger)
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		if manifest.Dropped != 1 || !manifest.Partial || manifest.Sealed != true {
			t.Fatalf("manifest = %+v, want the loss named beside the writer's seal", manifest)
		}
	})

	t.Run("the in-flight bound counts its own loss", func(t *testing.T) {
		writer := &recordingWriter{synced: true, block: true, release: make(chan struct{})}
		sink, err := NewCaptureSink("cap:flight", SinkConfig{MaxInFlight: 1}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if admission, err := sink.Record(ctx, sinkAttempt("at:flight")); err != nil || !admission.Admitted {
			t.Fatalf("first record = (%+v, %v), want it admitted", admission, err)
		}
		admission, err := sink.Record(ctx, sinkAttempt("at:flight-two"))
		if err != nil {
			t.Fatalf("second record while the first is in flight: %v", err)
		}
		if admission.Admitted || admission.Bound != BoundInFlight {
			t.Fatalf("admission = %+v, want a refusal under the in-flight bound", admission)
		}
		if ledger := sink.Ledger(); ledger.DroppedInFlight != 1 || ledger.InFlight != 1 {
			t.Fatalf("ledger = %+v, want one loss and the one record still in flight", ledger)
		}
		close(writer.release)
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		if manifest.Dropped != 1 || manifest.Records != 1 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want one written record, one counted loss and a partial account", manifest)
		}
	})

	t.Run("the byte budget counts its own loss", func(t *testing.T) {
		record := sinkAttempt("at:budget")
		line, err := MarshalLine(NewAttemptLine(record))
		if err != nil {
			t.Fatalf("marshal the record line: %v", err)
		}
		sink, err := NewCaptureSink("cap:budget", SinkConfig{MaxBytes: int64(len(line))}, &recordingWriter{synced: true})
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if admission, err := sink.Record(ctx, record); err != nil || !admission.Admitted {
			t.Fatalf("record spending the whole budget = (%+v, %v), want it admitted", admission, err)
		}
		admission, err := sink.Record(ctx, record)
		if err != nil {
			t.Fatalf("record past the byte budget: %v", err)
		}
		if admission.Admitted || admission.Bound != BoundBytes {
			t.Fatalf("admission = %+v, want a refusal under the byte budget", admission)
		}
		if ledger := sink.Ledger(); ledger.DroppedBytes != 1 || ledger.Dropped() != 1 {
			t.Fatalf("ledger = %+v, want one loss under the byte budget", ledger)
		}
	})

	t.Run("a sealed capture counts later records closed and keeps its frozen account", func(t *testing.T) {
		sink, err := NewCaptureSink("cap:closed", SinkConfig{}, &recordingWriter{synced: true})
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if admission, err := sink.Record(ctx, sinkAttempt("at:closed")); err != nil || !admission.Admitted {
			t.Fatalf("record before the seal = (%+v, %v), want it admitted", admission, err)
		}
		first, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		admission, err := sink.Record(ctx, sinkAttempt("at:closed-two"))
		if err != nil {
			t.Fatalf("record after the seal: %v", err)
		}
		if admission.Admitted || admission.Bound != BoundClosed {
			t.Fatalf("admission = %+v, want a refusal under the closed bound", admission)
		}
		if ledger := sink.Ledger(); ledger.DroppedClosed != 1 {
			t.Fatalf("ledger after a closed refusal = %+v, want the closed loss counted", ledger)
		}
		second, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("second seal: %v", err)
		}
		if second.CaptureID != first.CaptureID || second.Records != first.Records || second.Dropped != first.Dropped || second.InFlight != first.InFlight {
			t.Fatalf("second seal = %+v, want the frozen first account %+v", second, first)
		}
		if second.Dropped != 0 {
			t.Fatalf("frozen seal dropped = %d, want a late refusal kept out of the account sealed before it", second.Dropped)
		}
	})

	t.Run("a writer that reports no sync leaves the capture partial", func(t *testing.T) {
		writer := &recordingWriter{synced: false, reason: "disk refused sync"}
		sink, err := NewCaptureSink("cap:unsynced", SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if admission, err := sink.Record(ctx, sinkAttempt("at:unsynced")); err != nil || !admission.Admitted {
			t.Fatalf("record before the seal = (%+v, %v), want it admitted", admission, err)
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		if manifest.Sealed || !manifest.Partial {
			t.Fatalf("manifest = (sealed %v, partial %v), want an unsealed partial capture", manifest.Sealed, manifest.Partial)
		}
		if !mentions(manifest.Reasons, "no writer seal: the writer reported no sync: disk refused sync") {
			t.Fatalf("reasons = %v, want the writer's own sync refusal named", manifest.Reasons)
		}
	})

	t.Run("a seal the writer did not confirm is abandoned with its in-flight count", func(t *testing.T) {
		writer := &recordingWriter{synced: true, block: true, release: make(chan struct{})}
		sink, err := NewCaptureSink("cap:abandoned", SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if admission, err := sink.Record(ctx, sinkAttempt("at:abandoned")); err != nil || !admission.Admitted {
			t.Fatalf("record before the seal = (%+v, %v), want it admitted", admission, err)
		}
		deadline, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		manifest, err := sink.Seal(deadline)
		if err != nil {
			t.Fatalf("seal the sink under a deadline: %v", err)
		}
		if manifest.Sealed || !manifest.Partial || manifest.InFlight != 1 {
			t.Fatalf("manifest = %+v, want an abandoned partial account holding the in-flight record", manifest)
		}
		if !mentions(manifest.Reasons, "the drain was abandoned") {
			t.Fatalf("reasons = %v, want the abandonment named", manifest.Reasons)
		}
		close(writer.release)
	})
}

// mentions reports whether one reason line carries the given text.
func mentions(reasons []string, text string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, text) {
			return true
		}
	}
	return false
}

// exportAt is the fixed record time the capture invariants stamp their records and
// writer sync evidence with.
var exportAt = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// sinkAttempt is one attempt record the sink admits or refuses.
func sinkAttempt(id event.AttemptID) AttemptRecord {
	return AttemptRecord{
		ID:         id,
		DecisionID: "dec:1",
		EpisodeID:  "ep:1",
		Timestamp:  exportAt,
	}
}

// recordingWriter is a capture writer whose seal evidence and append behaviour the
// ledger invariants script: a blocking append holds records in flight, and the sync
// result is reported as scripted.
type recordingWriter struct {
	block   bool
	release chan struct{}
	synced  bool
	reason  string

	mu        sync.Mutex
	lines     int
	sealed    bool
	commits   int
	commitErr error
}

func (w *recordingWriter) Append(name string, line []byte) error {
	if w.block {
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sealed {
		return ErrWriterSealed
	}
	w.lines++
	return nil
}

func (w *recordingWriter) Seal() ([]CaptureFileDigest, WriterSyncResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sealed = true
	files := []CaptureFileDigest{{Name: SinkRecordsFile, SHA256: "digest", Records: int64(w.lines)}}
	return files, WriterSyncResult{Synced: w.synced, Reason: w.reason, At: exportAt}
}

func (w *recordingWriter) Commit(manifest CaptureManifest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commits++
	return w.commitErr
}
