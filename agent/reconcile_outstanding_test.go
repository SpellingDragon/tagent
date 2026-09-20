package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §5.7 — startup DIRECT reconcile: after the cold-start dir barrier and
// inventory, every outstanding envelope is checked against ITS OWN fixed
// receipt key; the confirmation list is never harvested from the projection
// scan. Dispositions: prepared-only → continue input; completion-only → re-submit
// ONLY the frozen receipt; matching receipt → clean up only; read I/O → block;
// any contradictory identity / receipt-without-completion → quarantine + report.

// completionOnlyEnvelope drives the real protocol to the "Phase A durable,
// Phase B never landed" state: input committed, completion frozen on the
// envelope, NO receipt on the chain, claim held (state=claimed).
func completionOnlyEnvelope(t *testing.T, dir string) (*TagentAgent, *memory.InMemoryStore, string) {
	t.Helper()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("recon-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	healthy := ta.contextManager.memStore.(*memory.InMemoryStore)

	// Phase B fails (receipt commit fault) → completion stays durable, claim held.
	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "precondition: not acked")
	ta.contextManager.memStore = healthy
	return ta, healthy, batch[0].claim.Path
}

func envelopeAt(t *testing.T, path string) *reliability.Envelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var env reliability.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return &env
}

func tamperEnvelope(t *testing.T, path string, edit func(*reliability.Envelope)) {
	t.Helper()
	env := envelopeAt(t, path)
	edit(env)
	raw, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
}

func countReceipts(t *testing.T, store interface {
	QueryEvents(memory.QueryOptions) ([]memory.EventReference, error)
}) int {
	t.Helper()
	n := 0
	refs, _ := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			n++
		}
	}
	return n
}

// TestReconcile_PreparedOnlyContinuesInput: an envelope with prepared facts but
// no completion is NOT disposed by reconcile — it stays for the normal replay
// path (核对补齐后执行), with no receipt and no ack.
func TestReconcile_PreparedOnlyContinuesInput(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("only-prep"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Continued)
	require.Zero(t, s.ReceiptsAdded+s.Cleaned+s.Quarantined+s.Blocked, "a prepared-only envelope is untouched")
	require.FileExists(t, batch[0].claim.Path)
	require.Equal(t, int64(1), bus.DurablePending(), "continue input: nothing consumed")
	require.Equal(t, 0, countReceipts(t, ta.contextManager.memStore))
}

// TestReconcile_CompletionOnlyResubmitsReceiptOnly is the crash window the
// code review left open: completion durable, receipt never landed. Reconcile
// re-submits ONLY the frozen receipt from the envelope's own durable
// completion and cleans up — NO model re-run (the input fact count never
// grows), no re-freeze. It converges WITHOUT any projection scan having ever
// listed the key (the direct-inventory property that kills the harvest path).
func TestReconcile_CompletionOnlyResubmitsReceiptOnly(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	reserved := envelopeAt(t, path).ReceiptKey
	require.NotEmpty(t, reserved)
	require.Equal(t, 0, countReceipts(t, healthy), "precondition: no receipt on the chain")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "completion-only → re-submit the receipt, then clean up")
	require.Zero(t, s.Quarantined+s.Blocked)

	require.NoFileExists(t, path, "envelope cleaned up")
	require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	require.Equal(t, 1, countReceipts(t, healthy), "exactly the ONE frozen receipt landed")
	key, kerr := tagentevent.ParseEventKey(reserved)
	require.NoError(t, kerr)
	stored, gerr := healthy.GetEvent(key)
	require.NoError(t, gerr, "receipt verified under the envelope's OWN reserved key")
	require.Equal(t, tagentevent.TypeInboxReceipt, stored.EventType)
	require.Equal(t, 2, healthy.GetStats().TotalEvents, "input fact + receipt only — no new input, no re-run")
}

// TestReconcile_MatchingReceiptCleansUpOnly covers the "receipt durable, ack
// died" window: the receipt is already on the chain under the reserved key, so
// reconcile must CLEAN UP ONLY — no second receipt commit (Major#1's lease
// release rides the same ConfirmDurable path).
func TestReconcile_MatchingReceiptCleansUpOnly(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)

	// Simulate Phase B having succeeded before the crash: commit the frozen
	// receipt through the real verify path, but never ack.
	env := envelopeAt(t, path)
	cred, verr := ta.contextManager.verifyReceiptCredential(env.Completion)
	require.NoError(t, verr)
	require.Equal(t, 1, countReceipts(t, healthy), "receipt on the chain")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Cleaned)
	require.Zero(t, s.ReceiptsAdded, "a matching receipt is NEVER re-submitted")
	require.Equal(t, cred.ReceiptKey, env.ReceiptKey)
	require.NoFileExists(t, path)
	require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	require.Equal(t, 1, countReceipts(t, healthy), "cleanup added no second receipt")
	require.Equal(t, 2, healthy.GetStats().TotalEvents, "input + the single receipt, nothing else")
}

// TestReconcile_ContradictionsQuarantine: each contradiction is isolated with
// its bytes kept — never re-stamped, never cleaned up, never input-consumed.
func TestReconcile_ContradictionsQuarantine(t *testing.T) {
	t.Run("undecodable completion", func(t *testing.T) {
		dir := t.TempDir()
		ta, healthy, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) { e.Completion = json.RawMessage(`"a string"`) })

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined)
		require.NoFileExists(t, path, "removed from the live inbox")
		require.FileExists(t, filepath.Join(dir, "inbox-v2", "quarantine", filepath.Base(path)), "bytes KEPT for inspection")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending(), "capacity released through quarantine accounting")
		require.Equal(t, 0, countReceipts(t, healthy), "a contradiction never produces a receipt")
	})

	t.Run("reserved key owned by different content", func(t *testing.T) {
		dir := t.TempDir()
		ta, healthy, path := completionOnlyEnvelope(t, dir)
		env := envelopeAt(t, path)
		key, kerr := tagentevent.ParseEventKey(env.ReceiptKey)
		require.NoError(t, kerr)
		foreign := memory.FullEvent{EventKey: key, PartitionID: 1, EventType: tagentevent.TypeInboxReceipt,
			EventSummary: "forged", Content: "forged", Timestamp: 1, Metadata: map[string]string{"request_id": "forged"}}
		require.NoError(t, healthy.StoreEvent(key, foreign), "a DIFFERENT record owns the reserved key")

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined, "same-key-different-content is a deterministic contradiction")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending(), "quarantine releases capacity, bytes kept")
		require.FileExists(t, filepath.Join(dir, "inbox-v2", "quarantine", filepath.Base(path)))
	})

	t.Run("completion identity drifts from the envelope", func(t *testing.T) {
		dir := t.TempDir()
		ta, _, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) { e.RequestID = "someone-else" })

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined)
		require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	})

	t.Run("receipted state without completion", func(t *testing.T) {
		dir := t.TempDir()
		ta, _, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) {
			e.State = reliability.InboxStateReceipted
			e.Completion = nil
		})

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined, "「任何缺 completion 的 receipt」quarantines — never falls through to continue-input")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	})
}

// ioFaultStore fails EVERY GetEvent with a transient I/O error (not not-found).
type ioFaultStore struct {
	*memory.InMemoryStore
}

func (s *ioFaultStore) GetEvent(key int64) (*memory.FullEvent, error) {
	return nil, errors.New("simulated disk I/O failure")
}

// TestReconcile_ReadIOBlocksConservatively: an unreadable chain must NOT be
// read as "receipt missing" — the envelope is retained, nothing is submitted,
// quarantined or acked, and a healthy retry converges.
func TestReconcile_ReadIOBlocksConservatively(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	ta.contextManager.memStore = &ioFaultStore{InMemoryStore: healthy}

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Blocked, "I/O trouble blocks the disposition, never guesses it")
	require.Zero(t, s.ReceiptsAdded+s.Cleaned+s.Quarantined)
	require.FileExists(t, path, "the envelope is retained")
	require.Equal(t, int64(1), ta.persistentBus.DurablePending())
	require.Equal(t, 0, countReceipts(t, healthy), "nothing was submitted on uncertain ground")

	ta.contextManager.memStore = healthy
	s2, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s2.ReceiptsAdded, "the same envelope converges once I/O recovers")
}

// TestReconcile_UnreadableEnvelopeBlocks: an envelope present but undecodable
// at inventory time blocks conservatively (open quarantines corrupt items;
// anything failing NOW is new I/O trouble — retain, report, never dispose).
func TestReconcile_UnreadableEnvelopeBlocks(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("io-env"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(batch[0].claim.Path, []byte("not-json-anymore"), 0o644))

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Blocked)
	require.Zero(t, s.Quarantined+s.ReceiptsAdded+s.Cleaned)
	require.FileExists(t, batch[0].claim.Path, "bytes retained, nothing guessed")
}
