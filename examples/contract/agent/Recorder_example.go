package agentexample

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// passRecorder is the canonical training-record face of the loop: it accepts the episode,
// decision and attempt records at the points those points observed them and counts what
// it wrote.
type passRecorder struct {
	episodes  int
	decisions int
	attempts  int
}

// RecordEpisode writes the episode record of one business process at its own boundary.
func (r *passRecorder) RecordEpisode(_ context.Context, _ event.EpisodeID, _ event.SessionID, _ time.Time) error {
	r.episodes++
	return nil
}

// RecordDecision writes the decision record of one model decision at its terminal, with
// the model view material that request ran on.
func (r *passRecorder) RecordDecision(_ context.Context, _ event.DecisionID, _ event.InvocationID, _ event.EpisodeID, _ []event.EventID, _, _ string, _ []string, _ time.Time) error {
	r.decisions++
	return nil
}

// RecordAttempt writes the attempt record of one observable model attempt, carrying the
// outcome the single model reduction derived.
func (r *passRecorder) RecordAttempt(_ context.Context, _ event.AttemptID, _ event.DecisionID, _ event.EpisodeID, _ model.Outcome) error {
	r.attempts++
	return nil
}

var _ agent.Recorder = (*passRecorder)(nil)

// ExampleRecorder drives one decision's recording: the episode the range starts, the
// decision it reduced and the attempt beside it. A write failure stays an observation
// and never reaches the model decision.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func ExampleRecorder() {
	ctx := context.Background()
	recorder := &passRecorder{}
	now := time.Now()

	if err := recorder.RecordEpisode(ctx, "episode-1", "session-1", now); err != nil {
		return
	}
	if err := recorder.RecordDecision(ctx, "decision-1", "inv-1", "episode-1",
		[]event.EventID{"event-1"}, "model-a", "prompt-1", []string{"read_file"}, now); err != nil {
		return
	}
	if err := recorder.RecordAttempt(ctx, "attempt-1", "decision-1", "episode-1",
		model.Outcome{Kind: model.KindCompleted}); err != nil {
		return
	}
}
