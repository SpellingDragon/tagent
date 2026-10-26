package acceptance

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// scenarioS13Input is the input that opens the range the closed-set scenario
// measures, so the transcript part of its request is identifiable.
const scenarioS13Input = "闭合集场景的输入。"

// TestScenario13ContextClosedSet drives one request whose boundary carries every part of the fixed layout.
// - the static head opens the request with the fixed prompt and skill bodies, in structural order
// - the folded history summary leads the replayed projection
// - the dynamic environment block stands ahead of the transcript and closes the assembly
// - every message of the request belongs to exactly one part of the closed five-part set, and the set is whole
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func TestScenario13ContextClosedSet(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S13")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}

	projection := scenarioProjection(t, scenarioFourTurns())
	report, err := projection.FoldByAge(ctx, replay.AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("fold the projection by age: %v", err)
	}
	if !report.Triggered {
		t.Fatalf("fold report = %+v, want the four turns to pass the volume threshold", report)
	}

	run, provider, promptBody, skillBody := scenarioContextRun(t, projection)
	got := scenarioInvokeOnce(t, run, provider, "inv-scenario-13", scenarioS13Input)
	if len(got) == 0 {
		t.Fatal("the request carries no message")
	}

	parts := scenarioParts(t, got, scenarioS13Input, promptBody, skillBody)
	head := scenarioHeadOrder(got, promptBody, skillBody)
	environmentAt := scenarioIndex(parts, scenarioPartEnvironment)
	transcriptAt := scenarioIndex(parts, scenarioPartTranscript)

	observations := map[string]any{}
	observations["static_head_order"] = head
	observations["folded_summary_leads_projection"] = scenarioSummaryLeads(got, parts)
	observations["environment_block_before_transcript"] = environmentAt >= 0 && transcriptAt >= 0 && environmentAt < transcriptAt
	observations["part_count_closed"] = len(scenarioDistinct(parts))

	refs := []string{"inv-scenario-13"}
	for _, product := range report.Products {
		refs = append(refs, string(product.Key))
	}
	EmitScenarioRecord(t, scenario, refs, observations)
}

// scenarioSummaryLeads reports whether the folded history summary leads the
// projection material of one request: the first presented entry that is not the
// replay path's recovery marker is the summary the age fold minted.
func scenarioSummaryLeads(messages []model.Message, parts []string) bool {
	for index, part := range parts {
		if part != scenarioPartProjection && part != scenarioPartFoldedSummary {
			continue
		}
		if strings.HasPrefix(messages[index].Content, scenarioRecoveryPrefix) {
			continue
		}
		return part == scenarioPartFoldedSummary
	}
	return false
}

// scenarioHeadOrder reports the labels of the static head messages in the order
// they open the request: the prompt body and the skill body the fixed files name.
func scenarioHeadOrder(messages []model.Message, promptBody, skillBody string) []string {
	out := []string{}
	for _, message := range messages {
		switch {
		case message.Role != model.RoleSystem:
			return out
		case strings.HasPrefix(message.Content, scenarioEnvironmentPrefix):
			return out
		case strings.Contains(message.Content, promptBody):
			out = append(out, "prompt")
		case strings.Contains(message.Content, skillBody):
			out = append(out, "skill")
		default:
			return out
		}
	}
	return out
}
