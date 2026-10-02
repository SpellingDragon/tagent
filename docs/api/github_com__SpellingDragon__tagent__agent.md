package agent // import "github.com/SpellingDragon/tagent/agent"

Package agent provides tagent's core agent mechanism coordination.

- TagentAgent 是顶层装配点：EventBus + AgentLoop 提供事件驱动执行引擎，Runner 保留给 session/plugin
生命周期，MemoryPlugin 负责事件持久化与因果链，Preprocessor 负责事件过滤、token 预算与 SmartCompress。 -
核心不变量：AgentLoop 是纯事件驱动引擎、无业务语义；事件过滤、shouldCallModel、压缩等全部领域裁决在 Preprocessor。 -
TagentAgent 实现 agent.Agent，因此可被包装为 agent.Tool。

Package agent 是 tagent 的事件驱动引擎核心。50 个文件按职责分五组：

# 事件循环组(引擎主干)

- agent.go: TagentAgent 聚合根与 AgentConfig;event_loop.go: runEventLoop 主循环
(Pull 批处理/退避重试/降级 backoff);event_bus.go: EventBus+AgentEvent+ReliableBus
磁盘溢出;inject.go: InjectMessageWithSource 渗透入口

# 上下文管理组(LLM 视图)

- context_manager.go: 粘合层(投影/持久化/settle 反馈/bundle 章盖章); output_overflow.go:
outputCh 宽限+溢出票据;helpers.go/lifecycle.go: 辅助与生命周期

# 子 Agent 组

- tool_agent.go(950L 最大): AgentToolWrapper(本地/A2A 统一封装/重入/交接); a2a.go: 远程协议

# 冥想组

- meditation.go: 门控触发(novelty+idle);meditation_digest.go: digest 组装

# 可选注入组(经 TagentAgent setter)

- degradation.go/reliability 注入;governance 经 govGate;evolution 经 root

子域独立成包:task/(任务生命周期)、compress/(压缩域)、governance/(治理闸)、 reliability/(退化追踪)——各自有独立

Package agent provides tool agent registration for extensible agent composition.

Tool agents are TagentAgent instances wrapped as CallableTool via
AgentToolWrapper. This file provides the registration mechanism and the wrapper
implementation.

Registration flow:

 1. Built-in factories are registered in tagent/builtin.go init()
 2. Custom factories can be registered via RegisterToolAgent()
 3. tagent.New() resolves ToolRef entries by building referenced agents

AgentToolWrapper replaces the previous agenttool.NewTool() approach. It handles:
- Declaring event_key parameter in InputSchema (when EventParams includes it) -
Resolving event_key → fetching full event from parent MemStore - Passing event
data as external context to the sub-agent

CONSTANTS

const (
	DefaultMaxToolIterations         = 50
	DefaultSubAgentMaxToolIterations = 10
	DefaultAgentName                 = "tagent"
	DefaultAgentDescription          = "TagentAgent - AI assistant powered by tagent"

	// DefaultResumeContextRounds caps the rounds restored by the subagent
	// task-chain restorer on resume.
	DefaultResumeContextRounds = 3

	// DefaultWorkspaceCleanupInterval Workspace cleanup defaults (periodic bounding of on-disk scratch files).
	DefaultWorkspaceCleanupInterval = time.Hour
	DefaultWorkspaceCleanupMaxAge   = 24 * time.Hour
	DefaultWorkspaceCleanupMaxFiles = 200
)
    DefaultMaxToolIterations Default configuration values

const (

	// TurnSpanName 是 turn root span 名。
	TurnSpanName = "tagent.turn"
)
const ExternalContextKey = "external_context"
    ExternalContextKey is the RuntimeState key used to pass external context
    through the Invocation → A2A metadata → Invocation chain. Exported so that
    tagent.go can use it with a2aagent.WithTransferStateKey.

const SourceTask = "task"
    SourceTask identifies task_settled events on the bus (a settled background
    task reclaimed into a new turn).

VARIABLES

var (
	ErrPublishTimeout = fmt.Errorf("eventbus: publish timed out (queue full)")
	ErrNilEvent       = fmt.Errorf("eventbus: nil event")
	ErrBusClosed      = fmt.Errorf("eventbus: bus closed or not accepting")
)
    ErrPublishTimeout Publish errors — callers of PublishContext can branch on
    these; the void Publish only logs and counts them.

var ErrExecClosed = errors.New("execution generation already closed: new work refused")
    ErrExecClosed is what the execution gate returns for NEW work arriving on
    a generation whose terminal Close has already CONVERGED — runner closed,
    drain reported clean, nothing left to wait for. It is deliberately distinct
    from the mid-close case: while a close is still draining, AcquireLease DOES
    register the reference so the bounded drain surfaces ErrExecUnconverged
    instead of pretending the shutdown was clean. After convergence, handing out
    the closed executor would be precisely the failure tryAcquireActive exists
    to prevent (「running a turn on a closed executor」), so new work is refused
    instead — 's 「关闭后的 Run/Inject/Acquire 真正再进一次且被拒」.

var ErrExecUnconverged = errors.New("execution generations unconverged: held, not force-closed")
    ErrExecUnconverged is what a bounded Close returns when executions
    are still outstanding. It is never a clean close: the generations
    whose producers failed to confirm a stop stay EXPLICITLY HELD (spec
    runtime-resource-ownership 「公开关闭调用有界返回，未确认停止者继续显式持有并执行既有 poisoned 保护，
    不无限等待或强关」), and their holders keep the shared resources they ride on —
    releasing a store lease under a possibly-live writer is the failure mode
    this error exists to prevent.

var ErrExecutionCredentialUnverified = errors.New("agent: execution credential not verified at model entry (§4.5)")
    ErrExecutionCredentialUnverified is returned by the execution gate when a
    durable turn installed an echo credential (its input facts were committed)
    but the credential was never verified — the framework never fed back
    the exact committed input, or a swallowed plugin error downgraded it. :
    this MUST block the real model call so the turn cannot cross the commit gate
    on unverified input.

var ErrLoopTerminated = errors.New("agent: persistent loop already terminated — create a new agent for a fresh loop")
    ErrLoopTerminated is returned by InjectMessageContext after StopLoop:
    the instance's output channel is closed (terminal lifecycle, V15) — a silent
    acceptance here would strand the input forever (3.1).

FUNCTIONS

func BindingHolders(owner string, agents []*TagentAgent) int
    BindingHolders counts the live execution generations across `agents` whose
    OWN published face still declares a callable wrapper for `owner` — the
    usage right of /D8. It is deliberately DERIVED rather than registered:
    the binding's face is already the single routing truth, so there is
    no second table to drift and no parallel notion of who may call whom;
    the caller supplies only the roster (who exists), which is the composition
    root's own fact, never the retirement decision.

    This is what protects a deferred delegation: a version that accepted a
    request while B was routed may still legitimately call B after a newer
    generation removed the route, so B must not be retired while such a
    generation lives. It grants no execution right of its own — holding a usage
    right does not create work in B, and it is not a second task domain (J7).

func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error)
    NewA2AServer creates an A2A server that exposes the given TagentAgent.

func RebuildTaskRegistry(store memory.MemoryStore, partitionID int, tm *task.TaskManager,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) (restored int)
    RebuildTaskRegistry：冷启动从事实链 纯全量回放重建 active 任务集（registry=fold：task_spawned
    记录 − 终态 settle）。 无 compaction snapshot（任务无折叠语义）。状态映射：running→suspect（进程内
    watch goroutine 不可恢复，交 R3 存活探测裁决）；alive-detached/suspect 原态；
    终态（completed/failed/cancelled/dead，含 inline settle 记录）不重建。 rebuildClosures
    由 tool/action 提供（承诺表：command 全/subagent Relaunch/generic ❌）。
    best-effort：单条记录损坏跳过 + WARN，不阻断重建。

func RegisterPlainTool(id string, factory PlainToolFactory)
    RegisterPlainTool registers a factory for creating plain tools by ID.
    Registering the same ID twice panics, so a caller that re-registers on every
    run — a test under a -count>1 repetition gate, for example — must use a
    unique ID or reset the registry.

func RegisterToolAgent(id string, factory ToolAgentFactory)
    RegisterToolAgent registers a factory for creating tool agents by ID.
    Registering the same ID twice panics, so a caller that re-registers on every
    run — a test under a -count>1 repetition gate, for example — must use a
    unique ID or reset the registry.

func ReplayProjectionHandler(ta *TagentAgent) func(memory.FullEvent)
    ReplayProjectionHandler 返回重放双写回调。两分支：普通事件 → Append（冥想的
    agent_output 先派生 Mark）；非投影记录 → 跳过。排除判定的唯一来源是 event 包的 谓词
    IsNonProjectionRecord，与正常提交（persistBusEvent）、冷启动重建共用同一处—— 若在此自带类型枚举，就会漏排
    inbox_receipt，把内部回执注进投影，破坏 「投影＝事实链可回放折叠」这条不变量。本路径只做同点补投影，绝不在活投影上整表 Replace。

func ResolveReentryDelegation(ctx context.Context, owner *ContextManager, agentName string) (*AgentToolWrapper, *ExecLease, error)
    ResolveReentryDelegation 为一次重入（存储任务的 Resume/Relaunch）解析委派目标，并返回随附的 子调用租约：
      - 上下文里有发起方租约时，按其**同一代**解析——目标不在该代的编排里就直接报错，绝不 悄悄改投到当前生效代（重入必须留在自己那一代的语义里）；
      - 无租约时退回属主常驻面，在其当前生效代上取租约；若该代已收敛关闭则拒绝；
      - 解析不到目标时释放刚取的租约再报错，不留悬挂引用。

    调用方拿到的租约必须由它负责释放。

func SubagentRedispatcher(resolve func(ctx context.Context, agentName string) (*AgentToolWrapper, *ExecLease, error), tm *task.TaskManager) func(ctx context.Context, agentName, body string) (task.SpawnResult, error)
    SubagentRedispatcher：跨重启 subagent Relaunch 的重投递器——镜像 subagentRelaunch
    的 detector 形状 （RedispatchAsync 同步跑在 detector 的 watch goroutine 内，Spawn
    的 sync-wait 窗口语义保持；spawnKey=agentName+":"+body 与无 extraName 的原 spawn
    键一致→幂等去重覆盖）。

    resolve 是**每次重投时**对目标所属调用绑定的一次解析，而不是启动时冻结的 wrapper 快照：否则一个被后续代移除的目标仍会在这里被旧代
    wrapper 静默复活（跑 的是已退役的声明与目标），而当时的拒绝文案又声称“current org”——两者均与
    task-registry-rebuild 的「不复活已退役执行器或静默改投」相逆。调用方应传入 「按有效执行面解析」的闭包（见
    ContextManager.SubagentWrapper），这就让显式 重投与普通委派共用同一个版本真源。 resolve is
    the SAME version source ordinary delegation and 's re-entry use (see
    ResolveReentryDelegation): the initiating call's binding when the
    re-entry rides one, the effective face otherwise. Freezing a wrapper
    snapshot here — or resolving against the effective face while an
    initiator holds an older binding — would let a target a later generation
    removed be silently revived by a stored task, which is precisely what
    task-registry-rebuild「不复活已退役执行器或静默改投」 and forbid.

func TagentAgentsConstructed() int64
    TagentAgentsConstructed reports the process-wide count of TagentAgent
    constructions.

    - Callers assert DELTAS around an operation, never absolute values: tests
    share the process.

func WireOrgGeneration(owners map[string]*ContextManager, staged map[string]*StagedGeneration)
    WireOrgGeneration performs the generation-level wiring of ONE org publish
    (3.2 trunk, D8 as precision-approved round 90). It must run after every
    owner of the publish has STAGED its next generation and before ANY of them
    is activated, so no execution path can observe a half-wired generation:

    - INCOMING: each staged face's wrappers are stamped with the STAGED child
    binding they declare, and the declaring generation records a hold on it. A
    call through that wrapper therefore resolves the child through the declaring
    generation's own execution view — never the child's "current" face, never a
    captured instance.

    - OUTGOING (retroactive): the previous generations' faces were wired when
    THEY were staged — except a cold-start owner whose binding was created
    lazily and never wired. Those wrappers are stamped against the still-active
    child bindings now, with the same holds, so an in-flight caller on the
    outgoing generation keeps reaching ITS generation's targets after this
    publish retires them. Stamps are idempotent: a wrapper already wired by an
    earlier publish keeps its (still correct) target.

    The holds never enter the obligation axes (J7/J8); they only gate the
    binding-level reclaim (retired ∧ refs==0 ∧ heldBy==0).

TYPES

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

	// Has unexported fields.
}
    AgentEvent is the unified event type for the agent persistent event bus
    (the mailbox between turns). Every event flowing through the bus is an
    AgentEvent, and exactly one type serves as a bus trigger: TypeExternalInput
    (user, tmux, meditation, task settle). agent_output does not enter the bus —
    it is emitted straight to outputCh.

    Scope: the bus coordinates turns. The tool loop inside a turn remains the
    upstream synchronous ReAct (runner.Run), so no tool-use event is ever a bus

func NewExternalInputEvent(source string, msg model.Message) *AgentEvent
    NewExternalInputEvent creates an external_input event with the given source
    and message payload. The message is stored by pointer — callers MUST NOT
    mutate it after publishing.

type AgentToolWrapper struct {
	// Has unexported fields.
}
    AgentToolWrapper 把一个 agent 包装成工具：携带其描述（可由 prompt.Source
    热更）、事件参数、 父级存储与投影，以及属主的常驻 cm 句柄。常驻 cm 是**有意**存 cm 而非存某一代执行器—— cm
    活得比它发布过的任何执行器都久，长寿命的任务闭包持有它不会钉住已退役的执行器。

func NewAgentToolWrapper(
	ag agent.Agent,
	desc string,
	eventParams []string,
	parentStore memory.MemoryStore,
) *AgentToolWrapper
    NewAgentToolWrapper creates a new AgentToolWrapper. - ag: the sub-agent to
    wrap (must implement agent.Agent — local TagentAgent or remote A2AAgent)
    - desc: tool description shown to parent agent's LLM - eventParams:
    which event-derived parameters to declare and resolve - parentStore:
    parent agent's MemStore for resolving event_key to full event data

func (w *AgentToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements trpctool.CallableTool. It:
     1. Parses JSON args to extract event_keys
     2. If event_keys are present and parentStore is available, fetches full
        event data
     3. Serializes the events into RuntimeState["external_context"] (compact
        JSON)
     4. Constructs an Invocation and calls agent.Run with timeout — unified for
        local and remote
     5. For remote A2A agents, retries once on failure with 500ms backoff
     6. Collects the sub-agent's final output from the event stream

func (w *AgentToolWrapper) Declaration() *trpctool.Declaration
    Declaration implements trpctool.Tool.

func (w *AgentToolWrapper) DeclaredAgentName() string
    DeclaredAgentName reports the wrapped sub-agent's name. It is the SAME
    string the spawn path persists into a task's Declarative.AgentName (Call
    derives that field from `w.agent.Info().Name`), so it is the one key that
    both the effective- generation routing table and a cross-restart relaunch
    can resolve against — deliberately not Declaration().Name, which may re-read
    a description source. Cheap, and safe to call while holding executor locks.

func (w *AgentToolWrapper) DenseDuration() time.Duration
    DenseDuration exposes the async dense-window length (R2 redispatch detector
    shape parity with subagentRelaunch).

func (w *AgentToolWrapper) RedispatchAsync(ctx context.Context, request string) (any, error)
    RedispatchAsync runAndCollect runs the sub-agent for the given invocation
    and collects its final output from the event stream. Shared by the
    synchronous path and the async task detector. Isolation is preserved
    by Run (fresh bus/CM/projection per invocation), so this is safe to run
    concurrently / in a background task. RedispatchAsync：跨重启 subagent relaunch
    的重投递入口——以原 request 重新走 Call 的完整 spawn 路径（声明了 extra params 时按无参调用降级：plan-name
    等 extra 参数不跨重启保留，已知边界）。

func (w *AgentToolWrapper) SetAsyncDenseDuration(d time.Duration)
    SetAsyncDenseDuration overrides the dense phase for sub-agent async spawning
    (0 → detector default). Runs shorter than this settle inline; longer ones
    ack.

func (w *AgentToolWrapper) SetAsyncDisabled(disabled bool)
    SetAsyncDisabled forces this sub-agent to always run synchronously (the
    fallback switch). By default sub-agent calls are async when a task spawner
    is present in the invocation context.

func (w *AgentToolWrapper) SetDescriptionSource(src *prompt.Source)
    SetDescriptionSource sets a hot-reloadable prompt source for the tool
    description. When set, Declaration() re-reads the description from disk on
    each call, detecting file changes via mtime. Falls back to static desc if
    source is nil or read fails.

func (w *AgentToolWrapper) SetExtraParams(params []ExtraParam)
    SetExtraParams declares additional routing-level parameters for this
    tool (added to InputSchema; packed with request into a JSON message when
    present).

func (w *AgentToolWrapper) SetParentCM(cm *ContextManager)
    SetParentCM wires the RESIDENT owner's context manager into a freshly built
    delegation wrapper (cold start, same point as SetParentProjection — /D2's
    allowed exception: a not-yet-published object is configured, a published one
    is never rewritten). It is what lets a stored task's re-entry resolve its
    target against the effective face instead of the instance that spawned it.

func (w *AgentToolWrapper) SetParentProjection(p *compress.SessionProjection)
    SetParentProjection sets the PUBLISHED parentProjection used as
    the auto-inject fallback when a call runs outside any flow. It is a
    construction/publish-time wire (cold start, new generation) — /D2:
    an in-flight invocation never re-binds it, because one wrapper instance
    is shared by every concurrent call of its owner. While a flow is running,
    the flow's own projection wins (see projectionForCall).

func (w *AgentToolWrapper) SetResumeContextRounds(n int)
    SetResumeContextRounds caps how many prior rounds the task-chain restorer
    injects on resume (0 → DefaultResumeContextRounds).

type Closer interface {
	Close() error
}
    Closer is implemented by components that hold resources requiring cleanup on
    agent shutdown (e.g., ActionTool stops its TmuxMonitor). Using an interface
    avoids a direct dependency on tagent/tool.

type CognitiveAssetChange struct {
	File      string
	OldHash   string
	NewHash   string
	Size      int64
	Timestamp int64
}
    CognitiveAssetChange 是漂移审计事件载荷的最小契约（与根包 tagent.AssetChange 字段对齐；agent
    包不反向依赖根包，以本类型解耦）。

type CompressConfig struct {
	// CompactKeysListed caps the number of keys listed in the rolling
	// compaction summary (default 32); older events stay recallable.
	CompactKeysListed int
	// RecentFullCount is the number of most recent refs resolved with full
	// content from MemoryStore. Unset (0) derives keepRecent ×
	// compress.DefaultRefsPerTurn ; explicit values win.
	RecentFullCount int
	// CardMaxChars caps the index-card section of the rolling compaction
	// summary (default compress.DefaultCardMaxChars); beyond it old card lines are
	// LLM-condensed (or sink, without a summary model).
	CardMaxChars int
	// SummaryMaxTokens is the output-token budget floor for summary calls
	// (0 → pkg default). Guards reasoning models against empty Content.
	SummaryMaxTokens int
}
    CompressConfig holds compress.SmartCompressor parameters.

type ContextManager struct {
	// Has unexported fields.
}
    ContextManager is the unified component for message building, compression
    orchestration, and framework Flow execution. It replaces Preprocessor and
    FrameworkFlowAdapter.

    Prototype mapping: - OnEvents (append inputs + call model) → BuildInvocation
    + RunFlow - Compact (clean projection) → compress.ContextCompressor in
    BeforeModel callback

func NewContextManager(cfg ContextManagerConfig) *ContextManager
    NewContextManager creates a ContextManager that wraps a framework LLMAgent
    with compress.ContextCompressor as the sole BeforeModel compression
    callback.

func (cm *ContextManager) AcquireLease(kind LeaseKind) *ExecLease
    AcquireLease pins the generation NEW work starts on. The pin is retried
    when the generation read here turns out to be retired ( re-entry with no
    initiating call, and every business turn, both arrive through here): reading
    the active binding and taking the reference cannot be one atomic step,
    because PublishExecutor retires the superseded generation after releasing
    executorMu.

    A retired binding with a SUCCESSOR means the publish won the race and the
    caller must re-pin the generation actually in force. A retired binding with
    no successor is the terminal Close that retired the active binding itself —
    there is nothing newer to move to, so the reference is taken as it always
    was and the close's bounded drain reports it (ErrExecUnconverged) instead
    of pretending the shutdown was clean. Retrying that case could never make
    progress, hence the identity test rather than an open-ended loop.

func (cm *ContextManager) ActivateExecutor(s *StagedGeneration) runner.Runner
    ActivateExecutor installs a staged generation — the same linearization
    PublishExecutor performs (face first, then the binding, then retire the
    superseded one; drain-free at turn granularity). Splitting it from staging
    is what lets the composition root wire every owner of one publish before ANY
    of them goes live.

func (cm *ContextManager) BeginTurn() (runner.Runner, func())
    BeginTurn is the ONE place a business turn takes its organization execution
    binding: the armed org-config check runs first (same entry the ops hook
    CheckOrgReload uses — one publish path, no second effective route), then the
    executor in force is handed to the turn to pin.

    Call it after the input batch is frozen and OUTSIDE the transport-retry
    loop: every attempt, model iteration and tool round of that turn then runs
    on the returned runner (RunFlowWithExecutor), so a publication happening
    mid-turn cannot split the turn across generations. Sub-agent invocations
    do not call this: their instances, executor and delegation tree were
    constructed inside the generation that published them.

    The returned release MUST run when the turn ends (the loop folds it
    into its per-turn cleanup alongside endTurnSpan). 「acquire 后立即登记」:
    the in-flight reference is registered BEFORE the executor is handed out,
    so a publish and its retire-sweep landing in the gap between handing out the
    executor and entering the run body cannot close the very runner this turn
    is about to run. 一个业务 turn 只登记 EXACTLY ONCE 次，且登记在它自己的代际上： 计数由各代的 in-flight
    引用持有，不存在第二份聚合计数。

func (cm *ContextManager) BeginTurnLease() *ExecLease
    BeginTurnLease is BeginTurn with the reference handle exposed, so the
    turn can publish its lease into the call-chain context and every derived
    execution (nested delegation, transport retry, post-ACK background run) adds
    its own reference on the SAME generation instead of re-reading whatever is
    published later.

func (cm *ContextManager) BuildInvocation(batch []*AgentEvent) model.Message
    BuildInvocation merges a batch of AgentEvents into a single model.Message.

func (cm *ContextManager) CheckOrgReload()
    CheckOrgReload runs the org-config hot-reload check and WAITS for its
    result (ops/test entry point). It drives the synchronous reload path
    (orgReloadSync) when armed, so the caller observes the build/reject outcome
    on return; business turns instead use the non-blocking lazy trigger via
    BeginTurn. There is no second route by which a configuration becomes
    effective — both reach the same reload/coordinator. Falls back to the lazy
    trigger when no sync variant is armed (tests that only set SetOrgReloader
    keep their synchronous behavior).

func (cm *ContextManager) Close() error
    Close is the terminal drain, bounded. It retires the generation in force,
    waits a grace for every generation's OWN references to converge (each
    then closes itself exactly once on the ordinary reclaim path) and returns.
    Generations whose producers never confirmed a stop are NOT force-closed:
    closing an executor under a live writer is exactly the failure spec
    runtime-resource-ownership forbids (「未确认停止者继续显式持有…不无限等待或强关」). They stay
    explicitly held, remain readable through UnconvergedRefs, and are reported
    as ErrExecUnconverged so no caller can claim a clean close. The holder acts
    on it by keeping the shared resources they ride on (see agent.Close).

func (cm *ContextManager) EmitTaskCancelledRecord(tk *task.Task)
    EmitTaskCancelledRecord：Cancel 终态的事实链记录 （registry-only；形态与 inline settle
    同款）。不写则重启回放以 suspect 复活 （看板幽灵 + subagent 同 Key dedup 永久锁死）。

func (cm *ContextManager) EmitTaskInlineSettleRecord(tk *task.Task, sig task.SettleSignal)
    EmitTaskInlineSettleRecord builds and persists the registry-only settle
    record for an INLINE settle (OnInlineSettle hook): minimal terminal note
    (status+task_id; the result itself already returned in-turn as the tool
    result). Flagged task_inline_record — projection rebuild/replay skip it
    (sixth-round 🔴3: without a record, inline settles become replay ghosts).

func (cm *ContextManager) EmitTaskSpawnedRecord(tk *task.Task)
    EmitTaskSpawnedRecord builds and persists the fact-chain task_spawned
    record for a freshly registered task (OnSpawn hook). Registry-only record:
    never a projection ref, never bus-published.

func (cm *ContextManager) ExecutorConfig() ContextManagerConfig
    ExecutorConfig returns the published execution-face snapshot (model/tools/
    prompt/genConfig actually in force). Read under the executor lock:
    a candidate being built elsewhere cannot be observed as "current".
    The returned face is a private copy : mutating its Tools container or
    deref-writing its value pointers must not write through to the online
    executor.

func (cm *ContextManager) ExecutorRefs() ExecutorRefs
    ExecutorRefs 交出执行器引用面的诊断快照（退役引用、未收敛属主等），供运维判断回收是否收敛； 管理器为 nil 时返回零值快照。

func (cm *ContextManager) GetInvocationMetadata() map[string]string
    GetInvocationMetadata returns the current RunFlow's metadata (thread-safe
    copy).

func (cm *ContextManager) LastTurnDegenerate() bool
    LastTurnDegenerate reports whether the most recent RunFlow turn produced
    nothing: no tool call and no non-empty final. The persistent loop uses it to
    retry such a turn once (an occasional model hiccup would otherwise stall the
    conversation until the next external event).

func (cm *ContextManager) LastTurnOutcome() turnOutcome
    LastTurnOutcome returns the reduced terminal state of the most recent
    RunFlow attempt. The persistent loop uses it to distinguish a genuinely
    completed turn from a response-error failure or a shutdown cancellation that
    a nil transport return would otherwise hide.

func (cm *ContextManager) NewExecutorCandidate(exec ContextManagerConfig) runner.Runner
    NewExecutorCandidate constructs the next generation's executor WITHOUT
    touching the live one: no swap, no snapshot update, no state-face change
    — the caller may still abandon it. Construction is side-effect free on the
    resident cm beyond reading it; the only mutation is on the candidate's own
    tool wrappers.

func (cm *ContextManager) OrgBudgetLine() int
    OrgBudgetLine returns this manager's compressor effective trigger line
    (maxTokens × threshold) — valid for BOTH the resident manager and an
    invocation-private one. 0 when no compressor is wired.

func (cm *ContextManager) OrgKeepRecent() int
    OrgKeepRecent returns this manager's live keepRecent value (, same rationale
    as OrgBudgetLine).

func (cm *ContextManager) OutstandingRefs() int
    OutstandingRefs reports how many execution references this manager's
    generations currently hold. Owner-level decisions above the agent layer
    ask "is any execution still in flight" through this, so they read the SAME
    accounting the reclaim path acts on instead of maintaining a parallel notion
    of busy.

func (cm *ContextManager) PublishExecutor(candidate runner.Runner, exec ContextManagerConfig) runner.Runner
    PublishExecutor is the ONE linearization point of an organization version
    switch: install the candidate, record it as the published execution face,
    then retire the superseded runner. Drain-free at turn granularity — an
    in-flight turn keeps the old runner reference and finishes on it; the next
    turn picks up the new one. Returns the runner now in force.

func (cm *ContextManager) RecoveryResult() *RecoveryResult
    RecoveryResult returns the cold-start rebuild outcome (nil before the first
    rebuild). Read-only snapshot for diagnostics.

func (cm *ContextManager) RunFlow(ctx context.Context, msg model.Message) error
    RunFlow 在"当前生效"的执行面上跑一个业务回合。需要在回合边界钉住特定执行器（如热更新与 发布交错的场景）时改用
    RunFlowWithExecutor。

func (cm *ContextManager) RunFlowWithExecutor(ctx context.Context, msg model.Message, pinned runner.Runner) error
    RunFlowWithExecutor runs one business turn on the executor pinned at the
    turn boundary. pinned MUST come from BeginTurn of the same turn; when it is
    nil the executor is resolved here instead — the correct behavior for paths
    that have no turn boundary of their own (one-shot/sub-agent invocations,
    whose TagentAgent instance and executor were already constructed inside one
    generation and are never republished in place).

func (cm *ContextManager) SetInvocationMetadata(md map[string]string)
    SetInvocationMetadata sets the metadata for the current RunFlow. These
    metadata are propagated to all events derived from the source event via
    event.StateDelta with "meta_" prefix in the onEvent callback.

func (cm *ContextManager) SetOrgReloadSyncCheck(fn func())
    SetOrgReloadSyncCheck arms the synchronous ops reload entry. The tagent
    layer wires it to the same reload the background builder runs, so ops blocks
    for its result while turns stay non-blocking.

func (cm *ContextManager) SetOrgReloader(fn func())
    SetOrgReloader 装上属主 org 配置的重挂入口，使重挂能按同一身份重建参数源。

func (cm *ContextManager) SetRetirementPoke(fn func())
    SetRetirementPoke arms the composition root's lazy drain check on this
    manager. It is called once per owner at assembly time, is nil for a
    standalone agent (no org, nothing to retire), and is only ever invoked from
    a generation's own release path — it schedules work and must not run the
    drain inline.

func (cm *ContextManager) SetTriggerSource(source string)
    SetTriggerSource 记下本回合的触发源（user、meditation、task 等），供归因与投递门判定；它是
    回合本地状态，必须由每个回合自己盖章，不从上一回合继承。

func (cm *ContextManager) SetUserIDSessionID(userID, sessionID string)
    SetUserIDSessionID updates the user/session context for runner.Run.

func (cm *ContextManager) StageExecutor(cand runner.Runner, face ContextManagerConfig, runCfg *TagentConfig) *StagedGeneration
    StageExecutor opens the next generation record for `cand` WITHOUT installing
    it. The binding snapshots the given face (isolated), so the wiring pass can
    stamp the face's wrappers against exactly this generation.

func (cm *ContextManager) SubagentWrapper(name string) *AgentToolWrapper
    SubagentWrapper resolves a delegation target **against the effective
    executor face** — the very same version source that serves ordinary
    delegation, so there is no second routing truth. A name a later generation
    removed is gone from the face and the caller gets nil, which is how an
    explicit relaunch refuses to revive a retired binding instead of silently
    running on the generation that spawned it.

func (cm *ContextManager) TakeRecoveryNotice() string
    TakeRecoveryNotice returns and clears the one-shot model-facing recovery
    notice (empty = healthy path, nothing injected into the request).

func (cm *ContextManager) UnconvergedRefs() []UnconvergedRef
    UnconvergedRefs lists the retired generations still held by a live reference
    (: a bounded Close must be able to say WHO is still running instead of
    force-closing to satisfy a count). The active generation is never listed.

func (cm *ContextManager) WaitForInFlight(timeout time.Duration) bool
    WaitForInFlight blocks until every outstanding execution reference (resident
    loop turns, one-shot/sub-call turns AND post-ACK background runs — all of
    them registered on their generation) has drained, or the timeout passes;
    false means work was still active, and the caller can name it with
    UnconvergedRefs. The close sequence uses it so the output channel is never
    settled while a turn may still send to it.

type ContextManagerConfig struct {
	Name         string
	UserID       string
	SessionID    string
	Model        model.Model
	Tools        []trpctool.Tool
	SystemPrompt string
	Temperature  float64
	MaxToolIters int

	// SystemPromptSource enables hot-reload of system prompt from files.
	// When set, the system message is re-read from disk before each LLM call.
	SystemPromptSource prompt.Getter

	// ThinkingEnabled Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	Compressor   *compress.SmartCompressor
	TokenCounter compress.TokenCounter
	MaxTokens    int
	ThresholdPct float64
	MemStore     memory.MemoryStore

	// HotNumbersSource is the  pull contract: when set, the compressor reads
	// the owner's live hot view at every consumption boundary (BudgetLine/
	// Compress) instead of relying on pushed construction values. MaxTokens/
	// ThresholdPct above stay as the construction fallback (no-source path).
	HotNumbersSource func() compress.HotNumbers

	// CompactKeysListed / RecentFullCount configure compress.ContextCompressor
	// constraints (0 = package defaults; RecentFullCount derives from
	// keepRecent × compress.DefaultRefsPerTurn when unset, D6).
	CompactKeysListed int
	RecentFullCount   int
	CardMaxChars      int

	// MemPlugin Unified Runner: plugins + session service registered on the same Runner
	MemPlugin  *plugin.MemoryPlugin
	SessionSvc session.Service

	OutputCh   chan *event.Event
	Bus        *EventBus
	Projection *compress.SessionProjection
	OnEvent    func(evt *event.Event)
}
    ContextManagerConfig holds everything needed to create a ContextManager.

func BuildExecutionFace(cfg *TagentConfig) ContextManagerConfig
    BuildExecutionFace derives the publishable execution face from an assembled
    TagentConfig WITHOUT constructing a ContextManager or TagentAgent (S-A/2.3:
    the reload path de-shells existing-agent regeneration — D1「热更换代不复制 agent
    状态」). The compressor is built with exactly the construction-time options
    newContextManagerFromConfig uses (buildCompressorOpts + token counter),
    so a face assembled here is byte-equivalent to the face a discarded shell's
    ExecutorConfig() would have returned.

type DegradationBehaviors struct {
	// ModelBackoff pauses the loop between turns while DepModel is degraded.
	ModelBackoff time.Duration
	// MCPProbeEvery circuit-breaks mcp_call while DepMCP is degraded and
	// lets 1 real probe through every N calls (half-open recovery).
	MCPProbeEvery int
	// DiskBlockSpawn rejects NEW task spawns while DepDisk is degraded
	// (in-flight tasks are unaffected).
	DiskBlockSpawn bool
}
    DegradationBehaviors configures the behavior-level responses to dependency
    degradation. All fields are independent; zero values disable each behavior
    (zero behavior change). Only effective when Degradation is wired; “a gate,
    not a wall”.

type EventBus struct {
	// Has unexported fields.
}
    EventBus is a per-agent ordered event queue.

    Producers (InjectMessage, TmuxMonitor, MeditationManager, sub-agent
    callbacks, and the AgentLoop itself) call Publish to enqueue events.

    The AgentLoop is the sole consumer: it calls Pull to block until at least
    one event arrives, then non-blocking drains all remaining pending events.

    Design rationale: a single consumer (AgentLoop) means no fan-out races,
    no ordering guarantees across consumers, and simple backpressure (channel
    fills up → Publish blocks).

    Durable mode (lossless under D2): with an Inbox configured,
    ALL inbound events are persisted to inbox-v2 BEFORE the durable
    receipt — the channel carries only wake-ups, never the durable truth.
    Each message slot keeps a lossless JSON snapshot of the original AgentEvent
    (ID/Type/Source/Timestamp/full Message/business Metadata), so a restart
    restores everything inbox-v1 dropped . Durable envelopes are consumed
    strictly in enqueue order (zero-padded seq); volatile channel events are
    best-effort by definition. Receipted items replaying after a crash are
    Ack-skipped without re-execution.

func NewEventBus() *EventBus
    NewEventBus creates an EventBus backed by a buffered channel (cap=256,
    matching the historical mailbox size).

func NewReliableEventBus(spillDir string) (*EventBus, error)
    NewReliableEventBus 在 spillDir/inbox-v2 打开持久收件箱。存在旧的 *.spill
    残留或未排空的 inbox-v1 树时拒绝升级（fail-loud 并给出迁移指引）：必须由前一个二进制排空，v2 从不猜测式
    迁移。所有错误一律返回——配置要求可靠性时，绝不静默降级为易失。

func (b *EventBus) ArmRetentionFromInbox() error
    ArmRetentionFromInbox rebuilds the store's retention lease from existing
    unacked envelopes and then arms it, releasing the lifecycle scanner's
    first destructive pass. Called once at agent-open after SetRetentionGuard.
    A durable inbox with no material still arms (empty protect) so the scanner
    is not gated on an inbox that never registers. A failed enumeration does NOT
    arm (never under-protect on a partial view); the lifecycle startup grace is
    the anti-starvation backstop.

func (b *EventBus) CloseDurable() error
    CloseDurable releases the inbox (unconfirmed items stay on disk for the next
    process). Called from the agent shutdown path.

func (b *EventBus) ConfirmDurable(path string, cred reliability.ReceiptCredential) error
    ConfirmDurable records the processing receipt then Acks. : the caller
    must present the verified receipt credential issued from a legal durable
    completion (ContextManager.verifyReceiptCredential) — RecordReceipt refuses
    without it, so no bare description string or request id can confirm an
    envelope. Failure leaves the claim on disk for replay (at-least-once).

func (b *EventBus) DrainRetentionCleanups() int
    DrainRetentionCleanups 收尾延迟的 ack-清理屏障：对每个 unlink 已落地、但目录同步尚未成功的
    信封，补齐所欠屏障，并在持久化确定后为其受保护材料释放保留租约——恰好一次。由消费循环 每轮驱动，使一次不确定的 ack
    收敛为"容量＋租约各释放一次"，且不重跑输入。返回收尾的账数。

func (b *EventBus) Durable() bool
    Durable reports whether the bus was configured with a durable inbox. The
    agent constructor uses this to enforce that reliable mode never runs against
    a store lacking explicit replay capability (task 3.5): a durable inbox whose
    facts cannot be idempotently replayed would silently degrade durability.

func (b *EventBus) DurablePending() int64
    DurablePending returns the unconfirmed durable envelope count (diagnostics).

func (b *EventBus) DurableProvenance(events []*AgentEvent) [][2]string
    DurableProvenance returns deduplicated (path, requestID) pairs for every
    durable envelope consumed by a finished turn, read from the typed claim.

func (b *EventBus) PrepareEnvelope(path, receiptKey string, facts []json.RawMessage) error
    PrepareEnvelope durably freezes the per-slot prepared facts and a reserved
    receipt_key onto the claimed envelope at path, BEFORE the caller writes
    any fact (D2 write-before barrier, task 3.4). facts are slot-aligned;
    a nil entry keeps that slot's already-frozen fact (replay partial-prepare).
    An already-durable receipt_key must match (a different one is a conflict).
    The caller MUST treat a returned error as "write nothing this turn" — the
    claim stays and replays. This replaces v1's post-hoc AppendDurableEventKeys/
    RecordEventKeys writeback .

func (b *EventBus) Publish(event *AgentEvent)
    Publish 是 void 兼容入口：包装 PublishContext，把拒绝记日志并计数而非失败。
    新调用方（HTTP、宿主，以及任何要向用户报告是否受理的路径）必须使用 PublishContext／InjectMessageContext。

func (b *EventBus) PublishContext(ctx context.Context, event *AgentEvent) (PublishReceipt, error)
    PublishContext 是可判定的受理入口：成功返回凭据（易失或持久），否则返回错误—— 满/超时/已关闭 绝不报成已受理。void
    Publish 是包装本函数的兼容入口。

func (b *EventBus) PublishDropped() int64
    PublishDropped 统计经由 void 兼容入口发生的拒绝。

func (b *EventBus) PublishEnvelopeContext(ctx context.Context, source string, msgs []model.Message) (PublishReceipt, error)
    PublishEnvelopeContext accepts a WHOLE batch as ONE durable envelope : every
    message keeps its own identity in the envelope, and the batch is durable (or
    rejected) as a unit — never partially accepted. Volatile mode falls back to
    per-message PublishContext.

func (b *EventBus) Pull(ctx context.Context) ([]*AgentEvent, error)
    Pull blocks until at least one event arrives or ctx is cancelled. Then
    non-blocking drains all remaining pending events. Returns the batch and nil
    error on success. Returns nil and ctx.Err() when ctx is cancelled before any
    event arrives.

func (b *EventBus) QuarantineEnvelope(path, reason string)
    QuarantineEnvelope isolates a deterministic-conflict envelope (kept on disk
    for inspection, capacity freed). See Inbox.QuarantineEnvelope.

    : quarantine is a terminal disposition just like Ack, so it MUST release the
    envelope's retention holders — otherwise an isolated envelope's originals
    stay leased forever and can never be TTL/capacity-evicted (a lease hang).
    The leaf reads the envelope ONCE under the mutation lock and hands back
    its material together with the moved flag, so "isolated ⇒ released" holds
    without a second read that could disagree (a transient material-read
    error between two reads used to quarantine the file and strand its lease).
    releaseRetention is nil-safe; an envelope that was never armed releases
    nothing. The rename's atomicity is the dir barrier — no separate cleanup is
    owed.

func (b *EventBus) RecordCompletion(path string, completion json.RawMessage) error
    RecordCompletion durably freezes a turn's completion payload onto the
    envelope at path BEFORE its receipt is submitted. Idempotent on an identical
    payload; a differing payload on an already-frozen envelope is a conflict
    (reliability.ErrCompletionConflict) — the frozen completion is authoritative
    and the caller must NOT overwrite it. The caller treats an error as
    "receipt not yet safe": the claim stays and the completion write is retried
    in-process without re-running the model.

func (b *EventBus) ReleaseClaim(path string) error
    ReleaseClaim returns a claimed envelope to pending for ordered re-claim on a
    transient submit failure. See Inbox.ReleaseClaim.

func (b *EventBus) ResetTransitional(confirm bool) (int, error)
    ResetTransitional is the operator-invoked managed reset of
    previous-format data for this bus's inbox. It is destructive and
    requires an explicit confirm; it never runs automatically and never
    clears current-format corruption (which must surface, not be wiped).
    See reliability.Inbox.ResetTransitional.

func (b *EventBus) SetRetentionGuard(g retentionGuard)
    SetRetentionGuard injects the store's retention guard (durable inbox only).
    Must be called before ArmRetentionFromInbox; a nil guard disables
    protect/release.

func (b *EventBus) TransitionalData() (spill, v1 []string)
    TransitionalData reports previous-format (inbox-v1 / .spill) items the
    inbox found at open. They are inert — never read or consumed — and the agent
    bootstraps may surface them to the operator. Safe to call on a nil/volatile
    bus (returns nils). A managed reset (ResetTransitional) is the only thing
    that clears them.

func (b *EventBus) TryPull() []*AgentEvent
    TryPull non-blocking reads all pending events without waiting. Returns an
    empty (non-nil) slice if no events are pending. Unlike Pull, this does not
    block — it immediately returns if the channel is empty.

type ExecLease struct {
	// Has unexported fields.
}
    ExecLease is one outstanding reference on one generation. The zero value and
    a nil lease are inert, so a derived path that found no parent lease does not
    have to branch.

func (l *ExecLease) Derive(kind LeaseKind) *ExecLease
    Derive takes an additional reference of kind on the SAME generation and
    returns its own idempotent release handle. Call it BEFORE the derived work
    starts (D6「派生前获取」), so the generation cannot be reclaimed in the gap between
    handing work over and it actually running.

func (l *ExecLease) Err() error
    Err reports why this lease carries no execution authority. It is nil for
    a live reference; ErrExecClosed means the generation it was asked for had
    already converged shut, so the holder must refuse the work rather than run
    on it.

func (l *ExecLease) Generation() int64
    Generation identifies which published version this reference belongs to.

func (l *ExecLease) Kind() LeaseKind
    Kind is what this reference stands for (turn / subcall / background).

func (l *ExecLease) Release()
    Release drops the reference. Idempotent: the first call wins, so a turn
    that both returns normally and hits a cleanup defer cannot double-count (
    「全路径恰一次」).

func (l *ExecLease) Runner() runner.Runner
    Runner is the executor this lease pins; the holder runs its turn on THIS
    instance for its whole life (drain-free at turn granularity).

func (l *ExecLease) SubagentWrapper(name string) *AgentToolWrapper
    SubagentWrapper resolves a delegation target against the face of the
    generation this lease pins — the version the holder's work was selected on.
    's re-entry rule uses it so an initiator keeps ITS OWN generation's targets
    instead of whatever is published by then.

func (l *ExecLease) WithContext(ctx context.Context) context.Context
    WithContext returns ctx carrying this lease, so calls made inside it inherit
    the binding instead of re-reading whatever is published now.

type ExecutorRefs struct {
	InFlightTurns   int64            `json:"inFlightTurns"`
	SubCalls        int              `json:"subCalls"`
	BackgroundRuns  int              `json:"backgroundRuns"`
	PendingRetirees int              `json:"pendingRetirees"`
	OldestPending   time.Duration    `json:"oldestPending"`
	LeakThreshold   time.Duration    `json:"leakThreshold"`
	Generations     []GenerationRefs `json:"generations"`
}
    ExecutorRefs 是执行器引用面的诊断快照（D9：退役引用与未收敛 owner 可见； ：业务
    turn／子调用／后台执行分报，不是一个模糊总数）。 只读：无任何执行路径据它分支（回收时机由各代自己的引用数决定），因此它不会 成为第二真源。

type ExternalContextEntry struct {
	EventKey     int64  `json:"event_key"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
}
    ExternalContextEntry is the serializable representation of an external event
    for cross-process context passing via RuntimeState.

type ExtraParam struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"`
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
}
    ExtraParam declares one additional routing-level parameter for an agent-kind
    tool. Declared params are added to the tool's InputSchema and, when present
    in a call, packed together with request into a JSON message body — a
    whitelist pass-through for small routing fields (e.g. plan's action/name),
    NOT a general RPC channel.

type GenerationRefs struct {
	Generation int64          `json:"generation"`
	Owner      string         `json:"owner"`
	Retired    bool           `json:"retired"`
	Closed     bool           `json:"closed"`
	Age        time.Duration  `json:"ageNanos"`
	Total      int            `json:"totalRefs"`
	Refs       map[string]int `json:"refs"`
}
    GenerationRefs is one generation's diagnostic row.

type LeaseKind int
    LeaseKind names the usage a reference stands for, so diagnostics can report
    business turns, sub-calls and background executions separately (「后台／流／ owner
    引用分报」) instead of one blurred total.

const (
	// LeaseTurn is a business turn (persistent-loop turn or an external Run that
	// inherits nothing and therefore acquired the effective generation).
	LeaseTurn LeaseKind = iota
	// LeaseSubCall is a derived invocation: synchronous / nested delegation and
	// its transport retries.
	LeaseSubCall
	// LeaseBackground is a post-ACK background execution whose producer outlives
	// the tool call that spawned it.
	LeaseBackground
)
func (k LeaseKind) String() string
    String is the diagnostic label; it must stay stable (payload key values).

type MeditationConfig struct {
	Enabled    bool
	Interval   time.Duration
	MinGap     time.Duration
	PromptText string
	// PromptSource 用 prompt.Getter 接口（而非具体 *prompt.Source）：保持冥想提示词可注入
	// （Getter 缝，C6 遗产）；git-native 后冥想提示词同为文件即真源（mtime 热重载直生效），
	// 改动经 refine register 登记纳入评估保护。
	// *prompt.Source 满足 Getter，既有构造点零改动；Source.Get 有 nil-receiver 守卫。
	PromptSource prompt.Getter

	// DigestExtra：可选的 digest 附加段生成器——
	// 冥想自我状态摘要末尾追加（如巩固候选清单）。nil = 无附加（现状）。
	// 由装配层注入（根包 tracker），保持 agent 包对巩固机制零依赖。
	DigestExtra func() string

	// AnchorPath 是冥想门控锚点持久化路径（T-G AnchorStore）。非空则跨重启保留三锚点
	// （novelty/idle/last-meditation），重启后不立即误触发冥想；空 = 纯内存（现状，重启失忆）。
	AnchorPath string
}
    MeditationConfig is the runtime configuration for the meditation manager.
    Converted from config.MeditationConfig (string durations) by tagent.go.

type MeditationManager struct {
	// Has unexported fields.
}
    MeditationManager periodically injects "meditation" external_input events
    into the event loop when the agent has been idle for at least MinGap AND
    there has been new user input since the last meditation.

    Gating is split across two independent anchors: the idle gate is
    lineage-AGNOSTIC (any turn end counts as busy), while the novelty gate is
    INPUT-side anchored (only source=="user" injections arm it). This split
    makes output-side lineage tracking unnecessary: activity derived from a
    meditation turn (e.g. a spawned task settling as Source="task") can only
    DELAY the next meditation via the idle gate, never re-arm the novelty gate —
    which kills the self-feeding perpetual-motion loop.

    The meditation event triggers the LLM to perform context cleanup,
    deep analysis of recent memories, and skill accumulation — all guided by the
    meditation prompt.

func NewMeditationManager(cfg MeditationConfig, injector messageInjector) *MeditationManager
    NewMeditationManager creates a MeditationManager. The injector is typically
    the *TagentAgent that owns this manager.

func (m *MeditationManager) SetAnchorStore(s *reliability.AnchorStore)
    SetAnchorStore 注入锚点持久化存储（T-G AnchorStore），并 Load 恢复三锚点——跨重启保留冥想
    门控连续性（重启后不立即误触发冥想、正确计算 novelty）。Load 失败保守用当前值（不阻断启动）。

func (m *MeditationManager) SetAuditLine(fn func() string)
    SetAuditLine wires the behavior-audit digest generator (see auditLine).
    Safe to leave unset; set at assembly before the loop starts, same discipline
    as SetTaskController.

func (m *MeditationManager) SetTaskController(tc task.TaskController)
    SetTaskController wires an optional read-only task controller used to render
    the self-state digest prepended to the meditation prompt. Safe to leave
    unset (digest is omitted — meditation behavior unchanged).

func (m *MeditationManager) Start()
    Start launches the meditation ticker goroutine. Must be called after the
    owner's persistent event loop is active.

func (m *MeditationManager) Stop()
    Stop signals the meditation goroutine to stop and waits for it.

func (m *MeditationManager) UpdateLastTurnEnd(t time.Time)
    UpdateLastTurnEnd records a turn-end timestamp — the idle-gate anchor.
    Called unconditionally by runEventLoop after every RunFlow, regardless of
    trigger source or success.

func (m *MeditationManager) UpdateLastUserInput(t time.Time)
    UpdateLastUserInput records a source=="user" injection timestamp — the
    novelty-gate anchor. Called from the injection points only (inject.go);
    non-user sources (meditation/task/tmux) must never arm this gate.

type ObligationReport struct {
	Executions  int
	Invocations int
	LiveTasks   int
}
    ObligationReport answers /D7's question for one resident owner: is anything
    still depending on it that would be broken by retiring it?

    - The three axes are disjoint by construction, each read from the accounting
    that owns it, so retirement never invents a parallel notion of "busy".
    - Executions: references held by this owner's own execution generations
    (resident turns, inherited sub-calls, post-ACK background runs) — the
    same lease accounting that gates per-generation reclaim. - Invocations:
    invocation-private contexts currently running on this owner; a delegation
    builds its own context instead of opening a turn on the sub-agent's resident
    generations, so this axis is what keeps a removed owner from being retired
    underneath a call it is still serving. - LiveTasks: entries still in a
    live state on this owner's task board (running, stable service sessions,
    suspect, alive-detached). A live board entry is an obligation even with
    nothing executing, because resume/relaunch and recovery reconciliation
    still route through this owner. - Terminal board entries are deliberately
    not obligations: retained data (history, rollback config, a name that once
    existed) is never a reason to keep a running instance alive.

func (o ObligationReport) Idle() bool
    Idle reports whether nothing depends on the owner any more.

func (o ObligationReport) String() string
    String renders the report for diagnostics and refusal reasons: a held
    retirement must say WHICH obligation holds it, so the host can act instead
    of guessing.

type OrgHotParams struct {
	ThresholdPct    float64
	MaxTokens       int
	KeepRecentTasks int
	TaskTerminalTTL time.Duration
	TaskDefaultTTL  time.Duration
}
    OrgHotParams SetTriggerSource sets the trigger source for the next RunFlow
    call. OrgHotParams is the hot-applicable numeric bundle (full-hot-config
    Phase 1, ): zero/negative fields keep current settings. Structural wiring
    (memStore/bus/projection/runner) is NOT in this bundle — that follows the
    shell-rebuild path.

type OutputLimitTool struct {
	// Has unexported fields.
}
    OutputLimitTool wraps a CallableTool and handles output that exceeds
    maxChars. When the serialized result exceeds the limit, the full output is
    saved to a file and a summary with the file path is returned instead.

    This prevents invalid JSON from mechanical truncation and avoids token
    explosion from large tool results in the LLM context.

func NewOutputLimitTool(inner trpctool.Tool, maxChars int) *OutputLimitTool
    NewOutputLimitTool wraps a tool with output size interception. maxChars is
    the maximum number of characters allowed in the serialized output.

func (t *OutputLimitTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call executes the inner tool and intercepts the output if it exceeds
    maxChars.

func (t *OutputLimitTool) Declaration() *trpctool.Declaration
    Declaration returns the inner tool's declaration unchanged.

func (t *OutputLimitTool) SetWorkspace(dir string)
    SetWorkspace sets the directory for saving oversized outputs.

func (t *OutputLimitTool) Unwrap() trpctool.Tool
    Unwrap exposes the wrapped tool so a caller that must identify a
    specific inner tool can see through this transparent pass-through layer.
    Construction wraps EVERY tool — including sub-agent delegation
    wrappers — in an OutputLimitTool, so a published execution face
    holds OutputLimitTool(*AgentToolWrapper), never the bare wrapper.
    Because OutputLimitTool preserves the inner declaration unchanged,

type PlainToolFactory func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error)
    PlainToolFactory creates a plain tool (implements tool.CallableTool) from
    the given config.

func GetPlainToolFactory(id string) (PlainToolFactory, bool)
    GetPlainToolFactory returns the factory for the given ID.

type PlainToolFactoryConfig struct {
	ID          string
	Description string
	Properties  map[string]any

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Tools that need a working/output directory derive it from here (e.g. the
	// action/exec tool uses <root>/exec as its tmux command working directory).
	WorkspaceRoot string

	// WorkingDir 是 agent 的统一工作根(file tools 的 base_dir 与 exec 命令 cwd 的共同基准)。
	// 空 = 各工具回退自身默认(file base_dir="."、exec 继承进程 cwd,现状不变);非空则作为二者
	// 共同根,优先级仍低于 ToolRef.properties 的显式 base_dir/workspace。由 config.WorkingDir 注入。
	WorkingDir string

	// MemStore Runtime dependencies (optional, injected by buildAgent).
	// Most plain tools (e.g., exec) ignore these fields.
	// Sub-tools that need runtime objects (e.g., skill_search needs SkillRepo,
	// memory_query needs MemStore) extract them from here.
	MemStore         memory.MemoryStore
	SkillRepo        tagenttool.SkillRepository
	MCPToolSets      []trpctool.ToolSet
	MCPRegistry      tagenttool.MCPRegistry
	ReadPartitionIDs []int

	// Degradation 是 per-agent 五依赖退化状态机（T-G，可选）。mcp_call 工具据此上报 DepMCP
	// 退化（MCP server 连续失败→degraded，成功→恢复）。nil = 未启用退化追踪（现状）。
	Degradation *reliability.DegradationManager

	// MCPProbeEvery：DepMCP degraded 时 mcp_call 的
	// 熔断半开探测间隔（每 N 次放行 1 次）。0 = 关闭。由 buildPlainToolRef 从 agent
	// DegradationBehaviors 注入。
	MCPProbeEvery int

	// ConsolidationMinSources：memory_consolidate
	// 的 min_source_events 硬门控（实际取回源不足即拒绝）。0 = 不校验。由
	// buildPlainToolRef 从该 agent 的 memory.engine.consolidation 注入。
	ConsolidationMinSources int
}
    PlainToolFactoryConfig provides everything a factory needs to create a plain
    tool.

type PublishReceipt struct {
	RequestID string
	Durable   bool
}
    PublishReceipt is the decidable result of a context-aware acceptance (3.1).

type ReconcileSummary struct {
	Continued     int
	ReceiptsAdded int
	Cleaned       int
	Quarantined   int
	Blocked       int
}
    ReconcileSummary counts one startup reconcile pass.

type RecoveryResult struct {
	Mode          string   `json:"mode"`
	Status        string   `json:"status"`
	Scanned       int      `json:"scanned"`
	Projected     int      `json:"projected"`
	Truncated     int      `json:"truncated"`
	MissingKeys   []string `json:"missing_keys,omitempty"`
	PagesFailed   int      `json:"pages_failed"`
	BatchErrors   int      `json:"batch_errors"`
	PayloadErrors int      `json:"payload_errors"`
	DurationMS    int64    `json:"duration_ms"`
}
    RecoveryResult is the structured outcome of a cold-start projection rebuild
    : observable by the host (diagnostics) AND by the model (a one-shot tail
    notice on the first request) — a log line alone never reached the actual
    consumers of the recovery.

type ResidentTopology struct {
	// Has unexported fields.
}
    ResidentTopology is the process-wide name → resident agent binding, shared
    by pointer with every built agent (4.5).

    Why the indirection: hot reload may ADD agents to the resident topology
    while other goroutines read the table (delegation identity checks,
    diagnostics, the next shell build). Publishing a NEW immutable map through
    an atomic pointer swap keeps those readers race-free; mutating the published
    map in place would be a data race on a live map.

func NewResidentTopology(initial map[string]*TagentAgent) *ResidentTopology
    NewResidentTopology takes ownership of the initial (startup) binding table.
    The caller must not mutate the map afterwards — publish changes through Add.

func (rt *ResidentTopology) Add(adds map[string]*TagentAgent)
    Add publishes newly resident agents. An existing name is never overwritten —
    the original owner keeps the binding (D7: 同名重入复用原存储 owner，禁止第二 writer).

func (rt *ResidentTopology) Get(name string) *TagentAgent
    Get returns the resident instance for name, or nil when not resident (:
    “is this agent already built and owned?” is exactly the hot-add question).

func (rt *ResidentTopology) Names() []string
    Names returns the resident topology names.

func (rt *ResidentTopology) Snapshot() map[string]*TagentAgent
    Snapshot returns a copy of the binding table (iteration / build seeding).

func (rt *ResidentTopology) Unpublish(names []string)
    Unpublish drops names (rollback of a REFUSED candidate's adds: a merged
    identity whose generation never published must not be borrowable later).

type SelfTelemetryAuditor struct {
	// Has unexported fields.
}
    SelfTelemetryAuditor 按滑动窗口样本判定自身遥测的可见性该升到哪一档：窗口时长、
    负例占比、最少样本数与每档驻留时间都是命名常量，避免"看一眼就永久外显"或"长期沉默
    无人察觉"。它只统计自管谱系（event.SelfManagedLineage：投递门白名单之外 ∧ 冥想）
    ——这些产出不是用户发起的交互，与宿主投递白名单同源派生，没有私有清单。

func NewSelfTelemetryAuditor(onAction func(level int, ratio float64, samples int, frozen bool)) *SelfTelemetryAuditor
    NewSelfTelemetryAuditor creates an auditor; onAction receives every level
    transition AND periodic evaluation (for fact-chain recording) — may be nil.

func (a *SelfTelemetryAuditor) DigestLine() string
    DigestLine renders the deterministic self-state digest row (trajectory
    statistics belong to the reflection layer, not the resident context).
    Empty when the auditor has no samples.

func (a *SelfTelemetryAuditor) GateReason(spec task.TaskSpec) string
    GateReason implements the task layer's AuditGate contract. L2 converge
    refuses SELF-MANAGED spawns only (internal-lineage Origin — the chores that
    feed the spin); L3 freeze refuses every non-protected spawn. Protected specs
    pass both steps: the durability defense is never withdrawn for attention
    governance.

func (a *SelfTelemetryAuditor) Level() int
    Level reports the current action level (0 normal … 3 frozen).

func (a *SelfTelemetryAuditor) ObserveInput()
    ObserveInput records a non-settle environmental input (user injection) as an
    external sample — it dilutes the self-managed ratio honestly.

func (a *SelfTelemetryAuditor) ObserveInputFor(source string)
    ObserveInputFor records an injected input classified by its source
    (meditation-derived injections are self-managed traffic, user/API are not).

func (a *SelfTelemetryAuditor) ObserveSettle(metadata map[string]any)
    ObserveSettle classifies one settle event by its Origin-courier lineage
    and records the sample. self-managed = internal lineage or absent lineage
    (unknown stays conservative per the withhold philosophy). Accepts the bus
    event's map[string]any metadata (values are written as strings by every
    producer).

func (a *SelfTelemetryAuditor) Snapshot() (level int, ratio float64, samples int)
    Snapshot exposes the audit state for reflection-layer consumers (meditation
    digest): current ladder level, window ratio and sample count.

type StagedGeneration struct {
	// Has unexported fields.
}
    StagedGeneration is a prepared-but-not-installed execution generation
    (3.2 trunk). Staging exists so a MULTI-OWNER publish can wire the
    generation-level declaration holds (and stamp each face's wrappers with
    their declared target bindings) BEFORE any owner's generation becomes
    visible — the same build-then-publish discipline applies uniformly to every
    routable owner. A staged generation nobody activates is not reachable by
    any execution path; Discard closes its candidate and releases the holds it
    recorded, so a failed candidate leaves nothing behind.

func (s *StagedGeneration) Discard()
    Discard abandons a staged generation that was never activated: release the
    declaration holds it recorded during wiring and close the never-installed
    candidate. Idempotent.

type TagentAgent struct {
	// Has unexported fields.
}
    TagentAgent is tagent's top-level Agent assembly. It implements agent.Agent
    so it can be used both as a standalone agent and as a tool-agent (wrapped
    via AgentToolWrapper).

    In the event-driven architecture, TagentAgent owns an EventBus and
    AgentLoop. External inputs (user messages, tmux callbacks, meditation)
    are published to the bus; the AgentLoop consumes them, calls the model via
    Preprocessor, and dispatches tool_use events asynchronously.

func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error)
    NewTagentAgent creates a TagentAgent with the given configuration.

    - Builds MemoryStore + MemoryPlugin + compress.SmartCompressor,
    then the Preprocessor that replaces ContextIntervention.BeforeModel.
    - Builds EventBus + AgentLoop, plus SessionService + Runner as the shell
    for session and plugin management (MemoryPlugin.OnEvent, SummaryPlugin).
    - Actual execution is driven by AgentLoop, not the Runner.

func (ta *TagentAgent) AppendProjectionRef(ref memory.EventReference)
    AppendProjectionRef appends an EventReference to this agent's session
    projection: used by the mem_spill replay double-write so replayed events
    restore the store⇔projection invariant. nil-safe.

func (ta *TagentAgent) CheckOrgReload()
    CheckOrgReload runs the armed org-config hot-reload check once (ops/test
    entry point; production arms it per-LLM-call via SetOrgReloader). No-op when
    no reloader is armed (config path unknown).

func (ta *TagentAgent) CleanerStopped() bool
    CleanerStopped reports whether this instance's workspace cleaner goroutine
    has returned. Only meaningful together with CloseStarted (the close cancels
    it); the spec asks whether the maintenance producer really converged.

func (ta *TagentAgent) Close() (err error)
    Close shuts the instance down. : the FIRST call executes the close sequence;
    every other caller — concurrent or later — waits for and returns the SAME
    completion result. Closers, leases and the store release therefore run
    exactly once per instance.

func (ta *TagentAgent) CloseStarted() bool
    CloseStarted reports whether this instance's close sequence has begun.
    The first Close wins the CAS and later callers wait on its result,
    so "started" means a terminal close owns this instance. Introspection for
    /R02's witnesses: an org Close must be observable in every resident owner,
    not just the entry.

func (ta *TagentAgent) ContextManager() *ContextManager
    ContextManager exposes the resident context manager (hotswap-fix 5.7):
    org hot-reload constructs the candidate executor on THIS cm (see
    ContextManager.NewExecutorCandidate / PublishExecutor) so the
    fresh execution face is assembled against the resident state face —
    projection/bus/callbacks are never the shell's own.

func (ta *TagentAgent) DeferredCloseOutcome() (done bool, err error)
    DeferredCloseOutcome reports the terminal tail's result separately from the
    first bounded report: done=false until the deferred exit has run (or the
    close completed inline and never deferred anything). The first Close's error
    is deliberately NOT rewritten by it — 「初次错误保持可见」.

func (ta *TagentAgent) EmitSystemAlert(alert string)
    EmitSystemAlert publishes an environment-level alert (config errors,
    hot reload failures/rollbacks, degraded fallbacks) onto the persistent bus
    so the agent perceives infrastructure problems as events (external_input) on
    its next iteration, instead of them being silently confined to log files.
    : motivated by the 03:52 incident -- the hot-reloader correctly rejected a
    bad config ("parse FAILED - serving previous") but the agent never saw it;
    the follow-up restart then cold-booted the same bad config.

func (ta *TagentAgent) ExecutorConfig() ContextManagerConfig
    ExecutorConfig returns THIS agent's assembled execution face (model/tools/
    prompt/genConfig — the product of the build that already passed validation).
    The hot-reload path builds a candidate shell, reads its face here,
    constructs the candidate executor on the RESIDENT ContextManager and only
    then publishes it. The face is returned by value; the caller owns the copy
    (mutating Tools must not disturb this agent).

    It replaces RebuildExecutorOn (hotswap-fix 5.7), which fused construction
    and swap and therefore could not be abandoned after construction. The
    incident it fixed still holds: the candidate is constructed ON the resident
    cm, so its BeforeModel closures read the resident projection/bus — never the
    shell's own empty one.

func (ta *TagentAgent) FindSubAgent(name string) agent.Agent
    FindSubAgent implements agent.Agent interface.

func (ta *TagentAgent) HotSnapshot() (OrgHotParams, bool)
    HotSnapshot returns the owner's CURRENT effective hot bundle — resolved
    solely through the installed source ( end state: the committed application
    record after the first commit, the construction bundle before it / for
    a standalone agent; NewTagentAgent always installs one, so ok=false only
    guards a hand-built agent). The second authority this replaces was the
    reloader-pushed hotSnapshot cache, whose rotation had to be kept in step
    with the record by hand at every commit point.

func (ta *TagentAgent) Info() agent.Info
    Info implements agent.Agent interface.

func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent)
    IngestExternalEvents 把外部事件暂存，供本 agent 的下一次 Run 摄入（direct 兼容入口）。
    它是单槽交收而非历史缓冲，并有守卫使并发 Run 的取走与本次写入互不撕裂。 主委托路径经调用的 RuntimeState
    传递上下文，从不碰这个共享槽。

func (ta *TagentAgent) InjectEnvelope(ctx context.Context, source string, msgs []model.Message) (requestID string, durable bool, err error)
    InjectEnvelope accepts a WHOLE batch as one acceptance unit (5.2): durable
    mode persists a single multi-message envelope; the returned requestID is the
    batch's stable identity (202 semantics belong to the HTTP layer).

func (ta *TagentAgent) InjectMessage(msg model.Message)
    InjectMessage injects a user message into the agent's event bus. The message
    is published to the persistent bus (not the invocation bus) so it can be
    processed by the persistent event loop.

func (ta *TagentAgent) InjectMessageContext(ctx context.Context, source string, msg model.Message) (PublishReceipt, error)
    InjectMessageContext 是可判定的注入入口：成功返回凭据（易失或持久已受理），否则返回错误—— 回路已终止、队列满、超时、持久写失败
    绝不报成已受理。宿主与 HTTP handler 必须用它； void 包装入口只留给内部生产者使用。

func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)
    InjectMessageWithMetadata injects a message with a source label and
    arbitrary metadata. The metadata is propagated to all events derived from
    this message via event.StateDelta with "meta_" prefix.

    Common metadata keys: - "chat_id": target user/session identifier for
    response routing - "user_name": human-readable user identifier for logs -
    "channel": communication channel (wechat, discord, etc.)

func (ta *TagentAgent) InjectMessageWithSource(source string, msg model.Message)
    InjectMessageWithSource injects a message with a source label that
    identifies the origin (e.g., "user", "meditation", "async_result").
    The source is propagated to outputCh events via StateDelta["trigger_source"]
    so consumers can deterministically dispatch responses without inferring.

    Messages ALWAYS go to persistentBus — never to invBus. This ensures
    that user messages sent during sub-agent execution are not lost when the
    sub-agent's invBus is discarded. The BeforeModel InjectBusInputs callback
    on the persistent ContextManager will TryPull these messages and inject them
    into the next ReAct iteration.

func (ta *TagentAgent) IsLoopActive() bool
    IsLoopActive returns true if the persistent event loop is currently running.

func (ta *TagentAgent) IsResidentAgent(name string) bool
    IsResidentAgent reports whether name is in the resident topology table.

func (ta *TagentAgent) LiveCMCount() int
    LiveCMCount reports the number of registered in-flight invocation-private
    CMs.

func (ta *TagentAgent) MarkMeditationEvent(key int64)
    MarkMeditationEvent 重放侧冥想章派生（session.go 活路径语义的镜像： trigger_source=meditation
    的 agent_output 事件 → MarkMeditationKey）。

func (ta *TagentAgent) MemStore() memory.MemoryStore
    MemStore returns the MemoryStore for direct access (e.g., by RecallTool).

func (ta *TagentAgent) Obligations() ObligationReport
    Obligations reads the three axes for this instance. Safe on a partially
    built instance (nil context manager / no board) — absent machinery counts as
    absent obligation, which is what a bare unit-test instance represents.

func (ta *TagentAgent) OrgBudgetLine() int
    OrgBudgetLine returns the compressor effective compression trigger line
    (maxTokens times the current threshold) — the real sub-model budget
    consumer, not the resident config field, so hot-param and rollback
    assertions read what the compressor actually uses. 0 when no compressor is

func (ta *TagentAgent) OrgDiagnostics() map[string]any
    OrgDiagnostics returns the current orchestration-generation diagnostic
    payload (nil when unset). Read-only by design: no execution path may branch
    on it.

func (ta *TagentAgent) OrgKeepRecent() int
    OrgKeepRecent returns the live keepRecent value (introspection, 4.6).

func (ta *TagentAgent) OrgThreshold() float64
    OrgThreshold returns the live compression threshold (introspection
    for tests/ops). It reads the compressor's resolved boundary value —
    no ContextManager mirror (D4/M-3 single source; the mirror was also a
    background-write vs read data race after hot-apply moved to the rebuild
    goroutine).

func (ta *TagentAgent) RebuildProjectionFromWAL()
    RebuildProjectionFromWAL rebuilds the projection from the fact chain at cold
    start (build_agent wiring; runs BEFORE spill replay is armed). Startup-only,
    once, into an EMPTY projection. No marker-tagged compaction event in the
    chain → D1 fallback full replay (rebuildProjectionFallback;

        spec change: WAL is the durable record — context must be

    recoverable even without compaction; supersedes the old no-op).

func (ta *TagentAgent) RebuildTaskRegistryFromWAL(store memory.MemoryStore,
	rebuildClosures func(decl task.Declarative) task.TaskSpec) int
    RebuildTaskRegistryFromWAL（R2 1.10）：冷启动任务 registry 重建入口（build 路径 在 R1
    投影重建之后调用；rebuildClosures 按承诺表由 tool 侧提供）。

func (ta *TagentAgent) ReconcileOutstanding() (ReconcileSummary, error)
    ReconcileOutstanding runs one pass over the durable inbox. A listing
    failure aborts the whole pass (never reconcile a partial view); per-envelope
    problems are classified individually so one bad envelope cannot block the
    healthy ones. It must run after the retention lease is armed (/: protection
    registered before anything converges/cleans) and before the consume loop
    starts feeding producers that may forget.

func (ta *TagentAgent) RecordCognitiveAssetChange(changes []CognitiveAssetChange)
    RecordCognitiveAssetChange 是漂移审计批次事件的事实链写入入口——进投影（被看见 是审计的最低目标），不发
    bus、不打断消息路由。

func (ta *TagentAgent) RecordResidentSession(sessionID, kind, name, detail string)
    RecordResidentSession（R3 2.5）：常驻会话生命周期事件的事实链写入入口 （ActionTool residentSink 经
    build 路径接线到这里；记录-only 不发 bus 不进投影）。

func (ta *TagentAgent) RecoveryResult() *RecoveryResult
    RecoveryResult returns the cold-start rebuild outcome for diagnostics.

func (ta *TagentAgent) RegisterCloser(c Closer)
    RegisterCloser registers a closer to be called on agent shutdown.

func (ta *TagentAgent) ResidentAgentNames() []string
    ResidentAgentNames returns the resident topology names.

func (ta *TagentAgent) ResidentTable() map[string]*TagentAgent
    ResidentTable returns the binding table copy (introspection, 4.6).

func (ta *TagentAgent) Rollback()
    Rollback triggers the wired rollback hook (R4 3.8；no-op if unset)。

func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error)
    Run 实现 agent.Agent 接口：这是子 agent 调用路径（本地由 AgentToolWrapper、远程由 A2A 使用），
    顶层使用必须走 StartLoop/InjectMessage/StopLoop。

    - 为本次调用新建 EventBus + AgentLoop，把初始消息作为 external_input 发布，返回 AgentLoop 的
    outputCh；调用方读事件直到通道关闭（上下文取消或产出 agent_output）。 - 上下文只在本次调用本地装配，绝不经过共享的 ta
    状态，因此并发 Run 无法互相注入；入口有二：RuntimeState 携带序列化的 ExternalContextEntry JSON，或
    direct 兼容入口经 IngestExternalEvents 在 Run 进入时原子排空以保持单槽交收语义。 - 租约拒绝发生在本调用计为
    live 之前：私有 CM 直接 Close 且不注册，否则清理 goroutine 永不运行，LiveCMCount 不归零、owner
    Obligations 到不了零、退役排空挂死。 - 终态 drain 的 defer 绑在 unbind 之前（LIFO 下后跑），把
    loop-exit 到 unbind 窗口内落地的 settle 转发到共享总线。

func (ta *TagentAgent) Runner() runner.Runner
    Runner returns the underlying Runner from ContextManager.

func (ta *TagentAgent) SessionSvc() session.Service
    SessionSvc exposes the resident session service (R4 review 🔴1:
    the executorOnly rebuild shell reuses it so session records and the
    AppendEventHook→outputCh wiring stay on the resident instance).

func (ta *TagentAgent) SetBundleIDProvider(fn func() string)
    SetBundleIDProvider wires the active-bundle lookup used by both event
    persistence paths to stamp bundle_id into FullEvent.Metadata . Entry-only;
    nil/no-active -> no stamp.

func (ta *TagentAgent) SetDelegationOwnerCM(cm *ContextManager)
    SetDelegationOwnerCM wires the resident owner's ContextManager into this
    agent's delegation wrappers, pierce-through the same transparent chain
    SetToolParentProjection uses (/W-1: asserting the bare type would miss every
    OutputLimitTool-wrapped wrapper). Cold start only — like the projection
    wire, it configures a not-yet-serving object and is the sole publish of that
    binding.

    It is what lets a stored task's Resume/Relaunch resolve its target on the
    generation in force AT RE-ENTRY instead of the instance that spawned it.

func (ta *TagentAgent) SetHotSource(src func() (OrgHotParams, bool))
    SetHotSource installs the owner's RECORD-BACKED hot-param source (S-C/2.3):
    a closure reading the single committed application record (injected once
    by the composition root after the coordinator exists; it always reads the
    LATEST record, so every commit — swap/recordHotApply/recordRollback — is
    the single writer by construction). While installed, HotSnapshot resolves
    through it; the construction-seeded snapshot cache remains only as the
    fallback for standalone/bare-constructed agents with no record to read.

func (ta *TagentAgent) SetOrgDiagnostics(fn func() map[string]any)
    SetOrgDiagnostics registers the orchestration-generation diagnostic provider
    。由装配层（tagent 包）在**启动期一次性**注入：它拥有 payload 形状， 本包不知道指纹/序号的含义。与 SetRollbackFn
    不同规——后者每次发布重写（故用 atomic），而本字段运行期只读，所以必须是启动期注入；在 reloader 里调它就错了。

func (ta *TagentAgent) SetOrgReloadSyncCheck(fn func())
    SetOrgReloadSyncCheck arms the synchronous org-reload entry used by ops
    (CheckOrgReload / Rollback semantics): it blocks until the request's build
    or rejection is settled, while business turns keep the non-blocking lazy
    trigger .

func (ta *TagentAgent) SetOrgReloader(fn func())
    SetOrgReloader arms a lazy org-config check invoked before each LLM call
    (agent-config-hot-reload, incremental A). The tagent layer wires this when a
    config path is known (WithConfigPath). fn must be cheap when nothing changed
    and must never fail the calling path.

    D3: production arms a NON-BLOCKING trigger here (schedule a merged
    background rebuild); the synchronous ops path is SetOrgReloadSyncCheck.

func (ta *TagentAgent) SetResidentTable(rt *ResidentTopology)
    SetResidentTable installs the shared binding table (4.5); the hot-reload
    shell borrows per-agent resources through it.

func (ta *TagentAgent) SetRollbackFn(fn func())
    SetRollbackFn wires the rollback hook (R4 3.8；tagent 包懒检查闭包注入——按
    ring 2 上一代配置重建并 Swap 回；宿主/运维可调 Rollback())。可安全地从发布 goroutine 重复调用：字段是
    atomic.Pointer，Rollback 读到的是完整闭包指针。 fn == nil 意为“摘钩”：必须存 **空指针**而不是“指向
    nil func 的指针”——否则 Rollback 的 nil 判定会骗过它并解引用空 func（这一条由 e2e 测的
    “SetRollbackFn(nil) 后 Rollback() 应 no-op” 钉住）。

func (ta *TagentAgent) SetStoreOwnerRevoker(fn func())
    SetStoreOwnerRevoker installs the assembly's store-owner deregistration hook
    . The registration lives in the assembly's collision registry, so the agent
    cannot revoke it alone: the hook is injected where the owner was registered
    and closeOnce calls it ONLY after this instance really took its store exit.
    An unconverged close keeps the registration — a holder whose stop was never
    confirmed may still write, and forgetting it would let a second owner be
    accepted for the same partition.

func (ta *TagentAgent) SetStoreOwnerSnapshot(fn func() map[string]bool)
    SetStoreOwnerSnapshot installs the store-owner introspection probe.
    Startup-injected once (like SetOrgDiagnostics); runtime read-only.

func (ta *TagentAgent) SetToolParentProjection()
    SetToolParentProjection wires the agent's compress.SessionProjection to all
    AgentToolWrapper instances in the tool list. This enables auto-inject of
    event_keys when LLM does not pass them. Must be called after NewTagentAgent
    (which creates the projection).

    /W-1: the list holds OutputLimitTool(*AgentToolWrapper) after agent.New has
    wrapped every tool, so the wiring pierces the transparent decorator chain
    (collectAgentToolWrappers) instead of asserting the bare type — otherwise
    the projection never reaches the sub-agent wrappers and auto-inject is dead.

    This is the ONLY production publish of that binding, and it runs while
    the agent is still being constructed (not yet serving). /D2 removed the
    per-ContextManager rebinding: a live invocation publishes nothing into
    shared tools and carries its own projection through the context instead.

func (ta *TagentAgent) SetTrajectoryRecorder(tr *rl.TrajectoryRecorder)
    SetTrajectoryRecorder sets the trajectory recorder for this agent. When set,
    StartLoop will automatically call SetSessionInfo on it. : do NOT also
    RegisterCloser it — closeOnce is its sole close owner, flushing AFTER the
    runner stopped; a closer registration would re-introduce double ownership
    and an early (pre-runner) close.

func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)
    StartLoop starts the persistent event loop for this agent. The loop runs
    in a dedicated goroutine and processes events until StopLoop is called.
    Returns the output channel that emits events as they are processed.
    The channel is closed exactly once, when the loop goroutine exits.
    StopLoop is TERMINAL: a second StartLoop on the same instance returns an
    error — the closed outputCh makes silent restart a production panic (V15).
    Creates a new TagentAgent for a fresh loop.

func (ta *TagentAgent) StopLoop()
    StopLoop stops the persistent event loop. : the CAS winner cancels and
    drains; every other caller — a second StopLoop, a Close arriving mid-stop
    — waits for the SAME terminal instead of skipping because the flag already
    reads false.

func (ta *TagentAgent) StoreOwnerSnapshot() map[string]bool
    StoreOwnerSnapshot returns the set of agent names currently holding a
    store-owner registration, or nil when no probe is wired. Introspection only
    — candidate rollback observability ; no execution path reads it.

func (ta *TagentAgent) SubAgents() []agent.Agent
    SubAgents implements agent.Agent interface. In the event-driven
    architecture, sub-agents are managed via AgentToolWrapper, not via the
    framework's sub-agent mechanism.

func (ta *TagentAgent) TaskManager() *task.TaskManager
    TaskManager exposes the org-level resident task registry (R2: rebuilt from
    the fact chain at cold start, shared across executor generations).

func (ta *TagentAgent) Tools() []tool.Tool
    Tools implements agent.Agent interface.

func (ta *TagentAgent) TrajectoryRecorder() *rl.TrajectoryRecorder
    TrajectoryRecorder returns the trajectory recorder if one is set, or nil.

type TagentConfig struct {
	Model       model.Model
	MemoryStore memory.MemoryStore
	// MemStoreRelease: lease release for the
	// provided MemoryStore; executed exactly once from Close. nil = the agent
	// does not own a registry lease.
	MemStoreRelease func() error
	// MemStoreBorrowed marks an executor shell that BORROWS
	// the resident shared store without a lease: it holds no close right over
	// shared state at all — the direct-Close fallback must not fire. Default
	// false = the agent owns its (isolated) store exclusively and Close flushes
	// it via the fallback tail.
	MemStoreBorrowed   bool
	SessionSvc         session.Service
	SystemPrompt       string
	SystemPromptSource prompt.Getter
	Tools              []tool.Tool
	MaxToolIterations  int
	MaxTokens          int
	CompressThreshold  float64
	SummaryModel       model.Model
	SummaryEffort      string
	Temperature        float64
	KeepRecentTasks    int
	Compress           CompressConfig

	// TaskTerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained before pruning. It bounds the resume_task
	// window for terminal subagent tasks (task-chain restorer). Non-positive
	// falls back to the task package default (2m).
	TaskTerminalTTL time.Duration

	// TaskDefaultTTL is the unified reaper's fallback absolute lifetime for
	// spawns whose model-side `ttl` is unset (async-task-lifetime 10.5). Zero →
	// task package default (10m). There is no disable path — no task is immortal.
	TaskDefaultTTL time.Duration

	// ThinkingEnabled Thinking/reasoning controls (merged into model.GenerationConfig)
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	// Name Agent identity (for agent.Agent interface)
	Name        string
	Description string

	// Meditation configures the meditation/heartbeat mechanism.
	Meditation MeditationConfig

	// Degradation 是五依赖退化状态机（T-G，可选）。非 nil 时 event_loop 上报 model 依赖、
	// ErrorTrackingStore（wireMemoryEngine 最外层）上报 memory/disk/rustviking。nil=不启用（现状）。
	Degradation *reliability.DegradationManager

	// DegradationBehaviors是依赖退化的行为响应层（警告级，
	// 每项独立、默认零值=全部关闭，仅 Degradation 非 nil 时生效）。闸不是墙：行为只是
	// 免打已确认故障的依赖，上报恢复即回正常路径。
	DegradationBehaviors DegradationBehaviors

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Oversized tool outputs go to <root>/tool-output; the tmux command working
	// directory is <root>/exec. A periodic cleaner bounds tool-output files.
	WorkspaceRoot string

	// WorkspaceCleanupInterval Workspace cleanup (periodic, tool-output dir only). Non-positive values
	// fall back to the defaults below (there is no disable switch).
	WorkspaceCleanupInterval time.Duration
	WorkspaceCleanupMaxAge   time.Duration
	WorkspaceCleanupMaxFiles int

	// BusSpillDir 是事件总线磁盘溢出目录（T-G ReliableBus）。非空时 channel 满则事件溢出
	// 落盘而非丢弃（at-least-once，常驻不丢事件），重启后未消费项可回收；空 = 纯 channel（现状）。
	BusSpillDir string
}
    TagentConfig holds configuration for creating a TagentAgent.

type ToolAgentFactory func(cfg ToolAgentFactoryConfig) (*TagentConfig, error)
    ToolAgentFactory assembles a tool agent's EXECUTION CONFIGURATION from
    the given inputs. It must NOT construct the agent itself: the org owns the
    single birth path (wireAgent assembles every real owner — store-lease slot,
    drain wiring, task-domain recovery included), and the single publish
    path (stageOrgGenerations advances one face per owner per generation). A
    factory that returned a finished *TagentAgent would be a second owner-birth
    mechanism outside both: it could never advance through the face path, so
    every publish had to rebuild the whole agent (an orphan nobody closed) and
    every pinned delegation kept reading the stale construction config (design
    D1「避免用返回 完整临时 agent 的方式隐式制造第二 owner」; contract migrated round 91 with user
    approval — evidence ).

    The returned *TagentConfig is adopted verbatim where it is meaningful:
    - Name: the factory's choice is respected (the old contract's「产物整只
    使用」promise); empty falls back to the registered id. - MemoryStore:
    the org's store borrowed for this name fills a nil — a factory that opens
    its OWN store must not also be handed the org lease. - MemStoreRelease:
    always the org's, filled by the assembly after this call returns — a factory
    neither keeps nor invents a release for it. The assembly re-invokes the
    factory for each generation it builds and hands it that generation's values,
    so a declaration derived from them moves with the config.

func GetToolAgentFactory(id string) (ToolAgentFactory, bool)
    GetToolAgentFactory returns the factory for the given ID.

type ToolAgentFactoryConfig struct {
	// ID is the tool agent identifier (e.g., "knowledge", "recall")
	ID string

	// Model is the LLM model for the tool agent (resolved from config)
	Model model.Model

	// SystemPrompt is the loaded system prompt (already resolved from PromptConfig)
	SystemPrompt string

	// Description is the tool description shown to the parent agent's LLM
	Description string

	// SubTools are the pre-built sub-tools for this agent
	SubTools []trpctool.Tool

	// MemoryStore is the tool agent's own memory store (isolated from parent).
	// The factory should use this (or create its own) for the agent's internal storage.
	// Context from the parent is delivered via AgentToolWrapper at call time, not via MemStore.
	MemoryStore memory.MemoryStore

	// ReadPartitionIDs lists PartitionIDs this agent is allowed to read in addition
	// to its own namespace. Injected from MemoryConfig.ReadNamespaces at build time.
	// Used by recall agent's sub-tools to query across agent partitions.
	ReadPartitionIDs []int

	// SkillRepo is the skill repository for knowledge agent (optional).
	SkillRepo tagenttool.SkillRepository

	// MCPToolSets 是用于工具发现的 MCP 工具来源（可选）。
	MCPToolSets []trpctool.ToolSet

	// MCPRegistry is the live MCP server registry (preferred over
	// MCPToolSets): reads reflect runtime registration and config hot-sync.
	MCPRegistry tagenttool.MCPRegistry

	// MaxToolIterations Agent parameters
	MaxToolIterations int
	MaxTokens         int
	Temperature       float64

	// ThinkingEnabled Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}
    ToolAgentFactoryConfig provides everything a factory needs to produce the
    agent's configuration declaration (it does NOT construct a TagentAgent — see
    ToolAgentFactory).

    In the new architecture, each tool agent has its own isolated MemStore.
    The parent agent's MemStore is NOT passed here — context is delivered via
    the AgentToolWrapper's event_key resolution at call time.

type UnconvergedRef struct {
	Generation int64          `json:"generation"`
	Owner      string         `json:"owner"`
	HeldFor    time.Duration  `json:"heldForNanos"`
	Refs       map[string]int `json:"refs"`
}
    UnconvergedRef names a generation that is still held when a bounded close
    gives up. It is a report, not a force-close: the resources stay held until
    their producer confirms the stop.
