package learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"
)

// TestSFTCleaningContract drives exported training samples through the cleaning contract of the SFT dataset.
//   - a completed attempt beside its fixed prompt becomes one masked example and a decision sample is refused by name
//   - every message carries the mask side of its role and the completion is the last action
//   - an illegal sequence, an empty completion, a failed attempt, a v2 sample and a missing prompt carry their own gaps
//   - the training-use matrix decides SFT, DPO and sampled RL on their own evidence
//   - the split follows causal episode groups and a declared dependency holds two episodes of different sessions together
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTCleaningContract(t *testing.T) {
	t.Run("the formal chain converts strictly and masks actions apart from observations", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fixture := sftFixtureOf(t, ctx, []sftInvocation{{invocation: "inv-sft-1", session: "session-sft-1", episode: "ep-sft-1"}})
		before := sampleDigests(fixture.samples)
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  fixture.samples,
			Prompts:  fixture.prompts,
			Episodes: fixture.episodes,
			Split:    rl.SplitDeclaration{SplitSeed: 7, TrainRatio: 0.9},
		})

		if len(result.Records) != len(fixture.samples) {
			t.Fatalf("cleaning recorded %d samples beside %d input samples, want one record per sample and no silent drop", len(result.Records), len(fixture.samples))
		}
		if result.Kept != 2 || result.Rejected != 2 {
			t.Fatalf("cleaning kept %d and refused %d, want the two attempts kept and the two decisions refused: %+v", result.Kept, result.Rejected, result.Records)
		}
		if len(result.Examples) != 2 || len(result.Splits) != 1 {
			t.Fatalf("examples = %d beside %d splits, want the two attempts in one causal group", len(result.Examples), len(result.Splits))
		}
		for _, record := range result.Records {
			if record.Kept {
				if record.Gap.Kind != "" || record.Actions < 1 || record.Observations < 1 {
					t.Fatalf("kept record %q = (gap %q, %d actions, %d observations), want no gap and both mask sides", record.SampleID, record.Gap.Kind, record.Actions, record.Observations)
				}
				continue
			}
			if record.Gap.Kind != rl.SFTGapNotAnAttempt || record.Gap.Reason == "" {
				t.Fatalf("refused record %q = (%q, %q), want the named gap of a decision sample carrying no attempt", record.SampleID, record.Gap.Kind, record.Gap.Reason)
			}
		}
		if len(result.Ledger) != 1 || result.Ledger[0].Kind != rl.SFTGapNotAnAttempt || result.Ledger[0].Count != 2 {
			t.Fatalf("ledger = %+v, want one line naming the two refused decision samples", result.Ledger)
		}

		for _, example := range result.Examples {
			if example.Actions+example.Observations != len(example.Messages) {
				t.Fatalf("example %q counts %d actions beside %d observations and %d messages", example.SampleID, example.Actions, example.Observations, len(example.Messages))
			}
			if last := example.Messages[len(example.Messages)-1]; last.Mask != rl.SFTMaskAction || last.Message.Role != model.RoleAssistant {
				t.Fatalf("example %q ends on (%q, %q), want the observed completion as the last action", example.SampleID, last.Message.Role, last.Mask)
			}
			for i, message := range example.Messages {
				want := rl.SFTMaskObservation
				if message.Message.Role == model.RoleAssistant {
					want = rl.SFTMaskAction
				}
				if message.Mask != want {
					t.Fatalf("example %q message %d carries role %q and mask %q, want %q", example.SampleID, i, message.Message.Role, message.Mask, want)
				}
			}
			if example.Split != "train" && example.Split != "test" {
				t.Fatalf("example %q split = %q, want one of the two sides", example.SampleID, example.Split)
			}
			if example.SourceDigest == "" {
				t.Fatalf("example %q names no source digest, want the digest of the record it was read from", example.SampleID)
			}
		}

		assertActionObservationSequence(t, result.Examples)
		assertSamplesUntouched(t, fixture.samples, before, "the cleaning pass")
	})
}

// assertActionObservationSequence checks the two formal examples: the tool-call
// turn keeps the call as its last action with the answer left to the next
// request, and the final turn masks the request context around its two actions.
func assertActionObservationSequence(t *testing.T, examples []rl.SFTExample) {
	t.Helper()
	var callTurn, finalTurn *rl.SFTExample
	for i := range examples {
		switch {
		case examples[i].Actions == 1 && examples[i].Observations == 1:
			callTurn = &examples[i]
		case examples[i].Actions == 2 && examples[i].Observations == 3:
			finalTurn = &examples[i]
		}
	}
	if callTurn == nil || finalTurn == nil {
		t.Fatalf("cleaning kept %d examples, want the tool-call turn beside the final tool turn", len(examples))
	}
	lastCall := callTurn.Messages[len(callTurn.Messages)-1]
	if lastCall.Message.Role != model.RoleAssistant || lastCall.Mask != rl.SFTMaskAction || len(lastCall.Message.ToolCalls) != 1 {
		t.Fatalf("tool-call turn ends on (%q, %q, %d calls), want the observed tool call as the last action", lastCall.Message.Role, lastCall.Mask, len(lastCall.Message.ToolCalls))
	}
	for i, message := range callTurn.Messages {
		if message.Message.Role == model.RoleTool {
			t.Fatalf("tool-call turn message %d carries the tool result of a call the next request answers", i)
		}
	}
	want := []struct {
		role model.Role
		mask rl.SFTMask
	}{
		{model.RoleUser, rl.SFTMaskObservation},
		{model.RoleAssistant, rl.SFTMaskAction},
		{model.RoleTool, rl.SFTMaskObservation},
		{model.RoleUser, rl.SFTMaskObservation},
		{model.RoleAssistant, rl.SFTMaskAction},
	}
	if len(finalTurn.Messages) != len(want) {
		t.Fatalf("the final turn carries %d messages, want the %d of the formal request", len(finalTurn.Messages), len(want))
	}
	for i, expected := range want {
		message := finalTurn.Messages[i]
		if message.Message.Role != expected.role || message.Mask != expected.mask {
			t.Fatalf("final turn message %d = (%q, %q), want (%q, %q)", i, message.Message.Role, message.Mask, expected.role, expected.mask)
		}
	}
	calls := finalTurn.Messages[1].Message.ToolCalls
	answer := finalTurn.Messages[2].Message
	result := finalTurn.Messages[3].Message
	if len(calls) != 1 || answer.ToolID != calls[0].ID || result.ToolID != calls[0].ID || result.Content != "out-a" {
		t.Fatalf("final turn pairs the call %v with (%q, %q), want the tool result carried as context instead of an action", calls, answer.ToolID, result.Content)
	}
}

// sftInvocation names one invocation the fixture drives through the formal
// entry: its identity, the session it runs on and the episode boundary it names.
type sftInvocation struct {
	invocation event.InvocationID
	session    event.SessionID
	episode    event.EpisodeID
}

// sftFixture is one sealed capture exported under its own grant, beside the fixed
// prompts and episode records the cleaning pass reads.
type sftFixture struct {
	samples  []rl.SampleLine
	prompts  []rl.SFTPrompt
	episodes []rl.EpisodeRecord
}

// sftFixtureOf drives the formal runtime over one sealed capture, exports the
// capture under its own grant, and reads back the samples, the fixed prompts of
// the decisions and the episode records the export source holds.
func sftFixtureOf(t *testing.T, ctx context.Context, invocations []sftInvocation) sftFixture {
	t.Helper()
	dir := t.TempDir()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("build the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap-sft-clean", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("build the capture sink: %v", err)
	}
	run := formalRun(t, ctx, sink, exportAnswer)
	for _, invocation := range invocations {
		turn := formalInvoke(t, ctx, run, invocation.invocation, invocation.session, invocation.episode)
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn of %q = (%v, %d decisions), want the completed turn of two decisions", invocation.invocation, turn.Terminal, len(turn.Decisions))
		}
	}
	if _, err := sink.Seal(ctx); err != nil {
		t.Fatalf("seal the formal capture: %v", err)
	}
	src, err := rl.NewCaptureExportSource(dir)
	if err != nil {
		t.Fatalf("build the export source over the sealed capture: %v", err)
	}
	exporter := rl.NewTrainingExporter(src, rl.NewFeedbackLedger(), rl.NewRewardLedger(), exportTestRule())
	result, err := exporter.Export(ctx, rl.ExportRequest{PartitionIDs: []string{src.CaptureID()}})
	if err != nil || result.Partial {
		t.Fatalf("export = (%+v, %v), want one complete run over the sealed capture", result, err)
	}
	lines, held := exporter.Snapshot(result.Manifest.ExportID)
	if !held || len(lines) != int(result.Written) {
		t.Fatalf("snapshot holds %d samples (held=%v) beside %d written", len(lines), held, result.Written)
	}
	prompts := make([]rl.SFTPrompt, 0, len(invocations)*2)
	for _, snapshot := range run.RequestSnapshots() {
		prompts = append(prompts, rl.SFTPrompt{DecisionID: snapshot.Decision, Messages: snapshot.Snapshot.Messages})
	}
	episodes, err := src.Episodes(ctx)
	if err != nil {
		t.Fatalf("read the episode records of the sealed capture: %v", err)
	}
	return sftFixture{samples: lines, prompts: prompts, episodes: episodes}
}

// sampleDigests reports the digest of every sample body, so a pass that rewrites
// what it read is caught by comparing the digests before and after.
func sampleDigests(samples []rl.SampleLine) []string {
	digests := make([]string, 0, len(samples))
	for _, sample := range samples {
		sum := sha256.Sum256(sample.Fact.Body)
		digests = append(digests, hex.EncodeToString(sum[:]))
	}
	return digests
}

// assertSamplesUntouched checks that one pass rewrote no sample body it read.
func assertSamplesUntouched(t *testing.T, samples []rl.SampleLine, before []string, what string) {
	t.Helper()
	after := sampleDigests(samples)
	if len(after) != len(before) {
		t.Fatalf("%s changed the number of samples from %d to %d", what, len(before), len(after))
	}
	for i := range before {
		if after[i] != before[i] {
			t.Fatalf("%s rewrote sample %d: body digest %s became %s", what, i, before[i], after[i])
		}
	}
}

// attemptSampleOf builds one exported attempt sample from the records the test
// supplies, so a negative case is driven through the same public entry as the
// formal chain.
func attemptSampleOf(t *testing.T, attempt rl.AttemptRecord) rl.SampleLine {
	t.Helper()
	if attempt.Schema == "" {
		attempt.Schema = rl.SchemaCurrent
	}
	raw, err := rl.MarshalLine(rl.NewAttemptLine(attempt))
	if err != nil {
		t.Fatalf("marshal the attempt record: %v", err)
	}
	return rl.SampleLine{
		Schema: rl.SchemaCurrent,
		Provenance: rl.SampleProvenance{
			SampleID:   "sample:" + string(attempt.ID),
			EpisodeID:  attempt.EpisodeID,
			DecisionID: attempt.DecisionID,
			AttemptID:  attempt.ID,
			GroupID:    "group:" + string(attempt.EpisodeID),
		},
		Fact: rl.ExportedFact{
			EventKey:   event.EventID(attempt.ID),
			Partition:  "cap-sft-clean",
			CaptureID:  "cap-sft-clean",
			EpisodeID:  attempt.EpisodeID,
			DecisionID: attempt.DecisionID,
			AttemptID:  attempt.ID,
			Schema:     rl.SchemaCurrent,
			Timestamp:  attempt.Timestamp.UnixMilli(),
			Body:       raw,
		},
	}
}

// episodeOf builds the episode record one crafted sample belongs to, so its
// causal group is the group its own identity names.
func episodeOf(id event.EpisodeID, sessions ...event.SessionID) rl.EpisodeRecord {
	return rl.EpisodeRecord{Schema: rl.SchemaCurrent, ID: id, Sessions: sessions, Lineage: rl.LineageKnown}
}

// userPromptOf builds the fixed prompt of one decision: the user input the
// attempt answered.
func userPromptOf(decision event.DecisionID, content string) rl.SFTPrompt {
	return rl.SFTPrompt{DecisionID: decision, Messages: []model.Message{{Role: model.RoleUser, Content: content}}}
}

// completedAttemptOf builds one completed attempt carrying the supplied
// completion, under the named decision and episode.
func completedAttemptOf(id event.AttemptID, decision event.DecisionID, episode event.EpisodeID, content string) rl.AttemptRecord {
	return rl.AttemptRecord{
		Schema:     rl.SchemaCurrent,
		ID:         id,
		DecisionID: decision,
		EpisodeID:  episode,
		Terminal:   model.KindCompleted,
		Response:   model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}}},
		Timestamp:  time.UnixMilli(1),
	}
}

// unsupportedAttemptSampleOf builds one completed attempt whose provider
// declared logprobs unsupported, beside the declaration it rests on.
func unsupportedAttemptSampleOf(t *testing.T) rl.SampleLine {
	t.Helper()
	attempt := completedAttemptOf("att-unsupported", "dec-unsupported", "ep-unsupported", "settled")
	attempt.Logprobs = rl.LogprobsUnsupported
	attempt.LogprobsSource = "provider-capability"
	return attemptSampleOf(t, attempt)
}

// assertSingleGap checks that one cleaning run refused its only sample with the
// named gap and kept none.
func assertSingleGap(t *testing.T, result rl.SFTCleanResult, kind rl.SFTGapKind) rl.SFTCleanRecord {
	t.Helper()
	if result.Kept != 0 || len(result.Examples) != 0 || len(result.Records) != 1 {
		t.Fatalf("run kept %d of %d samples, want the single sample refused", len(result.Examples), len(result.Records))
	}
	record := result.Records[0]
	if record.Gap.Kind != kind || record.Gap.Reason == "" {
		t.Fatalf("refusal = (%q, %q), want the named gap %q with its reason", record.Gap.Kind, record.Gap.Reason, kind)
	}
	return record
}

// TestSFTCleaningRefusals checks that the cleaning contract refuses an illegal sample by name and never repairs it.
//   - an unanswered tool call, a tool result with no pending call and an unknown role are illegal messages
//   - an empty completion, a failed attempt, a v2 sample and a missing prompt carry their own named gaps
//   - every refusal is counted in the failure ledger
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTCleaningRefusals(t *testing.T) {
	episode := event.EpisodeID("ep-refusal")
	decision := event.DecisionID("dec-refusal")
	attempt := completedAttemptOf("att-refusal", decision, episode, "settled")
	base := attemptSampleOf(t, attempt)
	episodes := []rl.EpisodeRecord{episodeOf(episode, "session-refusal")}

	t.Run("an assistant message while a tool call is unanswered is illegal", func(t *testing.T) {
		prompt := rl.SFTPrompt{DecisionID: decision, Messages: []model.Message{
			{Role: model.RoleUser, Content: "run it"},
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{learnCall("call-open", "echo-a", `{"x":1}`)}},
		}}
		result := rl.CleanSFT(rl.SFTCleanRequest{Samples: []rl.SampleLine{base}, Prompts: []rl.SFTPrompt{prompt}, Episodes: episodes})
		record := assertSingleGap(t, result, rl.SFTGapIllegalMessage)
		if !contains(record.Gap.Reason, "unanswered") {
			t.Fatalf("refusal reason = %q, want the unanswered tool call named", record.Gap.Reason)
		}
	})

	t.Run("a tool call left open inside the conversation is illegal", func(t *testing.T) {
		err := rl.ValidateConversation([]model.Message{
			{Role: model.RoleUser, Content: "run both"},
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
				learnCall("call-a", "echo-a", `{"x":1}`),
				learnCall("call-b", "echo-a", `{"x":2}`),
			}},
			{Role: model.RoleTool, ToolID: "call-a", Content: "out-a"},
		})
		if err == nil || !contains(err.Error(), "never answered") {
			t.Fatalf("conversation error = %v, want the call opened beside its answer named as never answered", err)
		}
	})

	t.Run("a tool result with no pending call is illegal", func(t *testing.T) {
		prompt := rl.SFTPrompt{DecisionID: decision, Messages: []model.Message{
			{Role: model.RoleUser, Content: "run it"},
			{Role: model.RoleTool, ToolID: "call-absent", Content: "out-a"},
		}}
		result := rl.CleanSFT(rl.SFTCleanRequest{Samples: []rl.SampleLine{base}, Prompts: []rl.SFTPrompt{prompt}, Episodes: episodes})
		assertSingleGap(t, result, rl.SFTGapIllegalMessage)
	})

	t.Run("an unknown role is illegal", func(t *testing.T) {
		prompt := rl.SFTPrompt{DecisionID: decision, Messages: []model.Message{
			{Role: model.Role("wizard"), Content: "cast"},
		}}
		result := rl.CleanSFT(rl.SFTCleanRequest{Samples: []rl.SampleLine{base}, Prompts: []rl.SFTPrompt{prompt}, Episodes: episodes})
		assertSingleGap(t, result, rl.SFTGapIllegalMessage)
	})

	t.Run("an empty completion is refused instead of trained on", func(t *testing.T) {
		empty := completedAttemptOf("att-empty", decision, episode, "")
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{attemptSampleOf(t, empty)},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "run it")},
			Episodes: episodes,
		})
		assertSingleGap(t, result, rl.SFTGapEmptyCompletion)
	})

	t.Run("an attempt that did not complete is refused", func(t *testing.T) {
		failed := completedAttemptOf("att-failed", decision, episode, "settled")
		failed.Terminal = model.KindFailed
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{attemptSampleOf(t, failed)},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "run it")},
			Episodes: episodes,
		})
		assertSingleGap(t, result, rl.SFTGapTerminalNotCompleted)
	})

	t.Run("a legacy sample is routed to the read-only converter", func(t *testing.T) {
		legacy := base
		legacy.Schema = rl.SchemaLegacyV2
		bodyBefore := append([]byte(nil), legacy.Fact.Body...)
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{legacy},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "run it")},
			Episodes: episodes,
		})
		record := assertSingleGap(t, result, rl.SFTGapLegacyReadOnly)
		if !contains(record.Gap.Reason, rl.SFTLegacyConverterV2) {
			t.Fatalf("refusal reason = %q, want the read-only converter %q named", record.Gap.Reason, rl.SFTLegacyConverterV2)
		}
		if !bytes.Equal(bodyBefore, legacy.Fact.Body) {
			t.Fatal("the legacy refusal rewrote the sample body it read")
		}
	})

	t.Run("a sample with no fixed prompt is refused", func(t *testing.T) {
		result := rl.CleanSFT(rl.SFTCleanRequest{Samples: []rl.SampleLine{base}, Episodes: episodes})
		assertSingleGap(t, result, rl.SFTGapPromptMissing)
	})

	t.Run("a group that differs from its causal group is refused", func(t *testing.T) {
		mismatched := base
		mismatched.Provenance.GroupID = "group:another-episode"
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{mismatched},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "run it")},
			Episodes: episodes,
		})
		assertSingleGap(t, result, rl.SFTGapGroupMismatch)
	})

	t.Run("every refusal is counted in the failure ledger", func(t *testing.T) {
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{base, attemptSampleOf(t, completedAttemptOf("att-empty-2", decision, episode, ""))},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "run it")},
			Episodes: episodes,
		})
		if len(result.Ledger) != 1 || result.Ledger[0].Kind != rl.SFTGapEmptyCompletion || result.Ledger[0].Count != 1 {
			t.Fatalf("ledger = %+v, want the empty-completion refusal accounted beside the kept sample", result.Ledger)
		}
		if result.Kept != 1 || len(result.Examples) != 1 {
			t.Fatalf("kept %d samples, want the one legal example", result.Kept)
		}
	})
}

// contains reports whether one reason names a phrase.
func contains(reason, phrase string) bool {
	return bytes.Contains([]byte(reason), []byte(phrase))
}

// TestSFTCleaningQualification checks that the training-use matrix decides each purpose on its own evidence.
//   - every row carries its purpose, its required evidence, one verdict and the reasons behind it
//   - a qualified SFT sample never stands for DPO or sampled RL
//   - sampled RL reads logprobs only when the declared algorithm needs them
//   - candidate pairs beside compared preference evidence qualify DPO on their own evidence
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTCleaningQualification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fixture := sftFixtureOf(t, ctx, []sftInvocation{{invocation: "inv-sft-q", session: "session-sft-q", episode: "ep-sft-q"}})
	clean := func(consumer rl.TrainingUseConsumer) rl.SFTCleanResult {
		return rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  fixture.samples,
			Prompts:  fixture.prompts,
			Episodes: fixture.episodes,
			Consumer: consumer,
		})
	}

	t.Run("every row carries its four elements and one verdict", func(t *testing.T) {
		result := clean(rl.TrainingUseConsumer{})
		if len(result.Matrix.Rows) != 3 {
			t.Fatalf("matrix rows = %d, want one row per training purpose", len(result.Matrix.Rows))
		}
		seen := map[rl.TrainingPurpose]bool{}
		for _, row := range result.Matrix.Rows {
			if seen[row.Purpose] {
				t.Fatalf("purpose %q carries more than one row", row.Purpose)
			}
			seen[row.Purpose] = true
			if len(row.RequiredEvidence) == 0 {
				t.Fatalf("row %q names no required evidence", row.Purpose)
			}
			switch row.Verdict {
			case rl.QualificationUnknown, rl.QualificationIneligible:
				if len(row.MissingReasons) == 0 {
					t.Fatalf("row %q verdict %q names no missing reason", row.Purpose, row.Verdict)
				}
			case rl.QualificationQualified:
				if len(row.MissingReasons) != 0 {
					t.Fatalf("row %q is qualified beside missing reasons %v", row.Purpose, row.MissingReasons)
				}
			default:
				t.Fatalf("row %q verdict = %q, want exactly one of the three states", row.Purpose, row.Verdict)
			}
		}
		if result.Kept != 2 {
			t.Fatalf("cleaning kept %d samples, want the two formal attempts", result.Kept)
		}
	})

	t.Run("a qualified SFT sample never qualifies DPO or sampled RL", func(t *testing.T) {
		result := clean(rl.TrainingUseConsumer{})
		sft, held := result.Matrix.Row(rl.TrainingPurposeSFT)
		if !held || sft.Verdict == rl.QualificationQualified {
			t.Fatalf("SFT row = %+v, want a verdict short of qualified while no consumer is authorized", sft)
		}
		dpo, held := result.Matrix.Row(rl.TrainingPurposeDPO)
		if !held || dpo.Verdict == rl.QualificationQualified {
			t.Fatalf("DPO row = %+v, want a verdict short of qualified while the export holds no candidate pair", dpo)
		}
		if len(dpo.MissingReasons) == 0 {
			t.Fatal("the DPO row names no missing reason, want the candidate pair and preference evidence named")
		}
		rlRow, held := result.Matrix.Row(rl.TrainingPurposeSampledRL)
		if !held || rlRow.Verdict == rl.QualificationQualified {
			t.Fatalf("sampled RL row = %+v, want a verdict short of qualified while no sampling consumer is declared", rlRow)
		}
	})

	t.Run("sampled RL qualification reads logprobs only when the algorithm needs them", func(t *testing.T) {
		bestOfN := clean(rl.TrainingUseConsumer{
			Purpose:             rl.TrainingPurposeSampledRL,
			ConsumerID:          "consumer:best-of-n",
			Algorithm:           "best_of_n",
			BehaviourPolicy:     "policy:v1",
			SamplingTemperature: 0.8,
		})
		row, held := bestOfN.Matrix.Row(rl.TrainingPurposeSampledRL)
		if !held || row.Verdict != rl.QualificationQualified {
			t.Fatalf("sampled RL row without a logprobs requirement = %+v, want qualified: the algorithm needs no logprobs", row)
		}
		needsLogprobs := clean(rl.TrainingUseConsumer{
			Purpose:             rl.TrainingPurposeSampledRL,
			ConsumerID:          "consumer:importance-sampling",
			Algorithm:           "importance_sampling",
			BehaviourPolicy:     "policy:v1",
			RequiresLogprobs:    true,
			SamplingTemperature: 0.8,
		})
		row, held = needsLogprobs.Matrix.Row(rl.TrainingPurposeSampledRL)
		if !held || row.Verdict == rl.QualificationQualified {
			t.Fatalf("sampled RL row needing logprobs = %+v, want a verdict short of qualified while the attempts returned none", row)
		}
		unsupported := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{unsupportedAttemptSampleOf(t)},
			Prompts:  []rl.SFTPrompt{userPromptOf("dec-unsupported", "run it")},
			Episodes: []rl.EpisodeRecord{episodeOf("ep-unsupported", "session-unsupported")},
			Consumer: rl.TrainingUseConsumer{
				Purpose:             rl.TrainingPurposeSampledRL,
				ConsumerID:          "consumer:importance-sampling",
				Algorithm:           "importance_sampling",
				BehaviourPolicy:     "policy:v1",
				RequiresLogprobs:    true,
				SamplingTemperature: 0.8,
			},
		})
		row, held = unsupported.Matrix.Row(rl.TrainingPurposeSampledRL)
		if !held || row.Verdict != rl.QualificationIneligible {
			t.Fatalf("sampled RL row over an attempt the provider declared logprobs unsupported for = %+v, want ineligible with the declaration named", row)
		}
	})

	t.Run("a declared DPO consumer still fails without candidate pairs", func(t *testing.T) {
		result := clean(rl.TrainingUseConsumer{Purpose: rl.TrainingPurposeDPO, ConsumerID: "consumer:dpo"})
		row, held := result.Matrix.Row(rl.TrainingPurposeDPO)
		if !held || row.Verdict != rl.QualificationIneligible {
			t.Fatalf("DPO row = %+v, want ineligible while each prompt carries a single response", row)
		}
		if result.Matrix.Rows[0].Verdict == rl.QualificationQualified {
			t.Fatalf("SFT row = %+v, want the SFT verdict unchanged by a DPO consumer", result.Matrix.Rows[0])
		}
	})

	t.Run("candidate pairs with compared preference qualify DPO on their own evidence", func(t *testing.T) {
		episode := event.EpisodeID("ep-pair")
		decision := event.DecisionID("dec-pair")
		episodes := []rl.EpisodeRecord{episodeOf(episode, "session-pair")}
		left := attemptSampleOf(t, completedAttemptOf("att-pair-a", decision, episode, "answer a"))
		right := attemptSampleOf(t, completedAttemptOf("att-pair-b", decision, episode, "answer b"))
		left.Feedback = []rl.FeedbackRecord{preferenceOf("fb-pair-a", decision, "answer a is preferred")}
		right.Feedback = []rl.FeedbackRecord{preferenceOf("fb-pair-b", decision, "answer b is rejected")}
		result := rl.CleanSFT(rl.SFTCleanRequest{
			Samples:  []rl.SampleLine{left, right},
			Prompts:  []rl.SFTPrompt{userPromptOf(decision, "which answer")},
			Episodes: episodes,
			Consumer: rl.TrainingUseConsumer{Purpose: rl.TrainingPurposeDPO, ConsumerID: "consumer:dpo"},
		})
		if result.Kept != 2 {
			t.Fatalf("cleaning kept %d of the two candidate attempts, want both", result.Kept)
		}
		row, held := result.Matrix.Row(rl.TrainingPurposeDPO)
		if !held || row.Verdict != rl.QualificationQualified {
			t.Fatalf("DPO row = %+v, want qualified on the observed candidate pair and its compared preference evidence", row)
		}
		sft, _ := result.Matrix.Row(rl.TrainingPurposeSFT)
		if sft.Verdict == rl.QualificationQualified {
			t.Fatalf("SFT row = %+v, want its own verdict rather than the DPO one", sft)
		}
	})
}

// preferenceOf builds one feedback record carrying a preference conclusion with
// the source and version it came from.
func preferenceOf(id string, decision event.DecisionID, conclusion string) rl.FeedbackRecord {
	return rl.FeedbackRecord{
		Schema:     rl.SchemaCurrent,
		FeedbackID: id,
		EventKey:   string(decision),
		DecisionID: decision,
		Conclusion: conclusion,
		Source:     "human-review",
		Version:    "v1",
		RecordedAt: time.UnixMilli(2),
	}
}

// TestSFTCleaningGrouping checks that the split follows causal episode groups rather than session identities.
//   - two episodes of one session stay in two groups
//   - a declared handoff holds two episodes of different sessions in one group
//   - an episode spanning two sessions stays its own group
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTCleaningGrouping(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fixture := sftFixtureOf(t, ctx, []sftInvocation{
		{invocation: "inv-sft-a", session: "session-shared", episode: "ep-sft-a"},
		{invocation: "inv-sft-b", session: "session-shared", episode: "ep-sft-b"},
	})
	result := rl.CleanSFT(rl.SFTCleanRequest{
		Samples:  fixture.samples,
		Prompts:  fixture.prompts,
		Episodes: fixture.episodes,
	})
	if result.Kept != 4 || len(result.Splits) != 2 {
		t.Fatalf("kept %d examples in %d splits, want the four attempts of two episodes in two groups", result.Kept, len(result.Splits))
	}
	if result.Splits[0].GroupID == result.Splits[1].GroupID {
		t.Fatalf("both episodes of one session landed in the group %q, want one group per episode", result.Splits[0].GroupID)
	}
	for _, example := range result.Examples {
		if want := rl.CausalGroupOf(fixture.episodes, episodeOfSample(fixture.samples, example.SampleID)); example.GroupID != want.GroupID {
			t.Fatalf("example %q group = %q beside the causal group %q of its episode", example.SampleID, example.GroupID, want.GroupID)
		}
	}

	first := episodeOf("ep-group-a", "session-one")
	second := episodeOf("ep-group-b", "session-two")
	if rl.CausalGroupOf([]rl.EpisodeRecord{first, second}, first.ID).GroupID == rl.CausalGroupOf([]rl.EpisodeRecord{first, second}, second.ID).GroupID {
		t.Fatal("two undeclared episodes landed in one group, want a session never substituted for an episode")
	}
	second.DependsOn = []rl.EpisodeDependency{{EpisodeID: second.ID, DependsOn: first.ID, Kind: rl.DependencyHandoff, Lineage: rl.LineageKnown}}
	group := rl.CausalGroupOf([]rl.EpisodeRecord{first, second}, second.ID)
	if group.GroupID != rl.CausalGroupOf([]rl.EpisodeRecord{first, second}, first.ID).GroupID {
		t.Fatal("a declared handoff across sessions did not hold the two episodes in one group")
	}
	members := append([]event.EpisodeID(nil), group.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	if len(members) != 2 || members[0] != first.ID || members[1] != second.ID {
		t.Fatalf("group members = %v, want both episodes of the handoff", members)
	}
	spanned := episodeOf("ep-spanning", "session-one", "session-two")
	if group := rl.CausalGroupOf([]rl.EpisodeRecord{spanned}, spanned.ID); group.GroupID != "group:"+string(spanned.ID) {
		t.Fatalf("an episode spanning two sessions landed in group %q, want its own episode group", group.GroupID)
	}
}

// episodeOfSample reports the episode one exported sample belongs to.
func episodeOfSample(samples []rl.SampleLine, sampleID string) event.EpisodeID {
	for _, sample := range samples {
		if sample.Provenance.SampleID == sampleID {
			return sample.Fact.EpisodeID
		}
	}
	return ""
}
