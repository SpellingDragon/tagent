package action // import "github.com/SpellingDragon/tagent/tool/action"

Package action 提供 exec 类动作工具及其 tmux 会话承载：动作的发起与执行、会话存活
判定、完成裁决（settle）、状态跃迁通知、轮询调度、跨重启的常驻恢复，以及供跨重启 重建闭包的 Declarative
投影。长期行为判据不在此复述，见下列契约。

契约: docs/wiki/tool/tool-architecture.md#action-tool 契约:
docs/wiki/tool/tmux-action.md#liveness-first

FUNCTIONS

func DeclarativeFromArgs(args ActionArgs, sessionID string) *task.Declarative
    DeclarativeFromArgs builds the serializable projection of a command spawn.
    sessionID is the tmux session backing the task (TaskID bridge; empty for
    unnamed oneshots — those die with the round and are not restorable).

func IsTmuxAvailable() bool
    IsTmuxAvailable reports whether tmux is actually usable on this system — a
    real PATH probe. The old implementation returned NewTmuxExecutor() != nil,
    which was always true (the constructor never returns nil), making the
    availability gate at the construction site inert: no-tmux environments
    sailed through to first-execution failures.

func NamedSessionName(logical string) string
    NamedSessionName maps a caller-supplied logical name to the deterministic
    tmux session name. The "n-" prefix segment keeps named sessions visually
    and syntactically distinct from generated prefix-timestamp names. Callers
    validate the logical name first (validSessionName in action_tool.go);
    this function is the single place that knows the naming convention.

func StatusToSettle(s SessionStatus) (task.SettleKind, bool)
    StatusToSettle maps a tmux SessionStatus to a task-layer settle kind.

    It returns (kind, true) when the status is a settle point, or ("", false)
    for intermediate/suppressed states (Running, FakeDead, FakeAlive) that are
    NOT settles. The detector only makes the deterministic classification here;
    the LLM interprets ambiguous kinds (stable vs suspect) downstream.

        completed → SettleCompleted (process exited — definitely done)
        error → SettleCompleted (settled with failure; caller attaches Err)
        stable → SettleStable (output stable, process alive — usable/waiting)
        timed_out → SettleSuspect (quiet beyond fake-dead threshold — likely hung)

func SubagentSpecFromDeclarative(redispatch func(ctx context.Context, agentName, body string) (task.SpawnResult, error), decl task.Declarative) task.TaskSpec
    SubagentSpecFromDeclarative rebuilds a subagent TaskSpec (promise table:
    Relaunch✅ via redispatch through the resident agents map; Resume❌ — the
    rounds chain has no event source, cross-restart resume returns guidance).


TYPES

type ActionArgs struct {
	Command string            `json:"command"`
	Timeout int               `json:"timeout,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	IsTUI   bool              `json:"is_tui,omitempty"`
	// QuietTimeout overrides the fake-dead detection threshold for this session
	// (seconds). Silent-but-legal tasks (long downloads, compiles, inference
	// waits) produce no output while working; the default 150s threshold kills
	// them. 0 = use global default. Values < StableDuration are rejected in Call.
	QuietTimeout int `json:"quiet_timeout,omitempty"`
	// TTL is the requested ABSOLUTE lifetime (seconds) for a spawned session: the
	// unified reaper terminates its backing process and retires the task this long
	// after its last reentrant refresh (op=send/resume reset it; op=peek does not).
	// 0/omitted = the configured default TTL (empty config → 10 minutes). There is
	// NO disable sentinel and NO age exemption by mode: resident/interactive must
	// pass a large ttl or re-enter to stay alive. Orthogonal to QuietTimeout, which
	// only detects silence and never bounds total lifetime (async-task-lifetime).
	TTL int `json:"ttl,omitempty"`
	// Mode selects session liveness semantics:
	// "oneshot" (default) — command; settles on real exit; 60s quiet reports
	// stable (never kills).
	// "resident" — long-lived service (dev server / tunnel / trainer);
	// silence is healthy, no auto-kill, only unexpected death settles.
	// "interactive" — long conversational session (REPL); stable settle +
	// resume supported.
	Mode string `json:"mode,omitempty"`
	// Name: deterministic logical name for the session
	// ([a-zA-Z0-9-]{1,64}). The session becomes addressable by this name in
	// later calls (restart/exists checks); duplicate spawn under the same
	// name is refused. Empty = an auto-generated unique name.
	Name string `json:"name,omitempty"`
	// Op Session-operations. When Op != "" the call addresses an
	// existing session instead of spawning a new one:
	// Op="peek" — read incremental output since the last peek cursor
	// (tail=N caps lines; ansi=true keeps escape sequences).
	// Op="send" — inject Keys into the session (append Enter unless
	// enter=false); the session must be non-TUI.
	// Op="stop" — graceful stop: SIGTERM the pane process (fallback
	// kill-session), grace seconds then SIGKILL.
	// Target: session_id (exact) or name (logical name → n-<name>).
	Op        string `json:"op,omitempty"`
	Keys      string `json:"keys,omitempty"`
	Enter     *bool  `json:"enter,omitempty"`
	Tail      int    `json:"tail,omitempty"`
	Ansi      bool   `json:"ansi,omitempty"`
	GraceSec  int    `json:"grace,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// Watch: pattern-triggered wakeups. While the session
	// runs, output matching this regex wakes the agent (merged within a
	// 5s window; cumulative hit count reported). Primary use: resident
	// sessions (watch "ERROR|panic|OOM" on a dev server / trainer log).
	Watch string `json:"watch,omitempty"`
	// Probe: liveness check for resident sessions. A shell
	// command run every ProbeIntervalSec (default 30); after ProbeFailures
	// (default 3) consecutive failures the agent is woken once. Example:
	// "curl -sf http://localhost:8080/healthz".
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
}
    ActionArgs represents a command execution request.

type ActionTool struct {
	// Has unexported fields.
}
    ActionTool is a shell command execution tool.

    Every command runs in a tmux session and Call() blocks until the session
    reaches a stable state (Stable/Completed/Error/TimedOut) as detected
    by TmuxMonitor. The final tool result carries the command, session ID,
    final status and captured output — so the framework records it as a proper
    role=tool message.

    Tool name is "action" — it represents performing behavioral actions on
    real-world resources triggered by natural language descriptions.

func NewActionTool(opts ...ActionToolOption) *ActionTool
    NewActionTool creates a new ActionTool.

func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements tool.CallableTool.

    Creates a tmux session for the given command and blocks until the session
    reaches a stable state (Stable/Completed/Error/TimedOut) as observed by
    TmuxMonitor, at which point the final output is returned as the tool result.
    If ctx is cancelled first, the session keeps running but Call() returns with
    the context error.

func (ct *ActionTool) CanSpawnResident() bool
    CanSpawnResident reports whether a new resident/interactive session is
    allowed under the concurrency cap.

func (ct *ActionTool) Close() error
    Close stops the TmuxMonitor and reaps all sessions this instance still
    tracks: on graceful shutdown nothing keeps monitoring them, so leaving them
    alive would leak orphan sessions (and their ptys) until the next startup's
    orphan cleanup. Uses sync.Once to ensure idempotent closure.

func (ct *ActionTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

func (ct *ActionTool) IsTrackedSession(sessionID string) bool
    IsTrackedSession reports whether the monitor currently tracks the session
    （R3 2.6 TaskID 桥：重挂后按此与重建 registry 的任务重关联）。

func (ct *ActionTool) ReattachResidentSessions() int
    ReattachResidentSessions reconciles surviving tmux sessions against the
    metadata dir at startup and rebuilds tracking for resident/interactive
    sessions. Returns the number of sessions reattached. Best-effort: failures
    are logged, never fatal — a broken metadata file must not block startup.

func (ct *ActionTool) SetDefaultTTLSource(src func() time.Duration)
    SetDefaultTTLSource installs the pull source for the spawn-time default
    lifetime (spawner axis): the composition root binds it to the owner's
    committed application record, so a numeric-only rotation reaches every
    subsequent spawn without anyone pushing a number into this tool. It replaces
    SetDefaultTaskTTL, which kept a second, mutable copy of the same knob (and
    was a plain field: written on the reload goroutine, read by business turns).

func (ct *ActionTool) SetResidentMetaDir(dir string)
    SetResidentMetaDir overrides the ResidentMeta directory post-construction.

func (ct *ActionTool) SetResidentRecordSink(sink func(sessionID, kind, name, detail string))
    SetResidentRecordSink wires the fact-chain sink post-construction (R3 2.5;
    the build path obtains the ActionTool after factory creation).

func (ct *ActionTool) SpecFromDeclarative(spawner task.TaskSpawner, decl task.Declarative) task.TaskSpec
    SpecFromDeclarative rebuilds a command TaskSpec from its declarative
    projection (promise table: Relaunch✅ via fresh startSession; Alive✅ via the
    TaskID session check; Resume rebuilt as a monitored-check stub — the live
    detector binding is re-armed by the R3 reattach; until then the stub returns
    the same relaunch guidance as an unmonitored session).

func (ct *ActionTool) SweepStaleResidents(now time.Time) int
    SweepStaleResidents kills resident sessions nobody has adopted for longer
    than the TTL. Adoption paths (ReattachResidentSessions) stamp LastAdoptedAt;
    a session still tracked by the live monitor is NEVER swept regardless of
    freshness. Called after adoption in ReattachResidentSessions; safe to expose
    for a maintenance cron.

func (ct *ActionTool) TakeReattachedDetector(sessionID string) task.SettleDetector
    TakeReattachedDetector pops the reattach-built detector for a session
    (build_agent binds it to the restored task after RebuildTaskRegistry).

type ActionToolOption func(*ActionTool)
    ActionToolOption configures ActionTool.

func WithActionMonitorConfig(cfg MonitorConfig) ActionToolOption
    WithActionMonitorConfig sets a custom TmuxMonitor configuration.

func WithActionOutputDir(dir string) ActionToolOption
    WithActionOutputDir sets the directory where oversized command outputs are
    saved (defaults to the process working directory when empty). Kept separate
    from the command working directory — scratch artifacts must not force the
    command cwd away from the agent's world.

func WithActionRunAsGroup(group string) ActionToolOption
    WithActionRunAsGroup sets the group to run commands as.

func WithActionRunAsUser(user string) ActionToolOption
    WithActionRunAsUser sets the user to run commands as.

func WithActionWorkspace(dir string) ActionToolOption
    WithActionWorkspace sets the command working directory. Empty (the default)
    inherits the process working directory — keeping exec's relative paths
    consistent with the file tools' base directory, so the model sees ONE
    coherent filesystem view. A mismatch here (e.g. defaulting exec into a
    scratch dir) makes `list_file` results unreachable from `exec` and induces
    path hallucinations.

func WithDescription(desc string) ActionToolOption
    WithDescription sets the tool description.

func WithOrphanCleanupDisabled() ActionToolOption
    WithOrphanCleanupDisabled skips the startup orphan-session cleanup.
    Use it when multiple instances share one tmux server AND one session prefix
    (the cleanup would reap the other instance's live sessions); prefer distinct
    prefixes instead.

func WithResidentMetaDir(dir string) ActionToolOption
    WithResidentMetaDir（R3 2.5）：ResidentMeta 目录覆盖（默认 $TMPDIR；可指向
    持久卷以便机器重启后仍可审计/TTL sweep）。

func WithResidentRecordSink(sink func(sessionID, kind, name, detail string)) ActionToolOption
    WithResidentRecordSink：接线常驻会话 生命周期事件的事实链写入槽（build 路径→cm 记录-only
    持久化）。best-effort。

type ActionToolResult struct {
	SessionID  string `json:"session_id"`
	Command    string `json:"command"`
	OldStatus  string `json:"old_status,omitempty"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	OutputFile string `json:"output_file,omitempty"`
	Note       string `json:"note,omitempty"`
}
    ActionToolResult represents the outcome of a tmux command execution after
    the session reaches a stable state. It is returned as the tool_call result
    and rendered by the framework as a role=tool message.

type MonitorConfig struct {
	// Interval 基础轮询节奏（自适应调度下的上限见 MaxInterval）。
	Interval time.Duration
	// StableDuration 输出稳定判定阈值。
	StableDuration time.Duration
	// InteractiveStableDuration TUI 会话的稳定判定阈值。
	InteractiveStableDuration time.Duration
	// FakeDeadDuration 假死判定阈值。
	FakeDeadDuration time.Duration
	// HeartbeatCommand 探活所用命令。
	HeartbeatCommand string
	// HeartbeatTimeout 探活命令的超时。
	HeartbeatTimeout time.Duration

	// DenseInterval Adaptive poll schedule (optional; unset fields fall back to defaults, with
	// DenseInterval derived from Interval). See PollSchedule.
	DenseInterval time.Duration
	DenseDuration time.Duration
	BackoffFactor float64
	MaxInterval   time.Duration

	// ProbeUnknownLimit（R3）：连续 unknown 探测加闸阈值——达到才按 dead 处理。
	// 0 → defaultProbeUnknownLimit（3）。
	ProbeUnknownLimit int
}
    MonitorConfig holds configuration for TmuxMonitor

func DefaultMonitorConfig() MonitorConfig
    DefaultMonitorConfig returns default monitor configuration

type PollSchedule struct {
	DenseInterval time.Duration
	DenseDuration time.Duration
	BackoffFactor float64
	MaxInterval   time.Duration
}
    PollSchedule defines an adaptive per-task poll cadence: a dense phase
    (poll frequently right after spawn, for fast detection of quick commands)
    followed by a geometric backoff phase (sparse polling for long-running /
    alive-detached tasks), capped at a maximum interval. It replaces a single
    fixed Interval.

func DefaultPollSchedule() PollSchedule
    DefaultPollSchedule returns the default adaptive schedule. The dense phase
    duration approximates the retired sync_wait (so quick commands still settle
    inline), and backoff caps at a minute for long-lived sessions.

type ResidentMeta struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	Watch            string `json:"watch,omitempty"`
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
	SpawnedAt        string `json:"spawned_at"`
	// LastAdoptedAt is the last time an agent instance adopted (reattached or
	// found already-tracked) this session.
	// Sweep freshness is measured from HERE, never from SpawnedAt: a
	// long-running session re-adopted across restarts is not an orphan no
	// matter how old it is. Empty → fall back to SpawnedAt (metadata without an adoption timestamp).
	LastAdoptedAt string            `json:"last_adopted_at,omitempty"`
	Command       string            `json:"command,omitempty"`
	TaskID        string            `json:"task_id,omitempty"`
	Origin        map[string]string `json:"origin,omitempty"`
}
    ResidentMeta is the persisted parameter set needed to rebuild tracking. R3补
    Command/TaskID/Origin：Command 供人/LLM 审计与重建描述；TaskID=会话 id 桥键（与 task_spawned
    事件的 Declarative.TaskID 同源——重挂时按此与重建 registry 的任务重关联）；Origin 为可选路由 baggage
    （真相源在 task_spawned 事件，meta 侧预留零值兼容）。旧记录缺新字段：零值容错。

type SessionMode string
    SessionMode classifies how a session's liveness is interpreted by the
    monitor and the settle stream. Each mode's semantics are documented on its
    constant.

const (
	// ModeOneshot is the default command semantics: settle on exit; a 60s-quiet alive
	// session reports Stable (never Completed — see detectSessionState), and quiet_timeout
	// (if set) is a hard kill deadline.
	ModeOneshot SessionMode = "oneshot"
	// ModeResident is for long-lived services (dev servers, tunnels, training): silence
	// is HEALTHY — no stable settle, no fake-dead kill, no auto-reap. Only unexpected death
	// (Completed/Error) settles; pair with watch/probe to hear from it.
	ModeResident SessionMode = "resident"
	// ModeInteractive is for long-running conversational sessions (REPL, coding agents):
	// stable settle plus resume/send-keys semantics, with heartbeat-based fake-dead
	// detection.
	ModeInteractive SessionMode = "interactive"
)
type SessionStatus string
    SessionStatus represents the state of a tmux session

const (
	// SessionRunning 进程存活且输出未达稳定阈值的进行中态；探测不可辨且未达连续上限时也保持该态。
	SessionRunning SessionStatus = "running"
	// SessionStable 输出已稳定但进程存活、且未显式声明静默超时的判定；不视为死亡，按调度上限继续轮询。
	SessionStable SessionStatus = "stable"
	// SessionCompleted 会话已结束的终态：pane 死、命令收尾或探测彻底不可辨，随即移出监控。
	SessionCompleted SessionStatus = "completed"
	// SessionError 探测器未装配（executor 为 nil）时的终态：随即移出监控。
	SessionError SessionStatus = "error"
	// SessionFakeDead 静默越过阈值后的假死判定中间态：仅在显式声明静默超时、或心跳失败且 pane 未死时进入。
	// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
	SessionFakeDead SessionStatus = "fake_dead"
	// SessionFakeAlive 心跳仍有响应的假活判定中间态：以原会话 ID 重启以保持监控链条。
	// 契约: docs/wiki/tool/tmux-action.md#fake-alive-restart
	SessionFakeAlive SessionStatus = "fake_alive"
	// SessionTimedOut TUI 会话静默越过假死阈值后的终态（不做假死/假活探测）：随即移出监控。
	SessionTimedOut SessionStatus = "timed_out"
)
type TmuxCreateOptions struct {
	Command       string
	WorkDir       string
	IsInteractive bool
	// Mode selects the liveness interpretation (zero value = ModeOneshot).
	Mode SessionMode
	Env  map[string]string
	// Name: request a deterministic session name instead of
	// the generated prefix-timestamp. Empty = auto-generated. Non-empty
	// names must be DNS-label-safe ([a-zA-Z0-9-]{1,64}, enforced in Call) and
	// are prefixed to avoid colliding with generated names. Use-case: named
	// resident/interactive services so later calls can address them
	// (exists/restart/send) without keeping a session-id ticket.
	Name string
}
    TmuxCreateOptions defines how to create a tmux session

type TmuxExecutor struct {
	// Has unexported fields.
}
    TmuxExecutor manages tmux sessions for async command execution.

func NewTmuxExecutor(opts ...TmuxExecutorOption) *TmuxExecutor
    NewTmuxExecutor creates a new tmux executor

func (te *TmuxExecutor) CleanupOrphanSessions() int
    CleanupOrphanSessions kills prefix-matched generated-name tmux sessions.
    Called at startup: sessions from a previous (crashed or stopped) instance
    have no monitor watching them — they would never be reaped and each holds
    a pty (system-wide pty exhaustion was observed in the field). Best effort:
    a missing tmux server means nothing to clean. Returns the number killed.

    R3（orphan 语义重定义）：named 会话被排除—— cleanup 在装配时先于 reattach 执行，若纳入 named
    会话则会屠杀全部常驻会话 （修复前语义冲突：枚举双条件修复会让 cleanup 杀光 n-）。orphan=仅无主生成名 会话；named 会话由 R3
    重挂接管或由 ResidentMeta TTL sweep 兑现终局。

func (te *TmuxExecutor) CreateSession(ctx context.Context, opts TmuxCreateOptions) (*TmuxSession, error)
    CreateSession creates a new tmux session with the command

    契约: docs/wiki/tool/tmux-action.md#named-session-singleton

func (te *TmuxExecutor) GetSessionOutput(sessionID string) (string, error)
    GetSessionOutput 返回该会话当前可见的输出：优先读流式记录文件（pipe），文件缺失或 为空时回落到 capture-pane 的最近
    1000 行，该回落调用带 3s 超时。

func (te *TmuxExecutor) GetSessionPIDPublic(sessionID string) (int, error)
    GetSessionPIDPublic exposes the pane process PID lookup.

func (te *TmuxExecutor) IsPaneDead(sessionID string) bool
    IsPaneDead checks if the tmux pane is dead

func (te *TmuxExecutor) KillSession(sessionID string) error
    KillSession kills a tmux session

    契约: docs/wiki/tool/tmux-action.md#pipe-log

func (te *TmuxExecutor) ListSessions() ([]*TmuxSession, error)
    ListSessions lists all tmux sessions with our prefix

func (te *TmuxExecutor) PipeFileFor(sessionID string) string
    PipeFileFor exposes the streaming-log path for a session.

func (te *TmuxExecutor) ProcessExists(sessionID string) bool
    ProcessExists checks if the main process of a tmux session is still running

func (te *TmuxExecutor) RestartSession(sessionID string, opts TmuxCreateOptions) error
    RestartSession attempts to restart a tmux session under the SAME session
    name. This ensures the restarted session continues to be tracked by
    TmuxMonitor under its original ID — no state chain breakage.

func (te *TmuxExecutor) SendHeartbeat(sessionID string) string
    SendHeartbeat sends a heartbeat command to detect if session is alive

func (te *TmuxExecutor) SendKeys(sessionID string, keys string) error
    SendKeys sends keys to a tmux session (for interactive commands)

func (te *TmuxExecutor) SessionAlive3(sessionID string) (alive, known bool)
    SessionAlive3：三态存活探测——list-sessions 单源（会话在列表=活；不在=确定性死；命令
    err=不可辨）。known=false 时调用方 （monitor）计入 ProbeUnknownCount 连续加闸，不立即判死。

func (te *TmuxExecutor) SessionExists(sessionID string) bool
    SessionExists checks if a tmux session exists

type TmuxExecutorOption func(*TmuxExecutor)
    TmuxExecutorOption configures TmuxExecutor

func WithTmuxPrefix(prefix string) TmuxExecutorOption
    WithTmuxPrefix sets the session name prefix

func WithTmuxRunAsGroup(group string) TmuxExecutorOption
    WithTmuxRunAsGroup sets the group to run commands as

func WithTmuxRunAsUser(user string) TmuxExecutorOption
    WithTmuxRunAsUser sets the user to run commands as

func WithTmuxWorkspace(dir string) TmuxExecutorOption
    WithTmuxWorkspace sets the workspace directory

type TmuxMonitor struct {

	// StateChangeCallback is called when session state changes.
	// The callback receives session ID, old status, new status, and output snapshot.
	// It's the caller's responsibility to store events to MemoryStore.
	StateChangeCallback func(sessionID string, oldStatus, newStatus SessionStatus, output string)
	// Has unexported fields.
}
    TmuxMonitor monitors tmux sessions and detects state changes.

    契约: docs/wiki/tool/tmux-action.md#notify-gates

func NewTmuxMonitor(opts ...TmuxMonitorOption) *TmuxMonitor
    NewTmuxMonitor creates a new tmux monitor

func (tm *TmuxMonitor) AddSession(session *TmuxSession)
    AddSession adds a session to monitor

func (tm *TmuxMonitor) AddSessionWithCallback(session *TmuxSession, cb func(sessionID string, oldStatus, newStatus SessionStatus, output string))
    AddSessionWithCallback adds a session to monitoring and registers a callback
    that fires for THIS session's meaningful state changes, in addition to any
    global StateChangeCallback and under the same meaningful-state + dedup gate.
    Used by callers that want per-session routing (e.g. a per-call settle
    detector) without demultiplexing the global callback.

func (tm *TmuxMonitor) GetSession(sessionID string) (*TmuxSession, bool)
    GetSession gets a session by ID

func (tm *TmuxMonitor) GetSessionStatus(sessionID string) (SessionStatus, bool)
    GetSessionStatus returns the current status of a session (thread-safe).
    Use this instead of GetSession(...).Status to avoid data races with
    checkSession.

func (tm *TmuxMonitor) IsRunning() bool
    IsRunning returns whether the monitor is running (thread-safe).

func (tm *TmuxMonitor) ListSessions() []*TmuxSession
    ListSessions returns all monitored sessions

func (tm *TmuxMonitor) RemoveSession(sessionID string)
    RemoveSession removes a session from monitoring

func (tm *TmuxMonitor) SessionIDs() []string
    SessionIDs returns a snapshot of the currently monitored session IDs (used
    by ActionTool.Close to reap live sessions on graceful shutdown).

func (tm *TmuxMonitor) Start()
    Start starts the monitor

func (tm *TmuxMonitor) Stop()
    Stop stops the monitor

func (tm *TmuxMonitor) TouchSession(sessionID string) bool
    TouchSession re-enters dense polling for a LIVE session (resume path:
    a resumed round wants quick settle detection, exactly like a fresh spawn) by
    resetting the session's age and marking it due. Returns false if the session
    has already been reaped (not monitored) so the caller can surface the error.
    The per-session callback is NOT touched — the detector is bound to the
    session for its whole lifetime (see TmuxSettleDetector.Rearm).

type TmuxMonitorOption func(*TmuxMonitor)
    TmuxMonitorOption configures TmuxMonitor

func WithMonitorConfig(cfg MonitorConfig) TmuxMonitorOption
    WithMonitorConfig sets the monitor configuration

func WithMonitorExecutor(exec sessionInspector) TmuxMonitorOption
    WithMonitorExecutor sets the tmux executor

func WithMonitorStateChangeCallback(cb func(sessionID string, oldStatus, newStatus SessionStatus, output string)) TmuxMonitorOption
    WithMonitorStateChangeCallback sets the state change callback

type TmuxSession struct {
	ID            string
	Name          string
	Command       string
	WorkDir       string
	Status        SessionStatus
	CreatedAt     time.Time
	LastOutput    string
	LastOutputMD5 string
	StableSince   time.Time
	// IsInteractive Used as the sole stability indicator: elapsed duration determines
	// Stable / fakeDead thresholds, replacing count-based detection.
	IsInteractive bool
	IsTUI         bool
	// Mode selects the liveness interpretation (zero value = ModeOneshot).
	// See SessionMode docs. Derived: IsTUI → ModeInteractive-like handling
	// remains via IsTUI checks; Mode only extends, never overrides IsTUI.
	Mode SessionMode
	// QuietTimeout overrides the global fakeDeadDuration for THIS session
	// (silent-but-legal tasks: long downloads, compiles, inference waits).
	// Zero value = fall back to the monitor's global default (150s).
	QuietTimeout time.Duration
	PID          int
	// PipeFile is the streaming output log attached via tmux pipe-pane at
	// session creation. It records every pty byte as it arrives, so it stays
	// COMPLETE even after the pane dies (unlike capture-pane, which reads the
	// pane grid and can freeze a stale truncation frame on dead panes).
	PipeFile       string
	KillRetryCount int
	// ProbeUnknownCount：连续不可辨探测
	// （list-sessions err）计数——加闸达到 ProbeUnknownLimit 才按 dead 处理；
	// 任一可辨探测（alive/dead）清零。会话级字段（非包级），重启清零可接受。
	ProbeUnknownCount int
}
    TmuxSession represents a tmux session

type TmuxSettleDetector struct {
	// Has unexported fields.
}
    TmuxSettleDetector adapts a single tmux session's state-change stream into
    an task.SettleDetector. It is fed monitor transitions via OnStateChange
    (wired to the monitor callback for this session in the ActionTool
    integration) and emits task.SettleSignal on its channel, closing it on a
    terminal status.

    LIFETIME: the detector is bound to the SESSION, not to a round.
    A resume round does not replace it — Rearm resets the round state (output
    baseline + a fresh dense→detach timer) on the SAME detector, so the monitor
    callback wiring and the task-layer watch never change hands (no rebinding,
    no ordering discipline, no stale-signal cross-round risk).

func NewTmuxSettleDetector(sessionID string, cancelFn func(), denseDuration ...time.Duration) *TmuxSettleDetector
    NewTmuxSettleDetector creates a detector for the given session. cancelFn,
    when non-nil, reaps the underlying tmux session (kills it + drops monitor
    tracking); it runs at most once, on Cancel or on natural process death.
    An optional denseDuration overrides the default dense phase after which,
    if the session has not settled, the detector signals detach (→ async ack).

func (d *TmuxSettleDetector) Cancel()
    Cancel implements task.SettleDetector: reaps the session and closes the
    settle stream.

func (d *TmuxSettleDetector) Detached() <-chan struct{}
    Detached implements task.SettleDetector: fires at the dense→sparse boundary
    of the CURRENT round (Rearm re-creates it).

func (d *TmuxSettleDetector) Done() <-chan struct{}
    Done returns a channel closed when the detector closes (terminal status or
    Cancel). Sidecar loops (probe) select on it to exit.

func (d *TmuxSettleDetector) EmitProbeResult(ok bool, detail string)
    EmitProbeResult reports a liveness-probe outcome. A failure emits a
    watch-kind signal ONCE; repeated failures stay silent until a success resets
    the latch. This is the resident-session "service died" wakeup: the agent
    learns within one probe interval, not when a human notices.

func (d *TmuxSettleDetector) OnStateChange(newStatus SessionStatus, output string)
    OnStateChange feeds a monitor state transition for this session. It emits
    a settle signal when newStatus is a settle point, and closes the stream on
    a terminal status. Non-settle transitions (e.g. → Running) are ignored.
    The output is trimmed to the current round's baseline (Rearm), so resumed
    rounds settle with their own increment, not the whole scrollback.

func (d *TmuxSettleDetector) OnWatchOutput(output string)
    OnWatchOutput feeds the current visible pane buffer through the watch
    pattern. Called from the monitor's per-session callback on every refresh
    (including stable refreshes — a resident session's silence must not starve
    its watch). Snapshot-diff model: count hits in the WHOLE buffer, subtract
    the previous snapshot's count — no byte offsets, so pane scrollback rotation
    merely caps delta at 0 instead of mis-slicing content.

func (d *TmuxSettleDetector) Rearm(baseline int)
    Rearm resets the detector for a resume round on the SAME session: a new
    output baseline (settle outputs become this round's increment) and a fresh
    dense→detach timer (the resumed round gets its own sync-wait window).

func (d *TmuxSettleDetector) SetWatch(pattern string, window time.Duration) error
    SetWatch attaches a pattern-triggered wakeup to the detector ( C1).
    The regex is matched against INCREMENTAL output fed via OnWatchOutput;
    hits inside window are merged (one pending signal max), with the cumulative
    hit count in the signal output — a log flood of 50 ERRORs wakes the agent
    once with "xN", not 50 times.

func (d *TmuxSettleDetector) Settled() <-chan task.SettleSignal
    Settled implements task.SettleDetector.

func (d *TmuxSettleDetector) Stopped() <-chan struct{}
    Stopped implements task.SettleDetector. For a tmux-backed run the producer
    is an external session, so its stop credential is the same event that closes
    the detector: Cancel reaps the session synchronously (kill + drop monitor
    tracking) and then closes this channel; a terminal status closes it after
    the last settle was emitted. Either way nothing behind the detector can
    still touch shared state once it fires.

