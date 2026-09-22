package agent

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// §5.2 — the frozen post-turn completion (design 决策5 L122, spec L94). This is
// the payload stored in reliability.Envelope.Completion (an opaque json.RawMessage
// to the reliability leaf; the agent layer owns its schema). It is frozen ONCE at
// the turn's terminal state, BEFORE the receipt is submitted (§5.3), and carries
// the COMPLETE receipt fact so a restart re-submits the frozen bytes verbatim
// instead of rebuilding them from current time — the exact mistake the legacy
// finishDurableBatch/persistInboxReceipt path made (fresh snowflake key + fresh
// time.Now on every call, so a retry minted a different receipt).

// completionVersion tags the frozen schema. There is deliberately no v0/v2 reader
// (design 决策10: managed recovery-unit reset, not migration).
const completionVersion = 1

// batchResult is the turn-level outcome recorded in a completion. It is derived
// from §5.1's reduced turnOutcome. Both a completed and a deterministically
// failed turn form a completion ("完整失败是处理结果", design L121); a CANCELLED
// turn forms NO completion at all, so "cancelled" never appears as a batch result.
type batchResult string

const (
	batchCompleted batchResult = "completed"
	batchFailed    batchResult = "failed"
)

// batchResultFromOutcome maps a §5.1 reduced outcome to (result, errorSummary, ok).
// ok==false means the turn reached no terminal state (cancelled) and must NOT form
// a completion — the caller skips the freeze entirely and retains the claim.
func batchResultFromOutcome(o turnOutcome) (batchResult, string, bool) {
	switch o.status {
	case turnCompleted:
		return batchCompleted, "", true
	case turnFailed:
		return batchFailed, o.err, true
	default: // turnCancelled
		return "", "", false
	}
}

// slotDisposition is one slot's processing outcome (§5.2 L122): EXACTLY one per
// slot — processed (its canonical fact committed, FactKey set) or skipped (did not
// enter the turn, a stable Reason set). A full-skipped batch is legal: every slot
// skipped, no model, still per-slot evidence (spec L136-138).
type slotDisposition string

const (
	slotProcessed slotDisposition = "processed"
	slotSkipped   slotDisposition = "skipped"
)

// Stable, closed skip reasons (design L54: unknown enum values are failures, so a
// skipped slot must carry exactly one of these — never a free-form reason).
// The former third value "empty_input" was a dead enum: an empty input slot is
// NOT skipped — the commit gate still lays down a fact for it and marks it
// processed (§4.3), so no slot ever carried this reason (resident-review-fixes 5.2).
const (
	skipReasonMeditationYield = "meditation_yield" // mixed-batch meditation stepped aside (§4.1)
	skipReasonNotSelected     = "not_selected"     // present but not part of this turn's selected set
)

// validSkipReason is the closed set enforced by validate.
var validSkipReason = map[string]bool{
	skipReasonMeditationYield: true,
	skipReasonNotSelected:     true,
}

// completionSlot is one frozen slot disposition. SourceID is the originating
// AgentEvent id (the durable claim itself has no source id, so the caller threads
// it from the received set). FactKey is the committed fact's hex EventKey (set iff
// processed) — hex, matching Envelope.ReceiptKey, so an identity is never truncated
// through a float decode; jsonEqual decodes numbers with UseNumber regardless.
type completionSlot struct {
	Slot        int             `json:"slot"`
	SourceID    string          `json:"source_id"`
	Disposition slotDisposition `json:"disposition"`
	FactKey     string          `json:"fact_key,omitempty"`
	Reason      string          `json:"reason,omitempty"`
}

// completion is the whole frozen payload. Field set is exactly design L122:
// request id, fixed receipt key, first-generated completion time, batch_result,
// bounded error summary, ordered per-slot dispositions, and the complete receipt
// fact (with first attribution).
type completion struct {
	CompletionVersion int              `json:"completion_version"`
	RequestID         string           `json:"request_id"`
	ReceiptKey        string           `json:"receipt_key"`
	CompletedAtMs     int64            `json:"completed_at_ms"`
	BatchResult       batchResult      `json:"batch_result"`
	ErrorSummary      string           `json:"error_summary,omitempty"`
	Slots             []completionSlot `json:"slots"`
	ReceiptFact       memory.FullEvent `json:"receipt_fact"`
}

// buildReceiptFact mints the frozen inbox-receipt fact. The EventKey is the
// envelope's RESERVED receipt key parsed from hex — NOT a fresh snowflake — so
// re-submitting the receipt is idempotent-by-key and can never add a second
// receipt event; the Timestamp is the frozen completion time, never time.Now. The
// attribution map is the FIRST-hand turn attribution (rollout/trace/trigger),
// captured once; a restart reuses these frozen bytes rather than re-stamping.
func buildReceiptFact(receiptKeyHex string, partitionID int, requestID, agentName string, completedAtMs int64, attribution map[string]string) (memory.FullEvent, error) {
	key, err := tagentevent.ParseEventKey(receiptKeyHex)
	if err != nil {
		return memory.FullEvent{}, fmt.Errorf("completion: parse receipt key %q: %w", receiptKeyHex, err)
	}
	md := map[string]string{
		"inbox_request_id":           requestID,
		tagentevent.MetaKeyAgentName: agentName,
	}
	for k, v := range attribution {
		md[k] = v
	}
	summary := "[inbox receipt] " + requestID
	return memory.FullEvent{
		EventKey:     key,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeInboxReceipt,
		EventSummary: summary,
		Content:      summary,
		Timestamp:    completedAtMs,
		Metadata:     md,
	}, nil
}

// validate enforces the §5.2 invariants: schema version, a non-empty fixed receipt
// key, a batch result in the closed set, and EXACTLY one well-formed disposition
// per slot with no duplicate slot index (processed ⟺ fact_key & no reason; skipped
// ⟺ stable reason & no fact_key). Any deviation is a deterministic error — never
// silently repaired — so a corrupt completion cannot masquerade as processed.
func (c completion) validate() error {
	if c.CompletionVersion != completionVersion {
		return fmt.Errorf("completion: unsupported completion_version %d (want %d)", c.CompletionVersion, completionVersion)
	}
	if c.RequestID == "" {
		return fmt.Errorf("completion: empty request id")
	}
	if c.ReceiptKey == "" {
		return fmt.Errorf("completion: empty receipt key")
	}
	if c.BatchResult != batchCompleted && c.BatchResult != batchFailed {
		return fmt.Errorf("completion: unknown batch_result %q", c.BatchResult)
	}
	// A failed batch must carry its bounded summary; a completed one must not.
	if c.BatchResult == batchFailed && c.ErrorSummary == "" {
		return fmt.Errorf("completion: failed batch without an error summary")
	}
	if c.BatchResult == batchCompleted && c.ErrorSummary != "" {
		return fmt.Errorf("completion: completed batch carrying an error summary")
	}
	if c.ReceiptFact.EventType != tagentevent.TypeInboxReceipt {
		return fmt.Errorf("completion: receipt fact type %q, want %q", c.ReceiptFact.EventType, tagentevent.TypeInboxReceipt)
	}
	seen := make(map[int]bool, len(c.Slots))
	for _, s := range c.Slots {
		if seen[s.Slot] {
			return fmt.Errorf("completion: duplicate slot %d", s.Slot)
		}
		seen[s.Slot] = true
		switch s.Disposition {
		case slotProcessed:
			if s.FactKey == "" {
				return fmt.Errorf("completion: slot %d processed but no fact key", s.Slot)
			}
			if s.Reason != "" {
				return fmt.Errorf("completion: slot %d processed must not carry a reason", s.Slot)
			}
		case slotSkipped:
			if !validSkipReason[s.Reason] {
				return fmt.Errorf("completion: slot %d skipped with unknown/absent reason %q", s.Slot, s.Reason)
			}
			if s.FactKey != "" {
				return fmt.Errorf("completion: slot %d skipped must not carry a fact key", s.Slot)
			}
		default:
			return fmt.Errorf("completion: slot %d unknown disposition %q", s.Slot, s.Disposition)
		}
	}
	return nil
}

// freezeCompletion validates then deterministically marshals a completion. It does
// NOT read the clock or mint keys: the caller supplies the once-captured
// completedAtMs and the frozen receipt fact, so re-invoking on identical inputs
// yields byte-identical output (the property §5.3's idempotent RecordCompletion
// relies on).
func freezeCompletion(c completion) (json.RawMessage, error) {
	c.CompletionVersion = completionVersion
	if err := c.validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("completion: marshal: %w", err)
	}
	return b, nil
}

// decodeCompletion parses a frozen completion and re-validates it, so a restart
// reconciling outstanding envelopes (spec L160+) reads a fully-formed, self-consistent
// record rather than trusting opaque bytes.
func decodeCompletion(raw json.RawMessage) (completion, error) {
	if len(raw) == 0 {
		return completion{}, fmt.Errorf("completion: empty payload")
	}
	var c completion
	if err := json.Unmarshal(raw, &c); err != nil {
		return completion{}, fmt.Errorf("completion: decode: %w", err)
	}
	if err := c.validate(); err != nil {
		return completion{}, err
	}
	return c, nil
}

// ==================== §5.3 freeze-from-received wiring ====================

// slotKey is the (envelope path, fixed slot) identity used to mark which received
// slots were committed this turn (selected) vs filtered out (skipped). The path is
// seq-named and never reused, so it is a stable identity for the turn.
func slotKey(path string, slot int) string { return fmt.Sprintf("%s#%d", path, slot) }

// selectedKeySet indexes the committed slots. By the time finishDurableBatch runs,
// submitDurableBatch returned submitOK, which means EVERY selected fact committed
// (a commit failure is transient and never reaches the finish), so the selected set
// is exactly the processed set.
func selectedKeySet(selected []*AgentEvent) map[string]bool {
	m := make(map[string]bool, len(selected))
	for _, ev := range selected {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" {
			continue
		}
		m[slotKey(ev.claim.Path, ev.claim.Slot)] = true
	}
	return m
}

// groupClaimsByPath buckets the received events by envelope path, preserving
// first-seen order (deterministic) and dropping volatile events (no claim — they
// carry nothing durable to evidence). Returns the ordered paths and the per-path
// groups so one completion is frozen per envelope (Envelope.Completion is per-path).
func groupClaimsByPath(received []*AgentEvent) ([]string, map[string][]*AgentEvent) {
	byPath := make(map[string][]*AgentEvent)
	var order []string
	for _, ev := range received {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" {
			continue
		}
		if _, ok := byPath[ev.claim.Path]; !ok {
			order = append(order, ev.claim.Path)
		}
		byPath[ev.claim.Path] = append(byPath[ev.claim.Path], ev)
	}
	return order, byPath
}

// factKeyOfPrepared extracts the committed fact's hex key from a slot's frozen
// prepared_fact (design 决策2 L74: even skipped slots may carry a prepared fact,
// but only processed slots surface their key in the completion).
func factKeyOfPrepared(prepared json.RawMessage) (string, error) {
	if len(prepared) == 0 {
		return "", fmt.Errorf("completion: processed slot has no prepared fact")
	}
	var f memory.FullEvent
	if err := json.Unmarshal(prepared, &f); err != nil {
		return "", fmt.Errorf("completion: prepared fact undecodable: %w", err)
	}
	if f.EventKey == 0 {
		return "", fmt.Errorf("completion: prepared fact has no fixed identity key")
	}
	return tagentevent.FormatEventKey(f.EventKey), nil
}

// skipReasonFor classifies a non-committed slot with a closed-enum reason. A mixed
// batch's yielding meditation (§4.1) is the recognized filter; anything else present
// in received but not selected is not_selected. Empty-input handling (§4.3) is a
// distinct path that never reaches a committed fact, so it is not produced here.
func skipReasonFor(ev *AgentEvent) string {
	if ev.Type == tagentevent.TypeExternalInput && ev.Source == "meditation" {
		return skipReasonMeditationYield
	}
	return skipReasonNotSelected
}

// buildEnvelopeCompletion freezes the completion for ONE envelope (a path group of
// received events). It maps §5.1's batch outcome to batch_result (a cancelled turn
// yields ok=false and must never be frozen), marks each slot processed (fact key)
// or skipped (reason) from the committed set, and stamps the receipt fact with the
// envelope's reserved key + the once-captured completedAtMs/attribution so the freeze
// is deterministic and an identical retry converges idempotently at RecordCompletion.
func buildEnvelopeCompletion(group []*AgentEvent, committed map[string]bool, outcome turnOutcome, agentName string, partitionID int, completedAtMs int64, attribution map[string]string) (completion, json.RawMessage, error) {
	if len(group) == 0 || group[0].claim == nil {
		return completion{}, nil, fmt.Errorf("completion: empty envelope group")
	}
	result, summary, ok := batchResultFromOutcome(outcome)
	if !ok {
		return completion{}, nil, fmt.Errorf("completion: a cancelled turn forms no completion")
	}
	requestID := group[0].claim.RequestID
	receiptKey := group[0].claim.ReceiptKey
	slots := make([]completionSlot, 0, len(group))
	for _, ev := range group {
		c := ev.claim
		if committed[slotKey(c.Path, c.Slot)] {
			fk, err := factKeyOfPrepared(c.PreparedFact)
			if err != nil {
				return completion{}, nil, fmt.Errorf("completion: rid=%s slot=%d: %w", requestID, c.Slot, err)
			}
			slots = append(slots, completionSlot{Slot: c.Slot, SourceID: ev.ID, Disposition: slotProcessed, FactKey: fk})
			continue
		}
		slots = append(slots, completionSlot{Slot: c.Slot, SourceID: ev.ID, Disposition: slotSkipped, Reason: skipReasonFor(ev)})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Slot < slots[j].Slot })
	receiptFact, err := buildReceiptFact(receiptKey, partitionID, requestID, agentName, completedAtMs, attribution)
	if err != nil {
		return completion{}, nil, fmt.Errorf("completion: rid=%s: %w", requestID, err)
	}
	c := completion{
		CompletionVersion: completionVersion,
		RequestID:         requestID,
		ReceiptKey:        receiptKey,
		CompletedAtMs:     completedAtMs,
		BatchResult:       result,
		ErrorSummary:      summary,
		Slots:             slots,
		ReceiptFact:       receiptFact,
	}
	b, err := freezeCompletion(c)
	if err != nil {
		return completion{}, nil, fmt.Errorf("completion: rid=%s: %w", requestID, err)
	}
	return c, b, nil
}

// verifyReceiptCredential (§5.4, design 决策 L126) turns the durable frozen
// completion into the receipt credential RecordReceipt demands. It is the ONLY
// issuer of credentials in the agent layer: ① decode + re-validate the frozen
// bytes — an illegal completion (wrong version, malformed dispositions, unknown
// batch result) never yields a credential, closing the gap the schema-agnostic
// leaf cannot check (D2); ② bind the receipt fact to its reserved identity — the
// fact's EventKey must BE the completion's receipt_key, an identity drift is a
// contradiction, not a credential; ③ submit/verify the fact through the
// idempotent replay interface — success means the chain now holds the receipt
// under exactly that key (first commit or already-committed, both verify; a
// commit failure is a definite error and yields NO receipt — the claim stays and
// §5.7 re-submits only the receipt, never re-runs the input).
func (cm *ContextManager) verifyReceiptCredential(raw json.RawMessage) (reliability.ReceiptCredential, error) {
	c, err := decodeCompletion(raw)
	if err != nil {
		return reliability.ReceiptCredential{}, err
	}
	if tagentevent.FormatEventKey(c.ReceiptFact.EventKey) != c.ReceiptKey {
		return reliability.ReceiptCredential{}, fmt.Errorf("completion: receipt fact key %d is not the reserved %s — identity contradiction, no credential", c.ReceiptFact.EventKey, c.ReceiptKey)
	}
	if err := cm.commitReceiptFact(c.ReceiptFact); err != nil {
		return reliability.ReceiptCredential{}, err
	}
	return reliability.ReceiptCredential{ReceiptKey: c.ReceiptKey}, nil
}
