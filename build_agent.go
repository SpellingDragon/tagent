// 契约: docs/wiki/platform/org-hot-reload.md#generations
package tagent

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/SpellingDragon/tagent/tool/govx"
	"github.com/SpellingDragon/tagent/tool/plan"
)

// buildMode 类型化 build ownership 契约
// ，替代散落在签名与注释间的裸 bool：调用点自描述（常驻 vs 壳），
// ownership 规则集中为谓词（本类型即唯一真源），编译期防误传。
type buildMode uint8

const (
	// buildModeResident：常驻构建——冷启动 entry 及其子 agent 树。进程级共享
	// 物的唯一 bind 点，事实链状态（投影/任务 registry）的属主。
	buildModeResident buildMode = iota
	// buildModeExecutorShell：热重建壳——产物 ta 仅用于取 runner（Swap 进
	// 常驻实例），其自身 cm/bus/projection/registry 全部丢弃。
	buildModeExecutorShell
)

// isExecutorShell：壳专属动作——memStore 复用常驻事实链（runner 写入必须落在
// 真实链上，否则换代后事实链停止增长、投影丢失换代之后的全部 turn）、tmux 常驻
// 会话强制重挂（构造期 CAS 已被首实例消耗）、entry 壳复用常驻 SessionSvc。
func (m buildMode) isExecutorShell() bool { return m == buildModeExecutorShell }

// ownsPersistentState：是否拥有事实链持久状态（投影重建 / 任务 registry
// 重建）。壳的这些产物属常驻实例，重建在壳上空跑，故跳过。
func (m buildMode) ownsPersistentState() bool { return m == buildModeResident }

// bindsProcessShared：是否执行进程级共享物的 once 绑定（evoGit.BindRuntime、
// govLedger/Goals BindStore、BundleIDProvider、Approval AddChannel）。仅常驻
// entry 有权绑定——壳重绑会把共享组件重指到即将丢弃的 store（评估闭环静默
// 失明），或审批通道累积泄漏、旧 channel 继续多播。
func (m buildMode) bindsProcessShared() bool { return m == buildModeResident }

// buildAgent recursively creates a TagentAgent for the given agent name.
// It resolves tools by looking up referenced agents in the Config.Agents map.
func buildAgent(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	mode buildMode,
	subagentCollectors ...func(name string, w *agent.AgentToolWrapper),
) (*agent.TagentAgent, error) {
	defer rc.releaseStoreBarriers()
	return buildAgentDFS(name, acfg, cfg, rc, loader, cache, mode, map[string]bool{}, subagentCollectors...)
}

// buildAgentDFS is buildAgent with a reference-path set: agents referencing
// each other through config (A→B→A, or self-reference) fail with an explicit
// cycle error instead of overflowing
// the stack — the build cache only dedupes COMPLETED agents, so a cycle
// recurses forever without this check. Path-scoped (deleted on exit), so
// legitimate diamonds (A→B, A→C, B,C→D) still build D once via the cache.
// The subagent wrapper collector handed in by the caller is threaded through
// every recursion level: the outer assembly builds the cross-restart
// re-dispatch table from the wrappers collected at all depths, and the
// variadic shape keeps that pass-through from widening the signature.
// mode/stack semantics live in buildMode (the ownership single source) and in
// the cycle check above; a hot-rebuild executor shell borrows its resident
// store by agent identity rather than creating one — see the store sharing
// contract in the memory docs.
func buildAgentDFS(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	mode buildMode,
	stack map[string]bool,
	subagentCollectors ...func(name string, w *agent.AgentToolWrapper),
) (*agent.TagentAgent, error) {
	if stack[name] {
		return nil, fmt.Errorf("agent %q: reference cycle detected in config agents", name)
	}
	stack[name] = true
	defer delete(stack, name)

	if ta, ok := cache[name]; ok {
		return ta, nil
	}

	var memStore memory.MemoryStore
	var hintTracker *memory.ConsolidationHintTracker
	var err error
	var memStoreRelease func() error
	if mode.isExecutorShell() {
		if ra := rc.resident.Get(name); ra != nil {
			memStore = ra.MemStore()
		} else {
			memStore = rc.entryMemStore
		}
		if memStore == nil {
			memStore = memory.NewInMemoryStore()
		}
	} else {
		var underlying memory.MemoryStore
		var sharedEngine memory.MemoryEngine
		underlying, sharedEngine, memStoreRelease, err = resolveMemoryStore(acfg.Memory)
		if err != nil {
			return nil, fmt.Errorf("agent %q: create memory store: %w", name, err)
		}
		memStore = underlying
		if err := rc.registerStoreOwner(name, underlying); err != nil {
			if memStoreRelease != nil {
				if rerr := memStoreRelease(); rerr != nil {
					log.Warnf("[tagent] release after owner-registration failure: %v", rerr)
				}
			}
			return nil, err
		}
		rc.raiseStoreBarrier(underlying)
		hintTracker = newConsolidationHintTracker(acfg)
		var trackFn func(int64, int, string)
		if hintTracker != nil {
			trackFn = hintTracker.Track
		}
		memStore, err = wireMemoryEngine(memStore, sharedEngine, acfg.Memory, trackFn)
		if err != nil {
			return nil, fmt.Errorf("agent %q: wire memory engine: %w", name, err)
		}
	}
	var degradationMgr *reliability.DegradationManager
	var etsHolder *memory.ErrorTrackingStore
	if cfg.Reliability.DegradationEnabled {
		baseStore := memStore
		pid := memory.PartitionIDFromName(name)
		degradationMgr = reliability.NewDegradationManager(func(dep reliability.Dependency, from, to reliability.DepState) {
			content := fmt.Sprintf("[governance:degraded] 依赖 %s 状态迁移 %s→%s", dep, from, to)
			evt := memory.FullEvent{
				EventKey:     memory.NewSnowflakeEventKey(pid, 0),
				PartitionID:  pid,
				EventType:    tagentevent.TypeGovernance,
				Content:      content,
				EventSummary: content,
				Timestamp:    time.Now().UnixMilli(),
				Metadata: map[string]string{
					tagentevent.MetaKeySubtype: tagentevent.SubtypeDegraded,
					"dependency":               string(dep),
					"from":                     string(from),
					"to":                       string(to),
				},
			}
			_ = baseStore.StoreEvent(evt.EventKey, evt)
			log.Infof("[tagent] degradation: %s", content)
			if dep == reliability.DepMemory && to == reliability.StateNormal && etsHolder != nil {
				if n, rerr := etsHolder.ReplaySpilled(); rerr == nil && n > 0 {
					log.Infof("[tagent] mem_spill replayed %d events after memory recovery", n)
				}
			}
		})
		ets := memory.NewErrorTrackingStore(memStore, reliability.MemorySink{Mgr: degradationMgr})
		if cfg.Reliability.MemSpillDir != "" && !mode.isExecutorShell() {
			if serr := ets.SetMemSpill(filepath.Join(cfg.Reliability.MemSpillDir, name+".jsonl")); serr != nil {
				return nil, fmt.Errorf("agent %q: mem_spill retention: %w", name, serr)
			}
		}
		etsHolder = ets
		memStore = ets
		log.Infof("[tagent] degradation tracking enabled for agent %q (ErrorTrackingStore C2 outermost + 5-dep state machine + mem_spill=%q)", name, cfg.Reliability.MemSpillDir)
	}
	buildOK := false
	defer func() {
		if !buildOK && memStoreRelease != nil {
			if rerr := memStoreRelease(); rerr != nil {
				log.Warnf("[tagent] release after build failure: %v", rerr)
			}
		}
	}()

	assembled, aerr := assembleAgentConfig(name, acfg, cfg, rc, loader, memStore, degradationMgr, hintTracker, cache, mode, stack, subagentCollectors)
	if aerr != nil {
		return nil, aerr
	}
	ta, werr := wireAgent(name, cfg, rc, assembled, memStore, memStoreRelease, etsHolder, hintTracker, mode, cache)
	if werr != nil {
		return nil, werr
	}
	buildOK = true
	return ta, nil
}

// assembledAgent 是 buildAgentDFS 中段的产物：装配完成的
// TagentConfig＋其 ActionTool 句柄。ToolAgentFactory 分支同样只产配置，
// face/runCfg 与构造路径和 config-driven 完全同轨——不存在「无法去壳」的
// 第二 owner 形态。
type assembledAgent struct {
	cfg        *agent.TagentConfig
	actionTool *action.ActionTool
}

// assembleAgentConfig is buildAgentDFS 的中段：system
// prompt/model/tools/decorators/治理包裹/meditation/TTL 解析 → TagentConfig。
// 相对 agent 构造纯净——buildAgentFace 复用它为已存在 agent 装配换代 face
// 而不 NewTagentAgent。store 半的产物（memStore/degradationMgr/hintTracker）
// 是它的输入。
func assembleAgentConfig(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	memStore memory.MemoryStore,
	degradationMgr *reliability.DegradationManager,
	hintTracker *memory.ConsolidationHintTracker,
	cache map[string]*agent.TagentAgent,
	mode buildMode,
	stack map[string]bool,
	subagentCollectors []func(name string, w *agent.AgentToolWrapper),
) (*assembledAgent, error) {
	systemPrompt, err := loader.LoadComposite(
		acfg.SystemPrompt.Inline,
		acfg.SystemPrompt.Files,
		acfg.SystemPrompt.Dir,
	)
	if err != nil {
		return nil, fmt.Errorf("agent %q: load system prompt: %w", name, err)
	}
	var systemPromptSource prompt.Getter
	if !acfg.SystemPrompt.IsEmpty() {
		systemPromptSource = prompt.NewSource(loader, acfg.SystemPrompt)
	}
	if rc.evoGit != nil && name == cfg.Entry && mode.bindsProcessShared() {
		evSrc := evolution.NewStoreEvidenceSource(memStore, memory.PartitionIDFromName(name), 0)
		evSrc.SetActivationLog(rc.evoGit.Log())
		judgeModelRef := rc.resolveModelRef(cfg.Evolution.Judge, name, acfg, cfg)
		evJudge := evolution.NewLLMJudgeEvaluator(rc.judgeModel(name, cfg), evSrc,
			cfg.Evolution.JudgeMinSamples, cfg.Evolution.JudgePassThreshold,
			time.Duration(cfg.Evolution.JudgeTimeoutSeconds)*time.Second)
		if judgeModelRef != nil && judgeModelRef.effort != nil {
			evJudge = evJudge.WithEffort(*judgeModelRef.effort)
		}
		rc.evoGit.SetGovernanceSignalsAvailable(func() bool {
			return cfg.Governance.Enabled && rc.govGate != nil
		})
		rc.evoGit.BindRuntime(
			memStore, memory.PartitionIDFromName(name),
			evJudge,
			evolution.NewMetricGuardrail(evSrc, evolution.GuardrailConfig{
				MaxDenialRate:   cfg.Evolution.MaxDenialRate,
				MaxCriticalRate: cfg.Evolution.MaxCriticalRate,
				MaxNegFbRate:    cfg.Evolution.MaxNegFBRate,
			}),
		)
	}

	agentModel := rc.resolveAgentModel(name, acfg, cfg)

	ownPartition := memory.PartitionIDFromName(name)
	readPartitionIDs := []int{ownPartition}
	for _, ns := range acfg.Memory.ReadNamespaces {
		pid := memory.PartitionIDFromName(ns)
		if pid != ownPartition {
			readPartitionIDs = append(readPartitionIDs, pid)
		}
	}

	registry := GetRegistry()
	if !builtinAgentNames[name] {
		if factory, ok := registry.GetToolAgentFactory(name); ok {
			factoryCfg := agent.ToolAgentFactoryConfig{
				ID:                   name,
				Model:                agentModel,
				SystemPrompt:         systemPrompt,
				MemoryStore:          memStore,
				ReadPartitionIDs:     readPartitionIDs,
				MaxToolIterations:    acfg.MaxToolIterations,
				MaxTokens:            acfg.MaxTokens,
				Temperature:          acfg.Temperature,
				SkillRepo:            rc.skillRepo,
				MCPToolSets:          rc.mcpToolSets,
				ThinkingEnabled:      acfg.ThinkingEnabled,
				ThinkingTokens:       acfg.ThinkingTokens,
				ReasoningEffort:      acfg.ReasoningEffort,
				ReasoningContentMode: acfg.ReasoningContentMode,
			}
			if rc.mcpRegistry != nil {
				factoryCfg.MCPRegistry = rc.mcpRegistry
			}

			fcfg, err := factory(factoryCfg)
			if err != nil {
				return nil, fmt.Errorf("agent %q: factory failed: %w", name, err)
			}
			if fcfg == nil {
				return nil, fmt.Errorf("agent %q: factory failed: produced no configuration", name)
			}
			if fcfg.Name == "" {
				fcfg.Name = name
			}
			if fcfg.MemoryStore == nil {
				fcfg.MemoryStore = memStore
			}
			return &assembledAgent{cfg: fcfg}, nil
		}
	}

	var tools []trpctool.Tool
	var actionTool *action.ActionTool
	for _, tr := range acfg.Tools {
		t, isAction, err := buildToolFromRef(tr, cfg, acfg.WorkspaceRoot, rc, loader, cache, memStore, readPartitionIDs, degradationMgr, consolidationMinSources(acfg), mode, stack, subagentCollectors...)
		if err != nil {
			return nil, fmt.Errorf("agent %q: build tool %q: %w", name, tr.AgentID, err)
		}
		if w, ok := t.(*agent.AgentToolWrapper); ok {
			if acfg.ResumeContextRounds > 0 {
				w.SetResumeContextRounds(acfg.ResumeContextRounds)
			}
			for _, collect := range subagentCollectors {
				collect(w.DeclaredAgentName(), w)
			}
		}
		if isAction {
			actionTool = t.(*action.ActionTool)
			if mode.isExecutorShell() {
				actionTool.ReattachResidentSessions()
			}
		}
		tools = append(tools, t)
	}

	if rc.evoGit != nil && name == cfg.Entry {
		tools = append(tools, evolution.NewRefineTool(rc.evoGit))
	}

	if cfg.Governance.Enabled && rc.govGate != nil && name == cfg.Entry {
		tools = append(tools, govx.NewGoalTools(rc.govGate)...)
	}

	if rc.govGate != nil && rc.govGate.Enabled() {
		budgetDir := ""
		if cfg.Governance.Dir != "" {
			budgetDir = filepath.Join(cfg.Governance.Dir, "budget", name)
		}
		agentGate := governance.NewGovernanceGate(governance.GateDeps{
			Classifier: rc.govGate.Classifier(),
			Budget: governance.NewBudgetManager(governance.BudgetConfig{
				Window:        time.Duration(cfg.Governance.BudgetWindowMinutes) * time.Minute,
				MaxHighRisk:   cfg.Governance.MaxHighRisk,
				MaxMediumRisk: cfg.Governance.MaxMediumRisk,
			}, budgetDir),
			Approval:  rc.govGate.Approval(),
			Goals:     rc.govGate.Goals(),
			Ledger:    rc.govLedger,
			Config:    rc.govGate.Config(),
			AgentName: name,
		})
		if name == cfg.Entry && mode.bindsProcessShared() {
			rc.govLedger.BindStore(memStore, memory.PartitionIDFromName(name))
			rc.govGate.Goals().BindStore(memStore, memory.PartitionIDFromName(name))
		}
		for i, t := range tools {
			if _, isWrapper := t.(*agent.AgentToolWrapper); isWrapper {
				continue
			}
			tools[i] = governance.NewGovernanceTool(t, agentGate)
		}
	}

	var sessionSvcForShell session.Service
	if mode.isExecutorShell() && name == cfg.Entry {
		sessionSvcForShell = rc.entrySessionSvc
	}
	agentCfg := &agent.TagentConfig{
		Name:                 name,
		Model:                agentModel,
		MemoryStore:          memStore,
		MemStoreBorrowed:     mode.isExecutorShell(),
		SessionSvc:           sessionSvcForShell,
		SystemPrompt:         systemPrompt,
		SystemPromptSource:   systemPromptSource,
		Tools:                tools,
		MaxToolIterations:    acfg.MaxToolIterations,
		MaxTokens:            acfg.MaxTokens,
		Temperature:          acfg.Temperature,
		CompressThreshold:    acfg.CompressThreshold,
		KeepRecentTasks:      acfg.KeepRecentTasks,
		ThinkingEnabled:      acfg.ThinkingEnabled,
		ThinkingTokens:       acfg.ThinkingTokens,
		ReasoningEffort:      acfg.ReasoningEffort,
		ReasoningContentMode: acfg.ReasoningContentMode,
		DegradationBehaviors: buildDegradationBehaviors(cfg.Reliability),
		Compress: agent.CompressConfig{
			CompactKeysListed: acfg.Compress.CompactKeysListed,
			RecentFullCount:   acfg.Compress.RecentFullCount,
			CardMaxChars:      acfg.Compress.CardMaxChars,
			SummaryMaxTokens:  acfg.Compress.SummaryMaxTokens,
		},
		WorkspaceRoot: acfg.WorkspaceRoot,

		SummaryTimeoutSeconds: acfg.Compress.SummaryTimeoutSeconds,
		CaptureEnabled:        rc.captureEnabled,
	}
	if cfg.Reliability.BusSpillDir != "" && !mode.isExecutorShell() {
		agentCfg.BusSpillDir = filepath.Join(cfg.Reliability.BusSpillDir, name)
	}
	agentCfg.Degradation = degradationMgr
	if acfg.TaskTerminalTTL != "" {
		if ttl, err := time.ParseDuration(acfg.TaskTerminalTTL); err == nil && ttl > 0 {
			agentCfg.TaskTerminalTTL = ttl
		} else {
			log.Warnf("[tagent] agent %q: invalid task_terminal_ttl %q, using default", name, acfg.TaskTerminalTTL)
		}
	}
	if acfg.TaskDefaultTTL != "" {
		if d, err := time.ParseDuration(acfg.TaskDefaultTTL); err == nil && d > 0 {
			agentCfg.TaskDefaultTTL = d
		} else {
			log.Warnf("[tagent] agent %q: invalid task_default_ttl %q, using default", name, acfg.TaskDefaultTTL)
		}
	}
	if summaryRef := rc.resolveModelRef(acfg.Compress.Summary, name, acfg, cfg); summaryRef != nil {
		agentCfg.SummaryModel = summaryRef.model
		if summaryRef.effort != nil {
			agentCfg.SummaryEffort = *summaryRef.effort
		}
	}

	if acfg.Meditation.Enabled {
		interval, _ := time.ParseDuration(acfg.Meditation.Interval)
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		minGap, _ := time.ParseDuration(acfg.Meditation.MinGap)
		if minGap <= 0 {
			minGap = 2 * time.Hour
		}
		promptFile := acfg.Meditation.PromptFile
		if promptFile == "" {
			promptFile = "meditation.md"
		}
		promptText, err := loader.LoadFromFile(promptFile)
		if err != nil {
			return nil, fmt.Errorf("agent %q: load meditation prompt: %w", name, err)
		}
		meditationPromptSource := prompt.NewSource(loader, prompt.CompositeConfig{
			Files: []string{promptFile},
		})
		meditationAnchorPath := ""
		if cfg.Reliability.MeditationAnchorDir != "" {
			meditationAnchorPath = filepath.Join(cfg.Reliability.MeditationAnchorDir, name+".json")
		}
		agentCfg.Meditation = agent.MeditationConfig{
			Enabled:      true,
			Interval:     interval,
			MinGap:       minGap,
			PromptText:   promptText,
			PromptSource: meditationPromptSource,
			AnchorPath:   meditationAnchorPath,
		}
		if hintTracker != nil {
			tracker := hintTracker
			pid := memory.PartitionIDFromName(name)
			evoDigest := ""
			if rc.evoGit != nil {
				evoDigest = rc.evoGit.DigestSummary()
			}
			agentCfg.Meditation.DigestExtra = func() string {
				return tracker.CandidatesText(pid) + evoDigest
			}
		}
	}

	return &assembledAgent{cfg: agentCfg, actionTool: actionTool}, nil
}

// wireAgent is buildAgentDFS 的尾段：NewTagentAgent＋全部
// post 构造接线（resident-session sink、evolution/governance 通道、WAL 重建、
// wrapper owner 挂载、closer 登记、缓存写入）。只有本段构造 TagentAgent；
// face 路径（buildAgentFace）到中段为止。memStoreRelease/etsHolder 属 store
// 半产物，在此注入 cfg/重放接线。
func wireAgent(
	name string,
	cfg Config,
	rc *runtimeConfig,
	assembled *assembledAgent,
	memStore memory.MemoryStore,
	memStoreRelease func() error,
	etsHolder *memory.ErrorTrackingStore,
	hintTracker *memory.ConsolidationHintTracker,
	mode buildMode,
	cache map[string]*agent.TagentAgent,
) (*agent.TagentAgent, error) {
	agentCfg := assembled.cfg
	agentCfg.MemStoreRelease = memStoreRelease
	actionTool := assembled.actionTool
	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create tagent agent: %w", name, err)
	}

	if actionTool != nil {
		actionTool.SetResidentRecordSink(ta.RecordResidentSession)
		if cfg.ResidentMetaDir != "" {
			actionTool.SetResidentMetaDir(cfg.ResidentMetaDir)
		}
		actionTool.SetDefaultTTLSource(spawnerTTLSource(ta))
	}

	if fn := rc.retirementPoke.Load(); fn != nil {
		ta.ContextManager().SetRetirementPoke(*fn)
	}

	if rc.evoGit != nil && name == cfg.Entry && mode.bindsProcessShared() {
		evoGit := rc.evoGit
		ta.SetBundleIDProvider(evoGit.LatestSha)
		ta.RegisterCloser(stopCloser(evoGit.Stop))
	}

	if cfg.WorkingDir != "" && name == cfg.Entry && mode.bindsProcessShared() {
		auditor := evolution.NewAssetAuditor(cfg.WorkingDir, evolution.DefaultAssetPatterns(),
			[]string{cfg.ConfigPath}, func(changes []evolution.AssetChange) {
				cs := make([]agent.CognitiveAssetChange, 0, len(changes))
				for _, c := range changes {
					cs = append(cs, agent.CognitiveAssetChange(c))
				}
				ta.RecordCognitiveAssetChange(cs)
			})
		if err := auditor.Start(); err == nil {
			ta.RegisterCloser(auditor)
		} else {
			log.Errorf("[asset-drift-audit] start failed (audit disabled): %v", err)
		}
	}

	if cfg.Governance.Enabled && rc.govGate != nil && name == cfg.Entry && rc.govGate.Approval() != nil && mode.bindsProcessShared() {
		rc.govGate.Approval().AddChannel(&approvalInjectChannel{ta: ta})
		for _, ch := range rc.approvalChannels {
			rc.govGate.Approval().AddChannel(ch)
		}
	}

	if hintTracker != nil {
		hintTracker.SetOnHint(func(pid, count int) {
			ta.InjectMessageWithSource("consolidation_hint", model.Message{
				Role: model.RoleUser,
				Content: fmt.Sprintf("[consolidation_hint] 本分区已累计 %d 个边界事件（用户意图/任务产出）未做巩固。"+
					"若其中有值得沉淀的经验、约束或事实，可用 memory_consolidate 巩固（源事件 key 见近期时间线卡片，"+
					"工具会做收据指纹校验）；若无可沉淀内容，忽略本提示即可（snooze 窗内不会重复打扰）。", count),
			})
		})
	}

	if mode.ownsPersistentState() {
		ta.RebuildProjectionFromWAL()
		if _, rerr := ta.ReconcileOutstanding(); rerr != nil {
			log.Errorf("[recovery] §5.7 outstanding reconcile aborted on inventory failure — nothing disposed, retry next boot: %v", rerr)
		}
	}

	if tm := ta.TaskManager(); tm != nil && actionTool != nil {
		tm.SetSessionTracker(actionTool.IsTrackedSession)
	}
	if tm := ta.TaskManager(); tm != nil && mode.ownsPersistentState() {
		redispatch := agent.SubagentRedispatcher(func(ctx context.Context, name string) (*agent.AgentToolWrapper, *agent.ExecLease, error) {
			return agent.ResolveReentryDelegation(ctx, ta.ContextManager(), name)
		}, tm)
		rebuildClosures := func(decl task.Declarative) task.TaskSpec {
			switch decl.Kind {
			case "command":
				if actionTool != nil {
					return actionTool.SpecFromDeclarative(tm, decl)
				}
			case "subagent":
				return action.SubagentSpecFromDeclarative(redispatch, decl)
			}
			return task.TaskSpec{Kind: decl.Kind, Desc: decl.Desc, Key: decl.Key, Declarative: &decl}
		}
		ta.RebuildTaskRegistryFromWAL(memStore, rebuildClosures)
		if actionTool != nil {
			if n := tm.RetireOrphans(actionTool.IsTrackedSession); n > 0 {
				log.Infof("[rebuild-task-registry] orphan adjudication: retired %d nil-probe suspect(s) (reincarnation orphans)", n)
			}
			for _, tk := range tm.List() {
				if tk.Spec.Declarative == nil {
					continue
				}
				st := tk.Status()
				if st != task.TaskSuspect && st != task.TaskAliveDetached {
					continue
				}
				if actionTool.IsTrackedSession(tk.Spec.Declarative.TaskID) {
					if st == task.TaskSuspect {
						tm.MarkTaskRunning(tk.ID)
					}
					if d := actionTool.TakeReattachedDetector(tk.Spec.Declarative.TaskID); d != nil {
						if berr := tm.BindDetector(tk.ID, d); berr != nil {
							log.Warnf("[build] bind reattached detector to task %s failed: %v", tk.ID, berr)
						} else {
							log.Infof("[build] reattached detector bound: task=%s session=%s status=%s", tk.ID, tk.Spec.Declarative.TaskID, st)
						}
					}
				}
			}
		}
	}

	if etsHolder != nil {
		etsHolder.SetReplayProjection(agent.ReplayProjectionHandler(ta))
	}

	if actionTool != nil {
		ta.RegisterCloser(actionTool)
	}

	ta.SetToolParentProjection()

	ownerCM := ta.ContextManager()
	if !mode.ownsPersistentState() {
		ownerCM = nil
		if ra := rc.resident.Get(name); ra != nil {
			ownerCM = ra.ContextManager()
		}
	}
	ta.SetDelegationOwnerCM(ownerCM)

	if !mode.isExecutorShell() {
		ownerName := name
		ta.SetStoreOwnerRevoker(func() { rc.unRegisterStoreOwner(ownerName) })
	}

	cache[name] = ta
	return ta, nil
}

// buildAgentFace assembles an EXISTING agent next-generation execution face
// without constructing a TagentAgent: shell-semantics store borrowing plus the same assembly middle, then agent.BuildExecutionFace.
//
// - Refs resolve from cache; the caller supplies the candidate domain (resident snapshot plus this round hot-adds), so every owner contributes zero constructions and only a genuine cache miss recurses.
func buildAgentFace(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
) (agent.ContextManagerConfig, *assembledAgent, error) {
	// store half, shell semantics: borrow the RESIDENT owner's store by identity
	// (never blanket entryMemStore — F06); nil (defensive) falls back the same
	// way the shell path did.
	var memStore memory.MemoryStore
	if ra := rc.resident.Get(name); ra != nil {
		memStore = ra.MemStore()
	} else {
		memStore = rc.entryMemStore
	}
	if memStore == nil {
		memStore = memory.NewInMemoryStore()
	}
	parts, aerr := assembleAgentConfig(name, acfg, cfg, rc, loader, memStore, nil, nil, cache, buildModeExecutorShell, map[string]bool{}, nil)
	if aerr != nil {
		return agent.ContextManagerConfig{}, nil, aerr
	}
	return agent.BuildExecutionFace(parts.cfg), parts, nil
}

// buildToolFromRef creates a tool from a ToolRef entry.
func buildToolFromRef(
	tr ToolRef,
	cfg Config,
	workspaceRoot string,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
	readPartitionIDs []int,
	degradationMgr *reliability.DegradationManager,
	consolidationMin int,
	mode buildMode,
	stack map[string]bool,
	subagentCollectors ...func(name string, w *agent.AgentToolWrapper),
) (trpctool.Tool, bool, error) {
	desc, err := resolveToolDescription(tr, loader)
	if err != nil {
		return nil, false, err
	}

	switch tr.Kind {
	case ToolKindAgent:
		return buildAgentToolRef(tr, cfg, rc, loader, cache, parentMemStore, desc, mode, stack, subagentCollectors...)
	case ToolKindTool:
		return buildPlainToolRef(tr, workspaceRoot, cfg.WorkingDir, rc, parentMemStore, readPartitionIDs, desc, degradationMgr, consolidationMin)
	default:
		return nil, false, fmt.Errorf("unknown tool kind %q", tr.Kind)
	}
}

// buildAgentToolRef creates a tool agent and wraps it as a CallableTool.
// When ToolRef.Remote is set, creates a remote A2AAgent instead of a local TagentAgent.
// Both paths produce an agent.Agent, which AgentToolWrapper wraps uniformly.
func buildAgentToolRef(
	tr ToolRef,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
	desc string,
	mode buildMode,
	stack map[string]bool,
	subagentCollectors ...func(name string, w *agent.AgentToolWrapper),
) (trpctool.Tool, bool, error) {
	if tr.IsRemoteRef() {
		a2aAgent, err := a2aagent.New(
			a2aagent.WithName(tr.AgentID),
			a2aagent.WithDescription(desc),
			a2aagent.WithAgentCardURL(tr.Remote.URL),
			a2aagent.WithTransferStateKey(agent.ExternalContextKey),
		)
		if err != nil {
			return nil, false, fmt.Errorf("create remote A2A agent %q: %w", tr.AgentID, err)
		}
		wrapper := agent.NewAgentToolWrapper(a2aAgent, desc, tr.EventParams, parentMemStore)
		if tr.Async != nil && !*tr.Async {
			wrapper.SetAsyncDisabled(true)
		}
		if len(tr.ExtraParams) > 0 {
			wrapper.SetExtraParams(tr.ExtraParams)
		}
		if tr.DescriptionFile != "" {
			wrapper.SetDescriptionSource(prompt.NewSource(loader, prompt.CompositeConfig{
				Files: []string{tr.DescriptionFile},
			}))
		}
		log.Infof("[tagent] created remote A2A agent tool: %s → %s", tr.AgentID, tr.Remote.URL)
		return wrapper, false, nil
	}

	refCfg, ok := cfg.Agents[tr.AgentID]
	if !ok {
		return nil, false, fmt.Errorf("referenced agent %q not found in config", tr.AgentID)
	}

	subAgent, err := buildAgentDFS(tr.AgentID, refCfg, cfg, rc, loader, cache, mode, stack, subagentCollectors...)
	if err != nil {
		return nil, false, err
	}

	// Wrap with PlanAgent if this is the plan agent — enables dual-mode Run
	// (progress queries bypass LLM via direct file I/O).
	var agentImpl trpcagent.Agent = subAgent
	if tr.AgentID == "plan" {
		agentImpl = plan.NewPlanAgent(subAgent, ".")
	}

	wrapper := agent.NewAgentToolWrapper(agentImpl, desc, tr.EventParams, parentMemStore)
	if tr.Async != nil && !*tr.Async {
		wrapper.SetAsyncDisabled(true)
	}
	if len(tr.ExtraParams) > 0 {
		wrapper.SetExtraParams(tr.ExtraParams)
	}
	if tr.DescriptionFile != "" {
		wrapper.SetDescriptionSource(prompt.NewSource(loader, prompt.CompositeConfig{
			Files: []string{tr.DescriptionFile},
		}))
	}
	return wrapper, false, nil
}

// buildPlainToolRef creates a plain tool via the factory registry.
// Runtime dependencies (rc, memStore, readPartitionIDs) are injected into
// PlainToolFactoryConfig so that sub-tools like skill_search, memory_query,
// recall_query etc. can access them during factory creation.
func buildPlainToolRef(
	tr ToolRef,
	workspaceRoot string,
	workingDir string,
	rc *runtimeConfig,
	memStore memory.MemoryStore,
	readPartitionIDs []int,
	desc string,
	degradationMgr *reliability.DegradationManager,
	consolidationMinSources int,
) (trpctool.Tool, bool, error) {
	registry := GetRegistry()
	factory, ok := registry.GetPlainToolFactory(tr.ID)
	if !ok {
		return nil, false, fmt.Errorf("no plain tool factory registered for id %q", tr.ID)
	}

	factoryCfg := agent.PlainToolFactoryConfig{
		ID:                      tr.ID,
		Description:             desc,
		Properties:              tr.Properties,
		WorkspaceRoot:           workspaceRoot,
		WorkingDir:              workingDir,
		MemStore:                memStore,
		SkillRepo:               rc.skillRepo,
		MCPToolSets:             rc.mcpToolSets,
		ReadPartitionIDs:        readPartitionIDs,
		Degradation:             degradationMgr,
		MCPProbeEvery:           rc.reliability.DegradationMCPProbeEvery,
		ConsolidationMinSources: consolidationMinSources,
	}
	if rc.mcpRegistry != nil {
		factoryCfg.MCPRegistry = rc.mcpRegistry
	}
	callable, err := factory(factoryCfg)
	if err != nil {
		return nil, false, err
	}

	_, isAction := callable.(*action.ActionTool)
	return callable, isAction, nil
}
