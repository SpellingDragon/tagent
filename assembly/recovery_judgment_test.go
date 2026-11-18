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

// productStride counts how many times one index walk hands each compress product
// to its reader.
func productStride(t *testing.T, ctx context.Context, run *Run) map[event.EventID]int {
	t.Helper()
	out := map[event.EventID]int{}
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, typ string, _ event.SessionID) error {
		if typ != event.CompressProductType {
			return nil
		}
		out[key]++
		return nil
	}); err != nil {
		t.Fatalf("walk the index: %v", err)
	}
	return out
}

// requireOneProductPerStep requires every product key one walk handed to its
// reader to have been handed once.
func requireOneProductPerStep(t *testing.T, stride map[event.EventID]int, walk string) {
	t.Helper()
	if len(stride) == 0 {
		t.Fatalf("the %s walk read no product, so it proves nothing", walk)
	}
	for key, times := range stride {
		if times != 1 {
			t.Fatalf("the %s walk handed the compression product %s to its reader %d times, want once", walk, key, times)
		}
	}
}

// TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint pins one product per judgment across a process boundary.
// - the walk hands every product once (control)
// - every judgment answers exactly one product identity
// - a restart over the same root reads one product per judgment back (negative: the second answer was a reading of the chain, never a fact on it)
//
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-interleaved-judgment"}

	judged := windowChainRun(t, ctx, storage)
	judged.compress.sessions = nil
	judged.ageFold.setExecutor(nil)
	commitWindowTurns(t, ctx, judged, addr, 12)
	if len(pendingIdentities(judged.compress, addr.SessionID)) == 0 {
		t.Fatal("the fixture filed no compression judgment, so the restart proves nothing")
	}
	if _, err := judged.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the judging process: %v", err)
	}

	restarted := windowChainRun(t, ctx, storage)
	if len(pendingIdentities(restarted.compress, addr.SessionID)) == 0 {
		t.Fatal("the restarted run rebuilt no judgment from the chain, so the interleaving proves nothing")
	}
	restarted.stopCheckpointFiler()
	commitWindowFact(t, ctx, restarted, addr, windowEvent(addr, "j1-in", "inv-interleaved", "the commit that executes the pending judgment", event.TypeExternalInput))
	if count := countFoldProducts(t, ctx, restarted); count == 0 {
		t.Fatal("the fixture filed no product beside the executed judgment, so the interleaving proves nothing")
	}

	requireOneProductPerStep(t, productStride(t, ctx, restarted), "control")

	products := productsByRequest(t, ctx, restarted)
	if len(products) == 0 {
		t.Fatal("the chain states no product, so the judgment read proves nothing")
	}
	for request, count := range products {
		if count != 1 {
			t.Fatalf("the judgment %s answers %d product steps, want one", request, count)
		}
	}
	if _, err := restarted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the restarted process: %v", err)
	}

	again := windowChainRun(t, ctx, storage)
	again.stopCheckpointFiler()
	answers := productsByRequest(t, ctx, again)
	if len(answers) == 0 {
		t.Fatal("the restart read no product back, so the counterexample proves nothing")
	}
	for request, count := range answers {
		if count != 1 {
			t.Fatalf("the restart reads %d products answering the judgment %s, want one", count, request)
		}
	}
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
