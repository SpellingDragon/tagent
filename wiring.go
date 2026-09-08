package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/provider"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
)

func (rc *runtimeConfig) resolveAgentModel(name string, acfg AgentConfig, cfg Config) model.Model {
	// 1. Check overrides (SwappableModel for entry agent, etc.)
	if rc.modelOverrides != nil {
		if m, ok := rc.modelOverrides[name]; ok {
			return m
		}
	}

	// 2. If agent has no model override, use parent model
	if acfg.Model == "" {
		return rc.model
	}

	// 3. Resolve provider+model from config
	providerName := acfg.Provider
	if providerName == "" {
		providerName = cfg.Provider
	}
	cacheKey := providerName + ":" + acfg.Model
	if m, ok := rc.resolvedModels[cacheKey]; ok {
		return m
	}

	// 4. Look up provider connection info and determine protocol implementation
	var opts []provider.Option
	protocolName := providerName // default to registry key name
	if pcfg, ok := cfg.Providers[providerName]; ok {
		// If ProviderConfig specifies a protocol, use it (e.g., "zhipu" -> "openai")
		if pcfg.Provider != "" {
			protocolName = pcfg.Provider
		}
		if pcfg.APIEndpoint != "" {
			opts = append(opts, provider.WithBaseURL(pcfg.APIEndpoint))
		}
		if pcfg.APIKeyEnv != "" {
			if key := os.Getenv(pcfg.APIKeyEnv); key != "" {
				opts = append(opts, provider.WithAPIKey(key))
			}
		}
	}

	m, err := provider.Model(protocolName, acfg.Model, opts...)
	if err != nil {
		log.Warnf("agent %q: resolve model %q via provider %q (protocol %q) failed: %v, falling back to parent model",
			name, acfg.Model, providerName, protocolName, err)
		return rc.model
	}

	// Wrap with TrajectoryRecorder if enabled, so sub-agent LLM calls
	// are also recorded for RL training data.
	if rc.trajectoryRecorder != nil {
		m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
		log.Debugf("[tagent] agent %q: wrapped model %q with TrajectoryRecorder", name, acfg.Model)
	}

	if rc.resolvedModels == nil {
		rc.resolvedModels = make(map[string]model.Model)
	}
	rc.resolvedModels[cacheKey] = m
	log.Infof("[tagent] agent %q: resolved model %q via provider %q", name, acfg.Model, providerName)
	return m
}

// resolveSummaryModel resolves the summary model for a specific agent.
// Resolution order:
//  1. If agent has SummaryModel field in YAML → resolve via provider (SummaryProvider or agent's Provider)
//  2. If rc.summaryModel is set via Go option → use that
//  3. Otherwise → nil (no summary model)
func (rc *runtimeConfig) resolveSummaryModel(name string, acfg AgentConfig, cfg Config) model.Model {
	// Resolve model and provider from compress config.
	// Falls back to agent's main model/provider if compress.summary_model is empty.
	summaryModel := acfg.Compress.SummaryModel
	summaryProvider := acfg.Compress.SummaryProvider

	// 1. If resolved summary_model, resolve it
	if summaryModel != "" {
		// Use summaryProvider if specified, otherwise fall back to agent's Provider or global Provider
		providerName := summaryProvider
		if providerName == "" {
			providerName = acfg.Provider
		}
		if providerName == "" {
			providerName = cfg.Provider
		}
		cacheKey := "summary:" + providerName + ":" + summaryModel
		if m, ok := rc.resolvedModels[cacheKey]; ok {
			return m
		}

		var opts []provider.Option
		protocolName := providerName // default to registry key name
		if pcfg, ok := cfg.Providers[providerName]; ok {
			// If ProviderConfig specifies a protocol, use it (e.g., "zhipu" -> "openai")
			if pcfg.Provider != "" {
				protocolName = pcfg.Provider
			}
			if pcfg.APIEndpoint != "" {
				opts = append(opts, provider.WithBaseURL(pcfg.APIEndpoint))
			}
			if pcfg.APIKeyEnv != "" {
				if key := os.Getenv(pcfg.APIKeyEnv); key != "" {
					opts = append(opts, provider.WithAPIKey(key))
				}
			}
		}

		m, err := provider.Model(protocolName, summaryModel, opts...)
		if err != nil {
			log.Warnf("agent %q: resolve summary model %q via provider %q (protocol %q) failed: %v, falling back to rc.summaryModel",
				name, summaryModel, providerName, protocolName, err)
			return rc.summaryModel
		}

		if rc.trajectoryRecorder != nil {
			m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
			log.Debugf("[tagent] agent %q: wrapped summary model %q with TrajectoryRecorder", name, summaryModel)
		}

		if rc.resolvedModels == nil {
			rc.resolvedModels = make(map[string]model.Model)
		}
		rc.resolvedModels[cacheKey] = m
		log.Infof("[tagent] agent %q: resolved summary model %q via provider %q", name, summaryModel, providerName)
		return m
	}

	// 2. Fall back to Go option
	return rc.summaryModel
}

// resolveLifecycleConfig merges the optional YAML lifecycle declaration over
// the built-in defaults. Nil or partially-set fields keep defaults; a
// negative GlobalTTLDays disables TTL-based forgetting entirely.
func resolveLifecycleConfig(c *LifecycleConfig) memory.LifecycleConfig {
	cfg := memory.DefaultLifecycleConfig()
	if c == nil {
		return cfg
	}
	if c.GlobalTTLDays != nil {
		cfg.GlobalTTLDays = *c.GlobalTTLDays
	}
	if len(c.TypeTTL) > 0 {
		if cfg.TypeTTL == nil {
			cfg.TypeTTL = make(map[string]int, len(c.TypeTTL))
		}
		for k, v := range c.TypeTTL {
			cfg.TypeTTL[k] = v
		}
	}
	if c.CheckInterval != "" {
		if d, err := time.ParseDuration(c.CheckInterval); err == nil && d > 0 {
			cfg.CheckInterval = d
		} else {
			log.Warnf("[tagent] invalid lifecycle check_interval %q, keeping default", c.CheckInterval)
		}
	}
	if c.MaxEventsPerPartition != nil {
		cfg.MaxEventsPerPartition = *c.MaxEventsPerPartition
	}
	return cfg
}

// resolveMemoryStore creates a MemoryStore from MemoryConfig.
//
// For type: file, creates a FileSegmentStore backed by RustViking CLI
// and InMemRelationStore (WAL + snapshot persistence).
//
// For type: localfile, creates a FileSegmentStore backed by LocalFileKV
// (JSON file persistence, no external binary dependency) and InMemRelationStore.
// Same path → same FileSegmentStore instance (shared via namedFileStores registry).
//
// For type: memory, when a non-empty path is provided, the same path
// returns the same InMemoryStore instance (shared via registry).
// An empty path creates an isolated store — suitable for agents that
// don't need cross-agent memory access (e.g., knowledge agent).
func resolveMemoryStore(mc MemoryConfig) (memory.MemoryStore, error) {
	switch mc.Type {
	case "memory", "":
		if mc.Path == "" {
			// Isolated store — no sharing needed
			return memory.NewInMemoryStore(), nil
		}
		// Shared by path: same path → same InMemoryStore instance
		namedMemMu.Lock()
		defer namedMemMu.Unlock()
		if s, ok := namedMemStores[mc.Path]; ok {
			return s, nil
		}
		s := memory.NewInMemoryStore()
		namedMemStores[mc.Path] = s
		return s, nil
	case "file":
		if mc.Path == "" {
			return nil, fmt.Errorf("file memory store requires path")
		}
		// Shared by path（M-1，四审）：与 localfile 同构——同 path 同实例，防跨 agent
		// read_namespaces 下 RelationStore 内存图分歧（因果链断链）与双 Compactor 并发覆盖。
		namedRVMu.Lock()
		defer namedRVMu.Unlock()
		if s, ok := namedRVStores[mc.Path]; ok {
			return s, nil
		}
		rel, err := memory.NewInMemRelationStore(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create relation store: %w", err)
		}
		configPath, err := ensureRustVikingConfig(mc.RustVikingBinary, mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create rustviking config: %w", err)
		}
		kv := kv.NewRustVikingClient(mc.RustVikingBinary, configPath)
		store, err := memory.NewFileSegmentStore(kv, rel, mc.Path, 1000)
		if err != nil {
			return nil, fmt.Errorf("create file segment store: %w", err)
		}

		// Wire up lifecycle components: TombstoneSet → LifecycleManager → Compactor
		tombstone := memory.NewTombstoneSet(rel, kv, 0) // pid=0 for store-level tombstones
		if err := tombstone.RecoverFromKV(); err != nil {
			log.Warnf("[tagent] tombstone recovery failed (non-fatal): %v", err)
		}
		store.SetTombstoneSet(tombstone)

		lm := memory.NewLifecycleManager(store, tombstone, resolveLifecycleConfig(mc.Lifecycle))
		lm.Start()
		store.SetLifecycleManager(lm)

		compactor := memory.NewCompactor(store, kv, rel, tombstone, memory.DefaultCompactionConfig())
		compactor.Start()
		store.SetCompactor(compactor)

		namedRVStores[mc.Path] = store
		return store, nil
	case "localfile":
		if mc.Path == "" {
			return nil, fmt.Errorf("localfile memory store requires path")
		}
		// Shared by path: same path → same FileSegmentStore instance
		// (so recall can read tagent's partition via read_namespaces)
		namedFileMu.Lock()
		defer namedFileMu.Unlock()
		if s, ok := namedFileStores[mc.Path]; ok {
			return s, nil
		}
		rel, err := memory.NewInMemRelationStore(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create relation store: %w", err)
		}
		kv, err := kv.NewLocalFileKV(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create local file kv: %w", err)
		}
		store, err := memory.NewFileSegmentStore(kv, rel, mc.Path, 1000)
		if err != nil {
			return nil, fmt.Errorf("create file segment store: %w", err)
		}

		// Wire up lifecycle components: TombstoneSet → LifecycleManager → Compactor
		tombstone := memory.NewTombstoneSet(rel, kv, 0)
		if err := tombstone.RecoverFromKV(); err != nil {
			log.Warnf("[tagent] tombstone recovery failed (non-fatal): %v", err)
		}
		store.SetTombstoneSet(tombstone)

		lm := memory.NewLifecycleManager(store, tombstone, resolveLifecycleConfig(mc.Lifecycle))
		lm.Start()
		store.SetLifecycleManager(lm)

		compactor := memory.NewCompactor(store, kv, rel, tombstone, memory.DefaultCompactionConfig())
		compactor.Start()
		store.SetCompactor(compactor)

		namedFileStores[mc.Path] = store
		return store, nil
	default:
		return nil, fmt.Errorf("unknown memory store type %q", mc.Type)
	}
}

// wireMemoryEngine 按 MemoryConfig.Engine 为 store 包裹记忆引擎（T-A 解耦缝）。
// 未配置 Engine 或无 Embedding → 返回原 store（纯关键词，行为逐字节不变）。
// 共享 store（path 非空）的引擎按 path 共享（namedEngines），保跨 agent 语义召回一致。
// 嵌入器初始化失败（如无 API key）→ 优雅降级：记录并返回原 store（不阻断 agent 构建）。
func wireMemoryEngine(store memory.MemoryStore, mc MemoryConfig, onStoreEvent func(eventKey int64, partitionID int, eventType string)) (memory.MemoryStore, error) {
	hasEngine := mc.Engine != nil && mc.Engine.Embedding != nil
	if !hasEngine && onStoreEvent == nil {
		return store, nil
	}
	if !hasEngine {
		return wrapCapacityOnly(store, onStoreEvent), nil
	}
	if mc.Path != "" {
		// 引擎缓存键含 backend/model/dimensions（审查 Nit6）：同 path 但不同引擎配置
		// 不串用（否则会静默复用一个语义不同的引擎）。
		cacheKey := engineCacheKey(mc)
		namedEngineMu.Lock()
		defer namedEngineMu.Unlock()
		if eng, ok := namedEngines[cacheKey]; ok {
			return newEngineBridgeWithRemover(store, eng, onStoreEvent), nil
		}
		eng, err := buildMemoryEngine(store, *mc.Engine)
		if err != nil {
			// 8.10（review §8）：embedding 构建失败只降级向量能力——capacityHook
			//（巩固触发）不得连带丢失（「触发不依赖 embedding」承诺）。
			log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
			return wrapCapacityOnly(store, onStoreEvent), nil
		}
		namedEngines[cacheKey] = eng
		return newEngineBridgeWithRemover(store, eng, onStoreEvent), nil
	}
	eng, err := buildMemoryEngine(store, *mc.Engine)
	if err != nil {
		// 8.10：同上——降级路径保 capacityHook。
		log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
		return wrapCapacityOnly(store, onStoreEvent), nil
	}
	return newEngineBridgeWithRemover(store, eng, onStoreEvent), nil
}

// engineCacheKey 构造共享引擎缓存键：path + backend + embedding model/dimensions
// （审查 Nit6：同 path 不同引擎配置不串用）。
func engineCacheKey(mc MemoryConfig) string {
	backend, model, dims := "", "", 0
	if mc.Engine != nil {
		backend = mc.Engine.Backend
		if mc.Engine.Embedding != nil {
			model = mc.Engine.Embedding.Model
			dims = mc.Engine.Embedding.Dimensions
		}
	}
	return fmt.Sprintf("%s|%s|%s|%d", mc.Path, backend, model, dims)
}

// newEngineBridgeWithRemover 创建 engineBridge 并把向量移除回调接到 base store（若支持
// SetVectorRemover）——使 TTL/容量遗忘物理删除事件时同步移除向量（内存索引 + KV 持久键），
// 消除 engine.Remove 死代码、防死键堆积与重启复活（审查 M2）。
func newEngineBridgeWithRemover(store memory.MemoryStore, eng memory.MemoryEngine, onStoreEvent func(eventKey int64, partitionID int, eventType string)) memory.MemoryStore {
	bridge := engine.NewEngineBridge(store, eng)
	if onStoreEvent != nil {
		if provider, ok := bridge.(memory.CapacityHookProvider); ok {
			provider.SetCapacityHook(onStoreEvent) // 4.2: 容量触发计数点
		}
	}
	if setter, ok := store.(interface{ SetVectorRemover(memory.VectorRemover) }); ok {
		if vr, ok := bridge.(memory.VectorRemover); ok {
			setter.SetVectorRemover(vr)
		}
	}
	return bridge
}

// buildMemoryEngine 按配置构建嵌入器 + 引擎。嵌入器不可用（无 key）时返回 error（调用方降级）。
func buildMemoryEngine(store memory.MemoryStore, ec MemoryEngineConfig) (memory.MemoryEngine, error) {
	emb, err := buildEmbedder(*ec.Embedding)
	if err != nil {
		return nil, err
	}
	ecfg := engine.EngineConfig{
		VectorTopK:  ec.VectorTopK,
		KeywordTopK: ec.KeywordTopK,
		RRFK:        ec.RRFK,
	}
	// 持久化：store 若提供底层 KV（FileSegmentStore over rustviking/LocalFileKV），
	// 引擎向量序列化入 KV + 启动重建——跨重启恢复语义召回。纯内存 store 无 KV → 不持久。
	if kvp, ok := store.(memory.KVProvider); ok {
		ecfg.KV = kvp.KVBackend()
	}
	switch ec.Backend {
	case "", "memory":
		// MVP 内存向量索引 + 可选 KV 持久化。
		return engine.NewInMemoryEngine(store, emb, ecfg), nil
	case "rustviking":
		// S1: rustviking 后端在 MVP 阶段等价 memory 引擎（内存向量索引 + rustviking KV 持久化
		// 向量 + 启动重建，依据 F1 报告：rustviking 原生 index CLI 进程内易失）。**显式告警**
		// 避免"配了 rustviking 却静默得到 memory 引擎"的假自由度错觉；原生 HNSW/IVF 索引持久化
		// （接入 ivf_persist）为 rustviking backlog。
		log.Warnf("[tagent] memory engine backend=rustviking → MVP 阶段等价 memory 引擎（内存向量索引 + rustviking KV 持久化）；原生 HNSW/IVF 索引持久化为 rustviking backlog（见 f1-rustviking-capability-report.md）")
		return engine.NewInMemoryEngine(store, emb, ecfg), nil
	default:
		return nil, fmt.Errorf("unknown memory engine backend %q", ec.Backend)
	}
}

// buildEmbedder 按配置构建嵌入器。zhipu 无 key 时返回 error（调用方优雅降级）。
func buildEmbedder(ec EmbeddingConfig) (memory.Embedder, error) {
	var inner memory.Embedder
	switch ec.Provider {
	case "mock":
		dim := ec.Dimensions
		if dim <= 0 {
			dim = 64
		}
		inner = membed.NewMockEmbedder(dim)
	case "", "zhipu":
		z, err := membed.NewZhipuEmbedder(membed.ZhipuEmbedderConfig{
			Endpoint:   ec.Endpoint,
			Model:      ec.Model,
			APIKeyEnv:  ec.APIKeyEnv,
			Dimensions: ec.Dimensions,
		})
		if err != nil {
			return nil, err
		}
		inner = z
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", ec.Provider)
	}
	// 组8 向量链路可观测：TracedEmbedder 统一包裹（embedding span + GenAI 属性 + counter/
	// histogram）。noop 安全——未设 OTLP 时零开销、Embed 行为逐字节不变。
	return membed.NewTracedEmbedder(inner), nil
}

// ensureRustVikingConfig writes a rustviking config.toml to the data directory
// and returns the config file path. If the file already exists, it is reused.
func ensureRustVikingConfig(binary, dataDir string) (string, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dataDir, err)
	}
	configPath := filepath.Join(dataDir, "rustviking.toml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

	// Write default config
	config := fmt.Sprintf(`[storage]
path = "%s"
create_if_missing = true
max_open_files = 10000

[vector_store]
plugin = "memory"

[embedding]
plugin = "mock"
`, filepath.Join(dataDir, "rocksdb"))
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return "", fmt.Errorf("write config %s: %w", configPath, err)
	}
	return configPath, nil
}

// resolveToolDescription resolves the tool description from inline text or file.
func resolveToolDescription(tr ToolRef, loader *prompt.Loader) (string, error) {
	if tr.Description != "" {
		return tr.Description, nil
	}
	if tr.DescriptionFile != "" {
		desc, err := loader.LoadFromFile(tr.DescriptionFile)
		if err != nil {
			return "", fmt.Errorf("load description file %q: %w", tr.DescriptionFile, err)
		}
		return desc, nil
	}
	// For tool-kind tools, description is optional — the tool's built-in
	// description from trpc-agent-go will be used if not provided.
	return "", nil
}

// buildDegradationBehaviors (5.4, design-report-closeout) maps ReliabilityConfig
// behavior fields into the agent-side struct. Invalid duration → zero (behavior
// off, warn logged once at load time by config Validate).
func buildDegradationBehaviors(rc ReliabilityConfig) agent.DegradationBehaviors {
	var behaviors agent.DegradationBehaviors
	if d, err := time.ParseDuration(rc.DegradationModelBackoff); err == nil && d > 0 {
		behaviors.ModelBackoff = d
	} else if rc.DegradationModelBackoff != "" {
		log.Warnf("[tagent] invalid degradation_model_backoff %q, model backoff disabled", rc.DegradationModelBackoff)
	}
	behaviors.MCPProbeEvery = rc.DegradationMCPProbeEvery
	behaviors.DiskBlockSpawn = rc.DegradationDiskBlockSpawn
	return behaviors
}

// consolidationMinSources（4.4 design-report-closeout）提取该 agent 的
// memory.engine.consolidation.min_source_events（nil 链安全，缺省 0=不校验）。
func consolidationMinSources(acfg AgentConfig) int {
	if acfg.Memory.Engine == nil || acfg.Memory.Engine.Consolidation == nil {
		return 0
	}
	c := *acfg.Memory.Engine.Consolidation
	// 8.9（review §8）：min_source 硬门控只依赖自己的字段——其余字段非法独立降级
	//（此前整体 Validate 一票否决：snooze 拼错即静默关闭安全门）。
	if c.MinSourceEvents < 0 {
		log.Warnf("[tagent] consolidation.min_source_events < 0; min_source gate disabled")
		return 0
	}
	return c.MinSourceEvents
}

// newConsolidationHintTracker（4.2 design-report-closeout）从 agent 配置构造容量
// 触发器；配置缺失/非法/threshold<=0 返回 nil（关闭，零行为变化）。
func newConsolidationHintTracker(acfg AgentConfig) *ConsolidationHintTracker {
	if acfg.Memory.Engine == nil || acfg.Memory.Engine.Consolidation == nil {
		return nil
	}
	c := *acfg.Memory.Engine.Consolidation
	if err := c.Validate(); err != nil {
		log.Warnf("[tagent] invalid consolidation config (%v); capacity hint disabled", err)
		return nil
	}
	var snooze time.Duration
	if c.Snooze != "" {
		if d, err := time.ParseDuration(c.Snooze); err == nil {
			snooze = d
		}
	}
	return NewConsolidationHintTracker(c.CapacityThreshold, snooze)
}

// approvalInjectChannel（3.3 design-report-closeout）把 pending 审批请求渗透为
// external_input 消息（source=approval）进 entry 事件循环——渠道层（微信等）随
// 普通回复送达用户；用户回复 approve/reject <digest> 由渠道侧 listener 经
// governance.ParseApprovalReply + governance.RespondFile 落盘生效。
type approvalInjectChannel struct {
	ta *agent.TagentAgent
}

// Deliver 实现 governance.ApprovalChannel。永不返回错误阻塞门（闸不是墙）：
// 注入失败仅记日志，pending 文件已在 approvals 目录等待 CLI/文件批准。
func (c *approvalInjectChannel) Deliver(req *governance.ApprovalRequest) error {
	if c == nil || c.ta == nil || req == nil {
		return nil
	}
	short := req.ArgsDigest
	if len(short) > 12 {
		short = short[:12]
	}
	c.ta.InjectMessageWithSource("approval", model.Message{
		Role: model.RoleUser,
		Content: fmt.Sprintf("[approval_request] critical 操作等待人工批准：tool=%s risk=%s reason=%s digest=%s。"+
			"批准请回复 approve %s（或 CLI：wechat-bot approve %s）；拒绝请回复 reject %s。%s 后过期。",
			req.ToolName, req.RiskLevel, req.Reason, short, short, short, short,
			time.Until(time.UnixMilli(req.ExpiresMs)).Round(time.Minute)),
	})
	return nil
}

// wrapCapacityOnly（4.2/8.10 design-report-closeout）包一层无引擎的 bridge：
// 仅提供 capacityHook 写入旁路计数点（Index 跳过、向量方法退 inner）。hook 为 nil
// 时原样返回（零装饰）。
func wrapCapacityOnly(store memory.MemoryStore, onStoreEvent func(int64, int, string)) memory.MemoryStore {
	if onStoreEvent == nil {
		return store
	}
	bridge := engine.NewEngineBridge(store, nil)
	if provider, ok := bridge.(memory.CapacityHookProvider); ok {
		provider.SetCapacityHook(onStoreEvent)
	}
	return bridge
}

// stopCloser 适配 func() → agent.Closer（K4：GitEvolution.Stop 挂 shutdown 链）。
type stopCloser func()

func (f stopCloser) Close() error { f(); return nil }
