// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package assembly

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// benchOwedRun assembles one run over the localfile tier rooted at root, the
// benchmark counterpart of snapshotTestRun that answers a *testing.B.
func benchOwedRun(b *testing.B, ctx context.Context, root string) *Run {
	b.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}}, Done: true}
	}}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	natives := map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}
	run, err := assemble(ctx, decl, natives, true, WithGovernance(snapshotTestGovernance{}), WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		b.Fatalf("assemble the benchmark run: %v", err)
	}
	return run
}

// seedOwedColdChain writes one held obligation per identity beside filler that
// pushes every settlement fact out of the hot window, so the recovery read
// discovers them from the durable chain. The facts stay pinned (resident) when
// pin is set and are unpinned to trim cold otherwise: the two shapes are the
// same-tree control the cold-read baseline compares.
func seedOwedColdChain(b *testing.B, ctx context.Context, root string, obligations int, pin bool) {
	b.Helper()
	run := benchOwedRun(b, ctx, root)
	addr := deliveryTestAddress("session-owed-cold-bench")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	for index := 0; index < obligations; index++ {
		identity := fmt.Sprintf("delivery-owed-bench-%d", index)
		delivery := plugin.Delivery{Content: fmt.Sprintf("owed body %d", index), Lineage: "user", Target: addr}
		if err := host.writeDeliveryState(ctx, identity, delivery, heldStateOf(deliveryReasonApproval), time.Now()); err != nil {
			b.Fatalf("seed owed obligation %d: %v", index, err)
		}
		if !pin {
			run.unpinFact(deliverySettlementKey(identity, heldStateOf(deliveryReasonApproval)))
		}
	}
	for index := 0; index < noWindowResidentFacts+64; index++ {
		if _, err := run.commitFactChecked(ctx, event.Event{
			Type: event.TypeFeedback, Timestamp: time.Now(), Owner: addr, Source: addr, Target: addr,
			Meta: map[string]string{event.MetaKeyEventSummary: fmt.Sprintf("cold filler %d", index)},
		}); err != nil {
			b.Fatalf("seed filler %d: %v", index, err)
		}
	}
	deliveryTestStopRetries(host)
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		b.Fatalf("close the seeding run: %v", err)
	}
}

// BenchmarkDeliveryOwedColdRead measures the cold recovery read of owed
// obligations outside the hot window, after a warm-up pass, in two same-tree
// shapes: pinned (the residency the closure keeps) and unpinned (the control).
// The pinned read must not degrade against the control: the residency keeps the
// owed facts readable without an independent ledger.
func BenchmarkDeliveryOwedColdRead(b *testing.B) {
	for _, shape := range []struct {
		name string
		pin  bool
	}{{"pinned", true}, {"unpinned", false}} {
		b.Run(shape.name, func(b *testing.B) {
			ctx := context.Background()
			warmRoot := b.TempDir()
			seedOwedColdChain(b, ctx, warmRoot, 16, shape.pin)
			warmRun := benchOwedRun(b, ctx, warmRoot)
			warmHost := deliveryTestHost(ctx, warmRun, newDeliveryTestSender(nil))
			if _, _, _, err := warmHost.deliveryRecoveryFacts(ctx); err != nil {
				b.Fatalf("warm-up read: %v", err)
			}
			deliveryTestStopRetries(warmHost)
			if _, err := warmRun.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
				b.Fatalf("close warm-up run: %v", err)
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				root := b.TempDir()
				seedOwedColdChain(b, ctx, root, 16, shape.pin)
				run := benchOwedRun(b, ctx, root)
				host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
				b.StartTimer()
				if _, _, _, err := host.deliveryRecoveryFacts(ctx); err != nil {
					b.Fatalf("cold recovery read: %v", err)
				}
				b.StopTimer()
				deliveryTestStopRetries(host)
				if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
					b.Fatalf("close measured run: %v", err)
				}
				b.StartTimer()
			}
		})
	}
}
