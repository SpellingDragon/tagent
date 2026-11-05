package rl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestSampleIdentityAnnex pins that one annex line carries the attempt id and bench task id together, so one sample aggregates under that pair.
// - the annex line is the versioned RecordKindAnnex line, read back through Annex
// - both identities are written on every annex line, not just the first
// - two annex lines of one sample stay two lines after the seal (append-only)
// - the sealed manifest and loss ledger account the annex lines like every other record kind
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func TestSampleIdentityAnnex(t *testing.T) {
	sink, dir := annexTestSink(t, SinkConfig{})
	ctx := context.Background()
	acceptance := AnnexRecord{
		AttemptID:   event.AttemptID("attempt:sample-1"),
		TaskID:      "bench:qa-1",
		Verdict:     "pass",
		RuleVersion: "acceptance/v1",
		Artifacts:   []string{"artifact:bench:qa-1/answer.txt"},
	}
	incomplete := AnnexRecord{
		AttemptID:   acceptance.AttemptID,
		TaskID:      acceptance.TaskID,
		Verdict:     "incomplete",
		RuleVersion: "gate/v1",
	}
	if err := sink.RecordAnnex(ctx, acceptance); err != nil {
		t.Fatalf("record the acceptance annex: %v", err)
	}
	if err := sink.RecordAnnex(ctx, incomplete); err != nil {
		t.Fatalf("append the incomplete verdict: %v", err)
	}
	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	if !manifest.Sealed || manifest.Partial || manifest.Records != 2 || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want two accounted annex records and no loss", manifest)
	}
	if ledger := sink.Ledger(); ledger.Admitted != 2 || ledger.Written != 2 || ledger.Dropped() != 0 {
		t.Fatalf("ledger = %+v, want two admitted and written annex records", ledger)
	}
	records := readAnnexRecords(t, dir)
	if len(records) != 2 {
		t.Fatalf("annex records = %d, want the two appended lines kept", len(records))
	}
	bySample := map[string][]AnnexRecord{}
	for i, record := range records {
		if record.AttemptID != acceptance.AttemptID {
			t.Fatalf("annex[%d].AttemptID = %q, want %q", i, record.AttemptID, acceptance.AttemptID)
		}
		if record.TaskID != acceptance.TaskID {
			t.Fatalf("annex[%d].TaskID = %q, want the bench task id %q", i, record.TaskID, acceptance.TaskID)
		}
		key := string(record.AttemptID) + "|" + record.TaskID
		bySample[key] = append(bySample[key], record)
	}
	grouped := bySample[string(acceptance.AttemptID)+"|"+acceptance.TaskID]
	if len(grouped) != 2 {
		t.Fatalf("one sample identity aggregates %d annex records, want 2", len(grouped))
	}
	if grouped[0].Verdict != "pass" || grouped[1].Verdict != "incomplete" {
		t.Fatalf("verdicts = %q, %q, want the appended verdict beside the preserved acceptance verdict", grouped[0].Verdict, grouped[1].Verdict)
	}
}

// TestAcceptanceVerdictInAnnex pins the annex write path for a verdict and the loss account of a refused annex.
// - the verdict, rule version and artifact references survive into the stored line
// - both sample identities are written on the line
// - a refused annex is counted in the ledger and the sealed manifest
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func TestAcceptanceVerdictInAnnex(t *testing.T) {
	ctx := context.Background()
	sink, dir := annexTestSink(t, SinkConfig{})
	record := AnnexRecord{
		AttemptID:   event.AttemptID("attempt:sample-verdict"),
		TaskID:      "bench:file-2",
		Verdict:     "fail",
		RuleVersion: "acceptance/v1",
		Artifacts:   []string{"artifact:bench:file-2/report.md", "artifact:bench:file-2/stdout.txt"},
	}
	if err := sink.RecordAnnex(ctx, record); err != nil {
		t.Fatalf("record the acceptance verdict: %v", err)
	}
	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	if manifest.Records != 1 || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want the annex counted as one written record", manifest)
	}
	records := readAnnexRecords(t, dir)
	if len(records) != 1 {
		t.Fatalf("annex records = %d, want 1", len(records))
	}
	got := records[0]
	if got.Verdict != record.Verdict || got.RuleVersion != record.RuleVersion {
		t.Fatalf("annex verdict = (%q, %q), want (%q, %q)", got.Verdict, got.RuleVersion, record.Verdict, record.RuleVersion)
	}
	if !reflect.DeepEqual(got.Artifacts, record.Artifacts) {
		t.Fatalf("annex artifacts = %v, want %v", got.Artifacts, record.Artifacts)
	}
	if got.AttemptID != record.AttemptID || got.TaskID != record.TaskID {
		t.Fatalf("annex identity = (%q, %q), want (%q, %q)", got.AttemptID, got.TaskID, record.AttemptID, record.TaskID)
	}

	refused, _ := annexTestSink(t, SinkConfig{MaxRecordBytes: 8})
	if err := refused.RecordAnnex(ctx, record); !errors.Is(err, ErrCaptureRefused) {
		t.Fatalf("annex above the per-record bound = %v, want ErrCaptureRefused", err)
	}
	if ledger := refused.Ledger(); ledger.DroppedRecordSize != 1 || ledger.Dropped() != 1 {
		t.Fatalf("refused annex ledger = %+v, want one loss under the per-record bound", ledger)
	}
	refusedManifest, err := refused.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the refusing capture: %v", err)
	}
	if refusedManifest.Dropped != 1 || !refusedManifest.Partial {
		t.Fatalf("refused annex manifest = %+v, want the refused annex counted in the seal", refusedManifest)
	}
}

// TestAnnexRoundtrip pins the annex payload through MarshalLine and ParseLine, including the unexported field on the line.
// - the stored line keeps the payload in the unexported annex field, read through Annex()
// - the parsed annex matches the record's verdict, rule version and artifact references
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func TestAnnexRoundtrip(t *testing.T) {
	record := AnnexRecord{
		AttemptID:   event.AttemptID("attempt:roundtrip"),
		TaskID:      "bench:exec-3",
		Verdict:     "pass",
		RuleVersion: "acceptance/v1",
		Artifacts:   []string{"artifact:bench:exec-3/exit-code", "artifact:bench:exec-3/stderr.txt"},
	}
	line := NewAnnexLine(record)
	if line.Kind != RecordKindAnnex || line.Annex() == nil {
		t.Fatalf("built annex line = (%q, %v), want kind %q with an annex payload", line.Kind, line.Annex(), RecordKindAnnex)
	}
	data, err := MarshalLine(line)
	if err != nil {
		t.Fatalf("marshal the annex line: %v", err)
	}
	if !strings.Contains(string(data), `"annex"`) {
		t.Fatalf("annex line = %s, want the payload under the annex key", data)
	}
	parsed, err := ParseLine(data)
	if err != nil {
		t.Fatalf("parse the annex line: %v", err)
	}
	if parsed.Kind != RecordKindAnnex || parsed.Annex() == nil {
		t.Fatalf("parsed line = (%q, %v), want kind %q with an annex payload", parsed.Kind, parsed.Annex(), RecordKindAnnex)
	}
	if got := *parsed.Annex(); !reflect.DeepEqual(got, record) {
		t.Fatalf("parsed annex = %+v, want %+v", got, record)
	}
	plain, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal the line through encoding/json: %v", err)
	}
	var decoded Line
	if err := json.Unmarshal(plain, &decoded); err != nil {
		t.Fatalf("unmarshal the line through encoding/json: %v", err)
	}
	if decoded.Annex() == nil || !reflect.DeepEqual(*decoded.Annex(), record) {
		t.Fatalf("encoding/json roundtrip annex = %+v, want %+v", decoded.Annex(), record)
	}
	identityFree := `{"schema":"tagent.record.v1","kind":"annex","annex":{"attempt_id":"","task_id":"bench:exec-3","verdict":"pass"}}`
	if _, err := ParseLine([]byte(identityFree)); !errors.Is(err, ErrAnnexIdentityMissing) {
		t.Fatalf("identity-free annex line = %v, want ErrAnnexIdentityMissing", err)
	}
}

// annexTestSink opens one real capture writer and sink under a temporary
// directory, so the tests read the annex lines back from storage rather than
// from an in-memory answer.
func annexTestSink(t *testing.T, cfg SinkConfig) (*CaptureSink, string) {
	t.Helper()
	dir := t.TempDir()
	writer, err := NewFileCaptureWriter(dir, 4)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := NewCaptureSink("cap:annex", cfg, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	return sink, dir
}

// readAnnexRecords reads the sealed record file back and returns the annex
// payloads in write order, refusing any line that is not an annex line.
func readAnnexRecords(t *testing.T, dir string) []AnnexRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the sealed record file: %v", err)
	}
	var records []AnnexRecord
	for _, raw := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if raw == "" {
			continue
		}
		line, err := ParseLine([]byte(raw))
		if err != nil {
			t.Fatalf("parse the sealed line %q: %v", raw, err)
		}
		if line.Kind != RecordKindAnnex || line.Annex() == nil {
			t.Fatalf("sealed line kind = %q, want an annex line", line.Kind)
		}
		records = append(records, *line.Annex())
	}
	return records
}
