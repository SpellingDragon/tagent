package agent

import (
	"context"
	"encoding/json"
	"errors"
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
// "tool_use bus trigger" abstraction had no producer and no consumer and is
// gone as ghost code (implementation-hardening 4.1; resident-remaining-
// hardening 4.4 removed the dangling const its own note had already declared
// deleted).
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

	// claim is the runtime reference to the durable inbox envelope a claimed
	// event came from (fix-resident-reliability-boundaries D2/3.3). It is NOT
	// serialized (json:"-"), never travels in business Metadata/Origin/model
	// context, and is set only by claimDurable on the consumer side. Nil for
	// volatile events and for events not yet claimed.
	claim *durableClaim
}

// durableClaim is the typed, non-JSON provenance of one claimed inbox message
// slot. It replaces the v1 practice of stuffing inbox_path/inbox_request_id/
// inbox_dedup_key into AgentEvent.Metadata (F1/F4 root cause: those control
// keys leaked into Origin baggage, the model context and host delivery fields).
// PreparedFact carries the canonical FullEvent JSON frozen on the FIRST claim
// (D2 write-before barrier) so a replay reuses the exact key and never
// re-stamps time/attribution/summary.
type durableClaim struct {
	Path         string          // inbox envelope file path (durable location)
	RequestID    string          // envelope request id (batch identity)
	Slot         int             // fixed message slot within the envelope
	ReceiptKey   string          // reserved receipt EventKey (hex); "" until prepared
	PreparedFact json.RawMessage // frozen canonical FullEvent for THIS slot; "" until prepared
}

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
// Durable mode (resident-readiness-plan 3.2; lossless under D2): with an Inbox
// configured, ALL inbound events are persisted to inbox-v2 BEFORE the durable
// receipt — the channel carries only wake-ups, never the durable truth. Each
// message slot keeps a lossless JSON snapshot of the original AgentEvent
// (ID/Type/Source/Timestamp/full Message/business Metadata), so a restart
// restores everything inbox-v1 dropped (F1). Durable envelopes are consumed
// strictly in enqueue order (zero-padded seq); volatile channel events are
// best-effort by definition. Receipted items replaying after a crash are
// Ack-skipped without re-execution.
type EventBus struct {
	ch chan *AgentEvent

	// inbox 是可选的 durable 输入信箱（inbox-v2）。nil = 纯 channel 轻量模式。
	inbox *reliability.Inbox

	// retention 是可选的 §2.8 材料保留能力（memory.RetentionGuard 的结构性投影，
	// 避免此处 import memory）。durable inbox 下由 agent 装配注入，用于从现有未确认
	// envelope 重建保留租约并在 ack 后释放。nil = 未接线（不 arm、不释放）。
	retention retentionGuard

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

// NewReliableEventBus opens the durable inbox under spillDir/inbox-v2
// (resident-readiness-plan 3.2). Legacy *.spill leftovers or an undrained
// inbox-v1 tree REFUSE the upgrade (fail-loud with migration guidance) — the
// previous binary must drain them; v2 never guess-migrates (task 3.5).
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

// Durable reports whether the bus was configured with a durable inbox. The
// agent constructor uses this to enforce that reliable mode never runs against
// a store lacking explicit replay capability (task 3.5): a durable inbox whose
// facts cannot be idempotently replayed would silently degrade durability.
func (b *EventBus) Durable() bool {
	return b != nil && b.inbox != nil
}

// retentionGuard is the §2.8 材料保留 capability surface the bus needs from the store
// (structurally mirrors memory.RetentionGuard to avoid importing memory here). The
// recovery owner protects the durable originals of unacked envelopes until they are
// acked (dir-synced) and released, so TTL/capacity/compaction cannot destroy material
// a restart still needs.
type retentionGuard interface {
	ProtectKey(key int64)
	ReleaseKey(key int64)
	ArmRetention()
	// BeginHold/EndHold raise/release the §5.8 registration barrier (structurally
	// mirrors memory.RetentionGuard): forgetting pauses while an owner inventories.
	BeginHold()
	EndHold()
}

// SetRetentionGuard injects the store's retention guard (durable inbox only). Must be
// called before ArmRetentionFromInbox; a nil guard disables protect/release.
func (b *EventBus) SetRetentionGuard(g retentionGuard) {
	if b == nil {
		return
	}
	b.retention = g
}

// materialKeys converts envelope material to store EventKeys: the prepared fact keys
// plus the reserved receipt key (hex → int64). Only successfully-derived keys return.
func materialKeys(m reliability.UnackedMaterial) []int64 {
	keys := make([]int64, 0, len(m.FactKeys)+1)
	keys = append(keys, m.FactKeys...)
	if m.ReceiptKey != "" {
		if rk, err := tagentevent.ParseEventKey(m.ReceiptKey); err == nil && rk != 0 {
			keys = append(keys, rk)
		}
	}
	return keys
}

// ArmRetentionFromInbox rebuilds the store's retention lease from existing unacked
// envelopes (§2.8 restart recovery) and then arms it, releasing the lifecycle scanner's
// first destructive pass. Called once at agent-open after SetRetentionGuard. A durable
// inbox with no material still arms (empty protect) so the scanner is not gated on an
// inbox that never registers. A failed enumeration does NOT arm (never under-protect on
// a partial view); the lifecycle startup grace is the anti-starvation backstop.
func (b *EventBus) ArmRetentionFromInbox() error {
	if b == nil || b.inbox == nil || b.retention == nil {
		return nil
	}
	// §5.8: raise the registration barrier BEFORE touching the dir — on an already
	// running store (late attach) or a fresh one, no forgetting pass may land
	// between "inventory started" and "keys protected".
	b.retention.BeginHold()
	mats, err := b.inbox.UnackedMaterial()
	if err != nil {
		// The dir cannot be inventoried: HOLD the barrier (显式阻断). Forgetting
		// stays gated indefinitely — surviving material must not be destroyed on an
		// incomplete view. The caller refuses ingest (agent build fails), so the
		// block is reported, never silently swept.
		log.Errorf("[ReliableBus] recovery inventory FAILED — retention barrier HELD, forgetting stays blocked: %v", err)
		return err
	}
	for _, m := range mats {
		for _, k := range materialKeys(m) {
			b.retention.ProtectKey(k)
		}
	}
	b.retention.EndHold()
	b.retention.ArmRetention()
	log.Infof("[ReliableBus] retention lease armed from inbox: %d unacked envelope(s) protected", len(mats))
	return nil
}

// releaseRetention drops the lease holders for an acked envelope's originals so they
// resume normal age-based handling. The caller MUST read the material BEFORE Ack (which
// removes the file) and pass it here; releasing after dir-sync is the §2.8 release point.
func (b *EventBus) releaseRetention(m reliability.UnackedMaterial) {
	if b == nil || b.retention == nil {
		return
	}
	for _, k := range materialKeys(m) {
		b.retention.ReleaseKey(k)
	}
}

// DrainRetentionCleanups finalizes deferred ack-cleanup barriers (§3.6/L96): for
// every envelope whose unlink landed but whose dir-sync previously failed, it
// completes the outstanding barrier and, once durable, releases the retention lease
// for its protected material — exactly once (L110). Driven from the consume loop
// each turn so an uncertain ack converges to a single capacity+lease release
// without re-executing the input. Returns the number of accounts finalized.
func (b *EventBus) DrainRetentionCleanups() int {
	if b == nil || b.inbox == nil {
		return 0
	}
	mats := b.inbox.DrainCleanups()
	for _, m := range mats {
		b.releaseRetention(m)
	}
	return len(mats)
}

// TransitionalData reports previous-format (inbox-v1 / .spill) items the inbox found
// at open (§3.7). They are inert — never read or consumed — and the agent bootstraps
// may surface them to the operator. Safe to call on a nil/volatile bus (returns
// nils). A managed reset (ResetTransitional) is the only thing that clears them.
func (b *EventBus) TransitionalData() (spill, v1 []string) {
	if b == nil || b.inbox == nil {
		return nil, nil
	}
	return b.inbox.TransitionalData()
}

// ResetTransitional is the operator-invoked managed reset of previous-format data
// for this bus's inbox (§3.7). It is destructive and requires an explicit confirm;
// it never runs automatically and never clears current-format corruption (which
// must surface, not be wiped). See reliability.Inbox.ResetTransitional.
func (b *EventBus) ResetTransitional(confirm bool) (int, error) {
	if b == nil || b.inbox == nil {
		return 0, nil
	}
	return b.inbox.ResetTransitional(confirm)
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

	// Durable mode: EVERY event goes through the inbox barrier first, stored as
	// a lossless JSON snapshot of the whole AgentEvent (3.2). An event that
	// cannot be JSON-encoded is REFUSED at acceptance — never silently stripped
	// of the offending field. After the snapshot is written the caller may keep
	// mutating the in-memory event without affecting the durable payload.
	if b.inbox != nil {
		src, merr := json.Marshal(event)
		if merr != nil {
			return receipt, fmt.Errorf("eventbus: event not JSON-encodable, durable acceptance refused: %w", merr)
		}
		env := reliability.Envelope{
			RequestID: event.ID,
			Source:    event.Source,
			Messages:  []reliability.MessageSlot{{SourceEvent: src}},
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
		// Build every slot's lossless snapshot FIRST: an unencodable message
		// rejects the whole batch before anything is written (never a partially
		// accepted envelope). Each message becomes its own AgentEvent so its
		// per-message identity (id/timestamp) survives the round-trip.
		env := reliability.Envelope{RequestID: requestID, Source: source}
		for _, m := range msgs {
			evt := NewExternalInputEvent(source, m)
			src, merr := json.Marshal(evt)
			if merr != nil {
				return receipt, fmt.Errorf("eventbus: message not JSON-encodable, durable acceptance refused: %w", merr)
			}
			env.Messages = append(env.Messages, reliability.MessageSlot{SourceEvent: src})
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
// order), restoring each message slot from its lossless source_event snapshot
// and tagging the reconstructed AgentEvent with a typed durableClaim (path,
// request id, slot, reserved receipt_key, any already-frozen prepared_fact).
// The claim is a non-JSON field — no inbox_path/inbox_dedup_key control keys
// leak into business Metadata (F1/F4). Receipted items replaying after a crash
// are Ack-skipped WITHOUT re-execution. A claim is NOT a deletion: a crash
// after claim replays the envelope.
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
			m := reliability.MaterialOf(env) // §2.8: capture originals before Ack removes the file
			if aerr := b.inbox.Ack(path); aerr != nil {
				log.Warnf("[ReliableBus] ack of receipted %s deferred: %v", env.RequestID, aerr)
			} else {
				b.releaseRetention(m) // Ack dir-synced → release the lease holders (§2.8)
			}
			continue
		}
		for _, m := range env.Messages {
			evt, derr := decodeSourceEvent(m.SourceEvent)
			if derr != nil {
				// The leaf already quarantines unreadable envelopes; a slot
				// that still fails here is an anomaly. Keep the envelope
				// claimed (never silently ack a slot we could not restore) and
				// skip this slot so a re-claim retries.
				log.Errorf("[ReliableBus] undecodable source_event at %s slot %d (kept for retry): %v", path, m.Slot, derr)
				continue
			}
			evt.claim = &durableClaim{
				Path:         path,
				RequestID:    env.RequestID,
				Slot:         m.Slot,
				ReceiptKey:   env.ReceiptKey,
				PreparedFact: m.PreparedFact,
			}
			batch = append(batch, evt)
		}
	}
	return batch
}

// decodeSourceEvent restores the lossless AgentEvent snapshot persisted at
// acceptance. ID/Type/Source/Timestamp/full Message/business Metadata all come
// back intact (F1) — nothing is re-stamped or dropped. The restored event's
// claim is nil (json:"-"); the caller attaches the typed claim.
func decodeSourceEvent(raw json.RawMessage) (*AgentEvent, error) {
	var evt AgentEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		return nil, err
	}
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}
	return &evt, nil
}

// DurableProvenance returns deduplicated (path, requestID) pairs for every
// durable envelope consumed by a finished turn, read from the typed claim.
func (b *EventBus) DurableProvenance(events []*AgentEvent) [][2]string {
	if b.inbox == nil {
		return nil
	}
	var out [][2]string
	seen := map[string]bool{}
	for _, evt := range events {
		if evt == nil || evt.claim == nil {
			continue
		}
		path := evt.claim.Path
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, [2]string{path, evt.claim.RequestID})
	}
	return out
}

// PrepareEnvelope durably freezes the per-slot prepared facts and a reserved
// receipt_key onto the claimed envelope at path, BEFORE the caller writes any
// fact (D2 write-before barrier, task 3.4). facts are slot-aligned; a nil
// entry keeps that slot's already-frozen fact (replay partial-prepare). An
// already-durable receipt_key must match (a different one is a conflict). The
// caller MUST treat a returned error as "write nothing this turn" — the claim
// stays and replays. This replaces v1's post-hoc AppendDurableEventKeys/
// RecordEventKeys writeback (F4).
func (b *EventBus) PrepareEnvelope(path, receiptKey string, facts []json.RawMessage) error {
	if b.inbox == nil {
		return errors.New("eventbus: no durable inbox configured")
	}
	if path == "" {
		return errors.New("eventbus: prepare requires an envelope path")
	}
	return b.inbox.PrepareFacts(path, receiptKey, facts)
}

// RecordCompletion durably freezes a turn's completion payload onto the envelope
// at path BEFORE its receipt is submitted (§5.3, D3 step 7). Idempotent on an
// identical payload; a differing payload on an already-frozen envelope is a
// conflict (reliability.ErrCompletionConflict) — the frozen completion is
// authoritative and the caller must NOT overwrite it. The caller treats an error
// as "receipt not yet safe": the claim stays and the completion write is retried
// in-process without re-running the model (design 决策5 L62).
func (b *EventBus) RecordCompletion(path string, completion json.RawMessage) error {
	if b.inbox == nil {
		return errors.New("eventbus: no durable inbox configured")
	}
	if path == "" {
		return errors.New("eventbus: completion requires an envelope path")
	}
	return b.inbox.RecordCompletion(path, completion)
}

// QuarantineEnvelope isolates a deterministic-conflict envelope (kept on disk for
// inspection, capacity freed). See Inbox.QuarantineEnvelope (§4.2).
//
// §2.8 (resident-review-fixes 2.2): quarantine is a terminal disposition just like
// Ack, so it MUST release the envelope's retention holders — otherwise an isolated
// envelope's originals stay leased forever and can never be TTL/capacity-evicted
// (a lease hang). The leaf has no store handle, so the wrapper reads the material
// BEFORE the move (the rename relocates the file) and releases it after a confirmed
// isolation. releaseRetention is nil-safe; an envelope that was never armed releases
// nothing. The rename's atomicity is the dir barrier — no separate cleanup is owed.
func (b *EventBus) QuarantineEnvelope(path, reason string) {
	if b == nil || b.inbox == nil {
		return
	}
	m, ok, merr := b.inbox.MaterialOfPath(path)
	if merr != nil {
		log.Warnf("[ReliableBus] quarantine retention material read %s failed: %v", path, merr)
	}
	if b.inbox.QuarantineEnvelope(path, reason) && ok {
		b.releaseRetention(m) // isolated (terminal) → release (§2.8), symmetric with the Ack path
	}
}

// ReleaseClaim returns a claimed envelope to pending for ordered re-claim on a
// transient submit failure (§4.2). See Inbox.ReleaseClaim.
func (b *EventBus) ReleaseClaim(path string) error {
	if b == nil || b.inbox == nil {
		return nil
	}
	return b.inbox.ReleaseClaim(path)
}

// ConfirmDurable records the processing receipt then Acks. §5.4: the caller
// must present the verified receipt credential issued from a legal durable
// completion (ContextManager.verifyReceiptCredential) — RecordReceipt refuses
// without it, so no bare description string or request id can confirm an
// envelope. Failure leaves the claim on disk for replay (at-least-once).
func (b *EventBus) ConfirmDurable(path string, cred reliability.ReceiptCredential) error {
	if b.inbox == nil {
		return nil
	}
	// §2.8: read the envelope's originals BEFORE Ack removes the file, so the lease
	// holders can be released once the ack is dir-synced (the release point).
	m, ok, merr := b.inbox.MaterialOfPath(path)
	if merr != nil {
		log.Warnf("[ReliableBus] retention material read %s failed: %v", path, merr)
	}
	if err := b.inbox.RecordReceipt(path, cred); err != nil {
		return err
	}
	if err := b.inbox.Ack(path); err != nil {
		return err
	}
	if ok {
		b.releaseRetention(m) // Ack dir-synced → release (§2.8)
	}
	return nil
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
