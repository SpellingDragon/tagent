package acceptance

import (
	"bytes"
	"context"
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
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// scenarioS10GateEnv authorizes the one resource scenario: the case drives a real
// capture, a real export and a real SFT batch, so without an authorization it
// reports the scenario as the outstanding debit the contract already names
// instead of standing a deterministic fixture in for the resource.
const scenarioS10GateEnv = "TAGENT_SCENARIO_S10_RESOURCE"

// scenarioS10CaptureID, scenarioS10EpisodeA and scenarioS10EpisodeB are the
// capture and episode identities the resource scenario drives: one episode spans
// two sessions, so a split that followed a session instead of an episode would be
// observable.
const (
	scenarioS10CaptureID = "capture-a"
	scenarioS10EpisodeA  = event.EpisodeID("episode-a")
	scenarioS10EpisodeB  = event.EpisodeID("episode-b")
)

// scenarioS10VocabBound is the exclusive upper bound of the ids the deterministic
// encoder of the scenario mints (one marker per message below 1024, then one id
// per byte), so a label outside it is a value no encoder could have produced.
const scenarioS10VocabBound = 2048

// scenarioS10Inputs are the four inputs the scenario drives, one per invocation,
// so no two cleaned conversations carry the same body.
var scenarioS10Inputs = []string{
	"跨 session 场景在第一个 session 的输入。",
	"跨 session 场景在第二个 session 的输入。",
	"替换期间仍在途的那次请求的输入。",
	"替换之后开始的请求的输入。",
}

// TestScenario10EpisodeSplitAndModelReplacement drives one real training chain over the resource gate.
// - the split follows causal episode groups: one shared history never lands on opposite sides
// - an episode spanning two sessions is not split by session
// - the sealed capture exports real samples, and cleaning keeps each completed attempt beside its gap
// - the consumer tokenizes the samples into aligned id, label and loss planes whose mask matches the labels
// - the feedback face derives a versioned reward, and a later conclusion takes the next revision
// - a failed rebuild publishes no binding, an applied one publishes exactly one
// - a request in flight keeps the version it began on, and a later request binds the installed one
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestScenario10EpisodeSplitAndModelReplacement(t *testing.T) {
	if os.Getenv(scenarioS10GateEnv) == "" {
		t.Skipf("S10 stays remaining-required: %s is unset, so no authorized real SFT batch was driven", scenarioS10GateEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scenario, err := ScenarioOf("S10")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}

	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink(scenarioS10CaptureID, rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	running := newScenarioS10Model("model-a", "版本一的回答。")
	lease, err := rl.NewModelLease(running, "v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	run := scenarioS10Run(t, ctx, lease.Current(), lease.CurrentVersion(), sink)

	scenarioS10Invoke(t, ctx, run, "inv-s10-a", "session-s10-a", scenarioS10EpisodeA, scenarioS10Inputs[0])
	scenarioS10Invoke(t, ctx, run, "inv-s10-b", "session-s10-b", scenarioS10EpisodeA, scenarioS10Inputs[1])
	holding := make(chan struct{})
	running.holdUntil(holding)
	inflight := make(chan error, 1)
	go func() {
		inflight <- scenarioS10InvokeErr(ctx, run, "inv-s10-inflight", "session-s10-c", scenarioS10EpisodeB, scenarioS10Inputs[2])
	}()
	scenarioS10WaitFor(t, "the request in flight", func() bool { return lease.InFlight() == 1 })

	published := 0
	publisher := func(identity rl.ModelIdentity, next model.Model) error {
		published++
		outcome, err := run.Publish(ctx, scenarioS10Decl(identity.Name()), map[event.AgentID]tagent.NativeAgent{
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
	refused, refusedErr := lease.Replace(ctx, "v2", func() (model.Model, error) {
		return nil, errors.New("the rebuild of the second version failed")
	}, publisher)
	if !errors.Is(refusedErr, rl.ErrLeaseRebuildFailed) {
		t.Fatalf("replace the model with a failing rebuild = (%+v, %v), want the named rebuild refusal", refused, refusedErr)
	}
	publishedByFailure := published
	installed := newScenarioS10Model("model-a", "版本二的回答。")
	applied, err := lease.Replace(ctx, "v2", func() (model.Model, error) { return installed, nil }, publisher)
	if err != nil {
		t.Fatalf("replace the model with a rebuilt version: %v", err)
	}
	stillInFlight := lease.InFlight()
	close(holding)
	if err := <-inflight; err != nil {
		t.Fatalf("the request in flight: %v", err)
	}
	running.release()
	scenarioS10Invoke(t, ctx, run, "inv-s10-new", "session-s10-d", scenarioS10EpisodeB, scenarioS10Inputs[3])

	scenarioS10DeclareEpisodes(t, ctx, sink, scenarioS10StoredEpisodes(t, dir), scenarioS10EpisodeA, scenarioS10EpisodeB)

	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	attempts := scenarioS10Attempts(t, dir)
	servedByFirst, servedBySecond := 0, 0
	for _, attempt := range attempts {
		switch attempt.ModelVersion {
		case "v1":
			servedByFirst++
		case "v2":
			servedBySecond++
		}
	}

	source, err := rl.NewCaptureExportSource(dir)
	if err != nil {
		t.Fatalf("open the export source over the sealed capture: %v", err)
	}
	feedback := rl.NewFeedbackLedger()
	rewards := rl.NewRewardLedger()
	rule := scenarioS10RewardRule()
	exporter := rl.NewTrainingExporter(source, feedback, rewards, rule)
	exported, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{source.CaptureID()}})
	if err != nil || exported.Partial {
		t.Fatalf("export = (%+v, %v), want one complete run over the sealed capture", exported, err)
	}
	samples, held := exporter.Snapshot(exported.Manifest.ExportID)
	if !held || len(samples) != int(exported.Written) {
		t.Fatalf("snapshot holds %d sample(s) (held=%v) beside %d written", len(samples), held, exported.Written)
	}

	episodeOf := map[string]event.EpisodeID{}
	for _, sample := range samples {
		episodeOf[sample.Provenance.SampleID] = sample.Provenance.EpisodeID
	}
	real, err := source.Episodes(ctx)
	if err != nil {
		t.Fatalf("read the episode records of the sealed capture: %v", err)
	}

	prompts := make([]rl.SFTPrompt, 0, len(run.RequestSnapshots()))
	for _, snapshot := range run.RequestSnapshots() {
		prompts = append(prompts, rl.SFTPrompt{DecisionID: snapshot.Decision, Messages: snapshot.Snapshot.Messages})
	}
	cleaned := rl.CleanSFT(rl.SFTCleanRequest{
		Samples:  samples,
		Prompts:  prompts,
		Episodes: real,
		Split:    rl.SplitDeclaration{SplitSeed: 7, TrainRatio: 0.9},
	})
	if cleaned.Kept == 0 {
		t.Fatalf("cleaning kept no sample of the sealed capture: %+v", cleaned.Ledger)
	}
	consumed := rl.ConsumeSFT(rl.SFTConsumerRequest{
		Examples:  cleaned.Examples,
		Tokenizer: scenarioS10Tokenizer(),
		PaddingID: 0,
	})
	if consumed.Accepted() != len(cleaned.Examples) || len(consumed.Batches) != 1 {
		t.Fatalf("consumer accepted %d of %d example(s) in %d batch(es): %+v",
			consumed.Accepted(), len(cleaned.Examples), len(consumed.Batches), consumed.Ledger())
	}
	batch := consumed.Batches[0]

	firstReward, err := scenarioS10Bind(ctx, source, feedback, rewards, rule, samples, consumed)
	if err != nil {
		t.Fatalf("bind the first conclusion: %v", err)
	}
	secondReward, err := scenarioS10Bind(ctx, source, feedback, rewards, rule, samples, consumed)
	if err != nil {
		t.Fatalf("bind the second conclusion: %v", err)
	}

	observations := map[string]any{}
	observations["sft_samples_nonempty"] = len(samples) > 0 &&
		exported.Written == int64(len(samples)) &&
		exported.Manifest.Complete
	observations["sft_shape_aligned"] = scenarioS10ShapeAligned(t, cleaned, consumed)
	observations["sft_labels_shape_nonempty"] = scenarioS10LabelShape(batch)
	observations["sft_labels_values_well_formed"] = scenarioS10LabelValues(batch)
	observations["action_mask_shape_nonempty"] = scenarioS10MaskShape(batch)
	observations["action_mask_matches_labels"] = scenarioS10MaskMatchesLabels(batch)
	grouped, leakedFree := scenarioS10Split(t, real, cleaned, episodeOf, scenarioS10EpisodeA, scenarioS10EpisodeB)
	observations["split_grouped_by_episode"] = grouped
	observations["shared_ancestor_not_leaked"] = leakedFree
	observations["reward_versioned"] = firstReward.State == rl.FeedbackBound &&
		firstReward.RewardVersion == rule.Version &&
		firstReward.RewardInstanceID != "" &&
		firstReward.RewardRevision >= 1 &&
		firstReward.ModelVersion == "v1" &&
		secondReward.RewardRevision == firstReward.RewardRevision+1 &&
		secondReward.RewardInstanceID != firstReward.RewardInstanceID
	observations["model_replacement_binding"] = map[string]any{
		"new_request":      scenarioS10BindingLabel(servedBySecond == 1 && lease.CurrentVersion() == "model-a@v2", "new_binding"),
		"inflight_request": scenarioS10BindingLabel(servedByFirst == 3 && stillInFlight == 1, "previous_binding"),
	}
	observations["model_replacement_publishes_on_success_only"] = refused.State == rl.ReplacementRefused &&
		refused.Reason != "" &&
		publishedByFailure == 0 &&
		applied.State == rl.ReplacementApplied &&
		applied.From == "model-a@v1" && applied.To == "model-a@v2" &&
		published == 1
	observations["pipeline_artifact_links"] = scenarioS10ArtifactLinks(t, manifest, exported, samples, consumed)

	EmitScenarioRecord(t, scenario, scenarioS10Refs(manifest, exported, consumed), observations)
}

// scenarioS10Refs reports the chain artifacts one record names: the capture, the
// export drawn from it, and the first consumer sample as the witness of the tail.
func scenarioS10Refs(manifest rl.CaptureManifest, exported rl.ExportResult, consumed rl.SFTConsumerResult) []string {
	refs := []string{manifest.CaptureID, exported.Manifest.ExportID}
	if len(consumed.Artifacts) > 0 {
		refs = append(refs, consumed.Artifacts[0].SampleID)
	}
	return refs
}

// scenarioS10Model is one scripted model version of the resource scenario: it
// answers every call with the content it is named with, and holds the calls it
// is told to hold until the drive releases them, so a replacement can be observed
// while a stream of the replaced version is still open.
type scenarioS10Model struct {
	mu      sync.Mutex
	name    string
	content string
	hold    <-chan struct{}
	calls   int
	closes  int
}

// newScenarioS10Model returns one scripted version answering with its content.
func newScenarioS10Model(name, content string) *scenarioS10Model {
	return &scenarioS10Model{name: name, content: content}
}

// holdUntil holds every call that starts while the channel is the held one.
func (m *scenarioS10Model) holdUntil(release <-chan struct{}) {
	m.mu.Lock()
	m.hold = release
	m.mu.Unlock()
}

// release stops holding the calls that start after it.
func (m *scenarioS10Model) release() {
	m.mu.Lock()
	m.hold = nil
	m.mu.Unlock()
}

// GenerateContent counts the call it serves and answers it at once or only after
// the hold was released.
func (m *scenarioS10Model) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	hold := m.hold
	m.mu.Unlock()
	stream := make(chan *model.Response, 1)
	if hold == nil {
		stream <- scenarioFinalTurn(m.content)
		close(stream)
		return stream, nil
	}
	go func() {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		stream <- scenarioFinalTurn(m.content)
		close(stream)
	}()
	return stream, nil
}

// Info reports the scripted version's descriptor.
func (m *scenarioS10Model) Info() model.Info {
	return model.Info{Name: m.name}
}

// Close counts the one release the lease takes on a retired version.
func (m *scenarioS10Model) Close() error {
	m.mu.Lock()
	m.closes++
	m.mu.Unlock()
	return nil
}

// scenarioS10Decl is the declaration of the single agent the scenario runs,
// referencing the model identity the caller names.
func scenarioS10Decl(modelRef string) *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: modelRef}},
	}
}

// scenarioS10Run assembles one runtime over the model handle and declaration
// reference the caller names, writing through the capture it is handed.
func scenarioS10Run(t *testing.T, ctx context.Context, handle model.Model, modelRef string, records rl.Recorder) *tagent.Run {
	t.Helper()
	run, err := tagent.Assemble(ctx, scenarioS10Decl(modelRef), map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: handle},
	}, tagent.WithGovernance(&scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}),
		tagent.WithRecorder(records))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// scenarioS10Invoke drives one invocation through the formal runtime entry over
// the episode boundary it names.
func scenarioS10Invoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation, session string, episode event.EpisodeID, input string) {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", invocation, err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn of %s ended as %v, want the completed turn of one decision", invocation, turn.Terminal)
	}
}

// scenarioS10InvokeErr drives one invocation off the test goroutine, so the drive
// can observe the lease while that request is still in flight.
func scenarioS10InvokeErr(ctx context.Context, run *tagent.Run, invocation, session string, episode event.EpisodeID, input string) error {
	_, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: event.InvocationID(invocation),
		Episode:      episode,
		Address:      event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)},
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	return err
}

// scenarioS10WaitFor waits until the condition holds or the wait runs out, so the
// drive never reads a half-settled lease.
func scenarioS10WaitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s did not settle within the wait", what)
}

// scenarioS10Attempts reads the attempt records one sealed capture holds, in
// stored order.
func scenarioS10Attempts(t *testing.T, dir string) []rl.AttemptRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the stored records: %v", err)
	}
	var out []rl.AttemptRecord
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse the stored record %q: %v", raw, err)
		}
		if line.Kind == rl.RecordKindAttempt && line.Attempt != nil {
			out = append(out, *line.Attempt)
		}
	}
	return out
}

// scenarioS10StoredEpisodes reads the episode records the capture holds so far,
// in stored order, so the declaration the scenario writes is built on the
// sessions the runtime itself recorded.
func scenarioS10StoredEpisodes(t *testing.T, dir string) []rl.EpisodeRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the stored records: %v", err)
	}
	var out []rl.EpisodeRecord
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse the stored record %q: %v", raw, err)
		}
		if line.Kind == rl.RecordKindEpisode && line.Episode != nil {
			out = append(out, *line.Episode)
		}
	}
	return out
}

// scenarioS10DeclareEpisodes merges the stored episode records by identity — the
// sessions the same episode was driven on are one episode, not one per session —
// and declares the shared history the two episodes read through the recorder
// face, so both the export and the cleaning pass draw one causal group.
func scenarioS10DeclareEpisodes(t *testing.T, ctx context.Context, sink *rl.CaptureSink, stored []rl.EpisodeRecord, parent, child event.EpisodeID) {
	t.Helper()
	merged := map[event.EpisodeID]*rl.EpisodeRecord{}
	order := make([]event.EpisodeID, 0, len(stored))
	for _, record := range stored {
		held := merged[record.ID]
		if held == nil {
			copied := record
			copied.Schema = rl.SchemaCurrent
			copied.Sessions = append([]event.SessionID(nil), record.Sessions...)
			merged[record.ID] = &copied
			order = append(order, record.ID)
			continue
		}
		for _, session := range record.Sessions {
			known := false
			for _, seen := range held.Sessions {
				if seen == session {
					known = true
				}
			}
			if !known {
				held.Sessions = append(held.Sessions, session)
			}
		}
	}
	for _, id := range []event.EpisodeID{parent, child} {
		if _, held := merged[id]; !held {
			t.Fatalf("the stored episode records name no %q", id)
		}
	}
	for _, id := range order {
		record := *merged[id]
		record.Lineage = rl.LineageKnown
		if id == child {
			record.RootID = parent
			record.DependsOn = []rl.EpisodeDependency{{
				EpisodeID: id,
				DependsOn: parent,
				Kind:      rl.DependencySharedHistory,
				Lineage:   rl.LineageKnown,
			}}
		}
		if err := sink.RecordEpisode(ctx, record); err != nil {
			t.Fatalf("declare the episode %q: %v", id, err)
		}
	}
}

// scenarioS10RewardRule is the derivation rule the scenario binds a conclusion
// under: one point per positive conclusion, one off per negative, under one
// pinned rule version.
func scenarioS10RewardRule() rl.RewardRule {
	return rl.RewardRule{
		Version: "reward.rule.v1",
		Value: func(evidence []rl.FeedbackRecord) (float64, error) {
			total := 0.0
			for _, record := range evidence {
				if record.Conclusion == "good" {
					total += 1
				} else {
					total -= 1
				}
			}
			return total, nil
		},
	}
}

// scenarioS10Bind submits one conclusion against the witness attempt of the
// chain through the feedback face and reports the bound submission.
func scenarioS10Bind(ctx context.Context, source *rl.CaptureExportSource, feedback *rl.FeedbackLedger, rewards *rl.RewardLedger, rule rl.RewardRule, samples []rl.SampleLine, consumed rl.SFTConsumerResult) (rl.FeedbackSubmission, error) {
	if len(samples) == 0 || len(consumed.Artifacts) == 0 {
		return rl.FeedbackSubmission{}, errors.New("the chain holds no sample to bind a conclusion to")
	}
	witness := consumed.Artifacts[0].SampleID
	var attempt event.AttemptID
	for _, sample := range samples {
		if sample.Provenance.SampleID == witness {
			attempt = sample.Provenance.AttemptID
		}
	}
	if attempt == "" {
		return rl.FeedbackSubmission{}, errors.New("the witness sample names no attempt")
	}
	facts, err := source.Page(ctx, source.CaptureID(), 0, 128)
	if err != nil {
		return rl.FeedbackSubmission{}, err
	}
	endpoint, err := rl.NewFeedbackEndpoint(rl.NewCaptureFeedbackResolver(source), feedback, rewards, rule)
	if err != nil {
		return rl.FeedbackSubmission{}, err
	}
	for _, fact := range facts {
		if fact.AttemptID != attempt || fact.EventKey == "" {
			continue
		}
		conclusion := "good"
		if len(feedback.All()) > 0 {
			conclusion = "poor"
		}
		return endpoint.Submit(ctx, rl.FeedbackPayload{
			EventKey:   string(fact.EventKey),
			DecisionID: fact.DecisionID,
			AttemptID:  fact.AttemptID,
			Conclusion: conclusion,
			Source:     "human",
		})
	}
	return rl.FeedbackSubmission{}, fmt.Errorf("the capture records no event key for the attempt %q", attempt)
}

// scenarioS10ShapeAligned reports whether every cleaned example counts its mask
// sides over exactly its messages and every artifact carries one plane entry per
// token it produced.
func scenarioS10ShapeAligned(t *testing.T, cleaned rl.SFTCleanResult, consumed rl.SFTConsumerResult) bool {
	t.Helper()
	if len(cleaned.Examples) == 0 || len(consumed.Artifacts) != len(cleaned.Examples) {
		return false
	}
	for _, example := range cleaned.Examples {
		if example.Actions < 1 || example.Observations < 1 {
			return false
		}
		if example.Actions+example.Observations != len(example.Messages) {
			return false
		}
	}
	for _, artifact := range consumed.Artifacts {
		tokens := len(artifact.Sequence.InputIDs)
		if tokens == 0 ||
			len(artifact.Sequence.AttentionMask) != tokens ||
			len(artifact.Sequence.LossMask) != tokens ||
			len(artifact.Sequence.Labels) != tokens ||
			len(artifact.Sequence.Attribution.MessageOf) != tokens ||
			len(artifact.Sequence.Attribution.MessageMask) == 0 {
			return false
		}
	}
	return true
}

// scenarioS10LabelShape reports whether the batch carries one non-empty label row
// per accepted sample, each as wide as the ids and the loss mask of its row.
func scenarioS10LabelShape(batch rl.SFTBatch) bool {
	planes := batch.Planes
	if len(planes.Labels) == 0 ||
		len(planes.Labels) != len(planes.InputIDs) ||
		len(planes.Labels) != len(planes.LossMask) {
		return false
	}
	for row, labels := range planes.Labels {
		if len(labels) == 0 || len(labels) != len(planes.InputIDs[row]) || len(labels) != len(planes.LossMask[row]) {
			return false
		}
	}
	return true
}

// scenarioS10LabelValues reports whether every label is either the ignore index
// or an id the scenario's encoder could have produced, beside at least one
// trained position.
func scenarioS10LabelValues(batch rl.SFTBatch) bool {
	trained := 0
	for _, labels := range batch.Planes.Labels {
		for _, label := range labels {
			if label == rl.SFTLabelIgnoreIndex {
				continue
			}
			trained++
			if label < 0 || label >= scenarioS10VocabBound {
				return false
			}
		}
	}
	return trained > 0
}

// scenarioS10MaskShape reports whether the loss plane carries one non-empty row
// per label row, each as wide as it and made of the two loss positions only.
func scenarioS10MaskShape(batch rl.SFTBatch) bool {
	planes := batch.Planes
	if len(planes.LossMask) == 0 || len(planes.LossMask) != len(planes.Labels) {
		return false
	}
	for row, mask := range planes.LossMask {
		if len(mask) == 0 || len(mask) != len(planes.Labels[row]) {
			return false
		}
		trained := 0
		for _, position := range mask {
			switch position {
			case 0:
			case 1:
				trained++
			default:
				return false
			}
		}
		if trained == 0 {
			return false
		}
	}
	return true
}

// scenarioS10MaskMatchesLabels reports whether the loss plane trains on exactly
// the positions whose label a loss reads.
func scenarioS10MaskMatchesLabels(batch rl.SFTBatch) bool {
	planes := batch.Planes
	if len(planes.LossMask) != len(planes.Labels) {
		return false
	}
	for row, labels := range planes.Labels {
		if len(planes.LossMask[row]) != len(labels) {
			return false
		}
		for position, label := range labels {
			trained := label != rl.SFTLabelIgnoreIndex
			if (planes.LossMask[row][position] == 1) != trained {
				return false
			}
		}
	}
	return true
}

// scenarioS10Split reports whether the split follows causal episode groups and
// whether two episodes of one shared history stay on one side: a group never
// crosses the train and test sides, an episode spanning sessions is not split by
// session, and both episodes contribute examples to the one group they share.
func scenarioS10Split(t *testing.T, episodes []rl.EpisodeRecord, cleaned rl.SFTCleanResult, episodeOf map[string]event.EpisodeID, first, second event.EpisodeID) (grouped, leakedFree bool) {
	t.Helper()
	if len(cleaned.Examples) == 0 {
		return false, false
	}
	sides := map[string]string{}
	perEpisode := map[event.EpisodeID]int{}
	grouped = true
	for _, example := range cleaned.Examples {
		episode, held := episodeOf[example.SampleID]
		if !held {
			grouped = false
			continue
		}
		group := rl.CausalGroupOf(episodes, episode)
		if example.GroupID != group.GroupID {
			grouped = false
		}
		if previous, seen := sides[group.GroupID]; seen && previous != example.Split {
			grouped = false
		}
		sides[group.GroupID] = example.Split
		perEpisode[episode]++
	}
	grouped = grouped && len(sides) == len(cleaned.Splits) && len(sides) == 1

	shared := rl.CausalGroupOf(episodes, first)
	descendant := rl.CausalGroupOf(episodes, second)
	holds := func(group rl.EpisodeGroup, id event.EpisodeID) bool {
		for _, member := range group.Members {
			if member == id {
				return true
			}
		}
		return false
	}
	leakedFree = grouped &&
		shared.GroupID == descendant.GroupID &&
		holds(shared, first) && holds(shared, second) &&
		shared.RootID == first &&
		perEpisode[first] > 0 && perEpisode[second] > 0 &&
		scenarioS10SessionCount(episodes, first) == 2 &&
		scenarioS10SessionCount(episodes, second) == 2
	return grouped, leakedFree
}

// scenarioS10SessionCount reports how many sessions the records of one episode
// name, so an episode that spans sessions is observable.
func scenarioS10SessionCount(episodes []rl.EpisodeRecord, id event.EpisodeID) int {
	sessions := map[event.SessionID]bool{}
	for _, record := range episodes {
		if record.ID != id {
			continue
		}
		for _, session := range record.Sessions {
			sessions[session] = true
		}
	}
	return len(sessions)
}

// scenarioS10ArtifactLinks reports the artifact relation of the chain as the
// labels the evidence contract carries: the capture and the episode keep the
// identity the chain declared for them, and the seal, the export, the sample and
// the consumer input — each carrying a counter or a time — are labelled by the
// order the chain produced them in, so the relation stays one-to-one without
// freezing a generated identity into the report.
func scenarioS10ArtifactLinks(t *testing.T, manifest rl.CaptureManifest, exported rl.ExportResult, samples []rl.SampleLine, consumed rl.SFTConsumerResult) map[string]any {
	t.Helper()
	if len(consumed.Artifacts) == 0 {
		return map[string]any{}
	}
	witness := consumed.Artifacts[0].SampleID
	var sample rl.SampleLine
	found := false
	for _, held := range samples {
		if held.Provenance.SampleID == witness {
			sample = held
			found = true
			break
		}
	}
	linked := found &&
		manifest.Sealed && !manifest.Partial &&
		manifest.CaptureID == sample.Provenance.CaptureID &&
		exported.Manifest.ExportID == sample.Provenance.ExportID
	if !linked {
		return map[string]any{}
	}
	labels := newScenarioLabels()
	capture := labels.label("capture", sample.Provenance.CaptureID)
	export := labels.label("export", sample.Provenance.ExportID)
	return map[string]any{
		"capture": capture,
		"seal":    labels.label("seal", manifest.CaptureID),
		"export":  export,
		"samples": []any{map[string]any{
			"sample":         labels.label("sample", sample.Provenance.SampleID),
			"episode":        labels.label("episode", string(sample.Provenance.EpisodeID)),
			"capture":        capture,
			"export":         export,
			"consumer_input": labels.label("consumer-input", witness),
		}},
	}
}

// scenarioS10BindingLabel reports the label of one observed binding, so a
// binding the drive did not observe is reported as unobserved instead of being
// filled in from the expectation.
func scenarioS10BindingLabel(observed bool, label string) string {
	if observed {
		return label
	}
	return "unobserved"
}

// scenarioS10EncoderIdentity names the encoder the resource scenario reads its
// samples through: it stands on no tokenizer.
var scenarioS10EncoderIdentity = rl.SFTEncoderIdentity{
	Provenance:   rl.SFTEncoderDeterministic,
	Tokenizer:    "utf8-byte-marker",
	TemplateHash: "markers:v1",
}

// scenarioS10Tokenizer tokenizes through UTF-8 bytes framed by one marker per
// message, so every token carries the message it came from.
func scenarioS10Tokenizer() rl.SFTSourceTokenizer {
	return rl.SFTSourceTokenizer{Identity: scenarioS10EncoderIdentity, Encode: scenarioS10Encode}
}

// scenarioS10Encode renders one conversation as message markers around the UTF-8
// bytes of every message.
func scenarioS10Encode(example rl.SFTExample) (rl.SFTTokenization, error) {
	tokenization := rl.SFTTokenization{}
	emit := func(token, message int) {
		tokenization.InputIDs = append(tokenization.InputIDs, token)
		tokenization.MessageOf = append(tokenization.MessageOf, message)
	}
	for index, message := range example.Messages {
		emit(1000+len(message.Message.Role), rl.SFTScaffoldToken)
		for _, content := range []byte(scenarioS10MessageText(message.Message)) {
			emit(int(content), index)
		}
		emit(1000, rl.SFTScaffoldToken)
	}
	if len(tokenization.InputIDs) == 0 {
		return rl.SFTTokenization{}, fmt.Errorf("the conversation of %q carries no token", example.SampleID)
	}
	return tokenization, nil
}

// scenarioS10MessageText reports what one message contributes to a tokenization:
// its content, the tool it answers and the calls it opens.
func scenarioS10MessageText(message model.Message) string {
	text := message.Content
	if message.ToolID != "" {
		text += "|tool=" + message.ToolID
	}
	for _, call := range message.ToolCalls {
		text += "|call=" + call.Function.Name + "|args=" + string(call.Function.Arguments)
	}
	return text
}
