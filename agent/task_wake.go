// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

func turnInvocationID(ctx context.Context, cm *ContextManager, events []*AgentEvent) string {
	if cm != nil && cm.continuationID != "" {
		return cm.continuationID
	}
	if id, ok := invocationIDFromContext(ctx); ok {
		return id
	}
	return extractDelegationInvocationID(events)
}

func isTaskObservation(evt *AgentEvent) bool {
	return evt != nil && evt.Source == SourceTask && evt.Type == tagentevent.TypeExternalInput && evt.Metadata[tagentevent.MetaKeySettleWake] == "observe"
}

func observationInputs(events []*AgentEvent) []*AgentEvent {
	var observations []*AgentEvent
	for _, evt := range events {
		if isTaskObservation(evt) || (evt != nil && evt.recoveryConsumed) {
			observations = append(observations, evt)
		}
	}
	return observations
}

func decisionInputs(events []*AgentEvent) []*AgentEvent {
	var decisions []*AgentEvent
	for _, evt := range events {
		if evt != nil && !isTaskObservation(evt) && !evt.recoveryConsumed {
			decisions = append(decisions, evt)
		}
	}
	return decisions
}

func taskInputFact(evt *AgentEvent) (*memory.FullEvent, error) {
	if evt == nil {
		return nil, fmt.Errorf("missing task input")
	}
	if evt.claim != nil {
		var fact memory.FullEvent
		if err := json.Unmarshal(evt.claim.PreparedFact, &fact); err != nil {
			return nil, fmt.Errorf("invalid frozen task input: %w", err)
		}
		if !isCompletePreparedFact(&fact) {
			return nil, fmt.Errorf("incomplete frozen task input")
		}
		return &fact, nil
	}
	if evt.volatileFact == nil || !evt.volatileCommitted {
		return nil, fmt.Errorf("task input is not committed")
	}
	return evt.volatileFact, nil
}

func prepareTaskReceiptKeys(cm *ContextManager, events []*AgentEvent) {
	for _, evt := range events {
		if evt == nil || (evt.Source != SourceTask && !evt.recoveryConsumed && cm.continuationID == "") || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.claim != nil && len(evt.claim.PreparedFact) > 0 {
			var fact memory.FullEvent
			if json.Unmarshal(evt.claim.PreparedFact, &fact) == nil {
				if key := fact.Metadata[tagentevent.MetaKeyObservationReceiptKey]; key != "" {
					evt.observationReceiptKey = key
				}
			}
			if (evt.recoveryConsumed || (cm.continuationID != "" && evt.Source != SourceTask)) && evt.observationReceiptKey == "" {
				evt.observationReceiptKey = tagentevent.FormatEventKey(memory.NewSnowflakeEventKey(cm.partitionID, 0))
			}
			continue
		}
		if evt.volatileFact != nil {
			if key := evt.volatileFact.Metadata[tagentevent.MetaKeyObservationReceiptKey]; key != "" {
				evt.observationReceiptKey = key
			}
			if (evt.recoveryConsumed || (cm.continuationID != "" && evt.Source != SourceTask)) && evt.observationReceiptKey == "" {
				evt.observationReceiptKey = tagentevent.FormatEventKey(memory.NewSnowflakeEventKey(cm.partitionID, 0))
			}
			continue
		}
		if evt.observationReceiptKey == "" {
			evt.observationReceiptKey = tagentevent.FormatEventKey(memory.NewSnowflakeEventKey(cm.partitionID, 0))
		}
	}
}

func batchNeedsVolatileFacts(cm *ContextManager, events []*AgentEvent) bool {
	if !hasVolatileInput(events) {
		return false
	}
	if cm != nil && cm.continuationID != "" {
		return true
	}
	for _, evt := range events {
		if evt != nil && (evt.Source == SourceTask || evt.recoveryConsumed || evt.claim != nil) {
			return true
		}
	}
	return false
}

func hasVolatileInput(events []*AgentEvent) bool {
	for _, evt := range events {
		if evt != nil && evt.claim == nil && evt.Type == tagentevent.TypeExternalInput && evt.Message != nil {
			return true
		}
	}
	return false
}

func prepareVolatileTaskInputs(cm *ContextManager, events []*AgentEvent) {
	if !batchNeedsVolatileFacts(cm, events) {
		return
	}
	for _, evt := range events {
		if evt == nil || evt.claim != nil || evt.Type != tagentevent.TypeExternalInput || evt.Message == nil || evt.volatileFact != nil {
			continue
		}
		fact := cm.buildBusFact(evt)
		evt.volatileFact = &fact
	}
}

func waitTaskInputRetry(ctx context.Context) bool {
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func consumeTaskInputs(ctx context.Context, cm *ContextManager, events []*AgentEvent, disposition string) bool {
	for _, evt := range events {
		if evt == nil || (evt.Source != SourceTask && !evt.recoveryConsumed && cm.continuationID == "") || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		fact, err := taskInputFact(evt)
		if err != nil {
			log.Errorf("[task-input] consumption refused: %v", err)
			return false
		}
		raw := fact.Metadata[tagentevent.MetaKeyObservationReceiptKey]
		kind := disposition
		if evt.recoveryConsumed {
			kind = "recovered"
		}
		if raw == "" && (evt.recoveryConsumed || (cm.continuationID != "" && evt.Source != SourceTask)) {
			raw = evt.observationReceiptKey
			copyFact := *fact
			copyFact.Metadata = make(map[string]string, len(fact.Metadata)+1)
			for k, v := range fact.Metadata {
				copyFact.Metadata[k] = v
			}
			copyFact.Metadata[tagentevent.MetaKeyObservationReceiptKey] = raw
			fact = &copyFact
		}
		if evt.recoveryConsumed && compress.VerifiedInputDisposition(cm.memStore, fact) != "" {
			continue
		}

		if raw == "" && fact.Metadata[tagentevent.MetaKeySettleWake] == "" && evt.observationReceiptKey == "" {
			continue
		}
		key, err := tagentevent.ParseEventKey(raw)
		if err != nil || key <= 0 || tagentevent.FormatEventKey(key) != raw {
			log.Errorf("[task-input] consumption refused: invalid reserved receipt for %d", fact.EventKey)
			return false
		}
		if _, ok := cm.memStore.(memory.EventReplayer); !ok {
			log.Errorf("[task-input] consumption requires a replay-capable fact store")
			return false
		}
		receipt := memory.FullEvent{
			EventKey: key, PartitionID: fact.PartitionID,
			EventType:    tagentevent.TypeInputDisposition,
			EventSummary: kind + " input", Timestamp: fact.Timestamp,
			Content: kind + " " + tagentevent.FormatEventKey(fact.EventKey),
			Metadata: map[string]string{
				tagentevent.MetaKeyAgentName:        fact.Metadata[tagentevent.MetaKeyAgentName],
				tagentevent.MetaKeyConsumedEventKey: tagentevent.FormatEventKey(fact.EventKey),
				tagentevent.MetaKeyInputDisposition: kind,
			},
		}
		if cm.continuationID != "" {
			receipt.Metadata[tagentevent.MetaKeyContinuationID] = cm.continuationID
		}
		for {
			if ctx.Err() != nil {
				return false
			}
			if err := cm.commitReceiptFact(receipt); err != nil {
				if memory.IsDuplicateEventKey(err) || memory.IsEventForgotten(err) || cm.memStore == nil {
					log.Errorf("[task-input] consumption receipt refused: %v", err)
					return false
				}
				log.Warnf("[task-input] receipt write failed — retaining the current batch: %v", err)
			} else if compress.VerifiedInputDisposition(cm.memStore, fact) == kind {
				break
			} else {
				log.Errorf("[task-input] consumption receipt did not verify source key and partition")
				return false
			}
			if !waitTaskInputRetry(ctx) {
				return false
			}
		}
	}
	return true
}

func foldConsumedTaskObservations(ctx context.Context, cm *ContextManager) {
	if cm.contextCompressor != nil && cm.projection != nil {
		cm.projection.Replace(cm.contextCompressor.FoldConsumedObservations(ctx, cm.projection.GetAll()))
	}
}
