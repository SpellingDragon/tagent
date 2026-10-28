package learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// scenario10RecordMarker prefixes the machine-readable observation record of
// this scenario in the test log, exactly as the scenario runner reads it.
const scenario10RecordMarker = "TAGENT-SCENARIO-RECORD:"

// scenario10CaptureID, scenario10EpisodeA and scenario10EpisodeB name the episode
// and capture identities the scenario drives: episode A spans two sessions, so a
// split that followed a session instead of an episode would be observable, and
// episode B is the one a model replacement crosses.
const (
	scenario10CaptureID = "capture-s10"
	scenario10EpisodeA  = event.EpisodeID("episode-s10-a")
	scenario10EpisodeB  = event.EpisodeID("episode-s10-b")
)

// TestScenario10EpisodeObservationAndReplacement pins the S10 contract over the formal runtime entry.
//   - records group by episode: one episode spanning two sessions stays one group and no episode leaks into another
//   - the capture seals the attempt file its production points wrote, and the seal account matches the bytes on disk
//   - a successful replacement installs the new version, and the request in flight keeps the version it began on
//   - a failed rebuild publishes nothing and leaves the version in effect untouched
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestScenario10EpisodeObservationAndReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("build the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink(scenario10CaptureID, rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("build the capture sink: %v", err)
	}
	running := newScenario10Model("model-a", "version one answer")
	lease, err := rl.NewModelLease(running, "v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	run := scenario10Run(t, ctx, lease.Current(), lease.CurrentVersion(), sink)

	scenario10Invoke(t, ctx, run, "inv-s10-a1", "session-s10-a", scenario10EpisodeA, "question a one")
	scenario10Invoke(t, ctx, run, "inv-s10-a2", "session-s10-b", scenario10EpisodeA, "question a two")

	holding := make(chan struct{})
	running.holdUntil(holding)
	inflight := make(chan error, 1)
	go func() {
		inflight <- scenario10InvokeErr(ctx, run, "inv-s10-b1", "session-s10-c", scenario10EpisodeB, "question b one")
	}()
	scenario10WaitFor(t, "the request in flight", func() bool { return lease.InFlight() == 1 })

	published := 0
	failed, failedErr := lease.Replace(ctx, "v2", func() (model.Model, error) {
		return nil, errors.New("the rebuild of the second version failed")
	}, scenario10Publisher(ctx, run, &published))
	publishedByFailure := published

	installed := newScenario10Model("model-a", "version two answer")
	applied, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return installed, nil }, scenario10Publisher(ctx, run, &published))
	if err != nil {
		t.Fatalf("replace the model with a rebuilt version: %v", err)
	}
	stillInFlight := lease.InFlight()

	close(holding)
	if err := <-inflight; err != nil {
		t.Fatalf("the request in flight: %v", err)
	}
	running.release()
	scenario10Invoke(t, ctx, run, "inv-s10-b2", "session-s10-d", scenario10EpisodeB, "question b two")

	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	episodes, attempts, records := scenario10Stored(t, dir)
	servedByFirst, servedBySecond := 0, 0
	inflightKeptOld := false
	for _, attempt := range attempts {
		switch attempt.ModelVersion {
		case "v1":
			servedByFirst++
			inflightKeptOld = inflightKeptOld || attempt.EpisodeID == scenario10EpisodeB
		case "v2":
			servedBySecond++
		}
	}

	exporter := rl.NewChainExporter(run)
	samples, err := exporter.Export(ctx, rl.ExportOptions{SessionIDs: []event.SessionID{
		"session-s10-a", "session-s10-b", "session-s10-c", "session-s10-d",
	}})
	if err != nil {
		t.Fatalf("export the scenario chain: %v", err)
	}
	grouped := scenario10Grouped(samples)
	notLeaked := scenario10NotLeaked(samples, episodes)

	observations := map[string]any{
		"split_grouped_by_episode":   grouped,
		"shared_ancestor_not_leaked": notLeaked,
		"attempts_sealed": manifest.Sealed && !manifest.Partial &&
			manifest.Dropped == 0 && manifest.InFlight == 0 &&
			manifest.Records == int64(records) && len(manifest.Files) == 1 &&
			manifest.Files[0].Name == rl.SinkRecordsFile &&
			manifest.Files[0].SHA256 == fileDigest(t, filepath.Join(dir, rl.SinkRecordsFile)) &&
			len(attempts) == 4,
		"replacement_installs_on_success": applied.State == rl.ReplacementApplied &&
			applied.From == "model-a@v1" && applied.To == "model-a@v2" &&
			lease.CurrentVersion() == "model-a@v2" && published == 1 &&
			stillInFlight == 1 && servedBySecond == 1,
		"replacement_keeps_old_on_failure": errors.Is(failedErr, rl.ErrLeaseRebuildFailed) &&
			failed.State == rl.ReplacementRefused && publishedByFailure == 0 &&
			servedByFirst == 3 && inflightKeptOld,
	}
	scenario10Emit(t, observations, []string{scenario10CaptureID, rl.SinkRecordsFile, string(scenario10EpisodeA), string(scenario10EpisodeB)})
}

// scenario10Model is one model version of the scenario: it answers each call
// with the content it is scripted with, holds a call on the gate the check
// opens when one is set, and counts the calls it served and the times it was
// closed. It implements no iterator, so the lease bridges its stream through
// the same path an ordinary provider takes.
type scenario10Model struct {
	mu      sync.Mutex
	name    string
	content string
	hold    chan struct{}
}

// newScenario10Model returns one scripted version answering with the content it
// is named with.
func newScenario10Model(name, content string) *scenario10Model {
	return &scenario10Model{name: name, content: content}
}

// GenerateContent counts the call it serves and, when a gate is set, waits for
// the check to open it before handing over its scripted answer.
func (m *scenario10Model) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	hold, content := m.hold, m.content
	m.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	stream := make(chan *model.Response, 1)
	stream <- learnFinalTurn(content)
	close(stream)
	return stream, nil
}

// Info reports the scripted model's descriptor.
func (m *scenario10Model) Info() model.Info {
	return model.Info{Name: m.name}
}

// Close releases the version; the scripted model holds no resource.
func (m *scenario10Model) Close() error {
	return nil
}

// holdUntil makes every call of this version wait until the gate is closed.
func (m *scenario10Model) holdUntil(gate chan struct{}) {
	m.mu.Lock()
	m.hold = gate
	m.mu.Unlock()
}

// release stops holding calls, so a later call is not gated by the check.
func (m *scenario10Model) release() {
	m.mu.Lock()
	m.hold = nil
	m.mu.Unlock()
}

// scenario10Decl is the declaration of the single agent the scenario runs,
// referencing the model identity the caller names.
func scenario10Decl(modelRef string) *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: modelRef}},
	}
}

// scenario10Run assembles one runtime over the model handle and declaration
// reference the caller names, writing its records through the capture sink the
// scenario seals.
func scenario10Run(t *testing.T, ctx context.Context, handle model.Model, modelRef string, records rl.Recorder) *tagent.Run {
	t.Helper()
	run, err := tagent.Assemble(ctx, scenario10Decl(modelRef), map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: handle},
	}, tagent.WithGovernance(formalPolicy{}), tagent.WithRecorder(records))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// scenario10Invoke drives one invocation through the formal runtime entry and
// requires the completed terminal of the version in effect.
func scenario10Invoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, episode event.EpisodeID, input string) {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", invocation, err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("invocation %s terminal = %v, want the completed turn of the version in effect", invocation, turn.Terminal)
	}
}

// scenario10InvokeErr drives one invocation from a goroutine and reports the
// error it ended with, so a request held in flight is observed without
// blocking the check.
func scenario10InvokeErr(ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, episode event.EpisodeID, input string) error {
	_, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	return err
}

// scenario10WaitFor waits until the condition holds or the wait runs out, so a
// check that depends on a goroutine never reads a half-settled state.
func scenario10WaitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s did not arrive within the wait", what)
}

// scenario10Publisher is the publication entry a replacement installs its
// rebuilt model through: the declaration references the identity of the version
// being installed and the runtime publishes it as the generation a request that
// begins afterwards borrows. Every call is counted, so a rebuild that publishes
// nothing is observable.
func scenario10Publisher(ctx context.Context, run *tagent.Run, calls *int) func(rl.ModelIdentity, model.Model) error {
	return func(identity rl.ModelIdentity, next model.Model) error {
		*calls++
		outcome, err := run.Publish(ctx, scenario10Decl(identity.Name()), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: next},
		})
		if err != nil {
			return err
		}
		if outcome.Result != state.PublishApplied {
			return fmt.Errorf("the publication of %q ended as %v", identity.Name(), outcome.Result)
		}
		return nil
	}
}

// scenario10Stored reads the sealed record file of the capture back and reports
// the episode records, the attempt records and the count of every record line,
// so the seal account is reconciled against the very bytes on disk.
func scenario10Stored(t *testing.T, dir string) ([]rl.EpisodeRecord, []rl.AttemptRecord, int) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the stored records: %v", err)
	}
	var episodes []rl.EpisodeRecord
	var attempts []rl.AttemptRecord
	records := 0
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse stored record %q: %v", raw, err)
		}
		records++
		switch {
		case line.Kind == rl.RecordKindEpisode && line.Episode != nil:
			episodes = append(episodes, *line.Episode)
		case line.Kind == rl.RecordKindAttempt && line.Attempt != nil:
			attempts = append(attempts, *line.Attempt)
		}
	}
	return episodes, attempts, records
}

// scenario10Grouped reports whether the samples are exactly the four turns of
// the two episodes in chain order: episode A's two turns keep one numbering
// across its two sessions, episode B restarts its own, and each row pairs the
// input of a turn with the answer of the version that served it.
func scenario10Grouped(samples []rl.SFTSample) bool {
	want := []rl.SFTSample{
		{SessionID: "session-s10-a", Turn: 1, Input: "question a one", Label: "version one answer", EpisodeID: string(scenario10EpisodeA)},
		{SessionID: "session-s10-b", Turn: 2, Input: "question a two", Label: "version one answer", EpisodeID: string(scenario10EpisodeA)},
		{SessionID: "session-s10-c", Turn: 1, Input: "question b one", Label: "version one answer", EpisodeID: string(scenario10EpisodeB)},
		{SessionID: "session-s10-d", Turn: 2, Input: "question b two", Label: "version two answer", EpisodeID: string(scenario10EpisodeB)},
	}
	if len(samples) != len(want) {
		return false
	}
	for i := range want {
		if samples[i] != want[i] {
			return false
		}
	}
	return true
}

// scenario10NotLeaked reports whether every row pairs its own input with the
// answer of its own turn and keeps the episode of its own session, and whether
// every stored episode record spans only the sessions of that episode: the
// episode a session shares is never read as the episode of another session.
func scenario10NotLeaked(samples []rl.SFTSample, episodes []rl.EpisodeRecord) bool {
	labels := map[string]string{
		"question a one": "version one answer",
		"question a two": "version one answer",
		"question b one": "version one answer",
		"question b two": "version two answer",
	}
	owners := map[event.SessionID]event.EpisodeID{
		"session-s10-a": scenario10EpisodeA,
		"session-s10-b": scenario10EpisodeA,
		"session-s10-c": scenario10EpisodeB,
		"session-s10-d": scenario10EpisodeB,
	}
	if len(samples) != len(labels) {
		return false
	}
	for _, sample := range samples {
		if labels[sample.Input] != sample.Label {
			return false
		}
		owner, held := owners[event.SessionID(sample.SessionID)]
		if !held || owner != event.EpisodeID(sample.EpisodeID) {
			return false
		}
	}
	spans := map[event.EpisodeID]map[event.SessionID]bool{
		scenario10EpisodeA: {"session-s10-a": true, "session-s10-b": true},
		scenario10EpisodeB: {"session-s10-c": true, "session-s10-d": true},
	}
	seen := map[event.EpisodeID]bool{}
	for _, episode := range episodes {
		allowed, held := spans[episode.ID]
		if !held {
			return false
		}
		seen[episode.ID] = true
		for _, session := range episode.Sessions {
			if !allowed[session] {
				return false
			}
		}
	}
	return seen[scenario10EpisodeA] && seen[scenario10EpisodeB]
}

// scenario10Emit reports the scenario record as one marker-prefixed json line
// on the test log, carrying only the observations the drive derived.
func scenario10Emit(t *testing.T, observations map[string]any, refs []string) {
	t.Helper()
	record := struct {
		ScenarioID   string         `json:"scenario_id"`
		TestPackage  string         `json:"test_package"`
		TestName     string         `json:"test_name"`
		EventRefs    []string       `json:"event_refs"`
		Observations map[string]any `json:"observations"`
	}{
		ScenarioID:   "S10",
		TestPackage:  "tests/integration/learning",
		TestName:     "TestScenario10EpisodeObservationAndReplacement",
		EventRefs:    append([]string{}, refs...),
		Observations: observations,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode the observation record of S10: %v", err)
	}
	t.Logf("%s%s", scenario10RecordMarker, encoded)
}
