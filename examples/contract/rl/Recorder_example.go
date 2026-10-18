package rlexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/rl"
)

// errNoDecision is reported for an attempt record that names no decision to belong to.
var errNoDecision = errors.New("rlexample: the attempt record names no decision")

// versionedRecorder is the canonical production-point recorder: the three methods cover the
// three record kinds, one call per record written where that record originates.
type versionedRecorder struct {
	episodes  []rl.EpisodeRecord
	decisions []rl.DecisionRecord
	attempts  []rl.AttemptRecord
}

// RecordEpisode writes the episode record of one business process where it starts.
func (r *versionedRecorder) RecordEpisode(_ context.Context, record rl.EpisodeRecord) error {
	r.episodes = append(r.episodes, record)
	return nil
}

// RecordDecision writes the decision record of one model decision where it produced its
// terminal.
func (r *versionedRecorder) RecordDecision(_ context.Context, record rl.DecisionRecord) error {
	r.decisions = append(r.decisions, record)
	return nil
}

// RecordAttempt writes the attempt record of one observable attempt where it produced its
// response, and reports a record it cannot accept as an error.
func (r *versionedRecorder) RecordAttempt(_ context.Context, record rl.AttemptRecord) error {
	if record.DecisionID == "" {
		return errNoDecision
	}
	r.attempts = append(r.attempts, record)
	return nil
}

var _ rl.Recorder = (*versionedRecorder)(nil)

// ExampleRecorder writes one episode, one decision and one attempt at the points they
// originate, and reports an attempt that names no decision as an error rather than dropping
// it.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func ExampleRecorder() {
	ctx := context.Background()
	recorder := &versionedRecorder{}

	if err := recorder.RecordEpisode(ctx, rl.EpisodeRecord{Schema: rl.SchemaCurrent, ID: "episode-1"}); err != nil {
		return
	}
	if err := recorder.RecordDecision(ctx, rl.DecisionRecord{Schema: rl.SchemaCurrent, ID: "decision-1"}); err != nil {
		return
	}
	attempt := rl.AttemptRecord{Schema: rl.SchemaCurrent, ID: "attempt-1", DecisionID: "decision-1"}
	if err := recorder.RecordAttempt(ctx, attempt); err != nil {
		return
	}
	if err := recorder.RecordAttempt(ctx, rl.AttemptRecord{ID: "attempt-2"}); !errors.Is(err, errNoDecision) {
		return
	}
}
