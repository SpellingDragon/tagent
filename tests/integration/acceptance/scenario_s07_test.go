package acceptance

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/memory/replica"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// scenarioOriginalBody is the original text the reference drive commits.
var scenarioOriginalBody = []byte("first line\nsecond line\n")

// scenarioReplicaBody is the original text the replica drive materialises.
var scenarioReplicaBody = []byte("replica body: exactly these bytes reach the replica and every segment of it")

// TestScenario07ReplicaAndOwnerIsolation drives the original store, the replica materialiser and the projection presentation.
// - two owners keep their own reference over identical bytes without overwriting one another, and one blob serves both
// - the readback returns the committed bytes
// - a reclaimed replica is written again from its original at the same stable path
// - a projection entry carries the full original exactly when its recorded decision is a full-text one
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestScenario07ReplicaAndOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S07")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	ownerA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	ownerB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
	scopeA := ledger.ReadScope{Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
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

	root := t.TempDir()
	if err := store.Put(ctx, ledger.OriginalRef{Key: "replica-a", Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}, scenarioReplicaBody); err != nil {
		t.Fatalf("put the replica original: %v", err)
	}
	replicaRef := ledger.OriginalRef{Key: "replica-a", Owner: ownerA, Format: "tool-output", RetentionPolicy: "keep"}
	materializer, err := replica.NewMaterializer(root, store, scopeA)
	if err != nil {
		t.Fatalf("open the materializer: %v", err)
	}
	first, err := materializer.Materialize(ctx, replicaRef, "tool-shell")
	if err != nil {
		t.Fatalf("materialise the replica: %v", err)
	}
	cleaner, err := replica.NewReplicaCleaner(root)
	if err != nil {
		t.Fatalf("open the replica cleaner: %v", err)
	}
	if err := cleaner.CleanupStartup(ctx); err != nil {
		t.Fatalf("reclaim the replica: %v", err)
	}
	reclaimed := false
	if _, err := os.Lstat(first.Path); os.IsNotExist(err) {
		reclaimed = true
	}
	again, err := materializer.Materialize(ctx, replicaRef, "tool-shell")
	if err != nil {
		t.Fatalf("materialise the replica again: %v", err)
	}
	rebuilt, err := os.ReadFile(again.Path)
	observations["replica_materialized_after_cleanup"] = reclaimed && again.Path == first.Path &&
		err == nil && bytes.Equal(rebuilt, scenarioReplicaBody)
	refs = append(refs, string(replicaRef.Key))

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
		decision, recorded := projection.Decision(entry.Key)
		original, kept := projection.Original(entry.Key)
		if !recorded || !kept {
			iff = false
			continue
		}
		switch decision.Mode {
		case replay.PresentationFull:
			full++
		default:
			other++
		}
		if (entry.Content == original) != (decision.Mode == replay.PresentationFull) {
			iff = false
		}
	}
	observations["presentation_full_text_iff_decision_full"] = iff && full > 0 && other > 0
	refs = append(refs, string(small.Key), string(large.Key))

	EmitScenarioRecord(t, scenario, refs, observations)
}
