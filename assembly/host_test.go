package assembly

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestHandoffRedrivesStalledOwedRecords pins the send face of the hand-off as every obligation that still stands owed, not only the bodies one call registered.
//   - a body recorded owed whose send never happened is redriven by the next hand-off that meets it, and its record closes as sent
//   - a body the account already discharged is not sent a second time
//   - the turn commits its body under the notification's own invocation, and a host beside no outbound sender records the obligation without sending
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestHandoffRedrivesStalledOwedRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-handoff-redrive"}
	notice := task.Notice{TaskID: "task-handoff-redrive", Round: 1, Kind: task.NoticeSettled, Seq: 1}
	if _, err := run.Invoke(ctx, InvokeRequest{
		InvocationID: noticeInvocation(notice),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "answer the notification"}},
	}); err != nil {
		t.Fatalf("drive the follow-up turn: %v", err)
	}
	host := &Host{run: run, entry: "agent-a", pendingDeliveries: map[string]*heldDelivery{}}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside no sender: %v", err)
	}
	stalled := hostTestDeliveryRecords(t, ctx, host)
	if len(stalled) != 1 || stalled[0].Status != hostDeliveryOwed {
		t.Fatalf("records beside no sender = %+v, want the one obligation left owed", stalled)
	}
	channel := &hostTestChannel{}
	host.sender = channel
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside a sender: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 || got[0] != stalled[0].Content {
		t.Fatalf("bodies the hand-off sent = %q, want the one stalled obligation %q", got, stalled[0].Content)
	}
	if records := hostTestDeliveryRecords(t, ctx, host); len(records) != 1 || records[0].Status != hostDeliverySent {
		t.Fatalf("records beside the redrive = %+v, want the one obligation closed as sent", records)
	}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off a third time: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 {
		t.Fatalf("bodies after the discharged hand-off = %q, want the one body already sent", got)
	}
}

// TestDeliveredBodyNamesAnUnconfirmedReceipt pins the second result of a landed send whose receipt did not reach the account.
//   - positive: a send the channel accepted answers delivered beside a cause naming the missing receipt
//   - negative: the same send over a live account answers no cause at all
//   - the account is closed right behind the send, which is the window a stop between the two leaves
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveredBodyNamesAnUnconfirmedReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	host := &Host{run: run, entry: "agent-a", pendingDeliveries: map[string]*heldDelivery{}}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unconfirmed-receipt"}
	unconfirmed := &hostTestChannel{}
	unconfirmedCtx, unconfirmedCancel := context.WithCancel(ctx)
	unconfirmed.afterSend = unconfirmedCancel
	outcome, cause := host.deliverObligation(unconfirmedCtx, "delivery-fresh-unconfirmed",
		unconfirmed, plugin.Delivery{Content: "the body nobody confirmed", Lineage: "task", Target: addr})
	if outcome != deliverySent {
		t.Fatalf("outcome of the landed send = %v, want the delivered outcome", outcome)
	}
	if !strings.Contains(cause, "the delivery landed but its receipt did not") {
		t.Fatalf("cause of the landed send = %q, want the note naming the unconfirmed receipt", cause)
	}
	confirmed := &hostTestChannel{}
	outcome, cause = host.deliverObligation(ctx, "delivery-fresh-confirmed",
		confirmed, plugin.Delivery{Content: "the body the account confirmed", Lineage: "task", Target: addr})
	if outcome != deliverySent || cause != "" {
		t.Fatalf("outcome and cause of the confirmed send = %v, %q, want a delivered send beside no cause", outcome, cause)
	}
	if records := hostTestDeliveryRecords(t, ctx, host); len(records) != 1 || records[0].Status != hostDeliverySent || records[0].ID != "delivery-fresh-confirmed" {
		t.Fatalf("records beside the two sends = %+v, want the one receipt the live account took", records)
	}
}

// hostTestDeliveryRecords reads every delivery record the host's storage tier
// holds, ordered by identity so one check judges the same account however the
// tier answers a scan.
func hostTestDeliveryRecords(t *testing.T, ctx context.Context, host *Host) []hostDeliveryRecord {
	t.Helper()
	log := host.deliveryDurable()
	if log == nil {
		t.Fatalf("the assembled run opened no storage tier to read delivery records from")
	}
	held, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		t.Fatalf("scan the delivery records: %v", err)
	}
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]hostDeliveryRecord, 0, len(keys))
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(held[key], &record); err != nil {
			t.Fatalf("read the delivery record %s back: %v", key, err)
		}
		records = append(records, record)
	}
	return records
}

// hostTestChannel is the outbound face one check hands the host: it keeps every
// body it accepted and accepts all of them, so a redrive is counted rather than
// refused by the channel.
type hostTestChannel struct {
	// afterSend runs after one accepted body is recorded, so a check can close the
	// account behind a send that already landed.
	afterSend func()
	mu        sync.Mutex
	sent      []string
}

// Send records one accepted body and reports the delivery as landed.
func (c *hostTestChannel) Send(_ context.Context, _ event.Address, content string) error {
	c.mu.Lock()
	c.sent = append(c.sent, content)
	c.mu.Unlock()
	if c.afterSend != nil {
		c.afterSend()
	}
	return nil
}

// bodies reports the bodies the channel accepted, in the order it took them.
func (c *hostTestChannel) bodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}
