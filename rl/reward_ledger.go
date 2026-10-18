package rl

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// RewardRule is one derivation rule: the version a derived value is computed
// under and the function that reduces the feedback evidence to that value.
//
// The rule version is what a reader re-reads the value under. It is never an
// instance identity: the same rule deriving a value from late feedback is a
// different reward record, and the instance ID and revision are what a
// consumer cites.
type RewardRule struct {
	// Version is the rule version a derived value is stamped with.
	Version string
	// Value reduces the feedback evidence to the reward value. It runs only
	// over real evidence: a derivation with no evidence never reaches it.
	Value func(evidence []FeedbackRecord) (float64, error)
}

// ErrRewardNoFeedback and the reward errors beside it name the way a derivation
// or a restore was refused, so a caller routes it by name.
var (
	// ErrRewardNoFeedback reports a derivation that was handed no feedback
	// evidence. The record it accompanies is recorded as unrated and carries
	// no value: a reward is never invented from a task terminal, so the
	// refusal and the unrated mark are both observable.
	ErrRewardNoFeedback = errors.New("rl: the reward derivation carries no feedback evidence")
	// ErrRewardRuleMissing reports a derivation whose rule names no version or
	// carries no value function: a value computed without a rule could never
	// be re-read under the version it claims.
	ErrRewardRuleMissing = errors.New("rl: the reward rule names no version or carries no value function")
	// ErrRewardConflict reports a restore of a record under an instance ID the
	// account already holds with different content: a repeat of the same
	// record is idempotent and different content under one identity is a
	// conflict, reported rather than overwritten.
	ErrRewardConflict = errors.New("rl: the reward instance is already recorded with different content")
	// ErrRewardRecordInvalid reports a restore of a record that cannot stand
	// on its own: no decision to derive for, or no schema to read it under.
	ErrRewardRecordInvalid = errors.New("rl: the reward record cannot stand on its own")
	// ErrRewardVersionConflict reports evidence naming two different model
	// versions for one decision and attempt. A value derived across versions is
	// attributed to neither, so the derivation is refused by name rather than
	// stamped with one of them.
	ErrRewardVersionConflict = errors.New("rl: the reward evidence crosses two model versions")
)

// RewardLedger is the reward account: the derived reward records per decision
// boundary and attempt, append-only. Every derivation takes the next revision
// and its own instance ID, so two values derived from different evidence sets
// remain distinguishable and an earlier record keeps the revision it was
// written under, never rewritten in place.
type RewardLedger struct {
	mu      sync.Mutex
	records []RewardRecord
}

// NewRewardLedger returns an empty reward account.
func NewRewardLedger() *RewardLedger {
	return &RewardLedger{}
}

// rewardInstanceID names one derived record: the decision and attempt it is
// derived for and the revision it took. The name is deterministic over the
// derivation order, so a record read back after a restart carries the identity
// it was written with and never a regenerated one.
func rewardInstanceID(decision event.DecisionID, attempt event.AttemptID, revision int64) string {
	return fmt.Sprintf("reward:%s:%s:r%d", decision, attempt, revision)
}

// nextRevision returns the revision the next derivation for this decision and
// attempt takes. Records restored without an instance identity do not take a
// revision slot: they stay readable but never stand in the derivation order.
func (l *RewardLedger) nextRevision(decision event.DecisionID, attempt event.AttemptID) int64 {
	var next int64 = 1
	for _, record := range l.records {
		if record.DecisionID != decision || record.AttemptID != attempt {
			continue
		}
		if record.InstanceID == "" || record.Revision < 1 {
			continue
		}
		if record.Revision >= next {
			next = record.Revision + 1
		}
	}
	return next
}

// evidenceModelVersion reports the model version one evidence set was served
// under: the single version its records name, empty when none of them names one.
// A version a record does not name is unknown and does not contradict the
// version another record names, because both were bound to the same attempt. Two
// different named versions are the named conflict ErrRewardVersionConflict: one
// attempt was served by one version, so evidence crossing versions is refused
// rather than attributed to either.
func evidenceModelVersion(evidence []FeedbackRecord) (string, error) {
	var version string
	for _, record := range evidence {
		if record.ModelVersion == "" {
			continue
		}
		if version == "" {
			version = record.ModelVersion
			continue
		}
		if version != record.ModelVersion {
			return "", fmt.Errorf("%w: %q and %q", ErrRewardVersionConflict, version, record.ModelVersion)
		}
	}
	return version, nil
}

// Derive derives one reward record from the feedback evidence and records it
// as a new instance at the next revision. Nothing already recorded is
// rewritten.
//
// A derivation handed no evidence is refused by name with ErrRewardNoFeedback
// and recorded as an unrated instance that carries no value: a reward is never
// invented from a task terminal, and both the refusal and the unrated mark
// reach the caller. A rule that names no version or carries no value function
// is refused with ErrRewardRuleMissing before anything is recorded.
func (l *RewardLedger) Derive(decision event.DecisionID, attempt event.AttemptID, rule RewardRule, evidence []FeedbackRecord) (RewardRecord, error) {
	if decision == "" {
		return RewardRecord{}, ErrRewardRecordInvalid
	}
	if rule.Version == "" || rule.Value == nil {
		return RewardRecord{}, ErrRewardRuleMissing
	}
	modelVersion, err := evidenceModelVersion(evidence)
	if err != nil {
		return RewardRecord{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	revision := l.nextRevision(decision, attempt)
	record := RewardRecord{
		Schema:       SchemaCurrent,
		InstanceID:   rewardInstanceID(decision, attempt, revision),
		Revision:     revision,
		DecisionID:   decision,
		AttemptID:    attempt,
		ModelVersion: modelVersion,
		RuleVersion:  rule.Version,
		DerivedAt:    time.Now().UTC(),
	}
	if len(evidence) == 0 {
		record.State = RewardUnrated
		l.records = append(l.records, record)
		return record, ErrRewardNoFeedback
	}
	value, err := rule.Value(evidence)
	if err != nil {
		return RewardRecord{}, err
	}
	record.State = RewardRated
	record.Value = value
	for _, source := range evidence {
		record.DerivedFrom = append(record.DerivedFrom, RewardSource{
			FeedbackID: source.FeedbackID,
			EventKey:   source.EventKey,
			Version:    source.Version,
			RecordedAt: source.RecordedAt,
		})
	}
	l.records = append(l.records, record)
	return record, nil
}

// DeriveForEvidence returns the reward instance one decision boundary and attempt
// carries under the given evidence set. When the newest recorded derivation was
// computed from exactly that evidence under the same model version, it is
// returned as it stands; when the evidence changed — a late feedback arrived — or
// the version it was served under differs — a model replacement moved the attempt
// onto another generation — a new instance is derived at the next revision and
// the record earlier readers cite keeps the revision it was written under.
func (l *RewardLedger) DeriveForEvidence(decision event.DecisionID, attempt event.AttemptID, rule RewardRule, evidence []FeedbackRecord) (RewardRecord, error) {
	version, err := evidenceModelVersion(evidence)
	if err != nil {
		return RewardRecord{}, err
	}
	wanted := map[string]bool{}
	for _, record := range evidence {
		wanted[record.FeedbackID] = true
	}
	if latest, held := l.Latest(decision, attempt); held {
		match := latest.ModelVersion == version && len(latest.DerivedFrom) == len(wanted)
		if match {
			for _, source := range latest.DerivedFrom {
				if !wanted[source.FeedbackID] {
					match = false
					break
				}
			}
		}
		if match {
			return latest, nil
		}
	}
	record, err := l.Derive(decision, attempt, rule, evidence)
	if err != nil && !errors.Is(err, ErrRewardNoFeedback) {
		return RewardRecord{}, err
	}
	return record, nil
}

// Latest returns the newest reward record derived for one decision boundary
// and attempt, and reports whether one is held. The record is returned as it
// was written: a reader cites the instance ID and revision it carries, never a
// regenerated identity.
func (l *RewardLedger) Latest(decision event.DecisionID, attempt event.AttemptID) (RewardRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.records) - 1; i >= 0; i-- {
		record := l.records[i]
		if record.DecisionID == decision && record.AttemptID == attempt {
			return record, true
		}
	}
	return RewardRecord{}, false
}

// Records returns every reward record derived for one decision boundary and
// attempt, in the order they were derived. A late feedback derivation is
// another record beside the earlier ones, so the history stays readable.
func (l *RewardLedger) Records(decision event.DecisionID, attempt event.AttemptID) []RewardRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	var held []RewardRecord
	for _, record := range l.records {
		if record.DecisionID == decision && record.AttemptID == attempt {
			held = append(held, record)
		}
	}
	return held
}

// StateOf returns the reward state of the newest record derived for one
// decision boundary and attempt: RewardUnrated when nothing was derived or the
// newest derivation carried no evidence, RewardRated otherwise.
func (l *RewardLedger) StateOf(decision event.DecisionID, attempt event.AttemptID) RewardState {
	if latest, held := l.Latest(decision, attempt); held {
		return latest.State
	}
	return RewardUnrated
}

// Restore records one reward record read back from storage, keeping the
// identity and revision it was written with. A repeat of the same record is
// idempotent and different content under one instance identity is the named
// conflict ErrRewardConflict rather than a silent overwrite. A record without
// an instance identity is kept as it stands: it cannot be cited by an exported
// sample and stays readable for the rejection ledger to name.
func (l *RewardLedger) Restore(record RewardRecord) error {
	if record.DecisionID == "" {
		return ErrRewardRecordInvalid
	}
	if record.Schema == "" {
		record.Schema = SchemaCurrent
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if record.InstanceID != "" {
		for _, prior := range l.records {
			if prior.InstanceID != record.InstanceID {
				continue
			}
			if prior.Revision != record.Revision || prior.DecisionID != record.DecisionID ||
				prior.AttemptID != record.AttemptID || prior.State != record.State ||
				prior.Value != record.Value || prior.RuleVersion != record.RuleVersion ||
				prior.ModelVersion != record.ModelVersion {
				return ErrRewardConflict
			}
			return nil
		}
	}
	l.records = append(l.records, record)
	return nil
}
