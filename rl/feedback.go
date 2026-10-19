package rl

import (
	"errors"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#training-export
//
// FeedbackRecord is one feedback event as data: the external conclusion bound to
// one event key, kept exactly as it was recorded.
//
// The record is the raw feedback and not a reward: there is no value field
// because a conclusion is evidence and not a number, and no field here is ever
// derived from a task terminal. A reward derived from this record carries its
// identity in the reward's source list, so the feedback stays readable as the
// evidence it was after any number of derivations. The two accounts stay
// separate: a late feedback is read as new evidence and never as a rewrite of
// anything already derived.
type FeedbackRecord struct {
	// Schema is the record schema version the feedback was written with.
	Schema SchemaVersion `json:"schema"`
	// FeedbackID identifies the feedback event.
	FeedbackID string `json:"feedback_id"`
	// EventKey is the hex event key the feedback is bound to.
	EventKey string `json:"event_key"`
	// DecisionID names the decision the feedback belongs to; it is empty when
	// the feedback is bound to the event key alone.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback belongs to; it is empty when the
	// feedback is bound at the decision boundary.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// ModelVersion is the version of the model the bound evidence was served
	// under, read from the recorded attempt of the target. It is empty when the
	// target names no version, which is bound unversioned rather than assumed
	// to be the version in effect.
	ModelVersion string `json:"model_version,omitempty"`
	// Conclusion is the external conclusion that was bound.
	Conclusion string `json:"conclusion"`
	// Source names where the conclusion came from.
	Source string `json:"source"`
	// Version is the feedback version the record was written at, so a reader
	// names the version a derived reward was read from.
	Version string `json:"version"`
	// RecordedAt is the time the feedback was recorded.
	RecordedAt time.Time `json:"recorded_at"`
}

// ErrFeedbackIDMissing and the feedback errors beside it name the way a feedback
// record was refused, so a caller routes it by name.
var (
	// ErrFeedbackIDMissing reports a feedback record that carries no identity:
	// a reward derived from it could never name its source again.
	ErrFeedbackIDMissing = errors.New("rl: the feedback record carries no feedback id")
	// ErrFeedbackKeyMissing reports a feedback record bound to no event key: a
	// binding is never inherited from the most recent parent call.
	ErrFeedbackKeyMissing = errors.New("rl: the feedback record carries no event key")
	// ErrFeedbackVersionMissing reports a feedback record that names no
	// version: a source is cited at the version it was read at, never without
	// one.
	ErrFeedbackVersionMissing = errors.New("rl: the feedback record carries no version")
	// ErrFeedbackConflict reports a second record under one feedback id that
	// does not match the first: a repeat of the same record is idempotent and
	// different content under one identity is a conflict, reported rather than
	// overwritten.
	ErrFeedbackConflict = errors.New("rl: the feedback id is already bound to a different record")
)

// FeedbackLedger is the feedback account: the raw feedback records in the order
// they were recorded, append-only. Nothing in this account is derived and
// nothing is rewritten — a late feedback is another record beside the earlier
// ones, so the reward derivations that read this account always name the exact
// evidence set they were computed from.
type FeedbackLedger struct {
	mu      sync.Mutex
	records []FeedbackRecord
}

// NewFeedbackLedger returns an empty feedback account.
func NewFeedbackLedger() *FeedbackLedger {
	return &FeedbackLedger{}
}

// Add records one feedback event under its own identity. A record without an
// identity, a binding or a version is refused by name, a repeat of the same
// record is idempotent, and different content under one feedback id is the
// named conflict ErrFeedbackConflict rather than a silent overwrite. Stamping
// SchemaCurrent happens here and nowhere else, so every record the account
// hands out carries the schema it was written with.
func (l *FeedbackLedger) Add(record FeedbackRecord) error {
	if record.FeedbackID == "" {
		return ErrFeedbackIDMissing
	}
	if record.EventKey == "" {
		return ErrFeedbackKeyMissing
	}
	if record.Version == "" {
		return ErrFeedbackVersionMissing
	}
	record.Schema = SchemaCurrent
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.append(record)
}

// Restore records one feedback record read back from storage, keeping the schema,
// the identity and the recorded time it was written with, so a restarted reader
// attributes a late submission to the same decision and attempt as the writer did.
// A repeat of the same record is idempotent and different content under one feedback
// id is the named conflict ErrFeedbackConflict rather than an overwrite.
func (l *FeedbackLedger) Restore(record FeedbackRecord) error {
	if record.FeedbackID == "" {
		return ErrFeedbackIDMissing
	}
	if record.EventKey == "" {
		return ErrFeedbackKeyMissing
	}
	if record.Version == "" {
		return ErrFeedbackVersionMissing
	}
	if record.Schema == "" {
		record.Schema = SchemaCurrent
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.append(record)
}

// append holds one record under its identity: the repeat of a record already held is
// idempotent and different content under one identity is the named conflict.
func (l *FeedbackLedger) append(record FeedbackRecord) error {
	for _, prior := range l.records {
		if prior.FeedbackID != record.FeedbackID {
			continue
		}
		if !sameFeedbackRecord(prior, record) {
			return ErrFeedbackConflict
		}
		return nil
	}
	l.records = append(l.records, record)
	return nil
}

// sameFeedbackRecord reports whether two records under one identity carry the same
// content.
func sameFeedbackRecord(a, b FeedbackRecord) bool {
	return a.EventKey == b.EventKey && a.Conclusion == b.Conclusion &&
		a.Source == b.Source && a.Version == b.Version &&
		a.DecisionID == b.DecisionID && a.AttemptID == b.AttemptID &&
		a.ModelVersion == b.ModelVersion
}

// BoundTo returns the feedback records bound to one event key, in the order
// they were recorded. A key with no feedback returns no record, which is the
// unrated case and never an error.
func (l *FeedbackLedger) BoundTo(eventKey string) []FeedbackRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	var bound []FeedbackRecord
	for _, record := range l.records {
		if record.EventKey == eventKey {
			bound = append(bound, record)
		}
	}
	return bound
}

// All returns every feedback record the account holds, in the order they were
// recorded.
func (l *FeedbackLedger) All() []FeedbackRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]FeedbackRecord(nil), l.records...)
}
