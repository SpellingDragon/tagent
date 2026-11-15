package host

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// declaredSummaryBudget is the summary budget the consumer anchors declare: half
// the policy default, so a summary that answers the declaration is shorter than
// one that answers the default.
const declaredSummaryBudget = 1024

// longSummaryAnswer is the gist the scripted model answers a band summary with:
// longer than the declared budget, so a summary that carries it whole answers a
// bound other than the declared one.
var longSummaryAnswer = strings.Repeat("[10-20 08:00|gen:?] the settled line of the band\n", 60)

// historySummaryProducts reports the history-summary products one session chain
// holds, in commit order.
func historySummaryProducts(t *testing.T, run *assembly.Run, session event.SessionID) []compressProductFact {
	t.Helper()
	products, _ := compressProductsOf(t, run, session)
	out := make([]compressProductFact, 0, len(products))
	for _, product := range products {
		if strings.HasPrefix(string(product.Key), "-hist-") {
			out = append(out, product)
		}
	}
	return out
}

// TestCompressionSummaryObeysTheDeclaredBudget anchors the declared summary
// budget over the assembled runtime: a declaration naming summary_budget_bytes
// hands that bound to the fold, the instruction states it, and the product the
// execution files carries at most that many bytes while still carrying the model
// answer rather than the mechanical form.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionSummaryObeysTheDeclaredBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn(longSummaryAnswer)
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:                 "agent-a",
			Model:              "model-a",
			ReplayWindow:       100,
			SummaryBudgetBytes: declaredSummaryBudget,
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}), assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-summary-budget"}
	for turn := 0; turn < 8; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}
	summaries := historySummaryProducts(t, run, addr.SessionID)
	if len(summaries) == 0 {
		t.Fatal("the executed fold filed no history summary, so the declared budget was never read")
	}
	stated := false
	for _, request := range provider.recorded() {
		if _, summary := bandSummaryRequestMaterial(request); !summary {
			continue
		}
		if strings.Contains(request.Messages[0].Content, fmt.Sprintf("within %d bytes", declaredSummaryBudget)) {
			stated = true
		}
	}
	if !stated {
		t.Fatalf("no band summary request stated the declared %d-byte bound", declaredSummaryBudget)
	}
	for _, summary := range summaries {
		if len(summary.Text) > declaredSummaryBudget {
			t.Fatalf("history summary carries %d bytes, want at most the declared %d", len(summary.Text), declaredSummaryBudget)
		}
		if !strings.HasPrefix(summary.Text, "[10-20 08:00|gen:?] the settled line of the band") {
			t.Fatalf("history summary = %q, want the model answer cut to the declared bound rather than the mechanical form", summary.Text)
		}
	}
}

// TestCompressionSummaryAsksTheLeaseModel anchors the model face a summary asks
// over the assembled runtime: the summary is served by the agent's lease, a
// replacement installs the version the later summaries answer under, and the
// model the host contributed beside the lease is asked nothing.
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestCompressionSummaryAsksTheLeaseModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	first := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("[10-20 08:00|gen:?] the first version answered")
	}}
	second := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("[10-20 08:00|gen:?] the second version answered")
	}}
	contributed := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("[10-20 08:00|gen:?] the contributed model answered")
	}}
	lease, err := rl.NewModelLease(first, "model-a@v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: contributed},
	}, assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
		assembly.WithModelLease("agent-a", lease, assembly.WithLeaseRebuild(func(version string) (model.Model, error) {
			if version != "model-a@v2" {
				t.Fatalf("the rebuild was asked for version %q", version)
			}
			return second, nil
		})))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-summary-lease"}
	for turn := 0; turn < 8; turn++ {
		commitCompressTurn(t, ctx, run, addr, turn)
	}
	before := historySummaryProducts(t, run, addr.SessionID)
	if len(before) == 0 {
		t.Fatal("the executed fold filed no history summary, so the lease was never asked")
	}
	if !strings.Contains(before[len(before)-1].Text, "the first version answered") {
		t.Fatalf("history summary = %q, want the leased version's answer", before[len(before)-1].Text)
	}
	if quoted := contributed.recorded(); len(quoted) != 0 {
		t.Fatalf("the contributed model was asked %d time(s), want the lease to serve every summary", len(quoted))
	}

	if _, err := lease.Replace(ctx, "model-a@v2", func() (model.Model, error) {
		return second, nil
	}, func(rl.ModelIdentity, model.Model) error { return nil }); err != nil {
		t.Fatalf("replace the leased version: %v", err)
	}
	// A session folded after the replacement asks the version the replacement
	// installed: the lease is the agent's, so every summary it serves follows the
	// version in effect when the fold begins.
	after := event.Address{AgentID: "agent-a", SessionID: "session-summary-lease-after"}
	for turn := 20; turn < 28; turn++ {
		commitCompressTurn(t, ctx, run, after, turn)
	}
	installed := historySummaryProducts(t, run, after.SessionID)
	if len(installed) == 0 {
		t.Fatal("the folds after the replacement filed no history summary, so the installed version was never asked")
	}
	if !strings.Contains(installed[len(installed)-1].Text, "the second version answered") {
		t.Fatalf("history summary after the replacement = %q, want the version the replacement installed", installed[len(installed)-1].Text)
	}
	if strings.Contains(installed[len(installed)-1].Text, "the first version answered") {
		t.Fatalf("history summary after the replacement = %q, want the version in effect rather than the one it replaced", installed[len(installed)-1].Text)
	}
}
