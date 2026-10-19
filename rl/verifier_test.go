package rl

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestHeuristicVerifierVerdicts pins the offline judging contract of one episode record:
// - a compliant episode reaches the pass score and names its observations
// - a self-cited dependency, a self-cited parent and a broken ancestor chain each lower the score below the pass score
// - an episode record with no identity scores zero
// - an injected judge replaces the heuristic and its score is checked against the range
// - a score outside the range and a pass score outside its range are refused by name
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestHeuristicVerifierVerdicts(t *testing.T) {
	ctx := context.Background()

	t.Run("a compliant episode passes", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		verdict, err := verifier.Verify(ctx, compliantEpisode("ep-root"))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if verdict.Score != 1 || !verdict.Passed {
			t.Fatalf("verdict = (%v, %v), want a full score that passes", verdict.Score, verdict.Passed)
		}
		if len(verdict.Reasons) == 0 {
			t.Fatalf("verdict = %+v, want the observations it rests on", verdict)
		}
		if verifier.RuleVersion() != VerificationRuleHeuristic {
			t.Fatalf("rule version = %q, want %q", verifier.RuleVersion(), VerificationRuleHeuristic)
		}
	})

	t.Run("a self-cited dependency scores below the pass score", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		record := compliantEpisode("ep-root")
		record.DependsOn = []EpisodeDependency{{
			EpisodeID: "ep-root",
			DependsOn: "ep-root",
			Kind:      DependencyChildCall,
			Lineage:   LineageUnknown,
		}}
		verdict, err := verifier.Verify(ctx, record)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if verdict.Passed {
			t.Fatalf("verdict = %+v, want a self citation to fail", verdict)
		}
		if verdict.Score > 0.5 {
			t.Fatalf("score = %v, want it lowered by the self citation", verdict.Score)
		}
	})

	t.Run("a self-cited parent scores below the pass score", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		record := compliantEpisode("ep-root")
		record.ParentID = "ep-root"
		verdict, err := verifier.Verify(ctx, record)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if verdict.Passed || verdict.Score > 0.5 {
			t.Fatalf("verdict = %+v, want a self parent to fail", verdict)
		}
	})

	t.Run("a broken ancestor chain scores below the pass score", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		record := compliantEpisode("ep-child")
		record.ParentID = "ep-parent"
		record.RootID = "ep-root"
		record.Ancestors = []event.EpisodeID{"ep-other", "ep-parent"}
		verdict, err := verifier.Verify(ctx, record)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if verdict.Passed {
			t.Fatalf("verdict = %+v, want the broken chain to fail", verdict)
		}
		if math.Abs(verdict.Score-0.7) > 1e-9 {
			t.Fatalf("score = %v, want one citation violation beside a full score", verdict.Score)
		}
	})

	t.Run("an absent identity scores zero", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		verdict, err := verifier.Verify(ctx, EpisodeRecord{Schema: SchemaCurrent})
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if verdict.Score != 0 || verdict.Passed {
			t.Fatalf("verdict = %+v, want zero beside a failed verdict", verdict)
		}
	})

	t.Run("an injected judge replaces the heuristic", func(t *testing.T) {
		judged := false
		judge := func(_ context.Context, record EpisodeRecord) (Verification, error) {
			judged = record.ID == "ep-judged"
			return Verification{Score: 0.4, Reasons: []string{"the task stayed incomplete"}}, nil
		}
		verifier, err := NewHeuristicVerifier(WithLLMJudge(judge))
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		verdict, err := verifier.Verify(ctx, compliantEpisode("ep-judged"))
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !judged {
			t.Fatalf("the judge did not read the episode")
		}
		if verdict.Score != 0.4 || verdict.Passed {
			t.Fatalf("verdict = %+v, want the judge's score beside a failed verdict", verdict)
		}
		if verifier.RuleVersion() != VerificationRuleLLMJudge {
			t.Fatalf("rule version = %q, want %q", verifier.RuleVersion(), VerificationRuleLLMJudge)
		}
	})

	t.Run("a judge score outside the range is refused", func(t *testing.T) {
		judge := func(context.Context, EpisodeRecord) (Verification, error) {
			return Verification{Score: 1.5}, nil
		}
		verifier, err := NewHeuristicVerifier(WithLLMJudge(judge))
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		if _, err := verifier.Verify(ctx, compliantEpisode("ep-range")); !errors.Is(err, ErrVerificationScoreRange) {
			t.Fatalf("score outside the range = %v, want ErrVerificationScoreRange", err)
		}
	})

	t.Run("a pass score outside its range is refused", func(t *testing.T) {
		if _, err := NewHeuristicVerifier(WithPassScore(1.5)); !errors.Is(err, ErrVerificationPassScore) {
			t.Fatalf("pass score above its range = %v, want ErrVerificationPassScore", err)
		}
		if _, err := NewHeuristicVerifier(WithPassScore(0)); !errors.Is(err, ErrVerificationPassScore) {
			t.Fatalf("pass score at zero = %v, want ErrVerificationPassScore", err)
		}
	})

	t.Run("a cancelled context is reported", func(t *testing.T) {
		verifier, err := NewHeuristicVerifier()
		if err != nil {
			t.Fatalf("build the verifier: %v", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := verifier.Verify(cancelled, compliantEpisode("ep-cancel")); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled verify = %v, want context.Canceled", err)
		}
	})
}

// TestVerificationConsumerRecordsReward pins the reward reuse of one verification:
// - a consumer without a verifier, without a reward account or without a rule version is refused by name
// - a verdict is recorded as a rated reward instance carrying its value, pass outcome, reasons and rule version
// - a second verdict takes the next revision while the earlier instance keeps the revision it was written under
// - a restored instance with a different verdict under the same identity is the named conflict
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestVerificationConsumerRecordsReward(t *testing.T) {
	ctx := context.Background()
	verifier, err := NewHeuristicVerifier()
	if err != nil {
		t.Fatalf("build the verifier: %v", err)
	}
	ledger := NewRewardLedger()

	if _, err := NewVerificationConsumer(nil, ledger, VerificationRuleHeuristic); !errors.Is(err, ErrVerifierMissing) {
		t.Fatalf("consumer without a verifier = %v, want ErrVerifierMissing", err)
	}
	if _, err := NewVerificationConsumer(verifier, nil, VerificationRuleHeuristic); !errors.Is(err, ErrVerificationAccountMissing) {
		t.Fatalf("consumer without an account = %v, want ErrVerificationAccountMissing", err)
	}
	if _, err := NewVerificationConsumer(verifier, ledger, ""); !errors.Is(err, ErrVerificationRuleMissing) {
		t.Fatalf("consumer without a rule version = %v, want ErrVerificationRuleMissing", err)
	}

	consumer, err := NewVerificationConsumer(verifier, ledger, VerificationRuleHeuristic)
	if err != nil {
		t.Fatalf("build the consumer: %v", err)
	}
	first, err := consumer.Consume(ctx, "dec-1", "att-1", compliantEpisode("ep-1"))
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if first.State != RewardRated || first.Value != 1 || !first.Passed {
		t.Fatalf("record = %+v, want a rated passing instance at the full score", first)
	}
	if first.RuleVersion != VerificationRuleHeuristic || len(first.Reasons) == 0 {
		t.Fatalf("record = %+v, want its rule version and reasons", first)
	}
	if first.InstanceID == "" || first.Revision != 1 {
		t.Fatalf("record = %+v, want a citable instance at the first revision", first)
	}

	second, err := consumer.Consume(ctx, "dec-1", "att-1", compliantEpisode("ep-1"))
	if err != nil {
		t.Fatalf("consume again: %v", err)
	}
	if second.Revision != 2 || second.InstanceID == first.InstanceID {
		t.Fatalf("second record = %+v, want the next revision with its own identity", second)
	}
	history := ledger.Records("dec-1", "att-1")
	if len(history) != 2 || history[0].Revision != 1 {
		t.Fatalf("history = %+v, want the earlier instance kept at its revision", history)
	}

	conflicting := first
	conflicting.Reasons = []string{"a different observation"}
	if err := ledger.Restore(conflicting); !errors.Is(err, ErrRewardConflict) {
		t.Fatalf("restore with a different verdict = %v, want ErrRewardConflict", err)
	}
	if err := ledger.Restore(first); err != nil {
		t.Fatalf("restore the same verdict: %v", err)
	}
}

// compliantEpisode is one episode record whose identity, session evidence,
// lineage references and dependency citations agree with each other.
func compliantEpisode(id event.EpisodeID) EpisodeRecord {
	return EpisodeRecord{
		Schema:    SchemaCurrent,
		ID:        id,
		RootID:    id,
		Sessions:  []event.SessionID{"session-1"},
		Lineage:   LineageKnown,
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
}
