package learning

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
)

// refusingWriter is the capture writer whose storage refuses every write and
// its own seal persist: it stands in for a storage outage at the durable write
// point, so a seal over records that never committed can be read for what it
// claims and what it never claims.
type refusingWriter struct{}

// Append refuses the offered line with the storage's own named failure.
func (refusingWriter) Append(string, []byte) error {
	return errors.New("the storage refused the write")
}

// Seal reports the writer's own state over the writes it refused: no file holds
// bytes, so there is no digest entry and no confirmed sync to report.
func (refusingWriter) Seal() ([]rl.CaptureFileDigest, rl.WriterSyncResult) {
	return nil, rl.WriterSyncResult{Synced: false, Reason: "the storage refused the write", At: time.Now()}
}

// Commit refuses to persist the seal exactly as the record writes were refused.
func (refusingWriter) Commit(rl.CaptureManifest) error {
	return errors.New("the storage refused the seal persist")
}

// sealAnswer is the scripted model behind the seal checks: one tool turn on the
// request the user input opens and one settled final turn on the request the
// tool result answers.
func sealAnswer(req *model.Request) *model.Response {
	if last := req.Messages[len(req.Messages)-1]; last.Content == "learn me" {
		return learnToolTurn(learnCall("call-1", "echo-a", `{"x":1}`))
	}
	return learnFinalTurn("settled")
}

// TestFormalChainSealsCapture drives the assembled runtime over its formal entry and seals the capture its record writes land in.
//   - the episode, decision and attempt records of the formal chain reach the capture writer and seal under their real identities
//   - a record write the capture refused is counted in the run's explicit recording-loss account beside the turn it never touched
//   - a seal accounts only the records that reached storage, so refused and unwritten events seal as loss and never as records
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestFormalChainSealsCapture(t *testing.T) {
	t.Run("the formal chain's records are sealed by the writer and read back under their real identities", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-formal-seal", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, sealAnswer)
		turn := formalInvoke(t, ctx, run, "inv-seal-1", "session-seal-1", "ep-seal-1")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn of two decisions driving the record writes", turn.Terminal, len(turn.Decisions))
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the formal capture: %v", err)
		}
		if !manifest.Sealed || manifest.Partial || manifest.Records != 5 || manifest.Dropped != 0 || manifest.InFlight != 0 {
			t.Fatalf("manifest = %+v, want a complete seal over the 5 written records", manifest)
		}
		if !manifest.Sync.Synced {
			t.Fatalf("manifest sync = %+v, want the writer's own completed sync", manifest.Sync)
		}
		if len(manifest.Files) != 1 || manifest.Files[0].Name != rl.SinkRecordsFile {
			t.Fatalf("manifest files = %+v, want the one record file accounted", manifest.Files)
		}
		if got := fileDigest(t, filepath.Join(dir, rl.SinkRecordsFile)); got != manifest.Files[0].SHA256 {
			t.Fatalf("file digest = %s, want the digest of the bytes on disk (%s)", got, manifest.Files[0].SHA256)
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 5 || ledger.Written != 5 || ledger.InFlight != 0 {
			t.Fatalf("ledger = %+v, want the sink account of the 5 records it took and wrote", ledger)
		}
		if losses := run.RecordingLosses(); len(losses) != 0 {
			t.Fatalf("recording losses = %+v, want none beside the complete seal", losses)
		}
		stored, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
		if err != nil {
			t.Fatalf("read the stored records: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(string(stored), "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("stored history holds %d records, want the 5 the formal chain produced", len(lines))
		}
		var episodes []rl.EpisodeRecord
		var decisions []rl.DecisionRecord
		var attempts []rl.AttemptRecord
		for i, raw := range lines {
			line, err := rl.ParseLine([]byte(raw))
			if err != nil {
				t.Fatalf("parse record %d: %v", i, err)
			}
			switch line.Kind {
			case rl.RecordKindEpisode:
				episodes = append(episodes, *line.Episode)
			case rl.RecordKindDecision:
				decisions = append(decisions, *line.Decision)
			case rl.RecordKindAttempt:
				attempts = append(attempts, *line.Attempt)
			default:
				t.Fatalf("record %d kind = %q, want one of the three record kinds", i, line.Kind)
			}
		}
		if len(episodes) != 1 || len(decisions) != 2 || len(attempts) != 2 {
			t.Fatalf("stored records = (%d episodes, %d decisions, %d attempts), want the production points' account", len(episodes), len(decisions), len(attempts))
		}
		if episodes[0].Schema != rl.SchemaCurrent || episodes[0].ID != "ep-seal-1" {
			t.Fatalf("episode record = (schema %q, id %q), want the versioned record of ep-seal-1", episodes[0].Schema, episodes[0].ID)
		}
		if len(episodes[0].Sessions) != 1 || episodes[0].Sessions[0] != "session-seal-1" {
			t.Fatalf("episode sessions = %v, want the session the invocation ran on", episodes[0].Sessions)
		}
		if episodes[0].Lineage != rl.LineageUnknown || episodes[0].Timestamp.IsZero() {
			t.Fatalf("episode lineage and time = (%v, %v), want an explicit unknown lineage beside the write time", episodes[0].Lineage, episodes[0].Timestamp)
		}
		for i, decision := range decisions {
			if decision.ID != turn.Decisions[i].DecisionID {
				t.Fatalf("decision %d id = %q, want the real decision identity %q", i, decision.ID, turn.Decisions[i].DecisionID)
			}
			if decision.InvocationID != "inv-seal-1" || decision.EpisodeID != "ep-seal-1" {
				t.Fatalf("decision %d scope = (%q, %q), want the invocation and episode it belongs to", i, decision.InvocationID, decision.EpisodeID)
			}
			if decision.ModelView.Model != "model-a" || decision.Timestamp.IsZero() {
				t.Fatalf("decision %d model view and time = (%+v, %v), want the request material beside the write time", i, decision.ModelView, decision.Timestamp)
			}
			if attempts[i].ID == "" || attempts[i].DecisionID != decision.ID {
				t.Fatalf("attempt %d identity = (%q, %q), want one attempt bound to decision %q", i, attempts[i].ID, attempts[i].DecisionID, decision.ID)
			}
			if attempts[i].EpisodeID != "ep-seal-1" || attempts[i].Terminal != model.KindCompleted {
				t.Fatalf("attempt %d = (episode %q, terminal %v), want the shared completed terminal of ep-seal-1", i, attempts[i].EpisodeID, attempts[i].Terminal)
			}
		}
		read, err := rl.ReadCaptureSeal(dir)
		if err != nil {
			t.Fatalf("read the capture seal back: %v", err)
		}
		wantJSON, _ := json.Marshal(manifest)
		gotJSON, _ := json.Marshal(read)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("seal reads back %s, want the confirmed manifest %s", gotJSON, wantJSON)
		}
	})

	t.Run("a refused record write lands in the explicit recording-loss account and never in the sealed manifest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the capture writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-formal-loss", rl.SinkConfig{MaxRecordBytes: 1}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, sealAnswer)
		turn := formalInvoke(t, ctx, run, "inv-seal-2", "session-seal-2", "ep-seal-2")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn unaffected by the refused record writes", turn.Terminal, len(turn.Decisions))
		}
		losses := run.RecordingLosses()
		counts := map[string]int{}
		decisionLosses := map[string]bool{}
		for _, loss := range losses {
			if loss.ID == "" || loss.Err == nil || !errors.Is(loss.Err, rl.ErrCaptureRefused) {
				t.Fatalf("recording loss = %+v, want the record identity beside the named capture refusal", loss)
			}
			counts[loss.Kind]++
			if loss.Kind == "decision" {
				decisionLosses[loss.ID] = true
			}
		}
		if counts["episode"] != 1 || counts["decision"] != 2 || counts["attempt"] != 2 {
			t.Fatalf("recording loss counts = %v, want one episode, two decision and two attempt refusals", counts)
		}
		for _, decision := range turn.Decisions {
			if !decisionLosses[string(decision.DecisionID)] {
				t.Fatalf("decision loss identities = %v, want the real decision identity %q named", decisionLosses, decision.DecisionID)
			}
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 0 || ledger.DroppedRecordSize != 5 || ledger.Written != 0 || ledger.InFlight != 0 {
			t.Fatalf("ledger = %+v, want the 5 refusals counted under the record size bound", ledger)
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the refused capture: %v", err)
		}
		if manifest.Records != 0 || manifest.Dropped != 5 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want 0 sealed records beside the 5 counted refusals", manifest)
		}
		if len(manifest.Files) != 0 {
			t.Fatalf("manifest files = %+v, want no record file accounted over events that never committed", manifest.Files)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "5 records were refused under an admission bound") {
			t.Fatalf("manifest reasons = %v, want the counted refusals named", manifest.Reasons)
		}
		if _, err := os.Stat(filepath.Join(dir, rl.SinkRecordsFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("record file state = %v, want no record file beside events that never committed", err)
		}
	})

	t.Run("admitted records that never reach storage seal as loss and never as records", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sink, err := rl.NewCaptureSink("cap-formal-refused", rl.SinkConfig{}, refusingWriter{})
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		run := formalRun(t, ctx, sink, sealAnswer)
		turn := formalInvoke(t, ctx, run, "inv-seal-3", "session-seal-3", "ep-seal-3")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed turn unaffected by the storage refusals", turn.Terminal, len(turn.Decisions))
		}
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the failed capture: %v", err)
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 5 || ledger.WriteErrors != 5 || ledger.Written != 0 || ledger.InFlight != 0 {
			t.Fatalf("ledger = %+v, want the 5 admitted records accounted as write failures", ledger)
		}
		if losses := run.RecordingLosses(); len(losses) != 0 {
			t.Fatalf("recording losses = %+v, want the storage failures held in the capture ledger alone", losses)
		}
		if manifest.Sealed || manifest.Records != 0 || manifest.Dropped != 0 {
			t.Fatalf("manifest = %+v, want no record and no confirmed seal over the 5 events that never committed", manifest)
		}
		if !manifest.Partial {
			t.Fatalf("manifest = %+v, want the failed capture accounted partial", manifest)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "5 admitted records failed to reach storage") {
			t.Fatalf("manifest reasons = %v, want the write failures named", manifest.Reasons)
		}
		err = sink.RecordAttempt(context.Background(), attemptRecordOf("attempt-after-seal", "answer after seal"))
		if err == nil || !errors.Is(err, rl.ErrCaptureRefused) {
			t.Fatalf("record after seal = %v, want the named capture refusal", err)
		}
		if !strings.Contains(err.Error(), string(rl.BoundClosed)) {
			t.Fatalf("refusal = %v, want the closed bound named", err)
		}
		again, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the capture again: %v", err)
		}
		againJSON, _ := json.Marshal(again)
		firstJSON, _ := json.Marshal(manifest)
		if string(againJSON) != string(firstJSON) {
			t.Fatalf("second seal = %s, want the frozen manifest %s", againJSON, firstJSON)
		}
	})
}
