package rlexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/rl"
)

// episodeVerifier is the canonical open-domain judging port: one episode record in and
// one verdict out, judged from the observations the record carries rather than from a
// stored answer.
type episodeVerifier struct{}

// Verify judges one episode record and names the observations the verdict rests on.
func (episodeVerifier) Verify(_ context.Context, record rl.EpisodeRecord) (rl.Verification, error) {
	if record.ID == "" {
		return rl.Verification{Score: 0, Passed: false, Reasons: []string{"the episode identity is absent"}}, nil
	}
	return rl.Verification{Score: 1, Passed: true, Reasons: []string{"the episode identity is present"}}, nil
}

var _ rl.Verifier = episodeVerifier{}

// ExampleVerifier judges a compliant episode and a constraint violation through the same
// port: the compliant record reaches the pass score and the self-cited dependency is
// reported as a failed verdict rather than an error.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func ExampleVerifier() {
	ctx := context.Background()
	verifier, err := rl.NewHeuristicVerifier()
	if err != nil {
		return
	}
	compliant := rl.EpisodeRecord{
		Schema:   rl.SchemaCurrent,
		ID:       event.EpisodeID("episode-1"),
		RootID:   event.EpisodeID("episode-1"),
		Sessions: []event.SessionID{"session-1"},
		Lineage:  rl.LineageKnown,
	}
	if verdict, err := verifier.Verify(ctx, compliant); err != nil || !verdict.Passed {
		return
	}
	violating := compliant
	violating.DependsOn = []rl.EpisodeDependency{{
		EpisodeID: compliant.ID,
		DependsOn: compliant.ID,
		Kind:      rl.DependencyChildCall,
		Lineage:   rl.LineageUnknown,
	}}
	if verdict, err := verifier.Verify(ctx, violating); err != nil || verdict.Passed || verdict.Score >= 1 {
		return
	}
}
