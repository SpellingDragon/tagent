package learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/rl"
)

// TestCaptureAdmissionAndSeal drives the bounded capture queue and its writer seal and checks what they actually keep:
// - a record is admitted under a per-record, an in-flight and a byte bound, and every bound refusal is counted loss with its bound named
// - admission never waits on the writer and the loss ledger stays readable while the queue is full
// - sending and closing share one protocol, and the seal accounts exactly what was admitted before it
// - the writer seal is real storage evidence, and a capture without one reads back partial with its reason
// - the attempt record shares the one terminal the model reduction derived and never re-derives one
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestCaptureAdmissionAndSeal(t *testing.T) {
	normal := attemptRecordOf("attempt-normal", "the answer")
	oversized := attemptRecordOf("attempt-oversized", strings.Repeat("x", 4096))
	normalSize := int64(len(marshalAttemptLine(t, normal)))

	t.Run("a negative bound is refused and the queue holds its record size and in-flight bounds", func(t *testing.T) {
		writer := newMemWriter()
		if _, err := rl.NewCaptureSink("cap-bounds", rl.SinkConfig{MaxRecordBytes: -1}, writer); !errors.Is(err, rl.ErrNegativeBound) {
			t.Fatalf("negative record bound err = %v, want ErrNegativeBound", err)
		}
		if _, err := rl.NewCaptureSink("cap-bounds", rl.SinkConfig{MaxInFlight: -1}, writer); !errors.Is(err, rl.ErrNegativeBound) {
			t.Fatalf("negative in-flight bound err = %v, want ErrNegativeBound", err)
		}
		if _, err := rl.NewCaptureSink("cap-bounds", rl.SinkConfig{MaxBytes: -1}, writer); !errors.Is(err, rl.ErrNegativeBound) {
			t.Fatalf("negative byte budget err = %v, want ErrNegativeBound", err)
		}
		if _, err := rl.NewFileCaptureWriter(t.TempDir(), -1); !errors.Is(err, rl.ErrNegativeBound) {
			t.Fatalf("negative open file bound err = %v, want ErrNegativeBound", err)
		}
		if _, err := rl.NewCaptureSink("", rl.SinkConfig{}, writer); !errors.Is(err, rl.ErrCaptureIDMissing) {
			t.Fatalf("missing capture id err = %v, want ErrCaptureIDMissing", err)
		}
		if _, err := rl.NewCaptureSink("cap-bounds", rl.SinkConfig{}, nil); !errors.Is(err, rl.ErrWriterMissing) {
			t.Fatalf("missing writer err = %v, want ErrWriterMissing", err)
		}

		release := writer.gateAppends()
		sink, err := rl.NewCaptureSink("cap-bounds", rl.SinkConfig{MaxRecordBytes: normalSize, MaxInFlight: 2}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		defer func() {
			close(release)
			if _, err := sink.Seal(context.Background()); err != nil {
				t.Errorf("seal the sink at cleanup: %v", err)
			}
		}()
		if admission, err := sink.Record(context.Background(), oversized); err != nil || admission.Admitted || admission.Bound != rl.BoundRecordSize {
			t.Fatalf("oversized record admission = %+v (%v), want refused under BoundRecordSize", admission, err)
		}
		for i := 0; i < 2; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted under the in-flight bound", i, admission, err)
			}
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Admitted || admission.Bound != rl.BoundInFlight {
			t.Fatalf("third record admission = %+v (%v), want refused under BoundInFlight", admission, err)
		}
		done := make(chan rl.CaptureAdmission, 1)
		go func() {
			admission, _ := sink.Record(context.Background(), normal)
			done <- admission
		}()
		select {
		case admission := <-done:
			if admission.Admitted || admission.Bound != rl.BoundInFlight {
				t.Fatalf("record offered to the full queue = %+v, want refused without waiting on the stuck writer", admission)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Record waited on the stuck writer, want admission that never waits on the drain")
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 2 || ledger.InFlight != 2 || ledger.Written != 0 {
			t.Fatalf("ledger = %+v, want 2 admitted and still in flight while the writer is stuck", ledger)
		}
		if ledger.DroppedRecordSize != 1 || ledger.DroppedInFlight != 2 || ledger.Dropped() != 3 {
			t.Fatalf("ledger = %+v, want 1 record size and 2 in-flight refusals counted", ledger)
		}
	})

	t.Run("the byte budget refuses past its limit and keeps the stored history in place", func(t *testing.T) {
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the file writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-bytes", rl.SinkConfig{MaxBytes: normalSize}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		line := marshalAttemptLine(t, normal)
		if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
			t.Fatalf("first record admission = %+v (%v), want admitted inside the byte budget", admission, err)
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Admitted || admission.Bound != rl.BoundBytes {
			t.Fatalf("second record admission = %+v (%v), want refused under BoundBytes", admission, err)
		}
		manifest, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		if manifest.Records != 1 || manifest.Dropped != 1 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want 1 record written and the refused one counted partial", manifest)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "records were refused under an admission bound") {
			t.Fatalf("manifest reasons = %v, want the refusal named", manifest.Reasons)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read the capture directory: %v", err)
		}
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		if want := []string{rl.SinkRecordsFile, rl.SealFileName}; !reflect.DeepEqual(names, want) {
			t.Fatalf("capture directory holds %v, want %v with no rotated or removed file", names, want)
		}
		stored, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
		if err != nil {
			t.Fatalf("read the stored records: %v", err)
		}
		want := append(append([]byte(nil), line...), '\n')
		if !bytes.Equal(stored, want) {
			t.Fatalf("stored history = %q, want the admitted line kept byte for byte (%q)", stored, want)
		}
	})

	t.Run("the loss ledger counts every refusal by bound and stays readable while the queue is full", func(t *testing.T) {
		writer := newMemWriter()
		release := writer.gateAppends()
		var releaseOnce sync.Once
		open := func() { releaseOnce.Do(func() { close(release) }) }
		sink, err := rl.NewCaptureSink("cap-ledger", rl.SinkConfig{MaxRecordBytes: normalSize, MaxInFlight: 2}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		defer func() {
			open()
			if _, err := sink.Seal(context.Background()); err != nil {
				t.Errorf("seal the sink at cleanup: %v", err)
			}
		}()
		for i := 0; i < 2; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		if admission, err := sink.Record(context.Background(), oversized); err != nil || admission.Bound != rl.BoundRecordSize {
			t.Fatalf("oversized record admission = %+v (%v), want refused under BoundRecordSize", admission, err)
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Bound != rl.BoundInFlight {
			t.Fatalf("queued record admission = %+v (%v), want refused under BoundInFlight", admission, err)
		}
		ledger := sink.Ledger()
		if ledger.Admitted != 2 || ledger.InFlight != 2 || ledger.Written != 0 {
			t.Fatalf("ledger read through the full queue = %+v, want 2 admitted and in flight", ledger)
		}
		if ledger.DroppedRecordSize != 1 || ledger.DroppedInFlight != 1 || ledger.Dropped() != 2 {
			t.Fatalf("ledger = %+v, want one refusal counted under each bound", ledger)
		}
		open()
		manifest, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		if manifest.Records != 2 || manifest.Dropped != 2 || !manifest.Partial {
			t.Fatalf("manifest = %+v, want the 2 written and the 2 refused records both accounted", manifest)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "2 records were refused under an admission bound") {
			t.Fatalf("manifest reasons = %v, want the counted loss named", manifest.Reasons)
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Bound != rl.BoundClosed {
			t.Fatalf("record after close admission = %+v (%v), want refused under BoundClosed", admission, err)
		}
		ledger = sink.Ledger()
		if ledger.DroppedClosed != 1 || ledger.Dropped() != 3 {
			t.Fatalf("ledger after close = %+v, want the closed refusal counted", ledger)
		}
	})

	t.Run("sending and closing share one protocol under concurrency", func(t *testing.T) {
		writer := newMemWriter()
		release := writer.gateAppends()
		sink, err := rl.NewCaptureSink("cap-concurrent", rl.SinkConfig{MaxInFlight: 2}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i := 0; i < 2; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted while the drain is held", i, admission, err)
			}
		}
		var mu sync.Mutex
		admitted := 0
		refused := map[rl.CaptureBound]int{}
		var senders sync.WaitGroup
		for g := 0; g < 5; g++ {
			senders.Add(1)
			go func() {
				defer senders.Done()
				for i := 0; i < 20; i++ {
					admission, err := sink.Record(context.Background(), normal)
					if err != nil {
						t.Errorf("record: %v", err)
						return
					}
					mu.Lock()
					if admission.Admitted {
						admitted++
					} else {
						refused[admission.Bound]++
					}
					mu.Unlock()
				}
			}()
		}
		sealResult := make(chan rl.CaptureManifest, 1)
		go func() {
			manifest, err := sink.Seal(context.Background())
			if err != nil {
				t.Errorf("seal: %v", err)
			}
			sealResult <- manifest
		}()
		senders.Wait()
		close(release)
		manifest := <-sealResult
		ledger := sink.Ledger()
		if int(ledger.Admitted) != admitted+2 {
			t.Fatalf("ledger admitted = %d, want the 2 held and %d sender records admitted", ledger.Admitted, admitted)
		}
		if int(ledger.Dropped()) != refused[rl.BoundRecordSize]+refused[rl.BoundInFlight]+refused[rl.BoundBytes]+refused[rl.BoundClosed] {
			t.Fatalf("ledger drops = %d, want the sender refusals %v counted", ledger.Dropped(), refused)
		}
		if int(ledger.DroppedInFlight) != refused[rl.BoundInFlight] || int(ledger.DroppedClosed) != refused[rl.BoundClosed] {
			t.Fatalf("ledger = %+v, want each refusal counted under the bound that refused it (%v)", ledger, refused)
		}
		if manifest.Records != int64(admitted+2) {
			t.Fatalf("manifest records = %d, want the %d admitted records all written", manifest.Records, admitted+2)
		}
		if manifest.Dropped != ledger.Dropped() {
			t.Fatalf("manifest dropped = %d, want the ledger count %d of what the close refused", manifest.Dropped, ledger.Dropped())
		}
		if ledger.Dropped() > 0 && !manifest.Partial {
			t.Fatalf("manifest = %+v, want a capture that refused records accounted partial", manifest)
		}
		if manifest.Sealed && !manifest.Partial && (ledger.Dropped() != 0 || ledger.InFlight != 0 || ledger.WriteErrors != 0) {
			t.Fatalf("manifest = %+v claims complete over ledger %+v, want complete only over a quiet ledger", manifest, ledger)
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Bound != rl.BoundClosed {
			t.Fatalf("record after seal admission = %+v (%v), want refused under BoundClosed by the same protocol", admission, err)
		}
	})

	t.Run("the writer seal is real storage evidence and reads back after a restart", func(t *testing.T) {
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the file writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-seal", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i := 0; i < 3; i++ {
			if admission, err := sink.Record(context.Background(), attemptRecordOf(fmt.Sprintf("attempt-seal-%d", i), fmt.Sprintf("answer %d", i))); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		manifest, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		if !manifest.Sealed || manifest.Partial || manifest.Records != 3 {
			t.Fatalf("manifest = %+v, want a complete seal of 3 records", manifest)
		}
		if !manifest.Sync.Synced || manifest.Sync.Reason != "" {
			t.Fatalf("manifest sync = %+v, want the writer's own completed sync", manifest.Sync)
		}
		if len(manifest.Files) != 1 {
			t.Fatalf("manifest files = %+v, want the one record file accounted", manifest.Files)
		}
		file := manifest.Files[0]
		path := filepath.Join(dir, rl.SinkRecordsFile)
		if file.Name != rl.SinkRecordsFile {
			t.Fatalf("file entry name = %q, want %q", file.Name, rl.SinkRecordsFile)
		}
		if got := fileDigest(t, path); got != file.SHA256 {
			t.Fatalf("file digest = %s, want the digest of the bytes on disk (%s)", got, file.SHA256)
		}
		stored, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the stored records: %v", err)
		}
		if int64(len(stored)) != file.Bytes {
			t.Fatalf("file holds %d bytes, want the accounted %d", len(stored), file.Bytes)
		}
		if got := int64(bytes.Count(stored, []byte{'\n'})); got != file.Records || got != manifest.Records {
			t.Fatalf("file holds %d records, want the accounted (%d, %d)", got, file.Records, manifest.Records)
		}
		read, err := rl.ReadCaptureSeal(dir)
		if err != nil {
			t.Fatalf("read the capture seal back: %v", err)
		}
		wantJSON, _ := json.Marshal(manifest)
		gotJSON, _ := json.Marshal(read)
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Fatalf("seal reads back %s, want the confirmed manifest %s", gotJSON, wantJSON)
		}
	})

	t.Run("a sync the writer did not confirm is never reported as a completed seal", func(t *testing.T) {
		writer := newMemWriter()
		writer.syncErr = "the storage refused the sync"
		sink, err := rl.NewCaptureSink("cap-unsynced", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i := 0; i < 2; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		manifest, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		if manifest.Sealed || !manifest.Partial {
			t.Fatalf("manifest = %+v, want an unsynced seal accounted partial", manifest)
		}
		if manifest.Sync.Synced || manifest.Sync.Reason != "the storage refused the sync" {
			t.Fatalf("manifest sync = %+v, want the writer's own failed sync reported", manifest.Sync)
		}
		if reason := strings.Join(manifest.Reasons, "; "); !strings.Contains(reason, "no writer seal") || !strings.Contains(reason, "the storage refused the sync") {
			t.Fatalf("manifest reasons = %v, want the missing seal named with its cause", manifest.Reasons)
		}
		if manifest.Records != 2 {
			t.Fatalf("manifest records = %d, want the 2 written records still accounted", manifest.Records)
		}
	})

	t.Run("an abandoned drain is accounted partial with its in-flight and loss counts", func(t *testing.T) {
		writer := newMemWriter()
		release := writer.gateAppends()
		sink, err := rl.NewCaptureSink("cap-abandon", rl.SinkConfig{MaxInFlight: 8}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i := 0; i < 4; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		manifest, err := sink.Seal(ctx)
		if err != nil {
			t.Fatalf("seal the stuck capture: %v", err)
		}
		if manifest.Sealed || !manifest.Partial {
			t.Fatalf("manifest = %+v, want the abandoned drain accounted partial", manifest)
		}
		if manifest.InFlight != 4 || manifest.Records != 0 {
			t.Fatalf("manifest = %+v, want 4 records in flight and none written at the abandonment", manifest)
		}
		if reason := strings.Join(manifest.Reasons, "; "); !strings.Contains(reason, "no writer seal") || !strings.Contains(reason, "4 records were still in flight") {
			t.Fatalf("manifest reasons = %v, want the missing seal and the in-flight count named", manifest.Reasons)
		}
		close(release)
		final, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the drained capture: %v", err)
		}
		if !final.Sealed || final.Partial || final.Records != 4 || final.InFlight != 0 {
			t.Fatalf("final manifest = %+v, want the drained capture sealed complete", final)
		}
	})

	t.Run("a capture read back without a writer seal is partial", func(t *testing.T) {
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the file writer: %v", err)
		}
		for i := 0; i < 2; i++ {
			line := marshalAttemptLine(t, attemptRecordOf(fmt.Sprintf("attempt-crash-%d", i), fmt.Sprintf("answer %d", i)))
			if err := writer.Append(rl.SinkRecordsFile, line); err != nil {
				t.Fatalf("append record %d: %v", i, err)
			}
		}
		manifest, err := rl.ReadCaptureSeal(dir)
		if err != nil {
			t.Fatalf("read the capture state back: %v", err)
		}
		if manifest.Sealed || !manifest.Partial {
			t.Fatalf("manifest = %+v, want the capture without a writer seal read back partial", manifest)
		}
		if !strings.Contains(strings.Join(manifest.Reasons, "; "), "no writer seal") {
			t.Fatalf("manifest reasons = %v, want the missing seal named", manifest.Reasons)
		}
		stored, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
		if err != nil {
			t.Fatalf("read the stored records: %v", err)
		}
		if got := bytes.Count(stored, []byte{'\n'}); got != 2 {
			t.Fatalf("stored history holds %d records, want the 2 written before the crash kept", got)
		}
	})

	t.Run("the attempt record shares the one terminal the model reduction derived", func(t *testing.T) {
		reduce := func(responses ...*model.Response) model.Outcome {
			ch := make(chan *model.Response, len(responses))
			for _, resp := range responses {
				ch <- resp
			}
			close(ch)
			return model.ReduceChannel(context.Background(), ch, nil)
		}
		cancelled := &model.Response{Done: true, Error: &model.ResponseError{Message: "cancelled", Type: model.ErrorTypeCancelled}}
		partialTurn := learnFinalTurn("the part")
		partialTurn.IsPartial = true
		outcomes := []model.Outcome{
			reduce(learnFinalTurn("the answer")),
			reduce(learnFailTurn("the failure")),
			reduce(cancelled),
			reduce(partialTurn),
			reduce(),
		}
		want := []model.Kind{model.KindCompleted, model.KindFailed, model.KindCancelled, model.KindPartialOnly, model.KindUnknown}
		for i, outcome := range outcomes {
			if outcome.Kind != want[i] {
				t.Fatalf("reduction %d derived Kind %d, want %d", i, outcome.Kind, want[i])
			}
		}
		dir := t.TempDir()
		writer, err := rl.NewFileCaptureWriter(dir, 0)
		if err != nil {
			t.Fatalf("build the file writer: %v", err)
		}
		sink, err := rl.NewCaptureSink("cap-terminal", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i, outcome := range outcomes {
			admission, err := sink.RecordOutcome(context.Background(), event.AttemptID(fmt.Sprintf("attempt-terminal-%d", i)), event.DecisionID("decision-a"), event.EpisodeID("episode-a"), outcome)
			if err != nil || !admission.Admitted {
				t.Fatalf("outcome %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		if _, err := sink.Seal(context.Background()); err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		stored, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
		if err != nil {
			t.Fatalf("read the stored records: %v", err)
		}
		lines := bytes.Split(bytes.TrimSuffix(stored, []byte{'\n'}), []byte{'\n'})
		if len(lines) != len(outcomes) {
			t.Fatalf("stored history holds %d records, want the %d outcomes recorded", len(lines), len(outcomes))
		}
		for i, raw := range lines {
			line, err := rl.ParseLine(raw)
			if err != nil {
				t.Fatalf("parse record %d: %v", i, err)
			}
			if line.Attempt == nil || line.Attempt.Terminal != outcomes[i].Kind {
				t.Fatalf("record %d terminal = %+v, want the derived Kind %d shared, never re-derived", i, line.Attempt, outcomes[i].Kind)
			}
		}
	})

	t.Run("a second seal returns the first manifest and never seals twice", func(t *testing.T) {
		writer := newMemWriter()
		sink, err := rl.NewCaptureSink("cap-reseal", rl.SinkConfig{}, writer)
		if err != nil {
			t.Fatalf("build the capture sink: %v", err)
		}
		for i := 0; i < 2; i++ {
			if admission, err := sink.Record(context.Background(), normal); err != nil || !admission.Admitted {
				t.Fatalf("record %d admission = %+v (%v), want admitted", i, admission, err)
			}
		}
		first, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture: %v", err)
		}
		second, err := sink.Seal(context.Background())
		if err != nil {
			t.Fatalf("seal the capture again: %v", err)
		}
		if !reflect.DeepEqual(second, first) {
			t.Fatalf("second seal = %+v, want the first manifest %v returned unchanged", second, first)
		}
		if got := writer.sealCount(); got != 1 {
			t.Fatalf("writer sealed %d times, want one seal", got)
		}
		if got := writer.commitCount(); got != 1 {
			t.Fatalf("writer committed %d times, want one persisted seal", got)
		}
		if admission, err := sink.Record(context.Background(), normal); err != nil || admission.Bound != rl.BoundClosed {
			t.Fatalf("record after seal admission = %+v (%v), want refused under BoundClosed", admission, err)
		}
	})
}

// attemptRecordOf builds one attempt record of a completed scripted answer
// through the single observation face.
func attemptRecordOf(id, content string) rl.AttemptRecord {
	return rl.AttemptOf(event.AttemptID(id), event.DecisionID("decision-a"), event.EpisodeID("episode-a"), model.Outcome{
		Kind:  model.KindCompleted,
		Usage: model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		Body: &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
			Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			Done:    true,
		},
	})
}

// marshalAttemptLine reports the exact versioned line the sink marshals one
// attempt record into.
func marshalAttemptLine(t *testing.T, record rl.AttemptRecord) []byte {
	t.Helper()
	line, err := rl.MarshalLine(rl.NewAttemptLine(record))
	if err != nil {
		t.Fatalf("marshal the attempt line: %v", err)
	}
	return line
}

// memWriter is the writer stand-in: it accounts the lines it is handed and the
// seals and commits it is asked for, and it reports its own sync result at seal
// time. Its gate holds every append while it is open, so a stuck drain is
// observable.
type memWriter struct {
	mu        sync.Mutex
	gate      chan struct{}
	lines     map[string][][]byte
	seals     int
	commits   int
	syncErr   string
	committed rl.CaptureManifest
}

// newMemWriter returns an empty in-memory writer.
func newMemWriter() *memWriter {
	return &memWriter{lines: make(map[string][][]byte)}
}

// gateAppends holds every append of this writer until the returned release
// channel is closed.
func (w *memWriter) gateAppends() chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gate = make(chan struct{})
	return w.gate
}

// Append accounts one record line once the gate is open.
func (w *memWriter) Append(name string, line []byte) error {
	w.mu.Lock()
	gate := w.gate
	w.mu.Unlock()
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines[name] = append(w.lines[name], append([]byte(nil), line...))
	return nil
}

// Seal reports the per-file digests of the lines it holds and its own sync
// result.
func (w *memWriter) Seal() ([]rl.CaptureFileDigest, rl.WriterSyncResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seals++
	names := make([]string, 0, len(w.lines))
	for name := range w.lines {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]rl.CaptureFileDigest, 0, len(names))
	for _, name := range names {
		sha := sha256.New()
		var size int64
		for _, line := range w.lines[name] {
			sha.Write(line)
			sha.Write([]byte{'\n'})
			size += int64(len(line)) + 1
		}
		files = append(files, rl.CaptureFileDigest{
			Name:    name,
			SHA256:  hex.EncodeToString(sha.Sum(nil)),
			Bytes:   size,
			Records: int64(len(w.lines[name])),
		})
	}
	return files, rl.WriterSyncResult{Synced: w.syncErr == "", Reason: w.syncErr, At: time.Now()}
}

// Commit accounts the persisted seal.
func (w *memWriter) Commit(manifest rl.CaptureManifest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commits++
	w.committed = manifest
	return nil
}

// sealCount reports how many seals this writer confirmed.
func (w *memWriter) sealCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seals
}

// commitCount reports how many seals this writer persisted.
func (w *memWriter) commitCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.commits
}
