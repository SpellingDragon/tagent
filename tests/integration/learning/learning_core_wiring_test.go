package learning

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
)

// TestReplacementFactInstallsNewVersion pins the event-driven replacement lifecycle over the formal assembly entry.
//   - a model configuration change of the configuration change family commits on the agent's own chain under the model dimension
//   - the change is consumed through the agent's production pipeline by the mounted lease observer
//   - the lease rebuilds, publishes and installs the requested version, so later requests borrow it while earlier records keep theirs
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestReplacementFactInstallsNewVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	records := &collector{}
	first := newReplacementModel("model-a", "version one answer")
	lease, err := rl.NewModelLease(first, "v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	run, err := assembly.Assemble(ctx, replacementDecl("model-a"), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: first},
	}, assembly.WithGovernance(formalPolicy{}),
		assembly.WithRecorder(records),
		assembly.WithModelLease("agent-a", lease, assembly.WithLeaseRebuild(func(version string) (model.Model, error) {
			if version != "v2" {
				return nil, fmt.Errorf("the rebuild was asked for version %q", version)
			}
			return newReplacementModel("model-a", "version two answer"), nil
		})))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	if turn := replacementInvoke(t, ctx, run, "inv-fact-1", "session-fact", "ep-fact"); turn.Terminal != model.KindCompleted {
		t.Fatalf("first turn terminal = %v, want the completed turn of the version in effect", turn.Terminal)
	}
	if lease.CurrentVersion() != "model-a@v1" {
		t.Fatalf("the lease holds %q before any replacement, want the version it was built with", lease.CurrentVersion())
	}

	key := fileModelChange(t, ctx, run, "session-fact", "model-a@v1", "v2")
	if lease.CurrentVersion() != "model-a@v2" {
		t.Fatalf("the lease holds %q after the model configuration change, want the version the change installed", lease.CurrentVersion())
	}

	fact, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed model change: %v", err)
	}
	if fact.Type != event.TypeConfigChange {
		t.Fatalf("committed fact type = %q, want the configuration change family", fact.Type)
	}
	if fact.Owner.AgentID != "agent-a" {
		t.Fatalf("committed fact owner = %q, want the agent whose model moved", fact.Owner.AgentID)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact dimension = %q, want the model dimension", got)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeGeneration]; got != "0" {
		t.Fatalf("committed fact generation = %q, want no publication generation on an explicit judgment", got)
	}
	if got := fact.Meta[event.MetaKeyTriggerSource]; got != event.ConfigChangeSourceConfig {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta[event.MetaKeyModelFromVersion]; got != "model-a@v1" {
		t.Fatalf("committed fact from version = %q, want the version the move started from", got)
	}
	if got := fact.Meta[event.MetaKeyModelToVersion]; got != "v2" {
		t.Fatalf("committed fact to version = %q, want the version the move asked for", got)
	}
	if got := fact.Meta[event.MetaKeyModelReplacementResult]; got != event.ModelReplacementResultRequested {
		t.Fatalf("committed fact result = %q, want the requested result", got)
	}
	history := lease.Replacements()
	if len(history) != 1 || history[0].State != rl.ReplacementApplied {
		t.Fatalf("replacement history = %+v, want the one applied replacement", history)
	}
	if history[0].From != "model-a@v1" || history[0].To != "model-a@v2" {
		t.Fatalf("replacement record = %+v, want the move from model-a@v1 to model-a@v2", history[0])
	}

	if turn := replacementInvoke(t, ctx, run, "inv-fact-2", "session-fact", "ep-fact"); turn.Terminal != model.KindCompleted {
		t.Fatalf("second turn terminal = %v, want the completed turn of the installed version", turn.Terminal)
	}
	attempts := records.snapshot().attempts
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want one per decision the two versions served", len(attempts))
	}
	if attempts[0].ModelVersion != "v1" || attempts[1].ModelVersion != "v2" {
		t.Fatalf("attempt versions = (%q, %q), want the version that served each attempt", attempts[0].ModelVersion, attempts[1].ModelVersion)
	}
	publications := run.LeasePublications()
	if len(publications) != 1 || publications[0].Name() != "model-a@v2" {
		t.Fatalf("lease publications = %+v, want the one version the replacement installed", publications)
	}
}

// TestCaptureSealsAttemptsFile pins the three capture points of one turn over the formal assembly entry.
//   - the request view is fixed before the request starts and the fixed snapshot reads back through its reference
//   - every observed attempt is admitted through the bounded sink beside the response that produced it
//   - the turn seals the capture, so the attempt records land in the capture's record file and the writer-confirmed manifest reads back complete
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestCaptureSealsAttemptsFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("build the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap-wiring", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("build the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCapture("cap-wiring", sink, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("build the trajectory capture: %v", err)
	}
	provider := &learnModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return learnFinalTurn("settled")
	}}
	run, err := assembly.Assemble(ctx, replacementDecl("model-a"), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(formalPolicy{}), assembly.WithCapture(capture))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	if turn := replacementInvoke(t, ctx, run, "inv-capture", "session-capture", "ep-capture"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the captured request", turn.Terminal)
	}

	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the sealed record file: %v", err)
	}
	lines := 0
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		if _, err := rl.ParseLine(raw); err != nil {
			t.Fatalf("parse stored record %q: %v", raw, err)
		}
		lines++
	}
	if lines < 1 {
		t.Fatalf("stored record lines = %d, want at least the attempt of the one captured decision", lines)
	}

	manifest, err := rl.ReadCaptureSeal(dir)
	if err != nil {
		t.Fatalf("read the capture seal: %v", err)
	}
	if !manifest.Sealed || manifest.Partial {
		t.Fatalf("manifest = %+v, want the writer-confirmed seal of a complete capture", manifest)
	}
	if manifest.CaptureID != "cap-wiring" || manifest.Records < 1 || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want the capture id, at least one record and no loss", manifest)
	}
	if len(manifest.Files) == 0 || manifest.Files[0].Name != rl.SinkRecordsFile || manifest.Files[0].Records < 1 {
		t.Fatalf("manifest files = %+v, want the sealed record file entry", manifest.Files)
	}

	refs := capture.SnapshotRefs()
	if len(refs) != 1 {
		t.Fatalf("fixed snapshots = %d, want the one request view of the one decision", len(refs))
	}
	snapshot, err := capture.SnapshotOf(refs[0])
	if err != nil {
		t.Fatalf("read the fixed snapshot back: %v", err)
	}
	if snapshot.Model != "model-a" {
		t.Fatalf("snapshot model = %q, want the model the request was declared against", snapshot.Model)
	}
	if len(snapshot.Messages) == 0 || snapshot.Messages[len(snapshot.Messages)-1].Content != "learn me" {
		t.Fatalf("snapshot messages = %+v, want the request the model actually saw", snapshot.Messages)
	}
	attempts := replacementAttemptsOf(t, dir)
	if len(attempts) != 1 || attempts[0].DecisionID == "" {
		t.Fatalf("stored attempts = %+v, want the one attempt of the captured decision", attempts)
	}
}

// TestFailedReplacementKeepsOldVersion pins the negative anchor: a refused rebuild installs no version.
//   - a model configuration change whose rebuild fails installs no version: the lease keeps the version in effect
//   - the refused attempt is recorded beside its named reason, and the version in effect is never closed
//   - the committed fact still reads back, the refusal presents as a gap on the agent's pipeline, and later requests keep borrowing the old version
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestFailedReplacementKeepsOldVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	records := &collector{}
	first := newReplacementModel("model-a", "version one answer")
	lease, err := rl.NewModelLease(first, "v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	cause := errors.New("the rebuild could not build the model")
	run, err := assembly.Assemble(ctx, replacementDecl("model-a"), map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: first},
	}, assembly.WithGovernance(formalPolicy{}),
		assembly.WithRecorder(records),
		assembly.WithModelLease("agent-a", lease, assembly.WithLeaseRebuild(func(string) (model.Model, error) {
			return nil, cause
		})))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	replacementInvoke(t, ctx, run, "inv-refused-fact-1", "session-refused-fact", "ep-refused-fact")

	key := fileModelChange(t, ctx, run, "session-refused-fact", "model-a@v1", "v2")
	if lease.CurrentVersion() != "model-a@v1" {
		t.Fatalf("the lease holds %q after a refused rebuild, want the version in effect untouched", lease.CurrentVersion())
	}
	history := lease.Replacements()
	if len(history) != 1 || history[0].State != rl.ReplacementRefused {
		t.Fatalf("replacement history = %+v, want the one refused attempt", history)
	}
	if !strings.Contains(history[0].Reason, cause.Error()) {
		t.Fatalf("refused reason = %q, want the reason the rebuild failed", history[0].Reason)
	}
	if history[0].To != "model-a@v2" {
		t.Fatalf("refused record to = %q, want the version the fact asked for", history[0].To)
	}
	if closed := first.closed(); closed != 0 {
		t.Fatalf("the version in effect was closed %d times, want it open beside a refused replacement", closed)
	}

	fact, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed model change: %v", err)
	}
	if fact.Type != event.TypeConfigChange || fact.Owner.AgentID != "agent-a" {
		t.Fatalf("committed fact = %+v, want the model dimension of the configuration change family owned by the agent", fact)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact dimension = %q, want the model dimension", got)
	}
	refusal := replacementGapOf(t, run, key)
	if !errors.Is(refusal, rl.ErrLeaseRebuildFailed) || !errors.Is(refusal, cause) {
		t.Fatalf("presented refusal = %v, want the named rebuild refusal beside its cause", refusal)
	}

	replacementInvoke(t, ctx, run, "inv-refused-fact-2", "session-refused-fact", "ep-refused-fact")
	attempts := records.snapshot().attempts
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want the calls of both decisions", len(attempts))
	}
	for i, attempt := range attempts {
		if attempt.ModelVersion != "v1" {
			t.Fatalf("attempt %d model version = %q, want the version in effect that no refused replacement replaced", i, attempt.ModelVersion)
		}
	}
	if publications := run.LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside a refused rebuild", publications)
	}
}

// fileModelChange commits one model dimension configuration change on the
// agent's session chain and runs its After pass, the same fact a reload files.
func fileModelChange(t *testing.T, ctx context.Context, run *assembly.Run, session event.SessionID, from, to string) event.EventID {
	t.Helper()
	fact := event.ConfigChangeModelFor("agent-a", 0, from, to, event.ModelReplacementResultRequested)
	key, err := run.CommitToSession(ctx, event.Address{AgentID: "agent-a", SessionID: session}, fact.Event(time.Now().UTC()))
	if err != nil {
		t.Fatalf("file the model configuration change: %v", err)
	}
	if key == "" {
		t.Fatal("the model configuration change committed no fact")
	}
	return key
}

// replacementGapOf reports the observation gap the named fact presented through
// the agent's pipeline: the refusal the lease observer reported beside the fact
// that stayed committed as it committed.
func replacementGapOf(t *testing.T, run *assembly.Run, key event.EventID) error {
	t.Helper()
	for _, presentation := range run.Presentations() {
		if presentation.Fact != key {
			continue
		}
		for _, gap := range presentation.Gaps {
			if gap.Plugin == "model-lease" {
				return gap.Err
			}
		}
	}
	t.Fatalf("no model-lease gap was presented beside the committed fact %s, presentations: %+v", key, run.Presentations())
	return nil
}
