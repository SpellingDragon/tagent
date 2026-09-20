package agent

import (
	"context"
	"encoding/json"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §5.3 — the two-phase completion protocol wired end to end: freeze the completion
// durably (RecordCompletion) → submit the receipt under the envelope's RESERVED key
// through the idempotent replay interface → RecordReceipt → Ack. Discriminates
// against the old finishDurableBatch/persistInboxReceipt path, which minted a FRESH
// snowflake receipt key and stamped time.Now() on every call (non-idempotent).

func mustFact(t *testing.T, key int64, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(memory.FullEvent{EventKey: key, Content: content, EventType: tagentevent.TypeExternalInput})
	require.NoError(t, err)
	return b
}

// TestFinishDurableBatch_ReceiptUsesReservedKey is the §5.3 fail-before at the
// integration level: after a prepared+persisted durable turn, finishDurableBatch must
// ack ONLY after completion+receipt are durable, and the inbox_receipt event on the
// fact chain MUST carry the envelope's reserved key — not a fresh snowflake (the old
// persistInboxReceipt minted NewSnowflakeEventKey, so this equality would not hold).
func TestFinishDurableBatch_ReceiptUsesReservedKey(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	reservedKey := batch[0].claim.ReceiptKey
	require.NotEmpty(t, reservedKey, "prepare reserves the fixed receipt key")
	for _, e := range batch {
		require.True(t, ta.contextManager.persistBusEvent(e))
	}

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending(), "acked only after completion + receipt are durable")

	wantKey, err := tagentevent.ParseEventKey(reservedKey)
	require.NoError(t, err)
	var found bool
	refs, _ := ta.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			require.Equal(t, wantKey, r.EventKey,
				"§5.3: receipt must carry the RESERVED key, never a fresh snowflake (fail-before vs old persistInboxReceipt)")
			found = true
		}
	}
	require.True(t, found, "receipt event present on the fact chain")
}

// TestFinishDurableBatch_ReceiptIdempotentResubmit locks that re-committing the
// SAME reserved-key receipt (a lost-ack replay) converges to already-committed and
// does NOT add a second receipt event — the idempotency the old fresh-key path lacked.
func TestFinishDurableBatch_ReceiptIdempotentResubmit(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	for _, e := range batch {
		require.True(t, ta.contextManager.persistBusEvent(e))
	}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending())
	require.Equal(t, 2, ta.contextManager.memStore.GetStats().TotalEvents, "one input fact + one receipt")

	// Re-submitting the EXACT frozen receipt (a lost-ack replay) converges to
	// already-committed through the replay interface: no second receipt event is added.
	reservedKey := batch[0].claim.ReceiptKey
	require.NotEmpty(t, reservedKey)
	wantKey, err := tagentevent.ParseEventKey(reservedKey)
	require.NoError(t, err)
	committed, gerr := ta.contextManager.memStore.GetEvent(wantKey)
	require.NoError(t, gerr, "the reserved-key receipt is on the chain")
	require.NoError(t, ta.contextManager.commitReceiptFact(*committed), "re-committing the exact frozen receipt converges idempotently")
	require.Equal(t, 2, ta.contextManager.memStore.GetStats().TotalEvents, "re-commit must NOT double-write the receipt")
}

// TestBuildEnvelopeCompletion_DispositionsAndFixedKey locks the §5.3 builder: a
// committed slot freezes processed+fact-key, a yielding-meditation slot (present in
// received, absent from selected — now prepared with a reserved key by the widened
// prepare) freezes skipped(meditation_yield) with NO fact key, the receipt fact uses
// the envelope's reserved key, and a cancelled turn forms no completion.
func TestBuildEnvelopeCompletion_DispositionsAndFixedKey(t *testing.T) {
	evA := &AgentEvent{ID: "src-a", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/a.json", RequestID: "rid-a", Slot: 0, ReceiptKey: "aaa", PreparedFact: mustFact(t, 111, "A")}}
	evB := &AgentEvent{ID: "src-b", Type: tagentevent.TypeExternalInput, Source: "meditation",
		claim: &durableClaim{Path: "/inbox/b.json", RequestID: "rid-b", Slot: 0, ReceiptKey: "bbb", PreparedFact: mustFact(t, 222, "B")}}
	evC := &AgentEvent{ID: "src-c", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/c.json", RequestID: "rid-c", Slot: 1, ReceiptKey: "ccc", PreparedFact: mustFact(t, 333, "C")}}

	committed := selectedKeySet([]*AgentEvent{evA, evC}) // B yielded

	// A: committed → processed with its fact key + reserved receipt key.
	cA, _, err := buildEnvelopeCompletion([]*AgentEvent{evA}, committed, completedOutcome(), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, batchCompleted, cA.BatchResult)
	require.Len(t, cA.Slots, 1)
	require.Equal(t, slotProcessed, cA.Slots[0].Disposition)
	require.Equal(t, tagentevent.FormatEventKey(111), cA.Slots[0].FactKey)
	require.EqualValues(t, 0xaaa, cA.ReceiptFact.EventKey, "receipt uses the reserved key")

	// B: yielding meditation → skipped(meditation_yield), no fact key, own reserved key.
	cB, _, err := buildEnvelopeCompletion([]*AgentEvent{evB}, committed, completedOutcome(), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, slotSkipped, cB.Slots[0].Disposition)
	require.Equal(t, skipReasonMeditationYield, cB.Slots[0].Reason)
	require.Empty(t, cB.Slots[0].FactKey)
	require.EqualValues(t, 0xbbb, cB.ReceiptFact.EventKey)

	// A failed turn: committed inputs are still processed, batch_result=failed with summary.
	cF, _, err := buildEnvelopeCompletion([]*AgentEvent{evA, evC}, committed, failedOutcome("server_error: boom"), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, batchFailed, cF.BatchResult)
	require.Equal(t, "server_error: boom", cF.ErrorSummary)
	require.Len(t, cF.Slots, 2)
	for _, s := range cF.Slots {
		require.Equal(t, slotProcessed, s.Disposition, "committed inputs processed even when the turn failed")
	}
	require.Equal(t, []int{0, 1}, []int{cF.Slots[0].Slot, cF.Slots[1].Slot}, "slots ordered ascending")

	// Cancelled turn: no completion may be frozen.
	_, _, err = buildEnvelopeCompletion([]*AgentEvent{evA}, committed, cancelledOutcome(), "resident", 1, 1700000000000, nil)
	require.Error(t, err, "a cancelled turn forms no completion")

	// Missing prepared fact on a processed slot is a deterministic error (never a
	// restamp-fallback key).
	evNoFact := &AgentEvent{ID: "src-d", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/d.json", RequestID: "rid-d", Slot: 0, ReceiptKey: "ddd"}}
	_, _, err = buildEnvelopeCompletion([]*AgentEvent{evNoFact}, selectedKeySet([]*AgentEvent{evNoFact}), completedOutcome(), "resident", 1, 1, nil)
	require.Error(t, err, "processed slot without a prepared fact must not fabricate a key")
}
