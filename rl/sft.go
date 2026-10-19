// 契约: docs/wiki/rl/rl-architecture.md#training-export
package rl

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// SFTLegacyConverterV2 names the strict v2 converter that stays a read-only
// import: a sample written under the v2 record schema is routed to it and is
// never read as an SFT example of the current record schema.
const SFTLegacyConverterV2 = "scripts/convert_trajectories.py"

// DefaultSFTTrainRatio is the share of causal groups a cleaning run assigns to
// the training side when the request names no ratio.
const DefaultSFTTrainRatio = 0.9

// SFTMask names the loss side of one conversation message: an action is trained
// on and an observation is context only. The side follows the role of the
// message and is never read from its content.
type SFTMask string

const (
	// SFTMaskObservation marks context a loss never trains on.
	SFTMaskObservation SFTMask = "observation"
	// SFTMaskAction marks a model action a loss trains on.
	SFTMaskAction SFTMask = "action"
)

// SFTMessage is one message of a cleaned conversation beside the mask side it
// falls on. The message is carried as it was read, never rewritten.
type SFTMessage struct {
	// Message is the conversation message exactly as the records carried it.
	Message model.Message
	// Mask is the loss side of that message.
	Mask SFTMask
}

// SFTGapKind classifies why one sample was kept out of the dataset. Every
// refusal is named: a sample that is neither converted nor refused would be a
// silent drop.
type SFTGapKind string

const (
	// SFTGapLegacyReadOnly marks a sample written under the v2 record schema: it
	// is read through the v2 converter and never as an example here.
	SFTGapLegacyReadOnly SFTGapKind = "legacy_read_only"
	// SFTGapUnknownSchema marks a sample whose schema this package cannot read.
	SFTGapUnknownSchema SFTGapKind = "unknown_schema"
	// SFTGapNotAnAttempt marks a sample carrying no observed attempt: an SFT
	// example is the completion of one attempt, and a decision carries none.
	SFTGapNotAnAttempt SFTGapKind = "not_an_attempt"
	// SFTGapBodyUnreadable marks a sample whose stored body cannot be parsed.
	SFTGapBodyUnreadable SFTGapKind = "body_unreadable"
	// SFTGapIdentityMismatch marks a body whose identity differs from the fact
	// carrying it.
	SFTGapIdentityMismatch SFTGapKind = "identity_mismatch"
	// SFTGapTerminalNotCompleted marks an attempt that did not complete.
	SFTGapTerminalNotCompleted SFTGapKind = "terminal_not_completed"
	// SFTGapAttemptFailed marks an attempt that observed an error.
	SFTGapAttemptFailed SFTGapKind = "attempt_failed"
	// SFTGapCompletionMalformed marks a response that is not exactly one
	// assistant choice, or a choice of another role.
	SFTGapCompletionMalformed SFTGapKind = "completion_malformed"
	// SFTGapEmptyCompletion marks an assistant action with neither content nor
	// a tool call.
	SFTGapEmptyCompletion SFTGapKind = "empty_completion"
	// SFTGapPromptMissing marks a sample whose decision has no fixed prompt.
	SFTGapPromptMissing SFTGapKind = "prompt_missing"
	// SFTGapIllegalMessage marks a conversation whose message sequence or role
	// is not legal.
	SFTGapIllegalMessage SFTGapKind = "illegal_message"
	// SFTGapGroupMismatch marks a sample whose provenance group differs from
	// the group its episode's causal dependencies decide.
	SFTGapGroupMismatch SFTGapKind = "group_mismatch"
)

// SFTGap is one named refusal: the kind it is classified under and the reason
// it was refused.
type SFTGap struct {
	// Kind classifies the refusal.
	Kind SFTGapKind
	// Reason states what was observed.
	Reason string
}

// sftGapError marks a refusal raised inside the strict conversion so the caller
// records it instead of failing the run.
type sftGapError struct {
	gap SFTGap
}

// Error reports the refusal reason.
func (e sftGapError) Error() string { return e.gap.Reason }

// sftRefuse raises one named refusal.
func sftRefuse(kind SFTGapKind, format string, args ...any) error {
	return sftGapError{gap: SFTGap{Kind: kind, Reason: fmt.Sprintf(format, args...)}}
}

// asSFTGap reports the refusal behind an error, when the error is one.
func asSFTGap(err error) (SFTGap, bool) {
	refusal, ok := err.(sftGapError)
	if !ok {
		return SFTGap{}, false
	}
	return refusal.gap, true
}

// SFTExample is one sample that passed the cleaning contract: the conversation
// it converts to, the mask side of every message, the split group it belongs to
// and the digest of the bytes it was read from.
type SFTExample struct {
	// SampleID names the exported sample the example was converted from.
	SampleID string
	// GroupID names the causal split group of the example.
	GroupID string
	// Split names the side the group was assigned to.
	Split string
	// DecisionID names the decision the attempt was made under.
	DecisionID event.DecisionID
	// AttemptID names the attempt the completion was observed on.
	AttemptID event.AttemptID
	// Messages holds the conversation exactly as read, each with its mask side.
	Messages []SFTMessage
	// Actions counts the messages a loss trains on.
	Actions int
	// Observations counts the context messages a loss never trains on.
	Observations int
	// SourceDigest is the SHA-256 of the stored body the example was read from.
	SourceDigest string
}

// SFTCleanRecord is the cleaning record of one input sample: kept or refused,
// the single refusal that kept it out, the mask counts of a kept sample and the
// digest of the bytes it was read from. Every input sample carries exactly one
// record.
type SFTCleanRecord struct {
	// SampleID names the sample the record belongs to.
	SampleID string
	// EventKey names the stored event the sample was reduced from.
	EventKey event.EventID
	// GroupID is the causal split group the sample's provenance names.
	GroupID string
	// Kept reports whether the sample became an example.
	Kept bool
	// Gap names the refusal that kept the sample out; it is empty when kept.
	Gap SFTGap
	// Actions and Observations are the mask counts of a kept sample.
	Actions      int
	Observations int
	// SourceDigest is the SHA-256 of the stored body the sample carries.
	SourceDigest string
}

// SFTGapLedger is one line of the failure account: a gap kind, how many samples
// it refused and which ones, in sample order.
type SFTGapLedger struct {
	// Kind classifies the refusals this line accounts for.
	Kind SFTGapKind
	// Count is the number of samples the kind refused.
	Count int
	// SampleIDs names the refused samples.
	SampleIDs []string
}

// SFTSplit is one causal split group and the side it was assigned to, with the
// number of examples it holds.
type SFTSplit struct {
	// GroupID names the group.
	GroupID string
	// Split names the side the group was assigned to.
	Split string
	// Examples counts the examples the group holds.
	Examples int
}

// SFTCleanResult is the outcome of one cleaning run: one record per input
// sample, the examples that passed, the failure account by gap kind, the causal
// split groups and the qualification matrix of the run.
type SFTCleanResult struct {
	// Records holds one cleaning record per input sample, in sample order.
	Records []SFTCleanRecord
	// Examples holds the samples that passed the cleaning contract.
	Examples []SFTExample
	// Ledger is the failure account, in gap-kind order.
	Ledger []SFTGapLedger
	// Splits holds one entry per causal group the examples were drawn from.
	Splits []SFTSplit
	// Kept and Rejected count the samples on each side of the contract.
	Kept     int
	Rejected int
	// Matrix is the qualification contract of the run.
	Matrix QualificationMatrix
}

// SFTPrompt is the prompt fixed for one decision at the formal request boundary:
// the conversation the model actually saw before the attempt started.
type SFTPrompt struct {
	// DecisionID names the decision the prompt was fixed for.
	DecisionID event.DecisionID
	// Messages holds the request messages as the boundary fixed them.
	Messages []model.Message
}

// SplitDeclaration is the split configuration of one cleaning run: the seed
// and ratio together are the deterministic group-to-side assignment contract,
// kept apart from the data-bearing request fields so the request structure
// stays a record of data, not of choices.
type SplitDeclaration struct {
	// SplitSeed seeds the deterministic group-to-side assignment.
	SplitSeed int64
	// TrainRatio is the share of groups assigned to the training side; a value
	// outside (0, 1) takes DefaultSFTTrainRatio.
	TrainRatio float64
}

// SFTCleanRequest is one cleaning run: the exported samples, the fixed prompts
// they are paired with, the episode records their causal groups are drawn on,
// the consumer the data is qualified for and the split declaration.
type SFTCleanRequest struct {
	// Samples are the exported samples to clean.
	Samples []SampleLine
	// Prompts are the fixed prompts, one per decision a sample was made under.
	Prompts []SFTPrompt
	// Episodes are the episode records the causal groups are drawn on; the
	// split never follows a session identity.
	Episodes []EpisodeRecord
	// Consumer declares the real consumer the cleaned data is qualified for.
	Consumer TrainingUseConsumer
	// Split is the deterministic split declaration of this cleaning run.
	Split SplitDeclaration
}

// CausalGroupOf returns the leakage-free group one episode belongs to: every
// episode it shares a parent link or a declared dependency with. A session is
// never substituted for an episode, so two episodes of one session stay apart
// unless their records declare that their data cannot be split.
func CausalGroupOf(episodes []EpisodeRecord, episode event.EpisodeID) EpisodeGroup {
	return newGroupIndex(episodes).groupFor(episode)
}

// SplitOf assigns one causal group to a side deterministically: the group and
// the seed decide the side, so the same group lands on the same side on every
// run and no group crosses the train and test sides.
func SplitOf(groupID string, seed int64, trainRatio float64) string {
	ratio := trainRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = DefaultSFTTrainRatio
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", groupID, seed)))
	bucket := int(sum[0])<<8 | int(sum[1])
	threshold := int(ratio * 65535)
	if bucket < threshold {
		return "train"
	}
	return "test"
}

// CleanSFT runs one cleaning pass over exported samples: every sample is either
// converted into a masked example or refused with a named gap, the split is
// drawn on causal groups, and the qualification matrix is derived from the
// evidence the run observed. The samples and prompts are read and never edited.
func CleanSFT(req SFTCleanRequest) SFTCleanResult {
	prompts := map[event.DecisionID][]model.Message{}
	for _, prompt := range req.Prompts {
		if _, held := prompts[prompt.DecisionID]; !held {
			prompts[prompt.DecisionID] = prompt.Messages
		}
	}
	index := newGroupIndex(req.Episodes)
	result := SFTCleanResult{Records: make([]SFTCleanRecord, 0, len(req.Samples))}
	ledger := map[SFTGapKind]*SFTGapLedger{}
	groups := map[string]*SFTSplit{}
	evidence := sftEvidence{}
	for _, sample := range req.Samples {
		record := SFTCleanRecord{
			SampleID:     sample.Provenance.SampleID,
			EventKey:     sample.Fact.EventKey,
			GroupID:      sample.Provenance.GroupID,
			SourceDigest: digestOf(sample.Fact.Body),
		}
		example, err := convertSFT(sample, prompts, index, req.Split.SplitSeed, req.Split.TrainRatio, &evidence)
		if err != nil {
			gap, named := asSFTGap(err)
			if !named {
				gap = SFTGap{Kind: SFTGapBodyUnreadable, Reason: err.Error()}
			}
			record.Gap = gap
			result.Rejected++
			evidence.unreadableSeen(gap.Kind)
			line := ledger[gap.Kind]
			if line == nil {
				line = &SFTGapLedger{Kind: gap.Kind}
				ledger[gap.Kind] = line
			}
			line.Count++
			line.SampleIDs = append(line.SampleIDs, record.SampleID)
			result.Records = append(result.Records, record)
			continue
		}
		record.Kept = true
		record.GroupID = example.GroupID
		record.Actions = example.Actions
		record.Observations = example.Observations
		result.Kept++
		result.Records = append(result.Records, record)
		result.Examples = append(result.Examples, example)
		split := groups[example.GroupID]
		if split == nil {
			split = &SFTSplit{GroupID: example.GroupID, Split: example.Split}
			groups[example.GroupID] = split
		}
		split.Examples++
	}
	for _, line := range ledger {
		sort.Strings(line.SampleIDs)
		result.Ledger = append(result.Ledger, *line)
	}
	sort.Slice(result.Ledger, func(i, j int) bool { return result.Ledger[i].Kind < result.Ledger[j].Kind })
	for _, split := range groups {
		result.Splits = append(result.Splits, *split)
	}
	sort.Slice(result.Splits, func(i, j int) bool { return result.Splits[i].GroupID < result.Splits[j].GroupID })
	evidence.kept = result.Kept
	result.Matrix = qualify(evidence, req.Consumer)
	return result
}
