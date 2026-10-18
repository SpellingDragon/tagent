// 契约: docs/wiki/rl/rl-architecture.md#training-export
package rl

import (
	"fmt"
	"sort"
)

// SFTEncoderProvenance names what produced the token ids of one sample.
type SFTEncoderProvenance string

const (
	// SFTEncoderReal marks ids produced by a real tokenizer through its chat
	// template.
	SFTEncoderReal SFTEncoderProvenance = "real"
	// SFTEncoderDeterministic marks ids produced by a deterministic encoder that
	// stands on no tokenizer.
	SFTEncoderDeterministic SFTEncoderProvenance = "deterministic"
)

// SFTTokenizerFace names what stood behind one consumer run.
type SFTTokenizerFace string

const (
	// SFTTokenizerFaceReal marks a run encoded by a real tokenizer.
	SFTTokenizerFaceReal SFTTokenizerFace = "real"
	// SFTTokenizerFaceDeterministic marks a run encoded by a deterministic
	// encoder; it never counts as a tokenizer face.
	SFTTokenizerFaceDeterministic SFTTokenizerFace = "deterministic"
	// SFTTokenizerFaceAbsent marks a run that encoded nothing.
	SFTTokenizerFaceAbsent SFTTokenizerFace = "absent"
)

// SFTEncoderIdentity names the tokenizer an encoder stands on: the kind it
// claims, its name and the fingerprint of the template its ids were rendered
// with.
type SFTEncoderIdentity struct {
	// Provenance is the kind of encoder the identity claims.
	Provenance SFTEncoderProvenance `json:"provenance"`
	// Tokenizer names the tokenizer; an empty name declares no tokenizer.
	Tokenizer string `json:"tokenizer"`
	// TemplateHash fingerprints the chat template the ids came from.
	TemplateHash string `json:"template_hash"`
}

// face reports what the identity stands on.
func (i SFTEncoderIdentity) face() SFTTokenizerFace {
	switch i.Provenance {
	case SFTEncoderReal:
		return SFTTokenizerFaceReal
	case SFTEncoderDeterministic:
		return SFTTokenizerFaceDeterministic
	default:
		return SFTTokenizerFaceAbsent
	}
}

// refusal reports the identity violation, when the identity breaks its contract.
func (i SFTEncoderIdentity) refusal() error {
	if i.Tokenizer == "" {
		return sftConsumerRefuse(SFTRefusalEncoderIdentity, "the encoder names no tokenizer, so its ids stand on no tokenizer face")
	}
	switch i.Provenance {
	case SFTEncoderReal:
		if i.TemplateHash == "" {
			return sftConsumerRefuse(SFTRefusalEncoderIdentity, "the encoder claims a real tokenizer %q without the fingerprint of the template its ids came from", i.Tokenizer)
		}
	case SFTEncoderDeterministic:
	default:
		return sftConsumerRefuse(SFTRefusalEncoderIdentity, "the encoder claims the unknown provenance %q", i.Provenance)
	}
	return nil
}

// SFTRefusalKind classifies why one sample was kept out of a training batch.
type SFTRefusalKind string

const (
	// SFTRefusalNoTokenizer marks a run that named no tokenizer: nothing is
	// tokenized and no sample is accepted.
	SFTRefusalNoTokenizer SFTRefusalKind = "no_tokenizer"
	// SFTRefusalEncodeFailed marks a tokenizer that failed on one conversation.
	SFTRefusalEncodeFailed SFTRefusalKind = "encode_failed"
	// SFTRefusalEncoderConstant marks distinct conversations a tokenizer encoded
	// into one sequence: a stand-in carries no tokenization of its input.
	SFTRefusalEncoderConstant SFTRefusalKind = "encoder_constant"
	// SFTRefusalEncoderIdentity marks an encoder identity that breaks its
	// contract, including a real tokenizer claim without a template fingerprint.
	SFTRefusalEncoderIdentity SFTRefusalKind = "encoder_identity"
	// SFTRefusalUnknownSchema marks an artifact written under a schema this
	// package does not read.
	SFTRefusalUnknownSchema SFTRefusalKind = "unknown_schema"
	// SFTRefusalUnreadableArtifact marks a stored line that does not decode.
	SFTRefusalUnreadableArtifact SFTRefusalKind = "unreadable_artifact"
	// SFTRefusalEmptySample marks a sample carrying no token.
	SFTRefusalEmptySample SFTRefusalKind = "empty_sample"
	// SFTRefusalLengthMismatch marks planes of different lengths, so a position
	// has no id, mask, label or message.
	SFTRefusalLengthMismatch SFTRefusalKind = "length_mismatch"
	// SFTRefusalSequenceInvalid marks a token id, an attention position or a
	// loss position outside its contract.
	SFTRefusalSequenceInvalid SFTRefusalKind = "sequence_invalid"
	// SFTRefusalLabelMismatch marks a label that differs from the id a trained
	// token carries, or from SFTLabelIgnoreIndex on an untrained one.
	SFTRefusalLabelMismatch SFTRefusalKind = "label_mismatch"
	// SFTRefusalAttributionMissing marks a sample naming no message behind its
	// tokens, so no token side can be read.
	SFTRefusalAttributionMissing SFTRefusalKind = "attribution_missing"
	// SFTRefusalAttributionInvalid marks an attribution naming a message the
	// conversation does not carry, or an unknown mask side.
	SFTRefusalAttributionInvalid SFTRefusalKind = "attribution_invalid"
	// SFTRefusalTargetEmpty marks a sample whose last message carries no token.
	SFTRefusalTargetEmpty SFTRefusalKind = "target_empty"
	// SFTRefusalObservationTrained marks a loss position on an observation side.
	SFTRefusalObservationTrained SFTRefusalKind = "observation_trained"
	// SFTRefusalActionUntrained marks an action token no loss trains on.
	SFTRefusalActionUntrained SFTRefusalKind = "action_untrained"
	// SFTRefusalSourceMismatch marks an artifact whose identity, source bytes or
	// message masks differ from the cleaned sample it names.
	SFTRefusalSourceMismatch SFTRefusalKind = "source_mismatch"
	// SFTRefusalTooLong marks a sample longer than the longest sequence the run
	// accepts.
	SFTRefusalTooLong SFTRefusalKind = "too_long"
)

// SFTRefusal is one named refusal of the consumer: the kind it is classified
// under and what was observed.
type SFTRefusal struct {
	// Kind classifies the refusal.
	Kind SFTRefusalKind
	// Reason states what was observed.
	Reason string
}

// sftRefusalError carries one refusal out of a single-sample step.
type sftRefusalError struct {
	refusal SFTRefusal
}

// Error reports the refusal reason.
func (e sftRefusalError) Error() string { return e.refusal.Reason }

// sftConsumerRefuse raises one named refusal.
func sftConsumerRefuse(kind SFTRefusalKind, format string, args ...any) error {
	return sftRefusalError{refusal: SFTRefusal{Kind: kind, Reason: fmt.Sprintf(format, args...)}}
}

// asSFTRefusal reports the refusal behind an error, when the error is one.
func asSFTRefusal(err error) (SFTRefusal, bool) {
	refused, ok := err.(sftRefusalError)
	if !ok {
		return SFTRefusal{}, false
	}
	return refused.refusal, true
}

// SFTRefusalOf reports the named refusal an error carries, so a caller classifies
// a refusal by its kind rather than by its prose.
func SFTRefusalOf(err error) (SFTRefusal, bool) {
	return asSFTRefusal(err)
}

// SFTRefusalLedger is one line of the refusal account: a kind, how many samples
// it refused and which ones, in sample order.
type SFTRefusalLedger struct {
	// Kind classifies the refusals this line accounts for.
	Kind SFTRefusalKind
	// Count is the number of samples the kind refused.
	Count int
	// SampleIDs names the refused samples.
	SampleIDs []string
}

// SFTConsumerRecord is the consumer record of one cleaned sample: accepted into
// the batch or refused by name, beside the group and split the sample carries.
type SFTConsumerRecord struct {
	// SampleID names the sample the record belongs to.
	SampleID string
	// GroupID is the causal split group of the sample.
	GroupID string
	// Split is the side the group was assigned to.
	Split string
	// Accepted reports whether the sample entered the batch.
	Accepted bool
	// Refusal names the refusal that kept the sample out; it is zero when
	// accepted.
	Refusal SFTRefusal
}

// SFTBatchPlanes holds the padded planes of one batch, one row per sample.
type SFTBatchPlanes struct {
	// InputIDs carries one padded id row per sample.
	InputIDs [][]int `json:"input_ids"`
	// AttentionMask carries one padded attention row per sample.
	AttentionMask [][]int `json:"attention_mask"`
	// LossMask carries one padded loss row per sample.
	LossMask [][]int `json:"loss_mask"`
	// Labels carries one padded label row per sample.
	Labels [][]int `json:"labels"`
}

// SFTBatch is one collated batch: the padded planes, the unpadded length of every
// sample and the id every pad position carries.
type SFTBatch struct {
	// Planes holds the padded planes of the batch.
	Planes SFTBatchPlanes
	// Lengths holds the unpadded token count of every sample, in row order.
	Lengths []int
	// PaddingID is the id a pad position carries.
	PaddingID int
}

// SFTConsumerRequest is one consumer run: the cleaned samples to tokenize, the
// tokenizer they are read through, the longest sequence the run accepts and the
// id a pad position carries.
type SFTConsumerRequest struct {
	// Examples are the cleaned samples of one SFT run.
	Examples []SFTExample
	// Tokenizer produces and names the ids of every sample.
	Tokenizer SFTSourceTokenizer
	// MaxLength is the longest sequence the run accepts; a value at or below
	// zero accepts any length.
	MaxLength int
	// PaddingID is the id a pad position carries.
	PaddingID int
}

// SFTConsumerResult is the outcome of one consumer run: one record per sample,
// the artifacts that passed, the collated batch and the face the run stood on.
type SFTConsumerResult struct {
	// Records holds one consumer record per sample, in sample order.
	Records []SFTConsumerRecord
	// Artifacts holds the samples that passed, in sample order.
	Artifacts []SFTArtifact
	// Batches holds the collated batch; it is empty when no sample passed.
	Batches []SFTBatch
	// Face names what the run encoded through.
	Face SFTTokenizerFace
}

// Accepted counts the samples that entered the batch.
func (r SFTConsumerResult) Accepted() int {
	count := 0
	for _, record := range r.Records {
		if record.Accepted {
			count++
		}
	}
	return count
}

// Refused counts the samples kept out of the batch.
func (r SFTConsumerResult) Refused() int {
	return len(r.Records) - r.Accepted()
}

// Ledger reports the refusal account by kind, in kind order.
func (r SFTConsumerResult) Ledger() []SFTRefusalLedger {
	lines := map[SFTRefusalKind]*SFTRefusalLedger{}
	for _, record := range r.Records {
		if record.Accepted {
			continue
		}
		line := lines[record.Refusal.Kind]
		if line == nil {
			line = &SFTRefusalLedger{Kind: record.Refusal.Kind}
			lines[record.Refusal.Kind] = line
		}
		line.Count++
		line.SampleIDs = append(line.SampleIDs, record.SampleID)
	}
	account := make([]SFTRefusalLedger, 0, len(lines))
	for _, line := range lines {
		sort.Strings(line.SampleIDs)
		account = append(account, *line)
	}
	sort.Slice(account, func(i, j int) bool { return account[i].Kind < account[j].Kind })
	return account
}

// CollateSFT pads the token planes of one batch to the longest sample and keeps
// the unpadded length of every sample: a pad position carries the padding id, no
// attention, no loss and SFTLabelIgnoreIndex.
func CollateSFT(artifacts []SFTArtifact, paddingID, maxLength int) (SFTBatch, error) {
	if len(artifacts) == 0 {
		return SFTBatch{}, fmt.Errorf("collating an empty batch: no artifact was accepted")
	}
	longest := 0
	lengths := make([]int, 0, len(artifacts))
	for _, artifact := range artifacts {
		length := len(artifact.Sequence.InputIDs)
		if maxLength > 0 && length > maxLength {
			return SFTBatch{}, sftConsumerRefuse(SFTRefusalTooLong, "sample %q carries %d token(s) beside the longest accepted sequence of %d",
				artifact.SampleID, length, maxLength)
		}
		if length > longest {
			longest = length
		}
		lengths = append(lengths, length)
	}
	batch := SFTBatch{
		Planes: SFTBatchPlanes{
			InputIDs:      make([][]int, 0, len(artifacts)),
			AttentionMask: make([][]int, 0, len(artifacts)),
			LossMask:      make([][]int, 0, len(artifacts)),
			Labels:        make([][]int, 0, len(artifacts)),
		},
		Lengths:   lengths,
		PaddingID: paddingID,
	}
	for _, artifact := range artifacts {
		batch.Planes.InputIDs = append(batch.Planes.InputIDs, padSFTPlane(artifact.Sequence.InputIDs, paddingID, longest))
		batch.Planes.AttentionMask = append(batch.Planes.AttentionMask, padSFTPlane(artifact.Sequence.AttentionMask, 0, longest))
		batch.Planes.LossMask = append(batch.Planes.LossMask, padSFTPlane(artifact.Sequence.LossMask, 0, longest))
		batch.Planes.Labels = append(batch.Planes.Labels, padSFTPlane(artifact.Sequence.Labels, SFTLabelIgnoreIndex, longest))
	}
	return batch, nil
}

// padSFTPlane pads one plane to a length, carrying the pad value on every pad
// position.
func padSFTPlane(plane []int, pad, longest int) []int {
	padded := make([]int, longest)
	for index := range padded {
		padded[index] = pad
	}
	copy(padded, plane)
	return padded
}

// ConsumeSFT runs one consumer pass over cleaned samples: every sample is
// tokenized, stored, verified and collated, or refused by name. A run that names
// no tokenizer accepts nothing, and a tokenizer that encodes distinct
// conversations into one sequence is refused as a stand-in.
func ConsumeSFT(req SFTConsumerRequest) SFTConsumerResult {
	result := SFTConsumerResult{Records: make([]SFTConsumerRecord, 0, len(req.Examples)), Face: SFTTokenizerFaceAbsent}
	if req.Tokenizer.Encode != nil {
		result.Face = req.Tokenizer.Identity.face()
	}
	artifacts := make([]SFTArtifact, 0, len(req.Examples))
	conversations := make([]string, 0, len(req.Examples))
	signatures := make([]string, 0, len(req.Examples))
	for _, example := range req.Examples {
		record := SFTConsumerRecord{SampleID: example.SampleID, GroupID: example.GroupID, Split: example.Split}
		artifact, err := BuildSFTArtifact(example, req.Tokenizer)
		if err == nil && req.MaxLength > 0 && len(artifact.Sequence.InputIDs) > req.MaxLength {
			err = sftConsumerRefuse(SFTRefusalTooLong, "sample %q carries %d token(s) beside the longest accepted sequence of %d",
				example.SampleID, len(artifact.Sequence.InputIDs), req.MaxLength)
		}
		if err != nil {
			refusal, named := asSFTRefusal(err)
			if !named {
				refusal = SFTRefusal{Kind: SFTRefusalEncodeFailed, Reason: err.Error()}
			}
			record.Refusal = refusal
			result.Records = append(result.Records, record)
			artifacts = append(artifacts, SFTArtifact{})
			conversations = append(conversations, "")
			signatures = append(signatures, "")
			continue
		}
		record.Accepted = true
		result.Records = append(result.Records, record)
		artifacts = append(artifacts, artifact)
		conversations = append(conversations, digestOfJSON(example.Messages))
		signatures = append(signatures, fmt.Sprint(artifact.Sequence.InputIDs))
	}
	refuseConstant(result.Records, conversations, signatures)
	for index, record := range result.Records {
		if record.Accepted {
			result.Artifacts = append(result.Artifacts, artifacts[index])
		}
	}
	if len(result.Artifacts) > 0 {
		batch, err := CollateSFT(result.Artifacts, req.PaddingID, req.MaxLength)
		if err == nil {
			result.Batches = append(result.Batches, batch)
		}
	}
	return result
}

// refuseConstant refuses every accepted sample whose sequence a sample of another
// conversation also carries: one sequence for distinct conversations is a
// stand-in that encodes no input.
func refuseConstant(records []SFTConsumerRecord, conversations, signatures []string) {
	shared := map[string]map[string]bool{}
	for index, signature := range signatures {
		if signature == "" {
			continue
		}
		if shared[signature] == nil {
			shared[signature] = map[string]bool{}
		}
		shared[signature][conversations[index]] = true
	}
	for index := range records {
		if signatures[index] == "" || len(shared[signatures[index]]) < 2 {
			continue
		}
		records[index].Accepted = false
		records[index].Refusal = SFTRefusal{
			Kind: SFTRefusalEncoderConstant,
			Reason: fmt.Sprintf("sample %q carries the sequence of %d distinct conversation(s), so no tokenizer encoded it",
				records[index].SampleID, len(shared[signatures[index]])),
		}
	}
}
