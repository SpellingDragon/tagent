package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/prompt"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ---------------------------------------------------------------------------
// compress.TokenCounter
// ---------------------------------------------------------------------------

// ContextManager
// ---------------------------------------------------------------------------

// ContextManager is the unified component for message building, compression
// orchestration, and framework Flow execution. It replaces Preprocessor and
// FrameworkFlowAdapter.
//
// Prototype mapping:
//   - OnEvents (append inputs + call model) → BuildInvocation + RunFlow
//   - Compact (clean projection) → compress.ContextCompressor in BeforeModel callback
type ContextManager struct {
	// turnEcho (§4.4, design 决策4): the batch echo spec runEventLoop installs
	// before the model runs, ONLY when this turn's durable input facts are already
	// committed. RunFlow stamps a FRESH per-attempt plugin.EchoCredential from it
	// (unique request token per runner attempt; no whole-turn / "first envelope"
	// state — the old DurableInbound framing). nil → no pre-committed echo this turn.
	turnEcho   *echoSpec
	attemptSeq atomic.Int64
	// lastEchoCred is the credential RunFlow minted for the most recent attempt; the
	// event loop consults it after RunFlow to fail-closed (§4.5) when a durable turn's
	// echo was never verified at the model entry.
	lastEchoCred *plugin.EchoCredential

	// recovery* (resident-readiness-plan 3.8): cold-start rebuild outcome and
	// the one-shot model-facing notice; written once at rebuild, read-only after.
	recoveryMu     sync.Mutex
	recovery       *RecoveryResult
	recoveryNotice string

	contextCompressor *compress.ContextCompressor

	// hotswap-fix 5.7：executor 重建所需的状态面引用与执行面快照。
	// NewContextManager 时从 cfg 快照；RebuildExecutor 重建时执行面取调用方
	// 覆盖值、状态面复用快照实例（cfg 零值不可信）。
	memPlugin    *plugin.MemoryPlugin
	sessionSvc   session.Service
	execCfg      ContextManagerConfig // 冷启动 cfg 快照（含全部字段）
	tokenCounter compress.TokenCounter
	memStore     memory.MemoryStore
	maxTokens    int
	thresholdPct float64

	// Framework integration
	runner     runner.Runner
	executorMu sync.RWMutex // R4（resident-continuity-r2-r4 3.3）：runner 可换缝守护——SwapExecutor（写）vs RunFlow per-turn RLock（读）

	// Retired-runner recycling (implementation-hardening 5.1): swapped-out
	// runners wait here until no in-flight turn references any of them, then
	// get an idempotent io.Closer Close. See RetireRunner / sweepRetiredRunners.
	retireMu       sync.Mutex
	retiredRunners []retiredRunner
	runnerInFlight atomic.Int64
	name           string
	userID         string
	sessionID      string

	// Event routing
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
	orgReloader func()

	// bundleIDFn returns the currently active evolution bundle id (empty when
	// evolution is disabled or no bundle is active). Both persistence paths
	// stamp it into FullEvent.Metadata (bundle_id, D1-B design-report-closeout)
	// so guardrail/feedback aggregation can join events to bundle versions
	// precisely. nil-safe.
	bundleIDFn func() string

	// taskController, when set, is injected into the RunFlow ctx (as a
	// task.TaskSpawner) so tools can hand long-running work to the task layer, and
	// is used to render the live task board at BeforeModel. nil → tools fall
	// back to synchronous execution and no board is rendered.
	taskController task.TaskController

	// triggerSource identifies what triggered the current RunFlow
	// (e.g., "user", "meditation", "async_result"). Set by runEventLoop
	// before calling RunFlow. Attached to outputCh events via
	// StateDelta["trigger_source"] for deterministic consumer dispatch.
	triggerSource string

	// turnProductive records whether the current/most-recent RunFlow turn
	// produced anything (a tool call or a non-empty final). Written and read
	// only on the loop goroutine that drives RunFlow.
	turnProductive bool

	// lastTurnOutcome is the §5.1 reduced terminal state of the most recent
	// RunFlow attempt (completed / failed / cancelled), observed from BOTH the
	// transport return AND the event stream (a Response.Error event, a mid-drain
	// ctx cancellation). The old loop trusted only the nil transport return and
	// ACKed failed/cancelled turns as success. Written and read only on the loop
	// goroutine that drives RunFlow; the persistent loop reduces it across the
	// retry budget into batchOutcome.
	lastTurnOutcome turnOutcome

	// lastBatchOutcome holds the §5.1 loop-level reduction of the retry attempts
	// for the most recent turn (completed or failed; a cancellation returns before
	// it is ever set). §5.2/§5.3 freeze the completion from this value; set on the
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

// SetTriggerSource sets the trigger source for the next RunFlow call.
// OrgHotParams is the hot-applicable numeric bundle (full-hot-config Phase 1,
// 2026-09-16): zero/negative fields keep current settings. Structural wiring
// (memStore/bus/projection/runner) is NOT in this bundle — that follows the
// shell-rebuild path.
type OrgHotParams struct {
	ThresholdPct    float64
	MaxTokens       int
	KeepRecentTasks int
	TaskTerminalTTL time.Duration
	TaskDefaultTTL  time.Duration // >0 set unified-reaper fallback lifetime / 0 keep (no disable)
}

// ApplyOrgParams hot-swaps the org-layer numeric parameters that can be
// migrated onto the live ContextManager without rebuilding the agent
// topology (design: incremental A of agent-config-hot-reload).
//
// Currently migratable: compress_threshold (via the live
// compress.ContextCompressor's atomic UpdateThreshold). cm.thresholdPct is
// kept in sync for introspection consistency; the authoritative consumer is
// the compressor.
//
// Structural fields (tools, sub-agents, prompts wiring, memStore) are NOT
// touched here — they belong to snapshot-level rebuild (incremental B).
func (cm *ContextManager) ApplyOrgParams(thresholdPct float64) {
	cm.ApplyOrgHotParams(OrgHotParams{ThresholdPct: thresholdPct})
}

// ApplyOrgHotParams applies the full hot numeric bundle at one point
// (full-hot-config Phase 1): the C-defect fix — maxTokens joins threshold in
// the hot face, so a yaml window change reaches the resident cm WITHOUT a
// process restart.
func (cm *ContextManager) ApplyOrgHotParams(p OrgHotParams) {
	if p.ThresholdPct > 0 {
		cm.thresholdPct = p.ThresholdPct
	}
	if cm.contextCompressor != nil {
		cm.contextCompressor.ApplyHotParams(p.ThresholdPct, p.MaxTokens, p.KeepRecentTasks)
	}
}

// SetOrgReloader arms the lazy org-config check (see orgReloader field).
// CheckOrgReload invokes the armed lazy org-config check once (ops/test hook;
// the production path fires it in the BeforeModel callback).
func (cm *ContextManager) CheckOrgReload() {
	if cm.orgReloader != nil {
		cm.orgReloader()
	}
}

func (cm *ContextManager) SetOrgReloader(fn func()) {
	cm.orgReloader = fn
}

// RunOrgReloader explicitly invokes the armed reloader if present. The lazy
// path fires it before each LLM call; this exported entry point lets tests and
// operators trigger the identical check deterministically (the closure itself
// is single-flight and mtime-guarded, so redundant calls are cheap no-ops).
func (cm *ContextManager) RunOrgReloader() {
	if cm.orgReloader != nil {
		cm.orgReloader()
	}
}

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

	// Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	Compressor   *compress.SmartCompressor
	TokenCounter compress.TokenCounter
	MaxTokens    int
	ThresholdPct float64
	MemStore     memory.MemoryStore

	// CompactKeysListed / RecentFullCount configure compress.ContextCompressor
	// constraints (0 = package defaults; RecentFullCount derives from
	// keepRecent × compress.DefaultRefsPerTurn when unset, D6).
	CompactKeysListed int
	RecentFullCount   int
	CardMaxChars      int

	// Unified Runner: plugins + session service registered on the same Runner
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
		maxTokens:          cfg.MaxTokens,
		thresholdPct:       cfg.ThresholdPct,
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

	// Build compress.ContextCompressor from compress.SmartCompressor.
	if cfg.Compressor != nil {
		keepRecent := cfg.Compressor.KeepRecentTasks
		cm.contextCompressor = compress.NewContextCompressor(
			cfg.Compressor,
			cfg.MemStore,
			cfg.TokenCounter,
			cfg.MaxTokens,
			cfg.ThresholdPct,
			keepRecent,
			compress.WithCompactKeysListed(cfg.CompactKeysListed),
			compress.WithRecentFullCount(cfg.RecentFullCount),
			compress.WithCardMaxChars(cfg.CardMaxChars),
		)
	}

	// NOTE: the LLMAgent construction lives ONLY in buildLLMAgent below — the
	// sole fwAgent path, gated by the execution model (§4.5). An earlier
	// inline option block here was dead residue of the hotswap-fix 5.7
	// extraction and misleading (it built a NON-gated option list that no
	// consumer ever used). §7.5: assembly must mirror the executed surface.
	// hotswap-fix 5.7：快照完整 cfg（执行面+状态面），供 RebuildExecutor 复用。
	cm.execCfg = cfg
	cm.memPlugin = cfg.MemPlugin
	cm.sessionSvc = cfg.SessionSvc

	fwAgent := cm.buildLLMAgent(cfg)
	cm.runner = buildRunner(cfg, fwAgent)

	return cm
}

// buildModelCallbacks（hotswap-fix 5.7）：回调链构造抽为 cm 方法——闭包捕获
// 同一个 cm（装配/热载/任务板/诊断全部同源）。冷启动与 RebuildExecutor 共用，
// 保证换装后 BeforeModel 闭包仍指向常驻状态面（修复空投影装配事故）。
func (cm *ContextManager) buildModelCallbacks(source prompt.Getter) *model.Callbacks {
	// Build BeforeModel callback chain.
	cb := model.NewCallbacks()

	// Callback -1: System prompt hot-reload.
	// When SystemPromptSource is configured, re-reads system prompt files
	// before each LLM call and replaces the system message.
	// This enables prompt tuning without restarting the agent process.
	// hardening-review-batch2 6.2（执行代快照）：捕获**本代配置**的 source，
	// 而非 cm 的可变字段——热更换 prompt source 后，旧代 in-flight runner 的
	// 回调仍读旧 source（不受扰），新代读新 source。
	if source != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			freshPrompt, err := source.Get()
			if err != nil || freshPrompt == "" {
				return nil, nil // Graceful: keep existing system prompt
			}
			// Replace or insert system message at position 0
			if len(args.Request.Messages) > 0 && args.Request.Messages[0].Role == model.RoleSystem {
				args.Request.Messages[0].Content = freshPrompt
			} else {
				// Prepend system message
				args.Request.Messages = append(
					[]model.Message{model.NewSystemMessage(freshPrompt)},
					args.Request.Messages...,
				)
			}
			return nil, nil
		})
	}

	// Unified BeforeModel callback: projection-sole-source assembly
	// (unified-event-projection D2). See assembleRequest: drains mid-turn bus
	// events into the projection, compresses, and rebuilds the request as
	// [system] + render(projection). Nothing is read back from the framework's
	// message tail — every event reaches the projection via the event-plugin
	// pipeline or persistBusEvent, so the boundary is strictly one-way.
	if cm.contextCompressor != nil && cm.projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			if cm.orgReloader != nil {
				cm.orgReloader() // lazy org-config hot-reload check (incr. A)
			}
			cm.assembleRequest(ctx, args)
			return nil, nil
		})
	} else if cm.bus != nil {
		// Fallback: if no compressor configured, still inject bus events
		// (append directly to messages, legacy behavior for tests without projection).
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

	// Callback 0.4: live task board (D6). Renders currently-active async tasks
	// fresh from the registry and injects them just before the current input —
	// a recency anchor of async state that never enters projection/history, so
	// it does NOT participate in compression. Skipped when no tasks are active.
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		cm.injectLiveTaskBoard(args)
		return nil, nil
	})

	// Callback 0.45 removed (unified-event-projection D6): tool-pairing repair
	// is unnecessary under pairing-free rendering — rendered history contains
	// no role=tool messages, so no pairing constraint can be violated.

	// Callback 0.5: BeforeLLM diagnostic log — print messages after compression.
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		log.Debugf("[BeforeLLM] messages:\n%s", formatMessages(args.Request.Messages))
		return nil, nil
	})

	return cb
}

// buildLLMAgent（hotswap-fix 5.7）：从 ContextManagerConfig 构造 fwAgent 的
// 纯函数段（依赖仅 cfg）。冷启动与 RebuildExecutor 共用，保证两条路径构造的
// fwAgent 行为学一致（model/tools/prompt/genConfig/并行工具开关）。
func (cm *ContextManager) buildLLMAgent(cfg ContextManagerConfig) *llmagent.LLMAgent {
	cb := cm.buildModelCallbacks(cfg.SystemPromptSource)
	maxIters := cfg.MaxToolIters
	if maxIters <= 0 {
		maxIters = DefaultMaxToolIterations
	}
	// §4.5: wrap the model with the execution-credential gate (block the real model call
	// when a durable turn's echo credential is unverified) + recovery-notice deferral to
	// actual invocation. buildLLMAgent is the sole fwAgent construction → covers cold start
	// and RebuildExecutor. nil-safe (some constructions carry no model).
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
// cfg）——冷启动与 SwapExecutor 热重建共用同一装配路径（行为学一致）。
func buildRunner(cfg ContextManagerConfig, fwAgent *llmagent.LLMAgent) runner.Runner {
	// Create unified Runner: LLMAgent + MemoryPlugin + SummaryPlugin + SessionService.
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

// SwapExecutor（R4 3.3）：原子换入新 runner（含其 tools/prompt 装配——随
// runner 一并构建完成）。drain-free turn 级：进行中 turn 已持有的旧 runner
// 引用跑完，下一 turn 起用新。返回被换下的旧 runner（调用方决定处置）。
func (cm *ContextManager) SwapExecutor(newRunner runner.Runner) runner.Runner {
	if cm == nil || newRunner == nil {
		return nil
	}
	cm.executorMu.Lock()
	defer cm.executorMu.Unlock()
	old := cm.runner
	cm.runner = newRunner
	return old
}

// RebuildExecutor（hotswap-fix 5.7）：在**同一 cm** 上重建 executor——
// 新 fwAgent（新 model/tools/prompt/genConfig）+ 新 runner，但 projection/bus/
// compressor/sessionSvc/BeforeModel 回调闭包全部保留。修复 2026-09-13 21:5x
// 事故根因：旧换装路径把「新壳自己的 runner」换进常驻 cm，而新壳 fwAgent 的
// BeforeModel 闭包捕获新壳自己的空 cm → 换装后所有 turn 的请求装配被接到空
// 投影上（n=1 system-only → provider 400/1214 ×3）。本方法保证装配闭包与
// 状态永远同源（都来自这个 cm），换装只换「模型+工具+提示词」这个纯执行面。
// cfg：沿用冷启动同构的 ContextManagerConfig（调用方从 fresh 配置装配）；
// 装配逻辑与 NewContextManager 尾段一致（fwAgent 构造 + buildRunner）。
func (cm *ContextManager) RebuildExecutor(cfg ContextManagerConfig) runner.Runner {
	if cm == nil {
		return nil
	}
	// 执行面取调用方覆盖（零值字段回落到冷启动快照 execCfg）；状态面强制
	// 复用 cm 快照实例——换装新配置只提供执行面差异，状态面永不换。
	merged := cm.execCfg
	if cfg.Model != nil {
		merged.Model = cfg.Model
	}
	if len(cfg.Tools) > 0 {
		merged.Tools = cfg.Tools
	}
	if cfg.SystemPrompt != "" {
		merged.SystemPrompt = cfg.SystemPrompt
	}
	if cfg.SystemPromptSource != nil {
		merged.SystemPromptSource = cfg.SystemPromptSource
	}
	if cfg.Temperature > 0 {
		merged.Temperature = cfg.Temperature
	}
	if cfg.MaxToolIters > 0 {
		merged.MaxToolIters = cfg.MaxToolIters
	}
	if cfg.ThinkingEnabled != nil {
		merged.ThinkingEnabled = cfg.ThinkingEnabled
	}
	if cfg.ThinkingTokens != nil {
		merged.ThinkingTokens = cfg.ThinkingTokens
	}
	if cfg.ReasoningEffort != nil {
		merged.ReasoningEffort = cfg.ReasoningEffort
	}
	if cfg.ReasoningContentMode != "" {
		merged.ReasoningContentMode = cfg.ReasoningContentMode
	}
	execCfg := merged
	execCfg.MemPlugin = cm.memPlugin
	execCfg.SessionSvc = cm.sessionSvc
	// hardening-review-batch2 6.1（执行代绑定）：换入前把新代工具 wrapper 的
	// parentProjection 重绑到常驻投影——新壳构建路径的 SetToolParentProjection
	// 绑的是新壳空投影（其 projection 为 nil/空），换入后子 agent 自动上下文
	// 注入会读到空（event_keys 省略时 nil）。
	for _, t := range execCfg.Tools {
		if w, ok := t.(*AgentToolWrapper); ok {
			w.SetParentProjection(cm.projection)
		}
	}
	// 6.2：生效代快照更新——本代 system prompt source 即 execCfg 所带；
	// （cm.systemPromptSource 保持冷启动值，供诊断/回退对照，不参与新代
	// callback——callback 已按代捕获。）
	fwAgent := cm.buildLLMAgent(execCfg)
	if old := cm.SwapExecutor(buildRunner(execCfg, fwAgent)); old != nil {
		cm.RetireRunner(old)
	}
	return cm.currentRunner()
}

// retiredRunner pairs a swapped-out runner with its retirement time (the
// leak-alarm input: a retiree still gated by in-flight turns after
// retiredLeakAfter is flagged loudly instead of silently leaking).
type retiredRunner struct {
	r  runner.Runner
	at time.Time
}

// retiredLeakAfter gates the leak alarm: past this age a retiree that still
// cannot be closed is almost certainly a leak (see sweepRetiredRunners).
const retiredLeakAfter = 10 * time.Minute

// RetireRunner queues a swapped-out runner for delayed Close (implementation-
// hardening 5.1): closed by the next sweep that observes zero in-flight turns
// (the persistent loop is a single consumer, so quiescence arrives at every
// turn boundary — a sustained-load fallback timer is unnecessary), or by
// Close. Rollback does NOT reuse old runners (it rebuilds from the ring-2
// config snapshot), so every retired runner is pure garbage. Close is part
// of the upstream Runner interface and documented idempotent.
func (cm *ContextManager) RetireRunner(old runner.Runner) {
	if cm == nil || old == nil {
		return
	}
	cm.retireMu.Lock()
	cm.retiredRunners = append(cm.retiredRunners, retiredRunner{r: old, at: time.Now()})
	cm.retireMu.Unlock()
	cm.sweepRetiredRunners()
}

// sweepRetiredRunners closes retired runners when no turn is in flight.
// Callers: RunFlow exit (counter drop), RetireRunner, Close.
func (cm *ContextManager) sweepRetiredRunners() {
	if cm.runnerInFlight.Load() != 0 {
		// Leak alarm (review P2-3): in-flight turns gate the close — correct
		// for drain-free, but a retiree stuck past retiredLeakAfter means the
		// gate never opens (sustained concurrent flows). Say so loudly;
		// force-closing here would break the in-flight turns instead.
		cm.retireMu.Lock()
		for _, e := range cm.retiredRunners {
			if time.Since(e.at) > retiredLeakAfter {
				log.Warnf("[ContextManager] retired runner pending >%s under sustained in-flight load — possible leak, investigate RunFlow concurrency", retiredLeakAfter)
				break // one alarm per sweep
			}
		}
		cm.retireMu.Unlock()
		return
	}
	cm.retireMu.Lock()
	defer cm.retireMu.Unlock()
	if cm.runnerInFlight.Load() != 0 { // re-check under lock (inc may have raced in)
		return
	}
	for _, e := range cm.retiredRunners {
		if err := e.r.Close(); err != nil { // upstream documents Close as idempotent
			log.Warnf("[ContextManager] retired runner Close: %v", err)
		}
	}
	cm.retiredRunners = nil
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
		// §4.3: a valid non-text (image/file) payload has empty Content but must still
		// reach the request — collect its parts so an image-only input is never dropped.
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
//
//	[system] + render(projection) (+ live task board, injected by a later callback)
//
// The projection is the SOLE assembly source (unified-event-projection D2).
// Nothing is read back from the framework's message tail — every event
// (user input, tool calls, tool results, finals, bus injections) reaches the
// projection through the event-plugin pipeline or persistBusEvent, so the
// data flow across the framework boundary is strictly one-way.
func (cm *ContextManager) assembleRequest(ctx context.Context, args *model.BeforeModelArgs) {
	// F5 (D1 design): BeforeModel SHALL NOT claim bus events mid-turn.
	// The persistent event loop is the sole bus consumer; new arrivals wait
	// for the next Pull at the turn boundary. The previous TryPull block here
	// caused claimed durable envelopes to bypass the turn-completion protocol
	// (F5: inputs claimed here never reached finishDurableBatch).

	// Resolve projection → (possibly compressed) historical messages.
	refs := cm.projection.GetAll()
	result := cm.contextCompressor.Compress(ctx, refs)
	cm.projection.Replace(result.RetainedRefs)
	// event-sourced-projection D2：折叠本身是事实链的一条 compaction 事件（仅真折叠时发射，
	// 先 Replace 后写事件；失败留痕不阻断）。投影恒为事实链的旁路产物。
	// Compressed 门控（review 🟠1）：under-budget 轮 RetainedRefs 原样返回，首位仍是
	// 上次折叠遗留的综述负 key ref——仪靠 BuildCompactionPayload 的首位判定拦不住
	// 「已折叠后的 under-budget 轮」，会每轮误发（写放大+违反 spec「未折叠不写」）。
	if result.Compressed {
		if notice := cm.emitCompactionEvent(result.RetainedRefs); notice != nil {
			// 降级留痕：Notices 承载契约字面；Messages 是既有消费路径
			// （[context_compress_error] 先例），双写保证可感知。
			result.Notices = append(result.Notices, *notice)
			result.Messages = append(result.Messages, *notice)
		}
	}

	// Rebuild: [system] + render(projection). The system message is the only
	// part of args.Request.Messages that is read.
	systemMsg, _ := compress.SplitSystemMessage(args.Request.Messages)
	rebuilt := make([]model.Message, 0, len(result.Messages)+1)
	if systemMsg != nil {
		rebuilt = append(rebuilt, *systemMsg)
	}
	rebuilt = append(rebuilt, result.Messages...)

	// hotswap-fix 5.7 健康探针：装配产物只有 system（n<=1）说明投影/会话
	// 状态面异常（如换装事故中的空投影装配）。此时不打到 provider 吃 400，
	// 而是注入降级 user 消息：请求形态合法（LLM 可应答），同时 ERROR 日志
	// 带投影计数与重建模式，一击定位。user 消息本身会经事件管线回流，
	// 下个 turn 投影非空即自然恢复。
	if len(rebuilt) <= 1 {
		log.Errorf("[assemble-health] request has only %d message(s) (system-only); "+
			"projection len=%d — injecting degradation notice (hotswap empty-projection guard)",
			len(rebuilt), cm.projection.Len())
		rebuilt = append(rebuilt, model.NewUserMessage(
			"[context-guard] 会话状态异常（上下文为空）。请向用户如实说明当前对话上下文不可用，请其重发上一条消息。"))
	}

	// F9 (D6) recovery notice is NO LONGER consumed here: §4.5C moved it to the
	// execution gate (executionGateModel), injected at the ACTUAL model invocation so a
	// created-but-never-iterated lazy iterator does not consume the one-shot notice. The
	// gate runs after this rebuild and appends the notice at the same request tail (still
	// never persisted), preserving D6.
	args.Request.Messages = rebuilt
}

// emitCompactionEvent（event-sourced-projection D1/D4/D6）：真折叠后把折叠产物作为
// 一等 compaction 事件落事实链——context_compress_summary 正 key 事件，Content/EventSummary
// = 综述正文（可召回正文即叙事本身），Metadata[compaction_payload] 载重建载荷（综述 ref +
// 有序 retained 列表：正 key 只存 key、tool_chain 合成 ref 全身份逐字节 + fullBoundary）。
// 滚动 supersede：写前查 prior（限定 compaction=v1 代际标记，legacy 固化物不删不选）、
// 写后 DeleteEvent(prior)。事件自身 Timestamp=写入时刻（非综述 minTs，D4）。仅真折叠发射
// （under-budget 轮 BuildCompactionPayload 返回 ok=false）。StoreEvent 失败 ERROR 留痕并
// 返回通知（不阻断当轮装配）；supersede 失败仅 ERROR（下轮再补）。返回 nil = 成功/跳过。
func (cm *ContextManager) emitCompactionEvent(retained []memory.EventReference) *model.Message {
	if cm.memStore == nil || cm.contextCompressor == nil {
		return nil
	}
	payload, ok := compress.BuildCompactionPayload(retained, cm.contextCompressor.FullBoundary())
	if !ok {
		return nil // no real fold this round (under-budget / no new summary ref)
	}
	raw, err := payload.MarshalPayload()
	if err != nil {
		log.Errorf("[emitCompactionEvent] payload marshal failed: %v", err)
		return nil
	}
	// Supersede order (D4): query prior BEFORE writing — after the write a
	// timestamp_desc query would find the just-written event and delete it.
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
		Timestamp:    time.Now().UnixMilli(), // write time (D4), never the summary minTs
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

// latestCompactionKey returns the newest marker-tagged compaction event key
// (0 = none). QueryEvents cannot filter on Metadata, so this walks a short
// timestamp_desc window and checks the generation marker via GetEvent —
// legacy 固化物 (same type, no marker, TTL-immortal) is never selected and
// therefore never superseded/deleted (fresh-eyes D).
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
		// Agent identity check (review 🟠2): PartitionIDFromName is a 10-bit
		// FNV hash (collisions expected at ~38 agents) — without this check a
		// colliding agent's latest compaction event would be supersede-deleted
		// cross-agent, silently dropping that agent's rebuild snapshot.
		if evt.Metadata[compress.CompactionMetaKey] == compress.CompactionGenV1 &&
			evt.Metadata[tagentevent.MetaKeyAgentName] == cm.name {
			return r.EventKey
		}
	}
	return 0
}

// persistTaskRecord（R2，resident-continuity-r2-r4 1.6）：记录-only 持久化——只写
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
		return // no declarative → not replayable, no record (best-effort)
	}
	decl := *tk.Spec.Declarative
	if decl.StartedAtMilli == 0 {
		decl.StartedAtMilli = tk.StartedAt.UnixMilli()
	}
	// hardening-review-batch2 1.1（世系跨重启保真）：Declarative 构造点
	// （tool_agent / SpecFromDeclarative）不携带运行态 Spec.Origin——框架
	// OriginSpawner 在 Spawn 入口 stamp 到 spec 上，此处（OnSpawn hook，晚于
	// stamp）单点补填，保证 task_spawned 持久化记录携带 routing baggage。
	// 声明式字段已填 Origin 时以声明式为准（不覆盖）。
	if len(decl.Origin) == 0 && len(tk.Spec.Origin) > 0 {
		cp := make(map[string]string, len(tk.Spec.Origin))
		for k, v := range tk.Spec.Origin {
			cp[k] = v
		}
		decl.Origin = cp
	}
	// hardening-review-batch2 2.4：lifetime 类持久化——恢复侧据它还原同一
	// 生命周期分类（job 受 stale/deadline 治理；service 永不因年龄终止）。
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

// EmitTaskCancelledRecord（R2，review 终审🔴）：Cancel 终态的事实链记录
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
	// R4 review 🟡10：与 background 路径词汇归一——SettleStable 在 background
	// 侧记 "alive-detached"（detached 转换发生在 emitBackground），inline 侧
	// tk.Status() 仍是 running/stable；归一后恢复时 aliveDetached 抑制标志
	// 语义一致（避免恢复后多发一次 ready 通知）。
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
	// Defense-in-depth for §3.1: the receive boundary rejects an external_input with a
	// nil Message, so a nil here means a non-input or a caller bug. Returning a zero
	// fact (EventKey==0) lets the §3.5 completeness gate drop it rather than panic.
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
		// §4.3 / §3.4-deferred: freeze multimodal parts so an image-only (empty-text)
		// input is retained losslessly in the fact chain and re-rendered to the request.
		ContentParts: msg.ContentParts,
		// §3.4: freeze the message's tool fields onto the canonical fact via the
		// existing FullEvent payload fields — previously only Content was kept, so
		// a tool-bearing input fact lost its calls and could not be rebuilt.
		ToolCalls: msg.ToolCalls,
		ToolID:    msg.ToolID,
	}
	// 归因盖章（TC0，路径2/2）：与插件管线 onEvent 同盖，避免归因盲区（报告 R5）。
	// 基线盖 agent_name + trigger_source + rollout_id（sessionID）；bundle_id 属会话级
	// 版本上下文（非 turn 锚），D1-B（design-report-closeout）起双路径同盖。
	// M9（§8.4）设计边界（非缺陷）：persistBusEvent 处理 bus 回流的**系统注入消息**（如
	// action_tool_result，见上 RoleSystem→RoleUser 转换），非 RunFlow 的 LLM 调用产出——不属
	// 单一 turn span，故**不注入** turn trace 锚（trace_id/span_id 是 RunFlow 主路径经
	// Attribution 的职责）。其溯源经 rollout_id(sessionID) + trigger_source 达成（关联到会话与
	// 触发源，足够审计）；强加 turn span 锚反而会错误归属到无关 turn。
	fullEvent.Metadata = map[string]string{
		tagentevent.MetaKeyAgentName:     cm.name,
		tagentevent.MetaKeyTriggerSource: cm.triggerSource,
	}
	// R2（resident-continuity-r2-r4 1.5）：task 来源事件的结构化 settle 键拷入
	// FullEvent.Metadata——事实链 settle 记录可被 RebuildTaskRegistry 机器辨读
	// （task_id/settle_status；task_inline_record 标记内联终态记录，event 包非投影判定排除）。
	if evt.Source == SourceTask {
		for _, k := range []string{"task_id", "settle_status", tagentevent.MetaKeyTaskInlineRecord} {
			if v, ok := evt.Metadata[k]; ok {
				fullEvent.Metadata[k] = fmt.Sprint(v)
			}
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
	// Durable-input identity (fix-resident-reliability-boundaries D2 L67, task 4.0a):
	// a claim-bearing event freezes its batch identity onto the canonical fact so
	// the fact chain is SELF-identifying. buildBusFact is called by both the write-
	// before prepare barrier (prepareBatchFacts freezes this as prepared_fact)
	// and the volatile path; the claim is only set on the durable path, so these
	// keys land on the frozen fact and survive a restart. This is the prerequisite
	// for 4.6's startup reconcile to check a fact directly by receipt_key without
	// a projection/tail scan. The slot index is never compacted (F4), so (request
	// id, slot) uniquely names one input fact.
	if evt.claim != nil {
		fullEvent.Metadata[tagentevent.MetaKeyInboxRequestID] = evt.claim.RequestID
		fullEvent.Metadata[tagentevent.MetaKeyInboxSlot] = fmt.Sprintf("%d", evt.claim.Slot)
		if evt.ID != "" {
			fullEvent.Metadata[tagentevent.MetaKeySourceEventID] = evt.ID
		}
	}
	// §3.4: freeze the ORIGINAL source and the complete business Metadata as an
	// exact JSON snapshot under one reserved control key (event.MetaKeySourceSnapshot).
	// This preserves the input's provenance back to its host (chat_id, task
	// genealogy, ...) across a restart WITHOUT spreading arbitrary business keys
	// across the trusted control namespace — they live wholly inside this snapshot.
	if snap, err := tagentevent.EncodeSourceSnapshot(evt.Source, evt.Metadata); err != nil {
		log.Warnf("[buildBusFact] source snapshot encode failed src=%s: %v", evt.Source, err)
	} else if snap != "" {
		fullEvent.Metadata[tagentevent.MetaKeySourceSnapshot] = snap
	}
	return fullEvent
}

// isCompletePreparedFact checks the one invariant that unambiguously separates a
// genuinely-prepared canonical fact from corrupt/truncated material (§3.5, design
// 决策2「完整准备校验」): a fixed non-zero Snowflake identity key. buildBusFact
// always stamps a real key and never 0, so a decoded fact with EventKey == 0 is
// not a real prepared fact and persistBusEvent gates the store rather than minting
// a fresh key. Summary/attribution are intentionally NOT re-checked at decode: they
// are guaranteed at construction, and re-checking them here would risk false-gating
// legitimate durable facts (e.g. an unnamed ContextManager in fixtures).
func isCompletePreparedFact(f *memory.FullEvent) bool {
	return f != nil && f.EventKey != 0
}

// persistBusEvent persists an EventBus event to MemoryStore and appends it
// to the compress.SessionProjection immediately. This ensures that all messages
// visible to the LLM are also tracked in the projection — eliminating the
// "visible but not projected" state that caused ordering bugs.
//
// The event is stored as a FullEvent with:
//   - EventKey: Snowflake-generated (using ContextManager's partitionID)
//   - EventType: inferred from message role
//   - Content/EventSummary: from the AgentEvent's Message payload
//
// F2 fix: returns true when the fact is stored OR was already stored (replay
// dedup path). Returns false only when StoreEvent failed and projection append
// was gated. The §4.2 submit gate maps a false to submitTransient (no model, claims
// requeued), so §4.4's cm.turnEcho — and thus the MemoryPlugin echo skip — is installed
// ONLY once every selected fact is committed; otherwise the plugin would skip an input
// that was never durably stored and the input would be lost.
func (cm *ContextManager) persistBusEvent(evt *AgentEvent) bool {
	ok, _ := cm.persistBusEventCommitted(evt)
	return ok
}

// persistBusEventCommitted is the classified form of the commit step. The
// second result reports a DETERMINISTIC conflict — the typed same-key
// different-content collision or a forgotten tomb — which the commit protocol
// must ISOLATE rather than retry: a frozen key that collides with different
// content (cross-process snowflake collision included) conflicts on EVERY
// replay, so retrying it forever is a livelock. §8.5's 30-restart cadence
// proved the surface real; the leaf comment 「dispositioning is layered on by
// the commit protocol» is layered on HERE.
func (cm *ContextManager) persistBusEventCommitted(evt *AgentEvent) (stored, deterministic bool) {
	if evt == nil || evt.Message == nil {
		return false, false
	}

	msg := *evt.Message
	// Convert RoleSystem → RoleUser: system-injected messages (e.g.,
	// [action_tool_result]) should be treated as external input by the LLM.
	if msg.Role == model.RoleSystem {
		msg.Role = model.RoleUser
	}

	// Resolve the canonical fact for this event (fix-resident-reliability-
	// boundaries D2/D3 + task 3.5):
	//   - Durable claim WITH a frozen prepared_fact (normal reliable path):
	//     reuse it VERBATIM. Its EventKey, summary and attribution were frozen
	//     by the write-before prepare barrier on the first claim, so a replay
	//     never re-stamps time/rollout/bundle or double-writes the fact (F1/F4).
	//     If those bytes are undecodable OR incomplete it is current-format
	//     corruption: the store is GATED (return false) and surfaces — the
	//     regenerate-key weak fallback is DELETED (§3.5「删除重新生成 key 的
	//     恢复分支」); minting a fresh key would silently double-write the input
	//     under a new identity. The claim stays and replays.
	//   - Durable claim but NO prepared_fact: the barrier did not succeed for
	//     this envelope, so per D2「准备失败不调用 StoreEvent」we write NOTHING and
	//     return false — the claim stays and replays; a half-prepared input
	//     never silently enters the fact chain.
	//   - No claim (volatile path / direct test call): build a fresh canonical
	//     fact, exactly as before.
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
	// §4.6 (event-sourced-projection L7): the projection ref is built from the CANONICAL
	// fact the store holds/returned — never the current event's time, a re-derived summary,
	// or the original call object. Default to the freshly built fullEvent (volatile path);
	// the durable branch overwrites it with the canonical ReplayEvent returned.
	refCanonical := fullEvent

	// Stored-gate (event-sourced-projection D2, fresh-eyes E①): a failed
	// StoreEvent must NOT append to the projection — otherwise the projection
	// holds a ref the fact chain lacks and the rebuild invariant (projection =
	// fold of the fact chain) breaks. The spill path (ErrorTrackingStore →
	// ReplaySpilled dual-write) re-stores AND re-appends this event on
	// recovery, restoring the same-point semantics. nil store (test/bypass
	// scenarios) keeps the previous always-append behavior.
	stored = true /* named returns */
	// replayed marks a durable claim whose frozen canonical fact is ALREADY fully
	// committed on the chain (the pre-crash pass stored it, or the startup rebuild
	// restored it). Such a claim is a no-op: neither re-store NOR re-append NOR
	// re-feedback — otherwise the projection would hold a ref the fact chain counts
	// once and drift from it (F4「投影=事实链折叠」invariant) and the settle feedback
	// would duplicate (D3 step3). It is classified from the ReplayResult, NOT a
	// GetEvent probe: D4 forbids the weak "GetEvent success ⇒ done" degradation,
	// and the 3.5 gate guarantees a durable store is an EventReplayer.
	// A fresh volatile key is unique, so replayed stays false on the volatile path.
	replayed := false
	switch {
	case cm.memStore == nil:
		// Test/bypass scenario (nil store): keep the historical always-append behavior.
	case evt.claim != nil:
		// Durable commit path (fix-resident-reliability-boundaries D3 step1 / D4):
		// go through the internal replay interface, which content-checks the frozen
		// fact and distinguishes fresh commit / half-orphan repair / idempotent
		// already-committed / typed same-key-different-content conflict.
		replayer, ok := cm.memStore.(memory.EventReplayer)
		if !ok {
			stored = false
			log.Errorf("[persistBusEvent] durable store %T is not replay-capable rid=%s slot=%d — commit gated, claim will replay",
				cm.memStore, evt.claim.RequestID, evt.claim.Slot)
			break
		}
		result, canonicalOut, err := replayer.ReplayEvent(eventKey, fullEvent)
		if err == nil {
			// Authoritative stored bytes (fresh OR already-committed): the projection-ref source.
			refCanonical = canonicalOut
		}
		switch {
		case err == nil && result == memory.ReplayAlreadyCommitted:
			replayed = true
		case err == nil:
			// ReplayNew / ReplayRepaired: fact now durable under the frozen key.
		case memory.IsDuplicateEventKey(err):
			// Typed conflict — same key, DIFFERENT content. Never swallow it as a
			// replay (D2 L68): hold the claim, do not ack, surface it. Dispositioning
			// (isolate / completion=failed) is layered on by the commit protocol.
			stored = false
			deterministic = true
			log.Errorf("[persistBusEvent] CONFLICT rid=%s slot=%d key=%d (same key, different content) — claim held, envelope not acked: %v",
				evt.claim.RequestID, evt.claim.Slot, eventKey, err)
		case memory.IsEventForgotten(err):
			// 2.4: the fact is legally tombstoned — a replay MUST NOT resurrect it.
			// Hold the claim (never ack, never re-project) and surface it, exactly like a
			// conflict; the commit protocol dispositions it (isolate / completion=failed).
			stored = false
			deterministic = true
			log.Errorf("[persistBusEvent] FORGOTTEN rid=%s slot=%d key=%d (legally tombstoned) — claim held, not re-projected: %v",
				evt.claim.RequestID, evt.claim.Slot, eventKey, err)
		default:
			// Transient I/O / barrier failure: fact NOT durable this turn.
			stored = false
			log.Errorf("[persistBusEvent] ReplayEvent failed key=%d (append gated, replay will retry): %v", eventKey, err)
		}
	default:
		// Volatile path (no claim): unchanged — a fresh snowflake key is unique.
		if err := cm.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			stored = false
			log.Errorf("[persistBusEvent] StoreEvent failed key=%d (append gated, spill recovery will restore): %v", eventKey, err)
		}
	}

	// §4.6: the projection ref comes from the returned/written canonical — role derived
	// from the canonical EventType via the SAME fold rule the cold-start rebuild uses
	// (removing the old runtime-vs-rebuild role deviation), time from the frozen canonical,
	// never the current event's arrival time or the original message object.
	ref := memory.EventReference{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    refCanonical.EventType,
		EventSummary: refCanonical.EventSummary,
		Timestamp:    refCanonical.Timestamp,
		Role:         string(tagentevent.EventTypeRole(refCanonical.EventType)),
	}
	// R3（backlog-final-closeout）：projection nil 防御——测试/旁路场景（溢出登记）构造
	// 裸 cm 时不崩溃（零值鲁棒性；主路径恒有投影，行为不变）。
	// §4.6 (scenario「已有事实不等于当前请求已包含」): a SELECTED outstanding input whose
	// frozen fact is already committed (replayed) STILL must be present in the projection
	// before execution — the cold-start snapshot/tail scan may not have restored its older
	// key. SessionProjection.Append is EventKey-idempotent, so re-asserting a rebuild-restored
	// ref is a no-op while a missing one is backfilled; scoping to this function's callers
	// (the selected batch only) keeps it from re-appending unrelated historical keys. The
	// never-store-fails-append invariant holds: a store failure (stored=false, !replayed) skips.
	// §5.5: the same event-package predicate the spill-replay and cold-start paths use
	// guards the normal commit append — an internal record (receipt/registry/inline
	// settle/compaction body) must never occupy the projection even if it reaches here.
	if cm.projection != nil && (stored || replayed) && !tagentevent.IsNonProjectionRecord(refCanonical.EventType, refCanonical.Metadata) {
		cm.projection.Append(ref)
	}
	if replayed {
		log.Infof("[persistBusEvent] replay dedup rid=%s slot=%d key=%d already on chain — ref ensured, re-store+feedback skipped",
			evt.claim.RequestID, evt.claim.Slot, eventKey)
		return true, false
	}

	// 2.3（design-report-closeout）：OnSettle 自动反馈——task_settled 事件落库后，
	// 确定性 settle 裁决自动绑定 feedback（parent=task_settled 事件自身：结算记录
	// 即任务产出，bundle_id 章使其可归因到 active bundle；suspect/stable 不写）。
	// KNOWN WINDOW（review 🟡3）：StoreEvent 失败时 writeSettleFeedback 的
	// BindFeedback 会因 parent 未落库而丢弃该次裁决（spill 恢复只补事件本体，
	// 不补 feedback）——与改动前行为一致，非回归；如需消除可将本分支纳入
	// stored 门控（spill 恢复时补裁决），另立小变更。
	if evt.Source == SourceTask {
		cm.writeSettleFeedback(eventKey, evt.Metadata)
	}

	log.Infof("[persistBusEvent] persisted bus event key=%d type=%s source=%s content=%s",
		eventKey, eventType, evt.Source, truncateForLog(msg.Content, 80))
	return stored, deterministic // F2: caller gates the submit outcome (no §4.4 echo credential on failure)
}

// commitReceiptFact durably submits a frozen inbox-receipt fact under its RESERVED
// key (§5.3, design 决策5 L63/L122). Unlike the old persistInboxReceipt — which
// minted a FRESH snowflake key and stamped time.Now() on every call, so a retry or
// restart produced a DIFFERENT, non-idempotent receipt — this commits the exact
// frozen bytes the completion carries through the explicit replay interface, so a
// re-submit of an already-committed receipt converges to ReplayAlreadyCommitted
// (never a second receipt event) and a same-key/different-content collision
// surfaces as an error. The receipt is an internal processing record: it is
// deliberately NOT appended to the projection (it never feeds model context),
// matching the old receipt path and §5.5's non-projection classification.
// §5.7: it returns the TYPED error (nil = committed OR already-committed, both
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

// writeSettleFeedback（2.3 design-report-closeout）把确定性任务裁决写为 feedback
// 事件（因果边指向 task_settled 事件）。completed→positive / failed→negative；
// suspect/alive-detached/未知状态不写（只记确定性裁决，防噪声污染 guardrail）。
// 失败仅记日志（反馈是旁路产物，不阻塞主链路）。
//
// §2.7④ 不变量：feedback **不是** durable 提交/ack 凭据。本函数仅在 `stored` 已为 true
// （事实链已 durable）且投影已 append 之后运行，位于 ack 下游；BindFeedback 失败不撤销
// 提交、不影响 `stored` 返回值（F2：caller 据此决定 ack / §4.4 echo 凭据安装），也不影响可靠
// inbox 的 durable 判定（其凭据是 PublishReceipt.Durable，与 feedback 无关）。guardrail 的
// negative_feedback_rate 只作行为信号，绝不作输入确认/重放凭据。
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
		return // suspect / alive-detached / unknown: no deterministic verdict
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
// never fire (event-sourced-projection D3; previously it only lived on
// the in-memory StateDelta).
func (cm *ContextManager) buildTurnAttribution(ctx context.Context) plugin.Attribution {
	attr := plugin.Attribution{}
	if cm.sessionID != "" {
		attr[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	if traceID, spanID := spanTraceIDs(ctx); traceID != "" {
		attr[tagentevent.MetaKeyTraceID] = traceID
		attr[tagentevent.MetaKeySpanID] = spanID
	}
	// D1-B (design-report-closeout): stamp the active bundle id so every
	// produced event attributes to the exact prompt/config version.
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

// RunFlow calls runner.Run and forwards events to outputCh. Delivery only:
// projection writes happen in the event-plugin pipeline (ProjectionSink), and
// the loop waits for the next turn via bus.Pull — there is no bus echo.
// echoSpec is the turn-level, immutable template runEventLoop installs when this
// turn's durable input facts are already committed (§4.4). RunFlow clones it into a
// fresh per-attempt plugin.EchoCredential (unique token), so there is no shared mutable
// whole-turn state — the derived credential owns its own bind state. No mutex here.
type echoSpec struct {
	agent         string
	session       string
	mergedMessage string
	committedKeys []int64
}

// newAttemptEchoCredential stamps a unique per-runner-attempt credential from the
// turn's echo spec (§4.4). A retry of the same business turn calls this again → a new
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

// turnEchoVerified reports the §4.5 execution-credential outcome of the most recent
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

func (cm *ContextManager) RunFlow(ctx context.Context, msg model.Message) error {
	// Runner lifecycle accounting (implementation-hardening 5.1): this turn
	// holds a runner reference — the counter gates retired-runner sweeps so a
	// swapped-out runner is only closed after the last turn holding it ends
	// (drain-free swap semantics: “切” must be paired with “尾”).
	cm.runnerInFlight.Add(1)
	defer func() {
		cm.runnerInFlight.Add(-1)
		cm.sweepRetiredRunners()
	}()
	// Bind this invocation's projection as the pipeline projection sink:
	// MemoryPlugin projects each stored event at the same synchronous point
	// (write unification, unified-event-projection D1).
	if cm.turnEcho != nil {
		// §4.4: mint a fresh per-attempt echo credential (unique request token) so the
		// MemoryPlugin identifies THIS attempt's input echo precisely — root invocation,
		// author=user, and the exact committed merged message — instead of skipping every
		// user event in a whole-turn "first envelope" mode. Released when this ctx ends.
		// §4.5: also retained on cm so the loop can fail-closed if the model-entry gate
		// never saw a verified echo (framework fed a non-matching/absent input, or a
		// swallowed plugin error downgraded it).
		cred := cm.newAttemptEchoCredential()
		cm.lastEchoCred = cred
		ctx = plugin.WithEchoCredential(ctx, cred)
	}
	if cm.projection != nil {
		ctx = plugin.WithProjectionSink(ctx, cm.projection)
		// 归因章注入（TC0 路径1/2 + T-B trace 关联）：rollout_id + turn span 的 trace_id/span_id
		// → 事件 Metadata 携带 trace 锚，使事件溯源 / trajectory / OTel span 三投影由同一 id
		// 双向互链（指令2「一套数据模式、多场景投影、保一致性」）。空归因不注入。
		ctx = plugin.WithAttribution(ctx, cm.buildTurnAttribution(ctx))
	}
	// Inject the task spawner so tools can delegate long-running work to the
	// task layer (sync-wait window → inline or ack). Absent → synchronous.
	if cm.taskController != nil {
		// Wrap the spawner to snapshot the originating turn's invocation
		// metadata (chat_id, ...) as opaque origin baggage on each spawned
		// task, so a background settle can be routed back to the originating
		// session. The task layer never interprets it. (async-result-delivery.)
		var spawner task.TaskSpawner = cm.taskController
		// Origin baggage = invocation metadata (chat_id, ...) + T-B turn trace 锚点
		// (trace_id/span_id)。后者使异步 task_settled 事件经 Origin→Metadata 管道携带触发
		// 它的 turn 的 trace 锚点——指令2「一套数据模式多场景保一致性」延伸到异步任务链路：
		// task settle 回流的新 turn 可关联回原 trace（异步链路可追溯），复用现有 Origin 管道
		// 零新结构、零 task 包侵入。
		md := cm.GetInvocationMetadata()
		traceID, spanID := spanTraceIDs(ctx)
		// meditation-leak-r2 (2026-09-15): lineage WRITE side. The turn's resolved
		// trigger source rides the Origin baggage, so the async task_settled event
		// (event_bus.go copies Origin -> Metadata) re-arms extractTriggerSource's
		// lineage branch in the reclaim turn. Without this capture the read side
		// (e2195fc) never fires: meditation-spawned tasks settle as bare "task"
		// triggers and their outputs leak to lastActiveChat.
		ts := cm.triggerSource
		if len(md) > 0 || traceID != "" || ts != "" {
			cp := make(map[string]string, len(md)+3)
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
			spawner = &task.OriginSpawner{TaskController: cm.taskController, Origin: cp}
		}
		ctx = task.WithTaskSpawner(ctx, spawner)
	}
	eventCh, err := cm.currentRunner().Run(ctx, cm.userID, cm.sessionID, msg)
	if err != nil {
		// §5.1: a runner start/transport error is a definite failure of THIS
		// attempt. Record it so the loop can tell a real failure from a nil return
		// even on the last retry, rather than falling through to "completed".
		startErr := fmt.Errorf("runner.Run: %w", err)
		cm.lastTurnOutcome = reduceTurnOutcome(startErr, "", false)
		return startErr
	}

	cm.turnProductive = false
	// §5.1: response-internal error captured from the stream. The framework
	// surfaces a model/API failure as an event carrying Response.Error (it does
	// NOT make RunFlow return an error), so the old code — which never inspected
	// Response.Error — mistook such a turn for success and ACKed its durable
	// inputs. Capture the first such error to reduce the attempt honestly.
	var respErr string
	for fwEvt := range eventCh {
		if fwEvt == nil {
			continue
		}
		// Clone before any tagent-side mutation: the framework retains the
		// original event for its own post-processing reads (e.g. content
		// snapshot cloning on the runner goroutine), so writing StateDelta on
		// the shared object would be a data race. All tagent-side metadata
		// (trigger_source, meta_*) goes onto this private copy, which is what
		// onEvent and outputCh consumers observe.
		evt := cloneEventForDelivery(fwEvt)
		if cm.triggerSource != "" {
			// Attach trigger source to the event for deterministic
			// consumer-side dispatch (meditation vs task vs user).
			evt.StateDelta[tagentevent.MetaKeyTriggerSource] = []byte(cm.triggerSource)
		}
		// Track turn productivity: a turn that never calls a tool and ends in
		// an empty final produced nothing (occasional model hiccup) — the
		// persistent loop retries such a turn once (see runEventLoop).
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
			if len(m.ToolCalls) > 0 || (isFinalResponse(evt) && strings.TrimSpace(m.Content) != "") {
				cm.turnProductive = true
			}
		}
		// §5.1: a response-internal model/framework error arrives with a nil
		// transport return; capture it once (bounded) so the turn reduces to
		// failed rather than being mistaken for completion.
		if evt.Response != nil && evt.Response.Error != nil && respErr == "" {
			respErr = fmt.Sprintf("%s: %s", evt.Response.Error.Type, evt.Response.Error.Message)
		}
		if cm.onEvent != nil {
			cm.onEvent(evt)
		}
		if cm.outputCh != nil {
			// F2 (design-report-closeout): 2s grace → persist + ticket, never
			// block the loop on a stalled consumer.
			if !cm.deliverEvent(ctx, evt) && ctx.Err() != nil {
				// §5.1: a mid-drain shutdown cancellation previously returned nil,
				// which the loop read as "completed" and then ACKed — dropping the
				// claims of a turn that reached no terminal state. Classify it as
				// cancelled (spec L90/L130: no completion, claim retained) and
				// surface the ctx error so the loop stops rather than acking.
				cm.lastTurnOutcome = reduceTurnOutcome(ctx.Err(), respErr, true)
				return ctx.Err()
			}
		}
	}
	cm.lastTurnOutcome = reduceTurnOutcome(nil, respErr, false)
	return nil
}

// LastTurnOutcome returns the §5.1 reduced terminal state of the most recent
// RunFlow attempt. The persistent loop uses it to distinguish a genuinely
// completed turn from a response-error failure or a shutdown cancellation that
// the nil transport return used to hide.
func (cm *ContextManager) LastTurnOutcome() turnOutcome {
	return cm.lastTurnOutcome
}

// setLastBatchOutcome records the loop's §5.1 reduced terminal state for the
// most recent turn (called only on the loop goroutine after the retry budget).
func (cm *ContextManager) setLastBatchOutcome(o turnOutcome) { cm.lastBatchOutcome = o }

// LastBatchOutcome returns the §5.1 reduced turn result the persistent loop
// computed for the most recent durable batch, prior to §5.2/§5.3 freezing it
// into the completion. Completed/failed only — a cancelled turn returns from the
// loop without reaching the freeze, so no completion is ever formed for it.
func (cm *ContextManager) LastBatchOutcome() turnOutcome { return cm.lastBatchOutcome }

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
// results) — the board bytes change every call (task ages), so only the
// tail position keeps the prompt-cache prefix intact. The taskController
// nil-check is at CALL time, not registration time: taskController is wired
// AFTER ContextManager construction (agent.go), so a registration-time guard
// would permanently skip the board. The board is ephemeral (request-only,
// never projected/compressed).
// (async-result-delivery: task-board-injection-order fix; 2026-08-27 tail
// reposition for prefix-cache stability.)
func (cm *ContextManager) injectLiveTaskBoard(args *model.BeforeModelArgs) {
	if cm.taskController == nil {
		return
	}
	if board := task.RenderBoard(cm.taskController.List(), cm.taskController.DefaultTTL()); board != "" {
		args.Request.Messages = task.InjectBoard(args.Request.Messages, board)
	}
}

// SetUserIDSessionID updates the user/session context for runner.Run.
func (cm *ContextManager) SetUserIDSessionID(userID, sessionID string) {
	cm.userID = userID
	cm.sessionID = sessionID
}

// Close releases the runner resources.
func (cm *ContextManager) Close() error {
	// Terminal: drain retired runners unconditionally — no new turns will run
	// after Close, so the in-flight gate no longer applies.
	cm.retireMu.Lock()
	for _, e := range cm.retiredRunners {
		_ = e.r.Close() // upstream documents Close as idempotent
	}
	cm.retiredRunners = nil
	cm.retireMu.Unlock()
	if r, ok := cm.currentRunner().(interface{ Close() error }); ok {
		return r.Close()
	}
	return nil
}

// WaitForInFlight blocks until every runner turn currently streaming (resident
// loop turns AND one-shot/sub-call turns — all enter via RunFlow, which
// counts runnerInFlight) has drained, or the timeout passes; false means turns
// were still active. The §6.2 close sequence uses it so the output channel is
// never settled while a turn may still send to it (review M-1).
func (cm *ContextManager) WaitForInFlight(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for cm.runnerInFlight.Load() > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func ensureUserPrompt(messages []model.Message) []model.Message {
	for _, msg := range messages {
		if msg.Role == model.RoleUser {
			return messages
		}
	}
	// No user message found — add a neutral prompt to trigger model response.
	// Don't say "如果有新任务" which misleads the LLM into thinking previous tasks are done.
	return append(messages, model.Message{
		Role:    model.RoleUser,
		Content: "请基于以上上下文继续处理。",
	})
}

func isFinalResponse(evt *event.Event) bool {
	if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return false
	}
	choice := evt.Response.Choices[len(evt.Response.Choices)-1]
	// Only an assistant message without tool_calls is a final response.
	// A tool RESULT (Role=tool) also has no tool_calls but must NOT be
	// treated as final — that would cause premature turn termination in
	// the sub-agent path.
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
