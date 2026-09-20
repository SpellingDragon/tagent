package reliability

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// srcEvent builds a minimal opaque source_event payload for a slot. The
// reliability leaf treats it as raw JSON — only the agent layer knows the
// AgentEvent schema — so any non-empty JSON is a valid lossless snapshot here.
func srcEvent(id, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"id": id, "content": content})
	return b
}

func env(n, src string) *Envelope {
	return &Envelope{RequestID: n, Source: src, State: InboxStatePending,
		Messages: []MessageSlot{{Slot: 0, SourceEvent: srcEvent(n, "m-"+n)}}}
}

func mustEnqueue(t *testing.T, in *Inbox, e *Envelope) int64 {
	t.Helper()
	seq, err := in.Enqueue(e)
	require.NoError(t, err)
	return seq
}

// finish drives a claimed envelope through the two-phase order (reserve →
// completion → credentialed receipt → ack) that D3 step 7 mandates. Used by
// lifecycle tests that just want to retire an envelope.
func finish(t *testing.T, in *Inbox, path string) {
	t.Helper()
	cred := reserveCredential(t, in, path)
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(path, cred))
	require.NoError(t, in.Ack(path))
}

// reserveCredential mirrors the production two-phase protocol at the leaf: it
// reserves a receipt key via PrepareFacts when the envelope has none yet and
// returns the matching §5.4 verified receipt credential.
func reserveCredential(t *testing.T, in *Inbox, path string) ReceiptCredential {
	t.Helper()
	e, err := readEnvelope(path)
	require.NoError(t, err)
	key := e.ReceiptKey
	if key == "" {
		key = "rk-" + filepath.Base(path)
		require.NoError(t, in.PrepareFacts(path, key, make([]json.RawMessage, len(e.Messages))))
	}
	return ReceiptCredential{ReceiptKey: key}
}

// claimUntil claims envelopes in order until the named one shows up,
// finishing the older ones to keep the scan deterministic.
func claimUntil(t *testing.T, in *Inbox, id string) (*Envelope, string) {
	t.Helper()
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		require.NotNil(t, e)
		if e.RequestID == id {
			return e, p
		}
		finish(t, in, p)
	}
}

func TestInbox_EnqueueClaimAck_BasicOrder(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	mustEnqueue(t, in, env("b", "user"))
	require.Equal(t, int64(2), in.Pending())

	e1, p1, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", e1.RequestID)
	require.Equal(t, InboxStateClaimed, e1.State)
	require.Equal(t, int64(2), in.Pending(), "claim does not remove")

	require.Error(t, in.Ack(p1), "ack refuses non-receipted claim")

	// §5.4: the leaf ENFORCES the two-phase ordering — RecordReceipt refuses an
	// envelope whose completion was never frozen, and one without a reserved key
	// + matching verified credential (see
	// TestInbox_ReceiptAndAckRequireDurableCompletion,
	// TestInbox_RecordReceiptRequiresVerifiedCredential). The happy path freezes
	// completion first, exactly as the agent-layer composite (finishDurableBatch
	// Phase A → Phase B credential → Phase C/ConfirmDurable) orders it in production.
	cred1 := reserveCredential(t, in, p1)
	require.NoError(t, in.RecordCompletion(p1, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p1, cred1))
	require.NoError(t, in.Ack(p1))
	require.Equal(t, int64(1), in.Pending())
	require.NoError(t, in.Ack(p1)) // idempotent

	e2, p2, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "b", e2.RequestID)
	finish(t, in, p2)
	require.Equal(t, int64(0), in.Pending())
}

func TestInbox_FullIsExplicitRejection(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 2)
	require.NoError(t, err)
	mustEnqueue(t, in, env("1", "user"))
	mustEnqueue(t, in, env("2", "user"))
	_, err = in.Enqueue(env("3", "user"))
	require.True(t, errors.Is(err, ErrInboxFull), "overflow must be typed, got: %v", err)
}

func TestInbox_Reopen_RequeuesClaimed_KeepsReceipted(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	_, pA := claimUntil(t, in, "a")

	mustEnqueue(t, in, env("b", "user"))
	_, pB := claimUntil(t, in, "b")
	credB := reserveCredential(t, in, pB)
	require.NoError(t, in.RecordCompletion(pB, json.RawMessage(`{}`)))
	require.NoError(t, in.RecordReceipt(pB, credB)) // crash BEFORE ack

	mustEnqueue(t, in, env("c", "user"))
	require.NoError(t, in.Close())

	// Reopen: claimed(a) → replay; receipted(b) → Ack-skip; c → pending.
	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), in2.Pending())

	got1, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", got1.RequestID, "claimed-but-unreceipted replays")
	require.GreaterOrEqual(t, got1.Attempts, 2, "requeue + re-claim each count one attempt")

	// b (receipted) is now RETURNED to the caller (not silently swept by the leaf) so
	// the caller (claimDurable) can Ack it AND release its §2.8 retention in one place.
	// It must come back unchanged: NOT re-claimed, NOT Attempts++, still on disk.
	got2, pB2, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "b", got2.RequestID, "a settled receipted envelope is surfaced, not hidden")
	require.Equal(t, InboxStateReceipted, got2.State, "returned as-is: not re-claimed")
	require.NoError(t, in2.Ack(pB2)) // caller acks the settled item; only then frees capacity

	// After b is acked, the next claimable is c.
	got3, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "c", got3.RequestID)
	require.Equal(t, int64(2), in2.Pending(), "b acked (-1); a and c claimed but files kept until ack")
	_ = pA
	_ = pB
}

func TestInbox_CorruptItemQuarantined(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("good", "user"))
	corrupt := filepath.Join(in.Dir(), "00000000000000000002.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o644))

	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	e, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "good", e.RequestID)
	_, statErr := os.Stat(filepath.Join(in.Dir(), inboxQuarantine, "00000000000000000002.json"))
	require.NoError(t, statErr, "corrupt item kept in quarantine, not destroyed")
}

// TestInbox_UnknownVersionQuarantined: a v1-shaped (no version) or a v3 file
// is never consumed as a valid input — it is quarantined and alerted (D2).
func TestInbox_UnknownVersionQuarantined(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("good", "user"))
	// A legacy v1 envelope (no "version" field) written into the v2 dir.
	v1 := filepath.Join(in.Dir(), "00000000000000000005.json")
	require.NoError(t, os.WriteFile(v1,
		[]byte(`{"request_id":"old","source":"user","state":"pending","messages":[{"role":"user","content":"x"}]}`), 0o644))

	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	e, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "good", e.RequestID, "v1 item is never consumed")
	_, statErr := os.Stat(filepath.Join(in.Dir(), inboxQuarantine, "00000000000000000005.json"))
	require.NoError(t, statErr, "unknown-version item quarantined, not destroyed")
}

// TestInbox_EnqueueStampsVersionAndSlots: acceptance stamps version=2 and
// assigns fixed, sequential slot indices; slots are never renumbered by later
// claim/prepare rewrites (F4「序号不可压紧」).
func TestInbox_EnqueueStampsVersionAndSlots(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	e := &Envelope{RequestID: "batch", Source: "user",
		Messages: []MessageSlot{{SourceEvent: srcEvent("a", "A")}, {SourceEvent: srcEvent("b", "B")}}}
	_, err = in.Enqueue(e)
	require.NoError(t, err)

	claimed, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, envelopeVersion, claimed.Version)
	require.Len(t, claimed.Messages, 2)
	require.Equal(t, 0, claimed.Messages[0].Slot)
	require.Equal(t, 1, claimed.Messages[1].Slot)

	// A prepare rewrite must not disturb the slot numbers.
	require.NoError(t, in.PrepareFacts(path, "deadbeef",
		[]json.RawMessage{json.RawMessage(`{"k":1}`), json.RawMessage(`{"k":2}`)}))
	// Re-read from disk via a fresh receipt attempt to force a read.
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{}`)))
	got, err := readEnvelope(path)
	require.NoError(t, err)
	require.Equal(t, 0, got.Messages[0].Slot)
	require.Equal(t, 1, got.Messages[1].Slot)
	require.Equal(t, json.RawMessage(`{"k":1}`), got.Messages[0].PreparedFact)
}

// TestInbox_EnqueueRejectsEmptySourceEvent: a slot without a source_event is
// refused at acceptance — the inbox never accepts silently-lossy input (F1).
func TestInbox_EnqueueRejectsEmptySourceEvent(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	_, err = in.Enqueue(&Envelope{RequestID: "x", Source: "user",
		Messages: []MessageSlot{{Slot: 0}}}) // no source_event
	require.Error(t, err)
	require.Equal(t, int64(0), in.Pending(), "rejected input leaves no durable item")
}

// TestInbox_PrepareFacts_freezesAndConflicts: PrepareFacts reserves a
// receipt_key + per-slot facts, is idempotent on identical re-prepare, and
// refuses a conflicting receipt_key or a different already-frozen fact (D2).
func TestInbox_PrepareFacts_freezesAndConflicts(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("p", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	facts := []json.RawMessage{json.RawMessage(`{"a":1}`)}
	require.NoError(t, in.PrepareFacts(path, "key-1", facts))
	require.NoError(t, in.PrepareFacts(path, "key-1", facts), "identical re-prepare is idempotent")

	require.ErrorIs(t, in.PrepareFacts(path, "key-2", facts), ErrReceiptKeyConflict,
		"a different receipt_key is a conflict")
	require.Error(t, in.PrepareFacts(path, "key-1", []json.RawMessage{json.RawMessage(`{"a":2}`)}),
		"a different prepared fact is a conflict")
}

// TestInbox_RecordCompletion_idempotentConflict: the frozen completion cannot
// be overwritten with a different payload (D3 step 8).
func TestInbox_RecordCompletion_idempotentConflict(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("c", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)), "idempotent")
	require.ErrorIs(t, in.RecordCompletion(path, json.RawMessage(`{"result":"failed"}`)), ErrCompletionConflict)
}

// §3.7 (决策10): previous-format data no longer blocks boot — the inbox opens on
// the current format, classifies legacy items as inert transitional data (never read
// or consumed), and leaves them untouched until an explicit managed reset.
func TestInbox_LegacySpillIsInertNotBlocking(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err, "legacy .spill must NOT block boot on the current format")
	sp, _ := in.TransitionalData()
	require.Contains(t, sp, spill, "stray .spill is classified as transitional, not consumed")
	require.NoError(t, in.Close())
	_, statErr := os.Stat(spill)
	require.NoError(t, statErr, "classification is read-only: the legacy file is left untouched")
}

func TestInbox_LegacyV1DirIsInertNotBlocking(t *testing.T) {
	dir := t.TempDir()
	v1 := filepath.Join(dir, "inbox-v1")
	require.NoError(t, os.MkdirAll(v1, 0o755))
	item := filepath.Join(v1, "00000000000000000001.json")
	require.NoError(t, os.WriteFile(item, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err, "leftover inbox-v1 items must NOT block boot")
	_, v1s := in.TransitionalData()
	require.Contains(t, v1s, item, "v1 item is classified as transitional, never guess-migrated")
	require.Equal(t, int64(0), in.Pending(), "v1 items are never consumed into v2 pending")
	require.NoError(t, in.Close())
}

// TestInbox_EmptyV1DirDoesNotBlock: an empty (already-drained) inbox-v1 dir is
// not a blocker — the gate keys on leftover items, not the directory's mere
// existence, so a fully drained upgrade proceeds.
func TestInbox_EmptyV1DirDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "inbox-v1"), 0o755))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	require.NotNil(t, in)
}

// TestInbox_QuarantineUndispositionedBlocksReopen: quarantine items left by a
// prior run must be dispositioned by the operator before reopening — silently
// re-ignoring them every boot would hide data loss (D2 migration gate, 3.5).
func TestInbox_QuarantineUndispositionedBlocksReopen(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	qdir := filepath.Join(in.Dir(), inboxQuarantine)
	require.NoError(t, os.WriteFile(filepath.Join(qdir, "00000000000000000007.json"), []byte("{bad"), 0o644))
	require.NoError(t, in.Close())

	_, err = NewInbox(dir, 10)
	require.True(t, errors.Is(err, ErrQuarantineUndispositioned), "undispositioned quarantine must fail loud, got: %v", err)
}

// TestInbox_TransitionalClassificationIsReadOnly: §3.7 classification is read-only —
// opening on the current format must NOT mutate or delete the leftover legacy item
// (only an explicit managed reset may). The legacy spill stays byte-identical.
func TestInbox_TransitionalClassificationIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("keepme"), 0o644))
	_, err := NewInbox(dir, 10)
	require.NoError(t, err)
	got, rerr := os.ReadFile(spill)
	require.NoError(t, rerr, "classification must leave the legacy item untouched (read-only)")
	require.Equal(t, "keepme", string(got))
}

func TestInbox_ConcurrentEnqueue_NoOvertakeNoLoss(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 0) // default 2560
	require.NoError(t, err)
	const G, N = 10, 10
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < N; i++ {
				_, err := in.Enqueue(env(fmt.Sprintf("g%02d-i%02d", g, i), "user"))
				require.NoError(t, err)
			}
		}(g)
	}
	wg.Wait()
	require.Equal(t, int64(G*N), in.Pending())
	// Drain in strict seq order: envelopes come back grouped by file order.
	seen := 0
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		if e == nil {
			break
		}
		finish(t, in, p)
		seen++
	}
	require.Equal(t, G*N, seen, "every durable input must survive concurrency")
}

// TestInbox_ReceiptAndAckRequireDurableCompletion locks the §5.4 state gates:
// ① RecordReceipt refuses a claimed envelope whose completion was never frozen,
// without touching the state (fail-before: pre-§5.4 the leaf receipted anything,
// letting a bare state transition stand in for processing evidence); ② the same
// receipt converges once the completion is durable AND the reservation + verified
// credential are in place; ③ a receipted STATE stripped of its completion (a
// corrupt/legacy contradiction) must never be deleted on the status string alone —
// the ack is refused and the original kept (spec L172-174).
func TestInbox_ReceiptAndAckRequireDurableCompletion(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)

	mustEnqueue(t, in, env("gate-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)

	// ① No receipt without a durable completion; state untouched.
	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "whatever"}), "no durable completion")
	envAfter, rerr := readEnvelope(p)
	require.NoError(t, rerr)
	require.Equal(t, InboxStateClaimed, envAfter.State, "a refused receipt must not advance the state")

	// ② Freeze the completion → with reservation + matching credential the same
	// receipt converges.
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, reserveCredential(t, in, p)))
	require.NoError(t, in.Ack(p))

	// ③ Contradiction guard: drive a second envelope legitimately to receipted,
	// then forge completion-loss. Ack must refuse deletion and KEEP the original.
	mustEnqueue(t, in, env("gate-2", "user"))
	_, p2, err := in.ClaimNext()
	require.NoError(t, err)
	cred2 := reserveCredential(t, in, p2)
	require.NoError(t, in.RecordCompletion(p2, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p2, cred2))
	bad, rerr := readEnvelope(p2)
	require.NoError(t, rerr)
	bad.Completion = nil
	_, werr := writeEnvelopeFile(p2, bad)
	require.NoError(t, werr)
	require.ErrorContains(t, in.Ack(p2), "without a durable completion")
	require.FileExists(t, p2, "a contradictory receipted item is kept for inspection, never deleted per status string")
}

// TestInbox_RecordReceiptRequiresVerifiedCredential locks the §5.4 credential
// gates (design 决策 L126): even with a durable legal completion, RecordReceipt
// refuses ① an envelope whose two-phase reservation was never established, ② an
// EMPTY credential, and ③ a credential whose key is not the envelope's reserved
// receipt key — a bare note string or request id confirms nothing any more
// (fail-before: pre-§5.4 the leaf accepted any description string). A refusal
// never advances the state; only the matching credential converges.
func TestInbox_RecordReceiptRequiresVerifiedCredential(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("cred-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))

	// ① Completion present but no reservation → refused.
	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "rk"}), "no reserved receipt key")

	cred := reserveCredential(t, in, p)
	require.NotEmpty(t, cred.ReceiptKey)

	// ② Empty and ③ foreign keys are refused without advancing the state.
	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{}), "credential key")
	require.ErrorContains(t, in.RecordReceipt(p, ReceiptCredential{ReceiptKey: "some-OTHER-key"}), "does not match")
	e, rerr := readEnvelope(p)
	require.NoError(t, rerr)
	require.Equal(t, InboxStateClaimed, e.State, "a refused receipt must keep the claim, never confirm on nothing")

	// The matching credential converges.
	require.NoError(t, in.RecordReceipt(p, cred))
	require.NoError(t, in.Ack(p))
	require.Equal(t, int64(0), in.Pending())
}
