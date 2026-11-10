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
	"github.com/SpellingDragon/tagent/api/model"
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

// deliverySlicePayloadOf answers the raw payload one checkpoint fact states for
// the delivery slice, so a check hands the carrier's bytes to the restore entry
// the boot calls.
func deliverySlicePayloadOf(t *testing.T, ctx context.Context, run *Run, key event.EventID) []byte {
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
		if slice.Name == checkpointSliceDelivery {
			return []byte(slice.Payload)
		}
	}
	t.Fatalf("the checkpoint %s states no delivery slice", key)
	return nil
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
			host.dropHeld(addr.SessionID, identity)
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
		if state, known := host.deliveryStateOf(addr.SessionID, identity); !known || state != deliveryStateSent {
			t.Fatalf("the source account states %s = (%q, %v), want the settled state it wrote", identity, state, known)
		}
	}
	owed := append([]string(nil), identities[settleCount:]...)
	for _, identity := range owed {
		if state, known := host.deliveryStateOf(addr.SessionID, identity); !known || state != heldStateOf(deliveryReasonApproval) {
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
	if got, known := recovered.deliveryStateOf(addr.SessionID, identity); !known || got != heldStateOf(deliveryReasonRetrying) {
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
	owed := 0
	for _, account := range h.deliveryAccounts {
		owed += len(account.held)
	}
	return owed
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
		host.noteDeliveryState(addr.SessionID, identity, deliveryStateSent)
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
	host.noteDeliveryState(addr.SessionID, open, heldStateOf(deliveryReasonApproval))
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

// declaredTestObligation commits the fact an ended turn answers with: the
// assistant body beside the delivery declaration it carries, through the very
// commit entry the loop lands its output on.
func declaredTestObligation(t *testing.T, ctx context.Context, run *Run, addr event.Address, key event.EventID, body string) {
	t.Helper()
	if _, err := run.commitFactChecked(ctx, event.Event{
		Key:       key,
		Type:      event.TypeAgentOutput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleAssistant, Content: body},
		Owner:     addr, Source: addr, Target: addr,
		Meta: map[string]string{
			deliveryOwedMeta:          "1",
			deliveryTargetAgentMeta:   string(addr.AgentID),
			deliveryTargetSessionMeta: string(addr.SessionID),
			deliveryLineageMeta:       "user",
		},
	}); err != nil {
		t.Fatalf("commit the owed declaration of %s: %v", key, err)
	}
}

// owedOfIdentity reports the owed entry one carrier states for one identity.
func owedOfIdentity(wire deliverySliceWire, identity string) (deliverySliceOwed, bool) {
	for _, owed := range wire.Owed {
		if owed.Identity == identity {
			return owed, true
		}
	}
	return deliverySliceOwed{}, false
}

// TestCheckpointKeepsDeclaredObligationAcrossCut pins a declaration no send has settled yet.
//   - positive: a declared body no send settled when the cut was filed is stated by the carrier and delivered exactly once by the restart
//   - negative: a declaration the account settled before the cut is stated nowhere, so the carrier never re-owes a body the chain already delivered
//   - consumer: the obligation reaches the sender through restorePendingDeliveries and restoreObligation, the boot path the host runs
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointKeepsDeclaredObligationAcrossCut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-declared-across-cut")
	openKey := event.EventID("declared-across-cut-open")
	settledKey := event.EventID("declared-across-cut-settled")
	openBody := "the body declared before its send"
	settledBody := "the body declared and delivered before the cut"
	openIdentity := hostDeliveryFreshPrefix + string(openKey)
	settledIdentity := hostDeliveryFreshPrefix + string(settledKey)

	first := snapshotTestRun(t, ctx, storage)
	host := deliveryTestHost(ctx, first, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	declaredTestObligation(t, ctx, first, addr, openKey, openBody)
	declaredTestObligation(t, ctx, first, addr, settledKey, settledBody)
	settledDelivery := plugin.Delivery{
		Content: settledBody, Lineage: "user",
		Target: event.Address{AgentID: addr.AgentID, SessionID: addr.SessionID},
	}
	if err := host.writeDeliveryState(ctx, settledIdentity, settledDelivery, deliveryStateSent, time.Time{}); err != nil {
		t.Fatalf("settle the delivered declaration: %v", err)
	}
	host.dropHeld(addr.SessionID, settledIdentity)
	key, filed, err := first.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint across the declarations = (%t, %v)", filed, err)
	}
	wire, carried := checkpointDeliveryOf(t, ctx, first, key)
	if !carried {
		t.Fatal("the checkpoint states no delivery slice at all")
	}
	owed, stated := owedOfIdentity(wire, openIdentity)
	if !stated {
		t.Fatalf("the carrier states %+v, want the declaration of %s the cut covers and no send settled", wire.Owed, openIdentity)
	}
	if owed.Content != openBody || owed.Lineage != "user" || owed.Agent != string(addr.AgentID) || owed.Session != string(addr.SessionID) {
		t.Fatalf("the carrier states %+v for %s, want the body, target and lineage the declaration carries", owed, openIdentity)
	}
	if owed.State != "" {
		t.Fatalf("the carrier states %s = %q, want no settlement state beside a declaration no send answered", openIdentity, owed.State)
	}
	if _, settledStated := owedOfIdentity(wire, settledIdentity); settledStated {
		t.Fatalf("the carrier states the settled %s, want the cut alone to cover a delivered body", settledIdentity)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the process that stopped beside the open declaration: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, storage)
	sender := newDeliveryTestSender(nil)
	recovered := deliveryTestHost(ctx, restarted, sender)
	if err := recovered.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the restored delivery account: %v", err)
	}
	if err := recovered.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the account the cut stated: %v", err)
	}
	defer deliveryTestStopRetries(recovered)
	if got := sender.attemptCount(openBody); got != 1 {
		t.Fatalf("the restart asked the channel to take the declared body %d time(s), want the one send the declaration owes", got)
	}
	if got := sender.attemptCount(settledBody); got != 0 {
		t.Fatalf("the restart asked the channel to take the settled body %d time(s), want none beside the receipt the chain keeps", got)
	}
	if state, known := recovered.deliveryStateOf(addr.SessionID, openIdentity); !known || state != deliveryStateSent {
		t.Fatalf("the restart states %s = (%q, %v), want the receipt of the send it made", openIdentity, state, known)
	}
	if _, stated := recovered.parkedDelivery(openIdentity); stated {
		t.Fatalf("the restart parked %s for a decision, want a declaration delivered and never an entry nobody approves", openIdentity)
	}
	if got := parkedCount(recovered); got != 0 {
		t.Fatalf("the restart holds %d obligation(s), want none beside the declaration it delivered", got)
	}
	if _, err := restarted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the restart that delivered the declaration: %v", err)
	}

	third := snapshotTestRun(t, ctx, storage)
	again := newDeliveryTestSender(nil)
	whole := deliveryTestHost(ctx, third, again)
	if err := whole.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the account of the third process: %v", err)
	}
	if err := whole.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the settled account: %v", err)
	}
	defer deliveryTestStopRetries(whole)
	if got := again.attemptCount(openBody); got != 0 {
		t.Fatalf("the third process asked the channel to take the delivered body %d time(s), want none: the receipt the second process wrote is the settlement", got)
	}
	if _, err := third.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the third process: %v", err)
	}
}

// TestDeliveryAccountStatesTheLiveSend pins the declaration account against the live path that settles it.
//   - the body a segment delivers enters the account inside the commit that declared it, and the send that settles it leaves no entry behind
//   - a cut beside a delivered turn states nothing, so a restart never sends a body the chain already delivered
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestDeliveryAccountStatesTheLiveSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-live-send-account")
	run := snapshotTestRun(t, ctx, storage)
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, run, sender)
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	invocation := event.InvocationID("invocation-live-send-account")
	body := "the body the live path delivers"
	if _, err := run.commitFactChecked(ctx, event.Event{
		Key:          "input-live-send-account",
		Type:         event.TypeExternalInput,
		Timestamp:    time.Now(),
		InvocationID: invocation,
		Message:      model.Message{Role: model.RoleUser, Content: "hello"},
		Owner:        addr, Source: addr, Target: addr,
	}); err != nil {
		t.Fatalf("commit the input the turn consumed: %v", err)
	}
	outputKey := event.EventID("output-live-send-account")
	if _, err := run.commitFactChecked(ctx, event.Event{
		Key:          outputKey,
		Type:         event.TypeAgentOutput,
		Timestamp:    time.Now(),
		InvocationID: invocation,
		Message:      model.Message{Role: model.RoleAssistant, Content: body},
		Owner:        addr, Source: addr, Target: addr,
		Meta: map[string]string{
			deliveryOwedMeta:          "1",
			deliveryTargetAgentMeta:   string(addr.AgentID),
			deliveryTargetSessionMeta: string(addr.SessionID),
			deliveryLineageMeta:       "user",
			event.MetaKeyInputEvents:  `["input-live-send-account"]`,
		},
	}); err != nil {
		t.Fatalf("commit the turn output: %v", err)
	}
	identity := hostDeliveryFreshPrefix + string(outputKey)
	carrier := func() deliverySliceWire {
		wire, carried := checkpointDeliveryOf(t, ctx, run, mustCheckpoint(t, ctx, run))
		if !carried {
			t.Fatal("the checkpoint states no delivery slice at all")
		}
		return wire
	}
	declared, stated := owedOfIdentity(carrier(), identity)
	if !stated {
		t.Fatalf("the carrier of the process states no obligation of %s, want the one the output fact declared inside its own landing unit", identity)
	}
	if declared.Content != body || declared.State != "" {
		t.Fatalf("the carrier states %+v for %s, want the body and no settlement state beside a declaration no send answered", declared, identity)
	}
	host.deliverFresh(ctx, addr, sender, "user", agent.Turn{InvocationID: invocation, Terminal: model.KindCompleted})
	if got := sender.attemptCount(body); got != 1 {
		t.Fatalf("the live path asked the channel to take the body %d time(s), want the one send of a segment closing", got)
	}
	if _, stated := owedOfIdentity(carrier(), identity); stated {
		t.Fatalf("the carrier still states the delivered %s, want the settlement to answer the declaration", identity)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the process: %v", err)
	}
}

// mustCheckpoint files one checkpoint of the run and answers its fact key.
func mustCheckpoint(t *testing.T, ctx context.Context, run *Run) event.EventID {
	t.Helper()
	key, filed, err := run.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the checkpoint = (%t, %v), want a filed fact", filed, err)
	}
	return key
}

// TestCrossSessionObligationsStayInTheirTargetSlice pins the target-session shard of the delivery account.
//
//   - positive: the carrier of the chain an obligation targets states it, and restoring those payloads redrives it exactly once
//   - negative: a chain's carrier states nothing of the chain beside it, and no slice files another chain's state
//   - the same check reds on a global account, which states both obligations in each chain's carrier
//   - the whole account states both obligations, so the exclusion above is the shard and never an emptied account
//   - the two chains interleave their rounds, so a carrier reading the whole account states work a chain beside it was writing at the same time
//   - consumer: Run.CheckpointSession files the carrier, and StateRegistry.RestoreState hands each payload to Host.restoreDeliverySlice on the boot path
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestCrossSessionObligationsStayInTheirTargetSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	source := event.Address{AgentID: deliveryTestAgent, SessionID: "session-shard-source"}
	kept := event.Address{AgentID: deliveryTestAgent, SessionID: "session-shard-kept"}
	other := event.Address{AgentID: deliveryTestAgent, SessionID: "session-shard-other"}
	run := windowChainRun(t, ctx, storage)
	run.stopCheckpointFiler()
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	if err := host.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the delivery account on the state seam: %v", err)
	}
	keptBody := "the body owed to the kept chain"
	otherBody := "the body owed to the chain beside"
	keptIdentity := declareObligationTargeting(t, ctx, run, source, kept, "shard-kept-declaration", keptBody)
	otherIdentity := declareObligationTargeting(t, ctx, run, source, other, "shard-other-declaration", otherBody)
	commitSessionTurns(t, ctx, run, kept, "shard-kept", 2)
	commitSessionTurns(t, ctx, run, other, "shard-other", 3)

	carrierOf := func(addr event.Address, foreign, own, body string) event.EventID {
		t.Helper()
		key, filed, err := run.CheckpointSession(ctx, addr)
		if err != nil || !filed {
			t.Fatalf("file the carrier of %s = (%t, %v), want a filed fact", addr.SessionID, filed, err)
		}
		wire, carried := checkpointDeliveryOf(t, ctx, run, key)
		if !carried {
			t.Fatalf("the carrier of %s states no delivery slice, want the obligation that chain is owed", addr.SessionID)
		}
		owed, stated := owedOfIdentity(wire, own)
		if !stated {
			t.Fatalf("the carrier of %s owes %d obligation(s) and none is %s", addr.SessionID, len(wire.Owed), own)
		}
		if owed.Session != string(addr.SessionID) || owed.Content != body {
			t.Fatalf("the carrier of %s states %+v, want the body declared against that chain", addr.SessionID, owed)
		}
		if _, bleeding := owedOfIdentity(wire, foreign); bleeding {
			t.Fatalf("the carrier of %s states %s, the obligation of the chain beside it: the account of a chain is what that chain is owed", addr.SessionID, foreign)
		}
		return key
	}
	keptKey := carrierOf(kept, otherIdentity, keptIdentity, keptBody)
	otherKey := carrierOf(other, keptIdentity, otherIdentity, otherBody)

	wholePayload, err := host.deliverySlice()
	if err != nil {
		t.Fatalf("state the whole open account: %v", err)
	}
	var whole deliverySliceWire
	if err := json.Unmarshal(wholePayload, &whole); err != nil {
		t.Fatalf("read the whole open account: %v", err)
	}
	for _, identity := range []string{keptIdentity, otherIdentity} {
		if _, stated := owedOfIdentity(whole, identity); !stated {
			t.Fatalf("the whole account owes no entry for %s, want the obligation a chain carrier is expected to state", identity)
		}
	}

	restarted := windowChainRun(t, ctx, storage)
	restarted.stopCheckpointFiler()
	recovered := deliveryTestHost(ctx, restarted, newDeliveryTestSender(nil))
	if err := recovered.registerDeliverySlice(); err != nil {
		t.Fatalf("mount the restored delivery account: %v", err)
	}
	sender := recovered.sender.(*deliveryTestSender)
	if err := restarted.states.RestoreState(ctx, []event.CheckpointSlice{
		{Name: checkpointSliceDelivery, Payload: deliverySlicePayloadOf(t, ctx, run, keptKey)},
		{Name: checkpointSliceDelivery, Payload: deliverySlicePayloadOf(t, ctx, run, otherKey)},
	}); err != nil {
		t.Fatalf("restore the delivery carriers of the two chains: %v", err)
	}
	if err := recovered.restoreEventifiedDeliveries(ctx); err != nil {
		t.Fatalf("restore the account the carriers state: %v", err)
	}
	defer deliveryTestStopRetries(recovered)
	for _, want := range []struct {
		identity string
		session  event.SessionID
		body     string
	}{
		{keptIdentity, kept.SessionID, keptBody},
		{otherIdentity, other.SessionID, otherBody},
	} {
		if got := sender.attemptCount(want.body); got != 1 {
			t.Fatalf("the boot sent %q %d time(s), want exactly once", want.body, got)
		}
		if state, known := recovered.deliveryStateOf(want.session, want.identity); !known || state != deliveryStateSent {
			t.Fatalf("the boot states %s on chain %s = (%q, %v), want the receipt of the send it made", want.identity, want.session, state, known)
		}
		if _, parked := recovered.parkedDelivery(want.identity); parked {
			t.Fatalf("the boot parked %s for a decision, want the delivered obligation closed", want.identity)
		}
	}
	if _, misfiled := recovered.deliveryStateOf(kept.SessionID, otherIdentity); misfiled {
		t.Fatalf("the slice of chain %s states %s, the obligation of the chain beside it: a state is filed under the chain it is owed to", kept.SessionID, otherIdentity)
	}
	if _, misfiled := recovered.deliveryStateOf(other.SessionID, keptIdentity); misfiled {
		t.Fatalf("the slice of chain %s states %s, the obligation of the chain beside it: a state is filed under the chain it is owed to", other.SessionID, keptIdentity)
	}
	if got := parkedCount(recovered); got != 0 {
		t.Fatalf("the boot holds %d obligation(s), want none beside two delivered obligations", got)
	}
}
