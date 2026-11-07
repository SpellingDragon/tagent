package assembly

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// TestRestartCompletionPointSequence pins ticket appears only at completion.
//   - step 2 (state restore in Assemble) and step 1 (events) file no ticket
//   - the completion point files exactly one snapshot and one ticket
//   - the next restart restores from that ticket
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestRestartCompletionPointSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	build := func() map[event.AgentID]NativeAgent {
		ticketModel := &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}, Done: true}
		}}
		return map[event.AgentID]NativeAgent{deliveryTestAgent: {Model: ticketModel}}
	}
	decl := &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}
	first, err := Assemble(ctx, decl, build(), WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("assemble (step 2): %v", err)
	}
	if held := probeLogKeys(t, root, "tagent/task/v1/snapshot:", "tagent/task/v1/ticket:"); held[0] || held[1] {
		t.Fatalf("step 2 wrote blob=%v ticket=%v, want neither before the completion point", held[0], held[1])
	}
	host := deliveryTestHost(ctx, first, newDeliveryTestSender(nil))
	if err := host.restoreUnconsumedEvents(ctx, nil); err != nil {
		t.Fatalf("step 1 (unconsumed events): %v", err)
	}
	if held := probeLogKeys(t, root, "tagent/task/v1/snapshot:", "tagent/task/v1/ticket:"); held[0] || held[1] {
		t.Fatalf("step 1 wrote blob=%v ticket=%v, want neither before the completion point", held[0], held[1])
	}
	first.CompleteRestartRecovery(ctx)
	if held := probeLogKeys(t, root, "tagent/task/v1/snapshot:", "tagent/task/v1/ticket:"); !held[0] || !held[1] {
		t.Fatalf("the completion point wrote blob=%v ticket=%v, want both", held[0], held[1])
	}
	if blobs := probeLogCount(t, root, "tagent/task/v1/snapshot:"); blobs != 1 {
		t.Fatalf("snapshot keys at the completion point = %d, want exactly one", blobs)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the first run: %v", err)
	}
	second, err := Assemble(ctx, decl, build(), WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("restart beside the completion-point ticket: %v", err)
	}
	second.CompleteRestartRecovery(ctx)
	if blobs := probeLogCount(t, root, "tagent/task/v1/snapshot:"); blobs != 1 {
		t.Fatalf("snapshot keys after the second completion point = %d, want exactly one (overwritten)", blobs)
	}
	if _, err := second.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the second run: %v", err)
	}
}

// probeLogCount answers how many keys the storage root holds under one
// prefix, the single-copy check the completion point counts through.
func probeLogCount(t *testing.T, root string, prefix string) int {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the tier for counting: %v", err)
	}
	pairs, err := ledger.TierFactLog(tier).Scan(ctx, prefix)
	if err != nil {
		t.Fatalf("count the prefix %s: %v", prefix, err)
	}
	return len(pairs)
}

// probeLogKeys answers, per prefix, whether the storage root holds at least
// one key under it — the independent read the ticket check looks through,
// taken through the tier's own fact face instead of the run's carrier.
func probeLogKeys(t *testing.T, root string, prefixes ...string) []bool {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	if err != nil {
		t.Fatalf("open the tier for probing: %v", err)
	}
	facts := ledger.TierFactLog(tier)
	held := make([]bool, len(prefixes))
	for i, prefix := range prefixes {
		pairs, err := facts.Scan(ctx, prefix)
		if err != nil {
			t.Fatalf("probe the prefix %s: %v", prefix, err)
		}
		held[i] = len(pairs) > 0
	}
	return held
}

// TestBatchCheckpointMatchesPostBatchProjection pins whole-batch survivor semantics.
// - several judgments execute in one batch after earlier products were applied
// - checkpoint survivors equal the final projection and bound anchored decoding
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
	if report.Anchors != 1 {
		t.Fatalf("the rebuild used %d anchor(s), want one", report.Anchors)
	}
	if report.Decoded != 8 {
		t.Fatalf("the anchored rebuild decoded %d event(s), want the post-batch bound of 8", report.Decoded)
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
