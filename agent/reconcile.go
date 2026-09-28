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

// ReconcileSummary counts one startup reconcile pass.
type ReconcileSummary struct {
	Continued     int
	ReceiptsAdded int
	Cleaned       int
	Quarantined   int
	Blocked       int
}

// ReconcileOutstanding runs one  pass over the durable inbox. A listing
// failure aborts the whole pass (never reconcile a partial view); per-envelope
// problems are classified individually so one bad envelope cannot block the
// healthy ones. It must run after the retention lease is armed (/:
// protection registered before anything converges/cleans) and before the
// consume loop starts feeding producers that may forget.
func (ta *TagentAgent) ReconcileOutstanding() (ReconcileSummary, error) {
	var s ReconcileSummary
	if ta == nil || ta.persistentBus == nil || ta.contextManager == nil {
		return s, nil
	}
	in := ta.persistentBus.inbox
	if in == nil {
		return s, nil
	}
	items, err := in.Outstanding()
	if err != nil {
		return s, fmt.Errorf("reconcile: inventory: %w", err)
	}
	cm := ta.contextManager
	for _, it := range items {
		if it.ReadErr != nil {
			s.Blocked++
			log.Errorf("[reconcile] %s unreadable during inventory — retained, NOT disposed: %v", it.Path, it.ReadErr)
			continue
		}
		env := it.Env
		if len(env.Completion) == 0 {
			if env.State == reliability.InboxStateReceipted {
				ta.quarantineReconcile(&s, it.Path, "state is receipted without a durable completion")
				continue
			}
			s.Continued++
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
			if !sameReceiptFact(*stored, c.ReceiptFact) {
				ta.quarantineReconcile(&s, it.Path, fmt.Sprintf("chain receipt under key %s differs from the frozen receipt fact", c.ReceiptKey))
				continue
			}
			if cerr := ta.persistentBus.ConfirmDurable(it.Path, reliability.ReceiptCredential{ReceiptKey: c.ReceiptKey}); cerr != nil {
				s.Blocked++
				log.Errorf("[reconcile] cleanup of %s deferred (ack barrier will be completed by drain/next boot): %v", it.Path, cerr)
				continue
			}
			s.Cleaned++
		case errors.Is(gerr, memory.ErrKeyNotFound), memory.IsEventForgotten(gerr):
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
// own reservations: request id,
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
