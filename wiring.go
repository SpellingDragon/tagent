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
	// 1. Check overrides (SwappableModel for entry agent, etc.).
	// 5.2 observation-blinding fix: overrides used to early-return WITHOUT the
	// TrajectoryRecorder wrapper, leaving entry-agent LLM calls invisible in
	// the trajectory dump. Wrap here too — OUTSIDE the SwappableModel so the
	// recorder observes post-swap traffic (recorder(Swappable) order); the
	// wrapper is created per buildAgent call, so repeated resolves never
	// stack wrappers on the same instance.
	if rc.modelOverrides != nil {
		if m, ok := rc.modelOverrides[name]; ok {
			if rc.trajectoryRecorder != nil {
				m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
			}
			return m
		}
	}

	// 2. If agent has no model override, resolve the GLOBAL default model
	// (cfg.Provider+cfg.Model) through the provider registry — same factory
	// path as explicit agent models — so yaml-only changes to the global
	// default take effect in hot-reload rebuilds (the WithModel-injected
	// instance is frozen at boot). Falls back to the injected rc.model when
	// the config carries no resolvable global provider (tests/minimal
	// configs): behavior-preserving.
	if acfg.Model == "" {
		if m := rc.resolveGlobalDefaultModel(cfg); m != nil {
			return m
		}
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

// resolveGlobalDefaultModel resolves cfg.Provider+cfg.Model via the provider
// registry (identical resolution to explicit agent models) and caches the
// instance under a reserved key. Returns nil when the config has no global
// provider/model to resolve — the caller then falls back to the
// WithModel-injected instance (rc.model), preserving legacy behavior for
// minimal configs and tests. Resolve failures return nil WITHOUT caching so
// a later hot-reload rebuild can retry (e.g. after env/API key appears).
func (rc *runtimeConfig) resolveGlobalDefaultModel(cfg Config) model.Model {
	if cfg.Model == "" || cfg.Provider == "" {
		return nil
	}
	var opts []provider.Option
	protocolName := cfg.Provider
	endpoint := ""
	if pcfg, ok := cfg.Providers[cfg.Provider]; ok {
		if pcfg.Provider != "" {
			protocolName = pcfg.Provider
		}
		if pcfg.APIEndpoint != "" {
			endpoint = pcfg.APIEndpoint
			opts = append(opts, provider.WithBaseURL(pcfg.APIEndpoint))
		}
		if pcfg.APIKeyEnv != "" {
			if key := os.Getenv(pcfg.APIKeyEnv); key != "" {
				opts = append(opts, provider.WithAPIKey(key))
			}
		}
	}
	// Cache key includes the resolved endpoint: a same-name provider whose
	// api_endpoint changed in yaml must NOT hit the old-instance cache.
	cacheKey := "@@global:" + cfg.Provider + ":" + cfg.Model + ":" + endpoint
	if m, ok := rc.resolvedModels[cacheKey]; ok {
		return m
	}
	m, err := provider.Model(protocolName, cfg.Model, opts...)
	if err != nil {
		log.Debugf("[tagent] global default model %q via provider %q: registry resolve failed (%v); using injected instance", cfg.Model, cfg.Provider, err)
		return nil
	}
	if rc.trajectoryRecorder != nil {
		m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
	}
	if rc.resolvedModels == nil {
		rc.resolvedModels = make(map[string]model.Model)
	}
	rc.resolvedModels[cacheKey] = m
	log.Infof("[tagent] resolved global default model %q via provider %q", cfg.Model, cfg.Provider)
	return m
}

// resolveSummaryModel resolves the summary model for a specific agent.
// Resolution order:
//  1. If agent has SummaryModel field in YAML → resolve via provider (SummaryProvider or agent's Provider)
//  2. If rc.summaryModel is set via Go option → use that
//  3. Otherwise → nil (no summary model)
//
// resolvedModelRef is the outcome of resolving a direct call site's ModelRef:
// the resolved model plus the generation knobs that ride on the request.
type resolvedModelRef struct {
	model  model.Model
	effort *string
}

// resolveModelRef is the single three-tier resolution chain for direct call
// sites (summary compression, evolution judge): explicit ModelRef → owning
// agent's provider/model → global provider/model. Previously only the summary
// site had a fallback chain and the judge was hard-wired to the entry model.
// (tagent-unify-model-call-config.)
func (rc *runtimeConfig) resolveModelRef(ref ModelRef, name string, acfg AgentConfig, cfg Config) *resolvedModelRef {
	out := &resolvedModelRef{effort: ref.ReasoningEffort}

	providerName := ref.Provider
	modelName := ref.Model
	if providerName == "" {
		providerName = acfg.Provider
	}
	if providerName == "" {
		providerName = cfg.Provider
	}
	if modelName == "" {
		modelName = acfg.Model
	}
	if modelName == "" {
		modelName = cfg.Model
	}
	if modelName == "" {
		return nil
	}

	cacheKey := "direct:" + providerName + ":" + modelName
	if m, ok := rc.resolvedModels[cacheKey]; ok {
		out.model = m
		return out
	}

	var opts []provider.Option
	protocolName := providerName
	if pcfg, ok := cfg.Providers[providerName]; ok {
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

	m, err := provider.Model(protocolName, modelName, opts...)
	if err != nil {
		log.Warnf("[tagent] agent %q: resolve direct model %q via %q: %v", name, modelName, providerName, err)
		return nil
	}
	if rc.resolvedModels == nil {
		rc.resolvedModels = make(map[string]model.Model)
	}
	rc.resolvedModels[cacheKey] = m
	log.Infof("[tagent] agent %q: resolved direct model %q via provider %q", name, modelName, providerName)
	out.model = m
	return out
}

// judgeModel resolves the evolution judge's model: explicit evolution.judge
// ModelRef wins; zero value falls back to the entry agent's model (legacy
// hard-wire behavior). (tagent-unify-model-call-config.)
func (rc *runtimeConfig) judgeModel(name string, cfg Config) model.Model {
	entry, ok := cfg.Agents[cfg.Entry]
	if !ok {
		entry = AgentConfig{}
	}
	if ref := rc.resolveModelRef(cfg.Evolution.Judge, name, entry, cfg); ref != nil {
		return ref.model
	}
	return rc.model
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
//
// Shared-path stores go through the RuntimeResources registry (4.2): same
// path + same fingerprint → same instance + one lease per consumer; the LAST
// release closes the store and frees the directory writer-lock (4.3), and an
// incompatible fingerprint is REJECTED (4.1 T3). Empty path = isolated store
// owned exclusively by that agent.
//
// The returned release func is bound to the acquiring agent's lifecycle
// (executed from TagentAgent.Close); executor shells do NOT acquire (they
// borrow the resident entry's store).
func resolveMemoryStore(mc MemoryConfig) (memory.MemoryStore, func(), error) {
	switch mc.Type {
	case "memory", "":
		if mc.Path == "" {
			// Isolated store — no sharing needed
			return memory.NewInMemoryStore(), nil, nil
		}
		// Shared by path → registry lease（4.2：租约化，最后释放才关闭）。
		return defaultResources.acquire("mem", mc.Path, fingerprintMemory(mc), func() (memory.MemoryStore, error) {
			return memory.NewInMemoryStore(), nil
		})
	case "file":
		if mc.Path == "" {
			return nil, nil, fmt.Errorf("file memory store requires path")
		}
		// Shared by path（M-1，四审）→ registry lease（4.2/4.3）。open 闭包含
		// tombstone 恢复 → 计数重建 → 扫描器启动的完整时序（2.8/2.9）。
		return defaultResources.acquire("rv", mc.Path, fingerprintMemory(mc), func() (memory.MemoryStore, error) {
			return openRVStore(mc)
		})
	case "localfile":
		if mc.Path == "" {
			return nil, nil, fmt.Errorf("localfile memory store requires path")
		}
		return defaultResources.acquire("localfile", mc.Path, fingerprintMemory(mc), func() (memory.MemoryStore, error) {
			return openLocalFileStore(mc)
		})
	default:
		return nil, nil, fmt.Errorf("unknown memory store type %q", mc.Type)
	}
}

// openLocalFileStore builds (and fully wires) a localfile-backed store.
func openLocalFileStore(mc MemoryConfig) (memory.MemoryStore, error) {
	rel, err := memory.NewInMemRelationStore(mc.Path)
	if err != nil {
		return nil, fmt.Errorf("create relation store: %w", err)
	}
	kvOpts := []kv.LocalFileKVOption{}
	if mc.FSync != nil && !*mc.FSync {
		kvOpts = append(kvOpts, kv.WithFSync(false)) // nil → default enabled (D1)
	}
	kvStore, err := kv.NewLocalFileKV(mc.Path, kvOpts...)
	if err != nil {
		return nil, fmt.Errorf("create local file kv: %w", err)
	}
	store, err := memory.NewFileSegmentStore(kvStore, rel, mc.Path, 1000)
	if err != nil {
		return nil, fmt.Errorf("create file segment store: %w", err)
	}
	wireStoreLifecycle(store, kvStore, rel, tombstoneOf(store, rel, kvStore, 0), resolveLifecycleConfig(mc.Lifecycle))
	return store, nil
}

// openRVStore builds (and fully wires) a rustviking-backed store.
func openRVStore(mc MemoryConfig) (memory.MemoryStore, error) {
	rel, err := memory.NewInMemRelationStore(mc.Path)
	if err != nil {
		return nil, fmt.Errorf("create relation store: %w", err)
	}
	configPath, err := ensureRustVikingConfig(mc.RustVikingBinary, mc.Path)
	if err != nil {
		return nil, fmt.Errorf("create rustviking config: %w", err)
	}
	kvClient := kv.NewRustVikingClient(mc.RustVikingBinary, configPath)
	store, err := memory.NewFileSegmentStore(kvClient, rel, mc.Path, 1000)
	if err != nil {
		return nil, fmt.Errorf("create file segment store: %w", err)
	}
	wireStoreLifecycle(store, kvClient, rel, tombstoneOf(store, rel, kvClient, 0), resolveLifecycleConfig(mc.Lifecycle))
	return store, nil
}

// tombstoneOf creates + recovers the store-level tombstone set.
func tombstoneOf(store *memory.FileSegmentStore, rel memory.RelationStore, kvStore memory.KVStore, pid int) *memory.TombstoneSet {
	tombstone := memory.NewTombstoneSet(rel, kvStore, pid)
	if err := tombstone.RecoverFromKV(); err != nil {
		log.Warnf("[tagent] tombstone recovery failed (non-fatal): %v", err)
	}
	store.SetTombstoneSet(tombstone)
	return tombstone
}

// wireStoreLifecycle starts tombstone rebuild → scanners in the 2.8 order
// (tombstone recovery happens in tombstoneOf BEFORE this; count rebuild next;
// lifecycle/compaction scanners LAST).
func wireStoreLifecycle(store *memory.FileSegmentStore, kvStore memory.KVStore, rel memory.RelationStore, tombstone *memory.TombstoneSet, lc memory.LifecycleConfig) {
	if err := store.RebuildLiveCounts(); err != nil {
		log.Warnf("[tagent] live-count rebuild failed — capacity eviction paused (counts unknown): %v", err)
	}
	lm := memory.NewLifecycleManager(store, tombstone, lc)
	lm.Start()
	store.SetLifecycleManager(lm)

	compactor := memory.NewCompactor(store, kvStore, rel, tombstone, memory.DefaultCompactionConfig())
	compactor.Start()
	store.SetCompactor(compactor)
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
