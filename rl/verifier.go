package rl

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/SpellingDragon/tagent/api/event"
)

// Verification is the open-domain verdict of one episode: the completion share,
// whether it reached the verifier's pass score, and the observations the verdict
// rests on.
type Verification struct {
	// Score is the completion share of the episode, in [0,1]; it is computed
	// from the episode record and is never a fixed answer comparison.
	Score float64
	// Passed reports whether Score reached the verifier's pass score.
	Passed bool
	// Reasons lists the observations the verdict rests on, in the order the
	// verifier read them.
	Reasons []string
}

// Verdict is the pass outcome a verification puts beside a reward value and the
// reasons it rests on.
type Verdict struct {
	// Passed reports whether the verified episode reached the pass score.
	Passed bool
	// Reasons lists the observations the verdict rests on.
	Reasons []string
}

// Verifier is the open-domain judging port: one episode record in, one verdict
// out, with no fixed answer set to compare against. A task whose answer set is
// not fixed is judged on the observations its episode record carries, and a low
// score is a result the caller records rather than an error it routes.
//
// The redline list (4 rules):
// MUST judge the episode from its own recorded observations and name them in Reasons.
// MUST return a score in [0,1] and a verdict that a reward record can cite.
// MUST NOT treat a low score as an error: a failed verdict is a recorded result.
// MUST NOT read or invent an answer set: an open-domain episode has none.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
type Verifier interface {
	// Verify judges one episode record and returns its verdict.
	Verify(ctx context.Context, record EpisodeRecord) (Verification, error)
}

const (
	// VerificationRuleHeuristic is the derivation rule version of the offline
	// heuristic verdict.
	VerificationRuleHeuristic = "verification.heuristic.v1"
	// VerificationRuleLLMJudge is the derivation rule version of a verdict
	// produced through an injected judge.
	VerificationRuleLLMJudge = "verification.llm_judge.v1"
	// DefaultVerificationPassScore is the pass score a heuristic verifier takes
	// when its construction names none.
	DefaultVerificationPassScore = 0.75
)

// ErrVerificationScoreRange and the other verification errors name the way a
// verdict or a consumer was refused, so a caller routes each by name.
var (
	// ErrVerificationScoreRange reports a score outside [0,1].
	ErrVerificationScoreRange = errors.New("rl: the verification score is outside [0,1]")
	// ErrVerificationPassScore reports a pass score outside (0,1].
	ErrVerificationPassScore = errors.New("rl: the verification pass score is outside (0,1]")
	// ErrVerificationRuleMissing reports a verification that names no rule version.
	ErrVerificationRuleMissing = errors.New("rl: the verification rule names no version")
	// ErrVerifierMissing reports a verification consumer built beside no verifier.
	ErrVerifierMissing = errors.New("rl: the verification consumer holds no verifier")
	// ErrVerificationAccountMissing reports a verification consumer built beside
	// no reward account.
	ErrVerificationAccountMissing = errors.New("rl: the verification consumer holds no reward account")
)

// JudgeFunc is the injected judging strategy a heuristic verifier delegates to:
// the same episode record in and the same verdict shape out, so a model-backed
// judge stays behind the port and the offline path stays the default.
type JudgeFunc func(ctx context.Context, record EpisodeRecord) (Verification, error)

// HeuristicOption configures one heuristic verifier.
type HeuristicOption func(*HeuristicVerifier)

// WithPassScore sets the score a verdict must reach to pass.
func WithPassScore(score float64) HeuristicOption {
	return func(v *HeuristicVerifier) {
		v.passScore = score
	}
}

// WithLLMJudge injects one judging strategy in place of the offline heuristic.
// A nil judge leaves the offline heuristic in place.
func WithLLMJudge(judge JudgeFunc) HeuristicOption {
	return func(v *HeuristicVerifier) {
		if judge == nil {
			return
		}
		v.judge = judge
		v.ruleVersion = VerificationRuleLLMJudge
	}
}

// WithVerificationRule sets the derivation rule version the verdict is stamped
// with.
func WithVerificationRule(version string) HeuristicOption {
	return func(v *HeuristicVerifier) {
		v.ruleVersion = version
	}
}

// HeuristicVerifier is the offline verifier: it computes a verdict from the
// observations one episode record carries and needs no model. An injected judge
// replaces the heuristic while the port and the offline default stay in place.
type HeuristicVerifier struct {
	passScore   float64
	ruleVersion string
	judge       JudgeFunc
}

// NewHeuristicVerifier builds one heuristic verifier over the given options. A
// pass score outside (0,1] or a rule version naming nothing is refused by name.
func NewHeuristicVerifier(opts ...HeuristicOption) (*HeuristicVerifier, error) {
	v := &HeuristicVerifier{
		passScore:   DefaultVerificationPassScore,
		ruleVersion: VerificationRuleHeuristic,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(v)
		}
	}
	if math.IsNaN(v.passScore) || math.IsInf(v.passScore, 0) || v.passScore <= 0 || v.passScore > 1 {
		return nil, fmt.Errorf("%w: %v", ErrVerificationPassScore, v.passScore)
	}
	if v.ruleVersion == "" {
		return nil, ErrVerificationRuleMissing
	}
	return v, nil
}

// RuleVersion reports the derivation rule version this verifier stamps its
// verdicts with.
func (v *HeuristicVerifier) RuleVersion() string {
	return v.ruleVersion
}

// Verify judges one episode record: the injected judge runs when one is set and
// the offline heuristic runs otherwise. The score is checked against [0,1] and
// Passed is set from the configured pass score, so a reader never re-derives a
// verdict from a threshold of its own.
func (v *HeuristicVerifier) Verify(ctx context.Context, record EpisodeRecord) (Verification, error) {
	if err := ctx.Err(); err != nil {
		return Verification{}, err
	}
	verdict := heuristicVerdict(record)
	if v.judge != nil {
		judged, err := v.judge(ctx, record)
		if err != nil {
			return Verification{}, err
		}
		verdict = judged
	}
	if err := ValidateVerification(verdict); err != nil {
		return Verification{}, err
	}
	verdict.Passed = verdict.Score >= v.passScore
	verdict.Reasons = append([]string(nil), verdict.Reasons...)
	return verdict, nil
}

// ValidateVerification reports whether one verdict may be recorded: its score
// stands in [0,1] and a failed verdict is a result rather than an error.
func ValidateVerification(verdict Verification) error {
	if math.IsNaN(verdict.Score) || math.IsInf(verdict.Score, 0) || verdict.Score < 0 || verdict.Score > 1 {
		return fmt.Errorf("%w: %v", ErrVerificationScoreRange, verdict.Score)
	}
	return nil
}

// heuristicVerdict computes the offline verdict of one episode record: the
// completion share starts at one and every observed constraint violation
// subtracts its weight. The observations are the episode identity, its session
// evidence, the record schema, the lineage references and the dependency
// citations the record declares.
func heuristicVerdict(record EpisodeRecord) Verification {
	var reasons []string
	penalty := 0.0
	violations := 0
	violate := func(weight float64, reason string) {
		penalty += weight
		violations++
		reasons = append(reasons, reason)
	}

	if record.ID == "" {
		violate(1, "the episode identity is absent")
	} else {
		reasons = append(reasons, "the episode identity is present")
	}
	if len(record.Sessions) == 0 {
		violate(0.3, "the episode names no session evidence")
	} else {
		reasons = append(reasons, "the episode names its session evidence")
	}
	if record.Schema != SchemaCurrent {
		violate(0.3, "the episode record is not written under the current schema")
	} else {
		reasons = append(reasons, "the episode record carries the current schema")
	}

	switch record.Lineage {
	case LineageKnown:
		knownLineageViolations(record, violate)
		if violations == 0 {
			reasons = append(reasons, "the lineage references agree with the recorded ancestry")
		}
	case LineageUnknown:
		if record.ParentID != "" || record.RootID != "" || len(record.Ancestors) > 0 {
			violate(0.3, "the episode names lineage references it marks unknown")
		} else {
			reasons = append(reasons, "the episode marks its lineage unknown beside no invented parent")
		}
	default:
		violate(0.3, "the episode lineage is not a declared state")
	}

	citationViolations := dependencyViolations(record, violate)
	if citationViolations == 0 {
		reasons = append(reasons, "the dependency citations name no self reference")
	}

	score := 1 - penalty
	if score < 0 {
		score = 0
	}
	if violations == 0 {
		reasons = append(reasons, "no constraint violation was observed")
	}
	return Verification{Score: score, Reasons: reasons}
}

// knownLineageViolations reports the constraints a known lineage breaks: a root
// reference, a parent below the root, a self citation, a repeated ancestor and
// an ancestor chain whose ends disagree with the root and parent it names.
func knownLineageViolations(record EpisodeRecord, violate func(float64, string)) {
	if record.RootID == "" {
		violate(0.3, "the known lineage names no root")
	} else if record.RootID == record.ID && record.ParentID != "" {
		violate(0.5, "the episode names itself as root beside a parent")
	} else if record.RootID != record.ID && record.ParentID == "" {
		violate(0.3, "an episode below its root names no parent")
	}
	if record.ParentID != "" && record.ParentID == record.ID {
		violate(0.5, "the episode cites itself as its parent")
	}
	seen := map[event.EpisodeID]bool{}
	for _, ancestor := range record.Ancestors {
		if ancestor == record.ID {
			violate(0.5, "the ancestor chain cites the episode itself")
		}
		if seen[ancestor] {
			violate(0.3, "the ancestor chain repeats an episode")
		}
		seen[ancestor] = true
	}
	switch {
	case record.ParentID == "" && len(record.Ancestors) > 0:
		violate(0.3, "the root episode names an ancestor chain")
	case record.ParentID != "" && len(record.Ancestors) == 0:
		violate(0.3, "the known lineage names a parent but no ancestor chain")
	case record.ParentID != "" && len(record.Ancestors) > 0:
		if record.RootID != "" && record.Ancestors[0] != record.RootID {
			violate(0.3, "the ancestor chain does not start at the named root")
		}
		if record.Ancestors[len(record.Ancestors)-1] != record.ParentID {
			violate(0.3, "the ancestor chain does not end at the named parent")
		}
	}
}

// dependencyViolations reports the constraints the declared dependency citations
// break and returns how many were observed: an empty citation, a self citation,
// a declared kind outside the vocabulary, a lineage state outside the vocabulary,
// a citation attributed to another episode and a citation declared twice.
func dependencyViolations(record EpisodeRecord, violate func(float64, string)) int {
	seen := map[string]bool{}
	count := 0
	violateAt := func(weight float64, index int, reason string) {
		count++
		violate(weight, fmt.Sprintf("dependency %d %s", index, reason))
	}
	for i, dep := range record.DependsOn {
		index := i + 1
		if dep.EpisodeID != "" && dep.EpisodeID != record.ID {
			violateAt(0.3, index, "is attributed to another episode")
		}
		if dep.DependsOn == "" {
			violateAt(0.3, index, "cites no episode")
		} else if dep.DependsOn == record.ID {
			violateAt(0.5, index, "cites the episode itself")
		}
		switch dep.Kind {
		case DependencyChildCall, DependencyHandoff, DependencySharedHistory:
		default:
			violateAt(0.3, index, "names no declared kind")
		}
		switch dep.Lineage {
		case LineageKnown, LineageUnknown:
		default:
			violateAt(0.3, index, "states no declared lineage")
		}
		key := string(dep.EpisodeID) + "\x00" + string(dep.DependsOn) + "\x00" + string(dep.Kind)
		if seen[key] {
			violateAt(0.3, index, "is declared twice")
		}
		seen[key] = true
	}
	return count
}

// VerificationConsumer judges one episode and records the verdict in the reward
// account: the completion share becomes the reward value and the pass outcome
// and its reasons ride beside it, so verification stays a reward derivation
// rather than a fourth record kind.
type VerificationConsumer struct {
	verifier    Verifier
	rewards     *RewardLedger
	ruleVersion string
}

// NewVerificationConsumer builds one verification consumer over a verifier, a
// reward account and the rule version its verdicts are recorded under. A missing
// verifier, a missing account and a rule naming no version are each refused by
// name.
func NewVerificationConsumer(verifier Verifier, rewards *RewardLedger, ruleVersion string) (*VerificationConsumer, error) {
	if verifier == nil {
		return nil, ErrVerifierMissing
	}
	if rewards == nil {
		return nil, ErrVerificationAccountMissing
	}
	if ruleVersion == "" {
		return nil, ErrVerificationRuleMissing
	}
	return &VerificationConsumer{verifier: verifier, rewards: rewards, ruleVersion: ruleVersion}, nil
}

// Consume judges one episode record and records the verdict for the named
// decision boundary and attempt. The verdict is recorded as a reward instance at
// the next revision, so an earlier verified instance keeps the revision it was
// written under.
func (c *VerificationConsumer) Consume(ctx context.Context, decision event.DecisionID, attempt event.AttemptID, record EpisodeRecord) (RewardRecord, error) {
	verdict, err := c.verifier.Verify(ctx, record)
	if err != nil {
		return RewardRecord{}, err
	}
	return c.rewards.RecordVerification(decision, attempt, c.ruleVersion, verdict)
}
