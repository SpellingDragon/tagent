// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// productsByRequest counts the chain products answering each compression
// judgment, read from the durable chain rather than from the consumer.
func productsByRequest(t *testing.T, ctx context.Context, run *Run) map[event.EventID]int {
	t.Helper()
	out := map[event.EventID]int{}
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if typ != event.CompressProductType {
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
		if stated {
			out[product.Answers.Request]++
		}
		return nil
	}); err != nil {
		t.Fatalf("read the chain products: %v", err)
	}
	return out
}

// pendingIdentities lists the judgment identities one consumer holds unanswered.
func pendingIdentities(consumer *CompressionConsumer, session event.SessionID) map[event.EventID]bool {
	out := map[event.EventID]bool{}
	for _, request := range consumer.pending(session) {
		out[request.ID] = true
	}
	return out
}

// TestRestartExecutesTheChainJudgmentOnce pins the chain judgment a restart owes.
// - a judgment the chain holds with no product is pending again after a restart
// - the next commit of that chain executes it and files its product
// - each judgment answers at most one product however many commits follow
// - a restart over a chain whose judgments are answered offers none (negative)
//
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestRestartExecutesTheChainJudgmentOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-chain-judgment"}
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})

	first := windowChainRun(t, ctx, storage)
	first.compress.sessions = nil
	first.ageFold.setExecutor(nil)
	commitWindowTurns(t, ctx, first, addr, 12)
	filed := pendingIdentities(first.compress, addr.SessionID)
	if len(filed) == 0 {
		t.Fatal("the fixture filed no compression judgment, so the restart proves nothing")
	}
	if count := countFoldProducts(t, ctx, first); count != 0 {
		t.Fatalf("the first process filed %d product(s) beside the executor off, want none", count)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := windowChainRun(t, ctx, storage)
	rebuilt := pendingIdentities(restarted.compress, addr.SessionID)
	if len(rebuilt) == 0 {
		t.Fatal("the restarted run rebuilt no judgment from the chain, want the ones the first process filed")
	}
	for id := range rebuilt {
		if !filed[id] {
			t.Fatalf("the restarted run holds the judgment %s the first process never filed, want the chain's own judgments", id)
		}
	}
	if count := countFoldProducts(t, ctx, restarted); count != 0 {
		t.Fatalf("the restarted run filed %d product(s) before any commit, want the judgments pending", count)
	}

	commitWindowFact(t, ctx, restarted, addr, windowEvent(addr, "j1-in", "inv-judgment", "the commit that executes the pending judgment", event.TypeExternalInput))
	products := productsByRequest(t, ctx, restarted)
	executed := 0
	for request := range products {
		if rebuilt[request] {
			executed++
		}
	}
	if executed == 0 {
		t.Fatalf("the commit after the restart executed none of the %d judgments the chain holds, want the recovered judgment executed", len(rebuilt))
	}
	for request, count := range products {
		if count != 1 {
			t.Fatalf("the judgment %s answers %d products, want one", request, count)
		}
	}

	commitWindowFact(t, ctx, restarted, addr, windowEvent(addr, "j2-in", "inv-judgment", "the commit that must not judge again", event.TypeExternalInput))
	for request, count := range productsByRequest(t, ctx, restarted) {
		if count != 1 {
			t.Fatalf("the judgment %s answers %d products after the second commit, want one", request, count)
		}
	}
	if _, err := restarted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the restarted process: %v", err)
	}

	cleanRoot := t.TempDir()
	cleanStorage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: cleanRoot})
	cleanAddr := event.Address{AgentID: "agent-a", SessionID: "session-answered-judgments"}
	clean := windowChainRun(t, ctx, cleanStorage)
	commitWindowTurns(t, ctx, clean, cleanAddr, 12)
	if count := countFoldProducts(t, ctx, clean); count == 0 {
		t.Fatal("the answered chain filed no product, so the negative proves nothing")
	}
	if left := pendingIdentities(clean.compress, cleanAddr.SessionID); len(left) != 0 {
		t.Fatalf("the live chain left %d judgment(s) pending beside its executor on, want every judgment answered", len(left))
	}
	if _, err := clean.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the answered process: %v", err)
	}
	answered := windowChainRun(t, ctx, cleanStorage)
	if left := pendingIdentities(answered.compress, cleanAddr.SessionID); len(left) != 0 {
		t.Fatalf("the restart over an answered chain rebuilt %d pending judgment(s), want none", len(left))
	}
}
