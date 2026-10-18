package evolution

import (
	"context"
	"os"
	"testing"

	evo "github.com/SpellingDragon/tagent/evolution"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
//
// TestRealLLMJudgeArtifact is the gated real-artifact check of the LLM judge over a real model endpoint:
// - it runs ModelJudge against the real endpoint and demands a decidable artifact, so a zero output is refused
// - the gate is the environment: without the three TAGENT_REAL_JUDGE variables the case skips as unprovisioned
func TestRealLLMJudgeArtifact(t *testing.T) {
	baseURL := os.Getenv("TAGENT_REAL_JUDGE_BASE_URL")
	apiKey := os.Getenv("TAGENT_REAL_JUDGE_API_KEY")
	modelName := os.Getenv("TAGENT_REAL_JUDGE_MODEL")
	if baseURL == "" || apiKey == "" || modelName == "" {
		t.Skip("real LLM judge artifact not provisioned: set TAGENT_REAL_JUDGE_BASE_URL, TAGENT_REAL_JUDGE_API_KEY and TAGENT_REAL_JUDGE_MODEL (deferred to 09.5)")
	}

	judge := evo.NewModelJudge(openai.New(modelName, openai.WithAPIKey(apiKey), openai.WithBaseURL(baseURL)), 0.5)
	judgment, err := judge(context.Background(), evo.Review{
		Version: "real/artifact",
		Turns:   1,
		Joined: []evo.Feedback{
			{Stamp: "real/artifact", Consumed: "real/artifact", Evidence: "fb/real-1"},
		},
		Guardrail: evo.Guardrail{Enabled: true},
	})
	if err != nil {
		t.Fatalf("the judge digests failures into an undecided judgment and never errors, got %v", err)
	}
	if !judgment.Decidable {
		t.Fatalf("a provisioned real judge must produce a decidable artifact, zero output is refused: %#v", judgment)
	}
}
