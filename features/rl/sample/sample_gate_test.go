package sample_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/features/rl/sample"
)

// TestUnratedWithoutAcceptance pins that a sample without an independent verdict is recorded as unrated and marked incomplete.
// - the unrated annex is followed by an append-only incomplete marker before the seal
// - the sample never counts as complete
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestUnratedWithoutAcceptance(t *testing.T) {
	sink, dir := gateCapture(t)
	ctx := context.Background()
	record := rl.AnnexRecord{
		AttemptID: event.AttemptID("attempt:unrated"),
		TaskID:    "bench:qa-unrated",
	}
	total, complete, err := sample.Evaluate(ctx, sink, []rl.AnnexRecord{record}, []bool{true})
	if err != nil {
		t.Fatalf("evaluate the unrated sample: %v", err)
	}
	if total != 1 || complete != 0 {
		t.Fatalf("gate counts = (%d, %d), want one sample and no complete sample", total, complete)
	}
	if ledger := sink.Ledger(); ledger.Admitted != 2 {
		t.Fatalf("pre-seal ledger = %+v, want the unrated annex and its incomplete marker admitted before the seal", ledger)
	}
	waitForLines(t, dir, 2)
	records := readAnnex(t, dir)
	if records[0].Verdict != sample.VerdictUnrated {
		t.Fatalf("first annex verdict = %q, want %q", records[0].Verdict, sample.VerdictUnrated)
	}
	if records[1].Verdict != sample.VerdictIncomplete {
		t.Fatalf("second annex verdict = %q, want the persistent incomplete marker", records[1].Verdict)
	}
	for _, got := range records {
		if got.AttemptID != record.AttemptID || got.TaskID != record.TaskID {
			t.Fatalf("annex identity = (%q, %q), want (%q, %q)", got.AttemptID, got.TaskID, record.AttemptID, record.TaskID)
		}
	}
	if _, err := sink.Seal(ctx); err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	if err := sink.RecordAnnex(ctx, record); err == nil {
		t.Fatal("annex offered after the seal = nil error, want the sink's post-seal refusal")
	}
}

// TestSampleGateRequiresAllThreeElements pins that a sample counts as complete only when read-back, independent verdict, and annex write are all present.
// - incomplete samples get an append-only marker and stay in the readiness denominator
// - the sealed manifest accounts every annex line and drops none
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestSampleGateRequiresAllThreeElements(t *testing.T) {
	sink, dir := gateCapture(t)
	ctx := context.Background()
	samples := []rl.AnnexRecord{
		{
			AttemptID:   "attempt:complete",
			TaskID:      "bench:qa-1",
			Verdict:     sample.VerdictPass,
			RuleVersion: sample.RuleVersionV1,
			Artifacts:   []string{"artifact:bench:qa-1/answer.txt"},
		},
		{
			AttemptID: "attempt:no-acceptance",
			TaskID:    "bench:file-2",
		},
		{
			AttemptID:   "attempt:no-readback",
			TaskID:      "bench:exec-3",
			Verdict:     sample.VerdictFail,
			RuleVersion: sample.RuleVersionV1,
			Artifacts:   []string{"artifact:bench:exec-3/stdout.txt"},
		},
	}
	readback := []bool{true, true, false}
	total, complete, err := sample.Evaluate(ctx, sink, samples, readback)
	if err != nil {
		t.Fatalf("evaluate the run's samples: %v", err)
	}
	if total != 3 || complete != 1 {
		t.Fatalf("gate counts = (%d, %d), want three samples and one complete", total, complete)
	}
	if rate := float64(complete) / float64(total); rate != 1.0/3.0 {
		t.Fatalf("readiness rate = %v, want 1/3 over all three samples including the incomplete ones", rate)
	}
	if ledger := sink.Ledger(); ledger.Admitted != 5 {
		t.Fatalf("pre-seal ledger = %+v, want three acceptance annexes and two incomplete markers admitted before the seal", ledger)
	}
	waitForLines(t, dir, 5)
	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	if manifest.Records != 5 || manifest.Dropped != 0 || !manifest.Sealed {
		t.Fatalf("manifest = %+v, want five accounted annex records and no loss", manifest)
	}
	records := readAnnex(t, dir)
	bySample := map[string][]rl.AnnexRecord{}
	for _, record := range records {
		key := string(record.AttemptID) + "|" + record.TaskID
		bySample[key] = append(bySample[key], record)
	}
	completeAnnex := bySample["attempt:complete|bench:qa-1"]
	if len(completeAnnex) != 1 || completeAnnex[0].Verdict != sample.VerdictPass {
		t.Fatalf("complete sample annex = %+v, want exactly the pass verdict", completeAnnex)
	}
	unrated := bySample["attempt:no-acceptance|bench:file-2"]
	if len(unrated) != 2 || unrated[0].Verdict != sample.VerdictUnrated || unrated[1].Verdict != sample.VerdictIncomplete {
		t.Fatalf("no-acceptance sample annex = %+v, want unrated beside the incomplete marker", unrated)
	}
	failed := bySample["attempt:no-readback|bench:exec-3"]
	if len(failed) != 2 || failed[0].Verdict != sample.VerdictFail || failed[1].Verdict != sample.VerdictIncomplete {
		t.Fatalf("no-readback sample annex = %+v, want the fail verdict beside the incomplete marker", failed)
	}
	incomplete := 0
	for _, record := range records {
		if record.Verdict == sample.VerdictIncomplete {
			incomplete++
		}
	}
	if incomplete != 2 {
		t.Fatalf("incomplete markers = %d, want the two incomplete samples kept in the account", incomplete)
	}
}

// gateCapture opens one real capture sink and its directory.
func gateCapture(t *testing.T) (*rl.CaptureSink, string) {
	t.Helper()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 4)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap:gate", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	return sink, dir
}

// waitForLines waits until the record file holds want annex lines, so a test
// can observe the gate's persistence before the seal rather than after it.
func waitForLines(t *testing.T, dir string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if countLines(filepath.Join(dir, rl.SinkRecordsFile)) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("record file holds fewer than %d lines before the seal", want)
		}
		time.Sleep(time.Millisecond)
	}
}

// countLines reports the non-empty lines one file currently holds.
func countLines(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, raw := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if raw != "" {
			count++
		}
	}
	return count
}

// readAnnex reads the stored record file back and returns the annex payloads in
// write order.
func readAnnex(t *testing.T, dir string) []rl.AnnexRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the record file: %v", err)
	}
	var records []rl.AnnexRecord
	for _, raw := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if raw == "" {
			continue
		}
		line, err := rl.ParseLine([]byte(raw))
		if err != nil {
			t.Fatalf("parse the stored line: %v", err)
		}
		if line.Annex() == nil {
			t.Fatalf("stored line kind = %q, want an annex line", line.Kind)
		}
		records = append(records, *line.Annex())
	}
	return records
}
