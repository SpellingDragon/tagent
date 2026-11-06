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
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCompressionFilesTicket pins product-filing also files a ticket.
//   - a tool turn commits, a product files, the log holds blob and ticket
//   - the second boot rebuilds the chain
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressionFilesTicket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	round := 0
	ticketModel := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		if round == 1 {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				ID: "call-probe", Type: "function", Function: model.FunctionDefinitionParam{Name: "probe", Arguments: []byte("{}")},
			}}}}}, Done: true}
		}
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}, Done: true}
	}}
	first, err := Assemble(ctx, &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{
		deliveryTestAgent: {Model: ticketModel, Tools: []trpctool.CallableTool{&staticProbeTool{name: "probe"}}},
	}, WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("assemble the first run: %v", err)
	}
	hostCtx, stopHost := context.WithCancel(ctx)
	host := deliveryTestHost(hostCtx, first, newDeliveryTestSender(nil))
	if _, err := host.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-compress-ticket",
		Address:      deliveryTestAddress("session-compress-ticket"),
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive one tool turn"}},
	}); err != nil {
		t.Fatalf("drive the committing turn: %v", err)
	}
	stopHost()
	session := deliveryTestAddress("session-compress-ticket").SessionID
	product := event.CompressProductFor("request-1", "probe", "-seg/fold-1", nil, "folded")
	if err := first.fileCompressProduct(ctx, session, product); err != nil {
		t.Fatalf("file the compression product: %v", err)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("drain the first run: %v", err)
	}
	held := probeLogKeys(t, root, "tagent/task/v1/snapshot:", "tagent/task/v1/ticket:")
	if !held[0] || !held[1] {
		t.Fatalf("the log holds blob=%v ticket=%v, want both beside the product", held[0], held[1])
	}
	second, err := Assemble(ctx, &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{
		deliveryTestAgent: {Model: ticketModel, Tools: []trpctool.CallableTool{&staticProbeTool{name: "probe"}}},
	}, WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("restart beside the ticket: %v", err)
	}
	if report := second.Recovery(); len(report.Tasks) != 1 || !report.Tasks[0].Recovered {
		t.Fatalf("recovery report = %+v, want the one chain rebuilt", report.Tasks)
	}
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
