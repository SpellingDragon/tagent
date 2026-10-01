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

// AgentEvent is the unified event type for the agent persistent event bus
// (the mailbox between turns). Every event flowing through the bus is an
// AgentEvent, and exactly one type serves as a bus trigger: TypeExternalInput
// (user, tmux, meditation, task settle). agent_output does not enter the bus —
// it is emitted straight to outputCh.
//
// Scope: the bus coordinates turns. The tool loop inside a turn remains the
// upstream synchronous ReAct (runner.Run), so no tool-use event is ever a bus
// trigger.
// 契约: docs/wiki/agent/event-flow.md#event-stream-overview
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
	// event came from. It is NOT
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
	Path         string
	RequestID    string
	Slot         int
	ReceiptKey   string
	PreparedFact json.RawMessage
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

// settleInlineCapChars 是 task_settled 通知的编译期内联结果上限：不超过它的原文内联
// （换行转义为 ␤ 以保持单行轨迹形态），超过的溢出到工具输出目录并留尾预览。
// 它是命名常量、不是配置项：内联上限与 token 预算解耦——按 `MaxTokens/2*4` 派生会在
// 128K 预算下给出约 256K 字符，那是无人负责的公式后果。
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
		return "◈", "watch"
	case sig.Kind == task.SettleFailed:
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
		log.Warnf("[task_settled] unknown settle kind %q — mapped to unknown (not completed)", sig.Kind)
		return "?", "unknown"
	}
}

// escapeNewlines flattens internal newlines to ␤ so a settle notice stays a
// single-line trajectory entry (dense, no blank-line padding).
func escapeNewlines(s string) string {
	return strings.NewReplacer("\r\n", "␤", "\n", "␤", "\r", "␤").Replace(s)
}

// formatExitCode renders a settle signal's exit code for the notification text
// (failure-polarity passthrough D2). A negative code is a signal death and is
// annotated; zero means no concrete code was resolved (unresolvable death) and
// yields an empty fragment so no misleading "exit_code=0" is emitted.
func formatExitCode(code int) string {
	if code == 0 {
		return ""
	}
	if code < 0 {
		return fmt.Sprintf(" exit_code=%d (signal)", code)
	}
	return fmt.Sprintf(" exit_code=%d", code)
}

// settleResultSegment decides the `→ 结果:` tail of a settle notification. A
// blank-only payload with no error degrades to a single-line "（无输出）" ticket
// (D5): an all-whitespace body must not be injected as content.
func settleResultSegment(result string, hasErr bool) string {
	if !hasErr && strings.TrimSpace(result) == "" {
		return "（无输出）"
	}
	return result
}

// newBatchRetiredSummaryEvent emits ONE external_input carrying N per-task
// settled lines, collapsing a retirement storm into a single notification.
// The line format mirrors newTaskSettledEvent header: retire outputs are
// short machine verdicts, so no spill is needed. An empty batch returns nil.
// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
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
		fmt.Fprintf(&b, "[task settled] %s %s (id=%s) %s%s",
			marker, truncateRunes(r.Task.Spec.Desc, settleDescMaxChars), task.ShortID(r.Task.ID), statusWord, formatExitCode(r.Sig.ExitCode))
		if r.Sig.Err != nil {
			fmt.Fprintf(&b, " 错误: %s", truncateRunes(r.Sig.Err.Error(), settleErrMaxChars))
		}
		if seg := settleResultSegment(r.Sig.Output, r.Sig.Err != nil); seg != "" {
			fmt.Fprintf(&b, " → 结果: %s", escapeNewlines(seg))
		}
	}
	msg := model.Message{Role: model.RoleUser, Content: b.String()}
	evt := NewExternalInputEvent("task-batch-retire", msg)
	// The production-side recognition authority (D10): fold eligibility comes
	// from this mark verified against the stored event, never from the body
	// prefix — a user message imitating the notice shape stays unmarked.
	evt.Metadata["settle_notice"] = "true"
	return evt
}

// newTaskSettledEvent builds a self-contained external_input event describing a
// background task that has settled, so the persistent loop reclaims it into a
// new turn. The event body is a COMPACT SINGLE-LINE trajectory form
// : `[task settled] <marker> <desc>
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
	fmt.Fprintf(&b, "[task settled] %s %s (id=%s) %s%s",
		marker, truncateRunes(tk.Spec.Desc, settleDescMaxChars), task.ShortID(tk.ID), statusWord, formatExitCode(sig.ExitCode))
	if sig.Err != nil {
		fmt.Fprintf(&b, " 错误: %s", truncateRunes(sig.Err.Error(), settleErrMaxChars))
	}
	if seg := settleResultSegment(result, sig.Err != nil); seg != "" {
		if spillPath != "" {
			fmt.Fprintf(&b, " → %s", seg)
		} else {
			fmt.Fprintf(&b, " → 结果: %s", escapeNewlines(seg))
		}
	}
	evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: b.String()})
	for k, v := range tk.Spec.Origin {
		evt.Metadata[k] = v
	}
	if len(tk.Spec.Origin) == 0 {
		evt.Metadata["lineage_absent"] = "true"
	}
	// Signal-level lineage outranks the spawn-time Origin: retirement stamps
	// task-retired on the settle signal, never on Origin itself — Origin stays
	// immutable so a resumed task's later settles keep their original value.
	if sig.Lineage != "" {
		evt.Metadata[tagentevent.MetaKeyTriggerSource] = sig.Lineage
	}
	if sig.Kind == task.SettleStable || sig.Kind == task.SettleWatch {
		if ms := tk.DetachedAtMilli(); ms > 0 {
			evt.Metadata["detached_at_ms"] = fmt.Sprintf("%d", ms)
		}
	}
	evt.Metadata["settle_status"] = statusWord
	evt.Metadata["task_id"] = tk.ID
	// The production-side recognition authority (D10): fold eligibility comes
	// from this mark verified against the stored event, never from the body
	// prefix — a user message imitating the notice shape stays unmarked.
	evt.Metadata["settle_notice"] = "true"
	return evt
}

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
// Durable mode (lossless under D2): with an Inbox
// configured, ALL inbound events are persisted to inbox-v2 BEFORE the durable
// receipt — the channel carries only wake-ups, never the durable truth. Each
// message slot keeps a lossless JSON snapshot of the original AgentEvent
// (ID/Type/Source/Timestamp/full Message/business Metadata), so a restart
// restores everything inbox-v1 dropped . Durable envelopes are consumed
// strictly in enqueue order (zero-padded seq); volatile channel events are
// best-effort by definition. Receipted items replaying after a crash are
// Ack-skipped without re-execution.
type EventBus struct {
	ch chan *AgentEvent

	// inbox 是可选的 durable 输入信箱（inbox-v2）。nil = 纯 channel 轻量模式。
	inbox *reliability.Inbox

	// retention 是可选的  材料保留能力（memory.RetentionGuard 的结构性投影，
	// 避免此处 import memory）。durable inbox 下由 agent 装配注入，用于从现有未确认
	// envelope 重建保留租约并在 ack 后释放。nil = 未接线（不 arm、不释放）。
	retention retentionGuard

	// publishDropped 统计 void Publish（兼容入口）未能接受的事件（超时/已关闭）——
	// 该入口按契约不显式失败，但拒绝必须保持可观测。
	publishDropped atomic.Int64
}

// PublishReceipt is the decidable result of a context-aware acceptance (3.1).
type PublishReceipt struct {
	RequestID string
	Durable   bool
}

// ErrPublishTimeout Publish errors — callers of PublishContext can branch on these; the void
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

// NewReliableEventBus 在 spillDir/inbox-v2 打开持久收件箱。存在旧的 *.spill 残留或未排空的
// inbox-v1 树时拒绝升级（fail-loud 并给出迁移指引）：必须由前一个二进制排空，v2 从不猜测式
// 迁移。所有错误一律返回——配置要求可靠性时，绝不静默降级为易失。
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

// retentionGuard is the  材料保留 capability surface the bus needs from the store
// (structurally mirrors memory.RetentionGuard to avoid importing memory here). The
// recovery owner protects the durable originals of unacked envelopes until they are
// acked (dir-synced) and released, so TTL/capacity/compaction cannot destroy material
// a restart still needs.
type retentionGuard interface {
	ProtectKey(key int64)
	ReleaseKey(key int64)
	ArmRetention()
	// BeginHold/EndHold raise/release the  registration barrier (structurally
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
// envelopes and then arms it, releasing the lifecycle scanner's
// first destructive pass. Called once at agent-open after SetRetentionGuard. A durable
// inbox with no material still arms (empty protect) so the scanner is not gated on an
// inbox that never registers. A failed enumeration does NOT arm (never under-protect on
// a partial view); the lifecycle startup grace is the anti-starvation backstop.
func (b *EventBus) ArmRetentionFromInbox() error {
	if b == nil || b.inbox == nil || b.retention == nil {
		return nil
	}
	b.retention.BeginHold()
	mats, err := b.inbox.UnackedMaterial()
	if err != nil {
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
// removes the file) and pass it here; releasing after dir-sync is the  release point.
func (b *EventBus) releaseRetention(m reliability.UnackedMaterial) {
	if b == nil || b.retention == nil {
		return
	}
	for _, k := range materialKeys(m) {
		b.retention.ReleaseKey(k)
	}
}

// DrainRetentionCleanups 收尾延迟的 ack-清理屏障：对每个 unlink 已落地、但目录同步尚未成功的
// 信封，补齐所欠屏障，并在持久化确定后为其受保护材料释放保留租约——恰好一次。由消费循环
// 每轮驱动，使一次不确定的 ack 收敛为"容量＋租约各释放一次"，且不重跑输入。返回收尾的账数。
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
// at open. They are inert — never read or consumed — and the agent bootstraps
// may surface them to the operator. Safe to call on a nil/volatile bus (returns
// nils). A managed reset (ResetTransitional) is the only thing that clears them.
func (b *EventBus) TransitionalData() (spill, v1 []string) {
	if b == nil || b.inbox == nil {
		return nil, nil
	}
	return b.inbox.TransitionalData()
}

// ResetTransitional is the operator-invoked managed reset of previous-format data
// for this bus's inbox. It is destructive and requires an explicit confirm;
// it never runs automatically and never clears current-format corruption (which
// must surface, not be wiped). See reliability.Inbox.ResetTransitional.
func (b *EventBus) ResetTransitional(confirm bool) (int, error) {
	if b == nil || b.inbox == nil {
		return 0, nil
	}
	return b.inbox.ResetTransitional(confirm)
}

// PublishDropped 统计经由 void 兼容入口发生的拒绝。
func (b *EventBus) PublishDropped() int64 {
	if b == nil {
		return 0
	}
	return b.publishDropped.Load()
}

// PublishContext 是可判定的受理入口：成功返回凭据（易失或持久），否则返回错误——
// 满/超时/已关闭 绝不报成已受理。void Publish 是包装本函数的兼容入口。
func (b *EventBus) PublishContext(ctx context.Context, event *AgentEvent) (PublishReceipt, error) {
	if event == nil {
		return PublishReceipt{}, ErrNilEvent
	}
	receipt := PublishReceipt{RequestID: event.ID}

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
		select {
		case b.ch <- wakeEvent():
		default:
		}
		return receipt, nil
	}

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
// : every message keeps its own identity in
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

// Publish 是 void 兼容入口：包装 PublishContext，把拒绝记日志并计数而非失败。
// 新调用方（HTTP、宿主，以及任何要向用户报告是否受理的路径）必须使用
// PublishContext／InjectMessageContext。
func (b *EventBus) Publish(event *AgentEvent) {
	if event == nil {
		log.Warnf("[EventBus] Publish nil event, skipped")
		return
	}
	if _, err := b.PublishContext(context.Background(), event); err != nil {
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
			m := reliability.MaterialOf(env)
			if aerr := b.inbox.Ack(path); aerr != nil {
				log.Warnf("[ReliableBus] ack of receipted %s deferred: %v", env.RequestID, aerr)
			} else {
				b.releaseRetention(m)
			}
			continue
		}
		startIdx := len(batch)
		badSlot := -1
		var badErr error
		for _, m := range env.Messages {
			evt, derr := decodeSourceEvent(m.SourceEvent)
			if derr != nil {
				badSlot, badErr = m.Slot, derr
				break
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
		if badErr != nil {
			log.Errorf("[ReliableBus] undecodable source_event at %s slot %d — envelope quarantined: %v", path, badSlot, badErr)
			b.QuarantineEnvelope(path, fmt.Sprintf("undecodable source_event slot %d: %v", badSlot, badErr))
			batch = batch[:startIdx]
			continue
		}
	}
	return batch
}

// decodeSourceEvent restores the lossless AgentEvent snapshot persisted at
// acceptance. ID/Type/Source/Timestamp/full Message/business Metadata all come
// back intact  — nothing is re-stamped or dropped. The restored event's
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
// RecordEventKeys writeback .
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
// at path BEFORE its receipt is submitted. Idempotent on an
// identical payload; a differing payload on an already-frozen envelope is a
// conflict (reliability.ErrCompletionConflict) — the frozen completion is
// authoritative and the caller must NOT overwrite it. The caller treats an error
// as "receipt not yet safe": the claim stays and the completion write is retried
// in-process without re-running the model.
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
// inspection, capacity freed). See Inbox.QuarantineEnvelope.
//
// : quarantine is a terminal disposition just like
// Ack, so it MUST release the envelope's retention holders — otherwise an isolated
// envelope's originals stay leased forever and can never be TTL/capacity-evicted
// (a lease hang). The leaf reads the envelope ONCE under the mutation lock and
// hands back its material together with the moved flag, so "isolated ⇒ released"
// holds without a second read that could disagree (a transient material-read error
// between two reads used to quarantine the file and strand its lease).
// releaseRetention is nil-safe; an envelope that was never armed releases nothing.
// The rename's atomicity is the dir barrier — no separate cleanup is owed.
func (b *EventBus) QuarantineEnvelope(path, reason string) {
	if b == nil || b.inbox == nil {
		return
	}
	if m, moved := b.inbox.QuarantineEnvelope(path, reason); moved {
		b.releaseRetention(m)
	}
}

// ReleaseClaim returns a claimed envelope to pending for ordered re-claim on a
// transient submit failure. See Inbox.ReleaseClaim.
func (b *EventBus) ReleaseClaim(path string) error {
	if b == nil || b.inbox == nil {
		return nil
	}
	return b.inbox.ReleaseClaim(path)
}

// ConfirmDurable records the processing receipt then Acks. : the caller
// must present the verified receipt credential issued from a legal durable
// completion (ContextManager.verifyReceiptCredential) — RecordReceipt refuses
// without it, so no bare description string or request id can confirm an
// envelope. Failure leaves the claim on disk for replay (at-least-once).
func (b *EventBus) ConfirmDurable(path string, cred reliability.ReceiptCredential) error {
	if b.inbox == nil {
		return nil
	}
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
		b.releaseRetention(m)
	}
	return nil
}

// Pull blocks until at least one event arrives or ctx is cancelled.
// Then non-blocking drains all remaining pending events.
// Returns the batch and nil error on success.
// Returns nil and ctx.Err() when ctx is cancelled before any event arrives.
func (b *EventBus) Pull(ctx context.Context) ([]*AgentEvent, error) {
	for {
		batch := b.drainChannelNonWake()
		batch = append(batch, b.claimDurable()...)
		if len(batch) > 0 {
			return batch, nil
		}
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
			continue
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
	batch := b.drainChannelNonWake()
	batch = append(batch, b.claimDurable()...)
	if batch == nil {
		batch = []*AgentEvent{}
	}
	return batch
}
