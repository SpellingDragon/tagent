package assembly

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestBatchCheckpointMatchesPostBatchProjection pins whole-batch survivor semantics.
// - several judgments execute in one batch after earlier products were applied
// - checkpoint survivors equal the final projection and bound anchored decoding
// - a checkpointed chain hydrates its survivors by key and replays only the tail past them, so the anchored boot decodes fewer bodies than the full walk
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestBatchCheckpointMatchesPostBatchProjection(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	consumer := run.compress
	sessions := consumer.sessions
	consumer.sessions = nil
	run.ageFold.setExecutor(nil)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-batch-checkpoint"}
	commitWindowTurns(t, ctx, run, addr, 12)
	consumer.sessions = sessions
	if len(consumer.pending(addr.SessionID)) < 2 {
		t.Fatal("the fixture did not accumulate a multi-product batch")
	}
	if err := consumer.Execute(ctx, addr.SessionID); err != nil {
		t.Fatalf("execute the accumulated batch: %v", err)
	}
	if count := countFoldProducts(t, ctx, run); count < 2 {
		t.Fatalf("the fixture filed %d product(s), want a multi-product batch", count)
	}
	projection, _, carried := sessions.ProjectionOf(ctx, addr.SessionID)
	if !carried {
		t.Fatal("the batch registered no projection")
	}
	entries := projection.Entries()
	survivors := latestFoldCheckpointSurvivors(t, ctx, run)
	if len(survivors) != len(entries) {
		t.Errorf("the checkpoint holds %d survivor(s), want %d post-batch entries", len(survivors), len(entries))
	} else {
		for i, survivor := range survivors {
			entry := entries[i]
			if survivor.Key != entry.Key || survivor.Type != entry.Type || survivor.Summary != entry.Summary || survivor.Content != entry.Content || survivor.Time != entry.Time.UnixMilli() {
				t.Errorf("survivor %d differs from the post-batch projection: %+v versus %+v", i, survivor, entry)
			}
		}
	}
	anchored := windowChainRun(t, ctx)
	_, report, err := anchored.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild the accumulated batch: %v", err)
	}
	if report.Anchors != 1 || report.Fallbacks != 0 {
		t.Fatalf("the rebuild used %d anchor(s) beside %d fallback(s), want the one checkpoint of the batch", report.Anchors, report.Fallbacks)
	}
	whole := windowChainRun(t, ctx)
	_, _, full, err := whole.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("walk the chain in full: %v", err)
	}
	if report.Decoded >= full.Decoded {
		t.Fatalf("the anchored rebuild decoded %d event(s), want fewer than the %d of the full walk", report.Decoded, full.Decoded)
	}
}

// latestFoldCheckpointSurvivors reads the newest checkpoint-bearing product.
func latestFoldCheckpointSurvivors(t *testing.T, ctx context.Context, run *Run) []anchorEntry {
	t.Helper()
	var survivors []anchorEntry
	found := false
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if found || typ != event.CompressProductType {
			return nil
		}
		stored, held, err := run.tier.Segments.GetEvent(ctx, key)
		if err != nil || !held {
			return err
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return err
		}
		evt := body.Event()
		evt.Key = stored.Key
		product, stated := compressProductOf(evt)
		if stated && len(product.Checkpoint()) > 0 {
			if err := json.Unmarshal(product.Checkpoint(), &survivors); err != nil {
				return err
			}
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("read the newest checkpoint: %v", err)
	}
	if !found {
		t.Fatal("the batch filed no checkpoint-bearing product")
	}
	return survivors
}

// countFoldProducts counts the products filed by one executed batch.
func countFoldProducts(t *testing.T, ctx context.Context, run *Run) int {
	t.Helper()
	count := 0
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(_ event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if typ == event.CompressProductType {
			count++
		}
		return nil
	}); err != nil {
		t.Fatalf("count the filed products: %v", err)
	}
	return count
}
