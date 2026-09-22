package agent

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// helper: a well-formed completion for reuse across tests.
func validCompletion(t *testing.T) completion {
	t.Helper()
	fact, err := buildReceiptFact("1a2b3c", 7, "req-1", "resident", 1700000000000, map[string]string{"rollout_id": "s1"})
	require.NoError(t, err)
	return completion{
		CompletionVersion: completionVersion,
		RequestID:         "req-1",
		ReceiptKey:        "1a2b3c",
		CompletedAtMs:     1700000000000,
		BatchResult:       batchCompleted,
		Slots: []completionSlot{
			{Slot: 0, SourceID: "src-a", Disposition: slotProcessed, FactKey: "beef"},
			{Slot: 1, SourceID: "src-b", Disposition: slotSkipped, Reason: skipReasonMeditationYield},
		},
		ReceiptFact: fact,
	}
}

// TestBatchResultFromOutcome locks the §5.1→§5.2 seam: completed/failed both form
// a completion (failed carries its summary), while a cancelled turn forms NO
// completion (ok==false) so the loop never freezes a non-terminal turn.
func TestBatchResultFromOutcome(t *testing.T) {
	r, sum, ok := batchResultFromOutcome(completedOutcome())
	require.True(t, ok)
	require.Equal(t, batchCompleted, r)
	require.Empty(t, sum)

	r, sum, ok = batchResultFromOutcome(failedOutcome("server_error: boom"))
	require.True(t, ok)
	require.Equal(t, batchFailed, r)
	require.Equal(t, "server_error: boom", sum)

	_, _, ok = batchResultFromOutcome(cancelledOutcome())
	require.False(t, ok, "a cancelled turn must not form a completion")
}

// TestBuildReceiptFact_UsesFrozenKeyAndTime is the fail-before against the legacy
// persistInboxReceipt behavior: the old path minted a FRESH snowflake EventKey and
// used time.Now() on every call, so a restart/retry produced a DIFFERENT receipt.
// The frozen receipt must instead carry the envelope's reserved key and the
// first-hand completion time + attribution.
func TestBuildReceiptFact_UsesFrozenKeyAndTime(t *testing.T) {
	reserved := int64(0x1a2b3c)
	fact, err := buildReceiptFact(tagentevent.FormatEventKey(reserved), 7, "req-1", "resident", 1700000000000,
		map[string]string{"rollout_id": "s1", "trigger_source": "user"})
	require.NoError(t, err)
	require.Equal(t, reserved, fact.EventKey, "receipt key is the RESERVED envelope key, not a fresh snowflake")
	require.EqualValues(t, tagentevent.TypeInboxReceipt, fact.EventType)
	require.EqualValues(t, 1700000000000, fact.Timestamp, "timestamp is the frozen completion time, not time.Now")
	require.Equal(t, "req-1", fact.Metadata["inbox_request_id"])
	require.Equal(t, "resident", fact.Metadata[tagentevent.MetaKeyAgentName])
	require.Equal(t, "s1", fact.Metadata["rollout_id"], "first attribution is carried into the receipt")
	require.Equal(t, "user", fact.Metadata["trigger_source"])

	// Idempotent-by-key: re-minting from the same frozen inputs yields the same key,
	// so re-submitting the receipt can never add a second event.
	fact2, err := buildReceiptFact(tagentevent.FormatEventKey(reserved), 7, "req-1", "resident", 1700000000000,
		map[string]string{"rollout_id": "s1", "trigger_source": "user"})
	require.NoError(t, err)
	require.Equal(t, fact.EventKey, fact2.EventKey)

	_, err = buildReceiptFact("!!!not-hex!!!", 7, "req", "a", 1, nil)
	require.Error(t, err, "an unparseable reserved key is a deterministic error, never a fresh-key fallback")
}

// TestFreezeCompletion_Deterministic locks that freeze is a pure function of its
// inputs (no clock/key minting inside), so §5.3's idempotent RecordCompletion sees
// byte-identical payloads on an identical retry, and the round-trip re-validates.
func TestFreezeCompletion_Deterministic(t *testing.T) {
	c := validCompletion(t)
	b1, err := freezeCompletion(c)
	require.NoError(t, err)
	b2, err := freezeCompletion(c)
	require.NoError(t, err)
	require.Equal(t, string(b1), string(b2), "identical inputs freeze byte-identical bytes")

	decoded, err := decodeCompletion(b1)
	require.NoError(t, err)
	require.Equal(t, completionVersion, decoded.CompletionVersion)
	require.Equal(t, batchCompleted, decoded.BatchResult)
	require.Len(t, decoded.Slots, 2)
	require.Equal(t, c.ReceiptFact.EventKey, decoded.ReceiptFact.EventKey)
}

// TestCompletionValidate_ExactlyOneDispositionPerSlot is the core §5.2 invariant:
// every slot has exactly one well-formed disposition and the batch-level fields are
// self-consistent. Deviations are deterministic errors, never silently repaired.
func TestCompletionValidate_ExactlyOneDispositionPerSlot(t *testing.T) {
	require.NoError(t, validCompletion(t).validate())

	// processed without a fact key
	procNoKey := validCompletion(t)
	procNoKey.Slots[0].FactKey = ""
	require.ErrorContains(t, procNoKey.validate(), "processed but no fact key")

	// processed carrying a reason
	procWithReason := validCompletion(t)
	procWithReason.Slots[0].Reason = "junk"
	require.Error(t, procWithReason.validate())

	// skipped without a reason
	skipNoReason := validCompletion(t)
	skipNoReason.Slots[1].Reason = ""
	require.ErrorContains(t, skipNoReason.validate(), "unknown/absent reason")

	// skipped with a free-form (non-enum) reason
	skipBadReason := validCompletion(t)
	skipBadReason.Slots[1].Reason = "i_felt_like_it"
	require.Error(t, skipBadReason.validate(), "skip reason must be from the closed enum set")

	// the retired "empty_input" enum is no longer a valid skip reason (closed set
	// is two values; an empty input is committed as a processed fact, never
	// skipped) — resident-review-fixes 5.2. fail-before: keeping it in
	// validSkipReason would make this validate cleanly.
	deadEmpty := validCompletion(t)
	deadEmpty.Slots[1].Reason = "empty_input"
	require.Error(t, deadEmpty.validate(), "empty_input must no longer be a valid skip reason")

	// skipped carrying a fact key
	skipWithKey := validCompletion(t)
	skipWithKey.Slots[1].FactKey = "cafe"
	require.Error(t, skipWithKey.validate())

	// unknown disposition
	unknownDisp := validCompletion(t)
	unknownDisp.Slots[0].Disposition = "half_processed"
	require.ErrorContains(t, unknownDisp.validate(), "unknown disposition")

	// duplicate slot
	dup := validCompletion(t)
	dup.Slots = append(dup.Slots, completionSlot{Slot: 0, SourceID: "src-a", Disposition: slotProcessed, FactKey: "beef"})
	require.ErrorContains(t, dup.validate(), "duplicate slot")

	// failed batch without a summary
	failedNoSum := validCompletion(t)
	failedNoSum.BatchResult = batchFailed
	require.ErrorContains(t, failedNoSum.validate(), "failed batch without an error summary")

	// completed batch carrying a summary
	completedWithSum := validCompletion(t)
	completedWithSum.ErrorSummary = "stray"
	require.Error(t, completedWithSum.validate())

	// empty receipt key
	noKey := validCompletion(t)
	noKey.ReceiptKey = ""
	require.Error(t, noKey.validate())

	// freeze rejects an invalid completion
	_, err := freezeCompletion(procNoKey)
	require.Error(t, err, "freeze must validate before marshalling")
}

// TestCompletion_FailedCarriesSummary shows a deterministic model failure freezes
// as a completed-processing (batch_result=failed) with its bounded summary — the
// value §5.3 turns into a receipted (not dropped) outcome.
func TestCompletion_FailedCarriesSummary(t *testing.T) {
	c := validCompletion(t)
	c.BatchResult = batchFailed
	c.ErrorSummary = "server_error: upstream exploded"
	b, err := freezeCompletion(c)
	require.NoError(t, err)
	decoded, err := decodeCompletion(b)
	require.NoError(t, err)
	require.Equal(t, batchFailed, decoded.BatchResult)
	require.Equal(t, "server_error: upstream exploded", decoded.ErrorSummary)
}

// TestCompletion_LargeKeyPrecisionRoundTrip locks spec L82-84's exact-integer
// identity through the completion: an EventKey above 2^53 must survive freeze →
// decode unchanged (typed int64 fields, and jsonEqual-side UseNumber downstream).
func TestCompletion_LargeKeyPrecisionRoundTrip(t *testing.T) {
	big := int64(1) << 60 // 1.15e18, far above 2^53
	fact, err := buildReceiptFact(tagentevent.FormatEventKey(big), 7, "req-big", "resident", 1700000000001, nil)
	require.NoError(t, err)
	require.Equal(t, big, fact.EventKey)
	c := validCompletion(t)
	c.ReceiptKey = tagentevent.FormatEventKey(big)
	c.ReceiptFact = fact
	c.Slots[0].FactKey = tagentevent.FormatEventKey(big + 1) // adjacent large key

	b, err := freezeCompletion(c)
	require.NoError(t, err)
	decoded, err := decodeCompletion(b)
	require.NoError(t, err)
	require.Equal(t, big, decoded.ReceiptFact.EventKey, "large key preserved exactly, not collapsed through float")
	require.NotEqual(t, tagentevent.FormatEventKey(big), decoded.Slots[0].FactKey, "adjacent large keys remain distinct")
	require.Equal(t, tagentevent.FormatEventKey(big+1), decoded.Slots[0].FactKey)
}
