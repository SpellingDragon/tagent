package rl

import (
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// RewardState states whether a reward was derived for a decision.
//
// RewardUnrated is the zero value: a decision with no linked feedback is recorded as
// unrated and is never given a value inferred from its task terminal.
type RewardState int

const (
	// RewardUnrated is the zero value: no feedback was linked, so no reward exists.
	RewardUnrated RewardState = iota
	// RewardRated marks a reward derived from at least one feedback source.
	RewardRated
)

// RewardRecord is a reward derived from feedback for one decision: a derived record
// and not the raw feedback event, carrying the instance it identifies itself by,
// the derivation rule version it was computed with and the feedback sources it was
// derived from.
//
// The record is derived and versioned rather than overwritten in place: a later
// feedback produces a new instance at a new revision, and the recorded instance
// stays readable under the rule that computed it. A rule version alone never
// identifies a reward record — the same rule deriving a value from late feedback
// is a different record — so the instance ID and revision are what a consumer
// cites, and a record without an instance identity cannot be cited at all.
type RewardRecord struct {
	// Schema is the record schema version the line was written with.
	Schema SchemaVersion
	// InstanceID identifies this derived record as one concrete reward instance:
	// every derivation for a decision takes its own instance ID, so two values
	// derived under one rule version remain distinguishable.
	InstanceID string
	// Revision orders the reward derivations recorded for one decision: each new
	// derivation takes the next revision and an earlier record keeps the revision
	// it was written under, never rewritten in place.
	Revision int64
	// DecisionID names the decision the reward is derived for.
	DecisionID event.DecisionID
	// AttemptID names the attempt the reward is derived for; it is empty when the
	// reward is derived at the decision boundary.
	AttemptID event.AttemptID
	// ModelVersion is the version of the model the evidence was served under, so
	// a reward derived before a model replacement is never read as the reward of
	// the version installed after it. It is empty for evidence that names no
	// version, which is recorded unversioned rather than assumed current.
	ModelVersion string
	// State states whether a reward was derived at all.
	State RewardState
	// Value is the derived reward value; it is meaningful only under RewardRated.
	Value float64
	// RuleVersion is the version of the derivation rule the value was computed
	// with, so a reader re-reads the value under that version rather than a
	// current one; it is not an instance identity.
	RuleVersion string
	// DerivedFrom lists the feedback sources the reward was derived from, in the
	// order they were read.
	DerivedFrom []RewardSource
	// DerivedAt is the time the reward was derived.
	DerivedAt time.Time
}

// RewardSource references one feedback a reward was derived from: the feedback
// event, the event key it was bound to, and the version it was read at.
type RewardSource struct {
	// FeedbackID identifies the feedback event the reward was derived from.
	FeedbackID string
	// EventKey is the hex event key the feedback was bound to.
	EventKey string
	// Version is the feedback version the source was read at.
	Version string
	// RecordedAt is the time the feedback was recorded.
	RecordedAt time.Time
}

// Reward contract assertions pin the derived record, its source and its states: a
// renamed field or a non-constant state stops the package from compiling.
var (
	_ = RewardRecord{
		Schema:       "",
		InstanceID:   "",
		Revision:     0,
		DecisionID:   "",
		AttemptID:    "",
		ModelVersion: "",
		State:        RewardUnrated,
		Value:        0,
		RuleVersion:  "",
		DerivedFrom:  []RewardSource{},
		DerivedAt:    time.Time{},
	}

	_ = RewardSource{
		FeedbackID: "",
		EventKey:   "",
		Version:    "",
		RecordedAt: time.Time{},
	}
)

const (
	_ = RewardUnrated
	_ = RewardRated
)
