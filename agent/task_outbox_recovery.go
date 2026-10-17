// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// RecoverTaskOutbox repairs accepted task inputs whose receiver had not formally consumed them before restart.
func (ta *TagentAgent) RecoverTaskOutbox() error {
	if ta == nil || ta.memStore == nil || ta.taskRecords == nil {
		return nil
	}
	records, err := queryTaskRecords(ta.memStore, ta.partitionID(), []string{tagentevent.TypeTaskSignal, tagentevent.TypeTaskSignalReceipt})
	if err != nil {
		return err
	}
	sources, err := queryTaskRecords(ta.memStore, ta.partitionID(), []string{tagentevent.TypeExternalInput})
	if err != nil {
		return err
	}
	proofs := make(map[int64]memory.FullEvent)
	inputs := make(map[string][]memory.FullEvent)
	for _, fact := range records {
		if fact.EventType == tagentevent.TypeTaskSignalReceipt {
			proofs[fact.EventKey] = fact
		}
	}
	for _, source := range sources {
		if id := source.Metadata[tagentevent.MetaKeySourceEventID]; id != "" {
			inputs[id] = append(inputs[id], source)
		}
	}
	scanned := map[string]error{ta.name: nil}
	var failures []error
	for _, fact := range records {
		if fact.EventType != tagentevent.TypeTaskSignal {
			continue
		}
		var raw taskSignalRecord
		if json.Unmarshal([]byte(fact.Content), &raw) != nil || raw.Event == nil || raw.ReceiptKey <= 0 || raw.ErrorKind == "continuation_interrupted" {
			continue
		}
		ta.taskRecords.retainSignalKeys(fact.EventKey, raw.ReceiptKey)
		inventoryReady := true
		for _, receiver := range ta.taskSignalRecoveryOwners(raw.RoundOwner, make(map[string]bool)) {
			if receiver == nil || receiver.memStore == nil {
				continue
			}
			if scanErr, exists := scanned[receiver.name]; exists {
				inventoryReady = inventoryReady && scanErr == nil
				continue
			}
			facts, err := queryTaskRecords(receiver.memStore, receiver.partitionID(), []string{tagentevent.TypeExternalInput})
			scanned[receiver.name] = err
			if err != nil {
				failures = append(failures, err)
				inventoryReady = false
				continue
			}
			for _, source := range facts {
				if id := source.Metadata[tagentevent.MetaKeySourceEventID]; id != "" {
					inputs[id] = append(inputs[id], source)
				}
			}
		}
		if !inventoryReady {
			continue
		}
		var source *memory.FullEvent
		consumed := inlineTaskSignalConsumed(ta.memStore, fact, raw) || ta.continuationInlineSignalConsumed(fact, raw)
		for _, candidate := range inputs[raw.Event.ID] {
			if !taskSignalSourceMatches(fact, raw, candidate) {
				continue
			}
			copySource := candidate
			source = &copySource
			if taskSignalInputConsumed(ta, raw.RoundOwner, source) {
				consumed = true
				break
			}
		}
		proof, received := proofs[raw.ReceiptKey]
		received = received && taskSignalReceiptMatches(fact, raw, proof)
		if !received && consumed {
			var err error
			proof, err = ta.restoreTaskOutboxProof(fact, raw)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			received = true
		}
		inlineEntry := raw.Inline && raw.RoundOwner == ""
		if !received && !inlineEntry {
			continue
		}
		acknowledged := received && ta.taskRecords.ackChildResult(fact, proof)
		if received && !acknowledged {
			failures = append(failures, fmt.Errorf("task outbox %d child acknowledgement is pending", fact.EventKey))
		}
		if acknowledged {
			ta.taskRecords.markSignalAccepted(fact.EventKey, raw.ReceiptKey)
		}
		if consumed {
			if acknowledged {
				ta.clearConsumedTaskDelivery(fact, raw)
				ta.taskRecords.releaseSignalKeys(fact.EventKey, raw.ReceiptKey)
			}
			continue
		}
		if raw.Inline && !inlineEntry {
			continue
		}
		bus, receiver, scope, err := ta.recoveredTaskReceiver(raw.RoundOwner, make(map[string]bool))
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if scope != nil {
			scope.mu.Lock()
			queued := continuationHasRecoveryInput(scope, raw.Event.ID)
			scope.mu.Unlock()
			if queued {
				continue
			}
		}
		marker := fmt.Sprintf("outbox_replay/%d", fact.EventKey)
		j := &ta.taskRecords.delivery
		j.mu.Lock()
		if j.phases == nil {
			j.phases = make(map[string]memory.FullEvent)
		}
		_, replayed := j.phases[marker]
		if !replayed {
			j.phases[marker] = memory.FullEvent{EventKey: fact.EventKey}
		}
		j.mu.Unlock()
		if replayed {
			if inlineEntry && !received {
				proof, err := ta.restoreTaskOutboxProof(fact, raw)
				if err != nil {
					failures = append(failures, err)
				} else if ta.taskRecords.ackChildResult(fact, proof) {
					ta.taskRecords.markSignalAccepted(fact.EventKey, raw.ReceiptKey)
				}
			}
			continue
		}
		evt := raw.Event
		if source != nil {
			evt.volatileFact, evt.volatileCommitted = source, true
		}
		ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
		_, publishErr := bus.PublishContext(ctx, evt)
		cancel()
		if publishErr != nil {
			j.mu.Lock()
			delete(j.phases, marker)
			j.mu.Unlock()
			failures = append(failures, publishErr)
			continue
		}
		if inlineEntry && !received {
			proof, err := ta.restoreTaskOutboxProof(fact, raw)
			if err != nil {
				failures = append(failures, err)
			} else if !ta.taskRecords.ackChildResult(fact, proof) {
				failures = append(failures, fmt.Errorf("task outbox %d child acknowledgement is pending", fact.EventKey))
			} else {
				ta.taskRecords.markSignalAccepted(fact.EventKey, raw.ReceiptKey)
			}
		}
		if scope != nil {
			scope.mu.Lock()
			scope.acceptedInputs = append(scope.acceptedInputs, evt)
			id := scope.state.InvocationID
			scope.mu.Unlock()
			receiver.settleSinks.reconcileAccepted(id, []*AgentEvent{evt})
		}
	}
	return errors.Join(failures...)
}

func (ta *TagentAgent) restoreTaskOutboxProof(fact memory.FullEvent, raw taskSignalRecord) (memory.FullEvent, error) {
	if !ta.taskRecords.restorePendingSignal(fact) {
		return memory.FullEvent{}, fmt.Errorf("task outbox %d cannot restore its frozen delivery", fact.EventKey)
	}
	j := &ta.taskRecords.delivery
	j.mu.Lock()
	frozen := j.pending[raw.Event.ID]
	frozen.received = true
	proof := frozen.receipt
	j.mu.Unlock()
	if !ta.taskRecords.commitFrozen(proof) {
		return memory.FullEvent{}, fmt.Errorf("task outbox %d cannot commit its recovered receiver proof", fact.EventKey)
	}
	return proof, nil
}

func (ta *TagentAgent) clearConsumedTaskDelivery(fact memory.FullEvent, raw taskSignalRecord) {
	j := &ta.taskRecords.delivery
	j.mu.Lock()
	delete(j.phases, fmt.Sprintf("outbox_replay/%d", fact.EventKey))
	j.mu.Unlock()
	if ta.taskManager != nil {
		ta.taskManager.ResolveRestoredDelivery(raw.TaskID, raw.Event.ID)
		if tk, ok := ta.taskManager.Get(raw.TaskID); ok {
			for _, sig := range tk.PendingSignals() {
				if sig.SourceID == raw.Event.ID {
					return
				}
			}
		}
	}
	j.mu.Lock()
	delete(j.pending, raw.Event.ID)
	j.mu.Unlock()
}

func continuationHasRecoveryInput(scope *continuation, id string) bool {
	for _, batch := range [][]*AgentEvent{scope.acceptedInputs, scope.replayInputs} {
		for _, evt := range batch {
			if evt != nil && evt.ID == id {
				return true
			}
		}
	}
	return false
}

func (ta *TagentAgent) taskSignalRecoveryOwners(invocationID string, visited map[string]bool) []*TagentAgent {
	if ta == nil || visited[ta.name+"/"+invocationID] {
		return nil
	}
	visited[ta.name+"/"+invocationID] = true
	owners := []*TagentAgent{ta}
	if invocationID == "" {
		return owners
	}
	scope, err := ta.continuationByID(invocationID)
	if err != nil || scope == nil {
		return owners
	}
	scope.mu.Lock()
	handoff := scope.state.Handoff
	scope.mu.Unlock()
	if handoff != nil {
		parent := ta.resolveContinuationOwner(handoff.ParentAgent)
		owners = append(owners, parent.taskSignalRecoveryOwners(handoff.ParentInvocationID, visited)...)
	}
	return owners
}

func taskSignalInputConsumed(ta *TagentAgent, ownerID string, source *memory.FullEvent) bool {
	for _, owner := range ta.taskSignalRecoveryOwners(ownerID, make(map[string]bool)) {
		if owner != nil && owner.memStore != nil && compress.VerifiedInputDisposition(owner.memStore, source) != "" {
			return true
		}
	}
	return false
}

func (ta *TagentAgent) recoveredTaskReceiver(invocationID string, visited map[string]bool) (*EventBus, *TagentAgent, *continuation, error) {
	address := ta.name + "/" + invocationID
	if visited[address] {
		return nil, nil, nil, fmt.Errorf("task outbox receiver handoff is cyclic at %s", address)
	}
	visited[address] = true
	if invocationID == "" {
		if ta.persistentBus == nil {
			return nil, nil, nil, fmt.Errorf("task outbox receiver %s is unavailable", address)
		}
		return ta.persistentBus, ta, nil, nil
	}
	scope, err := ta.continuationByID(invocationID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("task outbox receiver %s has no continuation: %w", address, err)
	}
	if scope == nil {
		return nil, nil, nil, fmt.Errorf("task outbox receiver %s has no continuation", address)
	}
	scope.mu.Lock()
	bus, handoff, outcome := scope.bus, scope.state.Handoff, scope.state.Outcome
	scope.mu.Unlock()
	if handoff != nil {
		parent := ta.resolveContinuationOwner(handoff.ParentAgent)
		if parent == nil {
			return nil, nil, nil, fmt.Errorf("task outbox handoff receiver %q is unavailable", handoff.ParentAgent)
		}
		return parent.recoveredTaskReceiver(handoff.ParentInvocationID, visited)
	}
	if bus == nil || outcome != "" {
		return nil, nil, nil, fmt.Errorf("task outbox receiver %s has no live destination", address)
	}
	return bus, ta, scope, nil
}

func taskSignalSourceMatches(fact memory.FullEvent, raw taskSignalRecord, source memory.FullEvent) bool {
	if owner := source.Metadata["task_signal_owner"]; owner != "" && owner != fact.Metadata[tagentevent.MetaKeyAgentName] {
		return false
	}
	return source.Metadata["task_signal_key"] == tagentevent.FormatEventKey(fact.EventKey) &&
		source.Metadata[tagentevent.MetaKeySourceEventID] == raw.Event.ID &&
		source.Metadata["task_id"] == raw.TaskID &&
		source.Metadata[tagentevent.MetaKeyTaskRoundID] == strconv.FormatUint(raw.RoundID, 10) &&
		source.Metadata[tagentevent.MetaKeyTaskSignalSeq] == strconv.FormatUint(raw.Seq, 10)
}

func releaseConsumedTaskSignal(cm *ContextManager, evt *AgentEvent, source *memory.FullEvent) error {
	if cm == nil || cm.memStore == nil || evt == nil || evt.Source != SourceTask || source == nil {
		return nil
	}
	keyText := source.Metadata["task_signal_key"]
	if keyText == "" {
		return nil
	}
	key, err := tagentevent.ParseEventKey(keyText)
	if err != nil || key <= 0 || tagentevent.FormatEventKey(key) != keyText {
		return fmt.Errorf("task input has an invalid raw signal key")
	}
	producerStore, journal := cm.memStore, cm.taskSignalJournal
	ownerName := source.Metadata["task_signal_owner"]
	if ownerName == "" {
		ownerName, _ = evt.Metadata["task_signal_owner"].(string)
	}
	if ownerName != "" && ownerName != cm.name && cm.taskSignalOwnerResolver != nil {
		if owner := cm.taskSignalOwnerResolver(ownerName); owner != nil && owner.memStore != nil {
			producerStore, journal = owner.memStore, owner.taskRecords
		}
	}
	fact, err := producerStore.GetEvent(key)
	if err != nil || fact == nil {
		return nil
	}
	var raw taskSignalRecord
	if fact.EventType != tagentevent.TypeTaskSignal || json.Unmarshal([]byte(fact.Content), &raw) != nil || raw.Event == nil || raw.Event.ID != evt.ID || !taskSignalSourceMatches(*fact, raw, *source) || memory.PartitionIDFromEventKey(key) != fact.PartitionID || memory.PartitionIDFromEventKey(raw.ReceiptKey) != fact.PartitionID {
		return fmt.Errorf("task input does not match its frozen raw signal")
	}
	if compress.VerifiedInputDisposition(cm.memStore, source) == "" {
		return fmt.Errorf("task input has no formal consumption proof")
	}
	if journal != nil {
		journal.delivery.mu.Lock()
		delete(journal.delivery.phases, fmt.Sprintf("outbox_replay/%d", fact.EventKey))
		journal.delivery.mu.Unlock()
		journal.markSignalConsumed(fact.EventKey, raw.ReceiptKey)
	}
	return nil
}
