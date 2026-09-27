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

// turnDisposition tells the persistent loop what to do after a batch: keep
// pulling, or stop auto-consuming entirely (the conditions that previously
// `return`ed out of runEventLoop mid-body — cancellation, a deterministic submit
// conflict, or an unverified execution credential).
type turnDisposition int

const (
	turnContinue turnDisposition = iota // batch handled; keep consuming
	turnStop                            // stop auto-consuming and exit the loop
)

// persistentTurnRetryBudget is the per-turn model retry budget, UNIFIED across both
// isomorphic forms since S3m-c (user decision 2026-09-26 "统一"): the top-level
// event loop AND a derived sub-agent invocation run the same turn with the same
// self-heal budget. A synchronous sub-call no longer passes 0 — its caller blocks on
// the FIRST answer regardless (the two-stage ACK→补最终 protocol is unchanged), and
// letting the callee self-heal a hiccup (≤~700ms) is far cheaper than the caller
// re-issuing the whole delegation.
const persistentTurnRetryBudget = 3

// loopSpec parameterizes the ONE shared consume shell for the two isomorphic forms
// (design I-2). The entry owner binds nothing (empty invocationID): it consumes
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
		// Termination for the invocation form: the越窗 tail is kept alive ONLY while
		// a bound bus still has booked-but-undelivered spawns (awaiting). Once nothing
		// is pending — OR the id is not bound at all (a loop whose registry never took
		// the binding, e.g. a bare test agent) so no settle can ever route here — the
		// tail drains the bus and exits. route publishes BEFORE it decrements pending,
		// so when awaiting is false every delivered settle is already pullable: draining
		// after the check can never miss one. The entry owner (empty id) has no barrier
		// and skips this, consuming until ctx/turnStop exactly as before.
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

// processTurn runs ONE batch through the agent's turn pipeline — freeze/drop →
// durable submit gate → BuildInvocation (message assembly) → projection / trigger
// source / metadata binding → turn lease → RunFlow (bounded retry) → outcome
// reduce → durable finish/receipt/ack → idle anchor.
//
// It is the single turn primitive (§7.1 D2): the persistent loop calls it once per
// pulled batch and a sub-agent Run calls it once for its invocation, so "直连宿主"
// and "作为被调方" hit the SAME assembly/projection/execution stage. The durable
// steps are self-classifying — a batch whose events carry no claim commits and
// acks nothing (§4.4), which is exactly the derived sub-call case under design
// interpretation A (transient delegation never enters the durable envelope).
//
// The turn's generation binding always comes from cm.BeginTurnLease(): for the
// persistent loop that is the owner CM's current face; for a sub-call whose cm is
// the PRIVATE invocation CM it is that CM's own construction-time binding (and a
// private CM has no org-reloader, so none fires) — the same executor RunFlow used
// before this unification. The caller's D5 lifetime reference is held separately
// by the sub-call path (Run's own invLease defer), not through this function.
//
// The retry budget is UNIFIED (S3m-c): both the persistent loop and a derived
// sub-call run this body with persistentTurnRetryBudget, so the turn stage has no
// entry-kind divergence. A synchronous sub-call's caller still blocks on the FIRST
// answer (the two-stage protocol lives in Run's channel close, not here); the only
// change is that a hiccup self-heals in the callee rather than being re-issued by
// the caller. The shared assembly / projection / execution stage is identical
// either way.
func (ta *TagentAgent) processTurn(ctx context.Context, cm *ContextManager, events []*AgentEvent) turnDisposition {
	retryDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	maxRetries := persistentTurnRetryBudget
	if maxRetries > len(retryDelays) {
		// retryDelays is indexed by attempt-1 in the loop; clamp so a caller cannot
		// pass a budget beyond the configured delays (would index-panic). Warn so a
		// future retry budget wider than the delay table is observable rather than
		// silently truncated (review S-5); if widened for real, grow retryDelays too.
		log.Warnf("[runEventLoop:%s] maxRetries=%d exceeds retryDelays=%d — clamped; widen retryDelays if a larger budget is intended",
			ta.name, maxRetries, len(retryDelays))
		maxRetries = len(retryDelays)
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
		return turnContinue
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
		return turnStop
	case submitConflict:
		// Fail-closed: the conflicting envelope is isolated (quarantined, kept on
		// disk); the remaining claims are retained (NOT acked, NOT dropped) and this
		// agent STOPS auto-consuming rather than guessing past the conflict (§4.2).
		cm.turnEcho = nil
		log.Errorf("[runEventLoop:%s] deterministic submit conflict on %s — isolated; STOPPING auto-consumption (fail-closed, §4.2)",
			ta.name, outcome.conflict)
		return turnStop
	case submitTransient:
		// Backoff budget spent, batch still uncommitted: requeue claims to pending
		// (oldest re-claimed first, order preserved) and skip the model — never pull a
		// next batch ahead of the stuck one, never model with missing inputs (§4.2).
		ta.releaseBatchClaims(events)
		cm.turnEcho = nil
		log.Warnf("[runEventLoop:%s] transient submit failure after backoff — claims requeued, model NOT called, next batch NOT taken", ta.name)
		return turnContinue
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
		return turnContinue
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
	// S2m (introduce-durable-workflow-engine): thread the delegation's correlation
	// handle (the S1 invocation id off the input event) onto the turn context so a
	// task spawned during this turn stamps it into Origin. This is pure data — no
	// consumer reads it yet — but it is the key S3m uses to route a越窗 task_settled
	// back to THIS invocation's loop. Empty for non-delegation (user/entry) turns.
	spanCtx = withInvocationID(spanCtx, extractDelegationInvocationID(events))

	// §3.1/§3.2（introduce-durable-workflow-engine）：批次已冻结、进入业务 turn
	// 且首次读取编排绑定之前——一次版本获取（与 ops 入口 CheckOrgReload 同一条
	// 发布通路）。取到引用必须在传输重试循环**之外**：本 turn 的全部 attempt、
	// 全部模型迭代与工具轮次共用同一执行器，中途发布的新代只服务下一个 turn。
	//
	// §2.3：获取即登记在途引用，故释放必须覆盖本 turn 的**每一条**退路。这里把它
	// 折进既有的 per-turn 清理（endTurnSpan 原本就已在每个 return 上调）——不是新增
	// 纪律，而是复用既有纪律：将来漏掉 endTurnSpan 的退路会同时漏掉释放。
	turnLease := cm.BeginTurnLease()
	turnExecutor := turnLease.Runner()
	// §3.2/§4.1: the turn's lease rides the call-chain context, so every retry
	// attempt and every execution derived inside this turn (nested delegation,
	// post-ACK background run) references the SAME generation instead of
	// re-reading whatever is published by then. RunFlow joins this lease rather
	// than taking a second count, which is what makes "one business turn, one
	// reference" true (§5.1「业务 turn 不因双重登记多计」).
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
			// Check ctx before retrying
			if err := ctx.Err(); err != nil {
				log.Infof("[runEventLoop:%s] ctx cancelled during retry, exiting: %v", ta.name, err)
				endTurn(retriedDegenerate) // M10（§8.4）：早退也 End span（防泄漏）
				return turnStop
			}
			delay := retryDelays[attempt-1]
			log.Warnf("[runEventLoop:%s] RunFlow retry %d/%d after %v", ta.name, attempt, maxRetries, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				log.Infof("[runEventLoop:%s] ctx cancelled during retry wait, exiting", ta.name)
				endTurn(retriedDegenerate) // M10（§8.4）：早退也 End span（防泄漏）
				return turnStop
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
				endTurn(retriedDegenerate)
				return turnStop
			}
		}
		if err := cm.RunFlowWithExecutor(spanCtx, msg, turnExecutor); err != nil {
			// §5.1: a mid-turn shutdown cancellation is now surfaced as an error
			// with a cancelled outcome (it used to return nil and fall through to
			// the ACK below, dropping a turn that reached no terminal state). No
			// completion is formed and the claim is retained for the next process
			// (spec L90/L130) — stop without finishDurableBatch.
			if cm.LastTurnOutcome().status == turnCancelled {
				endTurn(retriedDegenerate)
				log.Infof("[runEventLoop:%s] §5.1 turn cancelled mid-stream — claim retained, NOT acked", ta.name)
				return turnStop
			}
			if errors.Is(err, ErrExecClosed) {
				// The generation this turn was pinned on converged shut mid-flight. That is
				// NOT transient I/O: no transport retry (T-G warns that retries multiply the
				// failure count and collapse FailThreshold), no degradation report. Mirror
				// §5.1's mid-shutdown branch — form no completion, retain the claim so a
				// rebuilt owner reprocesses the durable input instead of losing it.
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
	endTurn(retriedDegenerate)

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
		return turnStop
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
	return turnContinue
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
//
// metaKeyInvocationID (§7.3/D4 groundwork, S1) carries a delegation input's
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
	// The v1 inbox control keys (inbox_path/inbox_request_id/inbox_dedup_key)
	// are gone: durable provenance is now a typed, non-JSON claim (F1/F4), so
	// there is nothing to leak into Origin baggage.
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
