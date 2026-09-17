package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// TaskStatus is the lifecycle state of a Task.
type TaskStatus string

const (
	TaskRunning       TaskStatus = "running"        // in flight, no settle yet
	TaskStable        TaskStatus = "stable"         // output stable, process alive (usable, maybe waiting)
	TaskAliveDetached TaskStatus = "alive_detached" // service-type: settled once, still alive (Phase 2)
	TaskStale         TaskStatus = "stale"          // hardening-review-batch2 2.5: job detached past stale_after — observed, NOT terminal (probe/reconcile still govern it)
	TaskCompleted     TaskStatus = "completed"      // finished successfully
	TaskFailed        TaskStatus = "failed"         // finished with error
	TaskSuspect       TaskStatus = "suspect"        // quiet too long — likely hung
	TaskDead          TaskStatus = "dead"           // abandoned; spec retained for relaunch
	TaskCancelled     TaskStatus = "cancelled"      // explicitly cancelled
)

// defaultZombieGrace is the minimum age a running/suspect task must reach
// before reconcileZombies may retire it. The liveness probe — not age — is
// the kill criterion; the grace merely keeps the sweep away from spawn
// windows and legitimately quiet long-runners.
const defaultZombieGrace = 10 * time.Minute

// defaultOrphanGrace bounds the reincarnation-orphan adjudication (§7):
// conservative enough to never kill a quiet but young this-life subagent,
// large enough that restored multi-hour suspects (the actual target) qualify
// immediately at rebuild.
const defaultOrphanGrace = 30 * time.Minute

// SettleKind classifies how a task reached a settle point. Detectors emit this
// deterministically; the LLM interprets ambiguous kinds (stable/suspect) later.
type SettleKind string

const (
	SettleCompleted SettleKind = "completed" // runnable exited — definitely done
	SettleStable    SettleKind = "stable"    // output stable, still alive — usable but maybe waiting
	SettleSuspect   SettleKind = "suspect"   // quiet beyond fake-dead threshold — likely hung
	SettleWatch     SettleKind = "watch"     // output matched a watch pattern (C1); informational, no state change
	SettleFailed    SettleKind = "failed"    // reconcile-retired: backing session provably gone (zombie sweep)
)

// Task lifecycle classes (hardening-review-batch2 2.4): jobs are one-round
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
	Output string // captured result/output at settle time
	Err    error  // non-nil when the task failed
}

// SettleDetector observes a running task and emits SettleSignals. Different task
// types provide different detectors:
//   - tmux command → wraps TmuxMonitor (stable/completed/suspect)  [Phase 1]
//   - sub-agent    → RunFlow returns                               [Phase 3]
//   - generic      → goroutine returns                             [Phase 0]
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
// chain, resident-continuity-r2-r4 D1).
type Declarative struct {
	Kind    string `json:"kind"`              // "command" | "subagent" | "generic"
	Desc    string `json:"desc"`              // board/logs
	Key     string `json:"key,omitempty"`     // idempotency key
	Command string `json:"command,omitempty"` // command Kind: original command line
	// subagent Kind relaunch inputs (re-dispatch through the resident agents
	// map). Resume is NOT rebuildable for subagent (rounds has no event source
	// — the factory returns a relaunch-guidance error, D1.2 promise table).
	AgentName   string            `json:"agent_name,omitempty"`
	MessageBody string            `json:"message_body,omitempty"`
	EventKeys   []int64           `json:"event_keys,omitempty"`
	Origin      map[string]string `json:"origin,omitempty"`  // routing baggage (courier)
	TaskID      string            `json:"task_id,omitempty"` // tmux session binding (R3 bridge)
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
	StartedAtMilli int64             `json:"started_at_ms"` // board age + zombie grace reseed
}

// TaskSpec captures enough to describe and (re)launch a task.
type TaskSpec struct {
	Kind string // "command" | "subagent" | "generic"
	Desc string // human-readable (board + logs)
	// Key is the idempotency key: while a task with this Key is active, a
	// repeat Spawn returns the existing task instead of creating a duplicate.
	// Empty Key disables dedup.
	Key string
	// Relaunch, when non-nil, re-spawns an equivalent task from scratch (used by
	// relaunch(id)). For command tasks it re-runs the original command in a
	// fresh session. Nil → the task is not relaunchable.
	Relaunch func() (SpawnResult, error)
	// ResumeFn, when non-nil, feeds new input into the task's LIVE session
	// (resume_task): tmux tasks SendKeys into the existing session, subagent
	// tasks start a new Run with framework-restored task-chain context. It
	// returns a fresh SettleDetector for the resumed round; the task then
	// re-enters the standard dense→ACK→settle lifecycle under the SAME task id.
	// Nil → the task is not resumable.
	ResumeFn func(input string) (SettleDetector, error)
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

	// Lifetime declares the job/service class (hardening-review-batch2 2.4).
	// Empty → inferred from Kind (command/subagent → job, generic → service).
	Lifetime string

	// Declarative is the serializable projection of this spec for the fact-chain
	// task_spawned record and cross-restart rebuild (R2). Optional at spawn
	// time: callers may set only closures (legacy path); when set, Spawn
	// persists it via OnSpawn so RebuildTaskRegistry can replay the task.
	Declarative *Declarative
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
	firstSettle   chan SettleSignal // cap 1: carries the first settle into the sync-wait window
	windowClosed  bool              // true once the sync-wait window ended (inline settle OR timeout)
	aliveDetached bool              // true once a service task's first stable "ready" was emitted (D4)
	detachedAt    time.Time         // when aliveDetached was set — stale/deadline observation anchor
	staleNoted    bool              // one-time stale observation notice already emitted
	watchDone     chan struct{}     // closed to retire the current watch goroutine (resume re-arms it)
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

// Result returns the latest captured output (thread-safe snapshot).
func (t *Task) Result() string { t.mu.Lock(); defer t.mu.Unlock(); return t.result }

// isActive reports whether the task is still live (dedup targets these).
func (t *Task) isActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.status {
	case TaskRunning, TaskStable, TaskAliveDetached, TaskStale, TaskSuspect:
		return true
	default:
		return false
	}
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
	case TaskRunning, TaskStable, TaskAliveDetached, TaskStale, TaskSuspect:
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
	Settled bool         // true: settled within the sync-wait window (inline)
	Signal  SettleSignal // valid when Settled
	Deduped bool         // true: an equivalent active task already existed
	Blocked string       // non-empty: spawn rejected (e.g. disk degraded, 5.4 design-report-closeout) — readable reason for the model
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
	Relaunch(id string) (SpawnResult, error)
	Resume(id string, input string) (SpawnResult, error)
}

var _ TaskController = (*TaskManager)(nil)

// originSpawner wraps a TaskController to stamp opaque origin baggage (the
// spawning turn's invocation metadata) onto each spawned task's spec — without
// the task layer needing to know about routing. It embeds TaskController so the
// full management surface (List/Get/Cancel/Relaunch) stays available to task
// tools; only Spawn is augmented. (async-result-delivery.)
type OriginSpawner struct {
	TaskController
	Origin map[string]string
}

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
	// OnSpawn (R2, resident-continuity-r2-r4): invoked after a task registers
	// (best-effort fact-chain task_spawned record; never blocks the spawn
	// path). May be nil.
	OnSpawn func(task *Task)
	// OnInlineSettle (R2): invoked when a task settles INSIDE its sync-wait
	// window (inline). Historically inline settles emitted NO record — the
	// result returns in-turn via the tool result — leaving fact-chain ghosts
	// for the registry replay. This hook emits a minimal settle record
	// (registry-only, never published to the bus — the LLM already saw the
	// result inline). May be nil.
	OnInlineSettle func(task *Task, sig SettleSignal)
	// OnCancel (R2, review 终审🔴)：任务被 Cancel 置为 cancelled 终态后调用——
	// 写事实链 cancelled 终态记录。终审发现：Cancel 仅改内存态，回放折叠
	// （spawned − 终态）下被取消的任务重启后以 suspect 复活（看板幽灵 +
	// subagent 同 Key dedup 永久锁死）。May be nil。
	OnCancel func(task *Task)
	// TerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained after settling before being pruned and its
	// resources reclaimed. It bounds the resume_task re-entry window for
	// terminal subagent tasks. Zero → defaultTerminalTTL.
	TerminalTTL time.Duration
	// SpawnGate (5.4, design-report-closeout): optional; returns a non-empty
	// readable reason to REJECT a new spawn (e.g. disk degraded). In-flight
	// tasks are never gated — a gate, not a wall. May be nil.
	SpawnGate func() string
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
	// StaleAfter is the stale-observation threshold (hardening-review-batch2
	// 2.5): a JOB-kind task alive_detached longer than this is marked TaskStale
	// (observed, one-time notice) — it is NOT terminated: age+alive never
	// prove a hang, and 7080753's force-fail mislabeled healthy services.
	// Service-kind tasks are never marked. Zero -> defaultStaleAfter; negative
	// disables observation entirely.
	StaleAfter time.Duration
	// JobDeadline is the OPTIONAL termination policy (2.6): a job-kind task
	// detached longer than this is cancelled by its owner (detector.Cancel)
	// and finalized failed — the one honest way to retire a suspected
	// dead-pipe zombie (cancel → confirmed exit → single settlement). Zero
	// (default) disables termination; negative is treated as zero.
	JobDeadline time.Duration
	// SessionTracker reports whether a task's Declarative.TaskID session is
	// still tracked by a live monitor (tmux). Wired post-construction via
	// SetSessionTracker (build_agent owns the ActionTool; TaskManager must
	// not import tool/action). May be nil.
	SessionTracker func(sessionID string) bool
}

// TaskManager is a deterministic (non-LLM) registry + scheduler for async tasks.
// The sync→async boundary is owned by each detector's detach signal (adaptive
// poll schedule); TaskManager holds no sync_wait knob.
// defaultTerminalTTL is the default grace period for retaining an exited task
// before pruning + resource reclamation (bounds the resume_task re-entry
// window for terminal subagent tasks).
const defaultTerminalTTL = 2 * time.Minute

// defaultStaleAfter bounds how long a job-kind detached task stays unnoticed
// before the stale observation fires (2.5). Observation only — see
// TaskManagerConfig.StaleAfter / JobDeadline.
const defaultStaleAfter = time.Hour

type TaskManager struct {
	mu               sync.Mutex
	tasks            map[string]*Task // id → task
	byKey            map[string]string
	onSettle         func(task *Task, sig SettleSignal)
	onSpawn          func(task *Task)
	onInlineSettle   func(task *Task, sig SettleSignal)
	onCancel         func(task *Task)
	spawnGate        func() string
	terminalTTL      time.Duration
	now              func() time.Time // injectable clock (tests); defaults to time.Now
	zombieGrace      time.Duration
	orphanGrace      time.Duration
	staleAfter       time.Duration // stale observation threshold; 0 = disabled (configured negative)
	jobDeadline      time.Duration // optional job termination policy; 0 = disabled
	isSessionTracked func(sessionID string) bool
}

// NewTaskManager creates a TaskManager.
// SetTerminalTTL hot-updates the terminal grace period (full-hot-config
// Phase 1). Reads happen under tm.mu in pruneTerminal — same lock here.
// d <= 0 keeps the current value.
func (tm *TaskManager) SetTerminalTTL(d time.Duration) {
	if tm == nil || d <= 0 {
		return
	}
	tm.mu.Lock()
	tm.terminalTTL = d
	tm.mu.Unlock()
}

// SetStaleAfter hot-updates the stale-observation threshold (full-hot-config
// Phase 1). Positive d sets the threshold; negative d disables observation;
// zero keeps the current value. Same lock protocol as SetTerminalTTL.
func (tm *TaskManager) SetStaleAfter(d time.Duration) {
	if tm == nil || d == 0 {
		return
	}
	tm.mu.Lock()
	if d > 0 {
		tm.staleAfter = d
	} else {
		tm.staleAfter = 0 // explicit disable
	}
	tm.mu.Unlock()
}

// SetJobDeadline hot-updates the OPTIONAL job termination policy. Positive d
// enables; zero keeps; negative disables termination entirely.
func (tm *TaskManager) SetJobDeadline(d time.Duration) {
	if tm == nil || d == 0 {
		return
	}
	tm.mu.Lock()
	if d > 0 {
		tm.jobDeadline = d
	} else {
		tm.jobDeadline = 0 // explicit disable
	}
	tm.mu.Unlock()
}

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
	// StaleAfter tri-state: 0 → default threshold, negative → disabled,
	// positive → explicit value. JobDeadline: non-positive → disabled.
	sa := cfg.StaleAfter
	switch {
	case sa == 0:
		sa = defaultStaleAfter
	case sa < 0:
		sa = 0
	}
	jd := cfg.JobDeadline
	if jd < 0 {
		jd = 0
	}
	return &TaskManager{
		tasks:            make(map[string]*Task),
		byKey:            make(map[string]string),
		onSettle:         cfg.OnSettle,
		onSpawn:          cfg.OnSpawn,
		onInlineSettle:   cfg.OnInlineSettle,
		onCancel:         cfg.OnCancel,
		spawnGate:        cfg.SpawnGate,
		terminalTTL:      ttl,
		zombieGrace:      zg,
		orphanGrace:      og,
		staleAfter:       sa,
		jobDeadline:      jd,
		isSessionTracked: cfg.SessionTracker,
		now:              time.Now,
	}
}

// Spawn starts a task and blocks until the first of {settle, detach}.
//   - Settle first  → SpawnResult{Settled: true, Signal} (inline).
//   - Detach first  → SpawnResult{Settled: false} (ack; tracked in background).
//   - Equivalent task active → SpawnResult{Deduped: true} (no new task).
//
// Multiple concurrent Spawn calls each wait their own detector's window in
// parallel (blocking ≈ the slowest, not the sum).
func (tm *TaskManager) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
	tm.pruneTerminal()
	// Idempotent dedup: an active task with the same Key short-circuits.
	// 8.6（review §8）：gate 在 dedup **之后**——同 Key 在飞任务命中 dedup 正常返回，
	// 不被 gate 误报 Blocked（"进行中任务不受影响"承诺）。
	tm.mu.Lock()
	if spec.Key != "" {
		if id, ok := tm.byKey[spec.Key]; ok {
			if existing, ok := tm.tasks[id]; ok && existing.isActive() {
				tm.mu.Unlock()
				detector.Cancel() // never double-run
				return SpawnResult{Task: existing, Deduped: true}
			}
		}
	}
	// 5.4（design-report-closeout）：disk degraded 时拒绝新 spawn（闸不是墙——
	// 进行中任务的 settle/轮询不受影响；nil gate = 不拒绝）。
	// 8.1（review §8）：调用方在 Spawn 前已启动实际工作（tmux 会话/子 agent goroutine），
	// gate 拒绝时 MUST detector.Cancel() 防孤儿会话/失控后台——Cancel 收敛在此单点，
	// 调用方文案须如实告知"已执行但未纳入任务层管理"。
	if tm.spawnGate != nil {
		if reason := tm.spawnGate(); reason != "" {
			tm.mu.Unlock()
			if detector != nil {
				detector.Cancel() // block adoption, not the work itself (already running)
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

	// R2: best-effort fact-chain spawn record (never blocks the spawn path;
	// the in-memory registry above is already authoritative for this process).
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
		tm.closeWindow(task, false) // settle closed the window; already consumed
		// R2: inline settles emit a registry-only settle record (sixth-round
		// fresh-eyes 🔴3: without it, the most common settle form leaves a
		// spawned-without-settled ghost for the restart replay).
		if tm.onInlineSettle != nil {
			tm.onInlineSettle(task, sig)
		}
		return SpawnResult{Task: task, Settled: true, Signal: sig}
	case <-detachCh:
		tm.closeWindow(task, true) // detach closed the window; drain any boundary settle
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
		return // nil detector: nothing to watch (pure-sync task settles via firstSettle)
	}
	for {
		select {
		case <-done:
			return
		case sig, ok := <-detector.Settled():
			if !ok {
				return
			}
			// hardening-review-batch2 1.7（fencing）：信号到达时任务已终态 →
			// 丢弃整条信号（不得改状态/通知）。注意 fence 只能放信号入口——
			// applyStatus→emitBackground 是同一信号的流水两段，emitBackground
			// 入口不得拦截（否则合法完成通知被杀）。
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
				// Still inside the window: hand the first settle to Spawn.
				select {
				case task.firstSettle <- sig:
				default: // buffer already holds one — ignore extras within window
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
// semantics for service-type tasks (D4):
//   - first stable → transition to alive-detached and emit the one-time "ready"
//     notification;
//   - once detached, subsequent stable/suspect signals (e.g. output changes, a
//     quiet service) are suppressed to avoid reclaim spam / permanent board churn;
//   - completion/failure (process death) always emits and ends the task.
func (tm *TaskManager) emitBackground(task *Task, sig SettleSignal) {
	task.mu.Lock()
	// 注：fencing 在 watch 循环信号入口——本函数与 applyStatus 是同一信号的
	// 两段流水，入口拦截会误杀合法完成通知（TestAliveDetached_CompletionEnds
	// AndNotifies 回归教训）。closeWindow drain 路径的信号产生于非终态时刻，
	// 同样不受 fence。
	switch sig.Kind {
	case SettleWatch:
		// Watch hits are pure notifications: never change lifecycle state,
		// never suppress — but DO respect merge at the detector level.
		task.mu.Unlock()
		if tm.onSettle != nil {
			tm.onSettle(task, sig)
		}
		return
	case SettleStable:
		if task.aliveDetached {
			task.mu.Unlock()
			return // already detached; suppress repeat "still alive" signals
		}
		task.aliveDetached = true
		task.status = TaskAliveDetached
		task.detachedAt = tm.now()
	case SettleSuspect:
		if task.aliveDetached {
			task.mu.Unlock()
			return // a detached service going quiet is expected; do not spam
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
	// hardening-review-batch2 1.7（fencing）：终态后迟到的 detector 信号一律
	// 丢弃——finalize 是唯一终态写入点，此处不得复活或重复结算。
	if isTerminalStatus(task.status) {
		return
	}
	task.result = sig.Output
	task.err = sig.Err
	task.settledAt = tm.now()
	switch sig.Kind {
	case SettleWatch:
		// Informational: keep lifecycle status as-is.
	case SettleCompleted:
		if sig.Err != nil {
			task.status = TaskFailed
		} else {
			task.status = TaskCompleted
		}
	case SettleStable:
		// Do not revert an already-detached service back to plain stable on a
		// subsequent output change; emitBackground keeps it alive-detached.
		// hardening-review-batch2 cold-eyes P1-1：stale 是观测事实，输出再变化
		// 不回滚（回滚即进入第三种僵尸轨道——治理面三不管）。
		if task.status != TaskAliveDetached && task.status != TaskStale {
			task.status = TaskStable
		}
	case SettleSuspect:
		if task.status != TaskAliveDetached {
			task.status = TaskSuspect
		}
	}
}

// RestoreTask（R2，resident-continuity-r2-r4 D1.3）：冷启动回放重建一个跨重启
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
		windowClosed: true, // sync-wait window is history — resume re-arms it
		// Lifecycle channels are initialized here even though the detector is
		// not: cross-restart detector state is unrecoverable (by design), but
		// Resume re-arms the watch by close(task.watchDone) — a nil channel here
		// panicked on the first post-restart resume (same crack family as the
		// pruneTerminal nil-detector panic, fixed 2026-09-13).
		watchDone:   make(chan struct{}),
		firstSettle: make(chan SettleSignal, 1),
	}
	if status == TaskAliveDetached {
		tk.aliveDetached = true // suppress repeat "ready" notifications
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, exists := tm.tasks[id]; exists {
		return tm.tasks[id] // idempotent restore (replay duplicates)
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
	tm.reconcileZombies()
	tm.mu.Lock()
	var candidates []*Task
	for _, t := range tm.tasks {
		t.mu.Lock()
		need := (t.status == TaskAliveDetached || t.status == TaskStale) && t.Spec.Alive != nil
		t.mu.Unlock()
		if need {
			candidates = append(candidates, t)
		}
	}
	tm.mu.Unlock()
	for _, t := range candidates {
		t.mu.Lock()
		if t.status != TaskAliveDetached && t.status != TaskStale {
			t.mu.Unlock()
			continue
		}
		probe := t.Spec.Alive
		t.mu.Unlock()
		if probe() {
			tm.markStaleDetached(t)
			tm.enforceJobDeadline(t)
			continue // backing session still alive - nothing more to do
		}
		out := "(backing session gone - auto-retired by liveness reconcile)"
		t.mu.Lock()
		if t.status == TaskAliveDetached || t.status == TaskStale {
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

// finalize is the SINGLE terminal-transition entry point (hardening-review-
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
// to the user as if it were the user's awaited result — leak, 2026-09-17).
// Lineage is downgraded to the dedicated "task-retired" stamp, which the
// fail-closed delivery gate holds by default.
func (tm *TaskManager) finalizeRetired(t *Task, output string, err error) {
	if t.Spec.Origin == nil {
		t.Spec.Origin = map[string]string{}
	}
	t.Spec.Origin[event.MetaKeyTriggerSource] = "task-retired"
	tm.finalize(t, SettleFailed, output, err)
}

func (tm *TaskManager) finalize(t *Task, kind SettleKind, output string, err error) {
	t.mu.Lock()
	// TOCTOU guard: candidates were collected outside the lock; another
	// reconciler may have finalized first. First terminal wins. Terminality
	// is judged by STATUS (not settledAt): resume legally restarts from
	// completed/failed and leaves the old settledAt in place.
	if isTerminalStatus(t.status) {
		t.mu.Unlock()
		return
	}
	t.result = output
	t.err = err
	t.settledAt = tm.now() // verdict time (aligns with applyStatus)
	switch {
	case kind == SettleFailed || err != nil:
		t.status = TaskFailed
	case kind == SettleCompleted:
		t.status = TaskCompleted
	default:
		// Reconcile retirement is always terminal failed/completed; anything
		// else is a caller bug — fail closed rather than guessing.
		t.status = TaskFailed
		kind = SettleFailed
		if err == nil {
			err = fmt.Errorf("finalize: non-terminal reconcile kind %q coerced to failed", kind)
		}
	}
	t.mu.Unlock()
	if tm.onSettle != nil {
		tm.onSettle(t, SettleSignal{Kind: kind, Output: output, Err: err})
	}
}

// markStaleDetached observes a job-kind alive_detached task whose detached
// stay exceeded StaleAfter: it flips the task to TaskStale (one-time notice)
// WITHOUT touching the process — age plus a live probe never prove a hang,
// and 7080753's force-fail mislabeled healthy services as failed.
// enforceJobDeadline (below) is the only age-based termination, and only
// when the host explicitly configures one.
// Called from reconcileDetached's probe-alive branch — tm.mu is NOT held
// here (probe may shell out); tm state is snapshotted before taking t.mu to
// keep the tm.mu→t.mu lock order.
func (tm *TaskManager) markStaleDetached(t *Task) {
	tm.mu.Lock()
	staleAfter, now := tm.staleAfter, tm.now()
	tm.mu.Unlock()
	if staleAfter <= 0 {
		return
	}
	t.mu.Lock()
	if (t.status != TaskAliveDetached && t.status != TaskStale) || t.detachedAt.IsZero() {
		t.mu.Unlock()
		return
	}
	if LifetimeOf(t.Spec) != LifetimeJob {
		t.mu.Unlock()
		return // service/display: long stay is by design — never staled
	}
	if now.Sub(t.detachedAt) < staleAfter || t.staleNoted {
		t.mu.Unlock()
		return
	}
	t.staleNoted = true
	t.status = TaskStale
	note := "(stale-detached: job-kind task detached past the observation threshold — suspected dead-pipe zombie; configure task_job_deadline to terminate)"
	t.mu.Unlock()
	log.Warnf("[task] stale-detached observation: task=%s note=%s", t.ID, note)
	if tm.onSettle != nil {
		// One-time notice (Watch = non-terminal notification semantics).
		tm.onSettle(t, SettleSignal{Kind: SettleWatch, Output: note})
	}
}

// enforceJobDeadline is the explicit termination policy (2.6): a job-kind
// task detached past JobDeadline is cancelled by its owner (detector.Cancel
// — which kills the backing session) and finalized failed ONCE. Service-kind
// tasks are never age-terminated. A disabled policy (<=0) is a no-op.
func (tm *TaskManager) enforceJobDeadline(t *Task) {
	tm.mu.Lock()
	deadline, now := tm.jobDeadline, tm.now()
	tm.mu.Unlock()
	if deadline <= 0 {
		return
	}
	t.mu.Lock()
	if (t.status != TaskAliveDetached && t.status != TaskStale) || t.detachedAt.IsZero() {
		t.mu.Unlock()
		return
	}
	if LifetimeOf(t.Spec) != LifetimeJob {
		t.mu.Unlock()
		return
	}
	if now.Sub(t.detachedAt) < deadline {
		t.mu.Unlock()
		return
	}
	detector := t.detector
	t.mu.Unlock()
	// Owner terminates the backing work OUTSIDE any lock (Cancel kills the
	// tmux session / goroutine), then a single failed settlement.
	if detector != nil {
		detector.Cancel()
	}
	out := "(job-deadline-exceeded: job-kind task detached past the configured deadline - cancelled by owner and retired)"
	tm.finalizeRetired(t, out, nil)
}

// sessionTrackerFn snapshots the wired tracker (lock-safe read).
func (tm *TaskManager) sessionTrackerFn() func(sessionID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.isSessionTracked
}

// RetireOrphans adjudicates reincarnation-orphan suspect tasks (§7 双通道回收):
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
	// §7 通道 2（运行期兜底）：同款孤儿判据随每次 reconcile 复评——覆盖
	// 重建后新产生的 nil-probe 孤儿（如 subagent 会话被销毁后未 settle）。
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
			continue // backing session alive - quiet, not dead
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
		if t.isTerminalExpired(now, tm.terminalTTL) {
			victims = append(victims, t)
			delete(tm.tasks, id)
			if t.Spec.Key != "" && tm.byKey[t.Spec.Key] == id {
				delete(tm.byKey, t.Spec.Key)
			}
		}
	}
	tm.mu.Unlock()
	for _, t := range victims {
		// detector 可为 nil：RestoreTask 重建的任务（跨重启不可复原，设计使然）。
		// 与 Cancel()/Spawn() 的守卫风格一致；锁内拷出避免与 resume 换 detector 竞态。
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
	// R2（review 终审🔴）：cancelled 终态入事实链（回放折叠的终态集合成员——
	// 不写则重启后以 suspect 复活）。
	if tm.onCancel != nil {
		tm.onCancel(t)
	}
	return true
}

// Relaunch re-spawns an equivalent task from the original task's spec. It runs
// the spec's Relaunch closure (set by the tool that spawned it — e.g. ActionTool
// re-runs the command in a fresh session). Returns an error if the task is
// unknown or not relaunchable.
func (tm *TaskManager) Relaunch(id string) (SpawnResult, error) {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return SpawnResult{}, fmt.Errorf("task %s not found", id)
	}
	if t.Spec.Relaunch == nil {
		return SpawnResult{}, fmt.Errorf("task %s is not relaunchable", id)
	}
	return t.Spec.Relaunch()
}

// Resume feeds new input into a task and re-enters the standard
// dense→ACK→settle lifecycle under the SAME task id.
//
// Legal source states:
//   - alive_detached / stable — the session is alive; tmux resume feeds
//     SendKeys into it (service/repl reentry).
//   - completed / failed — the previous round ended; for executor kinds that
//     are round-based (subagent: new Run + task-chain restorer), resume is the
//     natural continuation. tmux resume on a dead session fails cleanly at
//     SendKeys with an actionable error.
//
// Illegal source states: running / suspect (a round is in flight — wait and
// retry) and cancelled (session killed — relaunch or start fresh). Concurrency:
// the claim transitions to running under task.mu BEFORE ResumeFn runs, so
// parallel resumes (parallel tool execution is enabled) single-win; the loser
// is told the task is running.
func (tm *TaskManager) Resume(id string, input string) (SpawnResult, error) {
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
		// legal source states (see doc comment)
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
	// Claim the task: running under lock so a concurrent resume loses the race.
	task.status = TaskRunning
	task.mu.Unlock()

	detector, err := task.Spec.ResumeFn(input)
	if err != nil {
		// Roll back the claim; the session was not touched by us.
		task.mu.Lock()
		task.status = prevStatus
		task.mu.Unlock()
		return SpawnResult{}, fmt.Errorf("task %s resume: %w", id, err)
	}

	// Re-arm the task for a fresh round. Two shapes:
	//   - SAME detector returned (tmux Rearm: session-bound detector, round
	//     state reset internally) → the running watch keeps consuming the same
	//     Settled channel; only the sync-wait window is reopened.
	//   - NEW detector (subagent: each round is a new Run) → retire the old
	//     watch via watchDone and start a fresh one.
	task.mu.Lock()
	// Watch generation: a restored task has detector == nil (cross-restart,
	// unrecoverable) — that always counts as a new watch so the re-arm below
	// never closes a channel from a previous life.
	newWatch := task.detector == nil || detector != task.detector
	if newWatch {
		close(task.watchDone)
		task.watchDone = make(chan struct{})
		task.detector = detector
	}
	task.firstSettle = make(chan SettleSignal, 1)
	task.windowClosed = false
	task.aliveDetached = false
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
	ch     chan SettleSignal
	cancel context.CancelFunc
	detach <-chan struct{}
}

// NewFuncSettleDetector runs fn under a cancelable context and settles on return.
// An optional denseDuration overrides the default dense phase after which, if fn
// has not returned, the detector signals detach (→ async ack).
func NewFuncSettleDetector(ctx context.Context, fn func(context.Context) (string, error), denseDuration ...time.Duration) SettleDetector {
	cctx, cancel := context.WithCancel(ctx)
	d := &funcSettleDetector{ch: make(chan SettleSignal, 1), cancel: cancel}
	dd := defaultDenseDuration
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		dd = denseDuration[0]
	}
	d.detach = DetachAfter(dd, cctx.Done())
	go func() {
		out, err := fn(cctx)
		d.ch <- SettleSignal{Kind: SettleCompleted, Output: out, Err: err}
		close(d.ch)
		cancel() // stop the detach timer — the task settled
	}()
	return d
}

func (d *funcSettleDetector) Settled() <-chan SettleSignal { return d.ch }
func (d *funcSettleDetector) Detached() <-chan struct{}    { return d.detach }
func (d *funcSettleDetector) Cancel()                      { d.cancel() }
