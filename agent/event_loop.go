package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/plugin"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// runEventLoop runs the persistent event loop for this agent.
// It pulls events from the EventBus, builds an invocation, and runs the flow.
// The loop runs in a dedicated goroutine and exits when ctx is cancelled.
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
		events = dropMeditationFromMixedBatch(events, ta.name)
		if len(events) == 0 {
			// cold-eyes Warning 2：被清空的批次若含已 claim 的 durable 事件，
			// 跳过 receipt/ack 会僵尸化 envelope（每次重启重 claim→再空跑）。
			// finishDurableBatch 对空 provenance 幂等，此处安全收敛。
			ta.clearTurnDurableInbound()
			ta.finishDurableBatch(events)
			continue
		}
		log.Infof("[runEventLoop:%s] iteration start: pulled %d events (%s)",
			ta.name, len(events), summarizeEvents(events))

		// cold-eyes R2 M-1（结构修）: claim 事实统一由 persistBusEvent 逐消息
		// 预落库（GetEvent-guard 重放去重 + 单数 dedup_key + 回写一次到位），
		// 管线（MemoryPlugin）经 FactsPrePersisted 跳过合并输入的重复入库——
		// 消除「合并事实 vs 逐消息事实」粒度分裂：多 envelope 批次/跨 crash
		// 批次组成变化下每个 envelope 都有自有证据，receipt+ack 永不失据。
		prePersisted := false
		for _, ev := range events {
			if path, _ := ev.Metadata["inbox_path"].(string); path == "" {
				continue
			}
			cm.persistBusEvent(ev)
			prePersisted = true
		}
		if prePersisted {
			ev0 := events[0]
			path, _ := ev0.Metadata["inbox_path"].(string)
			rid, _ := ev0.Metadata["inbox_request_id"].(string)
			dk, _ := ev0.Metadata["inbox_dedup_key"].(string)
			ta.SetTurnDurableInbound(plugin.DurableInbound{
				Path: path, RequestID: rid, DedupKey: dk, FactsPrePersisted: true,
			})
		}
		msg := cm.BuildInvocation(events)
		if msg.Content == "" {
			log.Debugf("[runEventLoop:%s] empty message after merge, skipping", ta.name)
			ta.clearTurnDurableInbound()
			ta.finishDurableBatch(events) // cold-eyes Warning 2：同上，防 envelope 僵尸化
			continue
		}
		// Recovery one-shot tail notice (3.10): appended to the request TAIL —
		// never into the fact chain, never mutating the historical prefix
		// (prefix-cache safe). Empty for the healthy path.
		if notice := cm.TakeRecoveryNotice(); notice != "" {
			msg.Content += "\n\n" + notice
			log.Infof("[runEventLoop:%s] recovery notice attached to first request", ta.name)
		}

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

		// Durable inbox confirmation (3.4/3.5): the consuming turn finished —
		// write the fact-chain receipt event (dedup window truth source) and
		// only then receipt+ack the envelope. Store failure keeps the claim
		// on disk; the next process replays it.
		ta.clearTurnDurableInbound() // cold-eyes Major 1: turn-scoped provenance ends here
		ta.finishDurableBatch(events)

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
	// cold-eyes R2 S-1: inbox control metadata must never leak into Origin
	// baggage (filesystem paths downstream; stale inbox_path misleading the
	// receipt path when such an event re-enters a batch).
	"inbox_path": true, "inbox_request_id": true, "inbox_dedup_key": true,
	"inbox_dedup_keys": true,
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
