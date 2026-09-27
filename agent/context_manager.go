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

	// hotswap-fix 5.7 / introduce-durable-workflow-engine §2.1：executor 装配所需
	// 的状态面引用与**已发布执行面快照**。NewContextManager 从 cfg 快照；每次
	// PublishExecutor 更新为该代真实执行面——快照即「当前生效绑定」，供诊断与
	// 「原样重发同一执行面」使用，不再是候选构造的隐式回落源（旧 RebuildExecutor
	// 的零值合并会把清空字段退回上一份，属主换代后旧绑定残留）。
	memPlugin    *plugin.MemoryPlugin
	sessionSvc   session.Service
	execCfg      ContextManagerConfig // 已发布执行面快照（含全部字段）
	tokenCounter compress.TokenCounter
	memStore     memory.MemoryStore
	// Neither maxTokens nor thresholdPct is stored on ContextManager: the real
	// consumer is compress.ContextCompressor, which resolves both at every
	// Compress/BudgetLine boundary via liveNums — the hot SOURCE (this manager's
	// owner record view) wins and the atomics answer only as the construction
	// fallback (§6.4 pull; the ApplyHotParams push and its hot-update path are
	// gone). Earlier construction-only mirror fields were never read (maxTokens)
	// or were a background-write vs read race once hot-apply moved to the rebuild
	// goroutine (thresholdPct) — both removed under §2.4/D4 M-3 「修到实际消费者」.

	// Framework integration
	runner     runner.Runner
	executorMu sync.RWMutex // R4（resident-continuity-r2-r4 3.3）：runner 可换缝守护——发布（写：PublishExecutor / ActivateExecutor）vs RunFlow per-turn RLock（读）

	// Per-generation execution leases (§3.2/§4.1, design D6). `active` is the
	// binding of the runner currently in force; every retired binding that still
	// has references stays in retiredBindings until its OWN count drops (never a
	// shared aggregate gate). Guarded by executorMu, except each binding's own
	// reference set, which is guarded by the binding. See agent/exec_lease.go.
	active          *execBinding
	retiredMu       sync.Mutex
	retiredBindings map[*execBinding]struct{}
	bindingSeq      atomic.Int64
	// retirementPoke is the §4.3 drain-forward notification armed by the
	// composition root (see SetRetirementPoke); nil = standalone agent.
	retirementPoke atomic.Pointer[func()]
	// drainedHook/drainedOnce carry §4.1's deferred final exit: armed by an
	// owner whose bounded Close could not finish because an execution never
	// confirmed a stop, fired at most once when the last held generation is
	// reclaimed with nothing referencing it (see armFullyDrained).
	drainedHook atomic.Pointer[func()]
	drainedOnce sync.Once
	name        string
	userID      string
	sessionID   string

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
	//
	// D3 (§2.3 调度裁决): in production this is the NON-BLOCKING business-turn
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
	// stamp it into FullEvent.Metadata (bundle_id, D1-B design-report-closeout)
	// so guardrail/feedback aggregation can join events to bundle versions
	// precisely. nil-safe.
	bundleIDFn func() string

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

// ApplyOrgParams / ApplyOrgHotParams are GONE (§6.4 pull, S-E). They were the
// push entries that hot-swapped the org numeric bundle into this manager's
// compressor. Their successor is the compressor's own hot source
// (ContextManagerConfig.HotNumbersSource, wired to the owner's live view), read
// at every consumption boundary — so a manager needs no write at all to stay
// current, and a manager constructed mid-flight cannot fall behind a generation.
// OrgBudgetLine / OrgKeepRecent below stay as the read face.

// OrgBudgetLine returns this manager's compressor effective trigger line
// (maxTokens × threshold) — valid for BOTH the resident manager and an
// invocation-private one (§6.4 acceptance reads live calls through it).
// 0 when no compressor is wired.
func (cm *ContextManager) OrgBudgetLine() int {
	if cm == nil || cm.contextCompressor == nil {
		return 0
	}
	return cm.contextCompressor.BudgetLine()
}

// OrgKeepRecent returns this manager's live keepRecent value (§6.4, same
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
// turns instead use the non-blocking lazy trigger via BeginTurn (D3 §2.3). There
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

func (cm *ContextManager) SetOrgReloader(fn func()) {
	cm.orgReloader = fn
}

// SetOrgReloadSyncCheck arms the synchronous ops reload entry (D3 §2.3). The
// tagent layer wires it to the same reload the background builder runs, so ops
// blocks for its result while turns stay non-blocking.
func (cm *ContextManager) SetOrgReloadSyncCheck(fn func()) {
	cm.orgReloadSync = fn
}

// BeginTurn is the ONE place a business turn takes its organization execution
// binding (introduce-durable-workflow-engine §3.1/§3.2, spec
// swappable-executor「触发时机与观测」): the armed org-config check runs first
// (same entry the ops hook CheckOrgReload uses — one publish path, no second
// effective route), then the executor in force is handed to the turn to pin.
//
// Call it after the input batch is frozen and OUTSIDE the transport-retry loop:
// every attempt, model iteration and tool round of that turn then runs on the
// returned runner (RunFlowWithExecutor), so a publication happening mid-turn
// cannot split the turn across generations. Sub-agent invocations do not call
// this: their instances, executor and delegation tree were constructed inside
// the generation that published them.
//
// The returned release MUST run when the turn ends (the loop folds it into its
// per-turn cleanup alongside endTurnSpan). §2.3「acquire 后立即登记」: the
// in-flight reference is registered BEFORE the executor is handed out, so a
// publish and its retire-sweep landing in the gap between handing out the
// executor and entering the run body cannot close the very runner this turn is
// about to run. §4.1 removed the second, aggregate counter RunFlow used to keep:
// a business turn is now registered EXACTLY ONCE, on its own generation.
func (cm *ContextManager) BeginTurn() (runner.Runner, func()) {
	lease := cm.BeginTurnLease()
	return lease.Runner(), lease.Release
}

// BeginTurnLease is BeginTurn with the reference handle exposed, so the turn can
// publish its lease into the call-chain context and every derived execution
// (nested delegation, transport retry, post-ACK background run) adds its own
// reference on the SAME generation instead of re-reading whatever is published
// later (§3.2「子 Run 与重试继承」, D5).
func (cm *ContextManager) BeginTurnLease() *ExecLease {
	// D3 (§2.3): the business turn fires the NON-BLOCKING lazy trigger only. It
	// merges/schedules a background rebuild and returns immediately; this turn
	// then pins whatever generation is already published. A long parse/build/Close
	// therefore never runs on the turn's thread, and a turn that merely witnessed a
	// config edit keeps serving on the old effective until a LATER turn starts
	// after the publish (spec swappable-executor「懒检测不等待候选构建」).
	if cm.orgReloader != nil {
		cm.orgReloader()
	}
	// §2.3「acquire 后立即登记」: the reference is taken on the generation BEFORE it
	// is handed to the caller, so a publish plus its reclaim landing in the gap
	// cannot close the runner this turn is about to run.
	return cm.AcquireLease(LeaseTurn)
}

// AcquireLease pins the generation NEW work starts on. The pin is retried when the
// generation read here turns out to be retired (§4.2 re-entry with no initiating
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
			// No successor. While a terminal close is still draining, the reference IS
			// registered so that close surfaces ErrExecUnconverged rather than reporting a
			// clean shutdown it did not achieve. Once it has CONVERGED there is nothing left
			// to wait for: registering would re-open an obligation nobody holds and hand the
			// caller a closed runner, so new work is refused (§3.2).
			if b.isClosed() {
				return &ExecLease{b: b, kind: leaseKindNoop, refused: ErrExecClosed}
			}
			return b.acquire(kind)
		}
		b = next
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

	// HotNumbersSource is the §6.4 pull contract: when set, the compressor reads
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
			compress.WithHotSource(cfg.HotNumbersSource),
		)
	}

	// NOTE: the LLMAgent construction lives ONLY in buildExecutor/buildLLMAgent
	// below — the sole fwAgent path, gated by the execution model (§4.5). An
	// earlier inline option block here was dead residue of the hotswap-fix 5.7
	// extraction and misleading (it built a NON-gated option list that no
	// consumer ever used). §7.5: assembly must mirror the executed surface.
	// hotswap-fix 5.7：快照完整 cfg（执行面+状态面）。introduce-durable-workflow-engine
	// §2.1：冷启动与热更换装共用 buildExecutor 这唯一一条装配路径。
	cm.execCfg = cfg
	cm.memPlugin = cfg.MemPlugin
	cm.sessionSvc = cfg.SessionSvc

	cm.runner = cm.buildExecutor(cfg)
	// §3.2: the cold-start executor IS generation 1 — a business turn must be able
	// to hold a reference on it from the first request, so the binding exists at
	// construction rather than being invented lazily on the first swap.
	cm.active = cm.newBinding(cm.runner)

	return cm
}

// buildModelCallbacks（hotswap-fix 5.7）：回调链构造抽为 cm 方法——闭包捕获
// 同一个 cm（装配/热载/任务板/诊断全部同源）。冷启动与候选构造
// （buildExecutor）共用，保证换装后 BeforeModel 闭包仍指向常驻状态面
// （修复空投影装配事故）。
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
			// introduce-durable-workflow-engine §3.1: the org-config check used to run
			// HERE — i.e. before EVERY LLM iteration — so a config edit could change the
			// executor in the middle of a turn (multi-iteration ReAct loops crossed
			// generations). The check now belongs to the turn boundary (BeginTurn),
			// outside the transport-retry loop; a turn never re-reads the active version.
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
// 纯函数段（依赖仅 cfg）。冷启动与候选构造共用（都经 buildExecutor），保证
// 两条路径构造的 fwAgent 行为学一致（model/tools/prompt/genConfig/并行工具开关）。
func (cm *ContextManager) buildLLMAgent(cfg ContextManagerConfig) *llmagent.LLMAgent {
	cb := cm.buildModelCallbacks(cfg.SystemPromptSource)
	maxIters := cfg.MaxToolIters
	if maxIters <= 0 {
		maxIters = DefaultMaxToolIterations
	}
	// §4.5: wrap the model with the execution-credential gate (block the real model call
	// when a durable turn's echo credential is unverified) + recovery-notice deferral to
	// actual invocation. buildLLMAgent is the sole fwAgent construction → covers cold start
	// and the hot candidate path. nil-safe (some constructions carry no model).
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

// SwapExecutor was withdrawn with introduce-durable-workflow-engine §5.1: it
// installed a runner WITHOUT recording the face that runner was built from, so a
// published generation could end up executing one runner while resolving re-entry,
// compressor configuration and tool declarations from another (§2.1/§3.2 leave
// exactly one linearization point per owner: PublishExecutor for a single owner,
// StageExecutor→ActivateExecutor for an org, and both assign runner and face under
// the same executorMu write section). It had no production caller when removed;
// the turn-level drain-free, concurrent-read and resident-invariant semantics it
// pinned are re-pointed at PublishExecutor in agent/executor_publish_seam_test.go.

// publishActiveLocked is the ONE linearization body of an executor switch. It
// records `face` as the published execution configuration, then installs the
// generation running `r` as the active one, and returns the binding that becomes
// retired — which the caller MUST retire once it has dropped executorMu, so a slow
// runner Close never happens under the executor lock.
//
// `prepared` is the binding a staged generation already wired (declaration holds,
// face snapshot). It is non-nil only on the org path, where the binding had to
// exist and be wired BEFORE any owner made it visible; a single-owner publish has
// no such pre-condition and passes nil, so the binding is snapshotted here from
// the face just recorded.
//
// Re-publishing the same executor object is deliberately NOT a new generation: one
// runner object gets exactly one close, so a redundant publish (a rollback landing
// on the same face, a caller re-submitting the current candidate) must not create a
// second identity for it. The recorded face still advances — the binding keeps the
// face it was BUILT from, so advancing what is *recorded* never rewrites what this
// generation actually routes. (§5.3 merged the org activation path onto this same
// body, removing the second copy that used to skip the face advance.)
func (cm *ContextManager) publishActiveLocked(face ContextManagerConfig, r runner.Runner, prepared *execBinding) *execBinding {
	cm.execCfg = face.isolatedCopy() // R06: never alias the caller's or the staged face
	if cm.active != nil && cm.active.run == r {
		return nil
	}
	if cm.active == nil && cm.runner == r {
		// The runner set at construction is being published for the first time:
		// adopting it IS installing it. Minting a second binding for the same object
		// would hand that same runner to retireBinding — closing the executor the
		// next turn is about to run — so no predecessor may be produced here.
		// (§5.3 handed this corner to §5.4 to guard explicitly rather than leave it
		// to callers avoiding the shape.)
		cm.active = cm.newBinding(r)
		return nil
	}
	prev := cm.active
	if prev == nil && cm.runner != nil {
		// Adopt a runner that was set directly on the struct (hand-built cm, test
		// shell) as generation 1, so its in-flight users can still be counted.
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
// face is a private copy (R06): mutating its Tools container or deref-writing
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
// face** (§4.2, task-registry-rebuild「恢复与显式重投按当前编排绑定」) — the very
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
// a routing truth that outlives the version it came from — §4.2 (R03) is exactly
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
// face stays the SINGLE routing truth (§4.2): the peeled wrapper is the very
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

// buildExecutor is the SINGLE executor assembly path (cold start + hot candidate).
// The execution face comes from exec ONLY — no fallback to the previously
// published snapshot, so a field the new config clears really disappears
// (introduce-durable-workflow-engine D3/§2.1: 「不以非零 merge 保留旧绑定」).
// The state face (memory plugin, session service, projection the sub-agent
// wrappers auto-inject from) is always taken from this cm: swapping executors
// never moves shared state (hotswap-fix 5.7: the 2026-09-13 n=1-system-only
// incident came from a shell's own empty projection being carried in).
// hardening-review-batch2 6.1 wired candidate wrappers here — §6.5/D2 removed
// that rebinding: delegation wrappers live in the owner's config.Tools and are
// therefore SHARED by the resident cm, every candidate cm and every
// invocation-private cm. Writing this cm's projection into them was a
// construction-time side effect on published objects (design D5 forbids
// rebinding a published wrapper) and let concurrent calls of one agent read
// each other's projection. The projection is call-scoped data now and reaches
// tools through the flow context (withCallProjection); the only publish of a
// wrapper binding is the cold-start SetToolParentProjection.
func (cm *ContextManager) buildExecutor(exec ContextManagerConfig) runner.Runner {
	exec.MemPlugin = cm.memPlugin
	exec.SessionSvc = cm.sessionSvc
	return buildRunner(exec, cm.buildLLMAgent(exec))
}

// NewExecutorCandidate constructs the next generation's executor WITHOUT
// touching the live one (introduce-durable-workflow-engine §2.1): no swap, no
// snapshot update, no state-face change — the caller may still abandon it.
// Construction is side-effect free on the resident cm beyond reading it; the
// only mutation is on the candidate's own tool wrappers.
func (cm *ContextManager) NewExecutorCandidate(exec ContextManagerConfig) runner.Runner {
	if cm == nil {
		return nil
	}
	// 执行面完全由 exec 决定（零值即「本代就是空/默认」）；状态面恒取 cm。
	return cm.buildExecutor(exec)
}

// PublishExecutor is the ONE linearization point of an organization version
// switch: install the candidate, record it as the published execution face,
// then retire the superseded runner. Drain-free at turn granularity — an
// in-flight turn keeps the old runner reference and finishes on it; the next
// turn picks up the new one (spec swappable-executor「整份编排执行绑定发布」).
// Returns the runner now in force.
func (cm *ContextManager) PublishExecutor(candidate runner.Runner, exec ContextManagerConfig) runner.Runner {
	if cm == nil || candidate == nil {
		return cm.currentRunner()
	}
	// The binding is created inside the shared body from the face recorded in that
	// same critical section, so a reader can never observe a runner without its
	// face (§4.2 re-entry resolution depends on it).
	cm.executorMu.Lock()
	prev := cm.publishActiveLocked(exec, candidate, nil)
	cm.executorMu.Unlock()
	cm.retireBinding(prev)
	return cm.currentRunner()
}

// RebuildExecutor was withdrawn with introduce-durable-workflow-engine §2.1:
// it fused construction and swap, and merged the caller's zero fields onto the
// previous face (a cleared binding survived). Its two halves live on as
// NewExecutorCandidate (abandonable) and PublishExecutor (the one linearization
// point); agent/executor_publish_test.go pins both, plus the "cleared tool
// really disappears" contract that the merge used to violate.

// StagedGeneration is a prepared-but-not-installed execution generation (3.2
// trunk). Staging exists so a MULTI-OWNER publish can wire the generation-level
// declaration holds (and stamp each face's wrappers with their declared target
// bindings) BEFORE any owner's generation becomes visible — the same
// build-then-publish discipline the entry used to have alone, extended to every
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
	// §5.3: the SAME body a single-owner publish uses, with the already-wired
	// staged binding installed instead of a fresh snapshot. The same-runner case
	// advances the recorded face and creates no second generation, identically to
	// PublishExecutor — previously the two paths diverged on exactly that point.
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
// the binding, so the snapshot is the face THIS runner was built from (§4.2).
func (cm *ContextManager) newBinding(r runner.Runner) *execBinding {
	return newExecBinding(cm, cm.bindingSeq.Add(1), r, cm.execCfg)
}

// retireBinding marks `b` superseded and keeps it on the unconverged list until
// its own reference count drops. Close happens in ExecLease.Release, per
// generation — an unrelated in-flight turn cannot hold it open (§4.1).
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
	// No reference may exist yet (a publish with nobody pinned on the old
	// generation is the common case), so try the reclaim right away.
	b.release(leaseKindNoop)
}

// forgetBinding drops a closed generation from the unconverged list so the
// bookkeeping stays bounded across endless hot swaps (§5.3「资源量按当前路由和真实
// 活引用计，不按历史发布次数计」).
func (cm *ContextManager) forgetBinding(b *execBinding) {
	if cm == nil || b == nil {
		return
	}
	cm.retiredMu.Lock()
	delete(cm.retiredBindings, b)
	cm.retiredMu.Unlock()
	// A reclaim is the event §4.1's deferred tail waits on: it means one more
	// execution actually stopped, so the owner's final exit may now be complete.
	cm.fireFullyDrainedIfQuiet()
}

// armFullyDrained registers a one-shot continuation run when the last held
// generation has been reclaimed and nothing references any generation anymore —
// §4.1's「同一尾部保有责任并等真实停止后继续」. The notification comes from the
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
// unblocked it (§4.3: 「释放使用权/任务收尾经原生命周期轻量通知继续退役，不必须再来
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

// SetRetirementPoke arms the composition root's lazy drain check (§4.3) on this
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
// §5.1：业务 turn／子调用／后台执行分报，不再是一个模糊总数）。
// 只读：无任何执行路径据它分支（回收时机由各代自己的引用数决定），因此它不会
// 成为第二真源。
type ExecutorRefs struct {
	InFlightTurns   int64            `json:"inFlightTurns"`   // 业务 turn 引用（LeaseTurn）
	SubCalls        int              `json:"subCalls"`        // 继承的在途子调用引用
	BackgroundRuns  int              `json:"backgroundRuns"`  // ACK 后后台执行引用
	PendingRetirees int              `json:"pendingRetirees"` // 已退役但尚未 Close 的代（未收敛）
	OldestPending   time.Duration    `json:"oldestPending"`   // 等最久的退役代年龄
	LeakThreshold   time.Duration    `json:"leakThreshold"`   // 超过该年龄会开告警
	Generations     []GenerationRefs `json:"generations"`     // 逐代引用（含当前代）
}

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
// (§4.1: a bounded Close must be able to say WHO is still running instead of
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
			return // one alarm per call
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
	return cm.RunFlowWithExecutor(ctx, msg, nil)
}

// RunFlowWithExecutor runs one business turn on the executor pinned at the turn
// boundary (§3.2). pinned MUST come from BeginTurn of the same turn; when it is
// nil the executor is resolved here instead — the correct behavior for paths
// that have no turn boundary of their own (one-shot/sub-agent invocations, whose
// TagentAgent instance and executor were already constructed inside one
// generation and are never republished in place).
func (cm *ContextManager) RunFlowWithExecutor(ctx context.Context, msg model.Message, pinned runner.Runner) error {
	// §3.2/§4.1: a turn runs under EXACTLY ONE reference on ONE generation. Three
	// shapes decide who holds it:
	//  1. the context already carries a lease — the persistent loop's BeginTurn
	//     lease, or an ancestor invocation's inherited one → join it. Taking a
	//     second count here is the historical BeginTurn+RunFlow double registration
	//     that made one business turn look multiplied (§5.1);
	//  2. no lease, but the caller pinned an executor → the reference belongs to
	//     whoever holds the pin (contract below: it came from BeginTurn of this
	//     turn). Acquiring here would count the generation active NOW, which is not
	//     the one being run — a multiplied count AND a misreported one;
	//  3. neither → standalone flow: resolve the current generation, hold its own
	//     reference and release it when the turn's tail is done, which — thanks to
	//     the §6.3 producer-done credential — means the framework producer exited,
	//     not merely that the processed stream closed.
	lease, inherited := execLeaseFromContext(ctx)
	if !inherited && pinned == nil {
		lease = cm.AcquireLease(LeaseTurn)
		defer lease.Release()
		ctx = lease.WithContext(ctx)
		pinned = lease.Runner()
	}
	if err := lease.Err(); err != nil {
		// The turn's own reference was declined — either taken here against a converged
		// generation or inherited from an ancestor whose lease the gate refused. Either
		// way: do not start, and never run on a closed executor.
		return fmt.Errorf("%w: agent %q turn", err, cm.name)
	}
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
		// §6.5/D2: this call's projection also rides the context as the auto-inject
		// source for any delegation invoked by the flow. Delegation wrappers are
		// shared published objects (they live in the owner's config.Tools), so the
		// per-call value must never be written into them — see withCallProjection.
		ctx = withCallProjection(ctx, cm.projection)
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
		// S2m (introduce-durable-workflow-engine): the delegation's correlation handle
		// rides the same Origin→Metadata courier, so a越窗 task_settled carries the
		// invocation id S3m routes the late result back by. invocation_id is a CONTROL
		// key, so the reclaim turn's extractRootMetadata filters it from meta_*/model —
		// routing data the framework reads, never model-visible (D4).
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
			// S3m-b: on a sub-call turn (invID present + owner sinks wired), book
			// background spawns into the delivery-accounting barrier so the M2 loop
			// knows when every越窗 task this call started has settled+been delivered.
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
// currently hold. Owner-level decisions above the agent layer (§4.3 retirement) ask
// "is any execution still in flight" through this, so they read the SAME accounting
// the reclaim path acts on instead of maintaining a parallel notion of busy.
func (cm *ContextManager) OutstandingRefs() int { return cm.totalRefs() }

// totalRefs is the sum of outstanding references over all generations of this
// manager — the quantity a bounded drain waits for (§4.1: it covers background
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
// work was still active, and the caller can name it with UnconvergedRefs. The §6.2
// close sequence uses it so the output channel is never settled while a turn may
// still send to it (review M-1).
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

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

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
