package task // import "github.com/SpellingDragon/tagent/agent/task"

Package task 提供"把一次工作交给后台去跑"的任务层，管的对象是任务本身，而不是 它跑在哪个执行面上：

  - TaskManager：登记任务、跟踪终态、按 TTL 统一回收（生命周期锚点是派生时刻， 重入发送或 resume
    会刷新它），并把状态变化作为事实通知出去；
  - task_board：渲染在途面板，供模型在同一回合里判断"该等还是该再派"；
  - 结算检测器（NewFuncSettleDetector／NewManualDetector／NewManualDetectorDetach）：
    由宿主决定一次后台工作何时算完成，框架不猜；
  - DetachAfter／LifetimeOf：把"多久可脱离前台""还能活多久"这类策略交给声明方；
  - TaskControllerFromContext／TaskSpawnerFromContext：工具在调用上下文里取回本轮
    可用的委派入口，取不到就退化为同步执行。

三条边界：本包不决定是否投影到会话（在 memory 与 agent/compress），不决定进程如何 启动与回收（在
tool/action），也不为省事而把完成通知在入口拦掉——拦截会误杀合法的 完成信号。任何"已执行但未纳入任务层管理"的情形都必须向调用方如实说明。

CONSTANTS

const (
	LifetimeJob     = "job"
	LifetimeService = "service"
)
    LifetimeJob Task lifecycle classes: jobs are one-round work units subject
    to stale observation and an optional deadline; services are long-lived by
    design and are never age-terminated.


FUNCTIONS

func DetachAfter(d time.Duration, stop <-chan struct{}) <-chan struct{}
    DetachAfter returns a channel that is closed after d elapses, unless stop
    fires first (e.g. the detector settled/cancelled). It is the shared timer
    primitive behind the settle-or-detach contract; the returned channel closing
    means "dense phase ended — detach".

func InjectBoard(msgs []model.Message, board string) []model.Message
    InjectBoard appends the rendered board (as a user-role context message) at
    the very END of the message list — after the current input and any pending
    tool results.

    Cache rationale: the board is re-rendered before EVERY LLM call (task ages
    tick, tasks settle), so its bytes change call-to-call. Injecting it before
    the last user message broke the prompt-cache prefix at that point — every
    in-turn LLM call re-paid the whole active turn. At the tail, the cacheable
    prefix covers everything except the board itself; and the wait-guidance
    being the last thing the model reads strengthens the anti-spin teaching.
    A non-empty board string is required; callers should skip injection for "".

func LifetimeOf(spec TaskSpec) string
    LifetimeOf resolves the effective lifecycle class: an explicit declaration
    wins; otherwise Kind is the proxy (command/subagent behave as jobs — one
    round then exit; generic/unknown default to service — the conservative class
    that the stale wall never terminates).

func RenderBoard(tasks []*Task, defaultTTL time.Duration) string
    RenderBoard renders a compact, LLM-friendly snapshot of currently ACTIVE
    tasks (running/stable/alive-detached/suspect) from the registry. Terminal
    tasks (completed/failed/cancelled/dead) are aged out — they were already
    surfaced once via task_settled events, so keeping them on the board would be
    stale noise.

    The board is regenerated fresh each turn at BeforeModel time and never
    persisted, so it does NOT participate in context compression : it is a live
    recency anchor of current async state. Returns "" when no active tasks exist
    (the caller then injects nothing).

    Each row renders the task's effective REMAINING lifetime to the unified
    reaper (async-task-lifetime 10.6) — the same value reconcileTTL honors —
    so the model can decide once from a single read: every task is bounded and
    self-reclaiming, so a quiet/suspect task needs no per-turn re-arbitration.
    defaultTTL is the manager's reaper floor (TaskManager.DefaultTTL), used for
    tasks with no explicit spec.TTL.

func ShortID(id string) string
    ShortID returns a short, human-friendly prefix of a task id for display.

func WithTaskSpawner(ctx context.Context, s TaskSpawner) context.Context
    WithTaskSpawner returns a context carrying the given TaskSpawner. tagent
    sets this on the context before RunFlow so tools (e.g. ActionTool) can
    retrieve it during Call via TaskSpawnerFromContext. Context values propagate
    through the framework flow down to tool.Call (the same path that carries the
    invocation).


TYPES

type BatchRetired struct {
	Task *Task
	Sig  SettleSignal
}
    BatchRetired finalize is the SINGLE terminal-transition entry point
    (hardening-review- batch2 1.5): it stamps status/result/settledAt
    from one kind+err pair so the in-memory state, the SettleSignal kind,
    the WAL settle_status (mapper), and the feedback polarity cannot
    diverge — the 7080753 wall stamped TaskFailed in memory but signalled
    SettleCompleted, and the mapper's default branch recorded orphan/zombie
    SettleFailed as completed. Called with t.mu NOT held (takes it briefly);
    onSettle fires outside the lock, same as all emitters. Only reconcile-class
    terminals route through here; the sync-wait window's normal settle path
    (applyStatus) is unchanged. finalizeRetired is the retirement-path
    finalize (zombie/orphan/stale-deadline): the settle signal must NOT
    inherit the task's original trigger lineage (a user-spawned task retired
    by bookkeeping would otherwise be delivered back to the user as if it were
    the user's awaited result — leak, ). Lineage is downgraded to the dedicated
    "task-retired" stamp, which the fail-closed delivery gate holds by default.
    BatchRetired is one task settled inside a batch retirement: the per-task
    fact (record-only chain entry + registry fold) stays per-task; only the
    bus-side notification is collapsed.

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
    Declarative is the serializable projection of a TaskSpec — everything
    needed to rebuild the closure trio (Relaunch/ResumeFn/Alive) cross-restart
    via the tool/action closure factory. The in-process closures remain
    authoritative while alive; Declarative is what task_spawned events carry
    and what RebuildTaskRegistry replays (registry = fold of the fact chain,
    resident-continuity-r2-r4 D1).

type ManualDetector struct {
	// Has unexported fields.
}
    ManualDetector is a SettleDetector driven manually — for tests across
    packages whose signals are driven by the test.

func NewManualDetector() *ManualDetector
    NewManualDetector 构造一个人工驱动的结算探测器：结算、脱离、停止都由调用方显式触发。

func NewManualDetectorDetach(after time.Duration) *ManualDetector
    NewManualDetectorDetach 返回一个在 after 之后自动脱离前台的探测器，使"发出信号的
    时机"与一个固定时长窗口等价，便于跨包测试复用同一套时序语义。

func (m *ManualDetector) Cancel()
    Cancel 同时做两件事：记下"已取消"供 Cancelled 查询，并触发停止信号——因为模拟工作
    一被取消就结束了，不存在"取消了但还在跑"的中间态（要那一段请单独用 FireStop）。

func (m *ManualDetector) Cancelled() bool
    Cancelled 报告是否已被取消。

func (m *ManualDetector) Detached() <-chan struct{}
    Detached 交出脱离信号：关闭即表示任务可继续存活，而前台无需等待它。

func (m *ManualDetector) Done()
    Done 关闭结算通道，表示无后续异议。

func (m *ManualDetector) Emit(sig SettleSignal)
    Emit 投递一个结算信号。通道容量为 4，超出的发射会阻塞，从而让测试能观察到背压。

func (m *ManualDetector) FireDetach()
    FireDetach 关闭脱离信号，恰好一次。

func (m *ManualDetector) FireStop()
    FireStop 显式关闭停止信号，恰好一次；重复调用无副作用。

func (m *ManualDetector) Settled() <-chan SettleSignal
    Settled 交出结算信号流；Done 关闭它，因此消费方以"通道关闭"作为无进一步异议的依据。

func (m *ManualDetector) Stopped() <-chan struct{}
    Stopped mirrors the production contract (the real detector closes it when
    its producer goroutine returns). A fixture has no producer, so Cancel —
    the signal that its simulated work is over — closes it; FireStop drives it
    explicitly when a test needs the "cancelled but not yet stopped" window.

func (m *ManualDetector) TriggerDetach()
    TriggerDetach 是 FireDetach 的同义入口，供按"触发"语义书写的调用方使用。

type OriginSpawner struct {
	TaskController
	Origin map[string]string
}
    OriginSpawner originSpawner wraps a TaskController to stamp opaque origin
    baggage (the spawning turn's invocation metadata) onto each spawned task's
    spec — without the task layer needing to know about routing. It embeds
    TaskController so the full management surface (List/Get/Cancel/Relaunch)
    stays available to task tools; only Spawn is augmented.

func (o *OriginSpawner) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult
    Spawn 在 spec 未自带 Origin 时，把本包装器携带的 origin 逐键复制一份填进去（复制而非共享： 调用方随后改写自己的 map
    不会串到任务上），再委托给内层控制器。

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
    SettleDetector observes a running task and emits SettleSignals. Different
    task types provide different detectors: - tmux command → wraps TmuxMonitor
    (stable/completed/suspect) [Phase 1] - sub-agent → RunFlow returns [Phase 3]
    - generic → goroutine returns [Phase 0]

    Settled() MUST be closed when the detector will emit no further signals.

func NewFuncSettleDetector(ctx context.Context, fn func(context.Context) (string, error), denseDuration ...time.Duration) SettleDetector
    NewFuncSettleDetector runs fn under a cancelable context and settles on
    return. An optional denseDuration overrides the default dense phase after
    which, if fn has not returned, the detector signals detach (→ async ack).

type SettleKind string
    SettleKind classifies how a task reached a settle point. Detectors emit
    this deterministically; the LLM interprets ambiguous kinds (stable/suspect)
    later.

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
type SettleSignal struct {
	Kind   SettleKind
	Output string
	Err    error
}
    SettleSignal is emitted by a SettleDetector when a task reaches a settle
    point.

type SpawnResult struct {
	Task    *Task
	Settled bool
	Signal  SettleSignal
	Deduped bool
	Blocked string
}
    SpawnResult is returned by Spawn.

type Task struct {
	ID        string
	Spec      TaskSpec
	StartedAt time.Time

	// Has unexported fields.
}
    Task is a unit of async work tracked by the TaskManager.

func NewTaskFixture(id, desc string, st TaskStatus, startedAt time.Time) *Task
    NewTaskFixture builds a Task in a given status without driving the full
    spawn/settle lifecycle. For tests and board/digest previews only — real
    tasks are always produced by TaskManager.Spawn.

func (t *Task) DetachedAtMilli() int64
    DetachedAtMilli returns the alive-detached transition time (0 = never
    detached). Thread-safe snapshot for fact-chain persistence.

func (t *Task) Result() string
    Result returns the latest captured output (thread-safe snapshot).

func (t *Task) SetDetachedAtMilli(ms int64)
    SetDetachedAtMilli is the exported restore-side hook (agent package owns the
    replay; runtime transitions must use emitBackground instead).

func (t *Task) Status() TaskStatus
    Status returns the task's current status (thread-safe snapshot).

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
    TaskController is the broader task-management surface used by the live board
    and the LLM task tools (list/get/cancel). It embeds TaskSpawner so a single
    injected value serves both tool spawning and task management. *TaskManager
    implements it.

func TaskControllerFromContext(ctx context.Context) (TaskController, bool)
    TaskControllerFromContext returns the injected value as a TaskController
    (the broader surface used by the LLM task tools). The value injected via
    WithTaskSpawner is a *TaskManager, which satisfies TaskController.

type TaskManager struct {
	// Has unexported fields.
}
    TaskManager 是任务层的登记表与生命周期持有者：登记派生的任务、观察结算信号并驱动状态 迁移、按统一 TTL 回收，同时实现
    TaskController 供任务工具使用。

    它持有回调而不是具体宿主：onSpawn/onSettle/onInlineSettle/onBatchRetire/onCancel
    由 接线方提供，把状态变化落到事实链与事件总线；spawnGate/auditGate 决定"能不能派"与
    "派成什么"，因此委派策略留在宿主，任务层只管登记与回收。方法对 nil 接收者安全。

func NewTaskManager(cfg TaskManagerConfig) *TaskManager
    NewTaskManager 按配置构造管理器：任何非正数的时长取值一律回落到本包命名的默认值（终态 保留、僵尸宽限、孤儿宽限、默认 TTL）。0
    在此是"未设置"而非"无限制"——若把它当作无限制， 未配置的调用方会在无人察觉的情况下失去回收能力。

func (tm *TaskManager) BindDetector(id string, d SettleDetector) error
    BindDetector RestoreTask：冷启动回放重建一个跨重启 存续的任务——注册到 registry（复用原 id/Key
    去重语义）但不启动 watch goroutine（进程内探测器不可恢复：running 语义降级为 suspect 交 R3 存活探测
    裁决；alive-detached 原态恢复并置 aliveDetached 使 reconcileDetached 探针 路径可用）。spec
    的闭包由调用方经工厂重建（承诺表）；未重建则仅展示。 BindDetector wires a detector to an EXISTING
    (typically restored) task and starts the standard watch consumption — the
    R3 reattach bridge (hardening- review-batch2 3.1). Restored tasks have no
    in-process detector; without this binding their watch/probe signals never
    reach the manager and a "tracked session" silently loses its settle path.
    A finalized task refuses the bind (fencing). The task's watchDone retires
    the consumption on resume re-arm, same as Spawn.

func (tm *TaskManager) Cancel(id string) bool
    Cancel stops a task's underlying work and marks it cancelled.

func (tm *TaskManager) DefaultTTL() time.Duration
    DefaultTTL reports the unified reaper's current fallback lifetime
    (the manager floor used when a task's spec carries no explicit TTL).
    Exposed so board rendering shows exactly what reconcileTTL will honor
    (async-task-lifetime 10.6).

func (tm *TaskManager) Get(id string) (*Task, bool)
    Get returns a task by id.

func (tm *TaskManager) List() []*Task
    List returns a snapshot of all tracked tasks.

func (tm *TaskManager) MarkTaskRunning(id string) bool
    MarkTaskRunning（R3 2.6，TaskID 桥）：重挂发现会话存活时把重建的 suspect 任务提升回
    running（探测裁决的确定性分支；suspect→running 单向，不动其他态）。

func (tm *TaskManager) Relaunch(ctx context.Context, id string) (SpawnResult, error)
    Relaunch re-spawns an equivalent task from the original task's spec.
    It runs the spec's Relaunch closure (set by the tool that spawned it — e.g.
    ActionTool re-runs the command in a fresh session). Returns an error if the
    task is unknown or not relaunchable. ctx is the INITIATING call's context,
    forwarded verbatim to the closure so a re-entry can resolve its target on
    the version that call holds; pass context.Background() when there is no
    initiator.

func (tm *TaskManager) RenewTTLBySession(sessionID string) bool
    RenewTTLBySession resets the TTL anchor for the ACTIVE task whose backing
    session matches sessionID (async-task-lifetime 10.4). A write-type reentry
    (exec op=send) extends the task's absolute lifetime by moving the anchor to
    now, so the reaper measures ttl from this refresh rather than the original
    spawn. Read-only touches (op=peek) MUST NOT call this. Best-effort: a
    session with no matching active task (already reaped, or untracked) returns
    false.

func (tm *TaskManager) RestoreTask(id string, spec TaskSpec, startedAt time.Time, status TaskStatus) *Task
    RestoreTask 从已持久化的事实重建任务，返回登记在用的任务对象：沿用其 id、声明、派生时刻与 状态；状态为 alive_detached
    时同时标记已脱离，使其继续享受"脱离后信号抑制"的语义。

    三点关键行为：
      - 窗口标记为已关闭：重建出的任务不重启观察，因而不会因"重启后没人在看"被误判为静默或 僵尸；终态判定与 TTL 回收照常生效；
      - 幂等且不覆盖：同一 id 已在表内时直接返回既有任务——在途的真实状态不被重建值改写； spec.Key 同样只在无人占用时登记；
      - 管理器为 nil 或 id 为空时返回 nil；startedAt 为零值时取当前时刻。

func (tm *TaskManager) Resume(ctx context.Context, id string, input string) (SpawnResult, error)
    Resume feeds new input into a task and re-enters the standard
    dense→ACK→settle lifecycle under the SAME task id.

    Legal source states: - alive_detached / stable — the session is alive; tmux
    resume feeds SendKeys into it (service/repl reentry). - completed / failed —
    the previous round ended; for executor kinds that are round-based (subagent:
    new Run + task-chain restorer), resume is the natural continuation. tmux
    resume on a dead session fails cleanly at SendKeys with an actionable error.

    Illegal source states: running / suspect (a round is in flight — wait
    and retry) and cancelled (session killed — relaunch or start fresh).
    Concurrency: the claim transitions to running under task.mu BEFORE ResumeFn
    runs, so parallel resumes (parallel tool execution is enabled) single-win;
    the loser is told the task is running. ctx is the INITIATING call's context,
    forwarded to ResumeFn so the resumed round can resolve its target on the
    version that call holds; a refusal there rolls the claim back unchanged.

func (tm *TaskManager) RetireOrphans(isTracked func(sessionID string) bool) int
    RetireOrphans adjudicates reincarnation-orphan suspect tasks:
    nil-probe (Spec.Alive == nil) + declared (Declarative != nil) + backing
    session untracked + age >= orphanGrace → terminal failed via the same
    settle-once path as zombie retirement. Restored previous-life suspects
    qualify by their carried StartedAt (channel 1: called once at rebuild,
    before the suspect→running promotion, so orphans never get promoted then
    re-adjudicated); the same criterion re-runs from reconcileZombies as the
    runtime backstop (channel 2). Probe-carrying tasks and tracked sessions are
    never touched; generic display tasks without Declarative are never touched.
    Returns the number retired.

func (tm *TaskManager) SetSessionTracker(fn func(sessionID string) bool)
    SetSessionTracker wires the live-session tracking signal post-construction
    (build_agent owns the ActionTool; TaskManager must not import tool/action).

func (tm *TaskManager) SetTTLSource(src func() (terminal, defaultTTL time.Duration))
    SetTTLSource NewTaskManager creates a TaskManager. SetTTLSource installs the
    pull source for both manager-level TTL axes (terminal grace / unified-reaper
    fallback) — the replacement for the retired SetTerminalTTL/SetDefaultTTL
    pushes. The composition root injects the owner's committed-record view,
    so a hot rotation reaches the reaper at its NEXT sweep or board read with no
    per-manager write and no second authority.

func (tm *TaskManager) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult
    Spawn starts a task and blocks until the first of {settle, detach}.
    - Settle first → SpawnResult{Settled: true, Signal} (inline). - Detach first
    → SpawnResult{Settled: false} (ack; tracked in background). - Equivalent
    task active → SpawnResult{Deduped: true} (no new task).

    Multiple concurrent Spawn calls each wait their own detector's window in
    parallel (blocking ≈ the slowest, not the sum).

func (tm *TaskManager) TerminalTTL() time.Duration
    TerminalTTL reports the live terminal grace period (introspection;
    makes it a READ of the same resolution the reaper uses — the
    owner's record source when installed, else the construction value.
    introduce-durable-workflow-engine /L-3 rollback tests read this, not the
    config field). Same tm.mu lock as the reaper paths, so what a test sees is
    what pruneTerminal applied.

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
    TaskManagerConfig configures a TaskManager.

type TaskSpawner interface {
	Spawn(spec TaskSpec, detector SettleDetector) SpawnResult
}
    TaskSpawner is the narrow interface tools (e.g. ActionTool) use to
    hand a SettleDetector to the task layer at call time — injected via the
    invocation's RuntimeState — so tools stay free of any task-lifecycle state.
    *TaskManager implements it.

func TaskSpawnerFromContext(ctx context.Context) (TaskSpawner, bool)
    TaskSpawnerFromContext returns the TaskSpawner injected via WithTaskSpawner,
    or (nil, false) when none is present (in which case tools fall back to their
    synchronous behavior).

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
    TaskSpec captures enough to describe and (re)launch a task.

type TaskStatus string
    TaskStatus is the lifecycle state of a Task.

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
func (s TaskStatus) Live() bool
    Live reports whether s is still a live state (work outstanding, or a
    session another part of the system may still be using). Exported so the same
    question can be asked from outside this package with the same answer the
    board's own dedup gives (isActive below), rather than each caller re-listing
    the states from memory.

