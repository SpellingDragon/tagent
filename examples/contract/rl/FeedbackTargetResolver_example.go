package rlexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/rl"
)

// evidenceResolver is the canonical feedback attribution seam: it resolves the recorded
// identity behind one event key from the evidence it holds.
type evidenceResolver struct {
	targets map[string]rl.FeedbackTarget
}

// ResolveFeedbackTarget returns the recorded identity one event key names, and refuses a
// key its evidence does not hold by name.
func (r *evidenceResolver) ResolveFeedbackTarget(_ context.Context, eventKey string) (rl.FeedbackTarget, error) {
	target, ok := r.targets[eventKey]
	if !ok {
		return rl.FeedbackTarget{}, rl.ErrFeedbackEventUnknown
	}
	return target, nil
}

var _ rl.FeedbackTargetResolver = (*evidenceResolver)(nil)

// ExampleFeedbackTargetResolver resolves the decision and attempt one event key names and
// reports a key the evidence does not hold as unknown instead of a resolved target.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func ExampleFeedbackTargetResolver() {
	ctx := context.Background()
	resolver := &evidenceResolver{targets: map[string]rl.FeedbackTarget{
		"abc123": {EventKey: "abc123", DecisionID: event.DecisionID("decision-1"), AttemptID: event.AttemptID("attempt-1"), ModelVersion: "2026-01"},
	}}

	target, err := resolver.ResolveFeedbackTarget(ctx, "abc123")
	if err != nil || target.AttemptID != "attempt-1" {
		return
	}
	if _, err := resolver.ResolveFeedbackTarget(ctx, "missing"); err != rl.ErrFeedbackEventUnknown {
		return
	}
}
