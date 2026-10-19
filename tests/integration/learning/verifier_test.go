package learning

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/rl"
)

// TestOpenDomainVerificationReward drives an open-domain episode through the verifier port and the reward account:
// - a compliant episode reaches the pass score and a constraint violation produces a low, failed verdict
// - each verdict is recorded as a reward instance carrying the value, pass outcome, reasons and rule version
// - the recorded verdict survives a JSON round trip, so verification stays a reward record and not a fourth record kind
// - an injected judge replaces the offline heuristic behind the same port
// 契约: docs/wiki/rl/rl-architecture.md#training-export
func TestOpenDomainVerificationReward(t *testing.T) {
	ctx := context.Background()
	verifier, err := rl.NewHeuristicVerifier()
	if err != nil {
		t.Fatalf("build the verifier: %v", err)
	}
	rewards := rl.NewRewardLedger()
	consumer, err := rl.NewVerificationConsumer(verifier, rewards, rl.VerificationRuleHeuristic)
	if err != nil {
		t.Fatalf("build the consumer: %v", err)
	}

	compliant := rl.EpisodeRecord{
		Schema:   rl.SchemaCurrent,
		ID:       "ep-open-compliant",
		RootID:   "ep-open-compliant",
		Sessions: []event.SessionID{"session-open"},
		Lineage:  rl.LineageKnown,
	}
	passed, err := consumer.Consume(ctx, "dec-open-1", "att-open-1", compliant)
	if err != nil {
		t.Fatalf("consume the compliant episode: %v", err)
	}
	if !passed.Passed || passed.Value != 1 {
		t.Fatalf("compliant record = %+v, want a passing full score", passed)
	}

	violating := compliant
	violating.ID = "ep-open-violating"
	violating.RootID = "ep-open-violating"
	violating.DependsOn = []rl.EpisodeDependency{{
		EpisodeID: "ep-open-violating",
		DependsOn: "ep-open-violating",
		Kind:      rl.DependencyHandoff,
		Lineage:   rl.LineageUnknown,
	}}
	failed, err := consumer.Consume(ctx, "dec-open-2", "att-open-2", violating)
	if err != nil {
		t.Fatalf("consume the violating episode: %v", err)
	}
	if failed.Passed {
		t.Fatalf("violating record = %+v, want a failed verdict", failed)
	}
	if failed.Value > 0.5 || len(failed.Reasons) == 0 {
		t.Fatalf("violating record = %+v, want a low score beside its reasons", failed)
	}

	latest, held := rewards.Latest("dec-open-2", "att-open-2")
	if !held || latest.State != rl.RewardRated || latest.InstanceID == "" || latest.Revision < 1 {
		t.Fatalf("latest reward = (%+v, %v), want one citable rated instance", latest, held)
	}
	encoded, err := json.Marshal(failed)
	if err != nil {
		t.Fatalf("marshal the reward record: %v", err)
	}
	var decoded rl.RewardRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal the reward record: %v", err)
	}
	if decoded.Passed != failed.Passed || decoded.Value != failed.Value || len(decoded.Reasons) != len(failed.Reasons) {
		t.Fatalf("decoded reward = %+v, want the recorded verdict read back", decoded)
	}

	judge := func(context.Context, rl.EpisodeRecord) (rl.Verification, error) {
		return rl.Verification{Score: 0.2, Reasons: []string{"the judge read an incomplete task"}}, nil
	}
	judged, err := rl.NewHeuristicVerifier(rl.WithLLMJudge(judge))
	if err != nil {
		t.Fatalf("build the judged verifier: %v", err)
	}
	judgedConsumer, err := rl.NewVerificationConsumer(judged, rewards, rl.VerificationRuleLLMJudge)
	if err != nil {
		t.Fatalf("build the judged consumer: %v", err)
	}
	judgedRecord, err := judgedConsumer.Consume(ctx, "dec-open-3", "att-open-3", compliant)
	if err != nil {
		t.Fatalf("consume through the judge: %v", err)
	}
	if judgedRecord.Passed || judgedRecord.Value != 0.2 || judgedRecord.RuleVersion != rl.VerificationRuleLLMJudge {
		t.Fatalf("judged record = %+v, want the judge's score under its own rule version", judgedRecord)
	}
}
