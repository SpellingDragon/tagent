package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/modelutil"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ContextManager is the unified component for message building, compression
// orchestration, and framework Flow execution. It replaces Preprocessor and
// FrameworkFlowAdapter.
//
// Prototype mapping:
// - OnEvents (append inputs + call model) → BuildInvocation + RunFlow
// - Compact (clean projection) → compress.ContextCompressor in BeforeModel callback
type ContextManager struct {
	// turnEcho: the batch echo spec runEventLoop installs
	// before the model runs, ONLY when this turn's durable input facts are already
	// committed. RunFlow stamps a FRESH per-attempt plugin.EchoCredential from it
	// (unique request token per runner attempt; no whole-turn / "first envelope"
	// state — the old DurableInbound framing). nil → no pre-committed echo this turn.
	turnEcho   *echoSpec
	attemptSeq atomic.Int64
	// lastEchoCred is the credential RunFlow minted for the most recent attempt; the
	// event loop consults it after RunFlow to fail-closed when a durable turn's
	// echo was never verified at the model entry.
	lastEchoCred *plugin.EchoCredential

	// recoveryMu recovery*: cold-start rebuild outcome and
	// the one-shot model-facing notice; written once at rebuild, read-only after.
	recoveryMu     sync.Mutex
	recovery       *RecoveryResult
	recoveryNotice string

	contextCompressor *compress.ContextCompressor

	// memPlugin 是 executor 装配所需的状态面引用与**已发布执行面快照**。
	// NewContextManager 从 cfg 取快照；每次 PublishExecutor 更新为该代真实执行面——
	// 快照即「当前生效绑定」，供诊断与「原样重发同一执行面」使用。候选构造不得把它
	// 当隐式回落源：回落会把已清空的字段退回上一份，属主换代后留下旧绑定。
	memPlugin    *plugin.MemoryPlugin
	sessionSvc   session.Service
	execCfg      ContextManagerConfig
	tokenCounter compress.TokenCounter
	memStore     memory.MemoryStore

	// runner Framework integration
	runner     runner.Runner
	executorMu sync.RWMutex

	// active Per-generation execution leases. `active` is the
	// binding of the runner currently in force; every retired binding that still
	// has references stays in retiredBindings until its OWN count drops (never a
	// shared aggregate gate). Guarded by executorMu, except each binding's own
	// reference set, which is guarded by the binding. See agent/exec_lease.go.
	active          *execBinding
	retiredMu       sync.Mutex
	retiredBindings map[*execBinding]struct{}
	bindingSeq      atomic.Int64
	// retirementPoke is the  drain-forward notification armed by the
	// composition root (see SetRetirementPoke); nil = standalone agent.
	retirementPoke atomic.Pointer[func()]
	// drainedHook/drainedOnce carry 's deferred final exit: armed by an
	// owner whose bounded Close could not finish because an execution never
	// confirmed a stop, fired at most once when the last held generation is
	// reclaimed with nothing referencing it (see armFullyDrained).
	drainedHook atomic.Pointer[func()]
	drainedOnce sync.Once
	name        string
	userID      string
	sessionID   string

	// outputCh Event routing
	outputCh   chan *event.Event
	bus        *EventBus
	projection *compress.SessionProjection
	onEvent    func(evt *event.Event)

	// overflowDir persists events when the outputCh consumer stalls beyond
	// the F2 grace period (empty = drop with warning, e.g. sub-agent paths
	// with no workspace). <workspace>/tool-output/output-overflow.
	overflowDir string

	// orgReloader (agent-config-hot-reload, incremental A): optional lazy
	// check invoked at the top of the unified BeforeModel callback. Wired
	// by the tagent layer when a config path is known (WithConfigPath).
	// Must be cheap when nothing changed (single stat) and never fail the
	// LLM call (log-and-degrade inside the closure).
	//
	// D3: in production this is the NON-BLOCKING business-turn
	// trigger — it only merges/schedules a background rebuild and returns; it
	// MUST NOT run the long build/Close on the turn's thread. Ops who need the
	// result synchronously call orgReloadSync (see CheckOrgReload).
	orgReloader func()

	// orgReloadSync is the synchronous ops entry: the tagent layer arms it with
	// the same reload that runs on the background builder, so CheckOrgReload /
	// Rollback block until this request's build/reject result is settled while
	// business turns keep the non-blocking orgReloader path (no second effective
	// route — both drive the ONE coordinator/reload). nil → ops falls back to
	// orgReloader (tests that arm only a lazy stub stay synchronous).
	orgReloadSync func()

	// bundleIDFn returns the currently active evolution bundle id (empty when
	// evolution is disabled or no bundle is active). Both persistence paths
	// stamp it into FullEvent.Metadata
	// so guardrail/feedback aggregation can join events to bundle versions
	// precisely. nil-safe.
	bundleIDFn func() string

	// lastBudgetRefusal is the named fixed-overhead refusal the compressor handed
	// back for the MOST RECENT assembly of this manager (nil = no refusal). The
	// assembly records it, the final model gate consumes it — see
	// setBudgetRefusal/takeBudgetRefusal. budgetMu guards it because the gate may
	// run on a different goroutine than the one that assembled (retries).
	lastBudgetRefusal *budgetRefusal
	budgetMu          sync.Mutex

	// boardReq/boardText is the ONE live-board render made for the budget of the
	// request identified by boardReq, handed to the tail injection so the bytes
	// priced are the bytes sent (the board moves with task ages, so a second
	// render would inject what the budget never paid for). Single slot, consumed
	// on match; a non-matching request simply renders as before.
	boardReq  *model.Request
	boardText string
	boardMu   sync.Mutex

	// hotView is the owner's live hot-param pull source, kept on the manager so the
	// assembly can name the SAME input limit the compressor priced against
	// (SetHotSource installs it alongside the compressor; cfg.HotNumbersSource
	// seeds it). nil → the construction MaxTokens is the limit.
	hotView func() compress.HotNumbers

	// captureEnabled installs the per-attempt association scope of
	// D14-S2/S3 on the RunFlow ctx. false (default) → nothing is installed,
	// nothing is allocated, no cost on the hot path.
	captureEnabled bool

	// taskController, when set, is injected into the RunFlow ctx (as a
	// task.TaskSpawner) so tools can hand long-running work to the task layer, and
	// is used to render the live task board at BeforeModel. nil → tools fall
	// back to synchronous execution and no board is rendered.
	taskController task.TaskController

	// settleSinks is the owner agent's per-invocation settle routing table, wired
	// only on a sub-call's private ContextManager (Run). It lets the Origin-stamp
	// site install a countingSpawner so background tasks this turn spawns are
	// booked into the delivery-accounting barrier the M2 loop consumes. nil on the
	// resident/entry CM → no countingSpawner → behavior unchanged (S3m-a bus).
	settleSinks *settleSinkRegistry

	// triggerSource identifies what triggered the current RunFlow
	// (e.g., "user", "meditation", "async_result"). Set by runEventLoop
	// before calling RunFlow. Attached to outputCh events via
	// StateDelta["trigger_source"] for deterministic consumer dispatch.
	triggerSource string

	// turnProductive records whether the current/most-recent RunFlow turn
	// produced anything (a tool call or a non-empty final). Written and read
	// only on the loop goroutine that drives RunFlow.
	turnProductive bool

	// lastTurnOutcome is the  reduced terminal state of the most recent
	// RunFlow attempt (completed / failed / cancelled), observed from BOTH the
	// transport return AND the event stream (a Response.Error event, a mid-drain
	// ctx cancellation). The old loop trusted only the nil transport return and
	// ACKed failed/cancelled turns as success. Written and read only on the loop
	// goroutine that drives RunFlow; the persistent loop reduces it across the
	// retry budget into batchOutcome.
	lastTurnOutcome turnOutcome

	// lastBatchOutcome holds the  loop-level reduction of the retry attempts
	// for the most recent turn (completed or failed; a cancellation returns before
	// it is ever set). / freeze the completion from this value; set on the
	// loop goroutine only.
	lastBatchOutcome turnOutcome

	// currentMetadata holds arbitrary metadata from the source event of the
	// current RunFlow. Set by runEventLoop before calling RunFlow.
	// Propagated to derived events via StateDelta["meta_*"] in onEvent.
	currentMetadata map[string]string
	metadataMu      sync.RWMutex

	// partitionID is used for Snowflake EventKey generation when persisting
	// bus events directly (bypassing MemoryPlugin's OnEvent hook).
	partitionID int

	// systemPromptSource enables hot-reload of the system prompt.
	// When set, the system message is re-read from files before each LLM call.
	systemPromptSource prompt.Getter
}

// OrgHotParams SetTriggerSource sets the trigger source for the next RunFlow call.
// OrgHotParams is the hot-applicable numeric bundle (full-hot-config Phase 1,
// ): zero/negative fields keep current settings. Structural wiring
// (memStore/bus/projection/runner) is NOT in this bundle — that follows the
// shell-rebuild path.
type OrgHotParams struct {
	ThresholdPct    float64
	MaxTokens       int
	KeepRecentTasks int
	TaskTerminalTTL time.Duration
	TaskDefaultTTL  time.Duration
}

// OrgBudgetLine returns this manager's compressor effective trigger line
// (maxTokens × threshold) — valid for BOTH the resident manager and an
// invocation-private one.
// 0 when no compressor is wired.
func (cm *ContextManager) OrgBudgetLine() int {
	if cm == nil || cm.contextCompressor == nil {
		return 0
	}
	return cm.contextCompressor.BudgetLine()
}

// OrgKeepRecent returns this manager's live keepRecent value (, same
// rationale as OrgBudgetLine).
func (cm *ContextManager) OrgKeepRecent() int {
	if cm == nil || cm.contextCompressor == nil {
		return 0
	}
	return cm.contextCompressor.KeepRecentValue()
}

// CheckOrgReload runs the org-config hot-reload check and WAITS for its result
// (ops/test entry point). It drives the synchronous reload path (orgReloadSync)
// when armed, so the caller observes the build/reject outcome on return; business
// turns instead use the non-blocking lazy trigger via BeginTurn. There
// is no second route by which a configuration becomes effective — both reach the
// same reload/coordinator. Falls back to the lazy trigger when no sync variant
// is armed (tests that only set SetOrgReloader keep their synchronous behavior).
func (cm *ContextManager) CheckOrgReload() {
	if cm.orgReloadSync != nil {
		cm.orgReloadSync()
		return
	}
	if cm.orgReloader != nil {
		cm.orgReloader()
	}
}

// SetOrgReloader 装上属主 org 配置的重挂入口，使重挂能按同一身份重建参数源。
func (cm *ContextManager) SetOrgReloader(fn func()) {
	cm.orgReloader = fn
}

// SetOrgReloadSyncCheck arms the synchronous ops reload entry. The
// tagent layer wires it to the same reload the background builder runs, so ops
// blocks for its result while turns stay non-blocking.
func (cm *ContextManager) SetOrgReloadSyncCheck(fn func()) {
	cm.orgReloadSync = fn
}

// BeginTurn is the ONE place a business turn takes its organization execution binding:
// it pins the executor in force and returns the release that must run when the turn ends.
//
// - Call it after the input batch is frozen and OUTSIDE the transport-retry loop; the whole turn, every attempt, model iteration and tool round, then runs on the returned runner via RunFlowWithExecutor.
// - Sub-agent invocations do not call this: their instances, executor and delegation tree were constructed inside the generation that published them.
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func (cm *ContextManager) BeginTurn() (runner.Runner, func()) {
	lease := cm.BeginTurnLease()
	return lease.Runner(), lease.Release
}

// BeginTurnLease is BeginTurn with the reference handle exposed, so the turn can
// publish its lease into the call-chain context and every derived execution
// (nested delegation, transport retry, post-ACK background run) adds its own
// reference on the SAME generation instead of re-reading whatever is published
// later.
func (cm *ContextManager) BeginTurnLease() *ExecLease {
	if cm.orgReloader != nil {
		cm.orgReloader()
	}
	return cm.AcquireLease(LeaseTurn)
}

// AcquireLease pins the generation NEW work starts on. The pin is retried when the
// generation read here turns out to be retired ( re-entry with no initiating
// call, and every business turn, both arrive through here): reading the active
// binding and taking the reference cannot be one atomic step, because
// PublishExecutor retires the superseded generation after releasing executorMu.
//
// A retired binding with a SUCCESSOR means the publish won the race and the caller
// must re-pin the generation actually in force. A retired binding with no successor
// is the terminal Close that retired the active binding itself — there is nothing
// newer to move to, so the reference is taken as it always was and the close's
// bounded drain reports it (ErrExecUnconverged) instead of pretending the shutdown
// was clean. Retrying that case could never make progress, hence the identity test
// rather than an open-ended loop.
func (cm *ContextManager) AcquireLease(kind LeaseKind) *ExecLease {
	b := cm.activeBinding()
	for {
		if l, ok := b.tryAcquireActive(kind); ok {
			return l
		}
		next := cm.activeBinding()
		if next == b {
			if b.isClosed() {
				return &ExecLease{b: b, kind: leaseKindNoop, refused: ErrExecClosed}
			}
			return b.acquire(kind)
		}
		b = next
	}
}

// SetTriggerSource 记下本回合的触发源（user、meditation、task 等），供归因与投递门判定；它是
// 回合本地状态，必须由每个回合自己盖章，不从上一回合继承。
func (cm *ContextManager) SetTriggerSource(source string) {
	cm.triggerSource = source
}

// SetInvocationMetadata sets the metadata for the current RunFlow.
// These metadata are propagated to all events derived from the source event
// via event.StateDelta with "meta_" prefix in the onEvent callback.
func (cm *ContextManager) SetInvocationMetadata(md map[string]string) {
	cm.metadataMu.Lock()
	defer cm.metadataMu.Unlock()
	cm.currentMetadata = md
}

// GetInvocationMetadata returns the current RunFlow's metadata (thread-safe copy).
func (cm *ContextManager) GetInvocationMetadata() map[string]string {
	cm.metadataMu.RLock()
	defer cm.metadataMu.RUnlock()
	if cm.currentMetadata == nil {
		return nil
	}
	out := make(map[string]string, len(cm.currentMetadata))
	for k, v := range cm.currentMetadata {
		out[k] = v
	}
	return out
}

// ContextManagerConfig holds everything needed to create a ContextManager.
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

	// SummaryTimeoutSeconds bounds ONE real fold's synchronous summary calls
	// (O3.5: the config layer speaks seconds, the compressor speaks a Duration).
	// Non-positive leaves the compress package default in force — 0 does NOT mean
	// "no deadline".
	SummaryTimeoutSeconds int

	// CaptureEnabled turns on the optional per-attempt association scope
	// (trajectory_capture, D14-S2/S3). Default false = today's behavior.
	CaptureEnabled bool

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

// NewContextManager creates a ContextManager that wraps a framework LLMAgent
// with compress.ContextCompressor as the sole BeforeModel compression callback.
func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	cm := &ContextManager{
		tokenCounter:       cfg.TokenCounter,
		memStore:           cfg.MemStore,
		runner:             nil,
		name:               cfg.Name,
		userID:             cfg.UserID,
		sessionID:          cfg.SessionID,
		outputCh:           cfg.OutputCh,
		bus:                cfg.Bus,
		projection:         cfg.Projection,
		onEvent:            cfg.OnEvent,
		partitionID:        memory.PartitionIDFromName(cfg.Name),
		systemPromptSource: cfg.SystemPromptSource,
	}

	if cfg.Compressor != nil {
		keepRecent := cfg.Compressor.KeepRecentTasks
		copts := []compress.ContextCompressorOption{
			compress.WithCompactKeysListed(cfg.CompactKeysListed),
			compress.WithRecentFullCount(cfg.RecentFullCount),
			compress.WithCardMaxChars(cfg.CardMaxChars),
			compress.WithHotSource(cfg.HotNumbersSource),
		}
		if cfg.SummaryTimeoutSeconds > 0 {
			copts = append(copts, compress.WithSummaryTimeout(time.Duration(cfg.SummaryTimeoutSeconds)*time.Second))
		}
		cm.contextCompressor = compress.NewContextCompressor(
			cfg.Compressor,
			cfg.MemStore,
			cfg.TokenCounter,
			cfg.MaxTokens,
			cfg.ThresholdPct,
			keepRecent,
			copts...,
		)
	}

	cm.execCfg = cfg
	cm.captureEnabled = cfg.CaptureEnabled
	cm.hotView = cfg.HotNumbersSource
	cm.memPlugin = cfg.MemPlugin
	cm.sessionSvc = cfg.SessionSvc

	cm.runner = cm.buildExecutor(cfg)
	cm.active = cm.newBinding(cm.runner)

	return cm
}

// buildModelCallbacks（hotswap-fix 5.7）：回调链构造抽为 cm 方法——闭包捕获
// 同一个 cm（装配/热载/任务板/诊断全部同源）。冷启动与候选构造
// （buildExecutor）共用，保证换装后 BeforeModel 闭包仍指向常驻状态面
// （修复空投影装配事故）。
func (cm *ContextManager) buildModelCallbacks(source prompt.Getter) *model.Callbacks {
	cb := model.NewCallbacks()

	if source != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			freshPrompt, err := source.Get()
			if err != nil || freshPrompt == "" {
				return nil, nil
			}
			if len(args.Request.Messages) > 0 && args.Request.Messages[0].Role == model.RoleSystem {
				args.Request.Messages[0].Content = freshPrompt
			} else {
				args.Request.Messages = append(
					[]model.Message{model.NewSystemMessage(freshPrompt)},
					args.Request.Messages...,
				)
			}
			return nil, nil
		})
	}

	if cm.contextCompressor != nil && cm.projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			cm.assembleRequest(ctx, args)
			return nil, nil
		})
	} else if cm.bus != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			events := cm.bus.TryPull()
			for _, evt := range events {
				if evt == nil || evt.Type != tagentevent.TypeExternalInput || evt.Message == nil {
					continue
				}
				msg := *evt.Message
				if msg.Role == model.RoleSystem {
					msg.Role = model.RoleUser
				}
				args.Request.Messages = append(args.Request.Messages, msg)
				log.Infof("[InjectBusInputs] injected message during ReAct: role=%s content=%s", msg.Role, msg.Content)
			}
			return nil, nil
		})
	}

	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		cm.injectLiveTaskBoard(args)
		return nil, nil
	})

	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		log.Debugf("[BeforeLLM] messages:\n%s", formatMessages(args.Request.Messages))
		return nil, nil
	})

	return cb
}

// buildLLMAgent（hotswap-fix 5.7）：从 ContextManagerConfig 构造 fwAgent 的
// 纯函数段（依赖仅 cfg）。冷启动与候选构造共用（都经 buildExecutor），保证
// 两条路径构造的 fwAgent 行为学一致（model/tools/prompt/genConfig/并行工具开关）。
func (cm *ContextManager) buildLLMAgent(cfg ContextManagerConfig) *llmagent.LLMAgent {
	cb := cm.buildModelCallbacks(cfg.SystemPromptSource)
	maxIters := cfg.MaxToolIters
	if maxIters <= 0 {
		maxIters = DefaultMaxToolIterations
	}
	gatedModel := cfg.Model
	if gatedModel != nil {
		gatedModel = newExecutionGateModel(cfg.Model, cm)
	}
	agentOpts := []llmagent.Option{
		llmagent.WithModel(gatedModel),
		llmagent.WithModelCallbacks(cb),
		llmagent.WithMaxToolIterations(maxIters),
		llmagent.WithEnableParallelTools(true),
	}
	if cfg.SystemPrompt != "" {
		agentOpts = append(agentOpts, llmagent.WithInstruction(cfg.SystemPrompt))
	}
	if len(cfg.Tools) > 0 {
		agentOpts = append(agentOpts, llmagent.WithTools(cfg.Tools))
	}
	genConfig := model.GenerationConfig{}
	if cfg.Temperature > 0 {
		temp := cfg.Temperature
		genConfig.Temperature = &temp
	}
	if cfg.ThinkingEnabled != nil {
		genConfig.ThinkingEnabled = cfg.ThinkingEnabled
	}
	if cfg.ThinkingTokens != nil {
		genConfig.ThinkingTokens = cfg.ThinkingTokens
	}
	if cfg.ReasoningEffort != nil {
		genConfig.ReasoningEffort = cfg.ReasoningEffort
	}
	if genConfig.Temperature != nil || genConfig.ThinkingEnabled != nil ||
		genConfig.ThinkingTokens != nil || genConfig.ReasoningEffort != nil {
		agentOpts = append(agentOpts, llmagent.WithGenerationConfig(genConfig))
	}
	if cfg.ReasoningContentMode != "" {
		agentOpts = append(agentOpts, llmagent.WithReasoningContentMode(cfg.ReasoningContentMode))
	}
	return llmagent.New(cfg.Name, agentOpts...)
}

// buildRunner（R4 3.3/3.5 抽取）：fwAgent+runner 装配为纯函数段（依赖仅
// cfg）——冷启动与热重建共用同一装配路径（行为学一致）。
func buildRunner(cfg ContextManagerConfig, fwAgent *llmagent.LLMAgent) runner.Runner {
	runnerOpts := []runner.Option{}
	if cfg.MemPlugin != nil {
		runnerOpts = append(runnerOpts, runner.WithPlugins(
			plugin.NewSummaryPlugin(),
			cfg.MemPlugin,
		))
	}
	if cfg.SessionSvc != nil {
		runnerOpts = append(runnerOpts, runner.WithSessionService(cfg.SessionSvc))
	}
	return runner.NewRunner(cfg.Name, fwAgent, runnerOpts...)
}

// publishActiveLocked is the ONE linearization body of an executor switch: it records
// the published face, installs the generation running the new runner as active, and
// returns the binding the caller must retire once executorMu has been dropped.
//
// - prepared is the binding a staged generation already wired; non-nil only on the org path, where the binding had to exist and be wired before any owner made it visible. A single-owner publish passes nil and snapshots the binding from the face recorded here.
// - Re-publishing the same executor object is not a new generation: one runner object gets exactly one close, while the recorded face still advances without rewriting what this generation routes.
// 契约: docs/wiki/agent/execution-generations.md#single-linearization-body
func (cm *ContextManager) publishActiveLocked(face ContextManagerConfig, r runner.Runner, prepared *execBinding) *execBinding {
	cm.execCfg = face.isolatedCopy()
	if cm.active != nil && cm.active.run == r {
		return nil
	}
	if cm.active == nil && cm.runner == r {
		cm.active = cm.newBinding(r)
		return nil
	}
	prev := cm.active
	if prev == nil && cm.runner != nil {
		prev = cm.newBinding(cm.runner)
	}
	next := prepared
	if next == nil {
		next = cm.newBinding(r)
	}
	cm.active = next
	cm.runner = r
	return prev
}

// isolatedCopy returns a snapshot whose mutable configuration surface cannot be
// written through to the source (R06, D2「不可变执行配置与受限运行句柄」). It
// copies the struct, allocates a fresh Tools container (the element handles are
// reused by identity — real tools are runtime resources, not serialisable
// config, so they MUST NOT be blindly deep-copied), and independently allocates
// the thinking/reasoning value pointers so a deref-write on the copy cannot
// reach the live config. Runtime handles (model/store/session/plugin/bus/
// projection/callbacks) stay shared as-is. Both the getter and the publish
// entry use it, so neither a returned snapshot nor the caller's input face is
// ever an alias of the online executor configuration.
func (c ContextManagerConfig) isolatedCopy() ContextManagerConfig {
	out := c
	if c.Tools != nil {
		out.Tools = make([]trpctool.Tool, len(c.Tools))
		copy(out.Tools, c.Tools)
	}
	if c.ThinkingEnabled != nil {
		v := *c.ThinkingEnabled
		out.ThinkingEnabled = &v
	}
	if c.ThinkingTokens != nil {
		v := *c.ThinkingTokens
		out.ThinkingTokens = &v
	}
	if c.ReasoningEffort != nil {
		v := *c.ReasoningEffort
		out.ReasoningEffort = &v
	}
	return out
}

// ExecutorConfig returns the published execution-face snapshot (model/tools/
// prompt/genConfig actually in force). Read under the executor lock: a
// candidate being built elsewhere cannot be observed as "current". The returned
// face is a private copy : mutating its Tools container or deref-writing
// its value pointers must not write through to the online executor.
func (cm *ContextManager) ExecutorConfig() ContextManagerConfig {
	if cm == nil {
		return ContextManagerConfig{}
	}
	cm.executorMu.RLock()
	defer cm.executorMu.RUnlock()
	return cm.execCfg.isolatedCopy()
}

// SubagentWrapper resolves a delegation target **against the effective executor
// face** — the very
// same version source that serves ordinary delegation, so there is no second
// routing truth. A name a later generation removed is gone from the face and the
// caller gets nil, which is how an explicit relaunch refuses to revive a retired
// binding instead of silently running on the generation that spawned it.
func (cm *ContextManager) SubagentWrapper(name string) *AgentToolWrapper {
	if cm == nil || name == "" {
		return nil
	}
	cm.executorMu.RLock()
	defer cm.executorMu.RUnlock()
	return subagentWrapperIn(cm.execCfg.Tools, name)
}

// subagentWrapperIn is the ONE delegation-target scan: the effective face and a
// pinned generation's face are both a tool list, and both go through here. A
// second lookup (a name→wrapper registry, or a closure-captured wrapper) would be
// a routing truth that outlives the version it came from — is exactly
// the failure that produces.
func subagentWrapperIn(tools []trpctool.Tool, name string) *AgentToolWrapper {
	if name == "" {
		return nil
	}
	for _, tl := range tools {
		if w := unwrapAgentToolWrapper(tl); w != nil && w.DeclaredAgentName() == name {
			return w
		}
	}
	return nil
}

// unwrapAgentToolWrapper peels transparent pass-through wrappers (notably
// OutputLimitTool, which agent.New applies to every tool) to reach the
// delegation AgentToolWrapper underneath. It returns nil if no AgentToolWrapper
// lies in the chain, so non-delegation tools simply don't match. The published
// face stays the SINGLE routing truth: the peeled wrapper is the very
// instance ordinary delegation runs on this generation, because the runner is
// built from these same face tools. A parallel name→wrapper registry would be a
// second source with a lazy cache — explicitly rejected here.
func unwrapAgentToolWrapper(t trpctool.Tool) *AgentToolWrapper {
	for t != nil {
		if w, ok := t.(*AgentToolWrapper); ok {
			return w
		}
		uw, ok := t.(interface{ Unwrap() trpctool.Tool })
		if !ok {
			return nil
		}
		t = uw.Unwrap()
	}
	return nil
}

// buildExecutor is the single executor assembly path, used by both cold start and
// hot candidate construction. Inputs split into the execution face (taken from
// exec) and the state face (taken from this cm); the no-fallback rule and the
// constraints on writing call-scoped data into already-published delegation
// wrappers during construction are specified in the document below.
// 契约: docs/wiki/agent/execution-generations.md#published-wrapper-immutable
func (cm *ContextManager) buildExecutor(exec ContextManagerConfig) runner.Runner {
	exec.MemPlugin = cm.memPlugin
	exec.SessionSvc = cm.sessionSvc
	return buildRunner(exec, cm.buildLLMAgent(exec))
}

// NewExecutorCandidate constructs the next generation's executor WITHOUT
// touching the live one: no swap, no
// snapshot update, no state-face change — the caller may still abandon it.
// Construction is side-effect free on the resident cm beyond reading it; the
// only mutation is on the candidate's own tool wrappers.
func (cm *ContextManager) NewExecutorCandidate(exec ContextManagerConfig) runner.Runner {
	if cm == nil {
		return nil
	}
	return cm.buildExecutor(exec)
}

// PublishExecutor is the ONE linearization point of an organization version
// switch: install the candidate, record it as the published execution face,
// then retire the superseded runner. Drain-free at turn granularity — an
// in-flight turn keeps the old runner reference and finishes on it; the next
// turn picks up the new one.
// Returns the runner now in force.
func (cm *ContextManager) PublishExecutor(candidate runner.Runner, exec ContextManagerConfig) runner.Runner {
	if cm == nil || candidate == nil {
		return cm.currentRunner()
	}
	cm.executorMu.Lock()
	prev := cm.publishActiveLocked(exec, candidate, nil)
	cm.executorMu.Unlock()
	cm.retireBinding(prev)
	return cm.currentRunner()
}

// StagedGeneration is a prepared-but-not-installed execution generation (3.2
// trunk). Staging exists so a MULTI-OWNER publish can wire the generation-level
// declaration holds (and stamp each face's wrappers with their declared target
// bindings) BEFORE any owner's generation becomes visible — the same
// build-then-publish discipline applies uniformly to every
// routable owner. A staged generation nobody activates is not reachable by any
// execution path; Discard closes its candidate and releases the holds it
// recorded, so a failed candidate leaves nothing behind.
type StagedGeneration struct {
	cm        *ContextManager
	candidate runner.Runner
	face      ContextManagerConfig
	// runCfg is this generation's assembled TagentConfig — the per-generation
	// execution description a DECLARED invocation assembles its per-call context
	// manager from (3.2 trunk: 「不按陈旧 ta.config 先造后补」). It is the same
	// shape the owner's construction config had, so every downstream derivation
	// (fresh per-invocation compressor, overflow dir, …) behaves identically.
	runCfg    *TagentConfig
	binding   *execBinding
	discarded bool
}

// StageExecutor opens the next generation record for `cand` WITHOUT installing
// it. The binding snapshots the given face (isolated), so the wiring pass can
// stamp the face's wrappers against exactly this generation.
func (cm *ContextManager) StageExecutor(cand runner.Runner, face ContextManagerConfig, runCfg *TagentConfig) *StagedGeneration {
	if cm == nil || cand == nil {
		return nil
	}
	cm.executorMu.Lock()
	defer cm.executorMu.Unlock()
	face = face.isolatedCopy()
	b := newExecBinding(cm, cm.bindingSeq.Add(1), cand, face)
	b.runCfg = runCfg
	return &StagedGeneration{
		cm:        cm,
		candidate: cand,
		face:      face,
		runCfg:    runCfg,
		binding:   b,
	}
}

// ActivateExecutor installs a staged generation — the same linearization
// PublishExecutor performs (face first, then the binding, then retire the
// superseded one; drain-free at turn granularity). Splitting it from staging is
// what lets the composition root wire every owner of one publish before ANY of
// them goes live.
func (cm *ContextManager) ActivateExecutor(s *StagedGeneration) runner.Runner {
	if cm == nil || s == nil || s.cm != cm || s.discarded {
		return cm.currentRunner()
	}
	cm.executorMu.Lock()
	prev := cm.publishActiveLocked(s.face, s.candidate, s.binding)
	cm.executorMu.Unlock()
	cm.retireBinding(prev)
	return cm.currentRunner()
}

// Discard abandons a staged generation that was never activated: release the
// declaration holds it recorded during wiring and close the never-installed
// candidate. Idempotent.
func (s *StagedGeneration) Discard() {
	if s == nil || s.discarded {
		return
	}
	s.discarded = true
	for _, child := range s.binding.heldBindings() {
		child.dropDeclaredHold()
	}
	if s.candidate != nil {
		_ = s.candidate.Close()
	}
}

// retiredLeakAfter gates the leak alarm: past this age a retired generation that
// still cannot be closed is almost certainly a leak (see noteUnconverged).
const retiredLeakAfter = 10 * time.Minute

// activeBinding returns the generation currently in force, lazily wrapping the
// runner field so a hand-built ContextManager (tests, shells that only set
// `runner`) still has one coherent generation to be referenced through.
func (cm *ContextManager) activeBinding() *execBinding {
	cm.executorMu.RLock()
	b := cm.active
	cm.executorMu.RUnlock()
	if b != nil {
		return b
	}
	cm.executorMu.Lock()
	defer cm.executorMu.Unlock()
	if cm.active == nil {
		cm.active = cm.newBinding(cm.runner)
	}
	return cm.active
}

// newBinding opens a generation record (caller holds executorMu when it wants a
// deterministic id order; the lazy path above does). The generation snapshots the
// face currently in force — PublishExecutor assigns the new face BEFORE installing
// the binding, so the snapshot is the face THIS runner was built from.
func (cm *ContextManager) newBinding(r runner.Runner) *execBinding {
	return newExecBinding(cm, cm.bindingSeq.Add(1), r, cm.execCfg)
}

// retireBinding marks `b` superseded and keeps it on the unconverged list until
// its own reference count drops. Close happens in ExecLease.Release, per
// generation — an unrelated in-flight turn cannot hold it open.
func (cm *ContextManager) retireBinding(b *execBinding) {
	if cm == nil || b == nil {
		return
	}
	b.retire()
	cm.retiredMu.Lock()
	if cm.retiredBindings == nil {
		cm.retiredBindings = map[*execBinding]struct{}{}
	}
	cm.retiredBindings[b] = struct{}{}
	cm.retiredMu.Unlock()
	b.release(leaseKindNoop)
}

// forgetBinding drops a closed generation from the unconverged list so the
// bookkeeping stays bounded across endless hot swaps (「资源量按当前路由和真实
// 活引用计，不按历史发布次数计」).
func (cm *ContextManager) forgetBinding(b *execBinding) {
	if cm == nil || b == nil {
		return
	}
	cm.retiredMu.Lock()
	delete(cm.retiredBindings, b)
	cm.retiredMu.Unlock()
	cm.fireFullyDrainedIfQuiet()
}

// armFullyDrained registers a one-shot continuation run when the last held
// generation has been reclaimed and nothing references any generation anymore —
// 's「同一尾部保有责任并等真实停止后继续」. The notification comes from the
// reclaim path itself (a producer finally stopping), so it adds no timer, no
// polling loop and no second lifecycle framework. If the manager is ALREADY
// quiet, the continuation runs before this call returns: arming can never strand
// a tail, which is the failure mode the deferred exit must not become.
func (cm *ContextManager) armFullyDrained(fn func()) {
	if cm == nil || fn == nil {
		return
	}
	cm.drainedHook.Store(&fn)
	cm.fireFullyDrainedIfQuiet()
}

// fireFullyDrainedIfQuiet invokes the armed continuation at most once, and only
// when no retired generation is still held AND no reference is outstanding.
func (cm *ContextManager) fireFullyDrainedIfQuiet() {
	if cm == nil || cm.drainedHook.Load() == nil {
		return
	}
	cm.retiredMu.Lock()
	held := len(cm.retiredBindings)
	cm.retiredMu.Unlock()
	if held > 0 || cm.totalRefs() != 0 {
		return
	}
	hook := cm.drainedHook.Load()
	if hook == nil {
		return
	}
	cm.drainedOnce.Do(func() { (*hook)() })
}

// pokeRetirementDrain carries a pending retirement forward after the event that
// unblocked it (: 「释放使用权/任务收尾经原生命周期轻量通知继续退役，不必须再来
// 一个业务 turn」). It only invokes the composition root's lazy check — the same
// single-flight, non-blocking path a business turn rides — so it adds no timer and
// never runs a drain inline under the caller's locks. Nil for a standalone agent.
func (cm *ContextManager) pokeRetirementDrain() {
	if cm == nil {
		return
	}
	if poke := cm.retirementPoke.Load(); poke != nil && *poke != nil {
		(*poke)()
	}
}

// SetRetirementPoke arms the composition root's lazy drain check on this
// manager. It is called once per owner at assembly time, is nil for a standalone
// agent (no org, nothing to retire), and is only ever invoked from a generation's
// own release path — it schedules work and must not run the drain inline.
func (cm *ContextManager) SetRetirementPoke(fn func()) {
	if cm == nil || fn == nil {
		return
	}
	cm.retirementPoke.Store(&fn)
}

// ExecutorRefs 是执行器引用面的诊断快照（D9：退役引用与未收敛 owner 可见；
// ：业务 turn／子调用／后台执行分报，不是一个模糊总数）。
// 只读：无任何执行路径据它分支（回收时机由各代自己的引用数决定），因此它不会
// 成为第二真源。
type ExecutorRefs struct {
	InFlightTurns   int64            `json:"inFlightTurns"`
	SubCalls        int              `json:"subCalls"`
	BackgroundRuns  int              `json:"backgroundRuns"`
	PendingRetirees int              `json:"pendingRetirees"`
	OldestPending   time.Duration    `json:"oldestPending"`
	LeakThreshold   time.Duration    `json:"leakThreshold"`
	Generations     []GenerationRefs `json:"generations"`
}

// ExecutorRefs 交出执行器引用面的诊断快照（退役引用、未收敛属主等），供运维判断回收是否收敛；
// 管理器为 nil 时返回零值快照。
func (cm *ContextManager) ExecutorRefs() ExecutorRefs {
	if cm == nil {
		return ExecutorRefs{}
	}
	refs := ExecutorRefs{LeakThreshold: retiredLeakAfter}
	seen := map[*execBinding]bool{}
	var gens []GenerationRefs

	cm.executorMu.RLock()
	if active := cm.active; active != nil {
		seen[active] = true
		gens = append(gens, active.snapshot())
	}
	cm.executorMu.RUnlock()

	cm.retiredMu.Lock()
	for b := range cm.retiredBindings {
		if seen[b] {
			continue
		}
		gens = append(gens, b.snapshot())
	}
	refs.PendingRetirees = len(cm.retiredBindings)
	for b := range cm.retiredBindings {
		if age := time.Since(b.retiredAt); age > refs.OldestPending {
			refs.OldestPending = age
		}
	}
	cm.retiredMu.Unlock()

	for _, g := range gens {
		refs.InFlightTurns += int64(g.Refs[LeaseTurn.String()])
		refs.SubCalls += g.Refs[LeaseSubCall.String()]
		refs.BackgroundRuns += g.Refs[LeaseBackground.String()]
	}
	refs.Generations = gens
	return refs
}

// UnconvergedRefs lists the retired generations still held by a live reference
// (: a bounded Close must be able to say WHO is still running instead of
// force-closing to satisfy a count). The active generation is never listed.
func (cm *ContextManager) UnconvergedRefs() []UnconvergedRef {
	if cm == nil {
		return nil
	}
	cm.retiredMu.Lock()
	defer cm.retiredMu.Unlock()
	var out []UnconvergedRef
	for b := range cm.retiredBindings {
		g := b.snapshot()
		if g.Closed || g.Total == 0 {
			continue
		}
		out = append(out, UnconvergedRef{
			Generation: g.Generation,
			Owner:      g.Owner,
			HeldFor:    time.Since(b.retiredAt),
			Refs:       g.Refs,
		})
	}
	return out
}

// noteUnconverged keeps the historical leak alarm on the audit surface: a
// retiree still referenced past retiredLeakAfter is reported, never force-closed.
func (cm *ContextManager) noteUnconverged() {
	for _, u := range cm.UnconvergedRefs() {
		if u.HeldFor > retiredLeakAfter {
			log.Warnf("[ContextManager] generation %d of owner %q retired >%s ago but still holds %v references — investigate the execution that never stopped",
				u.Generation, u.Owner, retiredLeakAfter, u.Refs)
			return
		}
	}
}

// currentRunner（R4 3.3）：RunFlow per-turn 取引用（RLock 即放——不持锁跑
// turn，否则换入会被长 turn 阻塞）。in-flight turn 用旧 runner 跑完。
func (cm *ContextManager) currentRunner() runner.Runner {
	cm.executorMu.RLock()
	defer cm.executorMu.RUnlock()
	return cm.runner
}

// BuildInvocation merges a batch of AgentEvents into a single model.Message.
func (cm *ContextManager) BuildInvocation(batch []*AgentEvent) model.Message {
	var contents []string
	var parts []model.ContentPart
	for _, evt := range batch {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput || evt.Message == nil {
			continue
		}
		if evt.Message.Content != "" {
			contents = append(contents, evt.Message.Content)
		}
		parts = append(parts, evt.Message.ContentParts...)
	}
	if len(contents) == 0 && len(parts) == 0 {
		return model.NewUserMessage("")
	}
	msg := model.Message{Role: model.RoleUser, ContentParts: parts}
	switch {
	case len(contents) == 1:
		msg.Content = contents[0]
	case len(contents) > 1:
		msg.Content = strings.Join(contents, "\n\n---\n\n")
	}
	return msg
}

// assembleRequest builds the final message list sent to the model:
// [system] + render(projection) (+ live task board, injected by a later callback).
//
// - The projection is the SOLE assembly source; nothing is read back from the framework message tail.
// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
func (cm *ContextManager) assembleRequest(ctx context.Context, args *model.BeforeModelArgs) {

	systemMsg, _ := compress.SplitSystemMessage(args.Request.Messages)
	var systemText string
	if systemMsg != nil {
		systemText = systemMsg.Content
	}

	budget := compress.RequestBudgetContext{
		SystemText:  systemText,
		NoticesText: cm.turnOverheadText(args),
		Tools:       modelutil.NewRequestSnapshot(nil, args.Request.Tools).Tools,
	}

	refs := cm.projection.GetAll()
	result := cm.contextCompressor.Compress(ctx, refs, budget)
	cm.setBudgetRefusal(result)
	cm.projection.Replace(result.RetainedRefs)
	if result.Compressed {
		if notice := cm.emitCompactionEvent(result.RetainedRefs); notice != nil {
			result.Notices = append(result.Notices, *notice)
			result.Messages = append(result.Messages, *notice)
		}
	}

	rebuilt := make([]model.Message, 0, len(result.Messages)+1)
	if systemMsg != nil {
		rebuilt = append(rebuilt, *systemMsg)
	}
	rebuilt = append(rebuilt, result.Messages...)

	if len(rebuilt) <= 1 {
		log.Errorf("[assemble-health] request has only %d message(s) (system-only); "+
			"projection len=%d — injecting degradation notice (hotswap empty-projection guard)",
			len(rebuilt), cm.projection.Len())
		rebuilt = append(rebuilt, model.NewUserMessage(
			"[context-guard] 会话状态异常（上下文为空）。请向用户如实说明当前对话上下文不可用，请其重发上一条消息。"))
	}

	args.Request.Messages = rebuilt
}

// budgetRefusal is one named fixed-overhead refusal the compressor returned for the
// most recent assembly. reason is the compressor's own machine-checkable name, fixed
// is res.FixedOverhead, and limit is the input limit it priced against.
type budgetRefusal struct {
	reason string
	fixed  int
	limit  int
}

// setBudgetRefusal records (or clears) the refusal for the request that was just
// assembled. It is called on EVERY assembly, so a refusal can never outlive the
// round that produced it: the next assembly either re-prices and overwrites or
// explicitly clears.
func (cm *ContextManager) setBudgetRefusal(res compress.CompressResult) {
	cm.budgetMu.Lock()
	defer cm.budgetMu.Unlock()
	if !res.BudgetExceeded {
		cm.lastBudgetRefusal = nil
		return
	}
	limit, known := cm.liveInputLimit()
	if !known {
		limit = cm.contextCompressor.BudgetLine()
	}
	cm.lastBudgetRefusal = &budgetRefusal{reason: res.BudgetReason, fixed: res.FixedOverhead, limit: limit}
	log.Warnf("[ContextManager:%s] %s: fixed overhead %d tokens >= input limit %d — the send must be refused, timeline kept",
		cm.name, res.BudgetReason, res.FixedOverhead, limit)
}

// takeBudgetRefusal returns the refusal recorded for the most recent assembly, if
// the gate has not taken it yet. Taking is the gate's decision point: the record
// stays until taken so both outbound paths (channel and iterator) see the same
// verdict, and the one-shot recovery notice is never spent by a refused send.
func (cm *ContextManager) takeBudgetRefusal() (budgetRefusal, bool) {
	cm.budgetMu.Lock()
	defer cm.budgetMu.Unlock()
	if cm.lastBudgetRefusal == nil {
		return budgetRefusal{}, false
	}
	r := *cm.lastBudgetRefusal
	cm.lastBudgetRefusal = nil
	return r, true
}

// liveInputLimit reports the input limit the compressor prices against, read from
// the SAME hot source the compressor resolves its numbers from (so the number the
// gate names is the number the compressor used, not a mirrored guess). The
// construction value is the fallback when no source is installed.
//
// CALLER HOLDS budgetMu (setBudgetRefusal); the write side of hotView takes the
// same lock.
func (cm *ContextManager) liveInputLimit() (int, bool) {
	if cm.hotView != nil {
		if n := cm.hotView(); n.MaxTokens > 0 {
			return n.MaxTokens, true
		}
	}
	if cm.execCfg.MaxTokens > 0 {
		return cm.execCfg.MaxTokens, true
	}
	return 0, false
}

// SetHotSource installs the owner's live hot-param source on BOTH sides at once:
// the compressor (per-boundary numbers) and the manager (the limit named in a
// refusal). The resident ContextManager is built before the agent's own hot view
// exists, so this is the wiring-time seam; an existing compressor is required.
func (cm *ContextManager) SetHotSource(src func() compress.HotNumbers) {
	if src == nil {
		return
	}
	cm.budgetMu.Lock()
	cm.hotView = src
	cm.budgetMu.Unlock()
	if cm.contextCompressor != nil {
		cm.contextCompressor.SetHotSource(src)
	}
}

// turnOverheadText renders the text this turn injects OUTSIDE the frozen history —
// the live task board and the pending recovery notice — so the fixed overhead is
// priced with what will actually be sent. Both are PEEKED, never consumed: the
// board render is handed to the tail injection (its bytes move with task ages, and
// a second render would inject what the budget never priced), and the notice stays
// pending until a send actually goes out. Missing parts contribute nothing — the
// budget never invents text for surfaces this turn does not carry.
func (cm *ContextManager) turnOverheadText(args *model.BeforeModelArgs) string {
	var parts []string
	if cm.taskController != nil {
		board := task.RenderBoard(cm.taskController.List(), cm.taskController.DefaultTTL())
		cm.boardMu.Lock()
		cm.boardReq, cm.boardText = args.Request, board
		cm.boardMu.Unlock()
		if board != "" {
			parts = append(parts, board)
		}
	}
	if notice := cm.peekRecoveryNotice(); notice != "" {
		parts = append(parts, notice)
	}
	return strings.Join(parts, "\n\n")
}

// takeTurnBoard returns the board render the assembly made for THIS request, if
// there is one, and clears the handoff so it can be used exactly once. No match
// (no assembly ran for this request, or a different request took it) → the caller
// renders as it always did.
func (cm *ContextManager) takeTurnBoard(args *model.BeforeModelArgs) (string, bool) {
	cm.boardMu.Lock()
	defer cm.boardMu.Unlock()
	if cm.boardReq == nil || args == nil || cm.boardReq != args.Request {
		return "", false
	}
	text := cm.boardText
	cm.boardReq, cm.boardText = nil, ""
	return text, true
}

// emitCompactionEvent 在真折叠后把折叠产物作为一等 compaction 事件落到事实链——
// context_compress_summary 正 key 事件，Content/EventSummary 即综述正文（可召回正文就是叙事本身），
// Metadata[compaction_payload] 载重建载荷（综述 ref ＋ 有序 retained 列表：正 key 只存 key、
// tool_chain 合成 ref 全身份逐字节 ＋ fullBoundary）。
// 滚动 supersede：写前查 prior（限定 compaction=v1 代际标记；无标记的既有固化数据不删不选），
// 写后 DeleteEvent(prior)。事件自身 Timestamp 取写入时刻（不是综述的 minTs）。
// 仅真折叠发射（未超预算的轮次 BuildCompactionPayload 返回 ok=false）。
// StoreEvent 失败以 ERROR 留痕并返回通知（不阻断当轮装配）；supersede 失败仅 ERROR（下轮再补）。
// 返回 nil 表示成功或跳过。
func (cm *ContextManager) emitCompactionEvent(retained []memory.EventReference) *model.Message {
	if cm.memStore == nil || cm.contextCompressor == nil {
		return nil
	}
	payload, ok := compress.BuildCompactionPayload(retained, cm.contextCompressor.FullBoundary())
	if !ok {
		return nil
	}
	raw, err := payload.MarshalPayload()
	if err != nil {
		log.Errorf("[emitCompactionEvent] payload marshal failed: %v", err)
		return nil
	}
	priorKey := cm.latestCompactionKey()
	eventKey := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	md := map[string]string{
		compress.CompactionMetaKey:        compress.CompactionGenV1,
		compress.CompactionPayloadMetaKey: raw,
		tagentevent.MetaKeyAgentName:      cm.name,
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    tagentevent.TypeContextCompressSummary,
		EventSummary: payload.SummaryRef.EventSummary,
		Timestamp:    time.Now().UnixMilli(),
		Content:      payload.SummaryRef.EventSummary,
		Metadata:     md,
	}
	if err := cm.memStore.StoreEvent(eventKey, fullEvent); err != nil {
		log.Errorf("[emitCompactionEvent] StoreEvent failed key=%d: %v", eventKey, err)
		return &model.Message{
			Role:    model.RoleUser,
			Content: fmt.Sprintf("[compaction_event_write_failed] compaction 事件落库失败 key=%d: %v（本轮降级：重启重建将缺最新折叠）", eventKey, err),
		}
	}
	if priorKey > 0 {
		if err := cm.memStore.DeleteEvent(priorKey); err != nil {
			log.Errorf("[emitCompactionEvent] supersede DeleteEvent(prior=%d) failed: %v", priorKey, err)
		}
	}
	log.Infof("[emitCompactionEvent] compaction event persisted key=%d (superseded prior=%d) boundary=%d retained=%d",
		eventKey, priorKey, payload.FullBoundary, len(payload.Retained))
	return nil
}

// latestCompactionKey 返回带代际标记的最新 compaction 事件键（0 表示没有）。QueryEvents 无法按
// Metadata 过滤，故此处走一小段 timestamp_desc 窗口并用 GetEvent 校验代际标记——
// 无标记的既有固化数据（同类型、TTL 永久）永不被选中，因而也不会被 supersede/删除。
func (cm *ContextManager) latestCompactionKey() int64 {
	if cm.memStore == nil {
		return 0
	}
	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{cm.partitionID},
		EventTypes:   []string{tagentevent.TypeContextCompressSummary},
		OrderBy:      "timestamp_desc",
		Limit:        5,
	})
	if err != nil {
		return 0
	}
	for _, r := range refs {
		evt, err := cm.memStore.GetEvent(r.EventKey)
		if err != nil || evt == nil {
			continue
		}
		if evt.Metadata[compress.CompactionMetaKey] == compress.CompactionGenV1 &&
			evt.Metadata[tagentevent.MetaKeyAgentName] == cm.name {
			return r.EventKey
		}
	}
	return 0
}

// persistTaskRecord：记录-only 持久化——只写
// 事实链，不发 bus（不唤醒）、不进投影（task_spawned/inline settle 记录均非投影
// ref：看板由 registry 每轮渲染；inline 结果已作为工具结果在投影内）。best-effort。
func (cm *ContextManager) persistTaskRecord(fullEvent memory.FullEvent) {
	if cm == nil || cm.memStore == nil {
		return
	}
	if fullEvent.EventKey == 0 {
		fullEvent.EventKey = memory.NewSnowflakeEventKey(cm.partitionID, 0)
	}
	fullEvent.PartitionID = cm.partitionID
	if err := cm.memStore.StoreEvent(fullEvent.EventKey, fullEvent); err != nil {
		log.Errorf("[persistTaskRecord] StoreEvent failed key=%d type=%s: %v", fullEvent.EventKey, fullEvent.EventType, err)
	}
}

// EmitTaskSpawnedRecord builds and persists the fact-chain task_spawned record
// for a freshly registered task (OnSpawn hook). Registry-only record: never a
// projection ref, never bus-published.
func (cm *ContextManager) EmitTaskSpawnedRecord(tk *task.Task) {
	if cm == nil || tk == nil || tk.Spec.Declarative == nil {
		return
	}
	decl := *tk.Spec.Declarative
	if decl.StartedAtMilli == 0 {
		decl.StartedAtMilli = tk.StartedAt.UnixMilli()
	}
	if len(decl.Origin) == 0 && len(tk.Spec.Origin) > 0 {
		cp := make(map[string]string, len(tk.Spec.Origin))
		for k, v := range tk.Spec.Origin {
			cp[k] = v
		}
		decl.Origin = cp
	}
	if decl.Lifetime == "" {
		decl.Lifetime = task.LifetimeOf(tk.Spec)
	}
	raw, err := json.Marshal(decl)
	if err != nil {
		log.Errorf("[EmitTaskSpawnedRecord] marshal declarative failed: %v", err)
		return
	}
	md := map[string]string{
		tagentevent.MetaKeyAgentName: cm.name,
		"task_id":                    tk.ID,
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	cm.persistTaskRecord(memory.FullEvent{
		EventType:    tagentevent.TypeTaskSpawned,
		EventSummary: fmt.Sprintf("任务创建: %s", truncateForLog(tk.Spec.Desc, 80)),
		Content:      string(raw),
		Timestamp:    tk.StartedAt.UnixMilli(),
		Metadata:     md,
	})
}

// EmitTaskCancelledRecord：Cancel 终态的事实链记录
// （registry-only；形态与 inline settle 同款）。不写则重启回放以 suspect 复活
// （看板幽灵 + subagent 同 Key dedup 永久锁死）。
func (cm *ContextManager) EmitTaskCancelledRecord(tk *task.Task) {
	if cm == nil || tk == nil {
		return
	}
	md := map[string]string{
		tagentevent.MetaKeyAgentName:        cm.name,
		"task_id":                           tk.ID,
		"settle_status":                     "cancelled",
		tagentevent.MetaKeyTaskInlineRecord: "true",
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	cm.persistTaskRecord(memory.FullEvent{
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: fmt.Sprintf("[task cancelled] %s (id=%s)", truncateForLog(tk.Spec.Desc, 60), task.ShortID(tk.ID)),
		Content:      fmt.Sprintf("[task cancelled] %s (id=%s) cancelled", tk.Spec.Desc, tk.ID),
		Timestamp:    time.Now().UnixMilli(),
		Metadata:     md,
	})
}

// EmitTaskInlineSettleRecord builds and persists the registry-only settle
// record for an INLINE settle (OnInlineSettle hook): minimal terminal note
// (status+task_id; the result itself already returned in-turn as the tool
// result). Flagged task_inline_record — projection rebuild/replay skip it
// (sixth-round 🔴3: without a record, inline settles become replay ghosts).
func (cm *ContextManager) EmitTaskInlineSettleRecord(tk *task.Task, sig task.SettleSignal) {
	if cm == nil || tk == nil {
		return
	}
	status := string(sig.Kind)
	if tk.Status() != "" {
		status = string(tk.Status())
	}
	if sig.Kind == task.SettleStable {
		status = "alive-detached"
	}
	md := map[string]string{
		tagentevent.MetaKeyAgentName:        cm.name,
		"task_id":                           tk.ID,
		"settle_status":                     status,
		tagentevent.MetaKeyTaskInlineRecord: "true",
	}
	if cm.sessionID != "" {
		md[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	cm.persistTaskRecord(memory.FullEvent{
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: fmt.Sprintf("[task settled inline] %s (id=%s) %s", truncateForLog(tk.Spec.Desc, 60), task.ShortID(tk.ID), status),
		Content:      fmt.Sprintf("[task settled inline] %s (id=%s) %s", tk.Spec.Desc, tk.ID, status),
		Timestamp:    time.Now().UnixMilli(),
		Metadata:     md,
	})
}

// buildBusFact builds the CANONICAL FullEvent for one bus event: the same
// role-normalization, event-type inference, summary, fresh Snowflake key and
// attribution stamps (agent_name / trigger_source / rollout_id / bundle_id /
// task settle keys) persistBusEvent would otherwise derive inline. Both the
// write-before prepare barrier (which freezes this bytes as prepared_fact) and
// the volatile/claim-less commit path call it, so a durable fact's canonical
// form is decided ONCE and never re-derived on replay.
func (cm *ContextManager) buildBusFact(evt *AgentEvent) memory.FullEvent {
	if evt == nil || evt.Message == nil {
		return memory.FullEvent{}
	}
	msg := *evt.Message
	if msg.Role == model.RoleSystem {
		msg.Role = model.RoleUser
	}
	eventKey := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	eventType := tagentevent.ExtractEventType(msg)
	eventSummary := tagentevent.GenerateEventSummary(msg, eventType, tagentevent.DefaultOptionsForLLMContext())
	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    eventType,
		EventSummary: eventSummary,
		Timestamp:    evt.Timestamp.UnixMilli(),
		Content:      msg.Content,
		ContentParts: msg.ContentParts,
		ToolCalls:    msg.ToolCalls,
		ToolID:       msg.ToolID,
	}
	fullEvent.Metadata = map[string]string{
		tagentevent.MetaKeyAgentName: cm.name,
	}
	if cm.triggerSource != "" {
		fullEvent.Metadata[tagentevent.MetaKeyTriggerSource] = cm.triggerSource
	}
	if evt.Source == SourceTask {
		for _, k := range []string{"task_id", "settle_status", tagentevent.MetaKeyTaskInlineRecord} {
			if v, ok := evt.Metadata[k]; ok {
				fullEvent.Metadata[k] = fmt.Sprint(v)
			}
		}
		if v, ok := evt.Metadata[tagentevent.MetaKeyTriggerSource].(string); ok && v != "" {
			fullEvent.Metadata[tagentevent.MetaKeySettleTriggerSource] = v
		}
	}
	if cm.sessionID != "" {
		fullEvent.Metadata[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	if cm.bundleIDFn != nil {
		if bid := cm.bundleIDFn(); bid != "" {
			fullEvent.Metadata[tagentevent.MetaKeyBundleID] = bid
		}
	}
	if evt.claim != nil {
		fullEvent.Metadata[tagentevent.MetaKeyInboxRequestID] = evt.claim.RequestID
		fullEvent.Metadata[tagentevent.MetaKeyInboxSlot] = fmt.Sprintf("%d", evt.claim.Slot)
		if evt.ID != "" {
			fullEvent.Metadata[tagentevent.MetaKeySourceEventID] = evt.ID
		}
	}
	if snap, err := tagentevent.EncodeSourceSnapshot(evt.Source, evt.Metadata); err != nil {
		log.Warnf("[buildBusFact] source snapshot encode failed src=%s: %v", evt.Source, err)
	} else if snap != "" {
		fullEvent.Metadata[tagentevent.MetaKeySourceSnapshot] = snap
	}
	return fullEvent
}

// isCompletePreparedFact checks the one invariant that unambiguously separates a
// genuinely-prepared canonical fact from corrupt/truncated material (, design
// 决策2「完整准备校验」): a fixed non-zero Snowflake identity key. buildBusFact
// always stamps a real key and never 0, so a decoded fact with EventKey == 0 is
// not a real prepared fact and persistBusEvent gates the store rather than minting
// a fresh key. Summary/attribution are intentionally NOT re-checked at decode: they
// are guaranteed at construction, and re-checking them here would risk false-gating
// legitimate durable facts (e.g. an unnamed ContextManager in fixtures).
func isCompletePreparedFact(f *memory.FullEvent) bool {
	return f != nil && f.EventKey != 0
}

// persistBusEvent persists an EventBus event to MemoryStore and appends it to the
// compress.SessionProjection at the same point, so everything visible to the LLM is
// also tracked in the projection.
//
// - Returns true when the fact is stored or was already stored (replay dedup); false only when StoreEvent failed and the projection append was gated, which the submit gate maps to a transient submit.
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func (cm *ContextManager) persistBusEvent(evt *AgentEvent) bool {
	ok, _ := cm.persistBusEventCommitted(evt)
	return ok
}

// persistBusEventCommitted is the classified form of the commit step. The
// second result reports a DETERMINISTIC conflict — the typed same-key
// different-content collision or a forgotten tomb — which the commit protocol
// must ISOLATE rather than retry: a frozen key that collides with different
// content (cross-process snowflake collision included) conflicts on EVERY
// replay, so retrying it forever is a livelock. 's 30-restart cadence
// proved the surface real; the leaf comment 「dispositioning is layered on by
// the commit protocol» is layered on HERE.
func (cm *ContextManager) persistBusEventCommitted(evt *AgentEvent) (stored, deterministic bool) {
	if evt == nil || evt.Message == nil {
		return false, false
	}

	msg := *evt.Message
	if msg.Role == model.RoleSystem {
		msg.Role = model.RoleUser
	}

	// Resolve the canonical fact for this event: reuse the frozen prepared fact
	// verbatim on a durable claim, gate the store when the claim carries no usable fact,
	// and build a fresh one only on the volatile path.
	// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
	var fullEvent memory.FullEvent
	switch {
	case evt.claim != nil && len(evt.claim.PreparedFact) > 0:
		if err := json.Unmarshal(evt.claim.PreparedFact, &fullEvent); err != nil {
			log.Errorf("[persistBusEvent] durable prepared_fact undecodable rid=%s slot=%d — store gated, no restamp: %v",
				evt.claim.RequestID, evt.claim.Slot, err)
			return false, false
		}
		if !isCompletePreparedFact(&fullEvent) {
			log.Errorf("[persistBusEvent] durable prepared_fact incomplete (no fixed non-zero identity key) rid=%s slot=%d — store gated, no restamp",
				evt.claim.RequestID, evt.claim.Slot)
			return false, false
		}
	case evt.claim != nil:
		log.Warnf("[persistBusEvent] durable fact not prepared (barrier failed) rid=%s slot=%d — store gated, claim will replay",
			evt.claim.RequestID, evt.claim.Slot)
		return false, false
	default:
		fullEvent = cm.buildBusFact(evt)
	}
	eventKey := fullEvent.EventKey
	eventType := fullEvent.EventType
	refCanonical := fullEvent

	stored = true
	replayed := false
	switch {
	case cm.memStore == nil:
	case evt.claim != nil:
		replayer, ok := cm.memStore.(memory.EventReplayer)
		if !ok {
			stored = false
			log.Errorf("[persistBusEvent] durable store %T is not replay-capable rid=%s slot=%d — commit gated, claim will replay",
				cm.memStore, evt.claim.RequestID, evt.claim.Slot)
			break
		}
		result, canonicalOut, err := replayer.ReplayEvent(eventKey, fullEvent)
		if err == nil {
			refCanonical = canonicalOut
		}
		switch {
		case err == nil && result == memory.ReplayAlreadyCommitted:
			replayed = true
		case err == nil:
		case memory.IsDuplicateEventKey(err):
			stored = false
			deterministic = true
			log.Errorf("[persistBusEvent] CONFLICT rid=%s slot=%d key=%d (same key, different content) — claim held, envelope not acked: %v",
				evt.claim.RequestID, evt.claim.Slot, eventKey, err)
		case memory.IsEventForgotten(err):
			stored = false
			deterministic = true
			log.Errorf("[persistBusEvent] FORGOTTEN rid=%s slot=%d key=%d (legally tombstoned) — claim held, not re-projected: %v",
				evt.claim.RequestID, evt.claim.Slot, eventKey, err)
		default:
			stored = false
			log.Errorf("[persistBusEvent] ReplayEvent failed key=%d (append gated, replay will retry): %v", eventKey, err)
		}
	default:
		if err := cm.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			stored = false
			log.Errorf("[persistBusEvent] StoreEvent failed key=%d (append gated, spill recovery will restore): %v", eventKey, err)
		}
	}

	ref := memory.EventReference{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    refCanonical.EventType,
		EventSummary: refCanonical.EventSummary,
		Timestamp:    refCanonical.Timestamp,
		Role:         string(tagentevent.EventTypeRole(refCanonical.EventType)),
	}
	if cm.projection != nil && (stored || replayed) && !tagentevent.IsNonProjectionRecord(refCanonical.EventType, refCanonical.Metadata) {
		cm.projection.Append(ref)
	}
	if replayed {
		log.Infof("[persistBusEvent] replay dedup rid=%s slot=%d key=%d already on chain — ref ensured, re-store+feedback skipped",
			evt.claim.RequestID, evt.claim.Slot, eventKey)
		return true, false
	}

	if evt.Source == SourceTask {
		cm.writeSettleFeedback(eventKey, evt.Metadata)
	}

	log.Infof("[persistBusEvent] persisted bus event key=%d type=%s source=%s content=%s",
		eventKey, eventType, evt.Source, truncateForLog(msg.Content, 80))
	return stored, deterministic
}

// commitReceiptFact durably submits a frozen inbox-receipt fact under its RESERVED
// key. Unlike the old persistInboxReceipt — which
// minted a FRESH snowflake key and stamped time.Now() on every call, so a retry or
// restart produced a DIFFERENT, non-idempotent receipt — this commits the exact
// frozen bytes the completion carries through the explicit replay interface, so a
// re-submit of an already-committed receipt converges to ReplayAlreadyCommitted
// (never a second receipt event) and a same-key/different-content collision
// surfaces as an error. The receipt is an internal processing record: it is
// deliberately NOT appended to the projection (it never feeds model context),
// matching the old receipt path and 's non-projection classification.
// : it returns the TYPED error (nil = committed OR already-committed, both
// verify) so the startup reconcile can discriminate a deterministic contradiction
// (same-key-different-content, legally-forgotten receipt) — quarantine material —
// from transient I/O, which must block conservatively instead.
func (cm *ContextManager) commitReceiptFact(receipt memory.FullEvent) error {
	if cm.memStore == nil {
		return errors.New("commitReceiptFact: no store configured")
	}
	replayer, ok := cm.memStore.(memory.EventReplayer)
	if !ok {
		return fmt.Errorf("commitReceiptFact: store %T is not replay-capable key=%d — receipt not committed, claim held", cm.memStore, receipt.EventKey)
	}
	if _, _, err := replayer.ReplayEvent(receipt.EventKey, receipt); err != nil {
		return fmt.Errorf("commitReceiptFact: ReplayEvent key=%d failed — receipt not committed, claim held for replay: %w", receipt.EventKey, err)
	}
	return nil
}

// writeSettleFeedback 把确定性任务裁决写为 feedback 事件，因果边指向 task_settled 事件。
//
// - completed 记 positive、failed 记 negative；suspect、alive-detached 与未知状态不写，防噪声污染 guardrail；写失败仅记日志。
// - 反馈不是 durable 提交或 ack 的凭据，只在事实链 durable 且投影 append 之后运行。
// 契约: docs/wiki/memory/memory-architecture.md#feedback-bind
func (cm *ContextManager) writeSettleFeedback(settledKey int64, md map[string]any) {
	if cm.memStore == nil || md == nil {
		return
	}
	status, _ := md["settle_status"].(string)
	var verdict string
	switch status {
	case "completed":
		verdict = "positive"
	case "failed":
		verdict = "negative"
	default:
		return
	}
	if _, err := memory.BindFeedback(cm.memStore, settledKey, memory.FeedbackPayload{
		Verdict: verdict, Source: "task_settle",
	}); err != nil {
		log.Warnf("[persistBusEvent] settle feedback bind failed (key=%d): %v", settledKey, err)
	}
}

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// buildTurnAttribution assembles the per-turn attribution stamped onto
// LLM-produced events via plugin.WithAttribution: rollout/trace/bundle
// anchors, plus the trigger source. The trigger source (notably
// "meditation") MUST persist into agent_output Metadata — projection
// rebuild re-marks meditation keys from the fact chain, and without this
// stamp the reseed condition Metadata[trigger_source]==meditation can
// never fire unless stamped: this signal must live on the fact chain, not
// only in the in-memory StateDelta.
func (cm *ContextManager) buildTurnAttribution(ctx context.Context) plugin.Attribution {
	attr := plugin.Attribution{}
	if cm.sessionID != "" {
		attr[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	if traceID, spanID := spanTraceIDs(ctx); traceID != "" {
		attr[tagentevent.MetaKeyTraceID] = traceID
		attr[tagentevent.MetaKeySpanID] = spanID
	}
	if cm.bundleIDFn != nil {
		if bid := cm.bundleIDFn(); bid != "" {
			attr[tagentevent.MetaKeyBundleID] = bid
		}
	}
	if cm.triggerSource != "" {
		attr[tagentevent.MetaKeyTriggerSource] = cm.triggerSource
	}
	return attr
}

// captureOwnerAttrs assembles the attribution the INSTALLER already resolved for
// this attempt, so the capture never re-derives it (rl stays a leaf). Keys follow
// the owner block spelling the capture reads; a part this manager does not know is
// simply absent — no empty-value key, no substitute taken from another turn.
func (cm *ContextManager) captureOwnerAttrs(ctx context.Context, invID string) rl.OwnerAttrs {
	attrs := rl.OwnerAttrs{}
	put := func(key, value string) {
		if value != "" {
			attrs[key] = value
		}
	}
	put("capture_namespace", strconv.Itoa(cm.partitionID))
	put("agent_name", cm.name)
	put("root_session_id", cm.sessionID)
	put("session_id", cm.sessionID)
	put("user_id", cm.userID)
	put("invocation_id", invID)
	put("trigger_source", cm.triggerSource)
	if cm.turnEcho != nil && len(cm.turnEcho.committedKeys) > 0 {
		keys := make([]string, 0, len(cm.turnEcho.committedKeys))
		for _, k := range cm.turnEcho.committedKeys {
			keys = append(keys, strconv.FormatInt(k, 10))
		}
		put("input_event_keys", strings.Join(keys, ","))
	}
	for k, v := range cm.buildTurnAttribution(ctx) {
		put(k, v)
	}
	return attrs
}

// echoSpec RunFlow calls runner.Run and forwards events to outputCh. Delivery only:
// projection writes happen in the event-plugin pipeline (ProjectionSink), and
// the loop waits for the next turn via bus.Pull — there is no bus echo.
// echoSpec is the turn-level, immutable template runEventLoop installs when this
// turn's durable input facts are already committed. RunFlow clones it into a
// fresh per-attempt plugin.EchoCredential (unique token), so there is no shared mutable
// whole-turn state — the derived credential owns its own bind state. No mutex here.
type echoSpec struct {
	agent         string
	session       string
	mergedMessage string
	committedKeys []int64
}

// newAttemptEchoCredential stamps a unique per-runner-attempt credential from the
// turn's echo spec. A retry of the same business turn calls this again → a new
// AttemptToken reusing the same committed facts; the prior attempt's credential is
// released as its ctx ends.
func (cm *ContextManager) newAttemptEchoCredential() *plugin.EchoCredential {
	return &plugin.EchoCredential{
		AttemptToken:  fmt.Sprintf("%s#attempt-%d", cm.name, cm.attemptSeq.Add(1)),
		Agent:         cm.turnEcho.agent,
		Session:       cm.turnEcho.session,
		MergedMessage: cm.turnEcho.mergedMessage,
		CommittedKeys: cm.turnEcho.committedKeys,
	}
}

// turnEchoVerified reports the  execution-credential outcome of the most recent
// attempt as (installed, verified). installed=true means this turn had durable committed
// inputs expecting an echo; verified=false while installed means the model ran (or was
// gated) on UNverified input — the echo was never bound or a plugin error downgraded it,
// so the loop MUST NOT ack. When not installed (volatile / no durable commit) it reports
// (false, true) — normal ack.
func (cm *ContextManager) turnEchoVerified() (installed, verified bool) {
	if cm.turnEcho == nil {
		return false, true
	}
	return true, cm.lastEchoCred != nil && cm.lastEchoCred.Verified()
}

// RunFlow 在"当前生效"的执行面上跑一个业务回合。需要在回合边界钉住特定执行器（如热更新与
// 发布交错的场景）时改用 RunFlowWithExecutor。
func (cm *ContextManager) RunFlow(ctx context.Context, msg model.Message) error {
	return cm.RunFlowWithExecutor(ctx, msg, nil)
}

// RunFlowWithExecutor runs one business turn on the executor pinned at the turn
// boundary. pinned MUST come from BeginTurn of the same turn; when it is
// nil the executor is resolved here instead — the correct behavior for paths
// that have no turn boundary of their own (one-shot/sub-agent invocations, whose
// TagentAgent instance and executor were already constructed inside one
// generation and are never republished in place).
func (cm *ContextManager) RunFlowWithExecutor(ctx context.Context, msg model.Message, pinned runner.Runner) error {
	lease, inherited := execLeaseFromContext(ctx)
	if !inherited && pinned == nil {
		lease = cm.AcquireLease(LeaseTurn)
		defer lease.Release()
		ctx = lease.WithContext(ctx)
		pinned = lease.Runner()
	}
	if err := lease.Err(); err != nil {
		return fmt.Errorf("%w: agent %q turn", err, cm.name)
	}
	if cm.turnEcho != nil {
		cred := cm.newAttemptEchoCredential()
		cm.lastEchoCred = cred
		ctx = plugin.WithEchoCredential(ctx, cred)
	}
	if cm.projection != nil {
		ctx = plugin.WithProjectionSink(ctx, cm.projection)
		ctx = withCallProjection(ctx, cm.projection)
		ctx = plugin.WithAttribution(ctx, cm.buildTurnAttribution(ctx))
	}
	if cm.captureEnabled {
		invID, _ := invocationIDFromContext(ctx)
		scope := rl.NewCaptureScope(invID)
		scope.Owner = cm.captureOwnerAttrs(ctx, invID)
		ctx = rl.WithCaptureScope(ctx, scope)
		defer scope.Release()
	}
	if cm.taskController != nil {
		// Wrap the spawner to snapshot the originating turn's invocation
		// metadata (chat_id, ...) as opaque origin baggage on each spawned
		// task, so a background settle can be routed back to the originating
		// session. The task layer never interprets it.
		var spawner task.TaskSpawner = cm.taskController
		md := cm.GetInvocationMetadata()
		traceID, spanID := spanTraceIDs(ctx)
		ts := cm.triggerSource
		invID, _ := invocationIDFromContext(ctx)
		if len(md) > 0 || traceID != "" || ts != "" || invID != "" {
			cp := make(map[string]string, len(md)+4)
			for k, v := range md {
				cp[k] = v
			}
			if traceID != "" {
				cp[tagentevent.MetaKeyTraceID] = traceID
				cp[tagentevent.MetaKeySpanID] = spanID
			}
			if ts != "" {
				cp[tagentevent.MetaKeyTriggerSource] = ts
			}
			if invID != "" {
				cp[metaKeyInvocationID] = invID
			}
			spawner = &task.OriginSpawner{TaskController: cm.taskController, Origin: cp}
			if invID != "" && cm.settleSinks != nil {
				spawner = &countingSpawner{inner: spawner, sinks: cm.settleSinks, id: invID}
			}
		}
		ctx = task.WithTaskSpawner(ctx, spawner)
	}
	exec := pinned
	if exec == nil {
		exec = cm.currentRunner()
	}
	eventCh, err := exec.Run(ctx, cm.userID, cm.sessionID, msg)
	if err != nil {
		startErr := fmt.Errorf("runner.Run: %w", err)
		cm.lastTurnOutcome = reduceTurnOutcome(startErr, "", false)
		return startErr
	}

	cm.turnProductive = false
	// : response-internal error captured from the stream. The framework
	// surfaces a model/API failure as an event carrying Response.Error (it does
	// NOT make RunFlow return an error), so the old code — which never inspected
	// Response.Error — mistook such a turn for success and ACKed its durable
	// inputs. Capture the first such error to reduce the attempt honestly.
	var respErr string
	for fwEvt := range eventCh {
		if fwEvt == nil {
			continue
		}
		evt := cloneEventForDelivery(fwEvt)
		if cm.triggerSource != "" {
			evt.StateDelta[tagentevent.MetaKeyTriggerSource] = []byte(cm.triggerSource)
		}
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
			if len(m.ToolCalls) > 0 || (isFinalResponse(evt) && strings.TrimSpace(m.Content) != "") {
				cm.turnProductive = true
			}
		}
		if evt.Response != nil && evt.Response.Error != nil && respErr == "" {
			respErr = fmt.Sprintf("%s: %s", evt.Response.Error.Type, evt.Response.Error.Message)
		}
		if cm.onEvent != nil {
			cm.onEvent(evt)
		}
		if cm.outputCh != nil {
			if !cm.deliverEvent(ctx, evt) && ctx.Err() != nil {
				cm.lastTurnOutcome = reduceTurnOutcome(ctx.Err(), respErr, true)
				return ctx.Err()
			}
		}
	}
	cm.lastTurnOutcome = reduceTurnOutcome(nil, respErr, false)
	return nil
}

// LastTurnOutcome returns the reduced terminal state of the most recent
// RunFlow attempt. The persistent loop uses it to distinguish a genuinely
// completed turn from a response-error failure or a shutdown cancellation that
// a nil transport return would otherwise hide.
func (cm *ContextManager) LastTurnOutcome() turnOutcome {
	return cm.lastTurnOutcome
}

// setLastBatchOutcome records the loop's  reduced terminal state for the
// most recent turn (called only on the loop goroutine after the retry budget).
func (cm *ContextManager) setLastBatchOutcome(o turnOutcome) { cm.lastBatchOutcome = o }

// LastTurnDegenerate reports whether the most recent RunFlow turn produced
// nothing: no tool call and no non-empty final. The persistent loop uses it
// to retry such a turn once (an occasional model hiccup would otherwise
// stall the conversation until the next external event).
func (cm *ContextManager) LastTurnDegenerate() bool {
	return !cm.turnProductive
}

// cloneEventForDelivery makes a shallow copy of the event with its own
// StateDelta map, so tagent-side metadata writes never touch the framework's
// shared event object. Response and other pointers are shared read-only.
func cloneEventForDelivery(evt *event.Event) *event.Event {
	cp := *evt
	cp.StateDelta = make(map[string][]byte, len(evt.StateDelta)+4)
	for k, v := range evt.StateDelta {
		cp.StateDelta[k] = v
	}
	return &cp
}

// injectLiveTaskBoard renders the current active-task board and appends it at
// the TAIL of the message list (after the current input / pending tool
// results): the board bytes change every call (task ages), so only the tail
// position keeps the prompt-cache prefix intact. The taskController nil-check
// is at CALL time, not registration time — taskController is wired after
// ContextManager construction, so a registration-time guard would permanently
// skip the board. The board is ephemeral: request-only, never projected or
// compressed.
// 契约: docs/wiki/agent/task-lifecycle.md#board-rendering
func (cm *ContextManager) injectLiveTaskBoard(args *model.BeforeModelArgs) {
	if cm.taskController == nil {
		return
	}
	board, reused := cm.takeTurnBoard(args)
	if !reused {
		board = task.RenderBoard(cm.taskController.List(), cm.taskController.DefaultTTL())
	}
	if board != "" {
		args.Request.Messages = task.InjectBoard(args.Request.Messages, board)
	}
}

// peekRecoveryNotice reads the pending cold-start notice WITHOUT consuming it.
// The assembly must price what this turn will inject; the one-shot spend stays
// exactly where it always was — at the actual outbound call, inside the gate.
// TakeRecoveryNotice remains the only consumer.
func (cm *ContextManager) peekRecoveryNotice() string {
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	return cm.recoveryNotice
}

// SetUserIDSessionID updates the user/session context for runner.Run.
func (cm *ContextManager) SetUserIDSessionID(userID, sessionID string) {
	cm.userID = userID
	cm.sessionID = sessionID
}

// allBindings returns the active generation plus every retiree still on the
// unconverged list (dedicated snapshot: callers must not mutate it).
func (cm *ContextManager) allBindings() []*execBinding {
	var out []*execBinding
	cm.executorMu.RLock()
	if cm.active != nil {
		out = append(out, cm.active)
	}
	cm.executorMu.RUnlock()
	cm.retiredMu.Lock()
	for b := range cm.retiredBindings {
		out = append(out, b)
	}
	cm.retiredMu.Unlock()
	return out
}

// OutstandingRefs reports how many execution references this manager's generations
// currently hold. Owner-level decisions above the agent layer ask
// "is any execution still in flight" through this, so they read the SAME accounting
// the reclaim path acts on instead of maintaining a parallel notion of busy.
func (cm *ContextManager) OutstandingRefs() int { return cm.totalRefs() }

// totalRefs is the sum of outstanding references over all generations of this
// manager — the quantity a bounded drain waits for (: it covers background
// executions and inherited sub-calls, not just business turns).
func (cm *ContextManager) totalRefs() int {
	total := 0
	for _, b := range cm.allBindings() {
		b.mu.Lock()
		total += b.total
		b.mu.Unlock()
	}
	return total
}

// execCloseGrace is the last-chance convergence window a Close grants the
// generations it retires. The enclosing shutdown already had its own bounded
// drain (waitForTurns), so this is a grace for work landing in the gap — not a
// second full timeout. A var so tests can shrink it.
var execCloseGrace = 2 * time.Second

// retireActiveBinding retires the generation currently in force, so a terminal
// Close cannot leave the current executor unaccounted for: from here on it is an
// ordinary retiree — closed by its own last reference, reported while it still
// has one.
func (cm *ContextManager) retireActiveBinding() {
	cm.retireBinding(cm.activeBinding())
}

// Close is the terminal drain, bounded. It retires the generation in force, waits
// a grace for every generation's OWN references to converge (each then closes
// itself exactly once on the ordinary reclaim path) and returns. Generations
// whose producers never confirmed a stop are NOT force-closed: closing an executor
// under a live writer is exactly the failure spec runtime-resource-ownership
// forbids (「未确认停止者继续显式持有…不无限等待或强关」). They stay explicitly
// held, remain readable through UnconvergedRefs, and are reported as
// ErrExecUnconverged so no caller can claim a clean close. The holder acts on it
// by keeping the shared resources they ride on (see agent.Close).
func (cm *ContextManager) Close() error {
	cm.retireActiveBinding()
	cm.WaitForInFlight(execCloseGrace)
	held := cm.UnconvergedRefs()
	if len(held) == 0 {
		cm.retiredMu.Lock()
		cm.retiredBindings = nil
		cm.retiredMu.Unlock()
		return nil
	}
	log.Warnf("[ContextManager] bounded close left %d unconverged generation(s): %+v — their executors are HELD, not force-closed; investigate the execution that never confirmed a stop",
		len(held), held)
	return fmt.Errorf("%w: %d generation(s) still referenced: %+v", ErrExecUnconverged, len(held), held)
}

// WaitForInFlight blocks until every outstanding execution reference (resident
// loop turns, one-shot/sub-call turns AND post-ACK background runs — all of them
// registered on their generation) has drained, or the timeout passes; false means
// work was still active, and the caller can name it with UnconvergedRefs. The
// close sequence uses it so the output channel is never settled while a turn may
// still send to it.
func (cm *ContextManager) WaitForInFlight(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for cm.totalRefs() > 0 {
		if time.Now().After(deadline) {
			cm.noteUnconverged()
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

func isFinalResponse(evt *event.Event) bool {
	if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return false
	}
	choice := evt.Response.Choices[len(evt.Response.Choices)-1]
	return choice.Message.Role == model.RoleAssistant && len(choice.Message.ToolCalls) == 0
}

// formatMessages returns a human-readable summary of messages for debug logs.
func formatMessages(messages []model.Message) string {
	var sb strings.Builder
	for i, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "unknown"
		}
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		toolInfo := ""
		if len(msg.ToolCalls) > 0 {
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			toolInfo = fmt.Sprintf(" tool_calls=%v", names)
		}
		if msg.ToolID != "" {
			toolInfo += fmt.Sprintf(" tool_id=%s", msg.ToolID)
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s: %q%s\n", i, role, content, toolInfo))
	}
	return sb.String()
}
