package learning

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/rl"
)

// TestExportTracesFormalRecords drives the authorized training export over the sealed capture of the formal chain and checks the trace contract of 06.4.
//   - a request that names no grant is refused by name and one that names the capture exports exactly what the writer sealed
//   - every exported sample traces to the stored formal record it was reduced from, byte for byte and identity by identity, under the reward instance it cites
//   - a record that never reached storage is loss in the capture account and never an exported sample, and bytes beside the seal are refused rather than read
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestExportTracesFormalRecords(t *testing.T) {
	t.Run("an unauthorized request names its refusal and an authorized one exports", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-export-auth", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, exportAnswer)
		turn := formalInvoke(t, ctx, run, "inv-export-auth", "session-export-auth", "ep-export-auth")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn of two decisions driving the record writes", turn.Terminal, len(turn.Decisions))
		}
		if _, err := sink.Seal(ctx); err != nil {
			t.Fatalf("seal the formal capture: %v", err)
		}
		src, err := rl.NewCaptureExportSource(dir)
		if err != nil {
			t.Fatalf("build the export source over the sealed capture: %v", err)
		}
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())

		if _, err := exporter.Export(ctx, rl.ExportRequest{}); !errors.Is(err, rl.ErrExportUnauthorized) {
			t.Fatalf("empty grant = %v, want the named refusal", err)
		}
		foreign, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{"cap-export-foreign"}})
		if err != nil || foreign.Written != 0 || foreign.Partial || len(foreign.Manifest.Samples) != 0 {
			t.Fatalf("foreign grant = (%+v, %v), want no samples written beside a partition the capture does not serve", foreign, err)
		}
		granted, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{src.CaptureID()}})
		if err != nil || granted.Partial || !granted.Manifest.Complete {
			t.Fatalf("granted export = (%+v, %v), want one complete run over the named capture", granted, err)
		}
		stored := storedRecordsOf(t, dir)
		if granted.Written != 4 || len(stored) != 5 {
			t.Fatalf("granted export wrote %d samples over %d stored records, want the 4 facts of the 5 records the formal chain sealed", granted.Written, len(stored))
		}
		if len(granted.Manifest.Samples) != int(granted.Written) {
			t.Fatalf("manifest lists %d samples beside %d written, want one provenance entry per sample", len(granted.Manifest.Samples), granted.Written)
		}
	})

	t.Run("every exported sample traces to the formal record it was reduced from", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-export-trace", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, exportAnswer)
		turn := formalInvoke(t, ctx, run, "inv-export-trace", "session-export-trace", "ep-export-trace")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn of two decisions driving the record writes", turn.Terminal, len(turn.Decisions))
		}
		if _, err := sink.Seal(ctx); err != nil {
			t.Fatalf("seal the formal capture: %v", err)
		}
		stored := storedRecordsOf(t, dir)
		feedback := rl.NewFeedbackLedger()
		fbDecision := ""
		for _, rec := range stored {
			if rec.line.Kind != rl.RecordKindDecision {
				continue
			}
			fbDecision = string(rec.line.Decision.ID)
			if err := feedback.Add(rl.FeedbackRecord{
				FeedbackID: "fb-export-1", EventKey: fbDecision, DecisionID: rec.line.Decision.ID,
				Conclusion: "good", Source: "human", Version: "v1", RecordedAt: time.UnixMilli(1),
			}); err != nil {
				t.Fatalf("bind the feedback to the stored decision: %v", err)
			}
			break
		}
		if fbDecision == "" {
			t.Fatal("the sealed capture holds no decision to bind feedback to")
		}
		src, err := rl.NewCaptureExportSource(dir)
		if err != nil {
			t.Fatalf("build the export source over the sealed capture: %v", err)
		}
		exporter := rl.NewTrainingExporter(src, feedback, rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{src.CaptureID()}})
		if err != nil || result.Partial || result.Written != 4 {
			t.Fatalf("export = (%+v, %v), want 4 samples in one complete run", result, err)
		}
		lines, held := exporter.Snapshot(result.Manifest.ExportID)
		if !held || len(lines) != 4 {
			t.Fatalf("snapshot holds %d lines (held=%v), want the 4 exported samples", len(lines), held)
		}
		assertSamplesTraceStored(t, lines, stored, src.CaptureID())
		for i, line := range lines {
			if result.Manifest.Samples[i] != line.Provenance {
				t.Fatalf("manifest sample %d does not mirror the line: %+v vs %+v", i, result.Manifest.Samples[i], line.Provenance)
			}
			prov := line.Provenance
			if prov.RewardInstanceID != line.Reward.InstanceID || prov.RewardRevision != line.Reward.Revision || prov.RewardVersion != line.Reward.RuleVersion {
				t.Fatalf("sample %d provenance cites (%q, r%d, %q) beside the reward (%q, r%d, %q), want the instance the sample carries",
					i, prov.RewardInstanceID, prov.RewardRevision, prov.RewardVersion, line.Reward.InstanceID, line.Reward.Revision, line.Reward.RuleVersion)
			}
			if string(prov.DecisionID) == fbDecision {
				if line.Reward.State != rl.RewardRated || line.Reward.Value != 1 {
					t.Fatalf("sample %d reward = %+v, want the rated value of the bound feedback", i, line.Reward)
				}
				if len(line.Reward.DerivedFrom) != 1 || line.Reward.DerivedFrom[0].FeedbackID != "fb-export-1" {
					t.Fatalf("sample %d reward does not cite its evidence: %+v", i, line.Reward.DerivedFrom)
				}
			} else if line.Reward.State != rl.RewardUnrated || line.Reward.Value != 0 || len(line.Reward.DerivedFrom) != 0 {
				t.Fatalf("sample %d carries an invented reward beside no feedback: %+v", i, line.Reward)
			}
		}
		if got, want := result.Manifest.SourceSHA256, linesDigestOf(t, lines); got != want {
			t.Fatalf("source digest %q, want %q (the digest must cover the bytes written)", got, want)
		}
		if result.Manifest.ParentKeyMissing != 4 {
			t.Fatalf("parent-missing count %d, want 4 (records carry no parent key and none is invented)", result.Manifest.ParentKeyMissing)
		}
		if len(result.Manifest.Groups) != 1 || result.Manifest.Groups[0].GroupID != "group:ep-export-trace" {
			t.Fatalf("groups = %+v, want the one split group of ep-export-trace", result.Manifest.Groups)
		}
		for _, line := range lines {
			if line.Provenance.GroupID != "group:ep-export-trace" {
				t.Fatalf("sample %s group = %q, want the split group of its recorded episode", line.Provenance.SampleID, line.Provenance.GroupID)
			}
		}
		seen := map[string]bool{}
		for _, line := range lines {
			seen[string(line.Fact.EventKey)] = true
		}
		for _, decision := range turn.Decisions {
			if !seen[string(decision.DecisionID)] {
				t.Fatalf("decision %q of the formal chain carries no exported fact", decision.DecisionID)
			}
		}
	})

	t.Run("records that never reached storage are loss and never exported samples", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		inner, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		writer := &failingAfterWriter{inner: inner, take: 2}
		sink, err := rl.NewCaptureSink("cap-export-loss", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, exportAnswer)
		turn := formalInvoke(t, ctx, run, "inv-export-loss", "session-export-loss", "ep-export-loss")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn unaffected by the refused writes", turn.Terminal, len(turn.Decisions))
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the failed capture: %v", err)
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 5 || ledger.Written != 2 || ledger.WriteErrors != 3 || ledger.InFlight != 0 {
			t.Fatalf("ledger = %+v, want the 5 admitted records accounted as 2 written and 3 write failures", ledger)
		}
		if ledger.Admitted != ledger.Written+ledger.WriteErrors {
			t.Fatalf("ledger = %+v, want every admitted record accounted as written or failed", ledger)
		}
		if manifest.Records != 2 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want the 2 stored records beside the accounted losses", manifest)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "3 admitted records failed to reach storage") {
			t.Fatalf("manifest reasons = %v, want the write failures named", manifest.Reasons)
		}
		if losses := run.RecordingLosses(); len(losses) != 0 {
			t.Fatalf("recording losses = %+v, want the storage failures held in the capture ledger alone", losses)
		}
		src, err := rl.NewCaptureExportSource(dir)
		if err != nil {
			t.Fatalf("build the export source over the failed capture: %v", err)
		}
		if !src.Manifest().Partial || !strings.Contains(strings.Join(src.Manifest().Reasons, "; "), "3 admitted records failed to reach storage") {
			t.Fatalf("source manifest = %+v, want the loss account read beside the export", src.Manifest())
		}
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{src.CaptureID()}})
		if err != nil || result.Partial {
			t.Fatalf("export over the sealed loss = (%+v, %v), want a complete run over what reached storage", result, err)
		}
		stored := storedRecordsOf(t, dir)
		facts := 0
		for _, rec := range stored {
			if rec.line.Kind != rl.RecordKindEpisode {
				facts++
			}
		}
		if result.Written != int64(facts) {
			t.Fatalf("export wrote %d samples beside %d stored facts, want one sample per stored record", result.Written, facts)
		}
		if result.Written >= ledger.Admitted {
			t.Fatalf("export wrote %d samples beside %d admitted records, want the write failures kept out of the sample set", result.Written, ledger.Admitted)
		}
		lines, _ := exporter.Snapshot(result.Manifest.ExportID)
		assertSamplesTraceStored(t, lines, stored, src.CaptureID())
	})

	t.Run("bytes the seal does not confirm are refused and never exported", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		unsealed := t.TempDir()
		if err := os.WriteFile(filepath.Join(unsealed, rl.SinkRecordsFile), []byte("{\"schema\":\"tagent.record.v1\"}\n"), 0o600); err != nil {
			t.Fatalf("write the loose record file: %v", err)
		}
		if _, err := rl.NewCaptureExportSource(unsealed); !errors.Is(err, rl.ErrExportCaptureUnsealed) {
			t.Fatalf("unsealed capture = %v, want the named refusal beside the loose bytes", err)
		}

		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-export-tamper", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, exportAnswer)
		turn := formalInvoke(t, ctx, run, "inv-export-tamper", "session-export-tamper", "ep-export-tamper")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn of two decisions driving the record writes", turn.Terminal, len(turn.Decisions))
		}
		if _, err := sink.Seal(ctx); err != nil {
			t.Fatalf("seal the formal capture: %v", err)
		}
		file, err := os.OpenFile(filepath.Join(dir, rl.SinkRecordsFile), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatalf("open the sealed record file: %v", err)
		}
		if _, err := file.WriteString("tampered\n"); err != nil {
			file.Close()
			t.Fatalf("append the tampered line: %v", err)
		}
		file.Close()

		src, err := rl.NewCaptureExportSource(dir)
		if err != nil {
			t.Fatalf("build the export source over the tampered capture: %v", err)
		}
		exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
		if _, err := exporter.Export(ctx, rl.ExportRequest{}); !errors.Is(err, rl.ErrExportUnauthorized) {
			t.Fatalf("empty grant over failing reads = %v, want the named refusal before any read", err)
		}
		result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{src.CaptureID()}})
		if !errors.Is(err, rl.ErrExportReadFailed) || !errors.Is(err, rl.ErrExportCaptureMismatch) {
			t.Fatalf("export over tampered bytes = %v, want the read failure routed over the named seal mismatch", err)
		}
		if !result.Partial || result.Written != 0 || result.Manifest.Complete {
			t.Fatalf("export over tampered bytes = %+v, want a partial run that wrote nothing", result)
		}
		fresh, err := rl.NewCaptureExportSource(dir)
		if err != nil {
			t.Fatalf("build the export source again: %v", err)
		}
		if _, err := fresh.Page(ctx, fresh.CaptureID(), 0, 2); !errors.Is(err, rl.ErrExportCaptureMismatch) {
			t.Fatalf("page over tampered bytes = %v, want the named seal mismatch at the seam", err)
		}
	})
}

// exportAnswer is the scripted model behind the export trace checks: one tool
// turn on the request the user input opens and one settled final turn on the
// request the tool result answers, so the capture seals two decisions and two
// attempts beside their episode.
func exportAnswer(req *model.Request) *model.Response {
	if last := req.Messages[len(req.Messages)-1]; last.Content == "learn me" {
		return learnToolTurn(learnCall("call-export-1", "echo-a", `{"x":1}`))
	}
	return learnFinalTurn("settled")
}

// failingAfterWriter is the capture writer whose storage takes a fixed number
// of writes and refuses every write after them: it stands in for a storage that
// fails mid-capture, so the account of what it lost can be read beside what it
// sealed.
type failingAfterWriter struct {
	inner *rl.FileCaptureWriter
	mu    sync.Mutex
	take  int
}

// Append hands the line to storage while the writer still takes writes and
// refuses it by name afterwards.
func (w *failingAfterWriter) Append(name string, line []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.take <= 0 {
		return errors.New("the storage refused the write")
	}
	w.take--
	return w.inner.Append(name, line)
}

// Seal reports the writer's own state over the writes that reached storage.
func (w *failingAfterWriter) Seal() ([]rl.CaptureFileDigest, rl.WriterSyncResult) {
	return w.inner.Seal()
}

// Commit persists the manifest under the same account as the writes that
// reached storage.
func (w *failingAfterWriter) Commit(manifest rl.CaptureManifest) error {
	return w.inner.Commit(manifest)
}

// storedRecord is one line of the sealed record file beside the raw bytes it
// was written as, so an exported sample is reconciled against the very bytes
// the writer sealed.
type storedRecord struct {
	raw  []byte
	line rl.Line
}

// storedRecordsOf reads the sealed record file of one capture directory back,
// line by line.
func storedRecordsOf(t *testing.T, dir string) []storedRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the stored records: %v", err)
	}
	var out []storedRecord
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse stored record %q: %v", raw, err)
		}
		out = append(out, storedRecord{raw: append([]byte(nil), raw...), line: line})
	}
	return out
}

// assertSamplesTraceStored reconciles every exported sample against the sealed
// records: the fact body is the stored line byte for byte, the fact identity is
// the record identity that line carries, and every stored record that can
// become a sample is covered exactly once. A sample matching no stored record
// and a stored record no sample carries are both failures here.
func assertSamplesTraceStored(t *testing.T, lines []rl.SampleLine, stored []storedRecord, capture string) {
	t.Helper()
	covered := map[string]bool{}
	for i, line := range lines {
		fact := line.Fact
		matched := -1
		for j, rec := range stored {
			if rec.line.Kind == rl.RecordKindEpisode {
				continue
			}
			if bytes.Equal(rec.raw, fact.Body) {
				matched = j
				break
			}
		}
		if matched < 0 {
			t.Fatalf("sample %d body matches no stored record byte for byte", i)
		}
		rec := stored[matched]
		if covered[string(rec.raw)] {
			t.Fatalf("sample %d carries a stored record another sample already covered", i)
		}
		covered[string(rec.raw)] = true
		if fact.Partition != capture || fact.CaptureID != capture || fact.Schema != rl.SchemaCurrent {
			t.Fatalf("sample %d fact scope = (partition %q, capture %q, schema %q), want the sealed capture under the current schema", i, fact.Partition, fact.CaptureID, fact.Schema)
		}
		switch rec.line.Kind {
		case rl.RecordKindDecision:
			record := rec.line.Decision
			if string(fact.EventKey) != string(record.ID) || fact.DecisionID != record.ID ||
				fact.AttemptID != "" || fact.EpisodeID != record.EpisodeID ||
				fact.Timestamp != record.Timestamp.UnixMilli() {
				t.Fatalf("sample %d fact = %+v, want the identity of stored decision %q", i, fact, record.ID)
			}
		case rl.RecordKindAttempt:
			record := rec.line.Attempt
			if string(fact.EventKey) != string(record.ID) || fact.DecisionID != record.DecisionID ||
				fact.AttemptID != record.ID || fact.EpisodeID != record.EpisodeID ||
				fact.Timestamp != record.Timestamp.UnixMilli() {
				t.Fatalf("sample %d fact = %+v, want the identity of stored attempt %q", i, fact, record.ID)
			}
		}
	}
	for _, rec := range stored {
		if rec.line.Kind == rl.RecordKindEpisode {
			continue
		}
		if !covered[string(rec.raw)] {
			t.Fatalf("stored record %q carries no exported sample", rec.raw)
		}
	}
}
