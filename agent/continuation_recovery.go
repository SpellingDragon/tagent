// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func continuationFacts(store memory.MemoryStore, partitionID int) ([]memory.FullEvent, error) {
	if store == nil {
		return nil, errors.New("continuation recovery has no store")
	}
	var facts []memory.FullEvent
	for offset := 0; ; offset += 500 {
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{partitionID}, Limit: 500, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("continuation recovery page %d: %w", offset, err)
		}
		keys := make([]int64, 0, len(refs))
		for _, ref := range refs {
			keys = append(keys, ref.EventKey)
		}
		batch, err := store.GetEvents(keys)
		if err != nil {
			return nil, fmt.Errorf("continuation recovery hydration: %w", err)
		}
		if len(batch) != len(keys) {
			return nil, errors.New("continuation recovery contains missing facts")
		}
		for _, fact := range batch {
			if fact.PartitionID == partitionID {
				facts = append(facts, fact)
			}
		}
		if len(refs) < 500 {
			break
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].EventKey < facts[j].EventKey })
	if len(facts) > 0 {
		memory.RaiseSnowflakeFloor(partitionID, facts[len(facts)-1].EventKey)
	}
	return facts, nil
}

func (ta *TagentAgent) readContinuationCheckpoints() (map[string]*continuation, []memory.FullEvent, error) {
	facts, err := continuationFacts(ta.memStore, ta.partitionID())
	if err != nil {
		return nil, nil, err
	}
	entries := make(map[string]*continuation)
	for _, fact := range facts {
		if fact.EventType != tagentevent.TypeContinuationState || fact.Metadata[tagentevent.MetaKeyAgentName] != ta.name {
			continue
		}
		var state continuationCheckpoint
		if err := json.Unmarshal([]byte(fact.Content), &state); err != nil {
			return nil, nil, fmt.Errorf("continuation checkpoint %d: %w", fact.EventKey, err)
		}
		if state.Version != 1 || state.InvocationID == "" || state.InvocationID != fact.Metadata[tagentevent.MetaKeyContinuationID] {
			return nil, nil, fmt.Errorf("continuation checkpoint %d has invalid identity", fact.EventKey)
		}
		entries[state.InvocationID] = &continuation{state: state, stateKey: fact.EventKey, recovered: true, protected: make(map[int64]struct{}), listeners: make(map[chan *event.Event]struct{})}
	}
	return entries, facts, nil
}

func (ta *TagentAgent) continuationByID(id string) (*continuation, error) {
	r := ta.continuationRegistry()
	r.mu.Lock()
	existing := r.byID[id]
	r.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	if ta.memStore == nil {
		return nil, nil
	}
	var latest *memory.FullEvent
	for offset := 0; ; offset += 100 {
		refs, err := ta.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{ta.partitionID()}, EventTypes: []string{tagentevent.TypeContinuationState}, Keyword: id, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			f, err := ta.memStore.GetEvent(ref.EventKey)
			if err != nil {
				return nil, err
			}
			if f != nil && f.Metadata[tagentevent.MetaKeyAgentName] == ta.name && f.Metadata[tagentevent.MetaKeyContinuationID] == id && (latest == nil || f.EventKey > latest.EventKey) {
				latest = f
			}
		}
		if len(refs) < 100 {
			break
		}
	}
	if latest == nil {
		return nil, nil
	}
	var state continuationCheckpoint
	if err := json.Unmarshal([]byte(latest.Content), &state); err != nil {
		return nil, err
	}
	if state.Version != 1 || state.InvocationID != id {
		return nil, errors.New("continuation checkpoint identity is invalid")
	}
	c := &continuation{state: state, stateKey: latest.EventKey, recovered: true, protected: make(map[int64]struct{}), listeners: make(map[chan *event.Event]struct{})}
	r.mu.Lock()
	if prior := r.byID[id]; prior != nil {
		c = prior
	} else {
		r.byID[id] = c
	}
	r.mu.Unlock()
	return c, nil
}

// RecoveredContinuationBindingsFromWAL reads exact original parent addresses without starting work.
func (ta *TagentAgent) RecoveredContinuationBindingsFromWAL() ([]ContinuationBinding, error) {
	if ta == nil {
		return nil, nil
	}
	entries, _, err := ta.readContinuationCheckpoints()
	if err != nil {
		return nil, err
	}
	bindings := make([]ContinuationBinding, 0, len(entries))
	for _, c := range entries {
		a := c.state.Address
		bindings = append(bindings, ContinuationBinding{InvocationID: c.state.InvocationID, ParentAgent: a.ParentAgent, ParentInvocationID: a.ParentInvocationID, ParentTaskID: a.ParentTaskID, ParentTaskRound: a.ParentTaskRound, State: c.state.State})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].InvocationID < bindings[j].InvocationID })
	return bindings, nil
}

// RebuildContinuationsFromWAL restores private projections, waiting rounds and result caches.
func (ta *TagentAgent) RebuildContinuationsFromWAL() (int, error) {
	if ta == nil {
		return 0, nil
	}
	guard, _ := ta.memStore.(memory.RetentionGuard)
	if guard != nil {
		guard.BeginHold()
		defer guard.EndHold()
	}
	entries, facts, err := ta.readContinuationCheckpoints()
	if err != nil {
		return 0, err
	}
	r := ta.continuationRegistry()
	restored := 0
	for id, c := range entries {
		r.mu.Lock()
		prior := r.byID[id]
		r.mu.Unlock()
		if prior != nil {
			continue
		}
		if c.state.Outcome == "" {
			c.state.State = "suspended"
		}
		c.recoveryGoalClosed = c.state.RejectedBeforeExecution
		if err := ta.foldContinuationRecovery(c, facts); err != nil {
			return restored, err
		}
		c.recoveryFolded = true
		c.mu.Lock()
		if err := ta.openContinuationBus(c); err != nil {
			c.mu.Unlock()
			return restored, err
		}
		if err := ta.reconcileContinuationInboxLocked(c); err != nil {
			c.mu.Unlock()
			return restored, err
		}
		if err := ta.repairConsumedContinuationInboxLocked(context.Background(), c); err != nil {
			c.mu.Unlock()
			return restored, err
		}
		ta.protectContinuationLocked(c)
		c.mu.Unlock()
		r.mu.Lock()
		if r.byID[id] == nil {
			r.byID[id] = c
			restored++
		}
		r.mu.Unlock()
		ta.bindSettleBus(id, c.bus)
		ta.settleSinks.restoreRouting(id, c.state.Routing, ta.taskManager)
		ta.settleSinks.reconcileAccepted(id, c.acceptedInputs)
		if c.state.Outcome != "" {
			ta.settleSinks.retire(id)
		}
	}
	if guard != nil {
		guard.ArmRetention()
	}
	return restored, nil
}

func continuationFactBelongs(f memory.FullEvent, agentName, id string) bool {
	if f.Metadata[tagentevent.MetaKeyAgentName] != agentName {
		return false
	}
	return f.Metadata[tagentevent.MetaKeyContinuationID] == id || f.Metadata[tagentevent.MetaKeyTaskRoundOwner] == id || f.Metadata["round_owner"] == id
}

func continuationTaskDeclaration(f memory.FullEvent, agentName, id string) (task.Declarative, bool) {
	var decl task.Declarative
	if f.EventType != tagentevent.TypeTaskSpawned || f.Metadata[tagentevent.MetaKeyAgentName] != agentName {
		return decl, false
	}
	if json.Unmarshal([]byte(f.Content), &decl) != nil {
		return decl, false
	}
	owner := decl.RoundOwner
	if owner == "" {
		owner = f.Metadata[tagentevent.MetaKeyTaskRoundOwner]
	}
	if owner != "" {
		return decl, owner == id
	}
	return decl, decl.Origin[metaKeyInvocationID] == id
}

func continuationEventFromFact(f memory.FullEvent) *AgentEvent {
	if f.EventType == tagentevent.TypeTaskSignal {
		var raw taskSignalRecord
		if json.Unmarshal([]byte(f.Content), &raw) == nil && raw.Event != nil {
			return raw.Event
		}
	}
	if f.EventType != tagentevent.TypeExternalInput {
		return nil
	}
	if snapshot, err := tagentevent.DecodeSourceSnapshot(f.Metadata[tagentevent.MetaKeySourceSnapshot]); err == nil && snapshot.Source != "" {
		return &AgentEvent{ID: f.Metadata[tagentevent.MetaKeySourceEventID], Type: tagentevent.TypeExternalInput, Source: snapshot.Source, Timestamp: time.UnixMilli(f.Timestamp), Message: &model.Message{Role: model.RoleUser, Content: f.Content, ContentParts: f.ContentParts}, Metadata: snapshot.Metadata}
	}
	return nil
}

func (ta *TagentAgent) foldContinuationRecovery(c *continuation, facts []memory.FullEvent) error {
	id := c.state.InvocationID
	refs := append([]memory.EventReference(nil), c.state.Projection...)
	seen := make(map[int64]bool, len(refs))
	for _, ref := range refs {
		seen[ref.EventKey] = true
	}
	consumed := make(map[int64]bool, len(c.state.ConsumedKeys))
	for _, key := range c.state.ConsumedKeys {
		consumed[key] = true
	}
	type rawSignal struct {
		fact memory.FullEvent
		raw  taskSignalRecord
	}
	var pendingSignals []rawSignal
	var progress bool
	var tailFinal bool
	var tailConsumed bool
	acceptedFacts := make(map[string]memory.FullEvent)
	spawnWaits := make(map[string]settleWaitSnapshot)
	for _, f := range facts {
		_, isSpawn := continuationTaskDeclaration(f, ta.name, id)
		belongs := continuationFactBelongs(f, ta.name, id)
		if isSpawn {
			progress = true
			if ta.taskManager != nil {
				if tk, ok := ta.taskManager.Get(f.Metadata["task_id"]); ok {
					state := tk.RoutingSnapshot()
					if state.ProducerOwner == id && state.Status.Live() && !state.WaitSatisfied {
						spawnWaits[tk.ID] = settleWaitSnapshot{TaskID: tk.ID, RoundID: state.ProducerRound, WaitFor: task.WaitForOf(state.Spec)}
					}
				}
			}
			if !containsContinuationKey(c.state.RetainedKeys, f.EventKey) {
				c.state.RetainedKeys = append(c.state.RetainedKeys, f.EventKey)
			}
		}
		if !belongs {
			continue
		}
		if f.EventType != tagentevent.TypeContinuationState && !containsContinuationKey(c.state.RetainedKeys, f.EventKey) {
			c.state.RetainedKeys = append(c.state.RetainedKeys, f.EventKey)
		}
		if f.EventType == tagentevent.TypeInputDisposition {
			key, _ := tagentevent.ParseEventKey(f.Metadata[tagentevent.MetaKeyConsumedEventKey])
			if f.Metadata[tagentevent.MetaKeyInputDisposition] == "inline_processed" {
				source, err := ta.memStore.GetEvent(key)
				var raw taskSignalRecord
				if err != nil || source == nil || !continuationFactBelongs(*source, ta.name, id) || json.Unmarshal([]byte(source.Content), &raw) != nil || !inlineTaskSignalConsumed(ta.memStore, *source, raw) {
					return fmt.Errorf("continuation %s inline disposition has no exact raw result", id)
				}
				continue
			}
			if key > 0 {
				source, err := ta.memStore.GetEvent(key)
				if err != nil || source == nil || source.PartitionID != f.PartitionID || !continuationFactBelongs(*source, ta.name, id) {
					return fmt.Errorf("continuation %s disposition has no exact source fact", id)
				}
				copySource := *source
				copySource.Metadata = make(map[string]string, len(source.Metadata)+1)
				for name, value := range source.Metadata {
					copySource.Metadata[name] = value
				}
				copySource.Metadata[tagentevent.MetaKeyObservationReceiptKey] = tagentevent.FormatEventKey(f.EventKey)
				if compress.VerifiedInputDisposition(ta.memStore, &copySource) == "" {
					return fmt.Errorf("continuation %s disposition is not verified", id)
				}
				consumed[key] = true
				if f.EventKey > c.stateKey && f.Metadata[tagentevent.MetaKeyInputDisposition] == "processed" {
					tailConsumed = true
				}
				sourceID := source.Metadata[tagentevent.MetaKeySourceEventID]
				c.state.InputDisposition = append(c.state.InputDisposition, continuationInputDisposition{EventID: sourceID, EventKey: key, ReceiptKey: f.EventKey, Disposition: f.Metadata[tagentevent.MetaKeyInputDisposition]})
				if c.state.InitialGoal != nil && sourceID == c.state.InitialGoal.ID {
					c.state.InitialPending = false
				}
			}
		}
		if f.EventType == tagentevent.TypeTaskSignal || f.EventType == tagentevent.TypeTaskRound || len(f.ToolCalls) > 0 || f.EventType == tagentevent.TypeActionCommand {
			progress = true
		}
		if f.EventType == tagentevent.TypeTaskSignal {
			var raw taskSignalRecord
			if json.Unmarshal([]byte(f.Content), &raw) == nil && raw.Event != nil {
				pendingSignals = append(pendingSignals, rawSignal{fact: f, raw: raw})
				if raw.ReceiptKey > 0 && !containsContinuationKey(c.state.RetainedKeys, raw.ReceiptKey) {
					c.state.RetainedKeys = append(c.state.RetainedKeys, raw.ReceiptKey)
				}
			}
		}
		if f.EventType == tagentevent.TypeExternalInput {
			if evt := continuationEventFromFact(f); evt != nil && evt.Source == SourceTask {
				c.acceptedInputs = append(c.acceptedInputs, evt)
				acceptedFacts[evt.ID] = f
			}
		}
		if f.EventKey <= c.stateKey {
			continue
		}
		if f.EventType == tagentevent.TypeAgentOutput {
			c.state.LastOutput = f.Content
			progress = true
			if f.Response != nil && f.Response.Error != nil {
				c.state.LastBatchOutcome, c.state.LastBatchError = "failed", f.Response.Error.Message
				tailFinal = false
			} else if f.Response == nil && f.Content != "" {
				tailFinal = true
			} else if f.Response != nil && f.Response.Done && !f.Response.IsPartial && len(f.ToolCalls) == 0 {
				tailFinal = true
			}
		}
		if f.EventType == tagentevent.TypeContextCompressSummary && f.Metadata[compress.CompactionMetaKey] == compress.CompactionGenV1 {
			payload, err := compress.UnmarshalPayload(f.Metadata[compress.CompactionPayloadMetaKey])
			if err != nil {
				return fmt.Errorf("continuation %s compaction: %w", id, err)
			}
			ordered, keys, indices := payload.RestoreRefs()
			for i, key := range keys {
				evt, err := ta.memStore.GetEvent(key)
				if err != nil || evt == nil {
					return fmt.Errorf("continuation %s retained key %d missing", id, key)
				}
				ordered[indices[i]] = continuationProjectionRef(*evt)
			}
			refs = ordered
			seen = make(map[int64]bool, len(refs))
			for _, ref := range refs {
				seen[ref.EventKey] = true
			}
			c.state.FullBoundary = payload.FullBoundary
			continue
		}
		if !tagentevent.IsNonProjectionRecord(f.EventType, f.Metadata) && !seen[f.EventKey] {
			refs = append(refs, continuationProjectionRef(f))
			seen[f.EventKey] = true
		}
	}
	c.state.Projection = refs
	for key := range consumed {
		if !containsContinuationKey(c.state.ConsumedKeys, key) {
			c.state.ConsumedKeys = append(c.state.ConsumedKeys, key)
		}
	}
	for _, wait := range c.state.Routing.Waits {
		delete(spawnWaits, wait.TaskID)
	}
	for _, wait := range spawnWaits {
		c.state.Routing.Waits = append(c.state.Routing.Waits, wait)
	}
	for _, evt := range c.acceptedInputs {
		key := deliveryKey(evt)
		if settleSatisfied(evt) {
			filtered := c.state.Routing.Waits[:0]
			for _, wait := range c.state.Routing.Waits {
				if wait.TaskID != key.taskID || wait.RoundID != key.roundID {
					filtered = append(filtered, wait)
				}
			}
			c.state.Routing.Waits = filtered
		}
	}
	replayed := make(map[string]bool)
	for _, pending := range pendingSignals {
		evt := pending.raw.Event
		if continuationInlineSignalCheckpointed(c.state, pending.fact, pending.raw) || inlineTaskSignalConsumed(ta.memStore, pending.fact, pending.raw) {
			c.acceptedInputs = append(c.acceptedInputs, evt)
			continue
		}
		fact, accepted := acceptedFacts[evt.ID]
		if !accepted || !consumed[fact.EventKey] {
			if accepted {
				copyFact := fact
				evt.volatileFact = &copyFact
				evt.volatileCommitted = true
			}
			c.replayInputs = append(c.replayInputs, evt)
			replayed[evt.ID] = true
		}
	}
	for eventID, fact := range acceptedFacts {
		if replayed[eventID] || consumed[fact.EventKey] {
			continue
		}
		evt := continuationEventFromFact(fact)
		copyFact := fact
		evt.volatileFact, evt.volatileCommitted = &copyFact, true
		c.replayInputs = append(c.replayInputs, evt)
	}
	if progress && c.state.InitialPending {
		c.recoveryGoalClosed = true
		c.state.InitialPending = false
	}
	ownedProducer := false
	for _, stream := range c.state.Routing.Streams {
		if stream.Owner != "" && !stream.Terminal {
			ownedProducer = true
			break
		}
	}
	if c.state.Outcome == "" && !c.state.InitialPending && len(c.state.Routing.Waits) == 0 && len(c.replayInputs) == 0 && len(c.state.InheritedProducers) == 0 && !ownedProducer {
		if c.state.LastBatchOutcome == "failed" {
			c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", c.state.LastBatchError
		} else if (c.state.LastBatchOutcome == "completed" && !tailConsumed) || (tailFinal && len(c.state.ConsumedKeys) > 0) {
			c.state.State, c.state.Outcome = "completed", "completed"
		} else if progress {
			c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", "interrupted action needs reconciliation; the original goal was not repeated"
		}
	}
	protocol := make([]memory.FullEvent, 0, len(c.state.Projection))
	for _, ref := range c.state.Projection {
		if ref.EventKey > 0 {
			fact, err := ta.memStore.GetEvent(ref.EventKey)
			if err != nil || fact == nil {
				return fmt.Errorf("continuation %s retained key %d missing", id, ref.EventKey)
			}
			protocol = append(protocol, *fact)
		}
	}
	return ta.closeInterruptedToolCalls(c, protocol)
}

func continuationInlineSignalCheckpointed(state continuationCheckpoint, fact memory.FullEvent, raw taskSignalRecord) bool {
	return raw.Inline && raw.Event != nil && raw.RoundOwner == state.InvocationID &&
		fact.EventKey > 0 && fact.EventKey <= state.LastBatchThroughKey &&
		(state.LastBatchOutcome == "completed" || state.LastBatchOutcome == "failed")
}

func (ta *TagentAgent) continuationInlineSignalConsumed(fact memory.FullEvent, raw taskSignalRecord) bool {
	if !raw.Inline || raw.RoundOwner == "" {
		return false
	}
	c, err := ta.continuationByID(raw.RoundOwner)
	if err != nil || c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return continuationInlineSignalCheckpointed(c.state, fact, raw) || inlineTaskSignalConsumed(ta.memStore, fact, raw)
}

func (ta *TagentAgent) releaseConsumedInlineSignalsLocked(c *continuation) {
	if ta.taskRecords == nil || c.state.LastBatchThroughKey <= 0 {
		return
	}
	facts, err := queryTaskRecords(ta.memStore, ta.partitionID(), []string{tagentevent.TypeTaskSignal})
	if err != nil {
		return
	}
	for _, fact := range facts {
		if !continuationFactBelongs(fact, ta.name, c.state.InvocationID) {
			continue
		}
		var raw taskSignalRecord
		if json.Unmarshal([]byte(fact.Content), &raw) != nil || !continuationInlineSignalCheckpointed(c.state, fact, raw) {
			continue
		}
		proof, err := ta.memStore.GetEvent(raw.ReceiptKey)
		if err == nil && proof != nil && taskSignalReceiptMatches(fact, raw, *proof) {
			ta.taskRecords.markSignalConsumed(fact.EventKey, raw.ReceiptKey)
		}
	}
}

func containsContinuationKey(keys []int64, key int64) bool {
	for _, candidate := range keys {
		if candidate == key {
			return true
		}
	}
	return false
}

func continuationProjectionRef(f memory.FullEvent) memory.EventReference {
	return memory.EventReference{EventKey: f.EventKey, PartitionID: f.PartitionID, EventType: f.EventType, EventSummary: f.EventSummary, Timestamp: f.Timestamp, Role: string(tagentevent.EventTypeRole(f.EventType))}
}

func (ta *TagentAgent) closeInterruptedToolCalls(c *continuation, tail []memory.FullEvent) error {
	open := make(map[string]model.ToolCall)
	for _, f := range tail {
		for _, call := range f.ToolCalls {
			if call.ID != "" {
				open[call.ID] = call
			}
		}
		if f.ToolID != "" {
			delete(open, f.ToolID)
		}
	}
	for id, call := range open {
		body := `{"status":"interrupted","error":"The process stopped before this tool result was committed. Reconcile existing resources; do not repeat the original action."}`
		key := memory.NewSnowflakeEventKey(ta.partitionID(), 0)
		fact := memory.FullEvent{EventKey: key, PartitionID: ta.partitionID(), EventType: tagentevent.TypeActionCommand, EventSummary: "interrupted " + call.Function.Name, Content: body, ToolID: id, Timestamp: time.Now().UnixMilli(), Metadata: map[string]string{tagentevent.MetaKeyAgentName: ta.name, tagentevent.MetaKeyContinuationID: c.state.InvocationID, "tool_name": call.Function.Name}}
		if err := ta.memStore.StoreEvent(key, fact); err != nil {
			return fmt.Errorf("close interrupted tool protocol: %w", err)
		}
		c.state.Projection = append(c.state.Projection, continuationProjectionRef(fact))
	}
	return nil
}

func (ta *TagentAgent) reconcileContinuationInboxLocked(c *continuation) error {
	if c.bus == nil || c.bus.inbox == nil {
		return nil
	}
	items, err := c.bus.inbox.Outstanding()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ReadErr != nil {
			return item.ReadErr
		}
		env := item.Env
		if len(env.Completion) == 0 {
			continue
		}
		completion, err := decodeCompletion(env.Completion)
		if err != nil {
			return err
		}
		if err := reconcileIdentityChecks(completion, env); err != nil {
			return err
		}
		cm := &ContextManager{memStore: ta.memStore}
		cred, err := cm.verifyReceiptCredential(env.Completion)
		if err != nil {
			return err
		}
		if err := c.bus.ConfirmDurable(item.Path, cred); err != nil {
			return err
		}
		if c.state.Outcome == "" || (c.state.Outcome == "failed" && strings.HasPrefix(c.state.Error, "interrupted action needs reconciliation")) {
			c.state.LastBatchOutcome = string(completion.BatchResult)
			c.state.LastBatchError = completion.ErrorSummary
			if len(c.state.Routing.Waits) == 0 && len(c.replayInputs) == 0 {
				c.state.State, c.state.Outcome, c.state.Error = string(completion.BatchResult), string(completion.BatchResult), completion.ErrorSummary
			}
		}
		for _, slot := range completion.Slots {
			key, _ := tagentevent.ParseEventKey(slot.FactKey)
			receiptKey, _ := tagentevent.ParseEventKey(completion.ReceiptKey)
			c.state.InputDisposition = append(c.state.InputDisposition, continuationInputDisposition{EventID: slot.SourceID, EventKey: key, ReceiptKey: receiptKey, Disposition: string(slot.Disposition)})
			if key > 0 && !containsContinuationKey(c.state.ConsumedKeys, key) {
				c.state.ConsumedKeys = append(c.state.ConsumedKeys, key)
			}
			if c.state.InitialGoal != nil && slot.SourceID == c.state.InitialGoal.ID {
				c.state.InitialPending = false
			}
		}
	}
	return nil
}

func (ta *TagentAgent) restoreInitialContinuationInputLocked(c *continuation) error {
	if c.state.InitialGoal == nil {
		return errors.New("continuation has no recoverable initial goal")
	}
	if c.bus.inbox != nil {
		items, err := c.bus.inbox.Outstanding()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ReadErr != nil {
				return item.ReadErr
			}
			for _, slot := range item.Env.Messages {
				evt, err := decodeSourceEvent(slot.SourceEvent)
				if err != nil {
					return err
				}
				if evt.ID == c.state.InitialGoal.ID {
					return nil
				}
			}
		}
	} else {
		queued, err := ta.volatileContinuationInputsLocked(c)
		if err != nil {
			return err
		}
		if queued[c.state.InitialGoal.ID] {
			return nil
		}
	}
	_, err := c.bus.PublishContext(context.Background(), c.state.InitialGoal)
	return err
}

func (ta *TagentAgent) repairConsumedContinuationInboxLocked(ctx context.Context, c *continuation) error {
	if c.bus == nil || c.bus.inbox == nil {
		return nil
	}
	cm := &ContextManager{name: ta.name, memStore: ta.memStore, bus: c.bus, partitionID: ta.partitionID(), continuationID: c.state.InvocationID, projection: compress.NewSessionProjection()}
	summary, err := ta.reconcileDurableBusForCM(ctx, cm)
	if err != nil {
		return err
	}
	if summary.Blocked > 0 || summary.Quarantined > 0 {
		return errors.New("continuation completion material is blocked")
	}
	items, err := c.bus.inbox.Outstanding()
	if err != nil {
		return err
	}
	var releases []string
	defer func() {
		for _, path := range releases {
			_ = c.bus.ReleaseClaim(path)
		}
	}()
	for range items {
		env, path, err := c.bus.inbox.ClaimNext()
		if err != nil {
			return err
		}
		if env == nil {
			break
		}
		var group []*AgentEvent
		known := true
		for _, slot := range env.Messages {
			evt, err := decodeSourceEvent(slot.SourceEvent)
			if err != nil {
				return err
			}
			evt.claim = &durableClaim{Path: path, RequestID: env.RequestID, Slot: slot.Slot, ReceiptKey: env.ReceiptKey, PreparedFact: slot.PreparedFact}
			var fact memory.FullEvent
			_ = json.Unmarshal(slot.PreparedFact, &fact)
			proven := false
			for _, input := range c.state.InputDisposition {
				if input.EventID == evt.ID || (fact.EventKey > 0 && input.EventKey == fact.EventKey) {
					proven = true
					if input.ReceiptKey > 0 {
						if receipt, err := ta.memStore.GetEvent(input.ReceiptKey); err == nil && receipt != nil && receipt.EventType == tagentevent.TypeInputDisposition {
							evt.observationReceiptKey = tagentevent.FormatEventKey(input.ReceiptKey)
						}
					}
					break
				}
			}
			if !proven && fact.EventKey > 0 {
				proven = containsContinuationKey(c.state.ConsumedKeys, fact.EventKey)
			}
			if !proven && c.recoveryGoalClosed && c.state.InitialGoal != nil && evt.ID == c.state.InitialGoal.ID {
				proven = true
			}
			if !proven {
				known = false
			}
			evt.recoveryConsumed = true
			group = append(group, evt)
		}
		if !known {
			releases = append(releases, path)
			continue
		}
		prepareTaskReceiptKeys(cm, group)
		if outcome := ta.submitDurableBatchWithBackoffForCM(ctx, cm, group, group); outcome.status != submitOK {
			return errors.New("recovered consumed input could not be committed")
		}
		if !consumeTaskInputs(ctx, cm, group, "recovered") {
			return errors.New("recovered consumed input disposition could not be committed")
		}
		if !ta.finishDurableBatchForCM(ctx, cm, group, group, completedOutcome()) {
			return errors.New("recovered consumed envelope completion could not be committed")
		}
		for _, evt := range group {
			fact, err := taskInputFact(evt)
			if err != nil {
				return err
			}
			receiptKey, _ := tagentevent.ParseEventKey(evt.observationReceiptKey)
			c.state.InputDisposition = append(c.state.InputDisposition, continuationInputDisposition{EventID: evt.ID, EventKey: fact.EventKey, ReceiptKey: receiptKey, Disposition: "recovered"})
			if !containsContinuationKey(c.state.ConsumedKeys, fact.EventKey) {
				c.state.ConsumedKeys = append(c.state.ConsumedKeys, fact.EventKey)
			}
			if c.state.InitialGoal != nil && evt.ID == c.state.InitialGoal.ID {
				c.state.InitialPending = false
			}
		}
	}
	return nil
}

func (ta *TagentAgent) restoreContinuationSignalsLocked(c *continuation) error {
	queued := make(map[string]bool)
	if len(c.replayInputs) == 0 {
		return nil
	}
	if c.bus.inbox != nil {
		items, err := c.bus.inbox.Outstanding()
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ReadErr != nil {
				return item.ReadErr
			}
			for _, slot := range item.Env.Messages {
				evt, err := decodeSourceEvent(slot.SourceEvent)
				if err != nil {
					return err
				}
				queued[evt.ID] = true
			}
		}
	} else {
		var err error
		queued, err = ta.volatileContinuationInputsLocked(c)
		if err != nil {
			return err
		}
	}
	for _, evt := range c.replayInputs {
		if queued[evt.ID] {
			c.acceptedInputs = append(c.acceptedInputs, evt)
			continue
		}
		if _, err := c.bus.PublishContext(context.Background(), evt); err != nil {
			return err
		}
		queued[evt.ID] = true
		c.acceptedInputs = append(c.acceptedInputs, evt)
	}
	c.replayInputs = nil
	return nil
}

func (ta *TagentAgent) volatileContinuationInputsLocked(c *continuation) (map[string]bool, error) {
	release, err := ta.settleSinks.pauseDestination(context.Background(), c.state.InvocationID)
	if err != nil {
		return nil, err
	}
	defer release(nil)
	queued := make(map[string]bool)
	batch := c.bus.TryPull()
	for _, evt := range batch {
		queued[evt.ID] = true
		if _, err := c.bus.PublishContext(context.Background(), evt); err != nil {
			return nil, err
		}
	}
	return queued, nil
}

func (ta *TagentAgent) continuationDriverFailureLocked(c *continuation) string {
	inputs := append(append([]*AgentEvent(nil), c.acceptedInputs...), c.replayInputs...)
	for _, wait := range c.state.Routing.Waits {
		hasResult := false
		for _, evt := range inputs {
			key := deliveryKey(evt)
			if key.taskID == wait.TaskID && key.roundID == wait.RoundID && settleSatisfied(evt) {
				hasResult = true
				break
			}
		}
		if hasResult {
			continue
		}
		if ta.taskManager != nil {
			if tk, ok := ta.taskManager.Get(wait.TaskID); ok && tk.ProducerRound() == wait.RoundID && tk.HasProducer() {
				continue
			}
		}
		return fmt.Sprintf("original task %s round %d has no reattached producer or committed result; reconciliation is required", wait.TaskID, wait.RoundID)
	}
	for _, stream := range c.state.Routing.Streams {
		if stream.Owner == "" || stream.Terminal {
			continue
		}
		closed := false
		for _, evt := range inputs {
			key := deliveryKey(evt)
			if key.taskID == stream.TaskID && key.roundID == stream.RoundID && settleTerminal(evt) {
				closed = true
				break
			}
		}
		if closed {
			continue
		}
		if ta.taskManager != nil {
			if tk, ok := ta.taskManager.Get(stream.TaskID); ok && tk.ProducerRound() == stream.RoundID && tk.HasProducer() {
				continue
			}
		}
		return fmt.Sprintf("owned producer %s round %d has no reattached detector or terminal result; reconciliation is required", stream.TaskID, stream.RoundID)
	}
	return ""
}

// StartRecoveredContinuations starts parentless scopes after task detectors have reattached.
func (ta *TagentAgent) StartRecoveredContinuations(ctx context.Context) error {
	if ta == nil {
		return nil
	}
	r := ta.continuationRegistry()
	var entries []*continuation
	for _, c := range r.snapshot() {
		c.mu.Lock()
		if c.recovered && c.state.Address.ParentTaskID == "" && c.state.Outcome == "" {
			entries = append(entries, c)
		}
		c.mu.Unlock()
	}
	for _, c := range entries {
		lease := ta.contextManager.AcquireLease(LeaseSubCall)
		if err := lease.Err(); err != nil {
			lease.Release()
			return err
		}
		ch, err := ta.startContinuation(ctx, c, lease, "", "")
		lease.Release()
		if err != nil {
			return err
		}
		go func() {
			for range ch {
			}
		}()
	}
	return nil
}

// ResumeContinuationByID verifies the durable child identity before attaching to its original round.
func (ta *TagentAgent) ResumeContinuationByID(ctx context.Context, childInvocationID, parentTaskID string, round uint64) (<-chan *event.Event, bool, error) {
	if ta == nil {
		return nil, false, nil
	}
	c, err := ta.continuationByID(childInvocationID)
	if err != nil {
		return nil, false, err
	}
	if c == nil {
		return nil, false, nil
	}
	c.mu.Lock()
	valid := c.state.Address.ParentTaskID == parentTaskID && c.state.Address.ParentTaskRound == round
	c.mu.Unlock()
	if !valid {
		return nil, true, errors.New("continuation result address differs from the original task round")
	}
	r := ta.continuationRegistry()
	closing := r.closing.Load()
	if closing {
		return nil, true, ErrExecClosed
	}
	lease := ta.contextManager.AcquireLease(LeaseSubCall)
	defer lease.Release()
	if err := lease.Err(); err != nil {
		return nil, true, err
	}
	ch, err := ta.startContinuation(ctx, c, lease, "", "")
	ta.trimAcknowledgedContinuations()
	return ch, true, err
}

func (ta *TagentAgent) continuationParent(address ContinuationContext) *TagentAgent {
	if address.ParentAgent == ta.name {
		return ta
	}
	if ta.resident != nil {
		if parent := ta.resident.Get(address.ParentAgent); parent != nil {
			return parent
		}
	}
	return ta.resolveContinuationOwner(address.ParentAgent)
}

func (ta *TagentAgent) resolveContinuationReceiver(address ContinuationContext, visited map[string]bool) (settleAcceptor, error) {
	parent := ta.continuationParent(address)
	if parent == nil {
		return nil, fmt.Errorf("continuation parent %q is unavailable", address.ParentAgent)
	}
	key := parent.name + "/" + address.ParentInvocationID
	if visited[key] {
		return nil, errors.New("continuation handoff chain is cyclic")
	}
	visited[key] = true
	if address.ParentInvocationID == "" {
		if !parent.IsLoopActive() || parent.persistentBus == nil {
			return nil, fmt.Errorf("continuation entry receiver %q is not active", parent.name)
		}
		return parent.persistentBus.PublishContext, nil
	}
	c, err := parent.continuationByID(address.ParentInvocationID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("parent continuation %s is unavailable", address.ParentInvocationID)
	}
	c.mu.Lock()
	running, bus, handoff := c.running, c.bus, c.state.Handoff
	c.mu.Unlock()
	if running && bus != nil {
		return bus.PublishContext, nil
	}
	if handoff != nil {
		return parent.resolveContinuationReceiver(*handoff, visited)
	}
	return nil, fmt.Errorf("parent continuation %s has no active receiver", address.ParentInvocationID)
}

func (ta *TagentAgent) continuationHandoffAcceptor(address ContinuationContext) settleAcceptor {
	return func(ctx context.Context, evt *AgentEvent) (PublishReceipt, error) {
		dst, err := ta.resolveContinuationReceiver(address, make(map[string]bool))
		if err != nil {
			return PublishReceipt{}, err
		}
		return dst(ctx, evt)
	}
}

var errContinuationEventsPending = errors.New("continuation received events while sealing")

func (ta *TagentAgent) commitFailedContinuation(ctx context.Context, c *continuation, reason string) error {
	id := c.state.InvocationID
	release, err := ta.settleSinks.pauseDestination(ctx, id)
	if err != nil {
		return err
	}
	if c.bus.PendingCount() > 0 {
		release(nil)
		return errContinuationEventsPending
	}
	c.mu.Lock()
	oldState, oldOutcome, oldError := c.state.State, c.state.Outcome, c.state.Error
	c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", reason
	c.state.Routing = ta.settleSinks.snapshotRouting(id)
	err = ta.persistContinuationLocked(c)
	if err != nil {
		c.state.State, c.state.Outcome, c.state.Error = oldState, oldOutcome, oldError
	}
	c.mu.Unlock()
	if err != nil {
		release(nil)
		return err
	}
	release(func(deliveryCtx context.Context, evt *AgentEvent) (PublishReceipt, error) {
		return ta.acceptCancelledContinuationSignal(deliveryCtx, c, evt)
	})
	ta.settleSinks.retireStateOnly(id)
	return nil
}

func (ta *TagentAgent) handoffContinuationServices(ctx context.Context, c *continuation) error {
	c.mu.Lock()
	id := c.state.InvocationID
	address := c.state.Address
	inherited := append([]continuationProducerReference(nil), c.state.InheritedProducers...)
	c.mu.Unlock()
	release, err := ta.settleSinks.pauseDestination(ctx, id)
	if err != nil {
		return err
	}
	if c.bus.PendingCount() > 0 {
		release(nil)
		return errContinuationEventsPending
	}
	routing := ta.settleSinks.snapshotRouting(id)
	live := false
	var producers []continuationProducerReference
	for _, stream := range routing.Streams {
		if stream.Owner != "" {
			live = true
			producers = append(producers, continuationProducerReference{Agent: ta.name, InvocationID: id, TaskID: stream.TaskID, RoundID: stream.RoundID})
		}
	}
	var retained []continuationProducerReference
	for _, reference := range inherited {
		if ta.continuationProducerLive(reference) {
			producers = append(producers, reference)
			retained = append(retained, reference)
			live = true
		}
	}
	if !live {
		release(nil)
		return nil
	}
	if address.ParentAgent == "" {
		address = ContinuationContext{ParentAgent: ta.name}
	}
	if _, err := ta.resolveContinuationReceiver(address, make(map[string]bool)); err != nil {
		release(nil)
		return err
	}
	if err := ta.registerInheritedProducers(address, producers, false, make(map[string]bool)); err != nil {
		release(nil)
		return err
	}
	c.mu.Lock()
	c.state.Handoff = &address
	c.state.Routing = routing
	c.state.InheritedProducers = retained
	err = ta.persistContinuationLocked(c)
	c.mu.Unlock()
	if err != nil {
		release(nil)
		return err
	}
	release(ta.continuationHandoffAcceptor(address))
	return nil
}

func (ta *TagentAgent) continuationProducerLive(reference continuationProducerReference) bool {
	source := ta.resolveContinuationOwner(reference.Agent)
	if source == nil || source.taskManager == nil {
		return false
	}
	tk, ok := source.taskManager.Get(reference.TaskID)
	if !ok {
		return false
	}
	state := tk.RoutingSnapshot()
	if state.ProducerRound == reference.RoundID && state.Status.Live() {
		return true
	}
	for _, pending := range state.PendingSignals {
		if pending.RoundID == reference.RoundID {
			return true
		}
	}
	return false
}

func (ta *TagentAgent) registerInheritedProducers(address ContinuationContext, references []continuationProducerReference, allowSuspended bool, visited map[string]bool) error {
	parent := ta.continuationParent(address)
	if parent == nil {
		return fmt.Errorf("handoff parent %q is unavailable", address.ParentAgent)
	}
	if address.ParentInvocationID == "" {
		return nil
	}
	key := parent.name + "/" + address.ParentInvocationID
	if visited[key] {
		return errors.New("inherited producer ownership is cyclic")
	}
	visited[key] = true
	c, err := parent.continuationByID(address.ParentInvocationID)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("inherited producer parent scope is missing")
	}
	c.mu.Lock()
	if c.running || (allowSuspended && c.state.Outcome == "") {
		seen := make(map[continuationProducerReference]bool, len(c.state.InheritedProducers))
		for _, reference := range c.state.InheritedProducers {
			seen[reference] = true
		}
		for _, reference := range references {
			if !seen[reference] {
				c.state.InheritedProducers = append(c.state.InheritedProducers, reference)
				seen[reference] = true
			}
		}
		err := parent.persistContinuationLocked(c)
		c.mu.Unlock()
		return err
	}
	next := c.state.Handoff
	c.mu.Unlock()
	if next != nil {
		return parent.registerInheritedProducers(*next, references, allowSuspended, visited)
	}
	return errors.New("inherited producer has no parent receiver")
}

// RestoreServiceHandoffs reinstalls committed destinations after parent receivers exist.
func (ta *TagentAgent) RestoreServiceHandoffs(ctx context.Context) error {
	if ta == nil {
		return nil
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	var entries []*continuation
	for _, c := range r.byID {
		entries = append(entries, c)
	}
	r.mu.Unlock()
	for _, c := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		handoff, id, routing, outcome := c.state.Handoff, c.state.InvocationID, c.state.Routing, c.state.Outcome
		c.mu.Unlock()
		if outcome == "cancelled" || outcome == "failed" {
			ta.settleSinks.restoreRouting(id, routing, ta.taskManager)
			ta.settleSinks.setDestination(id, func(deliveryCtx context.Context, evt *AgentEvent) (PublishReceipt, error) {
				return ta.acceptCancelledContinuationSignal(deliveryCtx, c, evt)
			})
			ta.settleSinks.retireStateOnly(id)
			if err := ta.drainCancelledContinuationInputs(ctx, c); err != nil {
				return err
			}
			continue
		}
		if handoff == nil {
			continue
		}
		var references []continuationProducerReference
		for _, stream := range routing.Streams {
			if stream.Owner != "" {
				references = append(references, continuationProducerReference{Agent: ta.name, InvocationID: id, TaskID: stream.TaskID, RoundID: stream.RoundID})
			}
		}
		if len(references) > 0 {
			if err := ta.registerInheritedProducers(*handoff, references, true, make(map[string]bool)); err != nil {
				return err
			}
		}
		ta.settleSinks.restoreRouting(id, routing, ta.taskManager)
		ta.settleSinks.setDestination(id, ta.continuationHandoffAcceptor(*handoff))
		ta.settleSinks.retire(id)
	}
	return nil
}

// AcknowledgeContinuationResult releases retained material only after an exact parent receipt.
func (ta *TagentAgent) AcknowledgeContinuationResult(invocationID string) error {
	if ta == nil {
		return nil
	}
	c, err := ta.continuationByID(invocationID)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("continuation result is unknown")
	}
	c.mu.Lock()
	acked, address := c.state.ResultAcked, c.state.Address
	c.mu.Unlock()
	if acked {
		return nil
	}
	parent := ta.continuationParent(address)
	if parent == nil {
		return errors.New("continuation parent receipt store is unavailable")
	}
	facts, err := continuationFacts(parent.memStore, parent.partitionID())
	if err != nil {
		return err
	}
	for _, fact := range facts {
		if fact.EventType != tagentevent.TypeTaskSignalReceipt || fact.Metadata[tagentevent.MetaKeyAgentName] != parent.name {
			continue
		}
		round, _ := strconv.ParseUint(fact.Metadata[tagentevent.MetaKeyTaskRoundID], 10, 64)
		if fact.Metadata["task_id"] == address.ParentTaskID && round == address.ParentTaskRound && fact.Metadata[childInvocationIDParam] == invocationID {
			return ta.AcknowledgeContinuationResultWithReceipt(context.Background(), invocationID, fact)
		}
	}
	return errors.New("exact parent task signal receipt is not committed")
}

// AcknowledgeContinuationResultWithReceipt accepts the parent writer's exact committed receipt.
func (ta *TagentAgent) AcknowledgeContinuationResultWithReceipt(ctx context.Context, invocationID string, proof memory.FullEvent) error {
	if ta == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := ta.continuationByID(invocationID)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("continuation result is unknown")
	}
	c.mu.Lock()
	defer func() {
		c.mu.Unlock()
		ta.trimAcknowledgedContinuations()
	}()
	if c.state.ResultAcked {
		return nil
	}
	if c.state.Outcome == "" {
		return errors.New("continuation has no committed logical outcome")
	}
	if err := ta.verifyContinuationParentProof(invocationID, c.state.Address, proof, false); err != nil {
		return err
	}
	c.state.ResultAcked = true
	if err := ta.persistContinuationLocked(c); err != nil {
		c.state.ResultAcked = false
		return err
	}
	if guard, ok := ta.memStore.(memory.RetentionGuard); ok {
		for key := range c.protected {
			guard.ReleaseKey(key)
		}
	}
	c.protected = nil
	return nil
}

func (ta *TagentAgent) verifyContinuationParentProof(invocationID string, address ContinuationContext, proof memory.FullEvent, cancelled bool) error {
	if proof.EventKey <= 0 || proof.EventType != tagentevent.TypeTaskSignalReceipt {
		return errors.New("continuation parent proof is not a task signal receipt")
	}
	round, err := strconv.ParseUint(proof.Metadata[tagentevent.MetaKeyTaskRoundID], 10, 64)
	if err != nil || round != address.ParentTaskRound || proof.Metadata["task_id"] != address.ParentTaskID || proof.Metadata[childInvocationIDParam] != invocationID || proof.Metadata[tagentevent.MetaKeyAgentName] != address.ParentAgent {
		return errors.New("continuation parent receipt has a different task, round, child or agent")
	}
	if address.ParentInvocationID != "" && proof.Metadata[tagentevent.MetaKeyTaskRoundOwner] != address.ParentInvocationID {
		return errors.New("continuation parent receipt has a different invocation owner")
	}
	rawKey, err := tagentevent.ParseEventKey(proof.Content)
	if err != nil || rawKey <= 0 || tagentevent.FormatEventKey(rawKey) != proof.Content {
		return errors.New("continuation parent receipt has no exact raw signal key")
	}
	parent := ta.continuationParent(address)
	var raw *memory.FullEvent
	if parent != nil && parent.memStore != nil {
		stored, err := parent.memStore.GetEvent(proof.EventKey)
		if err != nil || stored == nil || !sameReceiptFact(*stored, proof) {
			return errors.New("continuation parent receipt is not committed verbatim")
		}
		raw, err = parent.memStore.GetEvent(rawKey)
		if err != nil || raw == nil {
			return errors.New("continuation parent raw signal is missing")
		}
	} else if ta.memStore != nil {
		candidate, _ := ta.memStore.GetEvent(rawKey)
		raw = candidate
	}
	if raw != nil {
		if raw.EventType != tagentevent.TypeTaskSignal || raw.PartitionID != proof.PartitionID {
			return errors.New("continuation raw signal type or partition differs from receipt")
		}
		var record taskSignalRecord
		if err := json.Unmarshal([]byte(raw.Content), &record); err != nil {
			return err
		}
		if record.TaskID != address.ParentTaskID || record.RoundID != round || record.ChildInvocationID != invocationID || record.ReceiptKey != proof.EventKey || record.Event == nil || record.Event.ID != proof.Metadata[tagentevent.MetaKeySourceEventID] {
			return errors.New("continuation receipt does not prove this raw child result")
		}
		if cancelled {
			if record.Lineage != task.LineageRetired && record.ProducerStatus != task.TaskCancelled {
				return errors.New("parent signal is not an explicit cancellation or retirement")
			}
		} else if (record.Kind != task.SettleCompleted && record.Kind != task.SettleFailed) || record.ErrorKind == "continuation_interrupted" {
			return errors.New("parent signal has no terminal child outcome")
		}
		return nil
	}
	kind := task.SettleKind(proof.Metadata["signal_kind"])
	if proof.Metadata[tagentevent.MetaKeySourceEventID] == "" || kind == "" {
		return errors.New("trusted parent receipt lacks the raw signal kind")
	}
	if cancelled {
		if proof.Metadata["signal_lineage"] != task.LineageRetired && proof.Metadata["producer_status"] != string(task.TaskCancelled) {
			return errors.New("trusted parent receipt is not cancellation or retirement")
		}
	} else if kind != task.SettleCompleted && kind != task.SettleFailed {
		return errors.New("trusted parent receipt is not a terminal child result")
	}
	return nil
}

// CancelContinuationByParent terminates a scope only when its parent cancellation is durable.
func (ta *TagentAgent) CancelContinuationByParent(ctx context.Context, invocationID string, proof memory.FullEvent) error {
	if ta == nil {
		return nil
	}
	c, err := ta.continuationByID(invocationID)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("cancelled continuation is unknown")
	}
	c.mu.Lock()
	address := c.state.Address
	c.mu.Unlock()
	if err := ta.verifyContinuationParentProof(invocationID, address, proof, true); err != nil {
		return err
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	done, running := c.done, c.running
	c.mu.Unlock()
	if running {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	release, err := ta.settleSinks.pauseDestination(ctx, invocationID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if err := ta.openContinuationBus(c); err != nil {
		c.mu.Unlock()
		release(nil)
		return err
	}
	c.state.State, c.state.Outcome, c.state.Error = "cancelled", "cancelled", proof.Content
	c.state.InitialPending = false
	oldAcked := c.state.ResultAcked
	c.state.ResultAcked = true
	if err := ta.persistContinuationLocked(c); err != nil {
		c.state.ResultAcked = oldAcked
		c.mu.Unlock()
		release(nil)
		return err
	}
	if guard, ok := ta.memStore.(memory.RetentionGuard); ok {
		for key := range c.protected {
			guard.ReleaseKey(key)
		}
	}
	c.protected = nil
	c.mu.Unlock()
	release(func(deliveryCtx context.Context, evt *AgentEvent) (PublishReceipt, error) {
		return ta.acceptCancelledContinuationSignal(deliveryCtx, c, evt)
	})
	if ta.taskManager != nil {
		for _, tk := range ta.taskManager.TasksSnapshot() {
			if tk.RoutingSnapshot().ProducerOwner == invocationID {
				ta.taskManager.Cancel(tk.ID)
			}
		}
	}
	if err := ta.drainCancelledContinuationInputs(ctx, c); err != nil {
		return err
	}
	ta.settleSinks.retireStateOnly(c.state.InvocationID)
	return nil
}

func (ta *TagentAgent) acceptCancelledContinuationSignal(ctx context.Context, c *continuation, evt *AgentEvent) (PublishReceipt, error) {
	if err := ctx.Err(); err != nil {
		return PublishReceipt{}, err
	}
	if evt == nil {
		return PublishReceipt{}, ErrNilEvent
	}
	c.stateOnlyMu.Lock()
	defer c.stateOnlyMu.Unlock()
	c.mu.Lock()
	bus := c.bus
	c.mu.Unlock()
	if bus == nil {
		return PublishReceipt{}, ErrBusClosed
	}
	receipt, err := bus.PublishContext(ctx, evt)
	if err != nil {
		return receipt, err
	}
	return receipt, ta.drainCancelledContinuationInputsLocked(ctx, c)
}

func (ta *TagentAgent) drainCancelledContinuationInputs(ctx context.Context, c *continuation) error {
	c.stateOnlyMu.Lock()
	defer c.stateOnlyMu.Unlock()
	return ta.drainCancelledContinuationInputsLocked(ctx, c)
}

func (ta *TagentAgent) drainCancelledContinuationInputsLocked(ctx context.Context, c *continuation) error {
	c.mu.Lock()
	bus, id, refs := c.bus, c.state.InvocationID, append([]memory.EventReference(nil), c.state.Projection...)
	c.mu.Unlock()
	if bus == nil {
		return ErrBusClosed
	}
	projection := compress.NewSessionProjection()
	projection.Replace(refs)
	cm := &ContextManager{name: ta.name, memStore: ta.memStore, bus: bus, partitionID: ta.partitionID(), continuationID: id, projection: projection}
	for {
		batch := bus.TryPull()
		if len(batch) == 0 {
			break
		}
		for _, input := range batch {
			input.recoveryConsumed = true
		}
		if ta.processTurn(ctx, cm, batch) == turnStop {
			return errors.New("cancelled continuation disposition was not committed")
		}
	}
	return nil
}
