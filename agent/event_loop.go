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

// runEventLoop runs the persistent event loop for this agent.
// It pulls events from the EventBus, builds an invocation, and runs the flow.
// The loop runs in a dedicated goroutine and exits when ctx is cancelled.
// durableCommittedKeys collects the distinct fact EventKeys already committed for this
// batch (§4.4): each durable claim carries its frozen FullEvent JSON (PreparedFact); the
// keys seed the echo credential consumed by §4.5. Volatile events (no claim) contribute
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

func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
	const maxRetries = 3
	retryDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	for {
		if err := ctx.Err(); err != nil {
			log.Infof("[runEventLoop:%s] ctx cancelled, exiting: %v", ta.name, err)
			return
		}

		events, err := bus.Pull(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("[runEventLoop:%s] Pull returned: %v, exiting", ta.name, err)
				return
			}
			log.Errorf("[runEventLoop:%s] Pull error: %v", ta.name, err)
			return
		}
		if len(events) == 0 {
			continue
		}
		// Mixed-batch defense (meditation-gate-split D4): a meditation event
		// sharing a batch with anything else means the agent was NOT idle when
		// the batch was pulled — meditation's constitutional premise ("never
		// interrupt activity") is already broken, so it yields. This also keeps
		// extractTriggerSource from mislabeling the whole turn in either
		// direction (meditation content tagged "task" / a user task result
		// tagged "meditation" and dropped by consumers).
		// §4.1 (design 决策4 L106): the original consumed set is FROZEN and never
		// overwritten. Meditation yielding is a model-input DISPOSITION (selected ⊆
		// received), not a mutation of the batch. Receipt/ack provenance always runs
		// over the full received set, so a yielding meditation's durable envelope is
		// still consumed-and-evidenced and never zombied by filtering (previously a
		// partially-dropped batch left the dropped-but-claimed envelope un-finished).
		received := events
		events = dropMeditationFromMixedBatch(events, ta.name)
		if len(events) == 0 {
			// cold-eyes Warning 2 + §4.1: the batch emptied after the meditation
			// disposition. Every consumed durable envelope (including the yielding
			// ones) still gets its completion evidence over `received`, so none is
			// re-claimed and re-run empty forever (zombie). finishDurableBatch is
			// idempotent on already-settled envelopes.
			cm.turnEcho = nil
			ta.finishDurableBatch(ctx, received, nil, completedOutcome())
			continue
		}
		log.Infof("[runEventLoop:%s] iteration start: pulled %d events (%s)",
			ta.name, len(events), summarizeEvents(events))

		// §3.6/L96: converge any deferred ack-cleanup barriers from prior turns —
		// envelopes whose unlink landed but whose dir-sync failed keep holding
		// capacity + the retention lease until this drain completes the barrier and
		// releases them exactly once. Idempotent and cheap when nothing is owed.
		ta.persistentBus.DrainRetentionCleanups()

		// §4.2/§4.3 durable submit gate: freeze + commit the WHOLE batch before the
		// model. The gate returns a CLASSIFIED outcome (not a bool), so the loop never
		// treats one input as the batch: a deterministic conflict isolates the input and
		// stops auto-consumption; a transient I/O failure is re-attempted on the SAME
		// batch under bounded backoff; the model is reached ONLY once every selected fact
		// committed. Batch all-or-nothing (§3.6-①): no fact is written unless every
		// envelope prepared, so a half-prepared input never enters the fact chain.
		switch outcome := ta.submitDurableBatchWithBackoff(ctx, received, events); outcome.status {
		case submitCancelled:
			cm.turnEcho = nil
			log.Infof("[runEventLoop:%s] submit cancelled mid-batch — claims retained, exiting", ta.name)
			return
		case submitConflict:
			// Fail-closed: the conflicting envelope is isolated (quarantined, kept on
			// disk); the remaining claims are retained (NOT acked, NOT dropped) and this
			// agent STOPS auto-consuming rather than guessing past the conflict (§4.2).
			cm.turnEcho = nil
			log.Errorf("[runEventLoop:%s] deterministic submit conflict on %s — isolated; STOPPING auto-consumption (fail-closed, §4.2)",
				ta.name, outcome.conflict)
			return
		case submitTransient:
			// Backoff budget spent, batch still uncommitted: requeue claims to pending
			// (oldest re-claimed first, order preserved) and skip the model — never pull a
			// next batch ahead of the stuck one, never model with missing inputs (§4.2).
			ta.releaseBatchClaims(events)
			cm.turnEcho = nil
			log.Warnf("[runEventLoop:%s] transient submit failure after backoff — claims requeued, model NOT called, next batch NOT taken", ta.name)
			continue
		case submitOK:
			// Every selected fact is committed — proceed to build the invocation + model.
		}

		msg := cm.BuildInvocation(events)
		if msg.Content == "" && len(msg.ContentParts) == 0 {
			// §4.3: skip only a genuinely empty invocation. A non-text (image/file) input
			// has empty Content but valid ContentParts — it must NOT be dropped here.
			log.Debugf("[runEventLoop:%s] empty message after merge, skipping", ta.name)
			cm.turnEcho = nil
			ta.finishDurableBatch(ctx, received, events, completedOutcome()) // §4.1: ack the full consumed set (同上, 防僵尸)
			continue
		}
		// §4.4 (design 决策4): this turn's durable facts are committed. Install the batch
		// echo spec ONLY when there are committed facts (durable claims) — RunFlow then
		// mints a per-attempt credential so MemoryPlugin skips exactly THIS merged echo
		// (root invocation, author=user, content==merged), never every user event. A
		// volatile batch (no claims) installs nothing → the plugin stores normally.
		if keys := durableCommittedKeys(events); len(keys) > 0 {
			cm.turnEcho = &echoSpec{
				agent:         cm.name,
				session:       cm.sessionID,
				mergedMessage: msg.Content,
				committedKeys: keys,
			}
		}
		// The one-shot cold-start recovery notice is deliberately NOT appended to
		// `msg` here (F9 fix): that would route it through the invocation store,
		// where it would enter the fact chain as a user input and pollute history.
		// It is instead consumed at the ACTUAL model call by executionGateModel's
		// withRecoveryNotice (cm.TakeRecoveryNotice), which appends it to a copy of
		// the request tail so it reaches the model without ever being stored (§4.5C/D6).

		// Determine trigger source from batch events for deterministic
		// consumer-side dispatch. The source is attached to outputCh
		// events via StateDelta["trigger_source"] in RunFlow.
		cm.SetTriggerSource(extractTriggerSource(events))

		// Extract and propagate metadata (chat_id, user_name, etc.) from
		// the source event to all derived events via StateDelta["meta_*"].
		cm.SetInvocationMetadata(extractRootMetadata(events))

		// T-B: turn root span（一 turn 一 trace）。spanCtx 传给 RunFlow，框架自动 span 挂
		// 为子树；turn 末显式 End（循环内禁用 defer，否则累积到函数退出）。noop 当未配 OTLP。
		turnMeta := extractRootMetadata(events)
		spanCtx, turnSpan := startTurnSpan(ctx, turnSpanAttrs{
			AgentName:     ta.name,
			TriggerSource: extractTriggerSource(events),
			ChatID:        turnMeta["chat_id"],
			UserID:        turnMeta["user_id"],
			BatchSize:     len(events),
			EventSources:  eventSources(events),
			// C9：task_settled 回流的新 turn 携带原 spawn turn 的 trace 锚点（经 Origin→Metadata），
			// 建 span link 闭合三投影的 OTel span 维度。非异步回流 turn 这两键为空 → 不建 link。
			LinkTraceID: turnMeta[tagentevent.MetaKeyTraceID],
			LinkSpanID:  turnMeta[tagentevent.MetaKeySpanID],
		})
		// T-G: 盖章 trigger source 到 turn ctx，供 GovernanceTool 做 goal-required 判定
		// （meditation/task 触发的 high+ 操作须挂 goal；user 触发不需）。spanCtx 经 RunFlow
		// 派生流到工具调用，GovernanceTool.Call 从 ctx 读回。治理关闭时该值不被消费（零开销）。
		spanCtx = governance.WithTriggerSource(spanCtx, extractTriggerSource(events))

		// RunFlow with exponential backoff retry
		var lastErr error
		retried := false
		retriedDegenerate := false
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				// Check ctx before retrying
				if err := ctx.Err(); err != nil {
					log.Infof("[runEventLoop:%s] ctx cancelled during retry, exiting: %v", ta.name, err)
					endTurnSpan(turnSpan, retriedDegenerate) // M10（§8.4）：早退也 End span（防泄漏）
					return
				}
				delay := retryDelays[attempt-1]
				log.Warnf("[runEventLoop:%s] RunFlow retry %d/%d after %v", ta.name, attempt, maxRetries, delay)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					log.Infof("[runEventLoop:%s] ctx cancelled during retry wait, exiting", ta.name)
					endTurnSpan(turnSpan, retriedDegenerate) // M10（§8.4）：早退也 End span（防泄漏）
					return
				}
			}

			// 5.4（design-report-closeout）+ §8.11⑫：model 依赖退化的行为响应——turn 级
			// 退避（仅首个 attempt；重试已有 retryDelays，不叠加）。
			if attempt == 0 && ta.config != nil && ta.config.DegradationBehaviors.ModelBackoff > 0 &&
				ta.degradation != nil && ta.degradation.IsDegraded(reliability.DepModel) {
				log.Warnf("[runEventLoop:%s] DepModel degraded, backing off %v before RunFlow (gate-not-wall)",
					ta.name, ta.config.DegradationBehaviors.ModelBackoff)
				select {
				case <-time.After(ta.config.DegradationBehaviors.ModelBackoff):
				case <-ctx.Done():
					endTurnSpan(turnSpan, retriedDegenerate)
					return
				}
			}
			if err := cm.RunFlow(spanCtx, msg); err != nil {
				// §5.1: a mid-turn shutdown cancellation is now surfaced as an error
				// with a cancelled outcome (it used to return nil and fall through to
				// the ACK below, dropping a turn that reached no terminal state). No
				// completion is formed and the claim is retained for the next process
				// (spec L90/L130) — stop without finishDurableBatch.
				if cm.LastTurnOutcome().status == turnCancelled {
					endTurnSpan(turnSpan, retriedDegenerate)
					log.Infof("[runEventLoop:%s] §5.1 turn cancelled mid-stream — claim retained, NOT acked", ta.name)
					return
				}
				lastErr = err
				log.Errorf("[runEventLoop:%s] RunFlow failed (attempt %d/%d): %v", ta.name, attempt+1, maxRetries+1, err)
				if attempt < maxRetries {
					retried = true
					continue
				}
				// Retries exhausted. Note: RunFlow only returns transport-level
				// errors (model-API errors flow through outputCh as events), so
				// there is no meaningful error event to publish — just log.
				// T-G: model 依赖退化上报——每 turn 仅一次（重试耗尽才算真失败；M1：避免单 turn
				// 多次重试放大计数使 FailThreshold 语义塌缩、瞬时抖动即 degraded）；ctx 取消
				// （关机）不计退化（N2）。
				if ta.degradation != nil && ctx.Err() == nil {
					ta.degradation.ReportFailure(reliability.DepModel, lastErr)
				}
				log.Errorf("[runEventLoop:%s] RunFlow exhausted %d retries: %v", ta.name, maxRetries, lastErr)
			} else {
				lastErr = nil
				// §5.1: a response-internal model/framework error arrives with a nil
				// transport return (the framework carries it as an event's
				// Response.Error, not as RunFlow's error). The old code reported
				// success and ACKed such a turn; it now reduces to a definite failed
				// result — no success report, and it is NOT retried on the transport
				// budget (this is a terminal model outcome, not transient I/O).
				if oc := cm.LastTurnOutcome(); oc.status == turnFailed {
					log.Errorf("[runEventLoop:%s] §5.1 turn failed with response error %q — recorded failed, not success", ta.name, oc.err)
					break
				}
				// T-G: model 依赖成功上报（degraded→recovering→normal 恢复路径）。
				if ta.degradation != nil {
					ta.degradation.ReportSuccess(reliability.DepModel)
				}
				// A degenerate turn (no tool call, empty final) is an occasional
				// model hiccup that would otherwise stall the conversation until
				// the next external event — retry it exactly once.
				if cm.LastTurnDegenerate() && !retriedDegenerate && attempt < maxRetries {
					retriedDegenerate = true
					log.Warnf("[runEventLoop:%s] degenerate turn (no tool call, empty final) — retrying once", ta.name)
					continue
				}
				break
			}
		}

		if lastErr != nil && !retried {
			// Single failure without retry (shouldn't happen with current logic, but defensive)
			log.Errorf("[runEventLoop:%s] RunFlow failed: %v", ta.name, lastErr)
		}

		// T-B: 关闭 turn span（退化重试标记为属性，同一 turn 不另开 root span）。
		endTurnSpan(turnSpan, retriedDegenerate)

		// §5.1: reduce the retried attempts into one explicit batch terminal state
		// for this turn. A transport/start error that spent the whole retry budget
		// is a definite failed result; otherwise the last attempt's reduced outcome
		// (completed or failed) stands. A cancellation never reaches here — it
		// returned above with the claim retained. This batchOutcome is the value the
		// completion freeze (§5.2/§5.3) persists; the loop no longer treats "RunFlow
		// returned nil" as the only definition of done.
		batchOutcome := cm.LastTurnOutcome()
		if lastErr != nil {
			batchOutcome = failedOutcome(lastErr.Error())
		}
		cm.setLastBatchOutcome(batchOutcome)
		log.Infof("[runEventLoop:%s] §5.1 turn reduced to %s (batch outcome)", ta.name, batchOutcome.status)

		// Durable inbox confirmation (3.4/3.5): the consuming turn finished —
		// write the fact-chain receipt event (dedup window truth source) and
		// only then receipt+ack the envelope. Store failure keeps the claim
		// on disk; the next process replays it.
		// §4.5: fail-closed if this durable turn's execution credential was never
		// verified at the model entry — the framework fed back a non-matching/absent input,
		// or a swallowed plugin error (a store failure the framework logs and continues
		// past) downgraded it. The model-entry gate blocks the real call in that state, so
		// the committed inputs were NOT confirmed as what ran: do NOT ack (claims stay for
		// replay) and stop auto-consuming rather than cross the commit gate (§4.2 pattern).
		if installed, verified := cm.turnEchoVerified(); installed && !verified {
			cm.turnEcho = nil
			log.Errorf("[runEventLoop:%s] §4.5 execution credential unverified at model entry — fail-closed, NOT acking, STOPPING auto-consumption", ta.name)
			return
		}
		cm.turnEcho = nil                                              // §4.4: per-attempt echo credential scope ends at turn end
		ta.finishDurableBatch(spanCtx, received, events, batchOutcome) // §5.3: freeze completion → fixed-key receipt → receipt+ack over the frozen consumed set

		// Idle-gate anchor (meditation-gate-split): every turn end counts as
		// activity, regardless of trigger source or success — lineage-agnostic
		// by design. A meditation-derived turn merely DELAYS the next
		// meditation; only a user injection can re-arm the novelty gate.
		if ta.meditationMgr != nil {
			ta.meditationMgr.UpdateLastTurnEnd(time.Now())
		}
	}
}

// dropMeditationFromMixedBatch removes meditation events from a batch that
// also contains any non-meditation event (meditation-gate-split D4).
// The dropped meditation is NOT re-injected: lastMeditation already advanced
// at fire time, and the novelty gate re-evaluates naturally at the next real
// idle window. Pure-meditation batches pass through unchanged.
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
	log.Infof("[runEventLoop:%s] mixed batch: dropped %d meditation event(s) — agent not idle",
		agentName, len(events)-len(filtered))
	return filtered
}

// extractTriggerSource determines the trigger source from a batch of
// AgentEvents. Uses the first external_input event's Source field. This
// provides deterministic source identification for consumer dispatch
// (meditation vs task vs user) without content-based inference.
func extractTriggerSource(events []*AgentEvent) string {
	// Priority chain (meditation-lineage fix, 2026-09-14):
	// 1. A real user input in the batch always wins (mechanical Source "user").
	// 2. Lineage: task_settled reclaim events carry the spawning turn's
	//    trigger_source in Metadata (Origin baggage captured at spawn time,
	//    fanned out at settle). A task spawned during a meditation turn must
	//    keep meditation lineage so app-side delivery gates (main.go dispatch)
	//    hold its reclaim output back from the user chat.
	// 3. Mechanical Source (pre-existing behavior for events without lineage).
	// 4. Default "user".
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
		// hardening-review-batch2 1.3：源头标记的「无世系 task 结算」——机械
		// 兜底 "task" 不可信（宿主白名单会放投）。降级为 task-unstamped，
		// 宿主 fail-closed 扣留；既有 bare-task 语义（无标记）不变。
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

// extractRootMetadata extracts metadata from a batch of AgentEvents.
// Collects metadata from external_input events and merges them into a single
// map. Later events override earlier ones. Empty keys or values are ignored.
// controlMetaKeys never propagate through the Origin/courier pipeline
// (cold-eyes P2-3): they are deterministic per-event facts written by the
// framework (settle verdicts, gate markers, registry keys, detached
// timestamps) — leaking them into a later task's Origin baggage makes the
// chain look like lineage and can mis-restore another task's detachedAt.
var controlMetaKeys = map[string]bool{
	"settle_status": true, "task_id": true, "lineage_absent": true,
	"detached_at_ms": true,
	// The v1 inbox control keys (inbox_path/inbox_request_id/inbox_dedup_key)
	// are gone: durable provenance is now a typed, non-JSON claim (F1/F4), so
	// there is nothing to leak into Origin baggage.
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
