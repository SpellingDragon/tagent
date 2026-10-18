package rl

import (
	"context"
	"fmt"
	"sync"
)

// 契约: docs/wiki/rl/rl-architecture.md#http-api
//
// CaptureFeedbackResolver is the evidence seam of the feedback endpoint over one
// sealed capture's records: an event key is looked up in the stored decision and
// attempt records, and the identity the record carries is the binding the feedback
// is written under. A binding is never inherited from the most recent parent call.
type CaptureFeedbackResolver struct {
	source *CaptureExportSource

	mu      sync.Mutex
	loaded  bool
	loadErr error
	facts   []ExportedFact
}

// NewCaptureFeedbackResolver returns the resolver over one capture source. The source
// reads under the seal the writer confirmed, so attribution rests on the records that
// reached storage and never on bytes beside them.
func NewCaptureFeedbackResolver(source *CaptureExportSource) *CaptureFeedbackResolver {
	return &CaptureFeedbackResolver{source: source}
}

var _ FeedbackTargetResolver = (*CaptureFeedbackResolver)(nil)

// ResolveFeedbackTarget returns the decision, attempt and model version the record
// under one event key carries. A key the capture does not hold is
// ErrFeedbackEventUnknown, and a key whose records disagree on the identity is
// ErrFeedbackTargetAmbiguous.
func (r *CaptureFeedbackResolver) ResolveFeedbackTarget(ctx context.Context, eventKey string) (FeedbackTarget, error) {
	if err := ctx.Err(); err != nil {
		return FeedbackTarget{}, err
	}
	if err := r.load(ctx); err != nil {
		return FeedbackTarget{}, err
	}
	var resolved *FeedbackTarget
	for _, fact := range r.facts {
		if string(fact.EventKey) != eventKey {
			continue
		}
		if fact.DecisionID == "" {
			return FeedbackTarget{}, fmt.Errorf("%w: the record under %q carries no decision", ErrFeedbackTargetAmbiguous, eventKey)
		}
		version, err := modelVersionOfFact(fact)
		if err != nil {
			return FeedbackTarget{}, err
		}
		target := FeedbackTarget{EventKey: eventKey, DecisionID: fact.DecisionID, AttemptID: fact.AttemptID, ModelVersion: version}
		if resolved == nil {
			resolved = &target
			continue
		}
		if resolved.DecisionID != target.DecisionID || resolved.AttemptID != target.AttemptID || resolved.ModelVersion != target.ModelVersion {
			return FeedbackTarget{}, fmt.Errorf("%w: %q is recorded at decision %q and decision %q", ErrFeedbackTargetAmbiguous, eventKey, resolved.DecisionID, target.DecisionID)
		}
	}
	if resolved == nil {
		return FeedbackTarget{}, fmt.Errorf("%w: %q", ErrFeedbackEventUnknown, eventKey)
	}
	return *resolved, nil
}

// modelVersionOfFact reads the model version one recorded fact was served under:
// the version an attempt record declares beside the response it observed, or the
// version a decision record declares in its model view. A fact naming neither is
// unversioned and is bound unversioned rather than assumed to be the version in
// effect.
func modelVersionOfFact(fact ExportedFact) (string, error) {
	line, err := ParseLine(fact.Body)
	if err != nil {
		return "", err
	}
	switch {
	case line.Attempt != nil:
		return line.Attempt.ModelVersion, nil
	case line.Decision != nil:
		return line.Decision.ModelView.ModelVersion, nil
	}
	return "", nil
}

// load reads every fact of the sealed capture once and holds what it read, so one
// submission and a later one face the same evidence and the same refusal.
func (r *CaptureFeedbackResolver) load(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return r.loadErr
	}
	r.loaded = true
	partition := r.source.CaptureID()
	for offset := 0; ; {
		page, err := r.source.Page(ctx, partition, offset, MaxExportPageLimit)
		if err != nil {
			r.loadErr = err
			return err
		}
		if len(page) == 0 {
			return nil
		}
		r.facts = append(r.facts, page...)
		offset += len(page)
	}
}
