package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
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
