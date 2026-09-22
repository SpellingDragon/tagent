package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// §2.8-E2 end-to-end: the durable store's lifecycle scanner is gated on the retention
// lease since open; the agent wires the guard and arms the lease from existing unacked
// inbox envelopes (protecting their prepared fact + receipt originals across the restart
// race), then releases them when the envelope is acked (dir-synced) so they resume
// normal age-based handling. This exercises the real arm/release orchestration + the
// gate-readiness linkage.
func TestRetention_ArmFromInboxAndReleaseOnAck(t *testing.T) {
	// 1. Durable store (real localfile backend) with an un-armed retention lease,
	//    exactly as buildSharedResource attaches it at open.
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)

	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(1, now-10*24*3600*1000) // an overdue unacked prepared fact
	receiptKey := memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "unacked input", Timestamp: now - 10*24*3600*1000,
	}))

	// 2. A durable inbox with one envelope prepared with that fact + reserved receipt.
	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "r1",
		State:     reliability.InboxStatePending,
		Messages:  []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e1"}`)}},
	})
	require.NoError(t, err)
	env, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NotNil(t, env)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))

	// 3. Bus over the same inbox dir, wired with the store guard. Before arming, the
	//    material is NOT protected and the gate is CLOSED (scanner would block).
	bus, err := NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.False(t, store.IsKeyProtected(factKey), "not yet armed → nothing protected")
	select {
	case <-lease.Ready():
		t.Fatal("gate must be closed before ArmRetentionFromInbox")
	default:
	}

	// 4. Arm from existing unacked material: fact + receipt protected, gate opens.
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "armed lease must protect the prepared fact original")
	require.True(t, store.IsKeyProtected(receiptKey), "armed lease must protect the receipt original")
	select {
	case <-lease.Ready():
	case <-time.After(time.Second):
		t.Fatal("gate must open after arm so the scanner can proceed")
	}

	// 5. Ack the envelope (credentialed receipt + ack, dir-synced) → exactly what was protected
	//    is released (no leak: keys become eligible for age-based handling again).
	//    §5.4: the receipt first demands a durable completion — the drain path freezes
	//    a stub payload at this leaf level, as the real protocol freezes the frozen one.
	require.NoError(t, bus.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
	require.NoError(t, bus.ConfirmDurable(path, reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(receiptKey)}))
	require.False(t, store.IsKeyProtected(factKey), "ack must release the fact original (§2.8, no leak)")
	require.False(t, store.IsKeyProtected(receiptKey), "ack must release the receipt original")
}

// TestRetention_QuarantineReleasesLease (resident-review-fixes 2.2): quarantine
// is a terminal envelope disposition exactly like Ack, so it MUST release the
// isolated envelope's retention holders. Before the fix, QuarantineEnvelope moved
// the file away but never released — the fact/receipt originals stayed leased
// forever (a lease hang) and could never be TTL/capacity-evicted. Fail-before:
// removing the wrapper's releaseRetention keeps IsKeyProtected true after isolate.
func TestRetention_QuarantineReleasesLease(t *testing.T) {
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(1, now-10*24*3600*1000) // overdue original
	receiptKey := memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "conflicting input", Timestamp: now - 10*24*3600*1000,
	}))

	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "rq", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"eq"}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))
	require.NoError(t, in.Close())

	bus, err := NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "armed fact original is protected")
	require.True(t, store.IsKeyProtected(receiptKey), "armed receipt original is protected")

	// Isolate the envelope as a terminal state.
	bus.QuarantineEnvelope(path, "deterministic conflict (test)")
	require.False(t, store.IsKeyProtected(factKey), "quarantine MUST release the fact original (§2.8, no lease hang)")
	require.False(t, store.IsKeyProtected(receiptKey), "quarantine MUST release the receipt original")

	// Once released the originals are destroyable again — TTL/capacity eviction
	// (which skips IsKeyProtected keys) can now reclaim them.
	require.NoError(t, store.DeleteEvent(factKey), "post-quarantine original must be evictable")
}

// itoa renders an int64 as JSON-number text for hand-building a prepared_fact payload.
func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// receiptedCrashEnvelope builds a real durable store+inbox holding ONE envelope that
// is receipted-but-not-acked (the crash window between RecordReceipt and Ack), with a
// prepared fact original + reserved receipt key. It reopens a fresh EventBus over the
// same dir and arms the §2.8 lease, returning everything the release tests assert on.
func receiptedCrashEnvelope(t *testing.T) (bus *EventBus, store *memory.FileSegmentStore, factKey, receiptKey int64) {
	t.Helper()
	kvDir := t.TempDir()
	kvStore, err := kv.NewLocalFileKV(kvDir)
	require.NoError(t, err)
	store, err = memory.NewFileSegmentStore(kvStore, nil, kvDir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())

	now := time.Now().UnixMilli()
	factKey = memory.NewSnowflakeEventKey(1, now-10*24*3600*1000)
	receiptKey = memory.NewSnowflakeEventKey(1, now)
	require.NoError(t, store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: "external_input",
		EventSummary: "unacked input", Timestamp: now - 10*24*3600*1000,
	}))

	inboxDir := t.TempDir()
	in, err := reliability.NewInbox(inboxDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "r1", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e1","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + itoa(factKey) + `}`)}))
	// §5.4: receipt only follows a durable completion (the state machine refuses
	// without one); the drain path freezes a stub payload at this leaf level.
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"completion_version":1}`)))
	// Crash window: receipt durable, ack NOT done. §5.4: the receipt carries the
	// reserved-key credential (the drain path stands in for the fact-chain verify).
	require.NoError(t, in.RecordReceipt(path, reliability.ReceiptCredential{ReceiptKey: tagentevent.FormatEventKey(receiptKey)}))
	require.NoError(t, in.Close())

	bus, err = NewReliableEventBus(inboxDir)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.True(t, store.IsKeyProtected(factKey), "precondition: receipted-but-unacked material is armed at open")
	return bus, store, factKey, receiptKey
}

// Major#1 (§2.8): a receipted-but-unacked envelope swept at claim time must be acked
// AND its protected originals released. Before the fix, nextClaimable removed receipted
// items in place WITHOUT releaseRetention → the fact/receipt keys leaked (still
// protected forever). Fail-before: pre-fix IsKeyProtected stays true after TryPull.
func TestRetention_ReceiptedSweepReleasesLease(t *testing.T) {
	bus, store, factKey, receiptKey := receiptedCrashEnvelope(t)

	got := bus.TryPull() // claimDurable sweeps the settled envelope: Ack + release
	require.Empty(t, got, "a receipted envelope must be Ack-skipped, never re-executed")
	require.False(t, store.IsKeyProtected(factKey), "receipted sweep MUST release the fact original (§2.8, no lease leak)")
	require.False(t, store.IsKeyProtected(receiptKey), "receipted sweep MUST release the receipt original")
}

// Major#1 (§2.8) / §5.7: the startup reconcile used to be the by-key harvest
// (ReconcileReceiptedByKey over the projection scan's ReceiptKeys). That path
// is deleted — §5.7 replaces it with the direct inventory reconcile, whose
// lease-release-on-cleanup guarantee is asserted in reconcile_outstanding_test
// (TestReconcile_MatchingReceiptCleansUpOnly keeps Major#1's IsKeyProtected
// checks for the reconcile disposition).
