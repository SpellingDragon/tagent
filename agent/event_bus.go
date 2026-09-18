package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentEvent is the unified event type for the agent's persistent event bus
// (turn-间事件邮箱). Every event flowing through the bus is an AgentEvent.
//
// Exactly one event type serves as a bus trigger:
//   - TypeExternalInput: external input (user, tmux, meditation, task settle)
//
// agent_output does NOT enter the bus — it is emitted directly to outputCh.
// Honest scope note: the bus coordinates TURNS; the turn-INTERNAL tool loop
// remains the upstream framework's synchronous ReAct (runner.Run). The old
// "tool_use bus trigger" abstraction (TypeToolUse) had no producer and no
// consumer — removed as ghost code (implementation-hardening 4.1).
type AgentEvent struct {
	// ID is a unique identifier for this event.
	ID string `json:"id"`

	// Type is the event type (e.g., "external_input").
	// Reuses tagentevent.TypeExternalInput for external inputs.
	Type string `json:"type"`

	// Source identifies the producer of this event.
	// Values: "user", "tmux", "meditation", "task", "subagent", "inject".
	Source string `json:"source"`

	// Timestamp is when this event was created.
	Timestamp time.Time `json:"timestamp"`

	// Message carries the payload for external_input events.
	// Nil for non-external_input events.
	Message *model.Message `json:"message,omitempty"`

	// Metadata holds extension data (event_key, partition_id, source_session, etc.).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TypeToolUse identifies tool invocation events on the bus.
// Unlike tagentevent.TypeThinkingPlan (which is an event *type* for persistence),
// TypeToolUse is a bus *trigger*: it causes the AgentLoop to dispatch the tool
// asynchronously and continue without blocking.
//
// The LLM's tool_calls are converted to TypeToolUse events on the bus;
// the LLM itself never sees this type in its context.
const TypeToolUse = "tool_use"

// NewExternalInputEvent creates an external_input event with the given source and message payload.
// The message is stored by pointer — callers MUST NOT mutate it after publishing.
func NewExternalInputEvent(source string, msg model.Message) *AgentEvent {
	return &AgentEvent{
		ID:        uuid.NewString(),
		Type:      tagentevent.TypeExternalInput,
		Source:    source,
		Timestamp: time.Now(),
		Message:   &msg,
		Metadata:  make(map[string]any),
	}
}

// SourceTask identifies task_settled events on the bus (a settled background
// task reclaimed into a new turn).
const SourceTask = "task"

// settleInlineTail is the tail excerpt kept inline in a spilled task_settled
// notice (aligned with ActionTool's tail view).
const settleInlineTail = 2000

// settleInlineCapChars is the compile-time inline-result cap for task_settled
// notices (context-efficiency-and-trajectory D2/D3): results at/below this stay
// inline (newlines escaped to ␤ for the single-line trajectory form); larger
// results spill to the tool-output dir with a tail preview. It is a named
// constant, NOT a config knob — the derivation `MaxTokens/2*4` that previously
// fed this path (~256K chars at a 128K budget, an unowned formula accident) is
// removed.
const settleInlineCapChars = 600

// settleDescMaxChars caps the task desc rendered in the single-line form.
const settleDescMaxChars = 60

// settleErrMaxChars caps the error text rendered inline for a failed settle.
const settleErrMaxChars = 200

// settleMarkerAndStatus maps a settle signal to its single-line trajectory
// marker and English status word. Markers: ✓ completed / ✗ failed / ∞
// alive-detached / ⚠ suspect.
func settleMarkerAndStatus(sig task.SettleSignal) (marker, statusWord string) {
	switch {
	case sig.Kind == task.SettleWatch:
		// R2（review 🔴2）：watch 命中是**通知**而非终态——信号恒带哨兵 Err
		//（"%d matches"），若落入下方 Err 判定则记为 failed 终态，RebuildTaskRegistry
		// 按「spawned−终态」折叠后，仍在运行的 watch 型服务任务重启即消失。
		// 记为非终态词汇 "watch"（不在终态集合 {completed,failed,cancelled,dead}，
		// 不抵消 spawned；通知正文照发不变）。
		return "◈", "watch"
	case sig.Kind == task.SettleFailed:
		// hardening-review-batch2 1.6：reconcile 回收（zombie/orphan/wall）的
		// 失败信号——此前无 Err 时落 default=completed，WAL/反馈错记成功。
		return "✗", "failed"
	case sig.Err != nil:
		return "✗", "failed"
	case sig.Kind == task.SettleStable:
		return "∞", "alive-detached"
	case sig.Kind == task.SettleSuspect:
		return "⚠", "suspect"
	case sig.Kind == task.SettleCompleted:
		return "✓", "completed"
	default:
		// hardening-review-batch2 1.6：未知 Kind 显式化——不默认 completed
		//（那会把调用方 bug 写成成功事实）。unknown 不在终态集合，不抵消
		// spawned；告警由调用侧记。
		log.Warnf("[task_settled] unknown settle kind %q — mapped to unknown (not completed)", sig.Kind)
		return "?", "unknown"
	}
}

// escapeNewlines flattens internal newlines to ␤ so a settle notice stays a
// single-line trajectory entry (dense, no blank-line padding).
func escapeNewlines(s string) string {
	return strings.NewReplacer("\r\n", "␤", "\n", "␤", "\r", "␤").Replace(s)
}

// newTaskSettledEvent builds a self-contained external_input event describing a
// background task that has settled, so the persistent loop reclaims it into a
// new turn. The event body is a COMPACT SINGLE-LINE trajectory form
// (context-efficiency-and-trajectory D2): `[task settled] <marker> <desc>
// (id=<short>) <status> → 结果: <inline|spill>` — dense, append-only friendly,
// and information-lossless (task_id / desc / status / error / result-or-spill
// ticket all present; only layout redundancy is dropped). Result bounding keeps
// the event body BOUNDED so recalling it can never re-inject an oversized
// result: results over maxChars spill to a file under outputDir
// (workspace.Cleaner bounds the directory) and the Content carries the path
// ticket + tail preview; consumption goes through read_file paging. Write
// failure degrades to inline full text (availability over bounding).
// maxChars<=0 or empty outputDir disables spillover (tests / small results).
// newBatchRetiredSummaryEvent (resident-remaining-hardening 1.2): ONE
// external_input carrying N per-task settled lines — the 6.7① storm collapse.
// Line format mirrors newTaskSettledEvent's header (retire outputs are short
// machine verdicts; no spill needed). Empty batch returns nil.
func newBatchRetiredSummaryEvent(batch []task.BatchRetired) *AgentEvent {
	if len(batch) == 0 {
		return nil
	}
	var b strings.Builder
	for i, r := range batch {
		marker, statusWord := settleMarkerAndStatus(r.Sig)
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&b, "[task settled] %s %s (id=%s) %s",
			marker, truncateRunes(r.Task.Spec.Desc, settleDescMaxChars), task.ShortID(r.Task.ID), statusWord)
		if r.Sig.Err != nil {
			fmt.Fprintf(&b, " 错误: %s", truncateRunes(r.Sig.Err.Error(), settleErrMaxChars))
		}
		if out := r.Sig.Output; out != "" {
			fmt.Fprintf(&b, " → 结果: %s", escapeNewlines(out))
		}
	}
	msg := model.Message{Role: model.RoleUser, Content: b.String()}
	evt := NewExternalInputEvent("task-batch-retire", msg)
	return evt
}

func newTaskSettledEvent(tk *task.Task, sig task.SettleSignal, maxChars int, outputDir string) *AgentEvent {
	marker, statusWord := settleMarkerAndStatus(sig)

	result := sig.Output
	spillPath := ""
	if maxChars > 0 && outputDir != "" && len(result) > maxChars {
		shortID := tk.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		path := filepath.Join(outputDir, fmt.Sprintf("task-%s-%d.txt", shortID, time.Now().UnixMilli()))
		if err := os.MkdirAll(outputDir, 0o755); err == nil {
			if err := os.WriteFile(path, []byte(result), 0o644); err == nil {
				log.Infof("[task_settled] result %d chars > %d limit, spilled to %s", len(result), maxChars, path)
				tail := result
				if len(result) > settleInlineTail {
					tail = result[len(result)-settleInlineTail:]
				}
				spillPath = path
				result = fmt.Sprintf("output_spilled 结果 %d 字符已保存到: %s（可用 read_file 配合 start_line/num_lines 分段读取）；尾部: %s",
					len(sig.Output), path, escapeNewlines(tail))
			} else {
				log.Warnf("[task_settled] spill write to %s failed (%v), falling back to inline full text", path, err)
			}
		} else {
			log.Warnf("[task_settled] spill dir %s ensure failed, falling back to inline full text", outputDir)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[task settled] %s %s (id=%s) %s",
		marker, truncateRunes(tk.Spec.Desc, settleDescMaxChars), task.ShortID(tk.ID), statusWord)
	if sig.Err != nil {
		fmt.Fprintf(&b, " 错误: %s", truncateRunes(sig.Err.Error(), settleErrMaxChars))
	}
	if result != "" {
		if spillPath != "" {
			// result already carries the spill ticket + escaped tail
			fmt.Fprintf(&b, " → %s", result)
		} else {
			fmt.Fprintf(&b, " → 结果: %s", escapeNewlines(result))
		}
	}
	evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: b.String()})
	// Carry the originating turn's opaque routing baggage (chat_id, ...) captured
	// at spawn time, so the reclaim turn's output can be delivered back to the
	// originating session. Reuses the existing extractRootMetadata → meta_*
	// pipeline. (async-result-delivery.)
	for k, v := range tk.Spec.Origin {
		evt.Metadata[k] = v
	}
	// hardening-review-batch2 1.3（unknown 保守扣留）：Origin 缺失（旧版本记录
	// /框架外 spawn）的任务，其结算事件无世系——下游 extractTriggerSource 会
	// 机械兜底为 "task" 并被宿主白名单放投。源头打标，让下游显式降级为
	// task-unstamped（宿主扣留），未知不得升级为可投递来源。
	if len(tk.Spec.Origin) == 0 {
		evt.Metadata["lineage_absent"] = "true"
	}
	// hardening-review-batch2 2.4：alive-detached 转变时刻随事件持久化——
	// 恢复侧据它还原 detachedAt（沿用真实脱离时长，不以恢复时间替代）。
	// cold-eyes P1-2：stale 一次性通知（Watch）同样携带——否则 watch 的
	// settle_status 会覆盖 alive-detached 成为末次记录，恢复侧 detachedAt
	// 与 detached 语义双双丢失。
	if sig.Kind == task.SettleStable || sig.Kind == task.SettleWatch {
		if ms := tk.DetachedAtMilli(); ms > 0 {
			evt.Metadata["detached_at_ms"] = fmt.Sprintf("%d", ms)
		}
	}
	// 2.3（design-report-closeout）：结构化 settle 状态随事件携带——persistBusEvent
	// 落库后据此自动写 task_settle feedback（completed→positive / failed→negative；
	// suspect/alive-detached 不写，只记确定性裁决）。
	evt.Metadata["settle_status"] = statusWord
	// R2（resident-continuity-r2-r4）：全量 task_id（UUID）随事件携带——事实链
	// settle 记录的结构化关联键（RebuildTaskRegistry 按此归并 spawned/settled，
	// 不解析正文；ShortID 仅人类可读）。
	evt.Metadata["task_id"] = tk.ID
	return evt
}

// ---------------------------------------------------------------------------
// EventBus
// ---------------------------------------------------------------------------

// EventBus is a per-agent ordered event queue.
//
// Producers (InjectMessage, TmuxMonitor, MeditationManager, sub-agent callbacks,
// and the AgentLoop itself) call Publish to enqueue events.
//
// The AgentLoop is the sole consumer: it calls Pull to block until at least one
// event arrives, then non-blocking drains all remaining pending events.
//
// Design rationale: a single consumer (AgentLoop) means no fan-out races,
// no ordering guarantees across consumers, and simple backpressure (channel
// fills up → Publish blocks).
//
// Durable mode (resident-readiness-plan 3.2): with an Inbox configured, ALL
// inbound events are persisted to inbox-v1 BEFORE the durable receipt — the
// channel carries only wake-ups, never the durable truth. Durable envelopes
// are consumed strictly in enqueue order (zero-padded seq); volatile channel
// events are best-effort by definition. Receipted items replaying after a
// crash are Ack-skipped without re-execution.
type EventBus struct {
	ch chan *AgentEvent

	// inbox 是可选的 durable 输入信箱（inbox-v1）。nil = 纯 channel 轻量模式。
	inbox *reliability.Inbox

	// publishDropped counts events the LEGACY void Publish could not accept
	// (timeout/closed) — the void entry never fails loudly by contract, but
	// the rejection must stay observable (3.1).
	publishDropped atomic.Int64
}

// PublishReceipt is the decidable result of a context-aware acceptance (3.1).
type PublishReceipt struct {
	RequestID string // stable identity of THIS acceptance (uuid of the event)
	Durable   bool   // true = persisted through the inbox barrier
}

// Publish errors — callers of PublishContext can branch on these; the void
// Publish only logs and counts them.
var (
	ErrPublishTimeout = fmt.Errorf("eventbus: publish timed out (queue full)")
	ErrNilEvent       = fmt.Errorf("eventbus: nil event")
	ErrBusClosed      = fmt.Errorf("eventbus: bus closed or not accepting")
)

// NewEventBus creates an EventBus backed by a buffered channel (cap=256,
// matching the historical mailbox size).
func NewEventBus() *EventBus {
	return &EventBus{
		ch: make(chan *AgentEvent, 256),
	}
}

// NewReliableEventBus opens the durable inbox under spillDir/inbox-v1
// (resident-readiness-plan 3.2). Legacy *.spill leftovers REFUSE the upgrade
// (fail-loud with migration guidance) — the previous binary must drain them.
// All errors are returned: reliability requested by config must never
// silently degrade to volatile.
func NewReliableEventBus(spillDir string) (*EventBus, error) {
	b := NewEventBus()
	if spillDir == "" {
		return b, nil
	}
	inbox, err := reliability.NewInbox(spillDir, 0)
	if err != nil {
		return nil, err
	}
	b.inbox = inbox
	log.Infof("[ReliableBus] durable inbox enabled at %s (all inputs persisted before receipt; pending=%d)", inbox.Dir(), inbox.Pending())
	return b, nil
}

// CloseDurable releases the inbox (unconfirmed items stay on disk for the
// next process). Called from the agent shutdown path.
func (b *EventBus) CloseDurable() error {
	if b == nil || b.inbox == nil {
		return nil
	}
	return b.inbox.Close()
}

// DurablePending returns the unconfirmed durable envelope count (diagnostics).
func (b *EventBus) DurablePending() int64 {
	if b == nil || b.inbox == nil {
		return 0
	}
	return b.inbox.Pending()
}

// PublishDropped counts rejections made through the legacy void entry (3.1).
func (b *EventBus) PublishDropped() int64 {
	if b == nil {
		return 0
	}
	return b.publishDropped.Load()
}

// PublishContext is the decidable acceptance entry (3.1): it returns a
// receipt on success (volatile or durable) or an error — full/timeout/closed
// are NEVER reported as accepted. The legacy void Publish wraps this.
func (b *EventBus) PublishContext(ctx context.Context, event *AgentEvent) (PublishReceipt, error) {
	if event == nil {
		return PublishReceipt{}, ErrNilEvent
	}
	receipt := PublishReceipt{RequestID: event.ID}

	// Durable mode: EVERY event goes through the inbox barrier first.
	if b.inbox != nil {
		env := reliability.Envelope{
			RequestID: event.ID,
			Source:    event.Source,
			Messages:  []reliability.EnvelopeMessage{{Role: eventRole(event), Content: eventContent(event)}},
		}
		if _, err := b.inbox.Enqueue(&env); err != nil {
			return receipt, fmt.Errorf("eventbus: durable enqueue rejected: %w", err)
		}
		receipt.Durable = true
		// Wake the consumer with a DEDICATED sentinel (never the event itself
		// — the event lives in the inbox and must not ALSO travel the channel,
		// or consumers would see it twice). Full channel: harmless, the next
		// Pull drains the inbox regardless.
		select {
		case b.ch <- wakeEvent():
		default:
		}
		return receipt, nil
	}

	// Volatile mode: bounded channel + timeout, then an explicit error.
	select {
	case b.ch <- event:
		return receipt, nil
	default:
	}
	select {
	case b.ch <- event:
		return receipt, nil
	case <-ctx.Done():
		b.publishDropped.Add(1)
		return receipt, fmt.Errorf("%w: %v", ErrPublishTimeout, ctx.Err())
	case <-time.After(publishTimeout):
		b.publishDropped.Add(1)
		return receipt, ErrPublishTimeout
	}
}

// inboxWakeType marks a channel wake-up sentinel for durable items — it is
// filtered out of every batch and never becomes a turn input.
const inboxWakeType = "inbox_wake"

func wakeEvent() *AgentEvent {
	return &AgentEvent{Type: inboxWakeType, ID: uuid.NewString(), Timestamp: time.Now()}
}

func isInboxWake(e *AgentEvent) bool { return e != nil && e.Type == inboxWakeType }

// eventContent extracts the payload text of a bus event for envelope storage.
// eventRole extracts the message role carried by an AgentEvent (cold-eyes R2
// Minor 5): system-type events (EmitSystemAlert) must survive the durable
// round-trip with their role intact — an empty role defaults to user.
func eventRole(event *AgentEvent) string {
	if event.Message != nil && event.Message.Role != "" {
		return string(event.Message.Role)
	}
	return string(model.RoleUser)
}

func eventContent(event *AgentEvent) string {
	if event.Message != nil {
		return event.Message.Content
	}
	return ""
}

// PublishEnvelopeContext accepts a WHOLE batch as ONE durable envelope
// (resident-readiness-plan 3.3/5.2): every message keeps its own identity in
// the envelope, and the batch is durable (or rejected) as a unit — never
// partially accepted. Volatile mode falls back to per-message PublishContext.
func (b *EventBus) PublishEnvelopeContext(ctx context.Context, source string, msgs []model.Message) (PublishReceipt, error) {
	if len(msgs) == 0 {
		return PublishReceipt{}, ErrNilEvent
	}
	requestID := uuid.NewString()
	receipt := PublishReceipt{RequestID: requestID}

	if b.inbox != nil {
		env := reliability.Envelope{RequestID: requestID, Source: source}
		for _, m := range msgs {
			env.Messages = append(env.Messages, reliability.EnvelopeMessage{Role: string(m.Role), Content: m.Content})
		}
		if _, err := b.inbox.Enqueue(&env); err != nil {
			return receipt, fmt.Errorf("eventbus: durable enqueue rejected: %w", err)
		}
		receipt.Durable = true
		select {
		case b.ch <- wakeEvent():
		default:
		}
		return receipt, nil
	}
	// Volatile: per-message acceptance; first failure rejects the batch.
	for _, m := range msgs {
		evt := NewExternalInputEvent(source, m)
		select {
		case b.ch <- evt:
		default:
			select {
			case b.ch <- evt:
			case <-ctx.Done():
				b.publishDropped.Add(1)
				return receipt, fmt.Errorf("%w: %v", ErrPublishTimeout, ctx.Err())
			case <-time.After(publishTimeout):
				b.publishDropped.Add(1)
				return receipt, ErrPublishTimeout
			}
		}
	}
	return receipt, nil
}

// Publish enqueues an event (legacy void entry, 3.1 compatibility): it wraps
// PublishContext, logging and counting rejections instead of failing. New
// callers — HTTP, hosts, anything that reports acceptance to a user — MUST
// use PublishContext/InjectMessageContext.
func (b *EventBus) Publish(event *AgentEvent) {
	if event == nil {
		log.Warnf("[EventBus] Publish nil event, skipped")
		return
	}
	if _, err := b.PublishContext(context.Background(), event); err != nil {
		// PublishContext already counted the rejection — log only.
		log.Warnf("[EventBus] Publish rejected: %v (type=%s source=%s)", err, event.Type, event.Source)
	}
}

// publishTimeout is the maximum time Publish will wait before dropping
// an event when the channel is full. This prevents permanent blocking
// if the AgentLoop goroutine has exited unexpectedly.
const publishTimeout = 5 * time.Second

// maxClaimPerPull 是单次 Pull/TryPull 从 inbox claim 的 envelope 上限：防一次
// 取出全部积压拼成巨型 LLM 消息；剩余按 seq 留待下次（语义无损，顺序不变）。
const maxClaimPerPull = 32

// claimDurable takes the OLDEST pending durable envelopes (strict enqueue
// order), converting each to in-batch AgentEvents tagged with the inbox
// provenance (inbox_path / inbox_request_id in Metadata) so the consumer can
// Receipt+Ack the envelope after the turn. Receipted items replaying after a
// crash are Ack-skipped WITHOUT re-execution (已确认 receipt 不重复处理).
// A claim is NOT a deletion: a crash after claim replays the envelope.
func (b *EventBus) claimDurable() []*AgentEvent {
	if b.inbox == nil {
		return nil
	}
	var batch []*AgentEvent
	for i := 0; i < maxClaimPerPull; i++ {
		env, path, err := b.inbox.ClaimNext()
		if err != nil {
			log.Warnf("[ReliableBus] inbox claim failed (kept for retry): %v", err)
			return batch
		}
		if env == nil {
			return batch
		}
		if env.State == reliability.InboxStateReceipted {
			// 处理完成 receipt 已持久：只补 Ack，不重执行（2.4 幂等）。
			if aerr := b.inbox.Ack(path); aerr != nil {
				log.Warnf("[ReliableBus] ack of receipted %s deferred: %v", env.RequestID, aerr)
			}
			continue
		}
		for i2, m := range env.Messages {
			// cold-eyes R2 Minor 5: honor the persisted role — system-type
			// events (EmitSystemAlert) must not come back as user messages.
			role := model.Role(m.Role)
			if role == "" {
				role = model.RoleUser
			}
			evt := NewExternalInputEvent(env.Source, model.Message{Role: role, Content: m.Content})
			if evt.Metadata == nil {
				evt.Metadata = make(map[string]any)
			}
			evt.Metadata["inbox_path"] = path
			evt.Metadata["inbox_request_id"] = env.RequestID
			if len(env.EventKeys) > 0 && i2 < len(env.EventKeys) {
				// 已提交事实键回写（3.4）：重放的消费者据此跳过重复入库，
				// 投影按同 key 幂等（满足「原始事件与投影不重复追加」）。
				// cold-eyes Major 3 修复：按消息序号分配**单条** key——joined
				// 复数形式会让第 2..n 条消息全部命中第 1 条的 key 而被误判
				// alreadyStored，事实静默丢失；EventKeys[i] 与 Messages[i] 序号对齐。
				evt.Metadata["inbox_dedup_key"] = env.EventKeys[i2]
			}
			batch = append(batch, evt)
		}
	}
	return batch
}

// ReconcileReceipted (cold-eyes Major 2): converge envelopes whose fact-chain
// receipt event survived a crash but whose ack did not — claimed envelopes are
// receipted+acked WITHOUT re-execution. Returns the number converged.
func (b *EventBus) ReconcileReceipted(rids []string) int {
	if b == nil || b.inbox == nil {
		return 0
	}
	n := 0
	for _, rid := range rids {
		if b.inbox.ConfirmDurableByRequestID(rid) {
			n++
			log.Infof("[ReliableBus] receipted %s reconciled from fact-chain receipt (no re-execution)", rid)
		}
	}
	return n
}

// DurableProvenance returns deduplicated (path, requestID) pairs for every
// durable envelope consumed by a finished turn.
func (b *EventBus) DurableProvenance(events []*AgentEvent) [][2]string {
	if b.inbox == nil {
		return nil
	}
	var out [][2]string
	seen := map[string]bool{}
	for _, evt := range events {
		if evt == nil {
			continue
		}
		path, _ := evt.Metadata["inbox_path"].(string)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		rid, _ := evt.Metadata["inbox_request_id"].(string)
		out = append(out, [2]string{path, rid})
	}
	return out
}

// AppendDurableEventKeys writes committed fact keys back onto the envelope
// (idempotent-replay dedup evidence; 3.4). Errors are the caller's to log —
// a failed writeback degrades replay to at-least-once re-execution.
func (b *EventBus) AppendDurableEventKeys(path string, keys []string) {
	if b.inbox == nil || len(keys) == 0 {
		return
	}
	if err := b.inbox.RecordEventKeys(path, keys); err != nil {
		log.Warnf("[ReliableBus] event-keys writeback failed for %s (replay may re-execute): %v", path, err)
	}
}

// ConfirmDurable records the processing receipt then Acks. Failure leaves
// the claim on disk for replay (at-least-once).
func (b *EventBus) ConfirmDurable(path string) error {
	if b.inbox == nil {
		return nil
	}
	if err := b.inbox.RecordReceipt(path, "turn finished"); err != nil {
		return err
	}
	return b.inbox.Ack(path)
}

// Pull blocks until at least one event arrives or ctx is cancelled.
// Then non-blocking drains all remaining pending events.
// Returns the batch and nil error on success.
// Returns nil and ctx.Err() when ctx is cancelled before any event arrives.
func (b *EventBus) Pull(ctx context.Context) ([]*AgentEvent, error) {
	// 回收顺序：channel 内 volatile 事件先（尽力而为），随后按 seq 严格序
	// claim durable envelope。批量上限防巨型 LLM 消息。
	for {
		batch := b.drainChannelNonWake()
		batch = append(batch, b.claimDurable()...)
		if len(batch) > 0 {
			return batch, nil
		}
		// 皆空：阻塞等 channel 首个事件（volatile 输入或 durable 唤醒哨兵）。
		select {
		case evt := <-b.ch:
			if !isInboxWake(evt) {
				batch = append(batch, evt)
			}
			batch = append(batch, b.drainChannelNonWake()...)
			batch = append(batch, b.claimDurable()...)
			if len(batch) > 0 {
				return batch, nil
			}
			continue // 仅哨兵：重新阻塞等待
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// drainChannelNonWake drains volatile events, dropping wake sentinels.
func (b *EventBus) drainChannelNonWake() []*AgentEvent {
	var batch []*AgentEvent
	for {
		select {
		case evt := <-b.ch:
			if isInboxWake(evt) {
				continue
			}
			if evt != nil {
				batch = append(batch, evt)
			}
		default:
			return batch
		}
	}
}

// drainChannel 非阻塞排空 channel 现有事件。
func (b *EventBus) drainChannel() []*AgentEvent {
	var batch []*AgentEvent
	for {
		select {
		case evt := <-b.ch:
			if evt != nil {
				batch = append(batch, evt)
			}
		default:
			return batch
		}
	}
}

// TryPull non-blocking reads all pending events without waiting.
// Returns an empty (non-nil) slice if no events are pending.
// Unlike Pull, this does not block — it immediately returns if the channel is empty.
func (b *EventBus) TryPull() []*AgentEvent {
	// 回收顺序与 Pull 一致：channel volatile 先，durable 按 seq 严格序。
	batch := b.drainChannelNonWake() // cold-eyes Minor 1: wake sentinels never leak into pulls
	batch = append(batch, b.claimDurable()...)
	if batch == nil {
		batch = []*AgentEvent{}
	}
	return batch
}
