// 契约: docs/wiki/rl/rl-architecture.md#training-export
package rl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// SFTLabelIgnoreIndex is the label a loss never reads: every position no loss
// trains on carries it.
const SFTLabelIgnoreIndex = -100

// SFTScaffoldToken attributes a token to no message: a token the chat template
// emitted around the messages.
const SFTScaffoldToken = -1

// SFTTokenization is one conversation tokenized as a whole: the ids the encoder
// produced and the message every token came from.
type SFTTokenization struct {
	// InputIDs are the token ids in conversation order.
	InputIDs []int
	// MessageOf names, per token, the index of the message it came from;
	// SFTScaffoldToken marks a token the template emitted around the messages.
	MessageOf []int
}

// SFTEncodeFunc tokenizes one cleaned conversation.
type SFTEncodeFunc func(SFTExample) (SFTTokenization, error)

// SFTSourceTokenizer is the tokenizer one consumer run reads through: the
// identity its ids are stamped with and the function that produces them.
type SFTSourceTokenizer struct {
	// Identity names the tokenizer behind the ids.
	Identity SFTEncoderIdentity
	// Encode produces the ids of one conversation; a nil function tokenizes
	// nothing.
	Encode SFTEncodeFunc
}

// SFTTokenAttribution records what every token of one sample was produced from:
// the message it came from and the mask side of every message.
type SFTTokenAttribution struct {
	// MessageOf names, per token, the message it came from.
	MessageOf []int `json:"message_of"`
	// MessageMask is the mask side of every message of the conversation.
	MessageMask []SFTMask `json:"message_mask"`
}

// SFTSequence is the token planes of one sample: the ids, their attention, the
// loss positions, the labels a loss reads and the attribution of every token.
type SFTSequence struct {
	// InputIDs are the token ids in conversation order.
	InputIDs []int `json:"input_ids"`
	// AttentionMask carries one attention position per token.
	AttentionMask []int `json:"attention_mask"`
	// LossMask carries one loss position per token: 1 on a trained token.
	LossMask []int `json:"loss_mask"`
	// Labels carry the id a loss reads on a trained token and
	// SFTLabelIgnoreIndex everywhere else.
	Labels []int `json:"labels"`
	// Attribution names the message behind every token.
	Attribution SFTTokenAttribution `json:"attribution"`
}

// SFTArtifact is one token-level training sample as it is stored and read back:
// the schema it was written with, the sample it came from, the bytes it was read
// off, the encoder behind its ids and its token planes.
type SFTArtifact struct {
	// Schema is the artifact schema the sample was written with.
	Schema SchemaVersion `json:"schema"`
	// SampleID names the exported sample the artifact was built from.
	SampleID string `json:"sample_id"`
	// SourceDigest is the SHA-256 of the stored body the sample was read off.
	SourceDigest string `json:"source_digest"`
	// Encoder names the tokenizer behind the ids.
	Encoder SFTEncoderIdentity `json:"encoder"`
	// Sequence holds the token planes of the sample.
	Sequence SFTSequence `json:"sequence"`
}

// SFTTokenSides maps every token to the mask side of the message it came from: a
// token attributed to a message carries that message's side and a token the
// template emitted around the messages carries the observation side.
func SFTTokenSides(messageMask []SFTMask, messageOf []int) ([]SFTMask, error) {
	if len(messageMask) == 0 {
		return nil, sftConsumerRefuse(SFTRefusalAttributionMissing, "the sample names no message, so no token side can be read")
	}
	for index, mask := range messageMask {
		switch mask {
		case SFTMaskAction, SFTMaskObservation:
		default:
			return nil, sftConsumerRefuse(SFTRefusalAttributionInvalid, "message %d carries the unknown mask side %q", index, mask)
		}
	}
	if len(messageOf) == 0 {
		return nil, sftConsumerRefuse(SFTRefusalAttributionMissing, "the sample attributes no token to a message")
	}
	sides := make([]SFTMask, len(messageOf))
	for position, message := range messageOf {
		switch {
		case message == SFTScaffoldToken:
			sides[position] = SFTMaskObservation
		case message < 0 || message >= len(messageMask):
			return nil, sftConsumerRefuse(SFTRefusalAttributionInvalid, "token %d names the message %d beside %d message(s)", position, message, len(messageMask))
		default:
			sides[position] = messageMask[message]
		}
	}
	return sides, nil
}

// BuildSFTArtifact tokenizes one cleaned sample and stores its token planes: the
// mask side of every token follows the message it came from, the loss trains on
// every action token and the labels carry the ids a loss reads.
func BuildSFTArtifact(example SFTExample, tokenizer SFTSourceTokenizer) (SFTArtifact, error) {
	if tokenizer.Encode == nil {
		return SFTArtifact{}, sftConsumerRefuse(SFTRefusalNoTokenizer, "the run names no tokenizer, so sample %q is never tokenized", example.SampleID)
	}
	if err := tokenizer.Identity.refusal(); err != nil {
		return SFTArtifact{}, err
	}
	tokenization, err := tokenizer.Encode(example)
	if err != nil {
		return SFTArtifact{}, sftConsumerRefuse(SFTRefusalEncodeFailed, "the tokenizer failed on sample %q: %v", example.SampleID, err)
	}
	messageMask := make([]SFTMask, 0, len(example.Messages))
	for _, message := range example.Messages {
		messageMask = append(messageMask, message.Mask)
	}
	if len(tokenization.InputIDs) != len(tokenization.MessageOf) {
		return SFTArtifact{}, sftConsumerRefuse(SFTRefusalLengthMismatch, "the tokenizer returned %d ids beside %d attributions for sample %q",
			len(tokenization.InputIDs), len(tokenization.MessageOf), example.SampleID)
	}
	sides, err := SFTTokenSides(messageMask, tokenization.MessageOf)
	if err != nil {
		return SFTArtifact{}, err
	}
	sequence := SFTSequence{
		InputIDs:      append([]int(nil), tokenization.InputIDs...),
		AttentionMask: make([]int, len(tokenization.InputIDs)),
		LossMask:      make([]int, len(tokenization.InputIDs)),
		Labels:        make([]int, len(tokenization.InputIDs)),
		Attribution: SFTTokenAttribution{
			MessageOf:   append([]int(nil), tokenization.MessageOf...),
			MessageMask: messageMask,
		},
	}
	for position := range sequence.InputIDs {
		sequence.AttentionMask[position] = 1
		if sides[position] == SFTMaskAction {
			sequence.LossMask[position] = 1
			sequence.Labels[position] = sequence.InputIDs[position]
			continue
		}
		sequence.Labels[position] = SFTLabelIgnoreIndex
	}
	artifact := SFTArtifact{
		Schema:       SchemaCurrent,
		SampleID:     example.SampleID,
		SourceDigest: example.SourceDigest,
		Encoder:      tokenizer.Identity,
		Sequence:     sequence,
	}
	if err := VerifySFTArtifact(artifact); err != nil {
		return SFTArtifact{}, err
	}
	return artifact, nil
}

// VerifySFTArtifact reports the first violation of one stored artifact: its
// schema, its planes aligned to one length, one label per position, the loss
// positions exactly on the action side and a non-empty target.
func VerifySFTArtifact(artifact SFTArtifact) error {
	if artifact.Schema != SchemaCurrent {
		return sftConsumerRefuse(SFTRefusalUnknownSchema, "the artifact is written under %q and this package reads %q", artifact.Schema, SchemaCurrent)
	}
	if err := artifact.Encoder.refusal(); err != nil {
		return err
	}
	sequence := artifact.Sequence
	ids := len(sequence.InputIDs)
	if ids == 0 {
		return sftConsumerRefuse(SFTRefusalEmptySample, "sample %q carries no token", artifact.SampleID)
	}
	for _, plane := range []struct {
		name  string
		count int
	}{
		{name: "attention mask", count: len(sequence.AttentionMask)},
		{name: "loss mask", count: len(sequence.LossMask)},
		{name: "labels", count: len(sequence.Labels)},
	} {
		if plane.count != ids {
			return sftConsumerRefuse(SFTRefusalLengthMismatch, "sample %q carries %d ids beside %d %s position(s)", artifact.SampleID, ids, plane.count, plane.name)
		}
	}
	if len(sequence.Attribution.MessageOf) == 0 {
		return sftConsumerRefuse(SFTRefusalAttributionMissing, "sample %q attributes no token to a message", artifact.SampleID)
	}
	if len(sequence.Attribution.MessageOf) != ids {
		return sftConsumerRefuse(SFTRefusalLengthMismatch, "sample %q carries %d ids beside %d token attribution(s)",
			artifact.SampleID, ids, len(sequence.Attribution.MessageOf))
	}
	sides, err := SFTTokenSides(sequence.Attribution.MessageMask, sequence.Attribution.MessageOf)
	if err != nil {
		return err
	}
	for position := range sequence.InputIDs {
		if sequence.InputIDs[position] < 0 {
			return sftConsumerRefuse(SFTRefusalSequenceInvalid, "sample %q carries the token id %d at position %d", artifact.SampleID, sequence.InputIDs[position], position)
		}
		if sequence.AttentionMask[position] != 1 {
			return sftConsumerRefuse(SFTRefusalSequenceInvalid, "sample %q carries the attention position %d at %d", artifact.SampleID, sequence.AttentionMask[position], position)
		}
		switch sequence.LossMask[position] {
		case 0, 1:
		default:
			return sftConsumerRefuse(SFTRefusalSequenceInvalid, "sample %q carries the loss position %d at %d", artifact.SampleID, sequence.LossMask[position], position)
		}
		trained := sequence.LossMask[position] == 1
		if sides[position] == SFTMaskObservation && trained {
			return sftConsumerRefuse(SFTRefusalObservationTrained, "sample %q trains on the token %d of an observation", artifact.SampleID, position)
		}
		if sides[position] == SFTMaskAction && !trained {
			return sftConsumerRefuse(SFTRefusalActionUntrained, "sample %q carries no loss on the action token %d", artifact.SampleID, position)
		}
		if trained && sequence.Labels[position] != sequence.InputIDs[position] {
			return sftConsumerRefuse(SFTRefusalLabelMismatch, "sample %q carries the label %d on the trained token %d of id %d",
				artifact.SampleID, sequence.Labels[position], position, sequence.InputIDs[position])
		}
		if !trained && sequence.Labels[position] != SFTLabelIgnoreIndex {
			return sftConsumerRefuse(SFTRefusalLabelMismatch, "sample %q carries the label %d on the untrained token %d instead of %d",
				artifact.SampleID, sequence.Labels[position], position, SFTLabelIgnoreIndex)
		}
	}
	target := len(sequence.Attribution.MessageMask) - 1
	targetTokens := 0
	for position := range sequence.InputIDs {
		if sequence.Attribution.MessageOf[position] == target {
			targetTokens++
		}
	}
	if targetTokens == 0 {
		return sftConsumerRefuse(SFTRefusalTargetEmpty, "sample %q carries no token of its last message %d", artifact.SampleID, target)
	}
	return nil
}

// VerifySFTArtifactAgainst reports the first difference between one stored
// artifact and the cleaned sample it names: the bytes it was read off, the sample
// identity and the mask side of every message.
func VerifySFTArtifactAgainst(artifact SFTArtifact, example SFTExample) error {
	if artifact.SampleID != example.SampleID {
		return sftConsumerRefuse(SFTRefusalSourceMismatch, "the artifact names the sample %q beside %q", artifact.SampleID, example.SampleID)
	}
	if artifact.SourceDigest != example.SourceDigest {
		return sftConsumerRefuse(SFTRefusalSourceMismatch, "the artifact names the source digest %q beside %q", artifact.SourceDigest, example.SourceDigest)
	}
	mask := artifact.Sequence.Attribution.MessageMask
	if len(mask) != len(example.Messages) {
		return sftConsumerRefuse(SFTRefusalSourceMismatch, "the artifact names %d message mask(s) beside the %d message(s) of sample %q",
			len(mask), len(example.Messages), example.SampleID)
	}
	for index, side := range mask {
		if want := example.Messages[index].Mask; side != want {
			return sftConsumerRefuse(SFTRefusalSourceMismatch, "the artifact names the mask side %q on message %d beside the %q side of sample %q",
				side, index, want, example.SampleID)
		}
	}
	return nil
}

// ReadSFTArtifacts reads stored artifacts, one JSON line per sample, and
// verifies every artifact as it is read.
func ReadSFTArtifacts(source io.Reader) ([]SFTArtifact, error) {
	artifacts := make([]SFTArtifact, 0)
	reader := bufio.NewReader(source)
	for line := 1; ; line++ {
		raw, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 {
			var artifact SFTArtifact
			if decodeErr := json.Unmarshal(trimmed, &artifact); decodeErr != nil {
				return nil, sftConsumerRefuse(SFTRefusalUnreadableArtifact, "artifact line %d does not read: %v", line, decodeErr)
			}
			if verifyErr := VerifySFTArtifact(artifact); verifyErr != nil {
				return nil, verifyErr
			}
			artifacts = append(artifacts, artifact)
		}
		if err == io.EOF {
			return artifacts, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
