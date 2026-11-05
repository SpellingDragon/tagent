// 契约: docs/wiki/rl/rl-architecture.md#sample-gate
package sample

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/features/rl"
)

// VerdictPass, VerdictFail, VerdictUnrated and VerdictIncomplete name the
// acceptance and gate verdicts one annex line carries: pass and fail come from
// the independent verifier, unrated marks the absence of a verdict, and
// incomplete is the gate's persistent marker for a sample missing an element.
const (
	// VerdictPass is the independent verifier's positive verdict: the task
	// artifacts satisfy the task definition.
	VerdictPass = "pass"
	// VerdictFail is the independent verifier's negative verdict: the task
	// artifacts are missing or do not satisfy the task definition. A failed
	// sample is still a valid sample and is recorded as a negative one.
	VerdictFail = "fail"
	// VerdictUnrated is the annex verdict when no independent verdict exists:
	// the sample is incomplete and the absence is recorded rather than invented
	// as a pass.
	VerdictUnrated = "unrated"
	// VerdictIncomplete is the gate's persistent marker: the sample is missing
	// at least one of the three elements and is kept as an incomplete sample.
	VerdictIncomplete = "incomplete"
)

// RuleVersionV1 and GateRuleVersionV1 name the derivation rule versions an
// annex line carries: the acceptance rules a verdict was reached under, and the
// gate rules an incomplete marker was reached under. A later rule change writes
// a new version rather than editing an earlier verdict.
const (
	// RuleVersionV1 is the first accepted version of the independent
	// acceptance rules.
	RuleVersionV1 = "v1"
	// GateRuleVersionV1 is the first version of the sample qualification gate.
	GateRuleVersionV1 = "gate/v1"
)

// ErrSampleCountMismatch and the other named errors of the sample gate: each
// names the way a gate run was refused rather than producing a rate over a
// shape it could not read.
var (
	// ErrSampleCountMismatch reports a gate run whose read-back evidences do
	// not pair one-to-one with its samples.
	ErrSampleCountMismatch = errors.New("sample: the read-back evidences do not pair with the samples")
	// ErrSampleSinkMissing reports a gate run beside no capture sink: the gate
	// writes its verdict into the annex, and a gate with no write point cannot
	// make a sample complete.
	ErrSampleSinkMissing = errors.New("sample: the gate has no capture sink to write the annex through")
	// ErrSampleVerdictUnknown reports an annex verdict outside the acceptance
	// vocabulary; an unknown verdict is never counted as a pass or as unrated.
	ErrSampleVerdictUnknown = errors.New("sample: the annex verdict is outside the acceptance vocabulary")
)

// Evaluate runs the sample qualification gate over one run's samples before the seal,
// writes every sample's annex through the capture sink, and reports the sample count
// and how many carry all three elements.
// - a sample is complete only with read-back proof, an independent pass/fail verdict, and that verdict written into the annex
// - a missing element appends an incomplete marker under the same identity; the earlier verdict is never rewritten
// - the rate counts every sample in its denominator, incomplete ones included
// - the gate runs before the seal; the sink's post-seal refusal is unchanged
// 契约: docs/wiki/rl/rl-architecture.md#sample-gate
func Evaluate(ctx context.Context, sink *rl.CaptureSink, samples []rl.AnnexRecord, readback []bool) (total int, complete int, err error) {
	if sink == nil {
		return 0, 0, ErrSampleSinkMissing
	}
	if len(samples) != len(readback) {
		return 0, 0, fmt.Errorf("%w: %d samples, %d read-back evidences", ErrSampleCountMismatch, len(samples), len(readback))
	}
	for i, record := range samples {
		record.Verdict = verdictOrUnrated(record.Verdict)
		if !knownVerdict(record.Verdict) {
			return 0, 0, fmt.Errorf("%w: %q", ErrSampleVerdictUnknown, record.Verdict)
		}
		if (record.Verdict == VerdictPass || record.Verdict == VerdictFail) && record.RuleVersion == "" {
			record.RuleVersion = RuleVersionV1
		}
		if err := sink.RecordAnnex(ctx, record); err != nil {
			return 0, 0, fmt.Errorf("sample: write the annex of %s/%s: %w", record.AttemptID, record.TaskID, err)
		}
		total++
		accepted := record.Verdict == VerdictPass || record.Verdict == VerdictFail
		if accepted && readback[i] {
			complete++
			continue
		}
		marker := rl.AnnexRecord{
			AttemptID:   record.AttemptID,
			TaskID:      record.TaskID,
			Verdict:     VerdictIncomplete,
			RuleVersion: GateRuleVersionV1,
			Artifacts:   append([]string(nil), record.Artifacts...),
		}
		if err := sink.RecordAnnex(ctx, marker); err != nil {
			return 0, 0, fmt.Errorf("sample: write the incomplete marker of %s/%s: %w", record.AttemptID, record.TaskID, err)
		}
	}
	return total, complete, nil
}

// verdictOrUnrated reports the verdict an acceptance annex carries: an empty
// verdict means no independent verdict exists and is recorded as unrated, never
// as a pass.
func verdictOrUnrated(verdict string) string {
	if verdict == "" {
		return VerdictUnrated
	}
	return verdict
}

// knownVerdict reports whether one verdict is in the acceptance vocabulary: an
// unknown value is refused rather than counted as either a verdict or a missing
// one.
func knownVerdict(verdict string) bool {
	switch verdict {
	case VerdictPass, VerdictFail, VerdictUnrated, VerdictIncomplete:
		return true
	}
	return false
}
