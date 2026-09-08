package tagent

import (
	"fmt"
	"path/filepath"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/SpellingDragon/tagent/tool/govx"
	"github.com/SpellingDragon/tagent/tool/plan"
)

func buildAgent(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
) (*agent.TagentAgent, error) {
	// Check cache first
	if ta, ok := cache[name]; ok {
		return ta, nil
	}

	// 1. Create this agent's MemoryStore (isolated per-agent)
	memStore, err := resolveMemoryStore(acfg.Memory)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create memory store: %w", name, err)
	}
	// 1.5 按配置包裹记忆引擎（T-A 解耦缝）：未配置则原样返回（行为逐字节不变）。
	// 4.2（design-report-closeout）：巩固容量触发器（配置门控；threshold<=0 → nil=关闭）。
	hintTracker := newConsolidationHintTracker(acfg)
	var trackFn func(int64, int, string)
	if hintTracker != nil {
		trackFn = hintTracker.Track
	}
	memStore, err = wireMemoryEngine(memStore, acfg.Memory, trackFn)
	if err != nil {
		return nil, fmt.Errorf("agent %q: wire memory engine: %w", name, err)
	}
	// 1.6 T-G DegradationManager（报告 D3 C2 契约最外层）：配置启用时构造五依赖退化状态机 +
	// ErrorTrackingStore 包裹 memStore，补齐此前 DegradationManager 零接线的断点。onChange 写
	// governance degraded 事件用 baseStore（ErrorTrackingStore 包裹前），防「写失败→上报→
	// onChange→写事件」递归。未启用则 degradationMgr=nil、memStore 不包裹（现状零变化）。
	var degradationMgr *reliability.DegradationManager
	// etsHolder 延迟引用（函数级）：onChange 触发重放（步4）与 NewTagentAgent 后的
	// 重放投影双写回填（5.5, design-report-closeout）都晚于 ErrorTrackingStore 构造。
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
			_ = baseStore.StoreEvent(evt.EventKey, evt) // baseStore 非 ErrorTrackingStore，无递归
			log.Infof("[tagent] degradation: %s", content)
			// 步4：memory 恢复到 normal → 重放退化期间兜底落盘的事件（回灌，不丢）。
			if dep == reliability.DepMemory && to == reliability.StateNormal && etsHolder != nil {
				if n, rerr := etsHolder.ReplaySpilled(); rerr == nil && n > 0 {
					log.Infof("[tagent] mem_spill replayed %d events after memory recovery", n)
				}
			}
		})
		ets := memory.NewErrorTrackingStore(memStore, reliability.MemorySink{Mgr: degradationMgr})
		if cfg.Reliability.MemSpillDir != "" {
			ets.SetMemSpill(filepath.Join(cfg.Reliability.MemSpillDir, name+".jsonl"))
		}
		etsHolder = ets
		memStore = ets
		log.Infof("[tagent] degradation tracking enabled for agent %q (ErrorTrackingStore C2 outermost + 5-dep state machine + mem_spill=%q)", name, cfg.Reliability.MemSpillDir)
	}
	// 构建失败回收（审查 Nit5）：隔离 store（无共享）时，若后续步骤失败则关闭已启动的
	// 引擎（worker/重建 goroutine），防泄漏。共享 store 的引擎按 path 复用，不在此关闭。
	buildOK := false
	defer func() {
		if !buildOK && acfg.Memory.Path == "" {
			if c, ok := memStore.(agent.Closer); ok {
				_ = c.Close()
			}
		}
	}()

	// 2. Resolve system prompt
	systemPrompt, err := loader.LoadComposite(
		acfg.SystemPrompt.Inline,
		acfg.SystemPrompt.Files,
		acfg.SystemPrompt.Dir,
	)
	if err != nil {
		return nil, fmt.Errorf("agent %q: load system prompt: %w", name, err)
	}
	// Create hot-reloadable source for system prompt
	var systemPromptSource prompt.Getter
	if !acfg.SystemPrompt.IsEmpty() {
		systemPromptSource = prompt.NewSource(loader, acfg.SystemPrompt)
	}
	// evolution (self-evolution-git-native)：**系统提示词回归文件直读（mtime 热重载）**——
	// bundle 快照遮蔽层（VersionedSource）已随发布道退役（P2：文件即真源）。
	// 后验评估闭环：judge+guardrail 经 BindRuntime 绑定到 GitEvolution（同点位换接，
	// memStore 就绪时序保持——S3）；评估窗口锚=register 时刻（improvement 事件，W4 迁移），
	// 判定只产 evaluation 事件（建议式，P4）。
	if rc.evoGit != nil && name == cfg.Entry {
		evSrc := evolution.NewStoreEvidenceSource(memStore, memory.PartitionIDFromName(name), 0)
		evSrc.SetActivationLog(rc.evoGit.Log())
		rc.evoGit.BindRuntime(
			memStore, memory.PartitionIDFromName(name),
			evolution.NewLLMJudgeEvaluator(rc.model, evSrc,
				cfg.Evolution.JudgeMinSamples, cfg.Evolution.JudgePassThreshold,
				time.Duration(cfg.Evolution.JudgeTimeoutSeconds)*time.Second), // M8：零值走 judge 内部默认
			evolution.NewMetricGuardrail(evSrc, evolution.GuardrailConfig{
				MaxDenialRate:   cfg.Evolution.MaxDenialRate,
				MaxCriticalRate: cfg.Evolution.MaxCriticalRate,
				MaxNegFbRate:    cfg.Evolution.MaxNegFBRate,
			}), // Minor①：阈值经 config 通路（零值走内部默认）
		)
	}

	// 3. Resolve model — per-agent override supported
	agentModel := rc.resolveAgentModel(name, acfg, cfg)

	// Resolve read partitions for recall/query tools (computed once, used by
	// both factory path and config-driven path):
	//   1. The agent's OWN namespace always comes first — events are written to
	//      PartitionIDFromName(agentName), so an agent must be able to query its
	//      own timeline (e.g. timeline recall via since/until) without any
	//      extra config. Without this, query-mode recall on FileSegmentStore
	//      silently returns 0 events (resolvePartitions treats "no partitions"
	//      as "scan nothing", per event-segment-store isolation).
	//   2. read_namespaces adds CROSS-namespace read access on top (deduped).
	ownPartition := memory.PartitionIDFromName(name)
	readPartitionIDs := []int{ownPartition}
	for _, ns := range acfg.Memory.ReadNamespaces {
		pid := memory.PartitionIDFromName(ns)
		if pid != ownPartition {
			readPartitionIDs = append(readPartitionIDs, pid)
		}
	}

	// 3.5 Check for registered ToolAgentFactory — for custom agents only.
	// Built-in agent names (knowledge/recall/action) are
	// protected and always built via the config-driven path. This prevents a
	// registered factory from silently overriding the declared AgentConfig.Tools.
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
			// Nil-guard: keep the interface field nil when no registry exists
			// (a typed nil *Registry would read as non-nil).
			if rc.mcpRegistry != nil {
				factoryCfg.MCPRegistry = rc.mcpRegistry
			}

			ta, err := factory(factoryCfg)
			if err != nil {
				return nil, fmt.Errorf("agent %q: factory failed: %w", name, err)
			}

			cache[name] = ta
			return ta, nil
		}
	}

	// 4. Build tools from ToolRef list
	var tools []trpctool.Tool
	var actionTool *action.ActionTool

	for _, tr := range acfg.Tools {
		t, isAction, err := buildToolFromRef(tr, cfg, acfg.WorkspaceRoot, rc, loader, cache, memStore, readPartitionIDs, degradationMgr, consolidationMinSources(acfg))
		if err != nil {
			return nil, fmt.Errorf("agent %q: build tool %q: %w", name, tr.AgentID, err)
		}
		// Agent-level task knobs flow to sub-agent wrappers at assembly time
		// (ToolRef stays a pure reference declaration).
		if w, ok := t.(*agent.AgentToolWrapper); ok && acfg.ResumeContextRounds > 0 {
			w.SetResumeContextRounds(acfg.ResumeContextRounds)
		}
		if isAction {
			actionTool = t.(*action.ActionTool)
		}
		tools = append(tools, t)
	}

	// evolution: git 原生 refine 工具（register/status/rollback）——仅 entry agent 且
	// evolution 启用时注册。**先于治理包裹追加**（A3：refine 是最高权限通道，rollback 改
	// 受控产物必须过治理闸；DefaultRules 有 refine 规则）。
	if rc.evoGit != nil && name == cfg.Entry {
		tools = append(tools, evolution.NewRefineTool(rc.evoGit))
	}

	// 5.1（design-report-closeout）：治理面工具五件套（goal_declare/goal_list/
	// goal_resolve/denial_query/approval_list）——仅 entry agent 且治理启用时追加
	// （治理面收敛主循环，与 refine 同级）；先于治理包裹追加（goal 工具自身也过闸，
	// classify 判 low 直接放行）。工具只做登记/查询——批准权始终在人（approval_list
	// 只列 pending 与批准方式）。
	if cfg.Governance.Enabled && rc.govGate != nil && name == cfg.Entry {
		tools = append(tools, govx.NewGoalTools(rc.govGate)...)
	}

	// T-G: 治理闸包裹 leaf 工具（配置门控，默认关闭则不包裹 → 现状逐字节不变）。跳过 sub-agent
	// 包装器（*agent.AgentToolWrapper）——下游需按具体类型断言接 parentProjection；治理聚焦
	// exec/file/mcp/refine 等 leaf 工具（主风险面）。actionTool 原始引用已在循环内提取，包裹
	// tools[] 不影响其 RegisterCloser；agent.go 随后包 OutputLimitTool，链式委托 GovernanceTool.Call。
	// W3（§8.3）：治理扩展到**所有 agent**（此前 name==cfg.Entry 只包 entry，致 action/knowledge
	// 子 agent 的 exec/save_file/mcp_call 主风险面全部绕闸）。用户裁决：子 agent **独立预算**
	// （per-agent BudgetManager，隔离单 agent 刷爆，每 agent 各自有界）。共享 Classifier(纯函数)/
	// Approval(文件通道全局)/Goals(全局注册)/Config/**Ledger**(N2：rc.govLedger 共享账本，entry
	// buildAgent 延迟绑定 entry memStore，子 agent 治理记录也 durable 非兜底内存)。rc.govGate 作
	// 跨 agent 共享组件源（New 构造，其自身 Budget 不作生产用）。
	// Minor⑥（§8.9）已知边界：自定义 ToolAgentFactory（RegisterToolAgent）构造的 agent 不经本
	// buildPlainToolRef 包裹路径，其 leaf 工具需工厂自行治理（或后续在 factory 输出统一包裹）。
	if rc.govGate != nil && rc.govGate.Enabled() {
		// Minor①（§8.9，优先）：Governance.Dir="" 时预算纯内存不落盘——filepath.Join("", "budget",
		// name) 会得相对路径 "budget/name" 意外落盘进程 CWD，违反 BudgetManager"空 dir=纯内存"契约。
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
			}, budgetDir), // per-agent 独立 epoch 持久化（Dir="" 则纯内存）
			Approval:  rc.govGate.Approval(),
			Goals:     rc.govGate.Goals(),
			Ledger:    rc.govLedger, // N2：共享 entry 持久账本（子 agent 治理记录也 durable，非兜底内存）
			Config:    rc.govGate.Config(),
			AgentName: name, // §8.1：治理记录标注来源 agent（共享 Ledger 下多 agent 事件可区分）
		})
		if name == cfg.Entry {
			// N2：entry memStore 就绪 → 延迟绑定共享账本的持久 store（此后所有 agent gate 的
			// 治理记录写 entry governance 分区，重启可 recall）。替代原 agentGate.BindLedger。
			rc.govLedger.BindStore(memStore, memory.PartitionIDFromName(name))
			// 5.2（design-report-closeout）：goal 声明持久化——同 entry 分区（治理审计
			// 单区）；Declare/Resolve 双写 governance 事件，重启经回放重建。
			rc.govGate.Goals().BindStore(memStore, memory.PartitionIDFromName(name))
		}
		for i, t := range tools {
			if _, isWrapper := t.(*agent.AgentToolWrapper); isWrapper {
				continue
			}
			tools[i] = governance.NewGovernanceTool(t, agentGate)
		}
	}

	// 5. Create TagentAgent
	agentCfg := &agent.TagentConfig{
		Name:                 name,
		Model:                agentModel,
		MemoryStore:          memStore,
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
	}
	// T-G ReliableBus：per-agent 溢出子目录（<BusSpillDir>/<agentName> 隔离，防多 agent 事件串）。
	// 全局 BusSpillDir 空则保持空（NewReliableEventBus 回退纯 channel bus，现状零变化）。
	if cfg.Reliability.BusSpillDir != "" {
		agentCfg.BusSpillDir = filepath.Join(cfg.Reliability.BusSpillDir, name)
	}
	// T-G: DegradationManager 注入 agent（event_loop 据此上报 model 依赖退化；nil 若未启用）。
	agentCfg.Degradation = degradationMgr
	// task_terminal_ttl: duration string → time.Duration; empty/invalid falls
	// back to the task package default (2m) via zero value.
	if acfg.TaskTerminalTTL != "" {
		if ttl, err := time.ParseDuration(acfg.TaskTerminalTTL); err == nil && ttl > 0 {
			agentCfg.TaskTerminalTTL = ttl
		} else {
			log.Warnf("[tagent] agent %q: invalid task_terminal_ttl %q, using default", name, acfg.TaskTerminalTTL)
		}
	}
	if summaryModel := rc.resolveSummaryModel(name, acfg, cfg); summaryModel != nil {
		agentCfg.SummaryModel = summaryModel
	}

	// Parse meditation config (string durations → time.Duration)
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
		// Create hot-reloadable source for meditation prompt
		meditationPromptSource := prompt.NewSource(loader, prompt.CompositeConfig{
			Files: []string{promptFile},
		})
		// T-G AnchorStore：per-agent 冥想锚点持久化路径（<MeditationAnchorDir>/<name>.json）。
		// MeditationAnchorDir 空则 AnchorPath 空（冥想锚点纯内存，现状零变化）。
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
		// 4.3（design-report-closeout）：巩固候选清单注入冥想 digest（建议式——
		// 列 key 与计数，执行权在 LLM + memory_consolidate）。tracker nil 时不注入。
		if hintTracker != nil {
			tracker := hintTracker
			pid := memory.PartitionIDFromName(name)
			// M1（独立评审/裁决 Q4）：三来源组合——巩固候选 + 改进评估结论（劣化建议
			// 在下轮反思必现）+ 未登记产物提醒。evolution 关时退回纯候选（现状）。
			evoDigest := ""
			if rc.evoGit != nil {
				evoDigest = rc.evoGit.DigestSummary()
			}
			agentCfg.Meditation.DigestExtra = func() string {
				return tracker.CandidatesText(pid) + evoDigest
			}
		}
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create tagent agent: %w", name, err)
	}

	// D1-B（design-report-closeout）/git-native 4.4：entry agent 双持久化路径盖版本章——
	// 事件归属精确到**最新 improvement 的 commit sha**（guardrail/feedback join 键；键名
	// MetaKeyBundleID 保留，8.4 继承机制不变）。无改进事件时返回空串不盖章（退化时间窗 join）。
	if rc.evoGit != nil && name == cfg.Entry {
		evoGit := rc.evoGit
		ta.SetBundleIDProvider(evoGit.LatestSha)
		// K4：评估 goroutine 生命周期挂 entry agent shutdown（Stop 收敛+waitgroup）。
		ta.RegisterCloser(stopCloser(evoGit.Stop))
	}

	// 3.3（design-report-closeout）：审批请求经消息通道渗透（entry 事件循环 → 渠道侧
	// 送达用户）。Deliver 失败不阻塞门——pending 文件已落盘，CLI/文件批准始终可用。
	if cfg.Governance.Enabled && rc.govGate != nil && name == cfg.Entry && rc.govGate.Approval() != nil {
		rc.govGate.Approval().AddChannel(&approvalInjectChannel{ta: ta})
		// R5：外部审批直投通道（example/宿主经 WithApprovalChannel 注入）。
		for _, ch := range rc.approvalChannels {
			rc.govGate.Approval().AddChannel(ch)
		}
	}

	// 4.2（design-report-closeout）：容量 hint 回填——渗透消息进事件循环（建议式：
	// 执行权在 LLM + memory_consolidate；source=consolidation_hint 供消费端识别）。
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

	// 5.5（design-report-closeout）：mem_spill 重放双写——重放成功的每条事件补投影
	// （projection 此时已由 NewTagentAgent 创建），恢复「存储⇔投影同点」在退化路径
	// 的等价语义（Role 从事件类型派生）。
	if etsHolder != nil {
		etsHolder.SetReplayProjection(func(ev memory.FullEvent) {
			ta.AppendProjectionRef(memory.EventReference{
				EventKey: ev.EventKey, PartitionID: ev.PartitionID,
				EventType: ev.EventType, EventSummary: ev.EventSummary,
				Timestamp: ev.Timestamp, Role: string(tagentevent.EventTypeRole(ev.EventType)),
			})
		})
	}

	// Register ActionTool for cleanup on agent shutdown.
	if actionTool != nil {
		ta.RegisterCloser(actionTool)
	}

	// Register the memory store for graceful shutdown — file-backed stores
	// (FileSegmentStore over LocalFileKV/RustViking) perform their final
	// durability flush in Close. Same-path shared instances are safe: Close
	// is idempotent (closeOnce) and only invoked at process exit.
	if c, ok := memStore.(agent.Closer); ok {
		ta.RegisterCloser(c)
	}

	// Wire parentProjection to AgentToolWrapper instances for auto-inject fallback.
	// This must happen after TagentAgent creation (projection is created inside NewTagentAgent).
	ta.SetToolParentProjection()

	cache[name] = ta
	buildOK = true // 构建成功：取消失败回收 defer（审查 Nit5）
	return ta, nil
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
) (trpctool.Tool, bool, error) {
	desc, err := resolveToolDescription(tr, loader)
	if err != nil {
		return nil, false, err
	}

	switch tr.Kind {
	case ToolKindAgent:
		return buildAgentToolRef(tr, cfg, rc, loader, cache, parentMemStore, desc)
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
) (trpctool.Tool, bool, error) {
	// Remote path: create A2AAgent that communicates via trpc-a2a-go
	if tr.Remote != nil && tr.Remote.URL != "" {
		a2aAgent, err := a2aagent.New(
			a2aagent.WithName(tr.AgentID),
			a2aagent.WithDescription(desc),
			a2aagent.WithAgentCardURL(tr.Remote.URL),
			// TransferStateKey ensures RuntimeState["external_context"] is
			// auto-copied to A2A message metadata. The remote A2A server
			// auto-maps metadata back to RuntimeState (server.go:377).
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
		// Enable hot-reload for tool description if loaded from a file
		if tr.DescriptionFile != "" {
			wrapper.SetDescriptionSource(prompt.NewSource(loader, prompt.CompositeConfig{
				Files: []string{tr.DescriptionFile},
			}))
		}
		log.Infof("[tagent] created remote A2A agent tool: %s → %s", tr.AgentID, tr.Remote.URL)
		return wrapper, false, nil
	}

	// Local path: build the referenced agent recursively
	refCfg, ok := cfg.Agents[tr.AgentID]
	if !ok {
		return nil, false, fmt.Errorf("referenced agent %q not found in config", tr.AgentID)
	}

	subAgent, err := buildAgent(tr.AgentID, refCfg, cfg, rc, loader, cache)
	if err != nil {
		return nil, false, err
	}

	// Wrap with PlanAgent if this is the plan agent — enables dual-mode Run
	// (progress queries bypass LLM via direct file I/O).
	var agentImpl trpcagent.Agent = subAgent
	if tr.AgentID == "plan" {
		agentImpl = plan.NewPlanAgent(subAgent, ".")
	}

	// Wrap with AgentToolWrapper — this replaces agenttool.NewTool().
	wrapper := agent.NewAgentToolWrapper(agentImpl, desc, tr.EventParams, parentMemStore)
	if tr.Async != nil && !*tr.Async {
		wrapper.SetAsyncDisabled(true)
	}
	if len(tr.ExtraParams) > 0 {
		wrapper.SetExtraParams(tr.ExtraParams)
	}
	// Enable hot-reload for tool description if loaded from a file
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
		Degradation:             degradationMgr,                          // T-G: mcp_call 上报 DepMCP 退化（per-agent）
		MCPProbeEvery:           rc.reliability.DegradationMCPProbeEvery, // 5.4: degraded 熔断半开探测
		ConsolidationMinSources: consolidationMinSources,                 // 4.4: min_source_events 硬门控
	}
	// Nil-guard: assigning a typed nil *Registry to the interface field
	// would make cfg.MCPRegistry != nil inside factories.
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

// resolveAgentModel resolves the model for a specific agent.
// Resolution order:
//  1. modelOverrides (pre-resolved instances, e.g., SwappableModel for entry agent)
//  2. If agent has no Model field → use parent model (rc.model)
//  3. Resolve via provider.Model() using the agent's provider+model from config
//  4. On error, fall back to parent model with a warning
