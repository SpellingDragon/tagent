package task

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// TaskStatus is the lifecycle state of a Task.
type TaskStatus string

const (
	// TaskRunning 表示工作仍在产出：前台可继续等待，回收只看探活不看年龄。
	TaskRunning TaskStatus = "running"
	// TaskStable 是一次"输出稳定"观测对应的状态。已脱离前台的任务不回退到此态。
	TaskStable TaskStatus = "stable"
	// TaskAliveDetached 表示已脱离前台等待但仍存活：结算与可疑信号在此后
	// 被抑制，避免面板反复摆动与回收刷屏。
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
	// SettleStable 是"输出稳定"观测：在任务尚未脱离前台时把它置为 stable，
	// 已脱离则抑制外发，避免面板反复摆动。
	SettleStable SettleKind = "stable"
	// SettleSuspect 是"静默到可疑阈值"观测：任务可能还活着，面板改展示剩余寿命
	// 交给模型判断；已脱离前台时同样抑制。
	SettleSuspect SettleKind = "suspect"
	// SettleWatch 只作观察通知：不改变任务状态，仅把信号交给结算回调与事件总线。
	SettleWatch SettleKind = "watch"
	// SettleFailed 是失败结算：由僵尸/孤儿回收或携带错误的路径驱动；finalize 会把
	// 带错误的完成信号规范化为它，且只结算一次。
	SettleFailed SettleKind = "failed"
)

// LifetimeJob Task lifecycle classes: jobs are one-round
// work units subject to stale observation and an optional deadline; services
// are long-lived by design and are never age-terminated.
const (
	LifetimeJob     = "job"
	LifetimeService = "service"
)

// LifetimeOf resolves the effective lifecycle class: an explicit declaration
// wins; otherwise Kind is the proxy (command/subagent behave as jobs — one
// round then exit; generic/unknown default to service — the conservative
// class that the stale wall never terminates).
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

// SettleSignal is emitted by a SettleDetector when a task reaches a settle point.
type SettleSignal struct {
	Kind   SettleKind
	Output string
	Err    error
}

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
	Kind    string `json:"kind"`
	Desc    string `json:"desc"`
	Key     string `json:"key,omitempty"`
	Command string `json:"command,omitempty"`
	// AgentName subagent Kind relaunch inputs (re-dispatch through the resident agents
	// map). Resume is NOT rebuildable for subagent (rounds has no event source
	// — the factory returns a relaunch-guidance error, D1.2 promise table).
	AgentName   string            `json:"agent_name,omitempty"`
	MessageBody string            `json:"message_body,omitempty"`
	EventKeys   []int64           `json:"event_keys,omitempty"`
	Origin      map[string]string `json:"origin,omitempty"`
	TaskID      string            `json:"task_id,omitempty"`
	// Lifetime declares the job/service lifecycle class (hardening-review-
	// batch2 2.4): job tasks are subject to the stale-after observation and
	// an optional job deadline; service tasks are never age-terminated.
	// Persisted so restores re-derive the same class.
	Lifetime string `json:"lifetime,omitempty"`
	// DetachedAtMilli persists the alive-detached transition time so a
	// restored task keeps its REAL detached age (restore time must never
	// silently replace it). Carried on the alive-detached settle event's
	// metadata and replayed by RebuildTaskRegistry.
	DetachedAtMilli int64 `json:"detached_at_ms,omitempty"`
	// Params carries the ActionArgs spawn fields (WorkDir/Env/Mode/Name/IsTUI/
	// Watch/Probe/ProbeIntervalSec/ProbeFailures/QuietTimeout/Timeout — encoded
	// as strings; session-op fields excluded; conversion lives in tool/action).
	Params         map[string]string `json:"params,omitempty"`
	StartedAtMilli int64             `json:"started_at_ms"`
}

// TaskSpec captures enough to describe and (re)launch a task.
type TaskSpec struct {
	Kind string
	Desc string
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

	mu        sync.Mutex
	status    TaskStatus
	result    string
	err       error
	settledAt time.Time

	detector      SettleDetector
	firstSettle   chan SettleSignal
	windowClosed  bool
	aliveDetached bool
	detachedAt    time.Time
	ttlRenewedAt  time.Time
	watchDone     chan struct{}
}

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
	if t.settledAt.IsZero() {
		return false
	}
	return now.Sub(t.settledAt) > ttl
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
	Origin map[string]string
}

// Spawn 在 spec 未自带 Origin 时，把本包装器携带的 origin 逐键复制一份填进去（复制而非共享：
// 调用方随后改写自己的 map 不会串到任务上），再委托给内层控制器。
func (o *OriginSpawner) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
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
	// OnSettle is invoked when a task settles AFTER its window closed (detach) —
	// i.e. a background settle that must be written back as a task_settled event.
	// May be nil.
	OnSettle func(task *Task, sig SettleSignal)

	// OnBatchRetire 是可选的批量退役汇聚点：reconcile/孤儿退役走它——逐条的状态迁移与记账仍按
	// 单任务进行，但 bus 侧通知折叠为一条汇总事件。未注册时按逐条 OnSettle 通知。
	OnBatchRetire func(batch []BatchRetired)
	// OnSpawn: invoked after a task registers
	// (best-effort fact-chain task_spawned record; never blocks the spawn
	// path). May be nil.
	OnSpawn func(task *Task)
	// OnInlineSettle : invoked when a task settles INSIDE its sync-wait
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
	// subagent tasks. When >0, reconcileTTL terminates+retires any ACTIVE task
	// (ALL states incl. suspect/undetached, ALL lifetime classes incl.
	// resident/interactive) once `now - anchor >= DefaultTTL` (or the task's own
	// spec.TTL when larger). When <=0 the manager-level reaper is OFF and only a
	// per-task spec.TTL bounds its task — preserving the pre-TTL "no wall unless
	// configured" behavior for callers not yet on TTL.
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
	mu               sync.Mutex
	tasks            map[string]*Task
	byKey            map[string]string
	onSettle         func(task *Task, sig SettleSignal)
	onBatchRetire    func(batch []BatchRetired)
	batchCollect     *[]BatchRetired
	onSpawn          func(task *Task)
	onInlineSettle   func(task *Task, sig SettleSignal)
	onCancel         func(task *Task)
	spawnGate        func() string
	auditGate        func(spec TaskSpec) string
	terminalTTL      time.Duration
	now              func() time.Time
	zombieGrace      time.Duration
	orphanGrace      time.Duration
	defaultTTL       time.Duration
	isSessionTracked func(sessionID string) bool
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
	return &TaskManager{
		tasks:            make(map[string]*Task),
		byKey:            make(map[string]string),
		onSettle:         cfg.OnSettle,
		onBatchRetire:    cfg.OnBatchRetire,
		onSpawn:          cfg.OnSpawn,
		onInlineSettle:   cfg.OnInlineSettle,
		onCancel:         cfg.OnCancel,
		spawnGate:        cfg.SpawnGate,
		auditGate:        cfg.AuditGate,
		terminalTTL:      ttl,
		zombieGrace:      zg,
		orphanGrace:      og,
		defaultTTL:       dttl,
		isSessionTracked: cfg.SessionTracker,
		now:              time.Now,
	}
}

// Spawn starts a task and blocks until the first of {settle, detach}.
// - Settle first  → SpawnResult{Settled: true, Signal} (inline).
// - Detach first  → SpawnResult{Settled: false} (ack; tracked in background).
// - Equivalent task active → SpawnResult{Deduped: true} (no new task).
//
// Multiple concurrent Spawn calls each wait their own detector's window in
// parallel (blocking ≈ the slowest, not the sum).
func (tm *TaskManager) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
	tm.pruneTerminal()
	tm.mu.Lock()
	if spec.Key != "" {
		if id, ok := tm.byKey[spec.Key]; ok {
			if existing, ok := tm.tasks[id]; ok && existing.isActive() {
				tm.mu.Unlock()
				detector.Cancel()
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
	task := &Task{
		ID:          uuid.NewString(),
		Spec:        spec,
		StartedAt:   time.Now(),
		status:      TaskRunning,
		detector:    detector,
		firstSettle: make(chan SettleSignal, 1),
		watchDone:   make(chan struct{}),
	}
	tm.tasks[task.ID] = task
	if spec.Key != "" {
		tm.byKey[spec.Key] = task.ID
	}
	tm.mu.Unlock()

	if tm.onSpawn != nil {
		tm.onSpawn(task)
	}

	if detector != nil {
		go tm.watch(task, detector, task.watchDone)
	}

	// Wait for the first of {settle, detach}. The detach signal (dense→sparse
	// boundary, owned by the detector) is the sync→async ack point — there is no
	// separate sync_wait timer. A detector with no detach channel (nil) blocks
	// here until settle (pure synchronous).
	//
	// Nil-detector defense (poka-yoke): a nil interface would panic on
	// .Detached(); receive from a nil channel instead — same "blocks until
	// settle" semantics, no panic. (Two distinct nils: nil INTERFACE vs nil
	// DETACHED CHANNEL; the doc contract refers to the latter.)
	var detachCh <-chan struct{}
	if detector != nil {
		detachCh = detector.Detached()
	}
	select {
	case sig := <-task.firstSettle:
		tm.closeWindow(task, false)
		if tm.onInlineSettle != nil {
			tm.onInlineSettle(task, sig)
		}
		return SpawnResult{Task: task, Settled: true, Signal: sig}
	case <-detachCh:
		tm.closeWindow(task, true)
		return SpawnResult{Task: task, Settled: false}
	}
}

// watch consumes the detector's settle signals, updates task state, and routes
// each signal either into the sync-wait window (before it closes) or to OnSettle
// (after). The routing decision is made under task.mu together with the buffered
// send, so no settle is lost at the window boundary. It exits when the detector's
// channel closes OR when the watch is retired (resume re-arms a fresh detector).
func (tm *TaskManager) watch(task *Task, detector SettleDetector, done <-chan struct{}) {
	if detector == nil {
		return
	}
	for {
		select {
		case <-done:
			return
		case sig, ok := <-detector.Settled():
			if !ok {
				return
			}
			task.mu.Lock()
			terminal := isTerminalStatus(task.status)
			st := task.status
			task.mu.Unlock()
			if terminal {
				log.Warnf("[task] drop post-terminal signal: task=%s status=%s late_kind=%s", task.ID, st, sig.Kind)
				continue
			}
			tm.applyStatus(task, sig)

			task.mu.Lock()
			if task.windowClosed {
				task.mu.Unlock()
				tm.emitBackground(task, sig)
			} else {
				select {
				case task.firstSettle <- sig:
				default:
				}
				task.mu.Unlock()
			}
		}
	}
}

// closeWindow marks the sync-wait window closed. When drainToBg is true (timeout
// path), any settle that landed in the buffer at the boundary is routed to the
// background handler so it is never dropped.
func (tm *TaskManager) closeWindow(task *Task, drainToBg bool) {
	task.mu.Lock()
	if task.windowClosed {
		task.mu.Unlock()
		return
	}
	task.windowClosed = true
	var pending *SettleSignal
	if drainToBg {
		select {
		case sig := <-task.firstSettle:
			pending = &sig
		default:
		}
	}
	task.mu.Unlock()
	if pending != nil {
		tm.emitBackground(task, *pending)
	}
}

// emitBackground invokes the OnSettle hook for a settle that occurred after the
// sync-wait window closed (a background/reclaim settle), applying alive-detached
// semantics for service-type tasks :
// - first stable → transition to alive-detached and emit the one-time "ready"
// notification;
// - once detached, subsequent stable/suspect signals (e.g. output changes, a
// quiet service) are suppressed to avoid reclaim spam / permanent board churn;
// - completion/failure (process death) always emits and ends the task.
func (tm *TaskManager) emitBackground(task *Task, sig SettleSignal) {
	task.mu.Lock()
	switch sig.Kind {
	case SettleWatch:
		task.mu.Unlock()
		if tm.onSettle != nil {
			tm.onSettle(task, sig)
		}
		return
	case SettleStable:
		if task.aliveDetached {
			task.mu.Unlock()
			return
		}
		task.aliveDetached = true
		task.status = TaskAliveDetached
		task.detachedAt = tm.now()
	case SettleSuspect:
		if task.aliveDetached {
			task.mu.Unlock()
			return
		}
	}
	task.mu.Unlock()

	if tm.onSettle != nil {
		tm.onSettle(task, sig)
	}
}

// applyStatus maps a settle kind to the task's status and records the result.
// (Fencing lives at the watch-loop signal entry — applyStatus/emitBackground
// are two stages of the SAME signal and must not gate each other; this
// function's terminal check is a secondary guard for non-watch callers.)
func (tm *TaskManager) applyStatus(task *Task, sig SettleSignal) {
	task.mu.Lock()
	defer task.mu.Unlock()
	if isTerminalStatus(task.status) {
		return
	}
	task.result = sig.Output
	task.err = sig.Err
	task.settledAt = tm.now()
	switch sig.Kind {
	case SettleWatch:
	case SettleCompleted:
		if sig.Err != nil {
			task.status = TaskFailed
		} else {
			task.status = TaskCompleted
		}
	case SettleStable:
		if task.status != TaskAliveDetached {
			task.status = TaskStable
		}
	case SettleSuspect:
		if task.status != TaskAliveDetached {
			task.status = TaskSuspect
		}
	}
}

// BindDetector RestoreTask：冷启动回放重建一个跨重启
// 存续的任务——注册到 registry（复用原 id/Key 去重语义）但不启动 watch
// goroutine（进程内探测器不可恢复：running 语义降级为 suspect 交 R3 存活探测
// 裁决；alive-detached 原态恢复并置 aliveDetached 使 reconcileDetached 探针
// 路径可用）。spec 的闭包由调用方经工厂重建（承诺表）；未重建则仅展示。
// BindDetector wires a detector to an EXISTING (typically restored) task and
// starts the standard watch consumption — the R3 reattach bridge (hardening-
// review-batch2 3.1). Restored tasks have no in-process detector; without
// this binding their watch/probe signals never reach the manager and a
// "tracked session" silently loses its settle path. A finalized task refuses
// the bind (fencing). The task's watchDone retires the consumption on resume
// re-arm, same as Spawn.
func (tm *TaskManager) BindDetector(id string, d SettleDetector) error {
	if tm == nil || id == "" {
		return fmt.Errorf("BindDetector: empty id")
	}
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return fmt.Errorf("BindDetector: task %s not found", id)
	}
	t.mu.Lock()
	if isTerminalStatus(t.status) {
		t.mu.Unlock()
		return fmt.Errorf("BindDetector: task %s already finalized (%s)", id, t.status)
	}
	t.detector = d
	done := t.watchDone
	t.mu.Unlock()
	if d != nil {
		go tm.watch(t, d, done)
	}
	return nil
}

// RestoreTask 从已持久化的事实重建任务，返回登记在用的任务对象：沿用其 id、声明、派生时刻与
// 状态；状态为 alive_detached 时同时标记已脱离，使其继续享受"脱离后信号抑制"的语义。
//
// 三点关键行为：
//   - 窗口标记为已关闭：重建出的任务不重启观察，因而不会因"重启后没人在看"被误判为静默或
//     僵尸；终态判定与 TTL 回收照常生效；
//   - 幂等且不覆盖：同一 id 已在表内时直接返回既有任务——在途的真实状态不被重建值改写；
//     spec.Key 同样只在无人占用时登记；
//   - 管理器为 nil 或 id 为空时返回 nil；startedAt 为零值时取当前时刻。
func (tm *TaskManager) RestoreTask(id string, spec TaskSpec, startedAt time.Time, status TaskStatus) *Task {
	if tm == nil || id == "" {
		return nil
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	tk := &Task{
		ID:           id,
		Spec:         spec,
		StartedAt:    startedAt,
		status:       status,
		windowClosed: true,
		watchDone:    make(chan struct{}),
		firstSettle:  make(chan SettleSignal, 1),
	}
	if status == TaskAliveDetached {
		tk.aliveDetached = true
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
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
	if !ok || tk.status != TaskSuspect {
		return false
	}
	tk.status = TaskRunning
	return true
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
// else the manager DefaultTTL; when both are <=0 the reaper is OFF for that task
// (transitional — callers not yet on TTL keep the old "no wall unless
// configured" semantics until ). On expiry it cancels the backing work via
// the owner's detector.Cancel (kills the tmux session / goroutine) OUTSIDE the
// lock, then retires the task failed ONCE through finalizeRetired (SettleFailed),
// so it leaves the board. finalize's terminal fence makes concurrent/repeat
// reconciliation a no-op — no double settlement.
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
		expired := ttl > 0 && !isTerminalStatus(t.status) && !t.ttlAnchor().IsZero() && age >= ttl
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
		if det := detectors[i]; det != nil {
			det.Cancel()
		}
		tm.finalizeRetired(t, "(ttl-expired: task exceeded its absolute lifetime - cancelled by owner and retired)", nil)
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
		need := t.status == TaskAliveDetached && t.Spec.Alive != nil
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
// finalizeRetired is the retirement-path finalize (zombie/orphan/stale-deadline):
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
	if tm.onBatchRetire == nil {
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
		if tm.onBatchRetire != nil && len(batch) > 0 {
			tm.onBatchRetire(batch)
		}
	}
}

func (tm *TaskManager) finalizeRetired(t *Task, output string, err error) {
	t.mu.Lock()
	if t.Spec.Origin == nil {
		t.Spec.Origin = map[string]string{}
	}
	t.Spec.Origin[event.MetaKeyTriggerSource] = "task-retired"
	t.mu.Unlock()
	tm.finalize(t, SettleFailed, output, err)
}

func (tm *TaskManager) finalize(t *Task, kind SettleKind, output string, err error) {
	t.mu.Lock()
	if isTerminalStatus(t.status) {
		t.mu.Unlock()
		return
	}
	t.result = output
	t.err = err
	t.settledAt = tm.now()
	switch {
	case kind == SettleFailed || err != nil:
		t.status = TaskFailed
	case kind == SettleCompleted:
		t.status = TaskCompleted
	default:
		t.status = TaskFailed
		kind = SettleFailed
		if err == nil {
			err = fmt.Errorf("finalize: non-terminal reconcile kind %q coerced to failed", kind)
		}
	}
	t.mu.Unlock()
	tm.mu.Lock()
	if tm.batchCollect != nil {
		*tm.batchCollect = append(*tm.batchCollect, BatchRetired{Task: t, Sig: SettleSignal{Kind: kind, Output: output, Err: err}})
		tm.mu.Unlock()
		return
	}
	tm.mu.Unlock()
	if tm.onSettle != nil {
		tm.onSettle(t, SettleSignal{Kind: kind, Output: output, Err: err})
	}
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
	now := tm.now()
	tm.mu.Lock()
	var candidates []*Task
	for _, t := range tm.tasks {
		t.mu.Lock()
		need := t.status == TaskSuspect &&
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
		need := t.Spec.Alive != nil &&
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

// List returns a snapshot of all tracked tasks.
func (tm *TaskManager) List() []*Task {
	tm.reconcileDetached()
	tm.pruneTerminal()
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		out = append(out, t)
	}
	return out
}

// Cancel stops a task's underlying work and marks it cancelled.
func (tm *TaskManager) Cancel(id string) bool {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return false
	}
	t.mu.Lock()
	detector := t.detector
	t.mu.Unlock()
	if detector != nil {
		detector.Cancel()
	}
	t.mu.Lock()
	t.status = TaskCancelled
	if t.settledAt.IsZero() {
		t.settledAt = tm.now()
	}
	t.mu.Unlock()
	if tm.onCancel != nil {
		tm.onCancel(t)
	}
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
	if t.Spec.Relaunch == nil {
		return SpawnResult{}, fmt.Errorf("task %s is not relaunchable", id)
	}
	return t.Spec.Relaunch(ctx)
}

// Resume feeds new input into a task and re-enters the standard
// dense→ACK→settle lifecycle under the SAME task id.
//
// Legal source states:
// - alive_detached / stable — the session is alive; tmux resume feeds
// SendKeys into it (service/repl reentry).
// - completed / failed — the previous round ended; for executor kinds that
// are round-based (subagent: new Run + task-chain restorer), resume is the
// natural continuation. tmux resume on a dead session fails cleanly at
// SendKeys with an actionable error.
//
// Illegal source states: running / suspect (a round is in flight — wait and
// retry) and cancelled (session killed — relaunch or start fresh). Concurrency:
// the claim transitions to running under task.mu BEFORE ResumeFn runs, so
// parallel resumes (parallel tool execution is enabled) single-win; the loser
// is told the task is running. ctx is the INITIATING call's context, forwarded to
// ResumeFn so the resumed round can resolve its target on the version that call
// holds; a refusal there rolls the claim back unchanged.
func (tm *TaskManager) Resume(ctx context.Context, id string, input string) (SpawnResult, error) {
	tm.mu.Lock()
	task, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return SpawnResult{}, fmt.Errorf("task %s not found", id)
	}

	task.mu.Lock()
	prevStatus := task.status
	switch prevStatus {
	case TaskAliveDetached, TaskStable, TaskCompleted, TaskFailed:
	case TaskRunning, TaskSuspect:
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is %s — a round is in flight (or a concurrent resume); wait for it to settle and retry", id, prevStatus)
	case TaskCancelled:
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is cancelled (session killed) — use relaunch_task for a fresh run, or start a new call", id)
	default:
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s is %s (not resumable) — use relaunch_task for a fresh run, or start a new call", id, prevStatus)
	}
	if task.Spec.ResumeFn == nil {
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s does not support resume", id)
	}
	task.status = TaskRunning
	task.mu.Unlock()

	detector, err := task.Spec.ResumeFn(ctx, input)
	if err != nil {
		task.mu.Lock()
		task.status = prevStatus
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s resume: %w", id, err)
	}

	task.mu.Lock()
	newWatch := task.detector == nil || detector != task.detector
	if newWatch {
		close(task.watchDone)
		task.watchDone = make(chan struct{})
		task.detector = detector
	}
	task.firstSettle = make(chan SettleSignal, 1)
	task.windowClosed = false
	task.aliveDetached = false
	task.ttlRenewedAt = tm.now()
	done := task.watchDone
	task.mu.Unlock()

	if newWatch {
		if detector != nil {
			go tm.watch(task, detector, done)
		}
	}

	// Same nil-detector defense as Spawn: receive from a nil channel instead of
	// calling .Detached() on a nil interface.
	var detachCh <-chan struct{}
	if detector != nil {
		detachCh = detector.Detached()
	}
	select {
	case sig := <-task.firstSettle:
		tm.closeWindow(task, false)
		return SpawnResult{Task: task, Settled: true, Signal: sig}, nil
	case <-detachCh:
		tm.closeWindow(task, true)
		return SpawnResult{Task: task, Settled: false}, nil
	}
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
