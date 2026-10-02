// 契约: docs/wiki/platform/platform-subsystems.md#model-wiring
package tagent

import (
	"context"
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

// resolveAgentModel returns the model instance one agent LLM calls use and caches it
// per provider plus model pair.
//
// - Resolution order: per-name override, then the agent own model, then the global default model, then the WithModel-injected instance.
// - A TrajectoryRecorder, when enabled, wraps every returned instance including override hits, outside the SwappableModel so it observes post-swap traffic.
// 契约: docs/wiki/platform/platform-subsystems.md#model-wiring
func (rc *runtimeConfig) resolveAgentModel(name string, acfg AgentConfig, cfg Config) model.Model {
	if rc.modelOverrides != nil {
		if m, ok := rc.modelOverrides[name]; ok {
			if rc.trajectoryRecorder != nil {
				m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
			}
			return m
		}
	}

	if acfg.Model == "" {
		if m := rc.resolveGlobalDefaultModel(cfg); m != nil {
			return m
		}
		return rc.model
	}

	providerName := acfg.Provider
	if providerName == "" {
		providerName = cfg.Provider
	}
	cacheKey := providerName + ":" + acfg.Model
	if m, ok := rc.resolvedModels[cacheKey]; ok {
		return m
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

	m, err := provider.Model(protocolName, acfg.Model, opts...)
	if err != nil {
		log.Warnf("agent %q: resolve model %q via provider %q (protocol %q) failed: %v, falling back to parent model",
			name, acfg.Model, providerName, protocolName, err)
		return rc.model
	}

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
// provider/model to resolve — the caller then falls back to the injected
// rc.model, which keeps minimal configs and tests working. A resolve failure
// returns nil WITHOUT caching so a later hot-reload rebuild can retry
// (for example once the API-key environment variable appears). The cache key
// includes the resolved endpoint, so changing api_endpoint for a same-name
// provider cannot hit a stale cached instance.
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

// resolvedModelRef is the outcome of resolving a direct call site's ModelRef:
// the resolved model plus the generation knobs that ride on the request.
type resolvedModelRef struct {
	model  model.Model
	effort *string
}

// resolveModelRef is the single three-tier resolution chain for direct call
// sites (summary compression, evolution judge): explicit ModelRef → owning
// agent’s provider/model → global provider/model.
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
// ModelRef wins; a zero value falls back to the entry agent’s model.
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
// - type file backs FileSegmentStore with the RustViking CLI; type localfile backs it with LocalFileKV, which has no external binary dependency; both pair with InMemRelationStore.
// - Shared paths go through the RuntimeResources registry: same path plus same fingerprint yields the same instance with one lease per consumer.
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func resolveMemoryStore(mc MemoryConfig) (memory.MemoryStore, memory.MemoryEngine, func() error, error) {
	switch mc.Type {
	case "memory", "":
		if mc.Path == "" {
			return memory.NewInMemoryStore(), nil, nil, nil
		}
		return defaultResources.acquire("mem", mc.Path, fingerprintMemory(mc), func() (openedResource, error) {
			store := memory.NewInMemoryStore()
			return openedResource{store: store, engine: buildSharedEngine(store, mc)}, nil
		})
	case "file":
		if mc.Path == "" {
			return nil, nil, nil, fmt.Errorf("file memory store requires path")
		}
		return defaultResources.acquire("rv", mc.Path, fingerprintMemory(mc), func() (openedResource, error) {
			return openRVStore(mc)
		})
	case "localfile":
		if mc.Path == "" {
			return nil, nil, nil, fmt.Errorf("localfile memory store requires path")
		}
		return defaultResources.acquire("localfile", mc.Path, fingerprintMemory(mc), func() (openedResource, error) {
			return openLocalFileStore(mc)
		})
	default:
		return nil, nil, nil, fmt.Errorf("unknown memory store type %q", mc.Type)
	}
}

// openLocalFileStore builds a localfile-backed shared resource in the
// construction order (see buildSharedResource). It opens the backend only
// (rel+kv+store); a backend step that fails releases the relation store AND
// the KV opened by prior steps, so a half-built store never leaks a writer
// lock or a journal fd behind it.
func openLocalFileStore(mc MemoryConfig) (openedResource, error) {
	rel, err := memory.NewInMemRelationStore(mc.Path)
	if err != nil {
		return openedResource{}, fmt.Errorf("create relation store: %w", err)
	}
	kvStore, err := kv.NewLocalFileKV(mc.Path)
	if err != nil {
		releaseRelOnFailure(rel)
		return openedResource{}, fmt.Errorf("create local file kv: %w", err)
	}
	store, err := memory.NewFileSegmentStore(kvStore, rel, mc.Path, 1000)
	if err != nil {
		releaseRelOnFailure(rel)
		if cerr := closeKV(kvStore); cerr != nil {
			return openedResource{}, fmt.Errorf("create file segment store: %w; %w", err, fmt.Errorf("%w: kv close: %v", ErrReclaimUnconfirmed, cerr))
		}
		return openedResource{}, fmt.Errorf("create file segment store: %w", err)
	}
	return buildSharedResource(store, kvStore, rel, mc), nil
}

// openRVStore builds a rustviking-backed shared resource in the
// construction order (see buildSharedResource), with the same KV-leak guard as
// openLocalFileStore on a mid-build failure.
func openRVStore(mc MemoryConfig) (openedResource, error) {
	rel, err := memory.NewInMemRelationStore(mc.Path)
	if err != nil {
		return openedResource{}, fmt.Errorf("create relation store: %w", err)
	}
	configPath, err := ensureRustVikingConfig(mc.RustVikingBinary, mc.Path)
	if err != nil {
		releaseRelOnFailure(rel)
		return openedResource{}, fmt.Errorf("create rustviking config: %w", err)
	}
	kvClient := kv.NewRustVikingClient(mc.RustVikingBinary, configPath)
	store, err := memory.NewFileSegmentStore(kvClient, rel, mc.Path, 1000)
	if err != nil {
		releaseRelOnFailure(rel)
		if cerr := closeKV(kvClient); cerr != nil {
			return openedResource{}, fmt.Errorf("create file segment store: %w; %w", err, fmt.Errorf("%w: kv close: %v", ErrReclaimUnconfirmed, cerr))
		}
		return openedResource{}, fmt.Errorf("create file segment store: %w", err)
	}
	return buildSharedResource(store, kvClient, rel, mc), nil
}

// releaseRelOnFailure closes a relation store whose owning build failed.
// Once construction returns an error nobody else holds the handle, so every
// failed rebuild would strand one journal fd unless this path releases it
// (hot-swap amplifies the leak per generation). A failed release is logged —
// the construction error remains the primary.
func releaseRelOnFailure(rel *memory.InMemRelationStore) {
	if rel == nil {
		return
	}
	if err := rel.Close(); err != nil {
		log.Warnf("[tagent] relation store release on failed build: %v", err)
	}
}

// closeKV releases a KV backend whose store construction failed BEFORE the
// FileSegmentStore took ownership of it (once owned, store.Close flushes it).
// No-op for backends without an explicit Close (e.g. RustVikingClient). A
// failed Close is reported so the caller can mark the reclaim UNCONFIRMED —
// an unconfirmed reclaim must never leave the writer open.
func closeKV(k memory.KVStore) error {
	if c, ok := k.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
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

// buildSharedResource finishes a shared backend's wiring in the
// construction order: recover tombstones → rebuild live counts →
// build the entry-owned engine and attach its vector-remover callback to the
// base store → start background producers LAST. Producers run after the
// callback so the compactor/lifecycle scanners can never physically forget a
// vector before the engine's RemoveVector is wired (the orphan-vector window).
// engine==nil signals embedding degradation (keyword-only + capacity hook
// retained, 8.10), never a hard failure — a degraded entry is still published
// and closed as one generation.
//
// The shared retention lease is attached un-armed before producers start, so the
// lifecycle scanner's first destructive pass gates on Lease.Ready() (restart race). The
// durable recovery owner (the agent's reliable inbox / mem_spill) arms it at agent-open
// after rebuilding from on-disk unacked material; a startup grace (memory.lifecycle armGrace)
// backstops a durable backend that never registers a recovery owner so it cannot starve.
func buildSharedResource(store *memory.FileSegmentStore, kvStore memory.KVStore, rel memory.RelationStore, mc MemoryConfig) openedResource {
	tombstone := tombstoneOf(store, rel, kvStore, 0)
	if err := store.RebuildLiveCounts(); err != nil {
		log.Warnf("[tagent] live-count rebuild failed — capacity eviction paused (counts unknown): %v", err)
	}
	eng := buildSharedEngine(store, mc)
	store.SetRetentionLease(memory.NewRetentionLease())
	startStoreProducers(store, kvStore, rel, tombstone, resolveLifecycleConfig(mc.Lifecycle))
	return openedResource{store: store, engine: eng}
}

// startStoreProducers starts the lifecycle scanner and compactor. It MUST run
// after engine + vector-remover wiring so every forgetting scan can physically
// remove vectors from the moment it begins (2.8 order: recovery → engine →
// producers last).
func startStoreProducers(store *memory.FileSegmentStore, kvStore memory.KVStore, rel memory.RelationStore, tombstone *memory.TombstoneSet, lc memory.LifecycleConfig) {
	lm := memory.NewLifecycleManager(store, tombstone, lc)
	lm.Start()
	store.SetLifecycleManager(lm)

	compactor := memory.NewCompactor(store, kvStore, rel, tombstone, memory.DefaultCompactionConfig())
	compactor.Start()
	store.SetCompactor(compactor)
}

// wireMemoryEngine 按 MemoryConfig.Engine 为 store 包裹记忆引擎（T-A 解耦缝）。
// 未配置 Engine 或无 Embedding → 返回原 store（纯关键词，行为逐字节不变）。
// 共享 store（path 非空）的引擎由 registry entry 拥有（resolveMemoryStore 的 open
// 闭包同代构造，经 sharedEngine 传入）——本 agent 仅借桥获得独立能力钩子
// （capacityHook），桥不拥有共享 engine 的关闭权。sharedEngine==nil 表 entry
// 构建降级或无配置 → 只保留 capacityHook（向量能力降级，8.10 承诺不变）。
// 空 path（独享）→ 本 agent 自建并拥有 per-agent 引擎（桥持有并关闭）。
func wireMemoryEngine(store memory.MemoryStore, sharedEngine memory.MemoryEngine, mc MemoryConfig, onStoreEvent func(eventKey int64, partitionID int, eventType string)) (memory.MemoryStore, error) {
	hasEngine := mc.Engine != nil && mc.Engine.Embedding != nil
	if !hasEngine && onStoreEvent == nil {
		return store, nil
	}
	if !hasEngine {
		return wrapCapacityOnly(store, onStoreEvent), nil
	}
	if mc.Path != "" {
		if sharedEngine == nil {
			return wrapCapacityOnly(store, onStoreEvent), nil
		}
		return newEngineBridgeBorrow(store, sharedEngine, onStoreEvent), nil
	}
	eng, err := buildMemoryEngine(store, *mc.Engine)
	if err != nil {
		log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
		return wrapCapacityOnly(store, onStoreEvent), nil
	}
	return newEngineBridgeWithRemover(store, eng, onStoreEvent), nil
}

// buildSharedEngine 为共享 store（path 非空）构造 entry 拥有的引擎，并把 base
// store 的向量移除器接到该引擎——使 TTL/容量遗忘物理删除时移除 entry 引擎的向量
// （而非某 agent 借桥的向量，消除「末位 bridge 覆盖」歧义）。未配置引擎或构建失败
// （如无 embedding key）→ 返回 nil 并降级为纯关键词（调用方保 capacityHook），
// 不缓存悬挂引擎；仅 reopen（新代）才重试构造。
func buildSharedEngine(store memory.MemoryStore, mc MemoryConfig) memory.MemoryEngine {
	if mc.Engine == nil || mc.Engine.Embedding == nil {
		return nil
	}
	eng, err := buildMemoryEngine(store, *mc.Engine)
	if err != nil {
		log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
		return nil
	}
	if setter, ok := store.(interface{ SetVectorRemover(memory.VectorRemover) }); ok {
		setter.SetVectorRemover(sharedEngineRemover{eng})
	}
	return eng
}

// sharedEngineRemover 把 entry 拥有的引擎适配为 base store 的 VectorRemover 回调
// （物理删除 → engine.Remove，移除内存索引 + KV 持久向量）。
type sharedEngineRemover struct{ eng memory.MemoryEngine }

func (r sharedEngineRemover) RemoveVector(eventKey int64) {
	if r.eng != nil {
		_ = r.eng.Remove(context.Background(), eventKey)
	}
}

// newEngineBridgeWithRemover 创建 engineBridge 并把向量移除回调接到 base store（若支持
// SetVectorRemover）——使 TTL/容量遗忘物理删除事件时同步移除向量（内存索引 + KV 持久键），
// 消除 engine.Remove 死代码、防死键堆积与重启复活（审查 M2）。onStoreEvent 非空时，容量触发计数即由该处接入 bridge（唯一计数点）。
func newEngineBridgeWithRemover(store memory.MemoryStore, eng memory.MemoryEngine, onStoreEvent func(eventKey int64, partitionID int, eventType string)) memory.MemoryStore {
	bridge := engine.NewEngineBridge(store, eng)
	if onStoreEvent != nil {
		if provider, ok := bridge.(memory.CapacityHookProvider); ok {
			provider.SetCapacityHook(onStoreEvent)
		}
	}
	if setter, ok := store.(interface{ SetVectorRemover(memory.VectorRemover) }); ok {
		if vr, ok := bridge.(memory.VectorRemover); ok {
			setter.SetVectorRemover(vr)
		}
	}
	return bridge
}

// newEngineBridgeBorrow 为共享 store 创建借用的 engineBridge：仅注册本 agent 独立的
// capacityHook，不重设 base store 的向量移除器（该移除器已由 buildSharedEngine 接到
// entry 拥有的共享引擎——借桥不拥有共享引擎的关闭/移除权）。
func newEngineBridgeBorrow(store memory.MemoryStore, eng memory.MemoryEngine, onStoreEvent func(eventKey int64, partitionID int, eventType string)) memory.MemoryStore {
	bridge := engine.NewEngineBridge(store, eng)
	if onStoreEvent != nil {
		if provider, ok := bridge.(memory.CapacityHookProvider); ok {
			provider.SetCapacityHook(onStoreEvent)
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
	if kvp, ok := store.(memory.KVProvider); ok {
		ecfg.KV = kvp.KVBackend()
	}
	switch ec.Backend {
	case "", "memory":
		return engine.NewInMemoryEngine(store, emb, ecfg), nil
	case "rustviking":
		log.Warnf("[tagent] memory engine backend=rustviking → MVP 阶段等价 memory 引擎（内存向量索引 + rustviking KV 持久化）；原生 HNSW/IVF 索引持久化为 rustviking backlog（见 f1-rustviking-capability-report.md）")
		return engine.NewInMemoryEngine(store, emb, ecfg), nil
	default:
		return nil, fmt.Errorf("unknown memory engine backend %q", ec.Backend)
	}
}

// buildEmbedder 按配置构建嵌入器。zhipu 无 key 时返回 error（调用方优雅降级）。
// 返回值一律由 TracedEmbedder 包裹（可观测与 noop 语义见该类型 doc，不在此重述）。
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
	return membed.NewTracedEmbedder(inner), nil
}

// ensureRustVikingConfig writes a rustviking config.toml to the data directory
// and returns the config file path. If the file already exists, it is reused.
func ensureRustVikingConfig(binary, dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dataDir, err)
	}
	configPath := filepath.Join(dataDir, "rustviking.toml")

	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

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
// An empty result is not an error: a kind=tool entry with neither inline text nor
// description_file keeps the built-in description it ships with (from trpc-agent-go).
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
	return "", nil
}

// buildDegradationBehaviors maps ReliabilityConfig
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

// consolidationMinSources提取该 agent 的
// memory.engine.consolidation.min_source_events（nil 链安全，缺省 0=不校验）。
// A negative value disables this gate and is logged; each consolidation field is
// judged on its own, so an invalid sibling field never switches this gate off.
func consolidationMinSources(acfg AgentConfig) int {
	if acfg.Memory.Engine == nil || acfg.Memory.Engine.Consolidation == nil {
		return 0
	}
	c := *acfg.Memory.Engine.Consolidation
	if c.MinSourceEvents < 0 {
		log.Warnf("[tagent] consolidation.min_source_events < 0; min_source gate disabled")
		return 0
	}
	return c.MinSourceEvents
}

// newConsolidationHintTracker从 agent 配置构造容量
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

// approvalInjectChannel把 pending 审批请求渗透为
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

// wrapCapacityOnly包一层无引擎的 bridge：
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
