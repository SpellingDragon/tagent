// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

func inlineTaskTurnToken(cm *ContextManager, events []*AgentEvent) string {
	parts := []string{cm.name, cm.continuationID, cm.sessionID}
	for _, evt := range events {
		if evt == nil {
			continue
		}
		identity := evt.ID
		if fact, err := taskInputFact(evt); err == nil {
			identity += "/" + tagentevent.FormatEventKey(fact.EventKey)
		}
		parts = append(parts, identity)
	}
	sort.Strings(parts[3:])
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return fmt.Sprintf("inline-turn-%x", digest[:])
}

func inlineTaskDisposition(fact memory.FullEvent, raw taskSignalRecord) memory.FullEvent {
	md := map[string]string{
		tagentevent.MetaKeyAgentName:        fact.Metadata[tagentevent.MetaKeyAgentName],
		tagentevent.MetaKeyConsumedEventKey: tagentevent.FormatEventKey(fact.EventKey),
		tagentevent.MetaKeyInputDisposition: "inline_processed",
		tagentevent.MetaKeySourceEventID:    raw.Event.ID,
		tagentevent.MetaKeyTaskRoundID:      fmt.Sprint(raw.RoundID),
		tagentevent.MetaKeyTaskRoundOwner:   raw.RoundOwner,
		"task_id":                           raw.TaskID,
		"task_inline_turn":                  raw.InlineTurnToken,
	}
	if raw.RoundOwner != "" {
		md[tagentevent.MetaKeyContinuationID] = raw.RoundOwner
	}
	return memory.FullEvent{EventKey: raw.InlineReceiptKey, PartitionID: fact.PartitionID, EventType: tagentevent.TypeInputDisposition,
		Timestamp: fact.Timestamp, Content: "inline_processed " + tagentevent.FormatEventKey(fact.EventKey), EventSummary: "inline task result consumed", Metadata: md}
}

func inlineTaskSignalConsumed(store memory.MemoryStore, fact memory.FullEvent, raw taskSignalRecord) bool {
	if store == nil || !raw.Inline || raw.Event == nil || raw.InlineReceiptKey <= 0 || raw.InlineTurnToken == "" {
		return false
	}
	proof, err := store.GetEvent(raw.InlineReceiptKey)
	return err == nil && proof != nil && sameReceiptFact(*proof, inlineTaskDisposition(fact, raw))
}

func finishInlineSignalsForTurn(ctx context.Context, cm *ContextManager) bool {
	if cm == nil || cm.taskSignalJournal == nil || cm.memStore == nil || cm.inlineTaskTurnToken == "" {
		return true
	}
	sink := cm.taskSignalJournal
	j := &sink.delivery
	j.mu.Lock()
	keys := make([]int64, 0, len(j.leases))
	for key := range j.leases {
		keys = append(keys, key)
	}
	j.mu.Unlock()
	for _, key := range keys {
		j.mu.Lock()
		var frozen *frozenTaskDelivery
		for _, d := range j.pending {
			if d.fact.EventKey == key {
				frozen = d
				break
			}
		}
		j.mu.Unlock()
		fact, err := cm.memStore.GetEvent(key)
		if frozen != nil {
			copyFact := frozen.fact
			fact = &copyFact
		} else if err != nil || fact == nil {
			continue
		}
		var raw taskSignalRecord
		if json.Unmarshal([]byte(fact.Content), &raw) != nil {
			return false
		}
		if !raw.Inline || raw.InlineTurnToken != cm.inlineTaskTurnToken || raw.RoundOwner != cm.continuationID {
			continue
		}
		for !sink.commitFrozen(*fact) {
			if !waitTaskInputRetry(ctx) {
				return false
			}
		}
		proof, err := cm.memStore.GetEvent(raw.ReceiptKey)
		if frozen != nil && (err != nil || proof == nil) {
			for !sink.commitFrozen(frozen.receipt) {
				if !waitTaskInputRetry(ctx) {
					return false
				}
			}
			if !sink.ackChildResult(*fact, frozen.receipt) {
				return false
			}
			j.mu.Lock()
			frozen.received = true
			j.mu.Unlock()
			copyProof := frozen.receipt
			proof, err = &copyProof, nil
		}
		if err != nil || proof == nil || !taskSignalReceiptMatches(*fact, raw, *proof) {
			continue
		}
		if !sink.ackChildResult(*fact, *proof) {
			return false
		}
		sink.markSignalAccepted(key, raw.ReceiptKey)
		if raw.InlineReceiptKey <= 0 || raw.Event == nil {
			return false
		}
		for !sink.commitFrozen(inlineTaskDisposition(*fact, raw)) {
			if !waitTaskInputRetry(ctx) {
				return false
			}
		}
		if !inlineTaskSignalConsumed(cm.memStore, *fact, raw) {
			return false
		}
		sink.markSignalConsumed(key, raw.ReceiptKey)
	}
	return true
}
