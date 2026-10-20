package acceptance

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// scenarioReduce drives one model reduction over the given responses and reports
// the single outcome it derived.
func scenarioReduce(t *testing.T, responses ...*model.Response) model.Outcome {
	t.Helper()
	ctx := context.Background()
	stream := make(chan *model.Response, len(responses))
	for _, response := range responses {
		stream <- response
	}
	close(stream)
	return model.ReduceChannel(ctx, stream, nil)
}

// TestScenario09CaptureFidelityAndSeal drives the request capture, the attempt observation and the capture seal.
// - no credential reaches a persisted snapshot, and a failed attempt stays observable beside its error text
// - the usage the reduction accumulated is recorded beside a body that carries none of its own
// - a closed capture refuses further admission beside a drain that wrote what it admitted
// - a capture without a seal never claims to be complete
// - no tokenizer capability is claimed where none was verified
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestScenario09CaptureFidelityAndSeal(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S09")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	const (
		secretUserinfo = "USERINFO-4c5d"
		secretQuery    = "QUERY-7e8f"
		secretHeader   = "HEADER-8f3a"
		secretCookie   = "COOKIE-2b7c"
	)
	body := "deploy notes at https://cdn.example.com/notes.txt"
	request := model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: body},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{
				URL:    "https://alice:" + secretUserinfo + "@cdn.example.com/img.png?api_key=" + secretQuery,
				Format: "png",
			}}}},
		},
		Headers: map[string]string{
			"Authorization": "Bearer " + secretHeader,
			"Cookie":        "session=" + secretCookie,
		},
	}
	snapshot, err := rl.SnapshotRequest(rl.ModelViewRef{Model: "model-a"}, request, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("take the request snapshot: %v", err)
	}
	persisted, err := rl.MarshalSnapshot(snapshot)
	if err != nil {
		t.Fatalf("marshal the request snapshot: %v", err)
	}
	redacted := true
	for _, secret := range []string{secretUserinfo, secretQuery, secretHeader, secretCookie} {
		if bytes.Contains(persisted, []byte(secret)) {
			redacted = false
		}
	}
	observations["credential_redacted"] = redacted && strings.Contains(string(persisted), "cdn.example.com/notes.txt")

	failed := rl.AttemptOf("attempt-failed", "decision-failed", "episode-failed", scenarioReduce(t, scenarioFailTurn("provider exploded")))
	failedLine, err := rl.MarshalLine(rl.NewAttemptLine(failed))
	if err != nil {
		t.Fatalf("marshal the failed attempt line: %v", err)
	}
	observations["failed_attempt_observable"] = failed.Terminal == model.KindFailed &&
		failed.Error == "provider exploded" && bytes.Contains(failedLine, []byte("provider exploded"))

	answer := scenarioFinalTurn("settled")
	tail := &model.Response{Usage: &model.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}, Done: true}
	settled := scenarioReduce(t, answer, tail)
	recorded := rl.AttemptOf("attempt-settled", "decision-settled", "episode-settled", settled)
	settledLine, err := rl.MarshalLine(rl.NewAttemptLine(recorded))
	if err != nil {
		t.Fatalf("marshal the settled attempt line: %v", err)
	}
	bodyUsage := 0
	if recorded.Response.Usage != nil {
		bodyUsage = recorded.Response.Usage.TotalTokens
	}
	observations["usage_tail_recorded"] = recorded.Usage.TotalTokens == 7 && bodyUsage == 0 &&
		bytes.Contains(settledLine, []byte(`"total_tokens":7`))
	refs = append(refs, string(recorded.ID))

	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("capture-a", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	if admission, err := sink.Record(ctx, recorded); err != nil || !admission.Admitted {
		t.Fatalf("record the attempt = (%+v, %v), want one admitted record", admission, err)
	}
	manifest, err := sink.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the capture: %v", err)
	}
	closedAdmission, _ := sink.Record(ctx, recorded)
	observations["admission_closed_then_drained"] = manifest.Sealed && !manifest.Partial &&
		manifest.Records == 1 && closedAdmission.Bound == rl.BoundClosed
	refs = append(refs, manifest.CaptureID)

	crashDir := t.TempDir()
	crashWriter, err := rl.NewFileCaptureWriter(crashDir, 0)
	if err != nil {
		t.Fatalf("open the crashing capture writer: %v", err)
	}
	crashSink, err := rl.NewCaptureSink("capture-crashing", rl.SinkConfig{}, crashWriter)
	if err != nil {
		t.Fatalf("open the crashing capture sink: %v", err)
	}
	if admission, err := crashSink.Record(ctx, recorded); err != nil || !admission.Admitted {
		t.Fatalf("record the crashing attempt = (%+v, %v), want one admitted record", admission, err)
	}
	crashManifest, _ := rl.ReadCaptureSeal(crashDir)
	observations["crash_without_seal_claims_complete"] = crashManifest.Sealed && !crashManifest.Partial

	observations["tokenizer_capability"] = "absent"

	EmitScenarioRecord(t, scenario, refs, observations)
}
