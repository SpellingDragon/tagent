package agent

import (
	"context"
	"encoding/json"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §5.4 — RecordReceipt demands a LEGAL completion plus a VERIFIED receipt
// credential, and receipt failure / input failure never mask each other.
// verifyReceiptCredential is the only issuer of credentials: it decodes +
// re-validates the frozen completion, binds the receipt fact to its reserved
// identity, and confirms the fact-chain commit before granting one. A refused
// receipt holds the claim and leaves the input's prepared evidence intact.

func credReceiptKeyHex(atMs int64) (int64, string) {
	key := memory.NewSnowflakeEventKey(1, atMs)
	return key, tagentevent.FormatEventKey(key)
}

func frozenCompletion(t *testing.T, receiptKeyHex string, fact memory.FullEvent) json.RawMessage {
	t.Helper()
	b, err := freezeCompletion(completion{
		CompletionVersion: completionVersion,
		RequestID:         "req-cred",
		ReceiptKey:        receiptKeyHex,
		CompletedAtMs:     1234567,
		BatchResult:       batchCompleted,
		Slots:             []completionSlot{{Slot: 0, SourceID: "s0", Disposition: slotSkipped, Reason: skipReasonNotSelected}},
		ReceiptFact:       fact,
	})
	require.NoError(t, err)
	return b
}

// TestVerifyReceiptCredential_IssuedOnlyAfterChainVerification locks the happy
// path and the idempotence of verification: the credential is the reserved key,
// issued exactly when the receipt fact is on the chain; re-verifying converges
// on already-committed (never a second receipt event).
func TestVerifyReceiptCredential_IssuedOnlyAfterChainVerification(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	fact, err := buildReceiptFact(keyHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.NoError(t, err)
	require.Equal(t, keyHex, cred.ReceiptKey)
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents, "the verified receipt fact is on the chain")

	cred2, err := cm.verifyReceiptCredential(raw)
	require.NoError(t, err)
	require.Equal(t, cred, cred2, "verification is idempotent")
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents, "never a second receipt event")
}

// TestVerifyReceiptCredential_RejectsIllegalCompletion: garbage bytes or a
// foreign schema version are NOT a legal completion — no credential, no chain
// write (fail-before: the pre-§5.4 path trusted opaque bytes at the leaf).
func TestVerifyReceiptCredential_RejectsIllegalCompletion(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}

	cred, err := cm.verifyReceiptCredential(json.RawMessage(`not-json`))
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)

	cred, err = cm.verifyReceiptCredential(json.RawMessage(`{"completion_version":99}`))
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "an illegal completion submits nothing")
}

// TestVerifyReceiptCredential_RejectsIdentityDrift: a receipt fact whose EventKey
// is NOT the completion's reserved key is a contradiction, not a credential —
// refused BEFORE any commit, so a foreign key can never slip onto the chain
// under a legitimate envelope's reservation.
func TestVerifyReceiptCredential_RejectsIdentityDrift(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	_, otherHex := credReceiptKeyHex(1_700_000_001_000)
	require.NotEqual(t, keyHex, otherHex)
	fact, err := buildReceiptFact(otherHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.ErrorContains(t, err, "identity contradiction")
	require.Empty(t, cred.ReceiptKey)
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "a drifting fact is never committed")
}

// TestVerifyReceiptCredential_CommitFailureYieldsNoCredential: if the receipt
// cannot be verified on the chain, RecordReceipt is never reached — the caller
// holds the claim (§5.7 re-submits the receipt without re-running the model).
func TestVerifyReceiptCredential_CommitFailureYieldsNoCredential(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	fact, err := buildReceiptFact(keyHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)
}

// TestFinishDurableBatch_ReceiptFailureDoesNotMaskInput: with the input fact
// already committed, a receipt-commit failure holds the claim, writes NO receipt
// event, and leaves the envelope's prepared reservation (fact keys + reserved
// key) fully intact for the §5.7 startup reconcile to re-submit ONLY the receipt
// from the durable completion — the receipt failure neither consumed nor
// corrupted the input evidence (no masking).
func TestFinishDurableBatch_ReceiptFailureDoesNotMaskInput(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-mask"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	healthy := ta.contextManager.memStore
	require.Equal(t, 1, healthy.GetStats().TotalEvents)

	// Receipt commit fails: claim held, no receipt event, input evidence intact.
	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "a receipt failure must not ack")
	m, ok, merr := bus.inbox.MaterialOfPath(batch[0].claim.Path)
	require.NoError(t, merr)
	require.True(t, ok)
	require.Equal(t, batch[0].claim.ReceiptKey, m.ReceiptKey, "the reserved key survives the failed attempt")
	require.NotEmpty(t, m.FactKeys, "the input's prepared fact key is untouched")

	// No receipt landed anywhere (the faulted chain stayed empty, the healthy
	// chain holds ONLY the input fact): a deferred receipt never fakes evidence.
	// Reaching Phase B at all proves Phase A's completion froze durably — that
	// is the authoritative record the §5.7 reconcile re-submits the receipt from.
	require.Equal(t, 1, healthy.GetStats().TotalEvents, "input fact stands alone; no receipt was written")
	refs, _ := healthy.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, r.EventType)
	}
}

// TestFinishDurableBatch_InputConflictIsNeverReceipted: the converse masking
// direction. An already-durable completion that CONTRADICTS this turn's freeze
// is a definite input-side error — no credential is minted, no receipt lands,
// the claim (and the authoritative frozen bytes) stay. A receipt error must not
// overwrite input evidence, and an input conflict must not produce a receipt.
func TestFinishDurableBatch_InputConflictIsNeverReceipted(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-conflict"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))

	// Freeze a divergent completion straight onto the envelope (what a previous,
	// different turn's durable freeze would look like on replay).
	path := batch[0].claim.Path
	require.NoError(t, bus.RecordCompletion(path, json.RawMessage(`{"completion_version":1,"request_id":"other"}`)))

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "a completion conflict never receipts the envelope")
	refs, _ := ta.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, r.EventType,
			"an input-side conflict must never leave a receipt behind")
	}
}
