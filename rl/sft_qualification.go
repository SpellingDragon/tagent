package rl

// TrainingPurpose names one training use the qualification matrix judges. Each
// purpose is qualified on its own evidence; a verdict for one purpose never
// stands for another.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
type TrainingPurpose string

const (
	// TrainingPurposeSFT names supervised fine tuning on one observed completion.
	TrainingPurposeSFT TrainingPurpose = "sft"
	// TrainingPurposeDPO names direct preference optimisation on same-input
	// candidate pairs.
	TrainingPurposeDPO TrainingPurpose = "dpo"
	// TrainingPurposeSampledRL names off-policy learning from sampled rollouts
	// under one behaviour policy.
	TrainingPurposeSampledRL TrainingPurpose = "sampled_rl"
)

// trainingPurposes is the row order of every qualification matrix.
var trainingPurposes = []TrainingPurpose{
	TrainingPurposeSFT,
	TrainingPurposeDPO,
	TrainingPurposeSampledRL,
}

// QualificationVerdict is exactly one of the three qualification states.
type QualificationVerdict string

const (
	// QualificationUnknown marks a purpose whose evidence is genuinely missing:
	// nothing observed rules the purpose out and nothing confirms it.
	QualificationUnknown QualificationVerdict = "unknown"
	// QualificationIneligible marks a purpose known not to meet a necessary
	// condition on the observed data.
	QualificationIneligible QualificationVerdict = "ineligible"
	// QualificationQualified marks a purpose with a named consumer and every
	// required condition met.
	QualificationQualified QualificationVerdict = "qualified"
)

// QualificationRow is one row of the training-use qualification matrix: the
// purpose, the real evidence it requires, its single verdict and the reasons
// behind a verdict that is not qualified.
type QualificationRow struct {
	// Purpose names the training use the row judges.
	Purpose TrainingPurpose
	// RequiredEvidence names the evidence the purpose is decided on.
	RequiredEvidence []string
	// Verdict is exactly one of the three qualification states.
	Verdict QualificationVerdict
	// MissingReasons names every condition the verdict rests on that the
	// observed evidence does not meet; it is empty only for a qualified row.
	MissingReasons []string
}

// QualificationMatrix is the qualification contract of one cleaning run: one
// row per training purpose, in purpose order.
type QualificationMatrix struct {
	// Rows holds one row per purpose.
	Rows []QualificationRow
}

// Row returns the row of one purpose and reports whether the matrix holds it.
func (m QualificationMatrix) Row(purpose TrainingPurpose) (QualificationRow, bool) {
	for _, row := range m.Rows {
		if row.Purpose == purpose {
			return row, true
		}
	}
	return QualificationRow{}, false
}

// TrainingUseConsumer declares one real consumer of the cleaned data: the
// purpose it consumes for, its identity, and the algorithm and sampling
// declaration it runs under. A zero consumer declares no consumer at all.
type TrainingUseConsumer struct {
	// Purpose names the training use the consumer reads the data for.
	Purpose TrainingPurpose
	// ConsumerID identifies the consumer; an empty identity declares none.
	ConsumerID string
	// Algorithm names the actual algorithm the consumer runs.
	Algorithm string
	// BehaviourPolicy identifies the policy the samples were drawn under.
	BehaviourPolicy string
	// RequiresLogprobs reports whether the algorithm needs logprobs of the
	// sampled tokens; a false value never makes their absence a failure.
	RequiresLogprobs bool
	// SamplingTemperature is the sampling temperature the consumer draws at; a
	// value at or below zero declares no sampling evidence.
	SamplingTemperature float64
}

// qualify derives the qualification matrix from the evidence one cleaning run
// observed and the consumer the data is declared for. Each row is decided on
// its own evidence: a qualified purpose never stands for another.
func qualify(evidence sftEvidence, consumer TrainingUseConsumer) QualificationMatrix {
	matrix := QualificationMatrix{Rows: make([]QualificationRow, 0, len(trainingPurposes))}
	for _, purpose := range trainingPurposes {
		row := QualificationRow{Purpose: purpose}
		switch purpose {
		case TrainingPurposeSFT:
			row.RequiredEvidence = []string{
				"a current-schema sample carrying one observed attempt",
				"a fixed prompt and a non-empty completion",
				"a legal message sequence with the action and observation masks aligned to it",
				"a causal split group",
				"a named consumer",
			}
			row.Verdict, row.MissingReasons = qualifySFT(evidence, consumer)
		case TrainingPurposeDPO:
			row.RequiredEvidence = []string{
				"same-input candidate pairs",
				"preference evidence for each candidate pair, with its source and version",
				"a causal split group per candidate set",
				"a named consumer",
			}
			row.Verdict, row.MissingReasons = qualifyDPO(evidence, consumer)
		case TrainingPurposeSampledRL:
			row.RequiredEvidence = []string{
				"the actual sampling algorithm",
				"the behaviour policy the samples were drawn under",
				"the sampling temperature or distribution",
				"logprobs evidence when the algorithm needs it",
				"a named consumer",
			}
			row.Verdict, row.MissingReasons = qualifySampledRL(evidence, consumer)
		}
		matrix.Rows = append(matrix.Rows, row)
	}
	return matrix
}

// qualifySFT decides the supervised fine tuning row on the examples the run
// kept and the consumer named for it.
func qualifySFT(evidence sftEvidence, consumer TrainingUseConsumer) (QualificationVerdict, []string) {
	if evidence.kept == 0 {
		return QualificationIneligible, []string{"no sample passed the cleaning contract"}
	}
	if consumer.Purpose != TrainingPurposeSFT || consumer.ConsumerID == "" {
		return QualificationUnknown, []string{"no authorized SFT consumer is named for the cleaned samples"}
	}
	return QualificationQualified, nil
}

// qualifyDPO decides the preference row on candidate pairs and the preference
// evidence that compares them. It never reads the SFT row.
func qualifyDPO(evidence sftEvidence, consumer TrainingUseConsumer) (QualificationVerdict, []string) {
	switch {
	case evidence.candidateSets() == 0 && evidence.unreadable > 0:
		return QualificationUnknown, []string{
			"same-input candidate pairs (a refused sample could carry a second response)",
			"preference evidence",
		}
	case evidence.candidateSets() == 0:
		return QualificationIneligible, []string{
			"same-input candidate pairs (no prompt carries more than one observed response)",
			"preference evidence (no feedback compares two responses)",
		}
	case evidence.comparedSets() == 0:
		return QualificationIneligible, []string{"preference evidence for each candidate pair"}
	case consumer.Purpose != TrainingPurposeDPO || consumer.ConsumerID == "":
		return QualificationUnknown, []string{"no authorized DPO consumer is named for the candidate pairs"}
	}
	return QualificationQualified, nil
}

// qualifySampledRL decides the sampled rollout row on the declared algorithm, the
// behaviour policy and the sampling evidence, reading logprobs only when the
// algorithm needs them.
func qualifySampledRL(evidence sftEvidence, consumer TrainingUseConsumer) (QualificationVerdict, []string) {
	if consumer.Purpose != TrainingPurposeSampledRL || consumer.ConsumerID == "" {
		return QualificationUnknown, []string{
			"an actual sampling algorithm consumer",
			"the behaviour policy the samples were drawn under",
		}
	}
	if consumer.Algorithm == "" {
		return QualificationUnknown, []string{"the sampling algorithm the consumer runs"}
	}
	if consumer.BehaviourPolicy == "" {
		return QualificationIneligible, []string{"the behaviour policy the samples were drawn under"}
	}
	if consumer.SamplingTemperature <= 0 {
		return QualificationIneligible, []string{"the sampling temperature or distribution of the rollouts"}
	}
	if consumer.RequiresLogprobs {
		switch {
		case evidence.logprobs[LogprobsPresent] > 0:
		case evidence.logprobs[LogprobsUnsupported] > 0:
			return QualificationIneligible, []string{"logprobs evidence (the provider declared them unsupported)"}
		default:
			return QualificationUnknown, []string{"logprobs evidence the declared algorithm needs"}
		}
	}
	return QualificationQualified, nil
}
