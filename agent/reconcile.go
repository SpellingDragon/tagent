package agent

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// reconcile.go — §5.7 startup direct reconcile (design 决策5 L63, spec L162):
// after the cold-start directory barrier and inventory (Inbox.Outstanding),
// EVERY outstanding envelope is checked against its OWN fixed receipt key —
// the confirmation list is never harvested from the projection snapshot/tail
// scan (an older receipt key outside the scan window would be silently missed
// and the input re-executed forever). Per-envelope dispositions follow the
// spec sentence exactly: 只有 prepared → continue input; completion 有而
// receipt 缺 → 只补回执; 两者匹配 → 只清理; 读取 I/O → block (retain,
// never guess); 任一矛盾身份或缺 completion 的 receipt → quarantine + report.
// Reconcile never re-executes the model: the durable completion IS the frozen
// result the receipt is re-submitted from.

// ReconcileSummary counts one startup reconcile pass.
type ReconcileSummary struct {
	Continued     int // no completion: left for the normal Pull path (continue input)
	ReceiptsAdded int // completion-only: frozen receipt re-submitted, then receipt+ack
	Cleaned       int // matching receipt already on chain: cleanup only, no re-submit
	Quarantined   int // contradictory identity / undecodable completion (kept, reported)
	Blocked       int // read I/O or deferred cleanup trouble: retained, retried next boot
}

// ReconcileOutstanding runs one §5.7 pass over the durable inbox. A listing
// failure aborts the whole pass (never reconcile a partial view); per-envelope
// problems are classified individually so one bad envelope cannot block the
// healthy ones. It must run after the retention lease is armed (§2.8/§5.8:
// protection registered before anything converges/cleans) and before the
// consume loop starts feeding producers that may forget.
func (ta *TagentAgent) ReconcileOutstanding() (ReconcileSummary, error) {
	var s ReconcileSummary
	if ta == nil || ta.persistentBus == nil || ta.contextManager == nil {
		return s, nil
	}
	in := ta.persistentBus.inbox
	if in == nil {
		return s, nil // volatile bus: nothing durable to reconcile
	}
	items, err := in.Outstanding()
	if err != nil {
		return s, fmt.Errorf("reconcile: inventory: %w", err)
	}
	cm := ta.contextManager
	for _, it := range items {
		if it.ReadErr != nil {
			// Present but unreadable NOW (open already quarantined corrupt items):
			// conservative block — retain, surface, never guess a confirmation.
			s.Blocked++
			log.Errorf("[reconcile] %s unreadable during inventory — retained, NOT disposed: %v", it.Path, it.ReadErr)
			continue
		}
		env := it.Env
		if len(env.Completion) == 0 {
			if env.State == reliability.InboxStateReceipted {
				// 「任何缺 completion 的 receipt」(spec L162): a receipted state with no
				// durable completion is a contradiction the Ack gate already refuses —
				// reconcile quarantines and reports it, it must NOT fall through to
				// "continue input" (the envelope would loop ack-refusal forever).
				ta.quarantineReconcile(&s, it.Path, "state is receipted without a durable completion")
				continue
			}
			s.Continued++ // prepared-only / plain pending → continue input via Pull
			continue
		}
		c, derr := decodeCompletion(env.Completion)
		if derr != nil {
			ta.quarantineReconcile(&s, it.Path, "durable completion is not decodable: "+derr.Error())
			continue
		}
		if verr := reconcileIdentityChecks(c, env); verr != nil {
			ta.quarantineReconcile(&s, it.Path, verr.Error())
			continue
		}
		key, kerr := tagentevent.ParseEventKey(c.ReceiptKey)
		if kerr != nil {
			ta.quarantineReconcile(&s, it.Path, fmt.Sprintf("reserved receipt key %q unparseable", c.ReceiptKey))
			continue
		}
		stored, gerr := cm.memStore.GetEvent(key)
		switch {
		case gerr == nil:
			// Receipt on the chain under the reserved key: it must BE the frozen
			// fact (byte-equal canonical), otherwise a different identity owns the
			// key — a deterministic contradiction, never cleaned up by guessing.
			if !sameReceiptFact(*stored, c.ReceiptFact) {
				ta.quarantineReconcile(&s, it.Path, fmt.Sprintf("chain receipt under key %s differs from the frozen receipt fact", c.ReceiptKey))
				continue
			}
			// 两者匹配 → 只清理：no re-submit, no re-execution, just receipt+ack.
			if cerr := ta.persistentBus.ConfirmDurable(it.Path, reliability.ReceiptCredential{ReceiptKey: c.ReceiptKey}); cerr != nil {
				s.Blocked++
				log.Errorf("[reconcile] cleanup of %s deferred (ack barrier will be completed by drain/next boot): %v", it.Path, cerr)
				continue
			}
			s.Cleaned++
		case errors.Is(gerr, memory.ErrKeyNotFound), memory.IsEventForgotten(gerr):
			// completion-only (or the receipt aged out of the 30d window): re-submit
			// ONLY the frozen receipt through the idempotent replay interface, then
			// credentialed receipt + ack. A legally-forgotten receipt makes the
			// re-submit itself a DEFINITE refusal (ErrEventForgotten) → quarantine.
			cred, verr := cm.verifyReceiptCredential(env.Completion)
			if verr != nil {
				if memory.IsEventForgotten(verr) || memory.IsDuplicateEventKey(verr) {
					ta.quarantineReconcile(&s, it.Path, "receipt re-submit refused deterministically: "+verr.Error())
					continue
				}
				s.Blocked++
				log.Warnf("[reconcile] receipt re-submit for %s blocked (transient) — claim retained: %v", it.Path, verr)
				continue
			}
			if cerr := ta.persistentBus.ConfirmDurable(it.Path, cred); cerr != nil {
				s.Blocked++
				log.Warnf("[reconcile] confirm after re-submit for %s deferred: %v", it.Path, cerr)
				continue
			}
			s.ReceiptsAdded++
		default:
			// Read I/O failure: block this envelope conservatively; nothing disposed.
			s.Blocked++
			log.Errorf("[reconcile] receipt lookup for %s key=%s failed (I/O) — retained, NOT disposed: %v", it.Path, c.ReceiptKey, gerr)
		}
	}
	log.Infof("[reconcile] §5.7 direct reconcile: continued=%d receipts_added=%d cleaned=%d quarantined=%d blocked=%d",
		s.Continued, s.ReceiptsAdded, s.Cleaned, s.Quarantined, s.Blocked)
	return s, nil
}

// quarantineReconcile isolates one contradictory envelope (bytes kept for
// inspection, capacity freed through the leaf's quarantine accounting) and
// counts it. Contradictions never silently consume or delete originals.
func (ta *TagentAgent) quarantineReconcile(s *ReconcileSummary, path, reason string) {
	s.Quarantined++
	ta.persistentBus.QuarantineEnvelope(path, "§5.7 reconcile: "+reason)
	log.Errorf("[reconcile] QUARANTINED %s — %s", path, reason)
}

// reconcileIdentityChecks verifies the frozen completion against the envelope's
// own reservations (spec L162「核对…request ID、准备身份、逐槽结果」): request id,
// reserved receipt key, and every processed slot's fact key equals the slot's
// prepared identity frozen at write-before-prepare time. Any drift is a
// contradiction — the envelope is isolated, never re-stamped.
func reconcileIdentityChecks(c completion, env *reliability.Envelope) error {
	if c.RequestID != env.RequestID {
		return fmt.Errorf("completion request id %q does not match envelope %q", c.RequestID, env.RequestID)
	}
	if env.ReceiptKey == "" || c.ReceiptKey != env.ReceiptKey {
		return fmt.Errorf("completion receipt key %q does not match reserved %q", c.ReceiptKey, env.ReceiptKey)
	}
	for _, slot := range c.Slots {
		if slot.Disposition != slotProcessed {
			continue
		}
		if slot.Slot < 0 || slot.Slot >= len(env.Messages) {
			return fmt.Errorf("processed slot %d out of envelope range (%d slots)", slot.Slot, len(env.Messages))
		}
		fk, err := factKeyOfPrepared(env.Messages[slot.Slot].PreparedFact)
		if err != nil {
			return fmt.Errorf("slot %d prepared identity unreadable: %w", slot.Slot, err)
		}
		if fk != slot.FactKey {
			return fmt.Errorf("slot %d: completion fact key %q does not match prepared identity %q", slot.Slot, slot.FactKey, fk)
		}
	}
	return nil
}

// sameReceiptFact compares a chain-read receipt with the frozen fact field by
// field (the canonical identity the envelope's completion attests to). Metadata
// maps compare deep-equal; the chain is immutable, so any difference means a
// different record owns the key — a contradiction, not a match.
func sameReceiptFact(stored, frozen memory.FullEvent) bool {
	return stored.EventKey == frozen.EventKey &&
		stored.PartitionID == frozen.PartitionID &&
		stored.EventType == frozen.EventType &&
		stored.EventSummary == frozen.EventSummary &&
		stored.Timestamp == frozen.Timestamp &&
		stored.Content == frozen.Content &&
		reflect.DeepEqual(stored.Metadata, frozen.Metadata)
}
