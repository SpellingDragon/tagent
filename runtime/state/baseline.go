package state

import (
	"errors"
	"fmt"
)

// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
//
// ErrBaselineAdvanced reports a candidate discarded at the commit point: the
// baseline it recorded at construction was advanced by a newer publication, so
// the candidate is stale and never overwrites the newer publication.
var ErrBaselineAdvanced = errors.New("state: the candidate baseline was advanced by a newer publication")

// baselineOf composes the version baseline of one publication state: the pair
// of the generation in effect and the fingerprint it published. A candidate
// records this value at construction, so the pair names exactly the publication
// state the candidate was compiled against.
func baselineOf(generation uint64, fingerprint string) string {
	return fmt.Sprintf("%d:%s", generation, fingerprint)
}

// Baseline returns the version baseline a candidate records at construction.
// The commit point re-checks the recorded value and discards the candidate if a
// publication advanced it while the candidate was being constructed.
func (p *Publisher) Baseline() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return baselineOf(p.effective.Generation, p.effective.Fingerprint)
}

// recheckBaseline compares the baseline a candidate recorded at construction
// with the baseline in effect and names the discard of the candidate once a
// publication advanced it.
func (p *Publisher) recheckBaseline(cand Candidate) error {
	current := baselineOf(p.effective.Generation, p.effective.Fingerprint)
	if cand.Plan.Plan.Baseline != current {
		return fmt.Errorf("%w: candidate %q beside effective %q", ErrBaselineAdvanced, cand.Plan.Plan.Baseline, current)
	}
	return nil
}
