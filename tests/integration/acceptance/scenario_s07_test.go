package acceptance

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// scenarioOriginalBody is the original text the reference drive commits.
var scenarioOriginalBody = []byte("first line\nsecond line\n")

// TestScenario07OwnerIsolation drives the original store and the projection presentation.
// - two owners keep their own reference over identical bytes without overwriting one another, and one blob serves both
// - the readback returns the committed bytes
// - a projection entry carries the full original exactly when its recorded decision is a full-text one
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestScenario07OwnerIsolation(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S07")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	ownerA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	ownerB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
	refA := ledger.OriginalRef{Key: "result-a", Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
	refB := ledger.OriginalRef{Key: "result-a", Owner: ownerB, Format: "text/markdown", RetentionPolicy: "keep"}

	store := ledger.NewMemoryOriginalStore(ledger.WithRetentionWindow("keep", 24*time.Hour))
	for _, ref := range []ledger.OriginalRef{refA, refB} {
		if err := store.Put(ctx, ref, scenarioOriginalBody); err != nil {
			t.Fatalf("put the original of %s/%s: %v", ref.Owner.AgentID, ref.Format, err)
		}
	}
	registered := store.References(refA.Key)
	observations["distinct_owner_references"] = len(registered)
	bothKept := len(registered) == 2
	for _, want := range []ledger.OriginalRef{refA, refB} {
		matched := false
		for _, ref := range registered {
			if ref.Owner == want.Owner && ref.Format == want.Format && ref.RetentionPolicy == want.RetentionPolicy {
				matched = true
			}
		}
		if !matched {
			bothKept = false
		}
	}
	observations["owner_references_nonoverwriting"] = bothKept
	observations["same_bytes_content_equal"] = store.BlobCount() == 1
	recalled := store.Get(ctx, refA.Key)
	observations["original_bytes_equal_readback"] = recalled.Outcome == ledger.RecallFound &&
		bytes.Equal(recalled.Body, scenarioOriginalBody)
	refs = append(refs, string(refA.Key))

	small := replay.ProjectionEntry{Key: "entry-small", Type: event.TypeAgentOutput, Summary: "small", Content: "short body", Time: time.Unix(0, 0)}
	largeBody := strings.Repeat("大结果正文。", 300)
	large := replay.ProjectionEntry{Key: "entry-large", Type: event.TypeAgentOutput, Summary: "large", Content: largeBody, Time: time.Unix(60, 0)}
	projection := replay.NewReplayProjection()
	projection.Append(ctx, small)
	projection.Append(ctx, large)
	assembled, err := projection.AssembleBudget(ctx, 200)
	if err != nil {
		t.Fatalf("assemble the projection: %v", err)
	}
	iff := len(assembled) == 2
	full, other := 0, 0
	for _, entry := range assembled {
		original, kept := projection.Original(entry.Key)
		if !kept {
			iff = false
			continue
		}
		if entry.Content == original {
			full++
		} else {
			other++
		}
	}
	observations["presentation_full_text_iff_full_room"] = iff && full > 0 && other > 0
	refs = append(refs, string(small.Key), string(large.Key))

	EmitScenarioRecord(t, scenario, refs, observations)
}
