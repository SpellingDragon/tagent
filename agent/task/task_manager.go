package task

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// TaskStatus is the lifecycle state of a Task.
type TaskStatus string

const (
	// TaskRunning 表示工作仍在产出：前台可继续等待，回收只看探活不看年龄。
	TaskRunning TaskStatus = "running"
	// TaskStable 是一次"输出稳定"观测对应的状态。已脱离前台的任务不回退到此态。
	TaskStable TaskStatus = "stable"
	// TaskAliveDetached 表示服务已就绪但仍存活：重复稳定/可疑观察被抑制，终局照常通知。
	TaskAliveDetached TaskStatus = "alive_detached"
	// TaskCompleted 是正常结束的终态（结算信号不带错误）。
	TaskCompleted TaskStatus = "completed"
	// TaskFailed 是异常结束的终态（结算信号带错误，或进程死亡）。
	TaskFailed TaskStatus = "failed"
	// TaskSuspect 表示静默超过可疑阈值：可能还活着，但久无输出。面板此时
	// 展示统一回收器算出的剩余寿命，由模型判断继续等还是再派。
	TaskSuspect TaskStatus = "suspect"
	// TaskDead 属于终态集合。当前生产路径只在终态判定与冥想摘要里读取它，
	// 没有写入点——保留意味着 "dead" 这个取值仍可能出现在外部数据里。
	TaskDead TaskStatus = "dead"
	// TaskCancelled 是被显式取消后的终态。
	TaskCancelled TaskStatus = "cancelled"
)

// defaultZombieGrace is the minimum age a running/suspect task must reach
// before reconcileZombies may retire it. The liveness probe — not age — is
// the kill criterion; the grace merely keeps the sweep away from spawn
// windows and legitimately quiet long-runners.
const defaultZombieGrace = 10 * time.Minute

// defaultOrphanGrace bounds the reincarnation-orphan adjudication:
// conservative enough to never kill a quiet but young this-life subagent,
// large enough that restored multi-hour suspects (the actual target) qualify
// immediately at rebuild.
const defaultOrphanGrace = 30 * time.Minute

// SettleKind classifies how a task reached a settle point. Detectors emit this
// deterministically; the LLM interprets ambiguous kinds (stable/suspect) later.
type SettleKind string

const (
	// SettleCompleted 表示一次工作正常结束；若同时带有错误，按失败处理。
	SettleCompleted SettleKind = "completed"
	// SettleStable 是输出稳定观察；job 通知模型判断，service 首次就绪后抑制重复观察。
	SettleStable SettleKind = "stable"
	// SettleSuspect 是静默可疑观察；job 通知模型判断，已就绪服务只抑制非终局观察。
	SettleSuspect SettleKind = "suspect"
	// SettleWatch 只作观察通知：不改变任务状态，仅把信号交给结算回调与事件总线。
	SettleWatch SettleKind = "watch"
	// SettleReady reports explicit producer readiness.
	SettleReady SettleKind = "ready"
	// SettleInput reports that an interactive producer is waiting for input.
	SettleInput SettleKind = "input"
	// SettleFailed 是失败结算：由僵尸/孤儿回收或携带错误的路径驱动；finalize 会把
	// 带错误的完成信号规范化为它，且只结算一次。
	SettleFailed SettleKind = "failed"
)

// LifetimeJob Task lifecycle classes: jobs are one-round
// work units; services are long-lived by design. The class only
// decides orchestration semantics — the unified TTL reaper
// covers both classes equally, with no age exemption.
const (
	LifetimeJob     = "job"
	LifetimeService = "service"
)

// LifetimeOf resolves the effective lifecycle class: an explicit declaration
// wins; otherwise Kind is the proxy (command/subagent behave as jobs — one
// round then exit; generic/unknown default to service — long-lived but
// still bounded by the unified TTL, never an age exemption).
func LifetimeOf(spec TaskSpec) string {
	if spec.Lifetime == LifetimeJob || spec.Lifetime == LifetimeService {
		return spec.Lifetime
	}
	switch spec.Kind {
	case "command", "subagent":
		return LifetimeJob
	default:
		return LifetimeService
	}
}

// WaitCondition is the outcome a foreground call is waiting for.
type WaitCondition string

const (
	// WaitExit waits for producer completion or failure.
	WaitExit WaitCondition = "exit"
	// WaitReady waits for explicit producer readiness.
	WaitReady WaitCondition = "ready"
	// WaitInput waits for an interactive input prompt.
	WaitInput WaitCondition = "input"
	// WaitSignal waits for the producer's declared signal condition.
	WaitSignal WaitCondition = "signal"
)

// WaitForOf resolves the foreground condition independently from producer lifetime.
func WaitForOf(spec TaskSpec) WaitCondition {
	if spec.WaitFor != "" {
		return spec.WaitFor
	}
	if spec.Mode == "interactive" {
		return WaitInput
	}
	if spec.Mode == "service" || spec.Mode == "resident" || LifetimeOf(spec) == LifetimeService {
		return WaitReady
	}
	return WaitExit
}

// SatisfiesWait evaluates an observation against the requested foreground condition.
func SatisfiesWait(spec TaskSpec, sig SettleSignal) bool {
	if sig.WaitSatisfied || sig.EndsRound() {
		return true
	}
	legacyReady := spec.WaitFor == "" && spec.Mode == "" && sig.Kind == SettleStable
	if sig.WaitFor != "" {
		spec.WaitFor = sig.WaitFor
	}
	switch WaitForOf(spec) {
	case WaitReady:
		return sig.Ready || sig.Kind == SettleReady || legacyReady
	case WaitInput:
		return sig.Kind == SettleInput
	case WaitSignal:
		return sig.Kind == SettleWatch && (sig.Observation == "signal" || sig.Observation == "watch")
	default:
		return false
	}
}

// SettleSignal is emitted by a SettleDetector when a task reaches a settle point.
type SettleSignal struct {
	WaitFor       WaitCondition
	InputBatchID  string
	Declarative   *Declarative
	RoundID       uint64
	ProducerRound uint64
	RoundOwner    string
	Seq           uint64
	ObservedAt    time.Time
	SourceID      string
	Ready         bool
	WaitSatisfied bool
	Observation   string
	Kind          SettleKind
	Output        string
	Err           error
	// Final marks the last signal of a round whose result is non-terminal
	// (for example a tmux timeout). Completed and failed always end a round.
	Final bool
	// ExitCode is the process exit status read at settle time (failure-polarity
	// passthrough D2): non-zero (or negative for signal death) reports how a dead
	// process exited. Zero is ambiguous between "clean exit 0" and "not read", so
	// it is only meaningful alongside a known process death; it is NOT persisted
	// (restored tasks re-adjudicate via liveness probe).
	ExitCode int
	// Lineage overrides the settled EVENT's trigger_source attribution for this
	// one settle (retirement stamps LineageRetired). Signal-level like ExitCode:
	// it is never written back into Spec.Origin — Origin is the spawn-time
	// provenance identity and stays immutable after birth — and it is not
	// persisted, so a restored task re-adjudicates its own lineage rather than
	// replaying a stale retirement mark.
	Lineage string
}

// SatisfiesWait reports the persisted wait decision for this signal.
func (s SettleSignal) SatisfiesWait() bool {
	return s.WaitSatisfied || s.EndsRound()
}

// SignalSourceID identifies one producer observation across retries and restarts.
func SignalSourceID(taskID string, roundID, seq uint64) string {
	return fmt.Sprintf("task-settle-%s-%d-%d", taskID, roundID, seq)
}

// EndsRound reports whether no further signals are expected for this round.
func (s SettleSignal) EndsRound() bool {
	return s.Final || s.Kind == SettleCompleted || s.Kind == SettleFailed
}

// LineageRetired is the settle-signal lineage stamped by TTL/liveness
// retirement onto the resulting settlement event's trigger_source.
const LineageRetired = "task-retired"

// SettleDetector observes a running task and emits SettleSignals. Different task
// types provide different detectors:
// - tmux command → wraps TmuxMonitor (stable/completed/suspect)  [Phase 1]
// - sub-agent → RunFlow returns [Phase 3]
// - generic → goroutine returns [Phase 0]
//
// Settled() MUST be closed when the detector will emit no further signals.
type SettleDetector interface {
	// Settled returns a channel delivering settle signals, closed when done.
	Settled() <-chan SettleSignal
	// Detached returns a channel that fires (is closed) once when the detector's
	// dense phase ends without a settle — the sync→async boundary. Spawn selects
	// on settle-or-detach; a detach means "stop blocking, notify later". A
	// detector that never wants to force-detach may return a nil/never channel.
	Detached() <-chan struct{}
	// Cancel stops the underlying work.
	Cancel()
	// Stopped returns a channel closed once the detector's underlying producer has
	// RETURNED — not merely been notified to cancel.  requires it of
	// every path that adopts no task (spawn rejected, dedup hit): the caller may
	// only let go of the execution reference it derived for that work after the
	// producer actually stopped, because Cancel is a signal, not a credential.
	Stopped() <-chan struct{}
}

// ActivatableDetector prepares resources before their business work starts.
type ActivatableDetector interface{ Start() }

// SignalAcknowledger releases producer resources after a signal is accepted.
type SignalAcknowledger interface{ AcknowledgeSignal(SettleSignal) }

// retireNotifyGrace is how long a terminal task waits after settledAt before
// the board's re-notify pass may pick it up — long enough for the synchronous
// finalize dispatch (both paths mark on callback return) to finish, so only
// genuinely unconfirmed terminals are resent.
const retireNotifyGrace = 2 * time.Second

// defaultDenseDuration is the default dense-phase length (≈ the retired
// sync_wait): how long a detector blocks before signalling detach.
const defaultDenseDuration = 10 * time.Second

// DetachAfter returns a channel that is closed after d elapses, unless stop
// fires first (e.g. the detector settled/cancelled). It is the shared timer
// primitive behind the settle-or-detach contract; the returned channel closing
// means "dense phase ended — detach".
func DetachAfter(d time.Duration, stop <-chan struct{}) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			close(ch)
		case <-stop:
		}
	}()
	return ch
}

// Declarative is the serializable projection of a TaskSpec — everything
// needed to rebuild the closure trio (Relaunch/ResumeFn/Alive)
// cross-restart via the tool/action closure factory. The in-process closures
// remain authoritative while alive; Declarative is what task_spawned events
// carry and what RebuildTaskRegistry replays (registry = fold of the fact
// chain).
type Declarative struct {
	Mode                  string        `json:"mode,omitempty"`
	WaitFor               WaitCondition `json:"wait_for,omitempty"`
	InputBatchID          string        `json:"input_batch_id,omitempty"`
	ProducerInputBatchID  string        `json:"producer_input_batch_id,omitempty"`
	RoundID               uint64        `json:"round_id,omitempty"`
	RoundOwner            string        `json:"round_owner,omitempty"`
	ProducerRound         uint64        `json:"producer_round,omitempty"`
	ProducerOwner         string        `json:"producer_owner,omitempty"`
	SignalSeq             uint64        `json:"signal_seq,omitempty"`
	ProducerWaitSatisfied bool          `json:"producer_wait_satisfied,omitempty"`
	TTLAnchorMilli        int64         `json:"ttl_anchor_ms,omitempty"`
	Kind                  string        `json:"kind"`
	Desc                  string        `json:"desc"`
	Key                   string        `json:"key,omitempty"`
	Command               string        `json:"command,omitempty"`
	// AgentName and MessageBody rebuild subagent delegation; the agent layer restores its round history and reentry closures.
	AgentName   string            `json:"agent_name,omitempty"`
	MessageBody string            `json:"message_body,omitempty"`
	EventKeys   []int64           `json:"event_keys,omitempty"`
	Origin      map[string]string `json:"origin,omitempty"`
	TaskID      string            `json:"task_id,omitempty"`
	// Lifetime declares the job/service lifecycle class: it decides
	// orchestration semantics only — both classes fall under the
	// unified TTL reaper with no age exemption. Persisted so
	// restores re-derive the same class.
	Lifetime string `json:"lifetime,omitempty"`
	// DetachedAtMilli persists the alive-detached transition time so a
	// restored task keeps its REAL detached age (restore time must never
	// silently replace it). Carried on the alive-detached settle event's
	// metadata and replayed by RebuildTaskRegistry.
	DetachedAtMilli int64 `json:"detached_at_ms,omitempty"`
	// Params carries the ActionArgs spawn fields (WorkDir/Env/Mode/Name/IsTUI/
	// Watch/Probe/ProbeIntervalSec/ProbeFailures/QuietTimeout/Timeout — encoded
	// as strings; session-op fields excluded; conversion lives in tool/action).
	Params map[string]string `json:"params,omitempty"`
	// Overrides carries the per-call execution-view overrides of a subagent
	// delegation (system prompt / model reference / tools subset). Division of
	// labor with Params: Params holds scalar knobs (ttl and friends), Overrides
	// holds the view the call assembled with; the two are never mixed.
	// Persisted so a relaunch replays the SAME view the original call ran on.
	Overrides *Overrides `json:"overrides,omitempty"`

	StartedAtMilli int64 `json:"started_at_ms"`
}

// Overrides is the per-call view-override payload of one subagent delegation:
// the three execution-view items a caller may replace for a single call. It
// lives on the invocation and on the task's Declarative only — there is no
// owner-level or process-wide store for it — so a call that carries no
// Overrides assembles the generation's own view.
//
//   - Empty field = that view face is not overridden; every face resolves independently.
//   - Validation happens where the arguments are read: a tools entry outside the
//     delegate's declared maximum domain, or a model reference that is not
//     registered, is a refusal, never a silent narrowing.
type Overrides struct {
	// SystemPrompt replaces the delegate's system prompt for this call.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// ModelRef names a registered model reference, resolved at assembly time.
	ModelRef string `json:"model_ref,omitempty"`
	// ToolsSubset narrows the delegate's tool surface for this call. A nil
	// slice means no narrowing; an empty one runs with no tools.
	ToolsSubset []string `json:"tools_subset,omitempty"`
}

// IsEmpty reports whether no view face is overridden.
func (o *Overrides) IsEmpty() bool {
	return o == nil || (o.SystemPrompt == "" && o.ModelRef == "" && o.ToolsSubset == nil)
}

// TaskSpec captures enough to describe and (re)launch a task.
type TaskSpec struct {
	Mode         string
	WaitFor      WaitCondition
	InputBatchID string
	RoundOwner   string
	Start        func(*Task) (TaskSpec, SettleDetector, error)
	OnRoundStart func(*Task)
	Kind         string
	Desc         string
	// Protected marks a durability/repair-class task (self-telemetry-audit
	// exemption whitelist): the AUDIT freeze gate passes it through, while the
	// global disk gate still applies. Set at construction by the owning
	// subsystem — never by model-facing arguments (a runtime self-label would
	// defeat the whitelist).
	Protected bool
	// Key is the idempotency key: while a task with this Key is active, a
	// repeat Spawn returns the existing task instead of creating a duplicate.
	// Empty Key disables dedup.
	Key string
	// Relaunch, when non-nil, re-spawns an equivalent task from scratch (used by
	// relaunch(id)). For command tasks it re-runs the original command in a
	// fresh session. Nil → the task is not relaunchable.
	//
	// It receives the INITIATING call's context: a re-entry riding a live
	// business turn resolves its delegation target on THAT turn's orchestration
	// generation, while a re-entry with no initiating call (console/WAL/ops) gets
	// the effective one. The task layer itself never interprets the context — it
	// forwards the caller's, which is the only version source the re-entry may use.
	Relaunch func(ctx context.Context) (SpawnResult, error)
	// ResumeFn, when non-nil, feeds new input into the task's LIVE session
	// (resume_task): tmux tasks SendKeys into the existing session, subagent
	// tasks start a new Run with framework-restored task-chain context. It
	// returns a fresh SettleDetector for the resumed round; the task then
	// re-enters the standard dense→ACK→settle lifecycle under the SAME task id.
	// Nil → the task is not resumable. Same initiating-context contract as Relaunch.
	ResumeFn func(ctx context.Context, input string) (SettleDetector, error)
	// Alive, when non-nil, is a liveness probe for service-type tasks. After a
	// task settles into alive_detached, List() consults the probe lazily: a
	// false answer retires the task through the normal completion path, so
	// board entries whose backing session (tmux) died out-of-band are not
	// shown forever. Nil -> never reconciled (e.g. subagent tasks).
	// See TaskManager.reconcileDetached.
	Alive func() bool
	// Origin is opaque baggage: a snapshot of the spawning turn's invocation
	// metadata (e.g. chat_id), stamped by the framework at spawn time and
	// carried verbatim to the task_settled event so a background result can be
	// routed back to the originating session. The task layer NEVER reads or
	// interprets it (courier, not router). Nil for tasks with no origin.
	Origin map[string]string

	// Lifetime declares the job/service class.
	// Empty → inferred from Kind (command/subagent → job, generic → service).
	Lifetime string

	// Declarative 是本 spec 的可序列化投影，用于事实链的 task_spawned 记录与跨重启重建：
	// 置位后 Spawn 经 OnSpawn 持久化它，使 RebuildTaskRegistry 能回放该任务。派生时可选——
	// 调用方可以只提供闭包（不经事实链重建的那类用法）。
	Declarative *Declarative

	// TTL 是该任务解析后的绝对生命周期。统一的回收器在其生命周期锚点（派生时刻，重入发送/
	// resume 会刷新）到期后终止底层进程并退役任务。它在派生时由 `ttl` 参数或配置默认值
	// 设定，恒大于 0（不存在"不限量"的任务）。为 0 表示调用方未显式设置（例如恢复路径
	// 未携带 TTL）；此时回收器退回自身默认值，而不是把 0 当成"不限量"。
	TTL time.Duration
}

// Task is a unit of async work tracked by the TaskManager.
type Task struct {
	ID        string
	Spec      TaskSpec
	StartedAt time.Time

	mu           sync.Mutex
	activationMu sync.Mutex
	status       TaskStatus
	result       string
	err          error
	settledAt    time.Time

	// retireNotified marks that the terminal notification for this task was
	// HANDED to a dispatcher callback that RETURNED (the in-flight kill window
	// inside a callback leaves it false, so the board's reconcile re-notify
	// pass resends after a restart). Zero on restore: a process replacement
	// re-notifies restored terminal tasks once via the stable request id.
	retireNotified bool

	detector              SettleDetector
	firstSettle           chan SettleSignal
	inlineOffered         bool
	windowClosed          bool
	windowDone            chan struct{}
	aliveDetached         bool
	detachedAt            time.Time
	ttlRenewedAt          time.Time
	watchDone             chan struct{}
	watchStopped          bool
	roundID               uint64
	roundOwner            string
	producerRound         uint64
	producerOwner         string
	inputBatchID          string
	producerInputBatchID  string
	producerWaitSatisfied bool
	signalSeq             uint64
	resuming              bool
	serviceObserved       bool
	serviceKind           SettleKind
	serviceOutput         string
	serviceRound          uint64
	recovering            bool
	deferredActivation    *detectorActivation
	rollback              *roundRollback
	pending               map[string]*pendingDelivery
}

type roundRollback struct {
	producerWaitSatisfied bool
	status                TaskStatus
	spec                  TaskSpec
	detector              SettleDetector
	producerRound         uint64
	producerOwner         string
	producerInputBatchID  string
	signalSeq             uint64
	ttlRenewedAt          time.Time
	settledAt             time.Time
	aliveDetached         bool
	detachedAt            time.Time
}

type pendingDelivery struct {
	signal   SettleSignal
	detector SettleDetector
	inline   bool
	aborted  bool
	inFlight bool
	retryAt  time.Time
	backoff  time.Duration
	restored bool
}

type detectorActivation struct {
	detector SettleDetector
	round    uint64
	owner    string
	refresh  bool
	ready    chan struct{}
}

// SpecSnapshot returns an isolated copy of the task declaration and metadata.
func (t *Task) SpecSnapshot() TaskSpec {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.specSnapshotLocked()
}

func (t *Task) specSnapshotLocked() TaskSpec {
	spec := cloneSpec(t.Spec)
	spec.InputBatchID = t.inputBatchID
	if spec.Declarative != nil {
		spec.Declarative.InputBatchID = t.inputBatchID
		spec.Declarative.ProducerInputBatchID = t.producerInputBatchID
		spec.Declarative.RoundID = t.roundID
		spec.Declarative.RoundOwner = t.roundOwner
		spec.Declarative.ProducerRound = t.producerRound
		spec.Declarative.ProducerOwner = t.producerOwner
		spec.Declarative.SignalSeq = t.signalSeq
		spec.Declarative.ProducerWaitSatisfied = t.producerWaitSatisfied
		spec.Declarative.TTLAnchorMilli = t.ttlAnchor().UnixMilli()
	}
	return spec
}

// RoutingSnapshot contains one consistent view of intent, producer and pending delivery identities.
type RoutingSnapshot struct {
	Spec           TaskSpec
	RoundID        uint64
	RoundOwner     string
	ProducerRound  uint64
	ProducerOwner  string
	Status         TaskStatus
	WaitSatisfied  bool
	PendingSignals []SettleSignal
}

// RoutingSnapshot reads every routing identity under one task lock.
func (t *Task) RoutingSnapshot() RoutingSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := RoutingSnapshot{Spec: t.specSnapshotLocked(), RoundID: t.roundID, RoundOwner: t.roundOwner, ProducerRound: t.producerRound, ProducerOwner: t.producerOwner, Status: t.status, WaitSatisfied: t.producerWaitSatisfied}
	for _, pending := range t.pending {
		state.PendingSignals = append(state.PendingSignals, pending.signal)
	}
	return state
}

func cloneSpec(spec TaskSpec) TaskSpec {
	copyMap := func(src map[string]string) map[string]string {
		if src == nil {
			return nil
		}
		dst := make(map[string]string, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	spec.Origin = copyMap(spec.Origin)
	if spec.Declarative != nil {
		d := *spec.Declarative
		d.Origin = copyMap(d.Origin)
		d.Params = copyMap(d.Params)
		d.EventKeys = append([]int64(nil), d.EventKeys...)
		if d.Overrides != nil {
			o := *d.Overrides
			if o.ToolsSubset != nil {
				o.ToolsSubset = append([]string{}, o.ToolsSubset...)
			}
			d.Overrides = &o
		}
		spec.Declarative = &d
	}
	return spec
}

// RoundID returns the latest claimed intent, including an aborted attempt.
func (t *Task) RoundID() uint64 { t.mu.Lock(); defer t.mu.Unlock(); return t.roundID }

// ProducerRound returns the round that owns the current backing resource.
func (t *Task) ProducerRound() uint64 { t.mu.Lock(); defer t.mu.Unlock(); return t.producerRound }

// ProducerOwner returns the invocation that owns the actual backing resource.
func (t *Task) ProducerOwner() string { t.mu.Lock(); defer t.mu.Unlock(); return t.producerOwner }

// WaitSatisfiedOfProducer reports whether the actual producer met its foreground condition.
func (t *Task) WaitSatisfiedOfProducer() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.producerWaitSatisfied
}

// RoundOwner returns the invocation that claimed the latest intent.
func (t *Task) RoundOwner() string { t.mu.Lock(); defer t.mu.Unlock(); return t.roundOwner }

// SignalSeq returns the last observed sequence of the current producer round.
func (t *Task) SignalSeq() uint64 { t.mu.Lock(); defer t.mu.Unlock(); return t.signalSeq }

// WaitFor returns the effective foreground waiting condition.
func (t *Task) WaitFor() WaitCondition { t.mu.Lock(); defer t.mu.Unlock(); return WaitForOf(t.Spec) }

// TTLAnchorMilli returns the actual producer lifetime anchor.
func (t *Task) TTLAnchorMilli() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ttlAnchor().UnixMilli()
}

// HasProducer reports whether a detector is attached to the task's resource.
func (t *Task) HasProducer() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.detector != nil }

// PendingSignals returns delivery identities that have not been accepted.
func (t *Task) PendingSignals() []SettleSignal {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]SettleSignal, 0, len(t.pending))
	for _, p := range t.pending {
		out = append(out, p.signal)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RoundID == out[j].RoundID {
			return out[i].Seq < out[j].Seq
		}
		return out[i].RoundID < out[j].RoundID
	})
	return out
}

// HasPendingDelivery reports whether any observation still needs acceptance.
func (t *Task) HasPendingDelivery() bool { t.mu.Lock(); defer t.mu.Unlock(); return len(t.pending) > 0 }

// Status returns the task's current status (thread-safe snapshot).
func (t *Task) Status() TaskStatus { t.mu.Lock(); defer t.mu.Unlock(); return t.status }

// DetachedAtMilli returns the alive-detached transition time (0 = never
// detached). Thread-safe snapshot for fact-chain persistence.
func (t *Task) DetachedAtMilli() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.detachedAt.IsZero() {
		return 0
	}
	return t.detachedAt.UnixMilli()
}

// setDetachedAtMilli restores the detached transition time from the fact
// chain (RebuildTaskRegistry replay). Must NOT be used at runtime — runtime
// transitions stamp it in emitBackground.
func (t *Task) setDetachedAtMilli(ms int64) {
	if ms <= 0 {
		return
	}
	t.mu.Lock()
	t.detachedAt = time.UnixMilli(ms)
	t.mu.Unlock()
}

// SetDetachedAtMilli is the exported restore-side hook (agent package owns
// the replay; runtime transitions must use emitBackground instead).
func (t *Task) SetDetachedAtMilli(ms int64) { t.setDetachedAtMilli(ms) }

// ttlAnchor returns the absolute-lifetime anchor for the unified reaper: the last
// reentrant refresh if any, else the immutable spawn time.
// Callers must hold t.mu while reading ttlRenewedAt (reconcileTTL does; StartedAt
// is set once at spawn and never mutated afterwards).
func (t *Task) ttlAnchor() time.Time {
	if !t.ttlRenewedAt.IsZero() {
		return t.ttlRenewedAt
	}
	return t.StartedAt
}

// remainingLifetime returns how long until the unified reaper would retire the
// task, given the manager's fallback TTL, and whether it is bounded at all. It
// mirrors reconcileTTL's effective-TTL + ttlAnchor so the board NEVER shows a
// number the reaper would not honor (async-task-lifetime 10.6). Callers must not
// already hold t.mu (this acquires it).
func (t *Task) remainingLifetime(now time.Time, defaultTTL time.Duration) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ttl := t.Spec.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if ttl <= 0 {
		return 0, false
	}
	anchor := t.ttlAnchor()
	if anchor.IsZero() {
		return 0, false
	}
	return ttl - now.Sub(anchor), true
}

// Result returns the latest captured output (thread-safe snapshot).
func (t *Task) Result() string { t.mu.Lock(); defer t.mu.Unlock(); return t.result }

// Live reports whether s is still a live state (work outstanding, or a session
// another part of the system may still be using). Exported so the same question can
// be asked from outside this package with the same answer the board's own dedup
// gives (isActive below), rather than each caller re-listing the states from memory.
func (s TaskStatus) Live() bool {
	switch s {
	case TaskRunning, TaskStable, TaskAliveDetached, TaskSuspect:
		return true
	default:
		return false
	}
}

// isActive reports whether the task is still live (dedup targets these).
func (t *Task) isActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status.Live()
}

// isTerminalStatus reports whether s is an exited state (pruneTerminal
// reclaims these). Lock-free: callers must already hold t.mu.
func isTerminalStatus(s TaskStatus) bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled, TaskDead:
		return true
	default:
		return false
	}
}

// isTerminalExpired reports whether the task has exited (terminal state) and
// its grace period has elapsed, making it eligible for pruning + resource
// reclamation. Live tasks are never expired. A terminal task without a recorded
// settledAt is not pruned until the timestamp is set (defensive).
func (t *Task) isTerminalExpired(now time.Time, ttl time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.status {
	case TaskRunning, TaskStable, TaskAliveDetached, TaskSuspect:
		return false
	}
	if t.settledAt.IsZero() || t.recovering {
		return false
	}
	return len(t.pending) == 0 && now.Sub(t.settledAt) >= ttl
}

// SpawnResult is returned by Spawn.
type SpawnResult struct {
	Task    *Task
	Settled bool
	Signal  SettleSignal
	Deduped bool
	Blocked string
}

// TaskSpawner is the narrow interface tools (e.g. ActionTool) use to hand a
// SettleDetector to the task layer at call time — injected via the invocation's
// RuntimeState — so tools stay free of any task-lifecycle state. *TaskManager
// implements it.
type TaskSpawner interface {
	Spawn(spec TaskSpec, detector SettleDetector) SpawnResult
}

var _ TaskSpawner = (*TaskManager)(nil)

// TaskController is the broader task-management surface used by the live board
// and the LLM task tools (list/get/cancel). It embeds TaskSpawner so a single
// injected value serves both tool spawning and task management. *TaskManager
// implements it.
type TaskController interface {
	TaskSpawner
	List() []*Task
	Get(id string) (*Task, bool)
	Cancel(id string) bool
	Relaunch(ctx context.Context, id string) (SpawnResult, error)
	Resume(ctx context.Context, id string, input string) (SpawnResult, error)
	// RenewTTLBySession resets the unified-reaper anchor for the ACTIVE task bound
	// to the given backing session (async-task-lifetime 10.4). Driven by a
	// write-type reentry (exec op=send); returns true if a live task was renewed.
	RenewTTLBySession(sessionID string) bool
	// DefaultTTL reports the unified reaper's fallback lifetime, so the board can
	// render the same effective remaining time reconcileTTL honors (10.6).
	DefaultTTL() time.Duration
}

var _ TaskController = (*TaskManager)(nil)

// OriginSpawner originSpawner wraps a TaskController to stamp opaque origin baggage (the
// spawning turn's invocation metadata) onto each spawned task's spec — without
// the task layer needing to know about routing. It embeds TaskController so the
// full management surface (List/Get/Cancel/Relaunch) stays available to task
// tools; only Spawn is augmented.
type OriginSpawner struct {
	TaskController
	Origin       map[string]string
	RoundOwner   string
	InputBatchID string
}

// Spawn 在 spec 未自带 Origin 时，把本包装器携带的 origin 逐键复制一份填进去（复制而非共享：
// 调用方随后改写自己的 map 不会串到任务上），再委托给内层控制器。
func (o *OriginSpawner) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
	if spec.InputBatchID == "" {
		spec.InputBatchID = o.InputBatchID
	}
	if spec.RoundOwner == "" {
		spec.RoundOwner = o.RoundOwner
	}
	if spec.Origin == nil && len(o.Origin) > 0 {
		cp := make(map[string]string, len(o.Origin))
		for k, v := range o.Origin {
			cp[k] = v
		}
		spec.Origin = cp
	}
	return o.TaskController.Spawn(spec, detector)
}

type taskSpawnerCtxKey struct{}
type roundOwnerCtxKey struct{}
type inputBatchCtxKey struct{}
type roundStartCtxKey struct{}
type taskIdentityCtxKey struct{}
type taskIdentity struct {
	id    string
	round uint64
}

// WithRoundOwner binds a resumed intent to its initiating invocation.
func WithRoundOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, roundOwnerCtxKey{}, owner)
}

// RoundOwnerFromContext returns the initiating invocation of a round.
func RoundOwnerFromContext(ctx context.Context) string {
	owner, _ := ctx.Value(roundOwnerCtxKey{}).(string)
	return owner
}

// WithInputBatchID carries the opaque input batch that initiated a task round.
func WithInputBatchID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, inputBatchCtxKey{}, id)
}

// InputBatchIDFromContext returns the initiating input batch without interpreting it.
func InputBatchIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(inputBatchCtxKey{}).(string)
	return id
}

// WithRoundStart registers bookkeeping before a resumed producer may emit signals.
func WithRoundStart(ctx context.Context, fn func(*Task)) context.Context {
	return context.WithValue(ctx, roundStartCtxKey{}, fn)
}

// TaskIDFromContext returns the task identity assigned before ResumeFn starts.
func TaskIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(taskIdentityCtxKey{}).(taskIdentity)
	return value.id
}

// RoundFromContext returns the intent assigned before ResumeFn starts.
func RoundFromContext(ctx context.Context) uint64 {
	value, _ := ctx.Value(taskIdentityCtxKey{}).(taskIdentity)
	return value.round
}

// WithTaskSpawner returns a context carrying the given TaskSpawner. tagent sets
// this on the context before RunFlow so tools (e.g. ActionTool) can retrieve it
// during Call via TaskSpawnerFromContext. Context values propagate through the
// framework flow down to tool.Call (the same path that carries the invocation).
func WithTaskSpawner(ctx context.Context, s TaskSpawner) context.Context {
	return context.WithValue(ctx, taskSpawnerCtxKey{}, s)
}

// TaskSpawnerFromContext returns the TaskSpawner injected via WithTaskSpawner,
// or (nil, false) when none is present (in which case tools fall back to their
// synchronous behavior).
func TaskSpawnerFromContext(ctx context.Context) (TaskSpawner, bool) {
	s, ok := ctx.Value(taskSpawnerCtxKey{}).(TaskSpawner)
	return s, ok && s != nil
}

// TaskControllerFromContext returns the injected value as a TaskController (the
// broader surface used by the LLM task tools). The value injected via
// WithTaskSpawner is a *TaskManager, which satisfies TaskController.
func TaskControllerFromContext(ctx context.Context) (TaskController, bool) {
	c, ok := ctx.Value(taskSpawnerCtxKey{}).(TaskController)
	return c, ok && c != nil
}

// TaskManagerConfig configures a TaskManager.
type TaskManagerConfig struct {
	OnBatchRetireAccepted  func([]BatchRetired) bool
	OnRetireResendAccepted func([]BatchRetired) bool
	OnSpawnAccepted        func(*Task) bool
	OnStartAccepted        func(*Task) bool
	OnRoundAccepted        func(*Task) bool
	OnRoundAbortedAccepted func(*Task, SettleSignal) bool
	OnInlineSettleAccepted func(*Task, SettleSignal) bool
	OnSettleAccepted       func(*Task, SettleSignal) bool
	EnableDeadlineReaper   bool
	// OnSettle is invoked when a task settles AFTER its window closed (detach) —
	// i.e. a background settle that must be written back as a task_settled event.
	// May be nil.
	OnSettle func(task *Task, sig SettleSignal)

	// OnBatchRetire 是可选的批量退役汇聚点：reconcile/孤儿退役走它——逐条的状态迁移与记账仍按
	// 单任务进行，但 bus 侧通知折叠为一条汇总事件。未注册时按逐条 OnSettle 通知。
	OnBatchRetire func(batch []BatchRetired)
	// OnRetireResend receives terminal tasks whose notification was never
	// confirmed handed off (callback interrupted, process replaced mid-flight).
	// The board reconcile pass calls it before prune; nil disables the pass.
	// Resend ids are stable per task+terminal-moment, so repeats across board
	// cycles and restarts collapse to one durable delivery.
	OnRetireResend func(batch []BatchRetired)
	// OnSpawn: invoked after a task registers
	// (best-effort fact-chain task_spawned record; never blocks the spawn
	// path). May be nil.
	OnSpawn func(task *Task)
	// OnInlineSettle : invoked when a task settles INSIDE its spawn/resume sync-wait
	// window (inline). Historically inline settles emitted NO record — the
	// result returns in-turn via the tool result — leaving fact-chain ghosts
	// for the registry replay. This hook emits a minimal settle record
	// (registry-only, never published to the bus — the LLM already saw the
	// result inline). May be nil.
	OnInlineSettle func(task *Task, sig SettleSignal)
	// OnCancel：任务被 Cancel 置为 cancelled 终态后调用——
	// 写事实链 cancelled 终态记录。终审发现：Cancel 仅改内存态，回放折叠
	// （spawned − 终态）下被取消的任务重启后以 suspect 复活（看板幽灵 +
	// subagent 同 Key dedup 永久锁死）。May be nil。
	OnCancel func(task *Task)
	// TerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained after settling before being pruned and its
	// resources reclaimed. It bounds the resume_task re-entry window for
	// terminal subagent tasks. Zero → defaultTerminalTTL.
	TerminalTTL time.Duration
	// SpawnGate: optional; returns a non-empty
	// readable reason to REJECT a new spawn (e.g. disk degraded). In-flight
	// tasks are never gated — a gate, not a wall. May be nil.
	SpawnGate func() string
	// AuditGate (self-telemetry-audit): optional per-spec gate consulted AFTER
	// SpawnGate — the behavioral-audit freeze source. Unlike the disk gate it
	// EXEMPTS protected specs (TaskSpec.Protected): the durability defense is
	// never withdrawn for attention governance (specs: 保护性任务豁免白名单).
	// May be nil.
	AuditGate func(spec TaskSpec) string
	// ZombieGrace is the minimum age a running/suspect task must reach before
	// the liveness reconcile may retire it as a zombie (reconcileZombies:
	// no settle + probe-dead backing session). Zero -> defaultZombieGrace.
	ZombieGrace time.Duration
	// OrphanGrace is the minimum age a nil-probe suspect task (Spec.Alive ==
	// nil, Declarative declared, backing session untracked) must reach before
	// the reincarnation-orphan adjudication (RetireOrphans) retires it as
	// failed. Covers restored (previous-life) suspects and this-life quiet
	// nil-probe subagents alike; tracked sessions and probe-carrying tasks
	// are never touched. Zero -> defaultOrphanGrace.
	OrphanGrace time.Duration
	// DefaultTTL is the unified reaper's fallback absolute lifetime for
	// tasks whose spec carries no explicit TTL — e.g. restored (previous-life) and
	// subagent tasks. reconcileTTL terminates+retires any ACTIVE task
	// (ALL states incl. suspect/undetached, ALL lifetime classes incl.
	// resident/interactive) once `now - anchor >= DefaultTTL` (or the task's own
	// spec.TTL when larger). The reaper is ALWAYS ON — there is no disable path:
	// any non-positive value (config or per-source) falls back to the package
	// 10-minute floor (async-task-lifetime 10.5), because "no wall" was never an
	// intended operating mode and an unconfigured caller silently loses its only
	// reclaim.
	DefaultTTL time.Duration
	// SessionTracker reports whether a task's Declarative.TaskID session is
	// still tracked by a live monitor (tmux). Wired post-construction via
	// SetSessionTracker (build_agent owns the ActionTool; TaskManager must
	// not import tool/action). May be nil.
	SessionTracker func(sessionID string) bool
}

// defaultTerminalTTL TaskManager is a deterministic (non-LLM) registry + scheduler for async tasks.
// The sync→async boundary is owned by each detector's detach signal (adaptive
// poll schedule); TaskManager holds no sync_wait knob.
// defaultTerminalTTL is the default grace period for retaining an exited task
// before pruning + resource reclamation (bounds the resume_task re-entry
// window for terminal subagent tasks).
const defaultTerminalTTL = 2 * time.Minute

// defaultManagerTTL is the unified reaper's floor applied when neither the
// task spec nor the operator config supplies a TTL, so no task is ever immortal
// (async-task-lifetime 10.5). The reaper is always on; there is no disable path.
const defaultManagerTTL = 10 * time.Minute

// TaskManager 是任务层的登记表与生命周期持有者：登记派生的任务、观察结算信号并驱动状态
// 迁移、按统一 TTL 回收，同时实现 TaskController 供任务工具使用。
//
// 它持有回调而不是具体宿主：onSpawn/onSettle/onInlineSettle/onBatchRetire/onCancel 由
// 接线方提供，把状态变化落到事实链与事件总线；spawnGate/auditGate 决定"能不能派"与
// "派成什么"，因此委派策略留在宿主，任务层只管登记与回收。方法对 nil 接收者安全。
type TaskManager struct {
	onBatchRetireAccepted  func([]BatchRetired) bool
	onRetireResendAccepted func([]BatchRetired) bool
	onSpawnAccepted        func(*Task) bool
	onStartAccepted        func(*Task) bool
	onRoundAccepted        func(*Task) bool
	onRoundAbortedAccepted func(*Task, SettleSignal) bool
	onInlineSettleAccepted func(*Task, SettleSignal) bool
	onSettleAccepted       func(*Task, SettleSignal) bool
	closing                bool
	stop                   chan struct{}
	closeDone              chan struct{}
	closeOnce              sync.Once
	operations             sync.WaitGroup
	watchers               sync.WaitGroup
	callbacks              sync.WaitGroup
	workers                sync.WaitGroup
	producers              []SettleDetector
	retryWake              chan struct{}
	deadlineWake           chan struct{}
	enableDeadlineReaper   bool
	mu                     sync.Mutex
	tasks                  map[string]*Task
	byKey                  map[string]string
	onSettle               func(task *Task, sig SettleSignal)
	onBatchRetire          func(batch []BatchRetired)
	onRetireResend         func(batch []BatchRetired)
	batchCollect           *[]BatchRetired
	onSpawn                func(task *Task)
	onInlineSettle         func(task *Task, sig SettleSignal)
	onCancel               func(task *Task)
	spawnGate              func() string
	auditGate              func(spec TaskSpec) string
	terminalTTL            time.Duration
	now                    func() time.Time
	zombieGrace            time.Duration
	orphanGrace            time.Duration
	defaultTTL             time.Duration
	isSessionTracked       func(sessionID string) bool
	// ttlSource is the  pull side for both manager-level TTL axes (design
	// ): installed by the composition root with the owner's committed-record
	// view, so a rotation reaches the reaper at its NEXT sweep/board read instead
	// of being pushed. nil → construction values only (standalone/direct-built
	// managers keep working).
	ttlSource atomic.Pointer[func() (terminal, defaultTTL time.Duration)]
}

// SetTTLSource NewTaskManager creates a TaskManager.
// SetTTLSource installs the  pull source for both manager-level TTL axes
// (terminal grace / unified-reaper fallback) — the replacement for the retired
// SetTerminalTTL/SetDefaultTTL pushes. The composition root injects the owner's
// committed-record view, so a hot rotation reaches the reaper at its NEXT sweep
// or board read with no per-manager write and no second authority.
func (tm *TaskManager) SetTTLSource(src func() (terminal, defaultTTL time.Duration)) {
	if tm == nil || src == nil {
		return
	}
	tm.ttlSource.Store(&src)
	tm.RecheckDeadlines()
}

// effTerminalTTL resolves the terminal grace period: the source wins when it
// carries a positive reading, otherwise the construction value is kept (the
// guard the retired setter had — a zero/absent record never resets a live
// grace period). Callers hold tm.mu; only the source load is lock-free.
func (tm *TaskManager) effTerminalTTL() time.Duration {
	if src := tm.ttlSource.Load(); src != nil {
		if d, _ := (*src)(); d > 0 {
			return d
		}
	}
	return tm.terminalTTL
}

// effDefaultTTL resolves the unified reaper's fallback lifetime. A positive
// source reading wins; a negative one is floored to defaultManagerTTL (age
// reclaim cannot be turned off, async-task-lifetime 10.5); zero means the
// record carries no opinion → construction value. Same lock discipline as
// effTerminalTTL.
func (tm *TaskManager) effDefaultTTL() time.Duration {
	if src := tm.ttlSource.Load(); src != nil {
		_, d := (*src)()
		if d > 0 {
			return d
		}
		if d < 0 {
			return defaultManagerTTL
		}
	}
	return tm.defaultTTL
}

// NewTaskManager 按配置构造管理器：任何非正数的时长取值一律回落到本包命名的默认值（终态
// 保留、僵尸宽限、孤儿宽限、默认 TTL）。0 在此是"未设置"而非"无限制"——若把它当作无限制，
// 未配置的调用方会在无人察觉的情况下失去回收能力。
func NewTaskManager(cfg TaskManagerConfig) *TaskManager {
	ttl := cfg.TerminalTTL
	if ttl <= 0 {
		ttl = defaultTerminalTTL
	}
	zg := cfg.ZombieGrace
	if zg <= 0 {
		zg = defaultZombieGrace
	}
	og := cfg.OrphanGrace
	if og <= 0 {
		og = defaultOrphanGrace
	}
	dttl := cfg.DefaultTTL
	if dttl <= 0 {
		dttl = defaultManagerTTL
	}
	tm := &TaskManager{
		onBatchRetireAccepted:  cfg.OnBatchRetireAccepted,
		onRetireResendAccepted: cfg.OnRetireResendAccepted,
		onSpawnAccepted:        cfg.OnSpawnAccepted,
		onStartAccepted:        cfg.OnStartAccepted,
		onRoundAccepted:        cfg.OnRoundAccepted,
		onRoundAbortedAccepted: cfg.OnRoundAbortedAccepted,
		onInlineSettleAccepted: cfg.OnInlineSettleAccepted,
		onSettleAccepted:       cfg.OnSettleAccepted,
		stop:                   make(chan struct{}), closeDone: make(chan struct{}),
		retryWake: make(chan struct{}, 1), deadlineWake: make(chan struct{}, 1),
		enableDeadlineReaper: cfg.EnableDeadlineReaper,
		tasks:                make(map[string]*Task),
		byKey:                make(map[string]string),
		onSettle:             cfg.OnSettle,
		onBatchRetire:        cfg.OnBatchRetire,
		onRetireResend:       cfg.OnRetireResend,
		onSpawn:              cfg.OnSpawn,
		onInlineSettle:       cfg.OnInlineSettle,
		onCancel:             cfg.OnCancel,
		spawnGate:            cfg.SpawnGate,
		auditGate:            cfg.AuditGate,
		terminalTTL:          ttl,
		zombieGrace:          zg,
		orphanGrace:          og,
		defaultTTL:           dttl,
		isSessionTracked:     cfg.SessionTracker,
		now:                  time.Now,
	}
	tm.workers.Add(1)
	go tm.deliveryWorker()
	if cfg.EnableDeadlineReaper {
		tm.workers.Add(1)
		go tm.deadlineWorker()
	}
	return tm
}

// Spawn starts a task and blocks until the first of {settle, detach}.
// - Settle first  → SpawnResult{Settled: true, Signal} (inline).
// - Detach first  → SpawnResult{Settled: false} (ack; tracked in background).
// - Equivalent task active → SpawnResult{Deduped: true} (no new task).
//
// Multiple concurrent Spawn calls each wait their own detector's window in
// parallel (blocking ≈ the slowest, not the sum).
func (tm *TaskManager) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
	if tm == nil {
		if detector != nil {
			detector.Cancel()
		}
		return SpawnResult{Blocked: "task manager unavailable"}
	}
	if !tm.beginOperation() {
		if detector != nil {
			detector.Cancel()
		}
		return SpawnResult{Blocked: "task manager is closing"}
	}
	defer tm.operations.Done()
	tm.pruneTerminal()
	tm.mu.Lock()
	if tm.closing {
		tm.mu.Unlock()
		if detector != nil {
			detector.Cancel()
		}
		return SpawnResult{Blocked: "task manager is closing"}
	}
	if spec.Key != "" {
		if id, ok := tm.byKey[spec.Key]; ok {
			if existing, ok := tm.tasks[id]; ok && existing.isActive() {
				tm.mu.Unlock()
				if detector != nil {
					detector.Cancel()
				}
				return SpawnResult{Task: existing, Deduped: true}
			}
		}
	}
	if tm.spawnGate != nil {
		if reason := tm.spawnGate(); reason != "" {
			tm.mu.Unlock()
			if detector != nil {
				detector.Cancel()
			}
			return SpawnResult{Blocked: reason}
		}
	}
	if tm.auditGate != nil {
		if reason := tm.auditGate(spec); reason != "" {
			tm.mu.Unlock()
			if detector != nil {
				detector.Cancel()
			}
			return SpawnResult{Blocked: reason}
		}
	}
	spec = cloneSpec(spec)
	task := &Task{ID: uuid.NewString(), Spec: spec, StartedAt: tm.now(), status: TaskRunning,
		firstSettle: make(chan SettleSignal, 1), windowDone: make(chan struct{}), watchDone: make(chan struct{}),
		roundID: 1, roundOwner: spec.RoundOwner, inputBatchID: spec.InputBatchID, pending: make(map[string]*pendingDelivery)}
	tm.tasks[task.ID] = task
	if detector != nil {
		tm.trackProducerLocked(detector)
	}
	if spec.Key != "" {
		tm.byKey[spec.Key] = task.ID
	}
	tm.mu.Unlock()
	accepted := true
	if tm.onSpawnAccepted != nil {
		accepted = tm.invoke(func() bool { return tm.onSpawnAccepted(task) })
	} else if tm.onSpawn != nil {
		accepted = tm.invoke(func() bool { tm.onSpawn(task); return true })
	}
	if !accepted {
		tm.removeUnstarted(task)
		if detector != nil {
			detector.Cancel()
		}
		return SpawnResult{Blocked: "task intent was not accepted"}
	}
	if spec.OnRoundStart != nil {
		spec.OnRoundStart(task)
	}
	if task.Status() == TaskCancelled {
		if detector != nil {
			detector.Cancel()
		}
		return tm.awaitWindow(task, detector, 1)
	}
	if spec.Start != nil {
		var err error
		spec, detector, err = spec.Start(task)
		if err != nil {
			if detector != nil {
				tm.mu.Lock()
				tm.trackProducerLocked(detector)
				tm.mu.Unlock()
				detector.Cancel()
			}
			tm.abortRound(task, 1, task.RoundOwner(), TaskFailed, err, true)
			return SpawnResult{Task: task, Blocked: err.Error()}
		}
	}
	if reason := tm.bindPrepared(task, spec, detector, 1, task.RoundOwner(), true); reason != "" {
		if detector != nil {
			detector.Cancel()
		}
		tm.abortRound(task, 1, task.RoundOwner(), TaskFailed, fmt.Errorf("%s", reason), true)
		return SpawnResult{Task: task, Blocked: reason}
	}
	return tm.awaitWindow(task, detector, 1)
}

func (tm *TaskManager) pruneProducersLocked() {
	live := tm.producers[:0]
	for _, detector := range tm.producers {
		select {
		case <-detector.Stopped():
		default:
			live = append(live, detector)
		}
	}
	for i := len(live); i < len(tm.producers); i++ {
		tm.producers[i] = nil
	}
	tm.producers = live
}

func (tm *TaskManager) trackProducerLocked(detector SettleDetector) {
	tm.pruneProducersLocked()
	stopped := detector.Stopped()
	if stopped != nil {
		for _, existing := range tm.producers {
			if existing.Stopped() == stopped {
				return
			}
		}
	}
	tm.producers = append(tm.producers, detector)
}

func (tm *TaskManager) removeUnstarted(t *Task) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.tasks, t.ID)
	if tm.byKey[t.Spec.Key] == t.ID {
		delete(tm.byKey, t.Spec.Key)
	}
}

func (tm *TaskManager) beginOperation() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.closing {
		return false
	}
	tm.operations.Add(1)
	return true
}

func (tm *TaskManager) invoke(fn func() bool) bool {
	tm.mu.Lock()
	if tm.closing {
		tm.mu.Unlock()
		return false
	}
	tm.callbacks.Add(1)
	tm.mu.Unlock()
	defer tm.callbacks.Done()
	return fn()
}

func (tm *TaskManager) bindPrepared(t *Task, spec TaskSpec, detector SettleDetector, round uint64, owner string, initial bool) string {
	tm.mu.Lock()
	closing := tm.closing
	tm.mu.Unlock()
	if closing {
		return "task manager is closing"
	}
	if d, ok := detector.(interface{ BeginRound(uint64) }); ok {
		d.BeginRound(round)
	}
	spec = cloneSpec(spec)
	if d, ok := detector.(interface{ DeclarativeParams() map[string]string }); ok && spec.Declarative != nil {
		if spec.Declarative.Params == nil {
			spec.Declarative.Params = make(map[string]string)
		}
		for k, v := range d.DeclarativeParams() {
			spec.Declarative.Params[k] = v
		}
	}
	if d, ok := detector.(interface{ RoundBaseline() int }); ok && spec.Declarative != nil {
		if spec.Declarative.Params == nil {
			spec.Declarative.Params = make(map[string]string)
		}
		spec.Declarative.Params["baseline"] = strconv.Itoa(d.RoundBaseline())
	}
	t.mu.Lock()
	if t.status == TaskCancelled {
		t.mu.Unlock()
		return "task was cancelled before activation"
	}
	oldSpec, oldDetector := t.Spec, t.detector
	oldProducer, oldOwner, oldSeq, oldWait, oldAnchor := t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied, t.ttlRenewedAt
	oldBatch := t.producerInputBatchID
	spec.RoundOwner = owner
	spec.InputBatchID = t.inputBatchID
	spec.Origin = oldSpec.Origin
	t.Spec, t.detector = spec, detector
	if !initial {
		t.ttlRenewedAt = tm.now()
	}
	t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied = round, owner, 0, false
	t.producerInputBatchID = t.inputBatchID
	if actual, ok := detector.(interface{ ProducerRound() uint64 }); ok {
		t.producerRound = actual.ProducerRound()
		if t.producerRound != round {
			t.producerOwner, t.signalSeq, t.producerWaitSatisfied = oldOwner, oldSeq, oldWait
			t.producerInputBatchID = oldBatch
		}
	}
	t.mu.Unlock()
	if detector != nil {
		tm.mu.Lock()
		tm.trackProducerLocked(detector)
		tm.mu.Unlock()
	}
	accepted := true
	if tm.onStartAccepted != nil {
		accepted = tm.invoke(func() bool { return tm.onStartAccepted(t) })
	}
	if !accepted {
		t.mu.Lock()
		t.Spec, t.detector = oldSpec, oldDetector
		t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied = oldProducer, oldOwner, oldSeq, oldWait
		t.producerInputBatchID = oldBatch
		t.ttlRenewedAt = oldAnchor
		t.mu.Unlock()
		return "task resource binding was not accepted"
	}
	tm.mu.Lock()
	closing = tm.closing
	tm.mu.Unlock()
	if closing {
		t.mu.Lock()
		t.Spec, t.detector = oldSpec, oldDetector
		t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied, t.ttlRenewedAt = oldProducer, oldOwner, oldSeq, oldWait, oldAnchor
		t.producerInputBatchID = oldBatch
		t.mu.Unlock()
		return "task manager is closing"
	}
	t.mu.Lock()
	if t.status == TaskCancelled {
		t.Spec, t.detector = oldSpec, oldDetector
		t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied, t.ttlRenewedAt = oldProducer, oldOwner, oldSeq, oldWait, oldAnchor
		t.producerInputBatchID = oldBatch
		t.mu.Unlock()
		return "task was cancelled before activation"
	}
	if !initial {
		t.retireWatchLocked()
		t.watchDone = make(chan struct{})
		t.watchStopped = false
	}
	t.firstSettle = make(chan SettleSignal, 1)
	t.inlineOffered = false
	t.windowClosed = false
	t.windowDone = make(chan struct{})
	t.aliveDetached = false
	t.serviceObserved = false
	t.retireNotified = false
	t.status = TaskRunning
	t.resuming = false
	done := t.watchDone
	activation := &detectorActivation{detector: detector, round: round, owner: owner, refresh: true, ready: make(chan struct{})}
	_, activatable := detector.(ActivatableDetector)
	deferred := t.recovering && activatable
	if deferred && detector != nil {
		t.deferredActivation = activation
	}
	t.mu.Unlock()
	if detector != nil {
		tm.mu.Lock()
		tm.watchers.Add(1)
		tm.mu.Unlock()
		if !deferred {
			tm.activateDetector(t, activation)
		}
		go func() { defer tm.watchers.Done(); tm.watch(t, detector, done, round, owner, activation.ready) }()
	}
	tm.RecheckDeadlines()
	return ""
}

func (tm *TaskManager) awaitWindow(t *Task, detector SettleDetector, round uint64) SpawnResult {
	t.mu.Lock()
	first, windowDone := t.firstSettle, t.windowDone
	t.mu.Unlock()
	var detach <-chan struct{}
	if detector != nil {
		detach = detector.Detached()
	}
	select {
	case sig := <-first:
		tm.closeCapturedWindow(t, first, windowDone, false, detector)
		if sig.Observation != "round_aborted" {
			tm.deliver(t, sig, detector, true, false)
		}
		return SpawnResult{Task: t, Settled: true, Signal: sig}
	case <-detach:
		tm.closeCapturedWindow(t, first, windowDone, true, detector)
		return SpawnResult{Task: t}
	case <-tm.stop:
		tm.closeCapturedWindow(t, first, windowDone, true, detector)
		return SpawnResult{Task: t, Blocked: "task manager is closing"}
	case <-windowDone:
		return SpawnResult{Task: t}
	}
}

// watch fences both state mutation and foreground handoff by producer identity.
func (tm *TaskManager) watch(t *Task, detector SettleDetector, done <-chan struct{}, round uint64, owner string, ready ...<-chan struct{}) {
	if len(ready) > 0 {
		select {
		case <-ready[0]:
		case <-done:
			return
		case <-tm.stop:
			return
		}
	}
	for {
		select {
		case <-tm.stop:
			return
		case <-done:
			return
		case sig, ok := <-detector.Settled():
			if !ok {
				return
			}
			t.mu.Lock()
			if t.watchDone != done {
				t.mu.Unlock()
				continue
			}
			if sig.Observation == "round_aborted" || (sig.RoundID == round && sig.ProducerRound > 0 && sig.ProducerRound != round && sig.Kind == SettleFailed) {
				previous := TaskStable
				if old := t.rollback; old != nil {
					previous = old.status
					t.Spec, t.detector, t.producerRound, t.producerOwner, t.signalSeq, t.producerWaitSatisfied = old.spec, old.detector, old.producerRound, old.producerOwner, old.signalSeq, old.producerWaitSatisfied
					t.producerInputBatchID = old.producerInputBatchID
					t.ttlRenewedAt, t.settledAt, t.aliveDetached, t.detachedAt = old.ttlRenewedAt, old.settledAt, old.aliveDetached, old.detachedAt
					t.rollback = nil
				}
				t.mu.Unlock()
				tm.abortRound(t, round, owner, previous, sig.Err, false, sig)
				continue
			}
			producerRound, producerOwner := t.producerRound, t.producerOwner
			if producerRound == 0 {
				producerRound = round
				producerOwner = owner
				t.producerRound, t.producerOwner = producerRound, producerOwner
			}
			if sig.RoundID != 0 && sig.RoundID != producerRound {
				t.mu.Unlock()
				continue
			}
			if sig.Seq > 0 && sig.Seq <= t.signalSeq {
				_, pending := t.pending[SignalSourceID(t.ID, producerRound, sig.Seq)]
				t.mu.Unlock()
				if !pending {
					tm.acknowledge(detector, sig)
				}
				continue
			}
			if isTerminalStatus(t.status) {
				t.mu.Unlock()
				continue
			}
			if sig.Seq == 0 {
				sig.Seq = t.signalSeq + 1
			}
			t.signalSeq = sig.Seq
			sig.RoundID, sig.ProducerRound, sig.RoundOwner = producerRound, producerRound, producerOwner
			if sig.InputBatchID == "" {
				sig.InputBatchID = t.producerInputBatchID
			}
			if sig.ObservedAt.IsZero() {
				sig.ObservedAt = tm.now()
			}
			if sig.SourceID == "" {
				sig.SourceID = SignalSourceID(t.ID, producerRound, sig.Seq)
			}
			if sig.Kind == SettleReady {
				sig.Ready = true
			}
			if sig.WaitFor == "" {
				sig.WaitFor = WaitForOf(t.Spec)
			}
			sig.WaitSatisfied = SatisfiesWait(t.Spec, sig)
			tm.freezeSignalLocked(t, &sig)
			if sig.WaitSatisfied {
				t.producerWaitSatisfied = true
			}
			if !sig.WaitSatisfied && sig.Observation == "" {
				sig.Observation = string(sig.Kind)
			}
			tm.applyStatusLocked(t, sig)
			legacyOffer := t.Spec.WaitFor == "" && t.Spec.Mode == ""
			if t.windowClosed || (!sig.WaitSatisfied && !legacyOffer) {
				t.mu.Unlock()
				tm.emitBackground(t, sig, detector)
				continue
			}
			windowDone := t.windowDone
			t.firstSettle <- sig
			t.inlineOffered = true
			t.mu.Unlock()
			select {
			case <-windowDone:
			case <-done:
				return
			case <-tm.stop:
				return
			}
		}
	}
}

// closeCapturedWindow closes only the foreground handoff observed by its caller.
func (tm *TaskManager) closeCapturedWindow(t *Task, first chan SettleSignal, done chan struct{}, drainToBg bool, detectors ...SettleDetector) {
	t.mu.Lock()
	current := done == t.windowDone
	if current && t.windowClosed {
		t.mu.Unlock()
		return
	}
	if current {
		t.windowClosed = true
	}
	var pending *SettleSignal
	if drainToBg {
		select {
		case sig := <-first:
			pending = &sig
		default:
		}
	}
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	t.mu.Unlock()
	if pending != nil {
		tm.emitBackground(t, *pending, detectors...)
	}
}

// emitBackground applies readiness suppression after the foreground condition is decided.
func (tm *TaskManager) emitBackground(t *Task, sig SettleSignal, detectors ...SettleDetector) {
	t.mu.Lock()
	if sig.RoundID == 0 {
		tm.identifySignalLocked(t, &sig)
	}
	tm.freezeSignalLocked(t, &sig)
	detector := t.detector
	if len(detectors) > 0 {
		detector = detectors[0]
	}
	currentProducer := sig.ProducerRound == t.producerRound
	service := currentProducer && LifetimeOf(t.Spec) == LifetimeService
	if service && (sig.Kind == SettleStable || sig.Kind == SettleReady || sig.Kind == SettleSuspect) {
		bare := t.Spec.Mode == "" && t.Spec.WaitFor == ""
		same := t.serviceObserved && t.serviceRound == sig.ProducerRound && t.serviceKind == sig.Kind && t.serviceOutput == sig.Output
		if t.aliveDetached && !sig.EndsRound() && (!bare || same) {
			t.mu.Unlock()
			tm.acknowledge(detector, sig)
			return
		}
		t.serviceObserved, t.serviceRound, t.serviceKind, t.serviceOutput = true, sig.ProducerRound, sig.Kind, sig.Output
		if !t.aliveDetached && (sig.Kind == SettleReady || (sig.Kind == SettleStable && sig.WaitSatisfied)) {
			t.aliveDetached, t.status, t.detachedAt = true, TaskAliveDetached, tm.now()
		}
	}
	t.mu.Unlock()
	tm.deliver(t, sig, detector, false, false)
}

func (tm *TaskManager) applyStatusLocked(t *Task, sig SettleSignal) {
	if isTerminalStatus(t.status) {
		return
	}
	t.result, t.err = sig.Output, sig.Err
	if sig.EndsRound() || sig.WaitSatisfied {
		t.settledAt = tm.now()
	}
	switch sig.Kind {
	case SettleCompleted:
		if sig.Err != nil {
			t.status = TaskFailed
		} else {
			t.status = TaskCompleted
		}
	case SettleFailed:
		t.status = TaskFailed
	case SettleStable, SettleReady, SettleInput:
		if sig.WaitSatisfied && t.status != TaskAliveDetached {
			t.status = TaskStable
		}
	case SettleSuspect:
		if sig.WaitSatisfied && t.status != TaskAliveDetached {
			t.status = TaskSuspect
		}
	}
}

// applyStatus updates task state for callers that already own a signal identity.
func (tm *TaskManager) applyStatus(t *Task, sig SettleSignal) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tm.applyStatusLocked(t, sig)
}

func (tm *TaskManager) activateDetector(t *Task, activation *detectorActivation) {
	defer close(activation.ready)
	t.activationMu.Lock()
	defer t.activationMu.Unlock()
	tm.mu.Lock()
	closing := tm.closing
	tm.mu.Unlock()
	t.mu.Lock()
	allowed := !closing && t.detector == activation.detector && t.status != TaskCancelled
	t.mu.Unlock()
	if !allowed {
		return
	}
	if active, ok := activation.detector.(ActivatableDetector); ok {
		active.Start()
	}
	if !activation.refresh {
		return
	}
	t.mu.Lock()
	if actual, ok := activation.detector.(interface{ ProducerRound() uint64 }); ok {
		t.producerRound = actual.ProducerRound()
		if t.producerRound == activation.round {
			t.producerOwner, t.signalSeq, t.rollback, t.producerWaitSatisfied = activation.owner, 0, nil, false
			t.producerInputBatchID = t.inputBatchID
		}
	} else {
		t.rollback = nil
	}
	t.mu.Unlock()
}

// BindDetector attaches an existing resource to its original producer round. Terminal tasks accept only acknowledgement-capable detectors; their business state is never reopened.
func (tm *TaskManager) BindDetector(id string, d SettleDetector) error {
	if tm == nil || id == "" {
		return fmt.Errorf("BindDetector: empty id")
	}
	if !tm.beginOperation() {
		return fmt.Errorf("BindDetector: task manager is closing")
	}
	defer tm.operations.Done()
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return fmt.Errorf("BindDetector: task %s not found", id)
	}
	var recovered *TaskSpec
	if provider, ok := d.(interface{ RecoveredSpec() TaskSpec }); ok {
		spec := cloneSpec(provider.RecoveredSpec())
		recovered = &spec
	}
	t.mu.Lock()
	terminal := isTerminalStatus(t.status)
	if terminal {
		if _, ackOnly := d.(SignalAcknowledger); !ackOnly {
			status := t.status
			t.mu.Unlock()
			return fmt.Errorf("BindDetector: task %s already finalized (%s)", id, status)
		}
	}
	if t.detector != nil {
		if t.detector == d {
			t.mu.Unlock()
			return nil
		}
		t.mu.Unlock()
		return fmt.Errorf("BindDetector: task %s already has a detector", id)
	}
	oldSpec := t.Spec
	if recovered != nil {
		recovered.Origin = oldSpec.Origin
		t.Spec = *recovered
	}
	t.detector = d
	if t.producerRound == 0 && !terminal {
		t.producerRound = t.roundID
		if t.producerRound == 0 {
			t.producerRound, t.roundID = 1, 1
		}
		t.producerInputBatchID = t.inputBatchID
	}
	if t.watchStopped {
		t.watchDone = make(chan struct{})
		t.watchStopped = false
	}
	done, round, owner := t.watchDone, t.producerRound, t.producerOwner
	activation := &detectorActivation{detector: d, round: round, owner: owner, ready: make(chan struct{})}
	_, activatable := d.(ActivatableDetector)
	deferred := t.recovering && activatable
	if deferred && d != nil && !terminal {
		t.deferredActivation = activation
	}
	t.mu.Unlock()
	if d != nil {
		tm.mu.Lock()
		tm.trackProducerLocked(d)
		tm.mu.Unlock()
	}
	if recovered != nil && tm.onStartAccepted != nil && !tm.invoke(func() bool { return tm.onStartAccepted(t) }) {
		t.mu.Lock()
		t.Spec, t.detector = oldSpec, nil
		t.deferredActivation = nil
		t.mu.Unlock()
		if d != nil {
			d.Cancel()
		}
		return fmt.Errorf("BindDetector: task %s recovered binding was not accepted", id)
	}
	t.mu.Lock()
	for _, p := range t.pending {
		if p.signal.ProducerRound == round {
			p.detector = d
		}
	}
	t.mu.Unlock()
	if d != nil && !terminal {
		tm.mu.Lock()
		tm.watchers.Add(1)
		tm.mu.Unlock()
		if !deferred {
			tm.activateDetector(t, activation)
		}
		go func() { defer tm.watchers.Done(); tm.watch(t, d, done, round, owner, activation.ready) }()
	}
	tm.RecheckDeadlines()
	return nil
}

// ResumeRestoredStart prepares an accepted intent that has no bound resource, keeping its original intent identity.
func (tm *TaskManager) ResumeRestoredStart(id string) error {
	if tm == nil || !tm.beginOperation() {
		return fmt.Errorf("task manager is closing")
	}
	defer tm.operations.Done()
	tm.mu.Lock()
	t := tm.tasks[id]
	if t == nil {
		tm.mu.Unlock()
		return fmt.Errorf("task %s not found", id)
	}
	t.mu.Lock()
	tm.mu.Unlock()
	if t.resuming || t.detector != nil || t.producerRound != 0 || t.Spec.Start == nil || isTerminalStatus(t.status) {
		t.mu.Unlock()
		return fmt.Errorf("task %s has no unbound recoverable intent", id)
	}
	spec, round, owner := cloneSpec(t.Spec), t.roundID, t.roundOwner
	t.resuming, t.status = true, TaskRunning
	t.mu.Unlock()
	prepared, detector, err := spec.Start(t)
	if err != nil {
		tm.cancelUnadoptedDetector(t, detector)
		tm.abortRound(t, round, owner, TaskFailed, err, true)
		return fmt.Errorf("task %s restore start: %w", id, err)
	}
	if reason := tm.bindPrepared(t, prepared, detector, round, owner, true); reason != "" {
		tm.cancelUnadoptedDetector(t, detector)
		tm.abortRound(t, round, owner, TaskFailed, fmt.Errorf("%s", reason), true)
		return fmt.Errorf("task %s restore start: %s", id, reason)
	}
	t.mu.Lock()
	first, done := t.firstSettle, t.windowDone
	t.mu.Unlock()
	tm.closeCapturedWindow(t, first, done, true, detector)
	return nil
}

// RestoreTask 从已持久化的事实重建任务，返回登记在用的任务对象：沿用其 id、声明、派生时刻与状态；
// 状态为 alive_detached 时同时标记已脱离，使其继续享受脱离后信号抑制的语义。
//
// - 结算窗口标记为已关闭：重建出的任务不重启观察，因而不会因"重启后没人在看"被误判为静默或僵尸；终态判定与 TTL 回收照常生效。
// - 幂等且不覆盖：同一 id 已在表内时直接返回既有任务，在途的真实状态不被重建值改写；spec.Key 同样只在无人占用时登记。
// - 管理器为 nil 或 id 为空时返回 nil；startedAt 为零值时取当前时刻。
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func (tm *TaskManager) RestoreTask(id string, spec TaskSpec, startedAt time.Time, status TaskStatus) *Task {
	if tm == nil || id == "" {
		return nil
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	tk := &Task{
		ID:           id,
		Spec:         cloneSpec(spec),
		StartedAt:    startedAt,
		status:       status,
		windowClosed: true,
		watchDone:    make(chan struct{}),
		firstSettle:  make(chan SettleSignal, 1),
		windowDone:   make(chan struct{}),
		pending:      make(map[string]*pendingDelivery),
		recovering:   tm.enableDeadlineReaper,
		roundID:      1, producerRound: 1, roundOwner: spec.RoundOwner, producerOwner: spec.RoundOwner,
		inputBatchID: spec.InputBatchID, producerInputBatchID: spec.InputBatchID,
	}
	if d := spec.Declarative; d != nil {
		if d.InputBatchID != "" {
			tk.inputBatchID = d.InputBatchID
		}
		tk.producerInputBatchID = d.ProducerInputBatchID
		if d.RoundID > 0 {
			tk.roundID = d.RoundID
		}
		if d.ProducerRound > 0 {
			tk.producerRound = d.ProducerRound
		} else if d.RoundID > 0 {
			tk.producerRound = 0
		} else {
			tk.producerRound = tk.roundID
		}
		if d.RoundOwner != "" {
			tk.roundOwner = d.RoundOwner
		}
		tk.producerOwner = d.ProducerOwner
		if tk.producerOwner == "" && tk.producerRound > 0 {
			tk.producerOwner = tk.roundOwner
		}
		if tk.producerRound == 0 {
			tk.producerOwner = ""
			tk.producerInputBatchID = ""
		} else if tk.producerRound == tk.roundID && tk.producerInputBatchID == "" {
			tk.producerInputBatchID = tk.inputBatchID
		}
		tk.signalSeq = d.SignalSeq
		tk.producerWaitSatisfied = d.ProducerWaitSatisfied
		if d.TTLAnchorMilli > 0 {
			tk.ttlRenewedAt = time.UnixMilli(d.TTLAnchorMilli)
		}
	}
	if isTerminalStatus(status) {
		tk.settledAt = tm.now()
	}
	if status == TaskAliveDetached {
		tk.aliveDetached = true
	}
	tm.mu.Lock()
	defer func() { tm.mu.Unlock(); tm.RecheckDeadlines() }()
	if tm.closing {
		return nil
	}
	if _, exists := tm.tasks[id]; exists {
		return tm.tasks[id]
	}
	tm.tasks[id] = tk
	if spec.Key != "" {
		if _, dup := tm.byKey[spec.Key]; !dup {
			tm.byKey[spec.Key] = id
		}
	}
	return tk
}

// MarkTaskRunning（R3 2.6，TaskID 桥）：重挂发现会话存活时把重建的 suspect
// 任务提升回 running（探测裁决的确定性分支；suspect→running 单向，不动其他态）。
func (tm *TaskManager) MarkTaskRunning(id string) bool {
	if tm == nil || id == "" {
		return false
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tk, ok := tm.tasks[id]
	if !ok {
		return false
	}
	tk.mu.Lock()
	defer tk.mu.Unlock()
	if tk.status != TaskSuspect {
		return false
	}
	tk.status = TaskRunning
	return true
}

// TasksSnapshot returns the registry without probing, retirement or pruning.
func (tm *TaskManager) TasksSnapshot() []*Task {
	if tm == nil {
		return nil
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		out = append(out, t)
	}
	return out
}

// Get returns a task by id.
func (tm *TaskManager) Get(id string) (*Task, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.tasks[id]
	return t, ok
}

// reconcileTTL is the unified absolute-lifetime reaper (async-task-lifetime
// 10.3). It is the ONLY age-based termination that reaches EVERY active state —
// including suspect and never-detached tasks — and every lifetime class (job AND
// resident/interactive), against an absolute anchor (spawn, refreshed by a
// reentrant send/resume in ). It closes the production blind spot where the
// old detached-gated walls (markStaleDetached / enforceJobDeadline, removed in
// ) never fired for a task that went quiet without detaching (56bf24c3,
// stuck on the board 23h). Effective lifetime = the task's own spec.TTL when >0,
// else the manager DefaultTTL (floored at defaultManagerTTL by NewTaskManager —
// the reaper is ALWAYS ON; there is no configured-off state). On expiry it
// cancels the backing work via
// the owner's detector.Cancel (kills the tmux session / goroutine) OUTSIDE the
// lock, then retires the task failed ONCE through finalizeRetired (SettleFailed),
// so it leaves the board. finalize's terminal fence makes concurrent/repeat
// reconciliation a no-op — no double settlement.
// Silent-leak block: a restored task carries no detector, so TTL retirement
// has no Cancel entry into its backing session (a tmux window keeps running
// with nobody reaping it). The warning rides the retire settle's OWN output —
// one observable event per victim, batch collapse stays exclusive (no extra
// per-task notify through the collect mode), and session-level reclaim itself
// stays future work.
func (tm *TaskManager) reconcileTTL() {
	tm.mu.Lock()
	var victims []*Task
	var detectors []SettleDetector
	now := tm.now()
	for _, t := range tm.tasks {
		t.mu.Lock()
		ttl := t.Spec.TTL
		if ttl <= 0 {
			ttl = tm.effDefaultTTL()
		}
		age := now.Sub(t.ttlAnchor())
		expired := !t.recovering && ttl > 0 && !isTerminalStatus(t.status) && !t.ttlAnchor().IsZero() && age >= ttl
		var det SettleDetector
		if expired {
			det = t.detector
		}
		t.mu.Unlock()
		if expired {
			victims = append(victims, t)
			detectors = append(detectors, det)
		}
	}
	tm.mu.Unlock()
	for i, t := range victims {
		out := "(ttl-expired: task exceeded its absolute lifetime - cancelled by owner and retired)"
		if det := detectors[i]; det != nil {
			det.Cancel()
		} else {
			t.mu.Lock()
			taskID := ""
			if t.Spec.Declarative != nil {
				taskID = t.Spec.Declarative.TaskID
			}
			t.mu.Unlock()
			if taskID != "" {
				out = fmt.Sprintf("%s ⚠恢复任务无结算探测器可取消，后台会话 %q 可能仍在运行且无人回收", out, taskID)
			}
		}
		tm.finalizeRetired(t, out, nil)
	}
}

// reconcileDetached retires alive_detached tasks whose liveness probe
// (Spec.Alive) reports the backing session gone - e.g. a tmux session
// killed out-of-band after the task had already settled once into
// alive_detached. Without this, such entries linger on the board forever:
// pruneTerminal only reaps terminal states, and nothing reconciles
// alive_detached against ground truth. The retire follows normal
// completion semantics (status -> completed, settledAt stamped, exactly
// one final onSettle notification) so downstream TTL pruning applies
// unchanged. Probe calls run outside tm.mu (they shell out to tmux); the
// re-check under t.mu makes double-retire impossible.
func (tm *TaskManager) reconcileDetached() {
	finishBatch := tm.beginBatchRetire()
	defer finishBatch()
	tm.reconcileTTL()
	tm.reconcileZombies()
	tm.mu.Lock()
	var candidates []*Task
	for _, t := range tm.tasks {
		t.mu.Lock()
		need := !t.recovering && t.status == TaskAliveDetached && t.Spec.Alive != nil
		t.mu.Unlock()
		if need {
			candidates = append(candidates, t)
		}
	}
	tm.mu.Unlock()
	for _, t := range candidates {
		t.mu.Lock()
		if t.status != TaskAliveDetached {
			t.mu.Unlock()
			continue
		}
		probe := t.Spec.Alive
		t.mu.Unlock()
		if probe() {
			continue
		}
		out := "(backing session gone - auto-retired by liveness reconcile)"
		t.mu.Lock()
		if t.status == TaskAliveDetached {
			t.mu.Unlock()
			tm.finalize(t, SettleCompleted, out, nil)
		} else {
			t.mu.Unlock()
		}
	}
	tm.reconcileRetireNotify()
}

// reconcileRetireNotify is the board's terminal-notification backstop: every
// board cycle it hands terminal tasks whose notification was never confirmed
// handed off to OnRetireResend, then marks them. The grace window lets the
// synchronous finalize dispatch run to completion first (both dispatch paths
// set retireNotified when their callback RETURNS), so the pass only sees
// genuinely unconfirmed terminals — an in-flight kill inside a callback, or a
// process replacement that restored a terminal task with the flag at zero.
// Marking happens AFTER the callback returns, for the same reason as finalize.
func (tm *TaskManager) reconcileRetireNotify() {
	if tm == nil || (tm.onRetireResend == nil && tm.onRetireResendAccepted == nil) {
		return
	}
	now := tm.now()
	tm.mu.Lock()
	var batch []BatchRetired
	for _, t := range tm.tasks {
		t.mu.Lock()
		if !t.recovering && isTerminalStatus(t.status) && !t.retireNotified && len(t.pending) == 0 && now.Sub(t.settledAt) > retireNotifyGrace {
			kind := SettleFailed
			if t.status == TaskCompleted {
				kind = SettleCompleted
			}
			seq := t.signalSeq
			if seq == 0 {
				seq = 1
			}
			sig := SettleSignal{Kind: kind, Output: t.result, Err: t.err, Final: true, Seq: seq, ObservedAt: t.settledAt}
			if t.status == TaskCancelled {
				sig.Observation, sig.Lineage = "cancelled", LineageRetired
			}
			tm.identifySignalLocked(t, &sig)
			tm.holdBatchSignalLocked(t, sig, t.detector)
			batch = append(batch, BatchRetired{Task: t, Sig: sig})
		}
		t.mu.Unlock()
	}
	tm.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	accepted := tm.invoke(func() bool {
		if tm.onRetireResendAccepted != nil {
			return tm.onRetireResendAccepted(batch)
		}
		tm.onRetireResend(batch)
		return true
	})
	tm.finishBatchDeliveries(batch, accepted)
}

// SetSessionTracker wires the live-session tracking signal post-construction
// (build_agent owns the ActionTool; TaskManager must not import tool/action).
func (tm *TaskManager) SetSessionTracker(fn func(sessionID string) bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.isSessionTracked = fn
}

// BatchRetired finalize is the SINGLE terminal-transition entry point (hardening-review-
// batch2 1.5): it stamps status/result/settledAt from one kind+err pair so the
// in-memory state, the SettleSignal kind, the WAL settle_status (mapper), and
// the feedback polarity cannot diverge — the 7080753 wall stamped TaskFailed
// in memory but signalled SettleCompleted, and the mapper's default branch
// recorded orphan/zombie SettleFailed as completed. Called with t.mu NOT held
// (takes it briefly); onSettle fires outside the lock, same as all emitters.
// Only reconcile-class terminals route through here; the sync-wait window's
// normal settle path (applyStatus) is unchanged.
// finalizeRetired is the retirement-path finalize (zombie/orphan/TTL-expired):
// the settle signal must NOT inherit the task's original trigger lineage (a
// user-spawned task retired by bookkeeping would otherwise be delivered back
// to the user as if it were the user's awaited result — leak, ).
// Lineage is downgraded to the dedicated "task-retired" stamp, which the
// fail-closed delivery gate holds by default.
// BatchRetired is one task settled inside a batch retirement: the
// per-task fact (record-only chain entry + registry fold) stays per-task; only
// the bus-side notification is collapsed.
type BatchRetired struct {
	Task *Task
	Sig  SettleSignal
}

// originKeyInvocationID is the Spec.Origin key carrying the delegation
// invocation a task was spawned under. Same wire key as
// agent.metaKeyInvocationID (the task layer must not import the agent layer);
// the batch split reads it to tell attributed retires from unattributed ones.
const originKeyInvocationID = "invocation_id"

// beginBatchRetire switches finalize into collect mode for the duration of a
// reconcile/orphan loop. NESTED-SAFE: reconcileZombies internally calls
// RetireOrphans — an inner begin reuses the outer collector and its finish is
// a no-op; only the outermost finish delivers the batch to OnBatchRetire.
func (tm *TaskManager) beginBatchRetire() (finish func()) {
	tm.mu.Lock()
	if tm.batchCollect != nil {
		tm.mu.Unlock()
		return func() {}
	}
	if tm.onBatchRetire == nil && tm.onBatchRetireAccepted == nil {
		tm.mu.Unlock()
		return func() {}
	}
	batch := make([]BatchRetired, 0, 8)
	tm.batchCollect = &batch
	tm.mu.Unlock()
	return func() {
		tm.mu.Lock()
		tm.batchCollect = nil
		tm.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		accepted := tm.invoke(func() bool {
			if tm.onBatchRetireAccepted != nil {
				return tm.onBatchRetireAccepted(batch)
			}
			tm.onBatchRetire(batch)
			return true
		})
		tm.finishBatchDeliveries(batch, accepted)
	}
}

func (tm *TaskManager) holdBatchSignalLocked(t *Task, sig SettleSignal, detector SettleDetector) {
	if t.pending == nil {
		t.pending = make(map[string]*pendingDelivery)
	}
	t.pending[sig.SourceID] = &pendingDelivery{signal: sig, detector: detector, inFlight: true, restored: t.recovering}
}

func (tm *TaskManager) finishBatchDeliveries(batch []BatchRetired, accepted bool) {
	for _, r := range batch {
		r.Task.mu.Lock()
		pending := r.Task.pending[r.Sig.SourceID]
		var detector SettleDetector
		if pending != nil {
			detector = pending.detector
			pending.inFlight = false
			if accepted {
				delete(r.Task.pending, r.Sig.SourceID)
				if r.Task.signalEndsCurrentRoundLocked(r.Sig) {
					r.Task.retireNotified = true
				}
			}
		}
		r.Task.mu.Unlock()
		if accepted {
			tm.acknowledge(detector, r.Sig)
		} else if pending != nil {
			tm.attemptDelivery(r.Task, pending)
		}
	}
	tm.RecheckDeadlines()
}

// finalizeRetired settles a retired task. Retirement attributes on the SIGNAL
// (LineageRetired), never on Spec.Origin: Origin is the spawn-time provenance
// identity and stays immutable, so a resumed task's later settles keep their
// original lineage and lock-free readers race with nothing.
func (tm *TaskManager) finalizeRetired(t *Task, output string, err error) {
	tm.finalizeWithSignal(t, SettleSignal{Kind: SettleFailed, Output: output, Err: err, Lineage: LineageRetired})
}

func (tm *TaskManager) finalize(t *Task, kind SettleKind, output string, err error) {
	tm.finalizeWithSignal(t, SettleSignal{Kind: kind, Output: output, Err: err})
}

// finalizeWithSignal is the production-side split point: a retire of a task
// attributed to a delegation invocation must NOT collapse into the batch
// summary — the summary is published to the shared bus only, so the owning
// invocation loop would never see its settle and the delivery-accounting
// barrier could never reach quiescence. Attributed settles keep the per-task
// onSettle path (which routes onto the bound invocation bus).
func (tm *TaskManager) finalizeWithSignal(t *Task, sig SettleSignal) {
	t.mu.Lock()
	if isTerminalStatus(t.status) {
		t.mu.Unlock()
		return
	}
	switch {
	case sig.Kind == SettleFailed || sig.Err != nil:
		t.status, sig.Kind = TaskFailed, SettleFailed
	case sig.Kind == SettleCompleted:
		t.status = TaskCompleted
	default:
		t.status, sig.Kind = TaskFailed, SettleFailed
	}
	t.result, t.err, t.settledAt = sig.Output, sig.Err, tm.now()
	tm.identifySignalLocked(t, &sig)
	t.retireWatchLocked()
	if !t.windowClosed && !t.inlineOffered && t.firstSettle != nil {
		t.firstSettle <- sig
		t.inlineOffered = true
		t.mu.Unlock()
		tm.RecheckDeadlines()
		return
	}
	detector, attributed := t.detector, sig.RoundOwner != "" || t.Spec.Origin[originKeyInvocationID] != ""
	t.mu.Unlock()
	tm.mu.Lock()
	if tm.batchCollect != nil && !attributed {
		t.mu.Lock()
		tm.holdBatchSignalLocked(t, sig, detector)
		t.mu.Unlock()
		*tm.batchCollect = append(*tm.batchCollect, BatchRetired{Task: t, Sig: sig})
		tm.mu.Unlock()
		tm.RecheckDeadlines()
		return
	}
	tm.mu.Unlock()
	tm.deliver(t, sig, detector, false, false)
	tm.RecheckDeadlines()
}

func (t *Task) retireWatchLocked() {
	if t.watchDone != nil && !t.watchStopped {
		close(t.watchDone)
		t.watchStopped = true
	}
}

func (tm *TaskManager) identifySignalLocked(t *Task, sig *SettleSignal) {
	if t.roundID == 0 && t.producerRound == 0 {
		t.roundID = 1
	}
	if sig.RoundID == 0 {
		sig.RoundID = t.producerRound
		if sig.RoundID == 0 {
			sig.RoundID = t.roundID
			if sig.RoundID == 0 {
				sig.RoundID = 1
			}
		}
	}
	if sig.ProducerRound == 0 {
		sig.ProducerRound = t.producerRound
	}
	if sig.RoundOwner == "" {
		sig.RoundOwner = t.producerOwner
	}
	if sig.Seq == 0 {
		t.signalSeq++
		sig.Seq = t.signalSeq
	}
	if sig.ObservedAt.IsZero() {
		sig.ObservedAt = tm.now()
	}
	if sig.SourceID == "" {
		sig.SourceID = SignalSourceID(t.ID, sig.RoundID, sig.Seq)
	}
	if sig.WaitFor == "" {
		sig.WaitFor = WaitForOf(t.Spec)
	}
	if sig.InputBatchID == "" {
		sig.InputBatchID = t.producerInputBatchID
		if t.producerRound == 0 {
			sig.InputBatchID = t.inputBatchID
		}
	}
	sig.WaitSatisfied = SatisfiesWait(t.Spec, *sig)
	tm.freezeSignalLocked(t, sig)
	if sig.WaitSatisfied && sig.RoundID == t.producerRound {
		t.producerWaitSatisfied = true
	}
}

func (tm *TaskManager) freezeSignalLocked(t *Task, sig *SettleSignal) {
	if sig.Declarative != nil {
		return
	}
	d := t.specSnapshotLocked().Declarative
	if d == nil {
		return
	}
	d.RoundID, d.RoundOwner, d.ProducerRound = sig.RoundID, sig.RoundOwner, sig.ProducerRound
	d.InputBatchID, d.SignalSeq, d.WaitFor = sig.InputBatchID, sig.Seq, sig.WaitFor
	if sig.Observation != "round_aborted" && sig.ProducerRound > 0 {
		d.ProducerInputBatchID = sig.InputBatchID
	}
	sig.Declarative = d
}

func (t *Task) signalEndsCurrentRoundLocked(sig SettleSignal) bool {
	if !sig.EndsRound() {
		return false
	}
	if t.producerRound == 0 {
		return sig.ProducerRound == 0 && sig.RoundID == t.roundID
	}
	return sig.RoundID == t.producerRound && sig.ProducerRound == t.producerRound
}

// sessionTrackerFn snapshots the wired tracker (lock-safe read).
func (tm *TaskManager) sessionTrackerFn() func(sessionID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.isSessionTracked
}

// TerminalTTL reports the live terminal grace period (introspection;  makes
// it a READ of the same resolution the reaper uses — the owner's record source
// when installed, else the construction value.
// rollback tests read this, not the config field). Same tm.mu lock as
// the reaper paths, so what a test sees is what pruneTerminal applied.
func (tm *TaskManager) TerminalTTL() time.Duration {
	if tm == nil {
		return 0
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.effTerminalTTL()
}

// DefaultTTL reports the unified reaper's current fallback lifetime (the manager
// floor used when a task's spec carries no explicit TTL). Exposed so board
// rendering shows exactly what reconcileTTL will honor (async-task-lifetime 10.6).
func (tm *TaskManager) DefaultTTL() time.Duration {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.effDefaultTTL()
}

// RetireOrphans adjudicates reincarnation-orphan suspect tasks:
// nil-probe (Spec.Alive == nil) + declared (Declarative != nil) + backing
// session untracked + age >= orphanGrace → terminal failed via the same
// settle-once path as zombie retirement. Restored previous-life suspects
// qualify by their carried StartedAt (channel 1: called once at rebuild,
// before the suspect→running promotion, so orphans never get promoted then
// re-adjudicated); the same criterion re-runs from reconcileZombies as the
// runtime backstop (channel 2). Probe-carrying tasks and tracked sessions are
// never touched; generic display tasks without Declarative are never touched.
// Returns the number retired.
func (tm *TaskManager) RetireOrphans(isTracked func(sessionID string) bool) int {
	if tm == nil || !tm.beginOperation() {
		return 0
	}
	defer tm.operations.Done()
	now := tm.now()
	tm.mu.Lock()
	var candidates []*Task
	for _, t := range tm.tasks {
		t.mu.Lock()
		need := !t.recovering && t.status == TaskSuspect &&
			t.Spec.Alive == nil &&
			t.Spec.Declarative != nil &&
			now.Sub(t.StartedAt) >= tm.orphanGrace
		t.mu.Unlock()
		if need {
			candidates = append(candidates, t)
		}
	}
	tm.mu.Unlock()

	retired := 0
	finishBatch := tm.beginBatchRetire()
	for _, t := range candidates {
		t.mu.Lock()
		taskID := ""
		if t.Spec.Declarative != nil {
			taskID = t.Spec.Declarative.TaskID
		}
		tracked := taskID != "" && isTracked != nil && isTracked(taskID)
		if tracked || t.status != TaskSuspect {
			t.mu.Unlock()
			continue
		}
		out := "(reincarnation orphan: nil-probe suspect untracked beyond grace - retired by orphan adjudication)"
		t.mu.Unlock()
		retired++
		tm.finalizeRetired(t, out, nil)
	}
	finishBatch()
	return retired
}

// reconcileZombies closes the running-state blind spot of reconcileDetached:
// a task whose detector never emits a settle (frozen output pipe, tmux
// session lost out-of-band) lingered on the board as [running] forever —
// observed live as a millisecond probe stuck for 4h+. A running/suspect task
// older than zombieGrace whose Alive probe reports the backing session gone
// is retired through the terminal failed path, so onSettle notifies exactly
// once and pruneTerminal reclaims the detector. Nil-probe tasks (subagents)
// are never touched; a live probe protects a quiet long-runner at any age.
func (tm *TaskManager) reconcileZombies() {
	now := tm.now()
	if fn := tm.sessionTrackerFn(); fn != nil {
		tm.RetireOrphans(fn)
	}
	tm.mu.Lock()
	var candidates []*Task
	for _, t := range tm.tasks {
		t.mu.Lock()
		need := !t.recovering && t.Spec.Alive != nil &&
			(t.status == TaskRunning || t.status == TaskSuspect) &&
			now.Sub(t.StartedAt) >= tm.zombieGrace
		t.mu.Unlock()
		if need {
			candidates = append(candidates, t)
		}
	}
	tm.mu.Unlock()
	finishBatch := tm.beginBatchRetire()
	for _, t := range candidates {
		t.mu.Lock()
		st := t.status
		if st != TaskRunning && st != TaskSuspect {
			t.mu.Unlock()
			continue
		}
		probe := t.Spec.Alive
		t.mu.Unlock()
		if probe() {
			continue
		}
		out := "(zombie retired: no settle and backing session gone beyond grace - auto-retired by liveness reconcile)"
		t.mu.Lock()
		st = t.status
		if st == TaskRunning || st == TaskSuspect {
			t.mu.Unlock()
			tm.finalizeRetired(t, out, nil)
		} else {
			t.mu.Unlock()
		}
	}
	finishBatch()
}

// pruneTerminal removes exited tasks (completed/failed/cancelled/dead) whose
// grace period has elapsed, reclaiming each victim's detector resources
// (goroutine/context/tmux session). It is lazy — invoked from List and Spawn —
// and never touches live tasks. detector.Cancel() is called OUTSIDE the
// registry lock so releasing a resource (e.g. killing a tmux session) never
// blocks other task operations; Cancel() is idempotent for already-exited work.
func (tm *TaskManager) pruneTerminal() {
	now := tm.now()
	tm.mu.Lock()
	var victims []*Task
	for id, t := range tm.tasks {
		if t.isTerminalExpired(now, tm.effTerminalTTL()) {
			victims = append(victims, t)
			delete(tm.tasks, id)
			if t.Spec.Key != "" && tm.byKey[t.Spec.Key] == id {
				delete(tm.byKey, t.Spec.Key)
			}
		}
	}
	tm.pruneProducersLocked()
	tm.mu.Unlock()
	for _, t := range victims {
		t.mu.Lock()
		detector := t.detector
		t.mu.Unlock()
		if detector != nil {
			detector.Cancel()
		}
	}
}

// RestoreSettlement restores terminal output and its actual settlement time from persisted facts.
func (t *Task) RestoreSettlement(output string, err error, settledAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result, t.err = output, err
	if !settledAt.IsZero() {
		t.settledAt = settledAt
	}
}

// SettledAtUnixNano reads the terminal-moment stamp under the task lock —
// stable resend ids derive from it so board cycles and restarts collapse to
// one durable delivery.
func (t *Task) SettledAtUnixNano() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settledAt.UnixNano()
}

// List returns a snapshot of all tracked tasks.
func (tm *TaskManager) List() []*Task {
	if tm == nil {
		return nil
	}
	if !tm.beginOperation() {
		return tm.TasksSnapshot()
	}
	defer tm.operations.Done()
	tm.reconcileDetached()
	tm.pruneTerminal()
	return tm.TasksSnapshot()
}

// Cancel stops a task's underlying work and marks it cancelled.
func (tm *TaskManager) Cancel(id string) bool {
	if tm == nil {
		return false
	}
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return false
	}
	t.activationMu.Lock()
	t.mu.Lock()
	if t.status == TaskCancelled {
		t.mu.Unlock()
		t.activationMu.Unlock()
		return false
	}
	wasActive := !isTerminalStatus(t.status)
	sig := SettleSignal{Kind: SettleFailed, Output: "task cancelled", Final: true, Lineage: LineageRetired, Observation: "cancelled"}
	tm.identifySignalLocked(t, &sig)
	notify := !wasActive || t.windowClosed || t.inlineOffered || t.firstSettle == nil
	if wasActive && !t.windowClosed && !t.inlineOffered && t.firstSettle != nil {
		t.firstSettle <- sig
		t.inlineOffered = true
	}
	detector := t.detector
	t.status, t.settledAt, t.result, t.err = TaskCancelled, tm.now(), sig.Output, sig.Err
	t.retireWatchLocked()
	t.mu.Unlock()
	if detector != nil {
		detector.Cancel()
	}
	t.activationMu.Unlock()
	if tm.onCancel != nil {
		tm.invoke(func() bool { tm.onCancel(t); return true })
	}
	if notify {
		tm.deliver(t, sig, detector, false, false)
	}
	tm.RecheckDeadlines()
	return true
}

// RenewTTLBySession resets the TTL anchor for the ACTIVE task whose backing
// session matches sessionID (async-task-lifetime 10.4). A write-type reentry
// (exec op=send) extends the task's absolute lifetime by moving the anchor to
// now, so the reaper measures ttl from this refresh rather than the original
// spawn. Read-only touches (op=peek) MUST NOT call this. Best-effort: a session
// with no matching active task (already reaped, or untracked) returns false.
func (tm *TaskManager) RenewTTLBySession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	now := tm.now()
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for _, t := range tm.tasks {
		t.mu.Lock()
		match := !isTerminalStatus(t.status) && t.Spec.Declarative != nil && t.Spec.Declarative.TaskID == sessionID
		if match {
			t.ttlRenewedAt = now
		}
		t.mu.Unlock()
		if match {
			tm.RecheckDeadlines()
			return true
		}
	}
	return false
}

// Relaunch re-spawns an equivalent task from the original task's spec. It runs
// the spec's Relaunch closure (set by the tool that spawned it — e.g. ActionTool
// re-runs the command in a fresh session). Returns an error if the task is
// unknown or not relaunchable. ctx is the INITIATING call's context, forwarded
// verbatim to the closure so a re-entry can resolve its target on the version that
// call holds; pass context.Background() when there is no initiator.
func (tm *TaskManager) Relaunch(ctx context.Context, id string) (SpawnResult, error) {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return SpawnResult{}, fmt.Errorf("task %s not found", id)
	}
	spec := t.SpecSnapshot()
	if spec.Relaunch == nil {
		return SpawnResult{}, fmt.Errorf("task %s is not relaunchable", id)
	}
	return spec.Relaunch(ctx)
}

// Resume feeds new input into a task and re-enters the standard dense, ACK and settle
// lifecycle under the SAME task id.
//
// - Legal source states are alive-detached, stable, completed and failed; running and suspect mean a round is in flight, cancelled means the session is dead.
// - The claim turns to running under task.mu before ResumeFn runs, so parallel resumes single-win; ctx is the initiating call context, forwarded so the resumed round resolves its target on the version that call holds.
// 契约: docs/wiki/agent/task-lifecycle.md#resume-states
func (tm *TaskManager) Resume(ctx context.Context, id string, input string) (SpawnResult, error) {
	if tm == nil || !tm.beginOperation() {
		return SpawnResult{}, fmt.Errorf("task manager is closing")
	}
	defer tm.operations.Done()
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	if !ok {
		tm.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s not found", id)
	}
	t.mu.Lock()
	tm.mu.Unlock()
	previous := t.status
	if t.resuming {
		t.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is running — a concurrent resume is in flight", id)
	}
	switch previous {
	case TaskAliveDetached, TaskStable, TaskCompleted, TaskFailed:
	case TaskCancelled:
		t.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is cancelled (session killed) — use relaunch_task", id)
	default:
		t.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is %s — a round is in flight; wait for it to settle", id, previous)
	}
	resume := t.Spec.ResumeFn
	if resume == nil {
		t.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s does not support resume", id)
	}
	t.rollback = &roundRollback{producerWaitSatisfied: t.producerWaitSatisfied, status: previous, spec: cloneSpec(t.Spec), detector: t.detector, producerRound: t.producerRound, producerOwner: t.producerOwner, producerInputBatchID: t.producerInputBatchID, signalSeq: t.signalSeq,
		ttlRenewedAt: t.ttlRenewedAt, settledAt: t.settledAt, aliveDetached: t.aliveDetached, detachedAt: t.detachedAt}
	t.roundID++
	if t.roundID == 0 {
		t.roundID = 1
	}
	round := t.roundID
	owner := RoundOwnerFromContext(ctx)
	if owner == "" {
		owner = t.roundOwner
	}
	t.roundOwner, t.resuming, t.status = owner, true, TaskRunning
	t.inputBatchID = InputBatchIDFromContext(ctx)
	spec := cloneSpec(t.Spec)
	spec.InputBatchID = t.inputBatchID
	t.mu.Unlock()
	if tm.onRoundAccepted != nil && !tm.invoke(func() bool { return tm.onRoundAccepted(t) }) {
		t.mu.Lock()
		t.status, t.resuming = previous, false
		t.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s round intent was not accepted", id)
	}
	if fn, ok := ctx.Value(roundStartCtxKey{}).(func(*Task)); ok && fn != nil {
		fn(t)
	}
	ctx = context.WithValue(ctx, taskIdentityCtxKey{}, taskIdentity{id: id, round: round})
	ctx = WithRoundOwner(ctx, owner)
	detector, err := resume(ctx, input)
	if err != nil {
		tm.cancelUnadoptedDetector(t, detector)
		tm.abortRound(t, round, owner, previous, err, false)
		return SpawnResult{Task: t}, fmt.Errorf("task %s resume: %w", id, err)
	}
	if reason := tm.bindPrepared(t, spec, detector, round, owner, false); reason != "" {
		tm.cancelUnadoptedDetector(t, detector)
		tm.abortRound(t, round, owner, previous, fmt.Errorf("%s", reason), false)
		return SpawnResult{Task: t}, fmt.Errorf("task %s resume: %s", id, reason)
	}
	return tm.awaitWindow(t, detector, round), nil
}

func (tm *TaskManager) cancelUnadoptedDetector(t *Task, detector SettleDetector) {
	if detector == nil {
		return
	}
	t.mu.Lock()
	current := t.detector
	t.mu.Unlock()
	if current != nil {
		if stopped := detector.Stopped(); stopped != nil && stopped == current.Stopped() {
			return
		}
		candidate, hasCandidate := detector.(interface{ ProducerID() string })
		active, hasActive := current.(interface{ ProducerID() string })
		if hasCandidate && hasActive && candidate.ProducerID() != "" && candidate.ProducerID() == active.ProducerID() {
			return
		}
	}
	tm.mu.Lock()
	tm.trackProducerLocked(detector)
	tm.mu.Unlock()
	detector.Cancel()
}

func (tm *TaskManager) abortRound(t *Task, round uint64, owner string, previous TaskStatus, err error, initial bool, observed ...SettleSignal) SettleSignal {
	t.mu.Lock()
	if t.status != TaskCancelled {
		t.status = previous
	}
	t.resuming = true
	t.rollback = nil
	if initial {
		t.settledAt = tm.now()
		t.result = "task could not start"
		t.err = err
	}
	sig := SettleSignal{Kind: SettleFailed, Final: true, Err: err, Output: "task round aborted", RoundID: round,
		ProducerRound: t.producerRound, RoundOwner: owner, InputBatchID: t.inputBatchID, Seq: 1, ObservedAt: tm.now(), WaitSatisfied: true, Observation: "round_aborted"}
	if len(observed) > 0 {
		sig = observed[0]
		sig.RoundID, sig.ProducerRound, sig.RoundOwner = round, t.producerRound, owner
		sig.InputBatchID = t.inputBatchID
		sig.Observation, sig.WaitSatisfied = "round_aborted", true
		if sig.Seq == 0 {
			sig.Seq = 1
		}
		if sig.ObservedAt.IsZero() {
			sig.ObservedAt = tm.now()
		}
	}
	if sig.SourceID == "" {
		sig.SourceID = SignalSourceID(t.ID, round, sig.Seq)
	}
	if sig.WaitFor == "" {
		sig.WaitFor = WaitForOf(t.Spec)
	}
	tm.freezeSignalLocked(t, &sig)
	t.mu.Unlock()
	tm.deliver(t, sig, nil, false, true)
	t.mu.Lock()
	t.resuming = false
	if len(observed) > 0 && !t.windowClosed && !t.inlineOffered {
		t.firstSettle <- sig
		t.inlineOffered = true
	}
	t.mu.Unlock()
	tm.RecheckDeadlines()
	return sig
}

// RestorePendingDelivery records a restored receipt without contacting receivers.
func (tm *TaskManager) RestorePendingDelivery(id string, sig SettleSignal) bool {
	return tm.RestorePendingDeliveryWithMode(id, sig, false)
}

// RestorePendingDeliveryWithMode preserves the original inline or background receipt destination.
func (tm *TaskManager) RestorePendingDeliveryWithMode(id string, sig SettleSignal, inline bool) bool {
	if tm == nil {
		return false
	}
	tm.mu.Lock()
	t := tm.tasks[id]
	if tm.closing || t == nil || sig.RoundID == 0 || sig.Seq == 0 {
		tm.mu.Unlock()
		return false
	}
	if sig.SourceID == "" {
		sig.SourceID = SignalSourceID(id, sig.RoundID, sig.Seq)
	}
	t.mu.Lock()
	tm.mu.Unlock()
	defer t.mu.Unlock()
	if sig.WaitFor == "" {
		sig.WaitFor = WaitForOf(t.Spec)
	}
	if t.pending == nil {
		t.pending = make(map[string]*pendingDelivery)
	}
	t.pending[sig.SourceID] = &pendingDelivery{signal: sig, inline: inline, restored: true, aborted: sig.Observation == "round_aborted"}
	return true
}

// ResolveRestoredDelivery releases one frozen receipt after recovery proves its receiver accepted it.
func (tm *TaskManager) ResolveRestoredDelivery(id, sourceID string) bool {
	if tm == nil || sourceID == "" || !tm.beginOperation() {
		return false
	}
	defer tm.operations.Done()
	tm.mu.Lock()
	t := tm.tasks[id]
	if t == nil {
		tm.mu.Unlock()
		return false
	}
	t.mu.Lock()
	tm.mu.Unlock()
	p := t.pending[sourceID]
	if p == nil || !p.restored || p.inFlight {
		t.mu.Unlock()
		return false
	}
	delete(t.pending, sourceID)
	sig, detector := p.signal, p.detector
	if t.signalEndsCurrentRoundLocked(sig) {
		t.retireNotified = true
	}
	t.mu.Unlock()
	tm.acknowledge(detector, sig)
	tm.RecheckDeadlines()
	return true
}

// ResumePendingDeliveries starts retries after every recovery receiver has bound.
func (tm *TaskManager) ResumePendingDeliveries() {
	if tm == nil || !tm.beginOperation() {
		return
	}
	defer tm.operations.Done()
	tm.mu.Lock()
	if tm.closing {
		tm.mu.Unlock()
		return
	}
	type deferredStart struct {
		task       *Task
		activation *detectorActivation
	}
	var activations []deferredStart
	for _, t := range tm.tasks {
		t.mu.Lock()
		t.recovering = false
		if activation := t.deferredActivation; activation != nil {
			activations = append(activations, deferredStart{t, activation})
			t.deferredActivation = nil
		}
		for _, p := range t.pending {
			p.restored = false
			p.retryAt = time.Time{}
		}
		t.mu.Unlock()
	}
	tm.mu.Unlock()
	for _, start := range activations {
		tm.activateDetector(start.task, start.activation)
	}
	tm.wakeDelivery()
	tm.RecheckDeadlines()
}

func (tm *TaskManager) deliver(t *Task, sig SettleSignal, detector SettleDetector, inline, aborted bool) bool {
	if sig.SourceID == "" {
		t.mu.Lock()
		if sig.RoundID == 0 {
			sig.RoundID = t.producerRound
			if sig.RoundID == 0 {
				sig.RoundID = t.roundID
			}
			if sig.RoundID == 0 {
				sig.RoundID = 1
				t.roundID = 1
				t.producerRound = 1
			}
		}
		if sig.ProducerRound == 0 {
			sig.ProducerRound = t.producerRound
		}
		if sig.Seq == 0 {
			t.signalSeq++
			sig.Seq = t.signalSeq
		}
		if sig.RoundOwner == "" {
			sig.RoundOwner = t.producerOwner
		}
		if sig.ObservedAt.IsZero() {
			sig.ObservedAt = tm.now()
		}
		if sig.WaitFor == "" {
			sig.WaitFor = WaitForOf(t.Spec)
		}
		if sig.InputBatchID == "" {
			sig.InputBatchID = t.producerInputBatchID
			if sig.Observation == "round_aborted" || t.producerRound == 0 {
				sig.InputBatchID = t.inputBatchID
			}
		}
		sig.WaitSatisfied = SatisfiesWait(t.Spec, sig)
		tm.freezeSignalLocked(t, &sig)
		sig.SourceID = SignalSourceID(t.ID, sig.RoundID, sig.Seq)
		t.mu.Unlock()
	}
	t.mu.Lock()
	if t.pending == nil {
		t.pending = make(map[string]*pendingDelivery)
	}
	p := t.pending[sig.SourceID]
	if p == nil {
		p = &pendingDelivery{signal: sig, detector: detector, inline: inline, aborted: aborted, restored: t.recovering}
		t.pending[sig.SourceID] = p
	}
	t.mu.Unlock()
	return tm.attemptDelivery(t, p)
}

func (tm *TaskManager) attemptDelivery(t *Task, p *pendingDelivery) bool {
	t.mu.Lock()
	if p.restored || p.inFlight || t.pending[p.signal.SourceID] != p {
		t.mu.Unlock()
		return false
	}
	p.inFlight = true
	sig, detector, inline, aborted := p.signal, p.detector, p.inline, p.aborted
	t.mu.Unlock()
	accepted := tm.invoke(func() bool {
		if aborted {
			if tm.onRoundAbortedAccepted != nil {
				return tm.onRoundAbortedAccepted(t, sig)
			}
			return false
		}
		if inline {
			if tm.onInlineSettleAccepted != nil {
				return tm.onInlineSettleAccepted(t, sig)
			}
			if tm.onInlineSettle != nil {
				tm.onInlineSettle(t, sig)
				return true
			}
			return false
		}
		if tm.onSettleAccepted != nil {
			return tm.onSettleAccepted(t, sig)
		}
		if tm.onSettle != nil {
			tm.onSettle(t, sig)
			return true
		}
		return false
	})
	t.mu.Lock()
	p.inFlight = false
	if accepted {
		delete(t.pending, sig.SourceID)
		if t.signalEndsCurrentRoundLocked(sig) {
			t.retireNotified = true
		}
	} else {
		if p.backoff == 0 {
			p.backoff = 400 * time.Millisecond
		} else {
			p.backoff *= 2
			if p.backoff > 4*time.Second {
				p.backoff = 4 * time.Second
			}
		}
		p.retryAt = time.Now().Add(p.backoff)
	}
	t.mu.Unlock()
	if accepted {
		tm.acknowledge(detector, sig)
		tm.RecheckDeadlines()
	}
	tm.wakeDelivery()
	return accepted
}

func (tm *TaskManager) acknowledge(detector SettleDetector, sig SettleSignal) {
	if ack, ok := detector.(SignalAcknowledger); ok {
		ack.AcknowledgeSignal(sig)
	}
}

func (tm *TaskManager) wakeDelivery() {
	select {
	case tm.retryWake <- struct{}{}:
	default:
	}
}

func resetTaskTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	if delay < 0 {
		delay = 0
	}
	timer.Reset(delay)
}

func (tm *TaskManager) deliveryWorker() {
	defer tm.workers.Done()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	select {
	case <-tm.stop:
		return
	case <-tm.retryWake:
	}
	for {
		tm.mu.Lock()
		var due []struct {
			task     *Task
			delivery *pendingDelivery
		}
		var nearest time.Time
		now := time.Now()
		for _, t := range tm.tasks {
			t.mu.Lock()
			for _, p := range t.pending {
				if p.restored || p.inFlight {
					continue
				}
				if p.retryAt.IsZero() || !p.retryAt.After(now) {
					due = append(due, struct {
						task     *Task
						delivery *pendingDelivery
					}{t, p})
				} else if nearest.IsZero() || p.retryAt.Before(nearest) {
					nearest = p.retryAt
				}
			}
			t.mu.Unlock()
		}
		tm.mu.Unlock()
		for _, item := range due {
			select {
			case <-tm.stop:
				return
			default:
			}
			tm.attemptDelivery(item.task, item.delivery)
		}
		if len(due) > 0 {
			continue
		}
		var tick <-chan time.Time
		if !nearest.IsZero() {
			resetTaskTimer(timer, time.Until(nearest))
			tick = timer.C
		}
		select {
		case <-tm.stop:
			return
		case <-tm.retryWake:
		case <-tick:
		}
	}
}

// RecheckDeadlines reschedules the nearest deadline against the current TTL source.
func (tm *TaskManager) RecheckDeadlines() {
	if tm == nil || !tm.enableDeadlineReaper {
		return
	}
	select {
	case tm.deadlineWake <- struct{}{}:
	default:
	}
}

func (tm *TaskManager) deadlineWorker() {
	defer tm.workers.Done()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		tm.mu.Lock()
		var nearest time.Time
		for _, t := range tm.tasks {
			t.mu.Lock()
			if t.recovering {
				t.mu.Unlock()
				continue
			}
			var deadline time.Time
			if !isTerminalStatus(t.status) {
				ttl := t.Spec.TTL
				if ttl <= 0 {
					ttl = tm.effDefaultTTL()
				}
				if !t.ttlAnchor().IsZero() {
					deadline = t.ttlAnchor().Add(ttl)
				}
			} else if len(t.pending) == 0 && !t.settledAt.IsZero() {
				deadline = t.settledAt.Add(tm.effTerminalTTL())
			}
			if !deadline.IsZero() && (nearest.IsZero() || deadline.Before(nearest)) {
				nearest = deadline
			}
			t.mu.Unlock()
		}
		tm.mu.Unlock()
		var tick <-chan time.Time
		if !nearest.IsZero() {
			resetTaskTimer(timer, nearest.Sub(tm.now()))
			tick = timer.C
		}
		select {
		case <-tm.stop:
			return
		case <-tm.deadlineWake:
		case <-tick:
			tm.reconcileTTL()
			tm.pruneTerminal()
		}
	}
}

// Close stops admission and observation, requests producer cancellation, and bounds the caller's wait.
func (tm *TaskManager) Close(ctx context.Context) error {
	if tm == nil {
		return nil
	}
	tm.closeOnce.Do(func() {
		tm.mu.Lock()
		tm.closing = true
		close(tm.stop)
		tm.mu.Unlock()
		go func() {
			var cancellations sync.WaitGroup
			tm.mu.Lock()
			initial := append([]SettleDetector(nil), tm.producers...)
			tm.mu.Unlock()
			for _, detector := range initial {
				cancellations.Add(1)
				go func(d SettleDetector) { defer cancellations.Done(); d.Cancel() }(detector)
			}
			tm.operations.Wait()
			tm.mu.Lock()
			producers := append([]SettleDetector(nil), tm.producers...)
			tm.mu.Unlock()
			seen := make(map[<-chan struct{}]bool, len(initial))
			for _, detector := range initial {
				if stopped := detector.Stopped(); stopped != nil {
					seen[stopped] = true
				}
			}
			for _, detector := range producers {
				if stopped := detector.Stopped(); stopped != nil && seen[stopped] {
					continue
				}
				cancellations.Add(1)
				go func(d SettleDetector) { defer cancellations.Done(); d.Cancel() }(detector)
			}
			cancellations.Wait()
			tm.watchers.Wait()
			tm.workers.Wait()
			tm.callbacks.Wait()
			for _, detector := range initial {
				<-detector.Stopped()
			}
			for _, detector := range producers {
				<-detector.Stopped()
			}
			close(tm.closeDone)
		}()
	})
	select {
	case <-tm.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CloseDone closes after cancellation, producers, observers, callbacks and workers have all returned.
func (tm *TaskManager) CloseDone() <-chan struct{} {
	if tm == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return tm.closeDone
}

// funcSettleDetector is a generic detector that runs fn in a goroutine and emits
// a single settle signal when fn returns (Completed on success, Completed+Err on
// failure), then closes. It doubles as the "generic goroutine task" detector.
type funcSettleDetector struct {
	ch      chan SettleSignal
	cancel  context.CancelFunc
	detach  <-chan struct{}
	stopped chan struct{}
}

// NewFuncSettleDetector runs fn under a cancelable context and settles on return.
// An optional denseDuration overrides the default dense phase after which, if fn
// has not returned, the detector signals detach (→ async ack).
func NewFuncSettleDetector(ctx context.Context, fn func(context.Context) (string, error), denseDuration ...time.Duration) SettleDetector {
	cctx, cancel := context.WithCancel(ctx)
	d := &funcSettleDetector{ch: make(chan SettleSignal, 1), cancel: cancel, stopped: make(chan struct{})}
	dd := defaultDenseDuration
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		dd = denseDuration[0]
	}
	d.detach = DetachAfter(dd, cctx.Done())
	go func() {
		defer close(d.stopped)
		out, err := fn(cctx)
		d.ch <- SettleSignal{Kind: SettleCompleted, Output: out, Err: err}
		close(d.ch)
		cancel()
	}()
	return d
}

func (d *funcSettleDetector) Settled() <-chan SettleSignal { return d.ch }
func (d *funcSettleDetector) Detached() <-chan struct{}    { return d.detach }
func (d *funcSettleDetector) Stopped() <-chan struct{}     { return d.stopped }
func (d *funcSettleDetector) Cancel()                      { d.cancel() }
