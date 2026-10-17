// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

const taskBatchSignalsKey = "task_batch_signals"

type taskBatchSignalRef struct {
	RawKey     int64  `json:"raw_key"`
	ReceiptKey int64  `json:"receipt_key"`
	SourceID   string `json:"source_id"`
	TaskID     string `json:"task_id"`
	RoundID    uint64 `json:"round_id"`
	Seq        uint64 `json:"seq"`
	Owner      string `json:"owner"`
}

func taskBatchID(refs []taskBatchSignalRef) string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.SourceID)
	}
	sort.Strings(ids)
	digest := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return fmt.Sprintf("task-retire-batch-%x", digest[:])
}

func taskBatchRefs(source memory.FullEvent) ([]taskBatchSignalRef, bool) {
	var refs []taskBatchSignalRef
	if json.Unmarshal([]byte(source.Metadata[taskBatchSignalsKey]), &refs) != nil || len(refs) == 0 || source.Metadata[tagentevent.MetaKeySourceEventID] != taskBatchID(refs) {
		return nil, false
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.RawKey <= 0 || ref.ReceiptKey <= 0 || ref.SourceID == "" || seen[ref.SourceID] {
			return nil, false
		}
		seen[ref.SourceID] = true
	}
	return refs, true
}

func taskBatchSourceMatches(fact memory.FullEvent, raw taskSignalRecord, source memory.FullEvent) bool {
	refs, ok := taskBatchRefs(source)
	if !ok || raw.Event == nil {
		return false
	}
	for _, ref := range refs {
		if ref.RawKey == fact.EventKey && ref.ReceiptKey == raw.ReceiptKey && ref.SourceID == raw.Event.ID && ref.TaskID == raw.TaskID && ref.RoundID == raw.RoundID && ref.Seq == raw.Seq && ref.Owner == fact.Metadata[tagentevent.MetaKeyAgentName] {
			return true
		}
	}
	return false
}

func releaseConsumedTaskBatch(cm *ContextManager, evt *AgentEvent, source *memory.FullEvent) error {
	refs, ok := taskBatchRefs(*source)
	if !ok || evt.ID != source.Metadata[tagentevent.MetaKeySourceEventID] || compress.VerifiedInputDisposition(cm.memStore, source) == "" {
		return fmt.Errorf("task batch has no exact consumption proof")
	}
	for _, ref := range refs {
		store, journal := cm.memStore, cm.taskSignalJournal
		if ref.Owner != cm.name && cm.taskSignalOwnerResolver != nil {
			owner := cm.taskSignalOwnerResolver(ref.Owner)
			if owner == nil {
				continue
			}
			store, journal = owner.memStore, owner.taskRecords
		}
		if store == nil || journal == nil {
			continue
		}
		fact, err := store.GetEvent(ref.RawKey)
		if err != nil || fact == nil {
			continue
		}
		var raw taskSignalRecord
		if fact.EventType != tagentevent.TypeTaskSignal || json.Unmarshal([]byte(fact.Content), &raw) != nil || !taskBatchSourceMatches(*fact, raw, *source) {
			return fmt.Errorf("task batch references a different frozen producer signal")
		}
		journal.markSignalConsumed(ref.RawKey, ref.ReceiptKey)
	}
	return nil
}
