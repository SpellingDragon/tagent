// 契约: docs/wiki/rl/rl-architecture.md#training-export
package learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/rl"
)

// TestSFTConsumer checks that the SFT consumer reads token-level training samples end to end.
//   - the formal chain tokenizes into ids, labels and masks whose sides follow the messages
//   - a stored artifact round-trips through the artifact format and verifies against its sample
//   - an artifact breaking the schema, label, mask, attribution or target contract is refused by name
//   - a constant encoder is refused instead of standing in for a tokenizer, and a run without a tokenizer accepts nothing
//   - collating pads every plane and an over-long sample is refused
//   - the real tokenizer and artifact face runs behind its gate and stays unverified without one
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestSFTConsumer(t *testing.T) {
	t.Run("the formal chain tokenizes into labels and masks on the message sides", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fixture := sftFixtureOf(t, ctx, []sftInvocation{{invocation: "inv-sft-consumer", session: "session-sft-consumer", episode: "ep-sft-consumer"}})
		cleaned := rl.CleanSFT(rl.SFTCleanRequest{Samples: fixture.samples, Prompts: fixture.prompts, Episodes: fixture.episodes})
		if cleaned.Kept != 2 || len(cleaned.Examples) != 2 {
			t.Fatalf("cleaning kept %d samples in %d examples, want the two completed attempts", cleaned.Kept, len(cleaned.Examples))
		}
		before := conversationDigests(cleaned.Examples)
		result := rl.ConsumeSFT(rl.SFTConsumerRequest{Examples: cleaned.Examples, Tokenizer: deterministicTokenizer(), PaddingID: 0})
		if result.Face != rl.SFTTokenizerFaceDeterministic {
			t.Fatalf("consumer face = %q, want the deterministic face of the injected encoder", result.Face)
		}
		if result.Accepted() != len(cleaned.Examples) || result.Refused() != 0 {
			t.Fatalf("consumer accepted %d of %d samples with the ledger %+v", result.Accepted(), len(cleaned.Examples), result.Ledger())
		}
		if len(result.Batches) != 1 || len(result.Artifacts) != len(cleaned.Examples) {
			t.Fatalf("consumer produced %d batch(es) over %d artifact(s), want one batch over every accepted sample", len(result.Batches), len(result.Artifacts))
		}
		for index, artifact := range result.Artifacts {
			example := cleaned.Examples[index]
			if err := rl.VerifySFTArtifact(artifact); err != nil {
				t.Fatalf("artifact %q does not verify: %v", artifact.SampleID, err)
			}
			if err := rl.VerifySFTArtifactAgainst(artifact, example); err != nil {
				t.Fatalf("artifact %q differs from its sample: %v", artifact.SampleID, err)
			}
			if artifact.Encoder != deterministicSFTIdentity {
				t.Fatalf("artifact %q carries the encoder %+v, want the identity the run was stamped with", artifact.SampleID, artifact.Encoder)
			}
			assertSFTMaskAlignment(t, artifact, example)
		}
		assertSFTBatchPadded(t, result.Batches[0], result.Artifacts)
		read, err := rl.ReadSFTArtifacts(bytes.NewReader(writeSFTArtifacts(t, result.Artifacts)))
		if err != nil {
			t.Fatalf("read the stored artifacts: %v", err)
		}
		if !reflect.DeepEqual(read, result.Artifacts) {
			t.Fatalf("read back %d artifact(s) that differ from the %d written", len(read), len(result.Artifacts))
		}
		if after := conversationDigests(cleaned.Examples); !reflect.DeepEqual(before, after) {
			t.Fatal("the consumer rewrote the conversations it read")
		}
	})

	t.Run("the token mask follows the message role and never crosses sides", func(t *testing.T) {
		example := sftConsumerExample()
		artifact, err := rl.BuildSFTArtifact(example, deterministicTokenizer())
		if err != nil {
			t.Fatalf("build the artifact of the crafted sample: %v", err)
		}
		trained := 0
		for position, message := range artifact.Sequence.Attribution.MessageOf {
			side := rl.SFTMaskObservation
			if message != rl.SFTScaffoldToken {
				side = example.Messages[message].Mask
			}
			switch side {
			case rl.SFTMaskAction:
				if artifact.Sequence.LossMask[position] != 1 {
					t.Fatalf("action token %d carries no loss", position)
				}
				if artifact.Sequence.Labels[position] != artifact.Sequence.InputIDs[position] {
					t.Fatalf("action token %d carries the label %d beside the id %d", position, artifact.Sequence.Labels[position], artifact.Sequence.InputIDs[position])
				}
				trained++
			default:
				if artifact.Sequence.LossMask[position] != 0 {
					t.Fatalf("observation token %d carries a loss", position)
				}
				if artifact.Sequence.Labels[position] != rl.SFTLabelIgnoreIndex {
					t.Fatalf("observation token %d carries the label %d", position, artifact.Sequence.Labels[position])
				}
			}
		}
		if trained != len([]byte("settled")) {
			t.Fatalf("the artifact trains on %d token(s), want the %d bytes of the action content", trained, len([]byte("settled")))
		}
		sides, err := rl.SFTTokenSides([]rl.SFTMask{rl.SFTMaskObservation, rl.SFTMaskAction}, []int{rl.SFTScaffoldToken, 0, 1, rl.SFTScaffoldToken})
		if err != nil {
			t.Fatalf("map the token sides of a two message conversation: %v", err)
		}
		want := []rl.SFTMask{rl.SFTMaskObservation, rl.SFTMaskObservation, rl.SFTMaskAction, rl.SFTMaskObservation}
		if !reflect.DeepEqual(sides, want) {
			t.Fatalf("token sides = %v, want %v", sides, want)
		}
		if _, err := rl.SFTTokenSides([]rl.SFTMask{rl.SFTMaskAction}, []int{3}); err == nil {
			t.Fatal("an attribution naming a message the conversation does not carry was read")
		}
		if _, err := rl.SFTTokenSides([]rl.SFTMask{"context"}, []int{0}); err == nil {
			t.Fatal("a message carrying an unknown mask side was read")
		}
	})

	t.Run("a stored artifact that breaks its contract is refused by name", func(t *testing.T) {
		example := sftConsumerExample()
		base, err := rl.BuildSFTArtifact(example, deterministicTokenizer())
		if err != nil {
			t.Fatalf("build the artifact of the crafted sample: %v", err)
		}
		lastAction := len(base.Sequence.InputIDs) - 1
		for lastAction > 0 && base.Sequence.LossMask[lastAction] != 1 {
			lastAction--
		}
		firstObservation := 0
		for firstObservation < len(base.Sequence.LossMask) && base.Sequence.LossMask[firstObservation] != 0 {
			firstObservation++
		}
		cases := []struct {
			name    string
			want    rl.SFTRefusalKind
			against bool
			mutate  func(*rl.SFTArtifact)
		}{
			{name: "an artifact of the legacy schema", want: rl.SFTRefusalUnknownSchema,
				mutate: func(a *rl.SFTArtifact) { a.Schema = rl.SchemaLegacyV2 }},
			{name: "an encoder naming no tokenizer", want: rl.SFTRefusalEncoderIdentity,
				mutate: func(a *rl.SFTArtifact) { a.Encoder.Tokenizer = "" }},
			{name: "a real encoder without a template fingerprint", want: rl.SFTRefusalEncoderIdentity,
				mutate: func(a *rl.SFTArtifact) {
					a.Encoder.Provenance = rl.SFTEncoderReal
					a.Encoder.TemplateHash = ""
				}},
			{name: "a sample carrying no token", want: rl.SFTRefusalEmptySample,
				mutate: func(a *rl.SFTArtifact) { a.Sequence = rl.SFTSequence{} }},
			{name: "planes of different lengths", want: rl.SFTRefusalLengthMismatch,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.Labels = a.Sequence.Labels[:len(a.Sequence.Labels)-1] }},
			{name: "an artifact naming no message behind its tokens", want: rl.SFTRefusalAttributionMissing,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.Attribution.MessageOf = nil }},
			{name: "an attribution naming a message the conversation does not carry", want: rl.SFTRefusalAttributionInvalid,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.Attribution.MessageOf[0] = 42 }},
			{name: "a message carrying an unknown mask side", want: rl.SFTRefusalAttributionInvalid,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.Attribution.MessageMask[0] = "context" }},
			{name: "a negative token id", want: rl.SFTRefusalSequenceInvalid,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.InputIDs[0] = -1 }},
			{name: "an attention position of zero", want: rl.SFTRefusalSequenceInvalid,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.AttentionMask[0] = 0 }},
			{name: "a loss position outside its contract", want: rl.SFTRefusalSequenceInvalid,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.LossMask[0] = 2 }},
			{name: "a loss trained on an observation", want: rl.SFTRefusalObservationTrained,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.LossMask[firstObservation] = 1 }},
			{name: "an action token no loss trains on", want: rl.SFTRefusalActionUntrained,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.LossMask[lastAction] = 0 }},
			{name: "a trained token carrying another label", want: rl.SFTRefusalLabelMismatch,
				mutate: func(a *rl.SFTArtifact) {
					a.Sequence.Labels[lastAction] = a.Sequence.InputIDs[lastAction] + 1
				}},
			{name: "an untrained token carrying a label", want: rl.SFTRefusalLabelMismatch,
				mutate: func(a *rl.SFTArtifact) { a.Sequence.Labels[firstObservation] = 7 }},
			{name: "a sample carrying no token of its target", want: rl.SFTRefusalTargetEmpty,
				mutate: func(a *rl.SFTArtifact) {
					for position := range a.Sequence.Attribution.MessageOf {
						a.Sequence.Attribution.MessageOf[position] = 0
						a.Sequence.LossMask[position] = 0
						a.Sequence.Labels[position] = rl.SFTLabelIgnoreIndex
					}
				}},
			{name: "an artifact read off other bytes", want: rl.SFTRefusalSourceMismatch, against: true,
				mutate: func(a *rl.SFTArtifact) { a.SourceDigest = strings.Repeat("0", 64) }},
			{name: "an artifact naming the other side of a message", want: rl.SFTRefusalSourceMismatch, against: true,
				mutate: func(a *rl.SFTArtifact) {
					a.Sequence.Attribution.MessageMask[len(a.Sequence.Attribution.MessageMask)-1] = rl.SFTMaskObservation
				}},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				broken := cloneSFTArtifact(t, base)
				testCase.mutate(&broken)
				var err error
				if testCase.against {
					err = rl.VerifySFTArtifactAgainst(broken, example)
				} else {
					err = rl.VerifySFTArtifact(broken)
				}
				if err == nil {
					t.Fatalf("the artifact passed verification, want the refusal %q", testCase.want)
				}
				refusal, named := rl.SFTRefusalOf(err)
				if !named || refusal.Kind != testCase.want || refusal.Reason == "" {
					t.Fatalf("refusal = (%+v, named=%v) from %v, want the named refusal %q with its reason", refusal, named, err, testCase.want)
				}
			})
		}
		if _, err := rl.ReadSFTArtifacts(bytes.NewReader([]byte("{not an artifact}\n"))); err == nil {
			t.Fatal("a stored line that does not decode was read")
		} else if refusal, named := rl.SFTRefusalOf(err); !named || refusal.Kind != rl.SFTRefusalUnreadableArtifact {
			t.Fatalf("refusal of an undecodable line = (%+v, named=%v), want the unreadable-artifact refusal", refusal, named)
		}
	})

	t.Run("a constant encoder is refused instead of standing in for a tokenizer", func(t *testing.T) {
		examples := []rl.SFTExample{sftConsumerExample(), sftConsumerExampleOther()}
		constant := rl.SFTSourceTokenizer{
			Identity: deterministicSFTIdentity,
			Encode: func(rl.SFTExample) (rl.SFTTokenization, error) {
				return rl.SFTTokenization{InputIDs: []int{5, 6, 7}, MessageOf: []int{rl.SFTScaffoldToken, 1, 1}}, nil
			},
		}
		result := rl.ConsumeSFT(rl.SFTConsumerRequest{Examples: examples, Tokenizer: constant})
		ledger := result.Ledger()
		if result.Accepted() != 0 || len(result.Artifacts) != 0 || len(result.Batches) != 0 {
			t.Fatalf("the constant encoder accepted %d sample(s) in %d artifact(s) and %d batch(es), want none", result.Accepted(), len(result.Artifacts), len(result.Batches))
		}
		if len(ledger) != 1 || ledger[0].Kind != rl.SFTRefusalEncoderConstant || ledger[0].Count != len(examples) {
			t.Fatalf("ledger = %+v, want both samples refused as a constant encoder", ledger)
		}
		unreal := rl.SFTSourceTokenizer{
			Identity: rl.SFTEncoderIdentity{Provenance: rl.SFTEncoderReal, Tokenizer: "qwen2.5-1.5b-instruct"},
			Encode:   encodeDeterministic,
		}
		result = rl.ConsumeSFT(rl.SFTConsumerRequest{Examples: examples[:1], Tokenizer: unreal})
		if result.Accepted() != 0 || len(result.Ledger()) != 1 || result.Ledger()[0].Kind != rl.SFTRefusalEncoderIdentity {
			t.Fatalf("a real tokenizer claim without a template fingerprint accepted %d sample(s) with the ledger %+v", result.Accepted(), result.Ledger())
		}
		result = rl.ConsumeSFT(rl.SFTConsumerRequest{Examples: examples})
		ledger = result.Ledger()
		if result.Face != rl.SFTTokenizerFaceAbsent || result.Accepted() != 0 {
			t.Fatalf("a run without a tokenizer reports the face %q and accepted %d sample(s)", result.Face, result.Accepted())
		}
		if len(ledger) != 1 || ledger[0].Kind != rl.SFTRefusalNoTokenizer || ledger[0].Count != len(examples) {
			t.Fatalf("ledger = %+v, want every sample refused without a tokenizer", ledger)
		}
	})

	t.Run("collating pads every plane and an over-long sample is refused", func(t *testing.T) {
		short, err := rl.BuildSFTArtifact(sftConsumerExampleOther(), deterministicTokenizer())
		if err != nil {
			t.Fatalf("build the short artifact: %v", err)
		}
		long, err := rl.BuildSFTArtifact(sftConsumerExample(), deterministicTokenizer())
		if err != nil {
			t.Fatalf("build the long artifact: %v", err)
		}
		if len(short.Sequence.InputIDs) == len(long.Sequence.InputIDs) {
			t.Fatalf("both artifacts carry %d token(s), want two lengths for the padding case", len(short.Sequence.InputIDs))
		}
		batch, err := rl.CollateSFT([]rl.SFTArtifact{short, long}, 0, 0)
		if err != nil {
			t.Fatalf("collate two artifacts: %v", err)
		}
		assertSFTBatchPadded(t, batch, []rl.SFTArtifact{short, long})
		shortest := len(short.Sequence.InputIDs)
		if len(long.Sequence.InputIDs) < shortest {
			shortest = len(long.Sequence.InputIDs)
		}
		if _, err := rl.CollateSFT([]rl.SFTArtifact{short, long}, 0, shortest-1); err == nil {
			t.Fatal("a sample longer than the longest accepted sequence was collated")
		} else if refusal, named := rl.SFTRefusalOf(err); !named || refusal.Kind != rl.SFTRefusalTooLong {
			t.Fatalf("refusal of an over-long sample = (%+v, named=%v), want the too-long refusal", refusal, named)
		}
		if _, err := rl.CollateSFT(nil, 0, 0); err == nil {
			t.Fatal("an empty batch was collated")
		}
		result := rl.ConsumeSFT(rl.SFTConsumerRequest{
			Examples:  []rl.SFTExample{sftConsumerExample()},
			Tokenizer: deterministicTokenizer(),
			MaxLength: 1,
		})
		if result.Accepted() != 0 || len(result.Ledger()) != 1 || result.Ledger()[0].Kind != rl.SFTRefusalTooLong {
			t.Fatalf("the run accepted %d sample(s) beyond its longest sequence with the ledger %+v", result.Accepted(), result.Ledger())
		}
	})

	t.Run("the real tokenizer and artifact face is reported unseen until its gate is set", func(t *testing.T) {
		tokenizerDir := os.Getenv(sftTokenizerGateEnv)
		artifactDir := os.Getenv(sftArtifactGateEnv)
		if tokenizerDir == "" && artifactDir == "" {
			t.Skipf("real tokenizer/artifact face unverified: %s and %s are unset, so the face is hung on 09.5", sftTokenizerGateEnv, sftArtifactGateEnv)
			result := rl.ConsumeSFT(rl.SFTConsumerRequest{Examples: []rl.SFTExample{sftConsumerExample()}, Tokenizer: deterministicTokenizer()})
			if result.Face == rl.SFTTokenizerFaceReal {
				t.Fatal("a deterministic encoder stood in for the real tokenizer face")
			}
			return
		}
		if tokenizerDir != "" {
			if info, err := os.Stat(filepath.Join(tokenizerDir, "tokenizer.json")); err != nil || info.IsDir() {
				t.Fatalf("the tokenizer gate %q carries no tokenizer.json: %v", tokenizerDir, err)
			}
		}
		if artifactDir == "" {
			t.Fatalf("the tokenizer gate %q is set beside no %s, so the real artifacts are missing", tokenizerDir, sftArtifactGateEnv)
		}
		entries, err := os.ReadDir(artifactDir)
		if err != nil || len(entries) == 0 {
			t.Fatalf("read the artifact gate %q: %v beside %d entry(ies)", artifactDir, err, len(entries))
		}
		total := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(artifactDir, entry.Name()))
			if err != nil {
				t.Fatalf("read the artifact file %q: %v", entry.Name(), err)
			}
			artifacts, err := rl.ReadSFTArtifacts(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("read the artifacts of %q: %v", entry.Name(), err)
			}
			for _, artifact := range artifacts {
				if artifact.Encoder.Provenance != rl.SFTEncoderReal {
					t.Fatalf("artifact %q carries the encoder %+v, want the real tokenizer face", artifact.SampleID, artifact.Encoder)
				}
			}
			if _, err := rl.CollateSFT(artifacts, 0, 0); err != nil {
				t.Fatalf("collate the artifacts of %q: %v", entry.Name(), err)
			}
			total += len(artifacts)
		}
		if total == 0 {
			t.Fatalf("the artifact gate %q holds no artifact line", artifactDir)
		}
	})
}

// sftTokenizerGateEnv names the directory of the real tokenizer the run may use.
const sftTokenizerGateEnv = "TAGENT_SFT_TOKENIZER_DIR"

// sftArtifactGateEnv names the directory of the real token-level artifacts.
const sftArtifactGateEnv = "TAGENT_SFT_ARTIFACT_DIR"

// deterministicSFTIdentity names the encoder the mechanism face runs on: it
// stands on no tokenizer.
var deterministicSFTIdentity = rl.SFTEncoderIdentity{
	Provenance:   rl.SFTEncoderDeterministic,
	Tokenizer:    "utf8-byte-marker",
	TemplateHash: "markers:v1",
}

// deterministicTokenizer tokenizes through UTF-8 bytes framed by one marker per
// message, so every token carries the message it came from.
func deterministicTokenizer() rl.SFTSourceTokenizer {
	return rl.SFTSourceTokenizer{Identity: deterministicSFTIdentity, Encode: encodeDeterministic}
}

// encodeDeterministic renders one conversation as message markers around the
// UTF-8 bytes of every message.
func encodeDeterministic(example rl.SFTExample) (rl.SFTTokenization, error) {
	tokenization := rl.SFTTokenization{}
	emit := func(token, message int) {
		tokenization.InputIDs = append(tokenization.InputIDs, token)
		tokenization.MessageOf = append(tokenization.MessageOf, message)
	}
	for index, message := range example.Messages {
		emit(1000+len(message.Message.Role), rl.SFTScaffoldToken)
		for _, content := range []byte(messageText(message.Message)) {
			emit(int(content), index)
		}
		emit(1000, rl.SFTScaffoldToken)
	}
	if len(tokenization.InputIDs) == 0 {
		return rl.SFTTokenization{}, fmt.Errorf("the conversation carries no token")
	}
	return tokenization, nil
}

// messageText reports what one message contributes to a tokenization: its
// content, the tool it answers and the calls it opens.
func messageText(message model.Message) string {
	text := message.Content
	if message.ToolID != "" {
		text += "|tool=" + message.ToolID
	}
	for _, call := range message.ToolCalls {
		text += "|call=" + call.Function.Name + "|args=" + string(call.Function.Arguments)
	}
	return text
}

// sftConsumerExample builds one cleaned sample of a user observation and an
// assistant action.
func sftConsumerExample() rl.SFTExample {
	return rl.SFTExample{
		SampleID:     "sample:consumer",
		GroupID:      "group:ep-consumer",
		Split:        "train",
		SourceDigest: strings.Repeat("a", 64),
		Messages: []rl.SFTMessage{
			{Message: model.Message{Role: model.RoleUser, Content: "list the files"}, Mask: rl.SFTMaskObservation},
			{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}, Mask: rl.SFTMaskAction},
		},
		Actions:      1,
		Observations: 1,
	}
}

// sftConsumerExampleOther builds a second cleaned sample beside the first, so an
// encoder ignoring its input is caught.
func sftConsumerExampleOther() rl.SFTExample {
	return rl.SFTExample{
		SampleID:     "sample:consumer-other",
		GroupID:      "group:ep-consumer-other",
		Split:        "test",
		SourceDigest: strings.Repeat("b", 64),
		Messages: []rl.SFTMessage{
			{Message: model.Message{Role: model.RoleUser, Content: "summarise the run"}, Mask: rl.SFTMaskObservation},
			{Message: model.Message{Role: model.RoleAssistant, Content: "ready"}, Mask: rl.SFTMaskAction},
		},
		Actions:      1,
		Observations: 1,
	}
}

// assertSFTMaskAlignment checks that every token of one artifact carries the mask
// side of the message it came from and the label its side decides.
func assertSFTMaskAlignment(t *testing.T, artifact rl.SFTArtifact, example rl.SFTExample) {
	t.Helper()
	if len(artifact.Sequence.InputIDs) == 0 {
		t.Fatalf("artifact %q carries no token", artifact.SampleID)
	}
	trained := 0
	for position, message := range artifact.Sequence.Attribution.MessageOf {
		side := rl.SFTMaskObservation
		if message != rl.SFTScaffoldToken {
			if message < 0 || message >= len(example.Messages) {
				t.Fatalf("artifact %q names the message %d beside %d message(s)", artifact.SampleID, message, len(example.Messages))
			}
			side = example.Messages[message].Mask
		}
		trainedOn := artifact.Sequence.LossMask[position] == 1
		if side == rl.SFTMaskAction && !trainedOn {
			t.Fatalf("action token %d of sample %q carries no loss", position, artifact.SampleID)
		}
		if side == rl.SFTMaskObservation && trainedOn {
			t.Fatalf("observation token %d of sample %q carries a loss", position, artifact.SampleID)
		}
		if trainedOn {
			trained++
			if artifact.Sequence.Labels[position] != artifact.Sequence.InputIDs[position] {
				t.Fatalf("trained token %d of sample %q carries the label %d beside the id %d",
					position, artifact.SampleID, artifact.Sequence.Labels[position], artifact.Sequence.InputIDs[position])
			}
			continue
		}
		if artifact.Sequence.Labels[position] != rl.SFTLabelIgnoreIndex {
			t.Fatalf("untrained token %d of sample %q carries the label %d", position, artifact.SampleID, artifact.Sequence.Labels[position])
		}
	}
	if trained == 0 {
		t.Fatalf("sample %q carries no trained token", artifact.SampleID)
	}
}

// assertSFTBatchPadded checks that one batch holds every row padded to the longest
// sample, with the pad value of every plane on the pad positions.
func assertSFTBatchPadded(t *testing.T, batch rl.SFTBatch, artifacts []rl.SFTArtifact) {
	t.Helper()
	longest := 0
	for _, artifact := range artifacts {
		if len(artifact.Sequence.InputIDs) > longest {
			longest = len(artifact.Sequence.InputIDs)
		}
	}
	if len(batch.Lengths) != len(artifacts) || len(batch.Planes.InputIDs) != len(artifacts) ||
		len(batch.Planes.AttentionMask) != len(artifacts) || len(batch.Planes.LossMask) != len(artifacts) ||
		len(batch.Planes.Labels) != len(artifacts) {
		t.Fatalf("batch holds %d row(s) beside %d artifact(s)", len(batch.Planes.InputIDs), len(artifacts))
	}
	for row := range artifacts {
		planes := [][]int{batch.Planes.InputIDs[row], batch.Planes.AttentionMask[row], batch.Planes.LossMask[row], batch.Planes.Labels[row]}
		for _, plane := range planes {
			if len(plane) != longest {
				t.Fatalf("row %d carries a plane of %d position(s) beside the longest sample of %d", row, len(plane), longest)
			}
		}
		length := batch.Lengths[row]
		if length != len(artifacts[row].Sequence.InputIDs) {
			t.Fatalf("row %d carries the length %d beside %d token(s)", row, length, len(artifacts[row].Sequence.InputIDs))
		}
		for position := length; position < longest; position++ {
			if batch.Planes.InputIDs[row][position] != batch.PaddingID || batch.Planes.AttentionMask[row][position] != 0 ||
				batch.Planes.LossMask[row][position] != 0 || batch.Planes.Labels[row][position] != rl.SFTLabelIgnoreIndex {
				t.Fatalf("row %d carries the pad position %d as (%d, %d, %d, %d)", row, position,
					batch.Planes.InputIDs[row][position], batch.Planes.AttentionMask[row][position],
					batch.Planes.LossMask[row][position], batch.Planes.Labels[row][position])
			}
		}
	}
}

// writeSFTArtifacts stores the artifacts in the wire form the consumer reads.
func writeSFTArtifacts(t *testing.T, artifacts []rl.SFTArtifact) []byte {
	t.Helper()
	var buffer bytes.Buffer
	for _, artifact := range artifacts {
		raw, err := json.Marshal(artifact)
		if err != nil {
			t.Fatalf("marshal the artifact of sample %q: %v", artifact.SampleID, err)
		}
		buffer.Write(raw)
		buffer.WriteByte('\n')
	}
	return buffer.Bytes()
}

// cloneSFTArtifact copies one artifact, so a broken case never edits the case it
// started from.
func cloneSFTArtifact(t *testing.T, artifact rl.SFTArtifact) rl.SFTArtifact {
	t.Helper()
	raw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal the artifact of sample %q: %v", artifact.SampleID, err)
	}
	var clone rl.SFTArtifact
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatalf("unmarshal the artifact of sample %q: %v", artifact.SampleID, err)
	}
	return clone
}

// conversationDigests reports the digest of every conversation, so a pass that
// rewrites what it read is caught by comparing the digests before and after.
func conversationDigests(examples []rl.SFTExample) []string {
	digests := make([]string, 0, len(examples))
	for _, example := range examples {
		raw, err := json.Marshal(example.Messages)
		if err != nil {
			digests = append(digests, "")
			continue
		}
		sum := sha256.Sum256(raw)
		digests = append(digests, hex.EncodeToString(sum[:]))
	}
	return digests
}
