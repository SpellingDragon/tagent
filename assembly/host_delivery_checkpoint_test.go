// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// checkpointDeliveryOf reads the delivery slice one checkpoint fact states, and
// reports whether the fact carries one at all.
func checkpointDeliveryOf(t *testing.T, ctx context.Context, run *Run, key event.EventID) (deliverySliceWire, bool) {
	t.Helper()
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the checkpoint %s back: %v", key, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the fact %s states no readable checkpoint", key)
	}
	for _, slice := range checkpoint.Slices {
		if slice.Name != checkpointSliceDelivery {
			continue
		}
		var wire deliverySliceWire
		if err := json.Unmarshal([]byte(slice.Payload), &wire); err != nil {
			t.Fatalf("the delivery slice states no readable account: %v", err)
		}
		return wire, true
	}
	return deliverySliceWire{}, false
}

// declaredIdentities counts how often one identity appears in the delivered
// declarations of one recovery, so a check reads "discovered once" as a number.
func declaredIdentities(declared []deliverySettlement) map[string]int {
	out := make(map[string]int, len(declared))
	for _, declaration := range declared {
		out[declaration.identity]++
	}
	return out
}

// TestDeliverySettlementAndCheckpointConcurrent pins the consistent cut.
//   - a settlement before the cut is in the snapshot and one after it is covered by the replay, so a restart states each obligation once
//   - a settled obligation is never declared owed again, and an owed one is never dropped
//   - the account the source process answered is the account the restart answers, whatever order the settles and the cuts took
//
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestDeliverySettlementAndCheckpointConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	const settleCount, holdCount = 4, 4
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-settle-cut")
	first := windowChainRun(t, ctx, storage)
	host := deliveryTestHost(ctx, first, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	bodyOf := func(identity string) string { return "the body of " + identity }
	identities := make([]string, 0, settleCount+holdCount)
	for index := 0; index < settleCount+holdCount; index++ {
		identity := fmt.Sprintf("delivery-cut-%02d", index)
		identities = append(identities, identity)
		delivery := plugin.Delivery{Content: bodyOf(identity), Lineage: "user", Target: addr}
		if err := host.writeDeliveryState(ctx, identity, delivery, heldStateOf(deliveryReasonApproval), time.Now()); err != nil {
			t.Fatalf("seed the hold of %s: %v", identity, err)
		}
		host.parkHeld(identity, delivery, time.Now())
	}
	// One side settles obligations of its own while the other side cuts the
	// framework state: the cut and the settlement commit under the same lock, so
	// no obligation is stated half settled and none is lost between the two.
	var wg sync.WaitGroup
	settled := append([]string(nil), identities[:settleCount]...)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for _, identity := range settled {
			delivery := plugin.Delivery{Content: bodyOf(identity), Lineage: "user", Target: addr}
			if err := host.writeDeliveryState(ctx, identity, delivery, deliveryStateSent, time.Time{}); err != nil {
				t.Errorf("settle %s: %v", identity, err)
			}
			host.dropHeld(identity)
		}
	}()
	go func() {
		defer wg.Done()
		for round := 0; round < settleCount+holdCount; round++ {
			if _, _, err := first.Checkpoint(ctx); err != nil {
				t.Errorf("file checkpoint %d: %v", round, err)
			}
		}
	}()
	wg.Wait()
	for _, identity := range settled {
		if state, known := host.deliveryStateOf(identity); !known || state != deliveryStateSent {
			t.Fatalf("the source account states %s = (%q, %v), want the settled state it wrote", identity, state, known)
		}
	}
	owed := append([]string(nil), identities[settleCount:]...)
	for _, identity := range owed {
		if state, known := host.deliveryStateOf(identity); !known || state != heldStateOf(deliveryReasonApproval) {
			t.Fatalf("the source account states %s = (%q, %v), want the hold it stands in", identity, state, known)
		}
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the process that settled beside the cut: %v", err)
	}

	restarted := windowChainRun(t, ctx, storage)
	recovered := deliveryTestHost(ctx, restarted, newDeliveryTestSender(nil))
	if err := recovered.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the restored delivery account: %v", err)
	}
	if err := recovered.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the delivery account: %v", err)
	}
	defer deliveryTestStopRetries(recovered)
	for _, identity := range settled {
		terminal, settled, err := recovered.deliverySettledOnChain(ctx, identity)
		if err != nil {
			t.Fatalf("read the settlement of %s back: %v", identity, err)
		}
		if !settled || terminal != deliveryStateSent {
			t.Fatalf("the restart reads the settled %s = (%q, %v), want the sent receipt the chain keeps once", identity, terminal, settled)
		}
		if _, parked := recovered.parkedDelivery(identity); parked {
			t.Fatalf("the restart parked the settled %s again, want a settled obligation never owed a second time", identity)
		}
	}
	for _, identity := range owed {
		held, parked := recovered.parkedDelivery(identity)
		if !parked {
			t.Fatalf("the restart owes %s no delivery, want the account the cut stated", identity)
		}
		if held.delivery.Content != bodyOf(identity) || held.delivery.Target != addr || held.delivery.Lineage != "user" {
			t.Fatalf("the restart holds %+v for %s, want the body, target and lineage the cut stated", held.delivery, identity)
		}
	}
	if got := parkedCount(recovered); got != holdCount {
		t.Fatalf("the restart holds %d obligation(s), want the %d the source still owed", got, holdCount)
	}
}

// TestCheckpointCarriesOpenObligationAcrossFold pins the embedded obligation.
//   - the obligation still open when the chain folds is stated in the checkpoint with its body, target, lineage, hold and attempt
//   - a restart discovers it and processes it exactly once, and a second restart finds it settled instead of owing it again
//
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestCheckpointCarriesOpenObligationAcrossFold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-open-obligation")
	identity := "delivery-open-obligation"
	body := "the body the fold must not swallow"
	first := windowChainRun(t, ctx, storage)
	host := deliveryTestHost(ctx, first, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	heldAt := time.Now().Add(-time.Minute).Round(time.Millisecond)
	delivery := plugin.Delivery{Content: body, Lineage: "user", Target: addr}
	if err := host.writeDeliveryState(ctx, identity, delivery, heldStateOf(deliveryReasonRetrying), heldAt); err != nil {
		t.Fatalf("state the open obligation: %v", err)
	}
	host.parkHeld(identity, delivery, heldAt)
	host.retryConsumer().schedule(identity, delivery, 1)
	commitWindowTurns(t, ctx, first, addr, 12)
	if count := countFoldProducts(t, ctx, first); count == 0 {
		t.Fatal("the fixture folded nothing, so the open obligation was never carried across a fold")
	}
	if _, filed, err := first.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the checkpoint of the folded chain = (%t, %v), want a filed fact", filed, err)
	}
	key, _ := first.CheckpointFiled()
	wire, carried := checkpointDeliveryOf(t, ctx, first, key)
	if !carried {
		t.Fatal("the checkpoint states no delivery slice")
	}
	owedOf := func(wire deliverySliceWire) *deliverySliceOwed {
		for at := range wire.Owed {
			if wire.Owed[at].Identity == identity {
				return &wire.Owed[at]
			}
		}
		return nil
	}
	owed := owedOf(wire)
	if owed == nil {
		t.Fatalf("the checkpoint states no owed obligation of %s, want the one still open at the fold", identity)
	}
	if owed.Content != body || owed.Lineage != "user" || owed.Agent != string(addr.AgentID) || owed.Session != string(addr.SessionID) {
		t.Fatalf("the checkpoint states %+v, want the body, lineage and target the obligation was opened with", owed)
	}
	if owed.State != heldStateOf(deliveryReasonRetrying) || !owed.HeldAt.Equal(heldAt) {
		t.Fatalf("the checkpoint states %+v, want the hold it stands in and the moment it was parked", owed)
	}
	attempted := false
	for _, attempt := range wire.Attempts {
		if attempt.Identity == identity && attempt.Attempt == 1 && attempt.Content == body {
			attempted = true
		}
	}
	if !attempted {
		t.Fatalf("the checkpoint states the attempts %+v, want the redrive the account still owes", wire.Attempts)
	}
	deliveryTestStopRetries(host)
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the process that folded beside the obligation: %v", err)
	}

	restarted := windowChainRun(t, ctx, storage)
	recovered := deliveryTestHost(ctx, restarted, newDeliveryTestSender(nil))
	if err := recovered.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the restored delivery account: %v", err)
	}
	recovered.deliveryBackoff = time.Hour
	if err := recovered.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the folded account: %v", err)
	}
	defer deliveryTestStopRetries(recovered)
	_, declared, attempts, err := recovered.deliveryRecoveryFacts(ctx)
	if err != nil {
		t.Fatalf("read the recovery facts of the restart: %v", err)
	}
	if got := declaredIdentities(declared)[identity]; got != 1 {
		t.Fatalf("the restart declares the open obligation %d time(s), want it discovered exactly once", got)
	}
	if got := parkedCount(recovered); got != 1 {
		t.Fatalf("the restart holds %d obligation(s), want the one the fold left open", got)
	}
	if got, known := recovered.deliveryStateOf(identity); !known || got != heldStateOf(deliveryReasonRetrying) {
		t.Fatalf("the restart states %s = (%q, %v), want the hold the checkpoint stated", identity, got, known)
	}
	if attempts[identity] != 1 {
		t.Fatalf("the restart resumes the redrive at attempt %d, want the 1 the account recorded", attempts[identity])
	}
	if err := recovered.writeDeliveryState(ctx, identity, delivery, deliveryStateSent, time.Time{}); err != nil {
		t.Fatalf("settle the discovered obligation: %v", err)
	}
	deliveryTestStopRetries(recovered)
	if _, err := restarted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the restart that processed the obligation: %v", err)
	}

	third := windowChainRun(t, ctx, storage)
	sender := newDeliveryTestSender(nil)
	again := deliveryTestHost(ctx, third, sender)
	if err := again.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the account of the third process: %v", err)
	}
	if err := again.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the settled account: %v", err)
	}
	defer deliveryTestStopRetries(again)
	if _, _, _, err := again.deliveryRecoveryFacts(ctx); err != nil {
		t.Fatalf("read the recovery facts of the third process: %v", err)
	}
	if terminal, settled, err := again.deliverySettledOnChain(ctx, identity); err != nil || !settled || terminal != deliveryStateSent {
		t.Fatalf("the third process reads %s = (%q, %v, %v), want the settled receipt", identity, terminal, settled, err)
	}
	if _, parked := again.parkedDelivery(identity); parked {
		t.Fatal("the third process owes the obligation a delivery again, want it processed exactly once")
	}
	if got := parkedCount(again); got != 0 {
		t.Fatalf("the third process holds %d obligation(s), want none beside the settled receipt", got)
	}
	if got := sender.attemptCount(body); got != 0 {
		t.Fatalf("the third process asked the channel to take the settled body %d time(s), want none", got)
	}
}

// parkedCount reports how many obligations one host still owes a delivery.
func parkedCount(h *Host) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pendingDeliveries)
}

// checkpointDeliveryPayload files one checkpoint of the run and answers the
// payload of its delivery slice, so a check reads the bytes the carrier states.
func checkpointDeliveryPayload(t *testing.T, ctx context.Context, run *Run) []byte {
	t.Helper()
	key, _, err := run.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("file the framework checkpoint: %v", err)
	}
	if key == "" {
		t.Fatal("the cut filed no checkpoint fact")
	}
	stored, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the checkpoint %s back: %v", key, err)
	}
	checkpoint, stated := event.CheckpointOf(stored)
	if !stated {
		t.Fatalf("the fact %s states no readable checkpoint", key)
	}
	for _, slice := range checkpoint.Slices {
		if slice.Name == checkpointSliceDelivery {
			return slice.Payload
		}
	}
	t.Fatal("the checkpoint states no delivery slice")
	return nil
}

// TestCheckpointBodyBoundedByTheOpenAccount pins the settled set as the cut.
//   - the delivery slice a checkpoint states is the open account alone: a settled obligation the cut covers states no token
//   - the payload is therefore the same bytes beside one settled obligation and beside hundreds of them, so the carrier bounds the open work and never the history
//   - an obligation still owed is still carried with its body, so the bound is the open account and never an emptied account
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointBodyBoundedByTheOpenAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := windowChainRun(t, ctx)
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	defer deliveryTestStopRetries(host)
	addr := deliveryTestAddress("session-bounded-carrier")
	bodyOf := func(identity string) string { return "the body of " + identity }
	settle := func(identity string) {
		t.Helper()
		delivery := plugin.Delivery{Content: bodyOf(identity), Lineage: "user", Target: addr}
		if err := host.writeDeliveryState(ctx, identity, delivery, deliveryStateSent, time.Time{}); err != nil {
			t.Fatalf("settle %s: %v", identity, err)
		}
	}
	settle("delivery-bounded-0000")
	brief := checkpointDeliveryPayload(t, ctx, run)
	for index := 1; index < 300; index++ {
		identity := fmt.Sprintf("delivery-bounded-%04d", index)
		host.noteDeliveryState(identity, deliveryStateSent)
	}
	settle("delivery-bounded-0300")
	long := checkpointDeliveryPayload(t, ctx, run)
	if !bytes.Equal(brief, long) {
		t.Fatalf("the carrier states %d bytes beside 300 settled obligations and %d bytes beside one, want the open account alone (a carrier that grows with the settled history bounds nothing)", len(long), len(brief))
	}
	if len(long) == 0 {
		t.Fatal("the carrier states no delivery slice at all")
	}
	open := "delivery-bounded-open"
	host.noteDeliveryState(open, heldStateOf(deliveryReasonApproval))
	host.parkHeld(open, plugin.Delivery{Content: bodyOf(open), Lineage: "user", Target: addr}, time.Now())
	settle("delivery-bounded-open-marker")
	heldPayload := checkpointDeliveryPayload(t, ctx, run)
	if !strings.Contains(string(heldPayload), open) {
		t.Fatalf("the carrier states %s, want the obligation the account still owes", heldPayload)
	}
	if strings.Contains(string(heldPayload), "delivery-bounded-0100") {
		t.Fatal("the carrier states a settled obligation, want the settled set stated by the cut alone")
	}
	if len(heldPayload) <= len(long) {
		t.Fatalf("the carrier states %d bytes beside one open obligation and %d beside none, want the open account carried", len(heldPayload), len(long))
	}
}
