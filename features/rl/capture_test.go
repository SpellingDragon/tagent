package rl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
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

// boundedAnchorBudgetBytes names the resident byte budget the capture must keep:
// the anchor fixes fifty snapshots of about one hundred kilobytes each and reads
// every one of them back, so a capture that keeps all of them resident grows by
// about five megabytes — five times the budget.
const (
	boundedAnchorDecisions = 50
	boundedAnchorBudget    = 1 << 20
	boundedAnchorBodyKB    = 100
)

// TestTrajectoryCaptureStaysBounded pins the resident bound of the fixed snapshots: their growth stays inside the stated byte budget while every one reads back.
//   - fifty decisions fix a snapshot of about 100KB each beside one capture
//   - the resident growth after a full collection stays within the 1MB budget
//   - every fixed snapshot still reads back through its reference, digest-checked
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestTrajectoryCaptureStaysBounded(t *testing.T) {
	ctx := context.Background()
	sink, err := NewCaptureSink("cap:bounded", SinkConfig{}, &recordingWriter{synced: true})
	if err != nil {
		t.Fatalf("open the sink: %v", err)
	}
	capture, err := NewTrajectoryCaptureWithBudget("bounded", sink, SnapshotPolicy{}, CaptureBudget{
		Bytes:   boundedAnchorBudget,
		SwapDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open the capture: %v", err)
	}

	body := strings.Repeat("s", boundedAnchorBodyKB<<10)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	refs := make([]SnapshotRef, 0, boundedAnchorDecisions)
	for at := 0; at < boundedAnchorDecisions; at++ {
		decision := event.DecisionID(fmt.Sprintf("decision-bounded-%d", at))
		scope := capture.Scope(ModelViewRef{Model: "model-a"}, decision)
		view, err := scope.Snapshot(ctx, model.Request{
			Messages: []model.Message{{Role: model.RoleUser, Content: body}},
		})
		if err != nil {
			t.Fatalf("fix snapshot %d: %v", at, err)
		}
		refs = append(refs, view.Snapshot)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > boundedAnchorBudget {
		t.Fatalf("resident growth after %d snapshots of %dKB = %d bytes, want at most the %d-byte budget: "+
			"the fixed snapshots the capture keeps are unbounded",
			boundedAnchorDecisions, boundedAnchorBodyKB, grew, boundedAnchorBudget)
	}

	for at, ref := range refs {
		if _, err := capture.SnapshotOf(ref); err != nil {
			t.Fatalf("read snapshot %d back through %s: %v", at, ref.SnapshotID, err)
		}
	}
	if got := len(capture.Snapshots()); got != boundedAnchorDecisions {
		t.Fatalf("snapshots readable = %d, want all %d fixed ones", got, boundedAnchorDecisions)
	}
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

// requestEvidenceOf builds one request evidence record beside the reference its
// snapshot content is read back under.
func requestEvidenceOf(t *testing.T, decision event.DecisionID, content []byte) RequestEvidenceRecord {
	t.Helper()
	return RequestEvidenceRecord{
		DecisionID: decision,
		EpisodeID:  "ep-evidence",
		Snapshot:   snapshotRef(content, "snapshot-"+string(decision), "capture/cap-evidence/"+string(decision)),
	}
}

// TestRecordKinds pins the closed record kind set and the bounded landing of request evidence.
//   - the request evidence line round-trips through the versioned line beside the four record kinds it joins
//   - unknown_kind_rejected: a kind this package does not declare, and a kind whose payload disagrees with it, are both refused
//   - request evidence naming no decision or carrying no readable reference is refused where it is written
//   - full_queue_counted: request evidence refused under the in-flight bound is counted in the loss ledger and reported by name
//   - the landed evidence is what the writer received, and the seal accounts it
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRecordKinds(t *testing.T) {
	ctx := context.Background()

	t.Run("the request evidence line round-trips", func(t *testing.T) {
		record := requestEvidenceOf(t, "dec-evidence", []byte("the runtime request snapshot"))
		line, err := MarshalLine(NewRequestEvidenceLine(record))
		if err != nil {
			t.Fatalf("marshal the request evidence line: %v", err)
		}
		parsed, err := ParseLine(line)
		if err != nil {
			t.Fatalf("parse the request evidence line: %v", err)
		}
		if parsed.Kind != RecordKindRequest {
			t.Fatalf("parsed kind = %q, want %q", parsed.Kind, RecordKindRequest)
		}
		evidence := parsed.RequestEvidence()
		if evidence == nil || !reflect.DeepEqual(*evidence, record) {
			t.Fatalf("parsed evidence = %+v, want %+v", evidence, record)
		}
		for _, kind := range []RecordKind{RecordKindEpisode, RecordKindDecision, RecordKindAttempt, RecordKindAnnex, RecordKindRequest} {
			if kind == "" {
				t.Fatalf("the kind set carries an unnamed kind")
			}
		}
	})

	t.Run("unknown_kind_rejected", func(t *testing.T) {
		record := requestEvidenceOf(t, "dec-refused", []byte("body"))
		line, err := MarshalLine(NewRequestEvidenceLine(record))
		if err != nil {
			t.Fatalf("marshal the request evidence line: %v", err)
		}
		var head map[string]any
		if err := json.Unmarshal(line, &head); err != nil {
			t.Fatalf("decode the marshaled line: %v", err)
		}
		head["kind"] = "request_evidence_v2"
		unknown, err := json.Marshal(head)
		if err != nil {
			t.Fatalf("encode the unknown kind line: %v", err)
		}
		if _, err := ParseLine(unknown); !errors.Is(err, ErrLineShape) {
			t.Fatalf("parse an undeclared kind = %v, want ErrLineShape", err)
		}
		if _, err := MarshalLine(Line{Schema: SchemaCurrent, Kind: RecordKindRequest, Episode: &EpisodeRecord{}}); !errors.Is(err, ErrLineShape) {
			t.Fatalf("marshal a request line carrying an episode payload = %v, want ErrLineShape", err)
		}
		if _, err := MarshalLine(Line{Schema: SchemaCurrent, Kind: "request_evidence_v2", request: &record}); !errors.Is(err, ErrLineShape) {
			t.Fatalf("marshal an undeclared kind = %v, want ErrLineShape", err)
		}
	})

	t.Run("evidence no request joins and evidence no content reads back are refused", func(t *testing.T) {
		unidentified := requestEvidenceOf(t, "", []byte("body"))
		if err := ValidateRequestEvidence(unidentified); !errors.Is(err, ErrRequestEvidenceMissing) {
			t.Fatalf("evidence without a decision = %v, want ErrRequestEvidenceMissing", err)
		}
		if _, err := MarshalLine(NewRequestEvidenceLine(unidentified)); !errors.Is(err, ErrRequestEvidenceMissing) {
			t.Fatalf("marshal evidence without a decision = %v, want ErrRequestEvidenceMissing", err)
		}
		unreadable := RequestEvidenceRecord{DecisionID: "dec-unreadable"}
		if err := ValidateRequestEvidence(unreadable); !errors.Is(err, ErrSnapshotUnreadable) {
			t.Fatalf("evidence without a reference = %v, want ErrSnapshotUnreadable", err)
		}
		if err := ValidateRequestEvidence(requestEvidenceOf(t, "dec-exact", []byte("body"))); err != nil {
			t.Fatalf("validate readable evidence: %v", err)
		}
	})

	t.Run("full_queue_counted", func(t *testing.T) {
		writer := &recordingWriter{synced: true, block: true, release: make(chan struct{})}
		sink, err := NewCaptureSink("cap:evidence-flight", SinkConfig{MaxInFlight: 1}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		if err := sink.RecordRequestEvidence(ctx, requestEvidenceOf(t, "dec-first", []byte("first body"))); err != nil {
			t.Fatalf("record the first evidence line: %v", err)
		}
		err = sink.RecordRequestEvidence(ctx, requestEvidenceOf(t, "dec-second", []byte("second body")))
		if !errors.Is(err, ErrCaptureRefused) || !strings.Contains(err.Error(), string(BoundInFlight)) {
			t.Fatalf("record past the in-flight bound = %v, want ErrCaptureRefused under BoundInFlight", err)
		}
		if ledger := sink.Ledger(); ledger.DroppedInFlight != 1 || ledger.Admitted != 1 || ledger.InFlight != 1 {
			t.Fatalf("ledger = %+v, want one admitted record, one in-flight loss and one record still in flight", ledger)
		}
		close(writer.release)
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		if manifest.Records != 1 || manifest.Dropped != 1 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want the landed evidence beside the counted loss", manifest)
		}
		if !mentions(manifest.Reasons, "1 records were refused under an admission bound") {
			t.Fatalf("reasons = %v, want the refusal named", manifest.Reasons)
		}
	})

	t.Run("the landed evidence is what the writer received", func(t *testing.T) {
		writer := &recordingWriter{synced: true}
		sink, err := NewCaptureSink("cap:evidence", SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("open the sink: %v", err)
		}
		record := requestEvidenceOf(t, "dec-landed", []byte("landed body"))
		if err := sink.RecordRequestEvidence(ctx, record); err != nil {
			t.Fatalf("record the evidence line: %v", err)
		}
		if _, err := sink.Seal(ctx); err != nil {
			t.Fatalf("seal the sink: %v", err)
		}
		writer.mu.Lock()
		landed := append([][]byte(nil), writer.payloads...)
		writer.mu.Unlock()
		if len(landed) != 1 {
			t.Fatalf("landed lines = %d, want the one evidence line", len(landed))
		}
		parsed, err := ParseLine(landed[0])
		if err != nil {
			t.Fatalf("parse the landed line: %v", err)
		}
		evidence := parsed.RequestEvidence()
		if evidence == nil || !reflect.DeepEqual(*evidence, record) {
			t.Fatalf("landed evidence = %+v, want %+v", evidence, record)
		}
		if err := VerifySnapshot(evidence.Snapshot, []byte("landed body")); err != nil {
			t.Fatalf("read the landed evidence back through its reference: %v", err)
		}
		if err := VerifySnapshot(evidence.Snapshot, []byte("another body")); !errors.Is(err, ErrSnapshotMismatch) {
			t.Fatalf("read another body through the reference = %v, want ErrSnapshotMismatch", err)
		}
	})
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
	payloads  [][]byte
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
	w.payloads = append(w.payloads, append([]byte(nil), line...))
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
