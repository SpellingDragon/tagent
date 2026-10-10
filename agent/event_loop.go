// 契约: docs/wiki/agent/event-flow.md#event-stream-overview
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// durableCommittedKeys runEventLoop runs the persistent event loop for this agent.
// It pulls events from the EventBus, builds an invocation, and runs the flow.
// The loop runs in a dedicated goroutine and exits when ctx is cancelled.
// durableCommittedKeys collects the distinct fact EventKeys already committed for this
// batch: each durable claim carries its frozen FullEvent JSON (PreparedFact); the
// keys seed the echo credential consumed by . Volatile events (no claim) contribute
// nothing.
func durableCommittedKeys(events []*AgentEvent) []int64 {
	seen := make(map[int64]bool)
	var keys []int64
	for _, ev := range events {
		if ev == nil || ev.claim == nil || len(ev.claim.PreparedFact) == 0 {
			continue
		}
		var f memory.FullEvent
		if err := json.Unmarshal(ev.claim.PreparedFact, &f); err != nil || f.EventKey == 0 {
			continue
		}
		if !seen[f.EventKey] {
			seen[f.EventKey] = true
			keys = append(keys, f.EventKey)
		}
	}
	return keys
}

// turnDisposition 告诉持久循环一批之后做什么：继续拉取，还是完全停止自动消费。
// 后者的判据是在函数体内直接提前返回的那几类情形——取消、确定性提交冲突、
// 或执行凭据未经验证。
type turnDisposition int

const (
	turnContinue turnDisposition = iota
	turnStop
)

// persistentTurnRetryBudget 是每个回合的模型重试预算，两种同形路径取同一值：顶层事件
// 循环与派生的子 agent 调用跑同一个回合、共用同一份自愈预算。同步子调用拿到的预算与
// 自身相同——其调用方无论如何都阻塞在第一个答复上（两阶段 ACK→补最终协议不变），而让
// 被调方自愈一次瞬时抖动（约 ≤700ms）远比调用方重发整次委托便宜。
const persistentTurnRetryBudget = 3

// loopSpec parameterizes the ONE shared consume shell for the two isomorphic forms
// . The entry owner binds nothing (empty invocationID): it consumes
// persistentBus until ctx is done or processTurn stops. A sub-agent invocation loop
// binds its own invocation id and additionally quiesces when its越窗 delivery-
// accounting barrier drains — every booked spawn delivered AND the bus empty — which
// is what closes the request/response channel after the last continuation.
type loopSpec struct {
	invocationID string
}

// runAgentLoop is the shared consume shell extracted from runEventLoop: Pull a batch
// → processTurn (the single per-turn primitive) → repeat. Both the persistent entry
// loop and a per-invocation越窗 tail drive THIS body, so "直连宿主" and "作为被调
// 方" share one transport (their own bus), one consume loop, and one retry budget —
// the only remaining difference is the output receiver. It exits on ctx done, a
// turnStop from processTurn, or (invocation form) delivery quiescence with an
// drained bus.
func (ta *TagentAgent) runAgentLoop(ctx context.Context, bus *EventBus, cm *ContextManager, spec loopSpec) {
	for {
		if err := ctx.Err(); err != nil {
			log.Infof("[runAgentLoop:%s] ctx cancelled, exiting: %v", ta.name, err)
			return
		}
		if spec.invocationID != "" && !ta.settleSinks.awaiting(spec.invocationID) {
			rest := bus.TryPull()
			if len(rest) == 0 {
				return
			}
			if ta.processTurn(ctx, cm, rest) == turnStop {
				return
			}
			continue
		}
		events, err := bus.Pull(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("[runAgentLoop:%s] Pull returned: %v, exiting", ta.name, err)
				return
			}
			log.Errorf("[runAgentLoop:%s] Pull error: %v", ta.name, err)
			return
		}
		if len(events) == 0 {
			continue
		}
		if ta.processTurn(ctx, cm, events) == turnStop {
			return
		}
	}
}

// runEventLoop is the persistent entry-owner consume loop: the shared shell with no
// invocation binding (consume persistentBus until ctx is done / a turn stops).
func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
	ta.runAgentLoop(ctx, bus, cm, loopSpec{})
}

// processTurn runs ONE batch through the agent turn pipeline and is the single turn
// primitive: the persistent loop calls it once per pulled batch, a sub-agent Run calls
// it once for its invocation, so both hit the same assembly, projection and execution stage.
//
// - The durable steps are self-classifying: a batch whose events carry no claim commits and acks nothing, which is the derived sub-call case because a transient delegation never enters the durable envelope.
// - The turn generation binding always comes from cm.BeginTurnLease().
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func (ta *TagentAgent) processTurn(ctx context.Context, cm *ContextManager, events []*AgentEvent) turnDisposition {
	retryDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	maxRetries := persistentTurnRetryBudget
	if maxRetries > len(retryDelays) {
		log.Warnf("[runEventLoop:%s] maxRetries=%d exceeds retryDelays=%d — clamped; widen retryDelays if a larger budget is intended",
			ta.name, maxRetries, len(retryDelays))
		maxRetries = len(retryDelays)
	}

	received := events
	selected := dropMeditationFromMixedBatch(events, ta.name)
	if len(selected) != len(events) && ta.meditationMgr != nil {
		ta.meditationMgr.NoteMeditationBatchOutcome(false)
	}
	events = selected
	if len(events) == 0 {
		cm.turnEcho = nil
		ta.finishDurableBatch(ctx, received, nil, completedOutcome())
		return turnContinue
	}

	if batchCarriesMeditation(events) && ta.persistentBus.PendingCount() > 0 {
		return ta.yieldMeditationOnPresence(ctx, cm, received)
	}
	log.Infof("[runEventLoop:%s] iteration start: pulled %d events (%s)",
		ta.name, len(events), summarizeEvents(events))

	ta.persistentBus.DrainRetentionCleanups()

	switch outcome := ta.submitDurableBatchWithBackoff(ctx, received, events); outcome.status {
	case submitCancelled:
		cm.turnEcho = nil
		log.Infof("[runEventLoop:%s] submit cancelled mid-batch — claims retained, exiting", ta.name)
		return turnStop
	case submitConflict:
		cm.turnEcho = nil
		log.Errorf("[runEventLoop:%s] deterministic submit conflict on %s — isolated; STOPPING auto-consumption (fail-closed, §4.2)",
			ta.name, outcome.conflict)
		return turnStop
	case submitTransient:
		ta.releaseBatchClaims(received)
		cm.turnEcho = nil
		log.Warnf("[runEventLoop:%s] transient submit failure after backoff — claims requeued, model NOT called, next batch NOT taken", ta.name)
		return turnContinue
	case submitOK:
	}

	msg := cm.BuildInvocation(events)
	if msg.Content == "" && len(msg.ContentParts) == 0 {
		log.Debugf("[runEventLoop:%s] empty message after merge, skipping", ta.name)
		cm.turnEcho = nil
		ta.finishDurableBatch(ctx, received, events, completedOutcome())
		return turnContinue
	}
	if keys := durableCommittedKeys(events); len(keys) > 0 {
		cm.turnEcho = &echoSpec{
			agent:         cm.name,
			session:       cm.sessionID,
			mergedMessage: msg.Content,
			committedKeys: keys,
		}
		defer func() { cm.turnEcho = nil }()
	}

	cm.SetTriggerSource(extractTriggerSource(events))

	cm.SetInvocationMetadata(extractRootMetadata(events))

	turnMeta := extractRootMetadata(events)
	spanCtx, turnSpan := startTurnSpan(ctx, turnSpanAttrs{
		AgentName:     ta.name,
		TriggerSource: extractTriggerSource(events),
		ChatID:        turnMeta["chat_id"],
		UserID:        turnMeta["user_id"],
		BatchSize:     len(events),
		EventSources:  eventSources(events),
		LinkTraceID:   turnMeta[tagentevent.MetaKeyTraceID],
		LinkSpanID:    turnMeta[tagentevent.MetaKeySpanID],
	})
	spanCtx = governance.WithTriggerSource(spanCtx, extractTriggerSource(events))
	spanCtx = withInvocationID(spanCtx, extractDelegationInvocationID(events))

	turnLease := cm.BeginTurnLease()
	turnExecutor := turnLease.Runner()
	spanCtx = turnLease.WithContext(spanCtx)
	endTurn := func(degenerate bool) {
		turnLease.Release()
		endTurnSpan(turnSpan, degenerate)
	}

	// RunFlow with exponential backoff retry
	var lastErr error
	retried := false
	retriedDegenerate := false
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := ctx.Err(); err != nil {
				log.Infof("[runEventLoop:%s] ctx cancelled during retry, exiting: %v", ta.name, err)
				endTurn(retriedDegenerate)
				return turnStop
			}
			delay := retryDelays[attempt-1]
			log.Warnf("[runEventLoop:%s] RunFlow retry %d/%d after %v", ta.name, attempt, maxRetries, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				log.Infof("[runEventLoop:%s] ctx cancelled during retry wait, exiting", ta.name)
				endTurn(retriedDegenerate)
				return turnStop
			}
		}

		if attempt == 0 && ta.config != nil && ta.config.DegradationBehaviors.ModelBackoff > 0 &&
			ta.degradation != nil && ta.degradation.IsDegraded(reliability.DepModel) {
			log.Warnf("[runEventLoop:%s] DepModel degraded, backing off %v before RunFlow (gate-not-wall)",
				ta.name, ta.config.DegradationBehaviors.ModelBackoff)
			select {
			case <-time.After(ta.config.DegradationBehaviors.ModelBackoff):
			case <-ctx.Done():
				endTurn(retriedDegenerate)
				return turnStop
			}
		}
		if err := cm.RunFlowWithExecutor(spanCtx, msg, turnExecutor); err != nil {
			if cm.LastTurnOutcome().status == turnCancelled {
				endTurn(retriedDegenerate)
				log.Infof("[runEventLoop:%s] §5.1 turn cancelled mid-stream — claim retained, NOT acked", ta.name)
				return turnStop
			}
			if errors.Is(err, ErrExecClosed) {
				endTurn(retriedDegenerate)
				log.Infof("[runEventLoop:%s] turn refused — generation already closed, claim retained", ta.name)
				return turnStop
			}
			lastErr = err
			log.Errorf("[runEventLoop:%s] RunFlow failed (attempt %d/%d): %v", ta.name, attempt+1, maxRetries+1, err)
			if attempt < maxRetries {
				retried = true
				continue
			}
			if ta.degradation != nil && ctx.Err() == nil {
				ta.degradation.ReportFailure(reliability.DepModel, lastErr)
			}
			log.Errorf("[runEventLoop:%s] RunFlow exhausted %d retries: %v", ta.name, maxRetries, lastErr)
		} else {
			lastErr = nil
			if oc := cm.LastTurnOutcome(); oc.status == turnFailed {
				log.Errorf("[runEventLoop:%s] §5.1 turn failed with response error %q — recorded failed, not success", ta.name, oc.err)
				break
			}
			if ta.degradation != nil {
				ta.degradation.ReportSuccess(reliability.DepModel)
			}
			if cm.LastTurnDegenerate() && !retriedDegenerate && attempt < maxRetries {
				retriedDegenerate = true
				log.Warnf("[runEventLoop:%s] degenerate turn (no tool call, empty final) — retrying once", ta.name)
				continue
			}
			break
		}
	}

	if lastErr != nil && !retried {
		log.Errorf("[runEventLoop:%s] RunFlow failed: %v", ta.name, lastErr)
	}

	endTurn(retriedDegenerate)

	batchOutcome := cm.LastTurnOutcome()
	if lastErr != nil {
		batchOutcome = failedOutcome(lastErr.Error())
	}
	cm.setLastBatchOutcome(batchOutcome)
	log.Infof("[runEventLoop:%s] §5.1 turn reduced to %s (batch outcome)", ta.name, batchOutcome.status)

	if installed, verified := cm.turnEchoVerified(); installed && !verified {
		cm.turnEcho = nil
		log.Errorf("[runEventLoop:%s] §4.5 execution credential unverified at model entry — fail-closed, NOT acking, STOPPING auto-consumption", ta.name)
		return turnStop
	}
	cm.turnEcho = nil
	ta.finishDurableBatch(spanCtx, received, events, batchOutcome)

	if ta.meditationMgr != nil && batchCarriesMeditation(events) {
		ta.meditationMgr.NoteMeditationBatchOutcome(true)
	}
	return turnContinue
}

// yieldMeditationOnPresence disposes the consumption-time yield — the second
// yield point, isomorphic to the injection-time drop: the pure-meditation batch
// gives up the turn with no RunFlow, no consumption, watermark unmoved and the
// re-entry guard cleared.
//
//   - Presence is structural (PendingCount on this bus at the moment of consumption), with no time parameter; arrivals after RunFlow started belong to turn atomicity and are deliberately not covered here.
//   - Presence seen here is necessarily non-meditation: the manager holds its single pending slot until the outcome report, so no second meditation event can queue inside this window.
//   - The real events stay queued and merge with the next batch; the yield itself injects nothing.
//   - A completion freezes only over the receipt key reserved by the write-before prepare gate, so the yield runs that gate over the whole received set with an empty selected set: nothing is persisted as an input fact, the model is not called, and every envelope still ends receipted and acked — the injection-time drop's end state.
//   - Transient gate failures requeue the claims with nothing acked; a deterministic conflict isolates and stops auto-consumption, as every other submit verdict does.
//   - An isolated curator bus (the external form) never shows presence, so that form keeps executing without a behavioral delta.
//
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
func (ta *TagentAgent) yieldMeditationOnPresence(ctx context.Context, cm *ContextManager, received []*AgentEvent) turnDisposition {
	log.Infof("[runEventLoop:%s] consumption re-check: %d pending event(s) behind a pure meditation batch — yielding",
		ta.name, ta.persistentBus.PendingCount())
	cm.turnEcho = nil
	if ta.meditationMgr != nil {
		ta.meditationMgr.NoteMeditationBatchOutcome(false)
	}
	switch gate := ta.submitDurableBatchWithBackoff(ctx, received, nil); gate.status {
	case submitCancelled:
		log.Infof("[runEventLoop:%s] yield prepare cancelled mid-batch — claims retained, exiting", ta.name)
		return turnStop
	case submitConflict:
		log.Errorf("[runEventLoop:%s] deterministic submit conflict on %s during yield prepare — isolated; STOPPING auto-consumption (fail-closed, §4.2)",
			ta.name, gate.conflict)
		return turnStop
	case submitTransient:
		ta.releaseBatchClaims(received)
		log.Warnf("[runEventLoop:%s] transient submit failure during yield prepare — claims requeued, nothing acked", ta.name)
		return turnContinue
	case submitOK:
	}
	ta.finishDurableBatch(ctx, received, nil, completedOutcome())
	return turnContinue
}

// batchCarriesMeditation reports whether the selected batch still holds a meditation
// external_input event at turn end — the turn being reduced executed the injected
// meditation, so the manager owes an outcome report. A mixed batch's meditation event
// was already dropped at the yield point, so only pure-meditation batches reach this
// true; a failed turn counts as executed too, which is what keeps a broken model from
// storming re-injections.
func batchCarriesMeditation(events []*AgentEvent) bool {
	for _, evt := range events {
		if evt != nil && evt.Type == tagentevent.TypeExternalInput && evt.Source == "meditation" {
			return true
		}
	}
	return false
}

// dropMeditationFromMixedBatch removes meditation events from a batch that
// also contains any non-meditation event.
// Yielding is postponing, not abandoning: the filter itself changes nothing else —
// the watermark stays put at the drop (it advances only on consumption), so the facts
// a yielded reflection was meant to chew on stay novel and the next tick re-evaluates
// the very same window. The manager-side accounting of that outcome report lives at
// the call site (NoteMeditationBatchOutcome). Pure-meditation batches pass through
// unchanged.
func dropMeditationFromMixedBatch(events []*AgentEvent, agentName string) []*AgentEvent {
	hasMeditation, hasOther := false, false
	for _, evt := range events {
		if evt == nil {
			continue
		}
		if evt.Type == tagentevent.TypeExternalInput && evt.Source == "meditation" {
			hasMeditation = true
		} else {
			hasOther = true
		}
	}
	if !hasMeditation || !hasOther {
		return events
	}
	filtered := make([]*AgentEvent, 0, len(events))
	for _, evt := range events {
		if evt != nil && evt.Type == tagentevent.TypeExternalInput && evt.Source == "meditation" {
			continue
		}
		filtered = append(filtered, evt)
	}
	log.Infof("[runEventLoop:%s] mixed batch: dropped %d meditation event(s) — yielding to real input",
		agentName, len(events)-len(filtered))
	return filtered
}

// extractTriggerSource determines the trigger source from a batch of
// AgentEvents. Uses the first external_input event's Source field. This
// provides deterministic source identification for consumer dispatch
// (meditation vs task vs user) without content-based inference.
func extractTriggerSource(events []*AgentEvent) string {
	firstLineage := ""
	firstMechanical := ""
	taskWithoutLineage := false
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == "user" {
			return "user"
		}
		if firstLineage == "" {
			if v, ok := evt.Metadata[tagentevent.MetaKeyTriggerSource].(string); ok && v != "" {
				firstLineage = v
			}
		}
		if firstMechanical == "" && evt.Source != "" {
			firstMechanical = evt.Source
		}
		if evt.Source == SourceTask && evt.Metadata["lineage_absent"] == "true" {
			taskWithoutLineage = true
		}
	}
	if firstLineage == "user" {
		return "user"
	}
	if firstLineage != "" {
		return firstLineage
	}
	if firstMechanical != "" {
		if firstMechanical == SourceTask && taskWithoutLineage {
			return "task-unstamped"
		}
		return firstMechanical
	}
	if taskWithoutLineage {
		return "task-unstamped"
	}
	return "user"
}

// metaKeyInvocationID extractRootMetadata extracts metadata from a batch of AgentEvents.
// Collects metadata from external_input events and merges them into a single
// map. Later events override earlier ones. Empty keys or values are ignored.
// controlMetaKeys never propagate through the Origin/courier pipeline
// : they are deterministic per-event facts written by the
// framework (settle verdicts, gate markers, registry keys, detached
// timestamps) — leaking them into a later task's Origin baggage makes the
// chain look like lineage and can mis-restore another task's detachedAt.
//
// metaKeyInvocationID carries a delegation input's
// originating invocation ID as the correlation handle a resident owner will use
// to route a background follow-up back to the right waiting request. It rides
// event.Metadata as a CONTROL key on purpose: extractRootMetadata must never
// forward it into user meta_* / model-visible text, so the model can neither
// observe nor spoof routing (D4「控制字段不透传给模型伪造路由」).
const metaKeyInvocationID = "invocation_id"

var controlMetaKeys = map[string]bool{
	"settle_status": true, "task_id": true, "lineage_absent": true,
	"detached_at_ms":    true,
	metaKeyInvocationID: true,
}

// extractDelegationInvocationID returns the S1 correlation handle (metaKeyInvocationID)
// off the first genuine delegation-INPUT event that carries it — the delegation's
// originating invocation id, empty for non-delegation turns. It reads event.Metadata
// directly rather than via extractRootMetadata because invocation_id is intentionally a
// CONTROL key (never model-visible); here only the framework reads it to thread routing
// context for S2m's Origin stamp.
//
// It EXCLUDES background-settle reclaim events (Source==SourceTask). newTaskSettledEvent
// copies the settled task's whole Origin — including the originating delegation's
// invocation_id — into the event's metadata; a reclaim turn is NOT the delegation input,
// so inheriting its invocation_id would stamp the host/entry owner's continuation-spawned
// tasks with a sub-call's id (mis-attribution, and once routing is wired, misdelivery to
// a finished sub-call's sink). A delegation's own continuation loop supplies its
// invocation_id explicitly rather than via this extraction, so narrowing to non-settle
// inputs is safe.
func extractDelegationInvocationID(events []*AgentEvent) string {
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput || evt.Source == SourceTask {
			continue
		}
		if s, ok := evt.Metadata[metaKeyInvocationID].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func extractRootMetadata(events []*AgentEvent) map[string]string {
	md := make(map[string]string)
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		for k, v := range evt.Metadata {
			if k == "" || controlMetaKeys[k] {
				continue
			}
			if s, ok := v.(string); ok && s != "" {
				md[k] = s
			}
		}
	}
	return md
}

// summarizeEvents returns a compact summary of event types in a batch.
func summarizeEvents(events []*AgentEvent) string {
	counts := make(map[string]int)
	for _, evt := range events {
		if evt == nil {
			counts["nil"]++
			continue
		}
		counts[evt.Type]++
	}
	var parts []string
	for typ, n := range counts {
		parts = append(parts, fmt.Sprintf("%s:%d", typ, n))
	}
	return strings.Join(parts, ", ")
}
