package rl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// exportAt is the event time every export invariant hands the source and the
// feedback account.
var exportAt = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// TestExportGrantIsNeverWidened pins the export grant: authorization precedes every read and no page, row or filter widens it.
// - a grant naming no partition is refused by name and the source is never read
// - a page limit outside the supported range is refused by name before any read
// - a duplicate grant is paged once, not once per name
// - a row filed under a partition the grant does not name is counted forbidden and never written
// - a page repeating already-seen events is refused by name instead of read as the end of the partition
// - a failed read is returned as a partial snapshot that keeps what it read
// - a failed episode read is returned as a partial snapshot and pages nothing
// - a sealed snapshot is never overwritten by a second seal of the same identity
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestExportGrantIsNeverWidened(t *testing.T) {
	ctx := context.Background()

	t.Run("a grant naming no partition is refused before any read", func(t *testing.T) {
		source := &scriptedExportSource{}
		exporter := newExporterUnderTest(t, source)
		for name, req := range map[string]ExportRequest{
			"no grant":         {},
			"only blank names": {PartitionIDs: []string{"", ""}},
		} {
			if _, err := exporter.Export(ctx, req); !errors.Is(err, ErrExportUnauthorized) {
				t.Fatalf("%s = %v, want ErrExportUnauthorized", name, err)
			}
		}
		if pages, episodes := source.counts(); pages != 0 || episodes != 0 {
			t.Fatalf("source reads = (pages %d, episodes %d), want none before authorization", pages, episodes)
		}
	})

	t.Run("a page limit outside the supported range is refused before any read", func(t *testing.T) {
		source := &scriptedExportSource{}
		exporter := newExporterUnderTest(t, source)
		for _, limit := range []int{-1, MaxExportPageLimit + 1} {
			req := ExportRequest{PartitionIDs: []string{"granted"}, PageLimit: limit}
			if _, err := exporter.Export(ctx, req); !errors.Is(err, ErrExportPageLimit) {
				t.Fatalf("page limit %d = %v, want ErrExportPageLimit", limit, err)
			}
		}
		if pages, episodes := source.counts(); pages != 0 || episodes != 0 {
			t.Fatalf("source reads = (pages %d, episodes %d), want none before the page limit is settled", pages, episodes)
		}
	})

	t.Run("a duplicate grant is paged once", func(t *testing.T) {
		source := &scriptedExportSource{page: func(string, int, int) ([]ExportedFact, error) {
			return nil, nil
		}}
		source.page = twoPageSource([]ExportedFact{exportFact("ga01", "granted"), exportFact("ga02", "granted")})
		exporter := newExporterUnderTest(t, source)
		result, err := exporter.Export(ctx, ExportRequest{PartitionIDs: []string{"granted", "granted"}, PageLimit: 2})
		if err != nil {
			t.Fatalf("export under a duplicated grant: %v", err)
		}
		if pages, _ := source.counts(); pages != 2 {
			t.Fatalf("page reads = %d, want one paging walk over the one granted partition", pages)
		}
		if result.Written != 2 || len(result.Manifest.Samples) != 2 {
			t.Fatalf("written = %d (samples %d), want each granted fact written once", result.Written, len(result.Manifest.Samples))
		}
	})

	t.Run("a row outside the grant is counted forbidden and never written", func(t *testing.T) {
		source := &scriptedExportSource{page: func(_ string, offset, _ int) ([]ExportedFact, error) {
			if offset > 0 {
				return nil, nil
			}
			return []ExportedFact{exportFact("gb01", "granted"), exportFact("gb02", "smuggled")}, nil
		}}
		exporter := newExporterUnderTest(t, source)
		result, err := exporter.Export(ctx, ExportRequest{PartitionIDs: []string{"granted"}, PageLimit: 2})
		if err != nil {
			t.Fatalf("export a page carrying a row outside the grant: %v", err)
		}
		if result.Forbidden != 1 || result.Manifest.Forbidden != 1 {
			t.Fatalf("forbidden rows = (result %d, manifest %d), want the row outside the grant counted", result.Forbidden, result.Manifest.Forbidden)
		}
		if result.Written != 1 || len(result.Manifest.Samples) != 1 {
			t.Fatalf("written = %d (samples %d), want only the granted row", result.Written, len(result.Manifest.Samples))
		}
		if got := result.Manifest.Samples[0].SampleID; got != "sample:gb01" {
			t.Fatalf("written sample = %q, want the granted row and nothing beside it", got)
		}
	})

	t.Run("a repeated page is refused by name", func(t *testing.T) {
		repeated := []ExportedFact{exportFact("gc01", "granted"), exportFact("gc02", "granted")}
		source := &scriptedExportSource{page: func(string, int, int) ([]ExportedFact, error) {
			return repeated, nil
		}}
		exporter := newExporterUnderTest(t, source)
		result, err := exporter.Export(ctx, ExportRequest{PartitionIDs: []string{"granted"}, PageLimit: 2})
		if !errors.Is(err, ErrExportRepeatedPage) {
			t.Fatalf("export over a repeated page = %v, want ErrExportRepeatedPage", err)
		}
		if !result.Partial || result.Manifest.Complete {
			t.Fatalf("result = (partial %v, complete %v), want a partial run that never claims completeness", result.Partial, result.Manifest.Complete)
		}
		if result.Written != 2 {
			t.Fatalf("written = %d, want the page read before the repeat", result.Written)
		}
	})

	t.Run("a failed read is returned as a partial snapshot", func(t *testing.T) {
		failure := errors.New("segment read failed")
		source := &scriptedExportSource{page: func(_ string, offset, _ int) ([]ExportedFact, error) {
			if offset == 0 {
				return []ExportedFact{exportFact("gd01", "granted"), exportFact("gd02", "granted")}, nil
			}
			return nil, failure
		}}
		exporter := newExporterUnderTest(t, source)
		result, err := exporter.Export(ctx, ExportRequest{PartitionIDs: []string{"granted"}, PageLimit: 2})
		if !errors.Is(err, ErrExportReadFailed) || !errors.Is(err, failure) {
			t.Fatalf("export over a failing read = %v, want ErrExportReadFailed naming the cause", err)
		}
		if !result.Partial || result.Written != 2 || result.Manifest.Complete {
			t.Fatalf("result = (partial %v, written %d, complete %v), want the two read facts beside a partial account",
				result.Partial, result.Written, result.Manifest.Complete)
		}
	})

	t.Run("a failed episode read is returned as a partial snapshot", func(t *testing.T) {
		source := &scriptedExportSource{
			eps: func() ([]EpisodeRecord, error) { return nil, errors.New("episode read failed") },
			page: func(string, int, int) ([]ExportedFact, error) {
				t.Fatal("the source was paged although no episode could be read")
				return nil, nil
			},
		}
		exporter := newExporterUnderTest(t, source)
		result, err := exporter.Export(ctx, ExportRequest{PartitionIDs: []string{"granted"}})
		if !errors.Is(err, ErrExportReadFailed) {
			t.Fatalf("export over a failing episode read = %v, want ErrExportReadFailed", err)
		}
		if !result.Partial || result.Written != 0 || result.Manifest.Complete {
			t.Fatalf("result = (partial %v, written %d, complete %v), want a partial run that wrote nothing",
				result.Partial, result.Written, result.Manifest.Complete)
		}
		if pages, _ := source.counts(); pages != 0 {
			t.Fatalf("page reads = %d, want none before the episode account is read", pages)
		}
	})

	t.Run("a sealed snapshot is never overwritten", func(t *testing.T) {
		exporter := newExporterUnderTest(t, &scriptedExportSource{})
		if err := exporter.SealSnapshot("export:fixed", []SampleLine{{Schema: SchemaCurrent}}); err != nil {
			t.Fatalf("seal the first snapshot: %v", err)
		}
		if err := exporter.SealSnapshot("export:fixed", nil); !errors.Is(err, ErrExportConflict) {
			t.Fatalf("seal the same identity again = %v, want ErrExportConflict", err)
		}
		lines, held := exporter.Snapshot("export:fixed")
		if !held || len(lines) != 1 {
			t.Fatalf("snapshot after a refused reseal = (held %v, lines %d), want the first snapshot kept", held, len(lines))
		}
		if _, held := exporter.Snapshot("export:absent"); held {
			t.Fatal("an unsealed identity reported a snapshot")
		}
	})
}

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

// scriptedExportSource is an export source whose pages and episode account the
// invariant scripts, counting every read so a refusal before reading is observable.
type scriptedExportSource struct {
	mu      sync.Mutex
	pages   int
	episode int

	page func(partition string, offset, limit int) ([]ExportedFact, error)
	eps  func() ([]EpisodeRecord, error)
}

func (s *scriptedExportSource) Page(ctx context.Context, partition string, offset, limit int) ([]ExportedFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.pages++
	s.mu.Unlock()
	if s.page == nil {
		return nil, nil
	}
	return s.page(partition, offset, limit)
}

func (s *scriptedExportSource) Episodes(ctx context.Context) ([]EpisodeRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.episode++
	s.mu.Unlock()
	if s.eps == nil {
		return nil, nil
	}
	return s.eps()
}

// counts reports how many page and episode reads the source served.
func (s *scriptedExportSource) counts() (pages, episodes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pages, s.episode
}

// twoPageSource serves the given facts as one full page and then an empty page, the
// shape a paging walk over one partition ends on.
func twoPageSource(facts []ExportedFact) func(string, int, int) ([]ExportedFact, error) {
	return func(_ string, offset, _ int) ([]ExportedFact, error) {
		if offset > 0 {
			return nil, nil
		}
		return facts, nil
	}
}

// exportFact is one grantable fact: current schema, parsing body and the episode,
// decision and attempt identities an exported sample is traced by.
func exportFact(key, partition string) ExportedFact {
	return ExportedFact{
		EventKey:   event.EventID(key),
		Partition:  partition,
		CallID:     "call:1",
		CaptureID:  "cap:1",
		EpisodeID:  "ep:1",
		DecisionID: "dec:1",
		AttemptID:  "att:1",
		Schema:     SchemaCurrent,
		Timestamp:  exportAt.UnixMilli(),
		Body:       json.RawMessage(`{"probe":true}`),
	}
}

// newExporterUnderTest returns an exporter over the given source with one feedback
// record bound to the decision every exported fact descends from, so an export
// invariant judges the grant matrix rather than a missing evidence account.
func newExporterUnderTest(t *testing.T, source ExportSource) *TrainingExporter {
	t.Helper()
	feedback := NewFeedbackLedger()
	if err := feedback.Add(FeedbackRecord{
		FeedbackID:   "fb:1",
		EventKey:     "ga01",
		DecisionID:   "dec:1",
		AttemptID:    "att:1",
		ModelVersion: "model:1",
		Conclusion:   "accepted",
		Source:       "operator",
		Version:      "v1",
		RecordedAt:   exportAt,
	}); err != nil {
		t.Fatalf("record the feedback evidence: %v", err)
	}
	rule := RewardRule{Version: "rule:1", Value: func(evidence []FeedbackRecord) (float64, error) {
		if len(evidence) == 0 {
			return 0, errors.New("no evidence")
		}
		return 1, nil
	}}
	return NewTrainingExporter(source, feedback, NewRewardLedger(), rule)
}

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
