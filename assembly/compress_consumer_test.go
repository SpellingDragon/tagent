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

// TestBootFilesRecoveryTicket pins the boot completion point files the ticket.
//   - a tool turn commits; the boot files the ticket before service opens
//   - the log holds blob and ticket; the second boot rebuilds the chain
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestBootFilesRecoveryTicket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	build := func() (*snapshotTestModel, map[event.AgentID]NativeAgent) {
		ticketModel := &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}, Done: true}
		}}
		return ticketModel, map[event.AgentID]NativeAgent{
			deliveryTestAgent: {Model: ticketModel},
		}
	}
	_, agents := build()
	first, err := Assemble(ctx, &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}, agents, WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("assemble the first run: %v", err)
	}
	hostCtx, stopHost := context.WithCancel(ctx)
	host := deliveryTestHost(hostCtx, first, newDeliveryTestSender(nil))
	if _, err := host.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-boot-ticket",
		Address:      deliveryTestAddress("session-boot-ticket"),
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive one turn"}},
	}); err != nil {
		t.Fatalf("drive the committing turn: %v", err)
	}
	stopHost()
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the first run: %v", err)
	}
	held := probeLogKeys(t, root, "tagent/task/v1/snapshot:", "tagent/task/v1/ticket:")
	if !held[0] || !held[1] {
		t.Fatalf("the log holds blob=%v ticket=%v, want both from the boot completion point", held[0], held[1])
	}
	_, agents2 := build()
	second, err := Assemble(ctx, &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}, agents2, WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("restart beside the boot ticket: %v", err)
	}
	if _, err := second.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the second run: %v", err)
	}
	if blobs := probeLogCount(t, root, "tagent/task/v1/snapshot:"); blobs != 1 {
		t.Fatalf("snapshot keys after two boots = %d, want exactly the one current blob", blobs)
	}
}

// probeLogCount answers how many keys the storage root holds under one
// prefix, the single-copy check the boot ticket look counts through.
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
