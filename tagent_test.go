package tagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

type countingHoldStore struct {
	*memory.InMemoryStore
	begins, ends int
}

func (s *countingHoldStore) BeginHold() { s.begins++ }
func (s *countingHoldStore) EndHold()   { s.ends++ }

// TestRuntimeConfig_StoreBarrierAggregatesAndReleases 钉住 组合根的遗忘屏障按 store 聚合、由顶层构建一次放尽。
// - 同一份共享 store 被两个 agent 触及只抬一层；
// - 不带保留租约的 store 没有屏障可抬，静默跳过，不算错误；
// - 重复释放不会多放一层；
// - 后一次构建在同一份 store 上再抬一层，窗口按构建嵌套计数；
// - 接收者为 nil 时抬与放都不触碰任何 store。
// 契约: docs/wiki/platform/resource-ownership.md#composition-barrier
func TestRuntimeConfig_StoreBarrierAggregatesAndReleases(t *testing.T) {
	rc := &runtimeConfig{}
	shared := &countingHoldStore{InMemoryStore: memory.NewInMemoryStore()}
	plain := memory.NewInMemoryStore()

	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(plain)
	require.Equal(t, 1, shared.begins, "one registration window per store per build")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends, "the build top-level releases exactly what it raised")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends)

	rc.raiseStoreBarrier(shared)
	require.Equal(t, 2, shared.begins)
	rc.releaseStoreBarriers()
	require.Equal(t, 2, shared.ends)

	var nilRC *runtimeConfig
	require.NotPanics(t, func() { nilRC.raiseStoreBarrier(shared); nilRC.releaseStoreBarriers() })
	require.Equal(t, 2, shared.begins, "a nil rc never mutates the store's barrier")
}

// TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger 钉住 过闸覆盖真实构建出的工具链，共享账本仍分得清来源。
// - entry 与子 agent 各自的 exec 都被拒，结果里带显式拒绝标记；
// - 两条拒绝记录落在同一份账本里，来源 agent 名各自在册；
// - 观察对象是构建产物上的工具链，不是手工组装的闸——绕过构建就看不见包裹有没有真发生。
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
func TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger(t *testing.T) {
	agent.RegisterPlainTool("test_gov_exec", func(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return &mockCallableTool{name: "exec"}, nil
	})

	rc := &runtimeConfig{model: &factoryMockModel{}}
	rc.govLedger = governance.NewDenialLedger(nil, 0)
	rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
		Ledger: rc.govLedger,
		Config: governance.GateConfig{Enabled: true, Enforcement: governance.EnforcementStrict},
	})

	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "entry prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				Tools:        []ToolRef{{Kind: ToolKindTool, ID: "test_gov_exec"}},
			},
			"worker": {
				SystemPrompt: PromptConfig{Inline: "worker prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				Tools:        []ToolRef{{Kind: ToolKindTool, ID: "test_gov_exec"}},
			},
		},
	}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	entry, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.NotNil(t, entry)
	sub, err := buildAgent("worker", cfg.Agents["worker"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.NotNil(t, sub)

	entryRes := callBuiltExec(t, entry)
	subRes := callBuiltExec(t, sub)
	assert.Contains(t, entryRes, "[governance_denied]", "entry exec 应过闸被拒（真实 buildAgent 包裹路径）")
	assert.Contains(t, subRes, "[governance_denied]", "子 agent exec 应过闸被拒（W3 前子 agent 主风险面绕闸）")

	recs := rc.govLedger.Query(-1)
	var sawEntry, sawSub bool
	for _, r := range recs {
		switch r.AgentName {
		case "tagent":
			sawEntry = true
		case "worker":
			sawSub = true
		}
	}
	assert.True(t, sawEntry, "共享 Ledger 应含 entry(tagent) 治理记录")
	assert.True(t, sawSub, "共享 Ledger 应含子 agent(worker) 治理记录（③ 同指针共享 + §8.1 按 agent 区分来源）")
}

// callBuiltExec 在已构建 agent 的工具列表里找声明名为 exec 的工具，经其（OutputLimitTool→
// GovernanceTool→mock）链式 Call 驱动一次 critical 操作，返回结果字符串。找不到即失败——
// 证明治理包裹路径确实构建了 exec leaf 工具。
func callBuiltExec(t *testing.T, ta *agent.TagentAgent) string {
	t.Helper()
	for _, tl := range ta.Tools() {
		decl := tl.Declaration()
		if decl == nil || decl.Name != "exec" {
			continue
		}
		callable, ok := tl.(trpctool.CallableTool)
		require.True(t, ok, "exec 工具应实现 CallableTool（OutputLimitTool 包裹 GovernanceTool）")
		res, err := callable.Call(context.Background(), []byte(`{"command":"rm -rf /tmp/x"}`))
		require.NoError(t, err)
		s, _ := res.(string)
		return s
	}
	t.Fatal("未找到声明名为 exec 的工具——治理包裹的 leaf 工具未经 buildAgent 构建")
	return ""
}

// TestGoalTools_EntryOnly 钉住 治理面入口只挂 entry。
// - entry 恰好拿到那五个治理面工具，一个不缺；
// - 子 agent 一个也不能有——治理面出现在子 agent 的工具表里就是本测要拦的形状。
// 契约: docs/wiki/tool/tool-architecture.md#govx-entry-only
func TestGoalTools_EntryOnly(t *testing.T) {
	rc := &runtimeConfig{model: &factoryMockModel{}}
	rc.govLedger = governance.NewDenialLedger(nil, 0)
	rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
		Ledger: rc.govLedger,
		Goals:  governance.NewGoalRegistry(),
		Config: governance.GateConfig{Enabled: true, Enforcement: governance.EnforcementStrict},
	})
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {SystemPrompt: PromptConfig{Inline: "entry prompt"}, Memory: MemoryConfig{Type: "memory"}},
			"worker": {SystemPrompt: PromptConfig{Inline: "worker prompt"}, Memory: MemoryConfig{Type: "memory"}},
		},
	}
	cfg.Governance.Enabled = true
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	entry, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	sub, err := buildAgent("worker", cfg.Agents["worker"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)

	hasGoalTools := func(ta *agent.TagentAgent) int {
		n := 0
		for _, tl := range ta.Tools() {
			switch tl.Declaration().Name {
			case "goal_declare", "goal_list", "goal_resolve", "denial_query", "approval_list":
				n++
			}
		}
		return n
	}
	if got := hasGoalTools(entry); got != 5 {
		t.Fatalf("entry should carry all 5 governance tools, got %d", got)
	}
	if got := hasGoalTools(sub); got != 0 {
		t.Fatalf("sub-agent must NOT carry governance tools, got %d", got)
	}
}

// wiringTool is a declaration-only tool for wiring tests.
type wiringTool struct{ name string }

func (w *wiringTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: w.name, Description: "wiring tool"}
}

// wiringToolSet is a ToolSet exposing fixed tools.
type wiringToolSet struct {
	name  string
	tools []trpctool.Tool
}

func (m *wiringToolSet) Tools(_ context.Context) []trpctool.Tool { return m.tools }
func (m *wiringToolSet) Close() error                            { return nil }
func (m *wiringToolSet) Name() string                            { return m.name }

// TestBuildPlainToolRef_MCPCallInjectsRegistry 钉住 网关工具看见的是装配根注入的那一份活注册表。
// - 证据形态：调用一个不存在的 server，失败结果里必须列出注册表里真实存在的服务名；
// - 注入没发生时清单为空，这一条当场失败。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestBuildPlainToolRef_MCPCallInjectsRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("mock", &wiringToolSet{name: "mock"})

	rc := &runtimeConfig{mcpRegistry: reg}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, isAction, err := buildPlainToolRef(tr, "", "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil, 0)
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.False(t, isAction)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"nope","tool":"x"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), "unknown MCP server")
	assert.Contains(t, string(b), "mock", "error must list registry servers, proving injection")
}

// TestBuildPlainToolRef_MCPCallWithoutRegistry 钉住 没有注册表可注入时网关照常构建，缺席只在调用结果里现形。
// - 构建不报错，工具可调用；
// - 失败以结果形态返回，内容显式说明没有任何服务被注册。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestBuildPlainToolRef_MCPCallWithoutRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	rc := &runtimeConfig{}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, _, err := buildPlainToolRef(tr, "", "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil, 0)
	require.NoError(t, err)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"a","tool":"b"}`))
	require.NoError(t, err)
	b, _ := json.Marshal(res)
	assert.Contains(t, string(b), "no MCP servers are registered")
}

// TestMCPDiscoverFactory_PrefersRegistry 钉住 发现工具读的是注入的活注册表，而且每次调用都重读。
// - 工厂创建之后才注册的服务同样必须被发现——静态切片做不到这一点；
// - 结果给出可照抄的调用方式，含 server 名与 tool 名。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func TestMCPDiscoverFactory_PrefersRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })

	factory, ok := agent.GetPlainToolFactory("mcp_discover")
	require.True(t, ok)
	ct, err := factory(agent.PlainToolFactoryConfig{ID: "mcp_discover", MCPRegistry: reg})
	require.NoError(t, err)

	reg.Add("web-search-prime", &wiringToolSet{
		name:  "web-search-prime",
		tools: []trpctool.Tool{&wiringTool{name: "webSearchPrime"}},
	})

	res, err := ct.Call(context.Background(), []byte(`{"query":"webSearchPrime"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), `mcp_call(server=\"web-search-prime\", tool=\"webSearchPrime\"`)
	assert.Contains(t, string(b), "mcp:web-search-prime")
}

// TestLoadConfig_MCPServers verifies YAML parsing + ConfigPath recording.
func TestLoadConfig_MCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tagent.yaml")
	yaml := `
entry: tagent
agents:
  tagent:
    system_prompt:
      inline: "hi"
mcp_servers:
  web-search-prime:
    transport: streamable-http
    url: https://open.bigmodel.cn/api/mcp/web_search_prime/mcp
    api_key_env: ZAI_API_KEY
    timeout: 30s
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)

	sc, ok := cfg.MCPServers["web-search-prime"]
	require.True(t, ok)
	assert.Equal(t, "streamable-http", sc.Transport)
	assert.Equal(t, "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp", sc.URL)
	assert.Equal(t, "ZAI_API_KEY", sc.APIKeyEnv)
	assert.Equal(t, "30s", sc.Timeout)
	assert.True(t, filepath.IsAbs(cfg.ConfigPath), "ConfigPath must be recorded (absolute)")
}

// TestConfigValidate_MCPServers covers the three validation failures.
func TestConfigValidate_MCPServers(t *testing.T) {
	base := func() Config {
		return Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{
				"tagent": {SystemPrompt: PromptConfig{Inline: "hi"}},
			},
		}
	}

	cases := []struct {
		name    string
		server  MCPServerConfig
		wantErr string
	}{
		{"sse missing url", MCPServerConfig{Transport: "sse"}, "requires url"},
		{"stdio missing command", MCPServerConfig{Transport: "stdio"}, "requires command"},
		{"unsupported transport", MCPServerConfig{Transport: "websocket", URL: "https://x"}, "unsupported transport"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.MCPServers = map[string]MCPServerConfig{"bad": tc.server}
			cfg.ApplyDefaults()
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "bad")
		})
	}

	cfg := base()
	cfg.MCPServers = map[string]MCPServerConfig{
		"ok": {Transport: "streamable-http", URL: "https://example.com/mcp"},
	}
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())
}

// TestWireMemoryEngine_NilEngineUnchanged 钉住 未配置引擎时拿到的是内层 store 本体，不新增任何包裹。
// - 判据是能力接口的缺席：它不得声称自己是引擎提供者；
// - 只有"无引擎配置"与"无 store 事件回调"同时成立才走这条路。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_NilEngineUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, nil, MemoryConfig{}, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未配置 Engine 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_EmbeddingNilUnchanged 钉住 只有引擎壳子而没有嵌入配置时同样不接引擎。
// - 没有向量能力就没有语义检索，store 保持与未配置引擎同一形态。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_EmbeddingNilUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, nil, MemoryConfig{Engine: &MemoryEngineConfig{}}, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 Embedding 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_MockEmbedderWraps 验证 mock 嵌入配置 → 包裹引擎（MemoryEngineProvider）。
func TestWireMemoryEngine_MockEmbedderWraps(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 32}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	ep, ok := got.(memory.MemoryEngineProvider)
	if !ok {
		t.Fatal("配置 mock 嵌入后应包裹引擎(MemoryEngineProvider)")
	}
	if ep.MemoryEngine() == nil {
		t.Fatal("引擎不应为 nil")
	}
	if c, ok := got.(interface{ Close() error }); ok {
		_ = c.Close()
	} else {
		t.Fatal("包裹后的 store 应可 Close（agent.Closer）")
	}
}

// TestWireMemoryEngine_ZhipuNoKeyDegrades 钉住 密钥未配时嵌入能力按"功能关闭"降级：不报错、不阻断构建。
// - 拿回的 store 不得声称自己是引擎提供者；
// - 缺 key 是配置事实，不构成调用方必须处理的错误。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
// 契约: docs/wiki/memory/memory-architecture.md#embedder
func TestWireMemoryEngine_ZhipuNoKeyDegrades(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "zhipu"}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("无 key 应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 key 时应降级为原 store(无引擎)")
	}
}

// TestWireMemoryEngine_UnknownBackendErrors 钉住 引擎 backend 名不认识时按"功能关闭"降级：不报错、不阻断构建。
// - 拿回的 store 不得声称自己是引擎提供者，它已被降为仅容量包裹；
// - 增强能力的故障只关掉增强本身，不构成调用方必须处理的错误。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_UnknownBackendErrors(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Backend: "bogus", Embedding: &EmbeddingConfig{Provider: "mock"}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未知 backend 应降级为原 store")
	}
}

// fakeModel 是满足 model.Model 的最小替身（New 构建期不调用 GenerateContent，仅运行期）。
type fakeModel struct{}

func (fakeModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response)
	close(ch)
	return ch, nil
}

func (fakeModel) Info() model.Info { return model.Info{} }

// minimalConfig 构造最小单 agent 配置（inline 提示词 + 无工具 + 内存 store），避开
// DefaultConfig 的多 agent 依赖（knowledge 需 SkillRepo、recall 需分区等）。
func minimalConfig(_ string, evoEnabled bool) Config {
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "你是 tagent 测试代理"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	cfg.ApplyDefaults()
	cfg.Evolution.Enabled = evoEnabled
	return cfg
}

// TestNew_EvolutionEnabled_GitNativeSmoke 钉住 启用自进化的配置能通过完整装配。
// - 本测只观察"启用不等于起不来"；改进行为本身由 evolution 包在临时 git 仓里钉。
// 契约: docs/wiki/platform/platform-subsystems.md#evolution-wiring
func TestNew_EvolutionEnabled_GitNativeSmoke(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	a, err := New(minimalConfig("", true), WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_EvolutionDisabled_NoSideEffect 验证配置门控：默认关闭时 New 成功（现状零行为变化）。
func TestNew_EvolutionDisabled_NoSideEffect(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig("", false)
	require.False(t, cfg.Evolution.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_GovernanceEnabled_Builds 钉住 启用治理的配置能通过完整装配。
// - 本测只观察"启用不等于起不来"；分级与处置语义由治理自身的测试钉。
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
func TestNew_GovernanceEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Governance.Enabled = true
	cfg.Governance.Enforcement = "warn"
	cfg.Governance.Dir = t.TempDir()

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_GovernanceDisabled_Default 验证 governance 默认关闭（零值）→ New 成功，现状不变。
func TestNew_GovernanceDisabled_Default(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.False(t, cfg.Governance.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_ReliableBusSpillDir 钉住 配了总线溢出根目录后，entry 得到属于自己的溢出目录。
// - 目录形如 `<溢出根>/<entry>`，装配完成时已存在，不必等第一次溢出。
// 契约: docs/wiki/platform/platform-subsystems.md#reliability-switches
func TestNew_ReliableBusSpillDir(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	spillRoot := filepath.Join(t.TempDir(), "bus-spill")
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.BusSpillDir = spillRoot

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	_, statErr := os.Stat(filepath.Join(spillRoot, cfg.Entry))
	require.NoError(t, statErr, "per-agent 溢出子目录 <BusSpillDir>/<entry> 应被创建")
}

// TestNew_ReliableBusDisabledDefault 验证配置门控：默认 BusSpillDir 空 → 不创建溢出目录（现状）。
func TestNew_ReliableBusDisabledDefault(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.Empty(t, cfg.Reliability.BusSpillDir)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_DegradationEnabled_Builds 钉住 退化开关独立成立：治理段关闭时启用它照样装配成功。
// - 本例的治理段是关的，只有退化开关为真；
// - 装配得到可用 agent，退化状态机不与治理配置相互牵连。
// 契约: docs/wiki/platform/platform-subsystems.md#reliability-switches
func TestNew_DegradationEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.DegradationEnabled = true

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestResolveMemoryStore_FileSamePathShared 钉住 `type: file` 的同一 path 得到同一个 store 实例，与 memory/localfile 同构。
// - 判据是对象身份，不是内容相似；
// - 两次解析各自领到释放钩子，两个都要交还。
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestResolveMemoryStore_FileSamePathShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-mem")
	s1, _, rel1, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	s2, _, rel2, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	require.Same(t, s1, s2, "file 后端同 path 必须共享同一实例（M-1：防因果链断链/双 Compactor 并发覆盖）")
	rel1()
	rel2()
}

// TestConfig_WorkingDir 钉住 工作根的三态取法：yaml 初值、环境变量非空覆盖、两者皆空保持空。
// - 环境变量写空值等于没写，不触发覆盖；
// - 空串不是 "." 的别名——它意味着 file 与 exec 各自继承进程工作目录。
// 契约: docs/wiki/platform/agent-behavior-matrix.md#working-dir
func TestConfig_WorkingDir(t *testing.T) {
	t.Run("yaml 解析 working_dir", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /home/user/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/home/user/codes", cfg.WorkingDir)
	})

	t.Run("空 working_dir 默认空(=继承进程 cwd,现状逐字节不变)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Empty(t, cfg.WorkingDir, "空=file base_dir '.'/exec 继承进程 cwd,现状不变")
	})

	t.Run("TAGENT_WORKING_DIR env 覆盖 yaml 值", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/codes")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /yaml/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/codes", cfg.WorkingDir, "env 应覆盖 yaml(部署时灵活指定 clone 根)")
	})

	t.Run("TAGENT_WORKING_DIR env 注入(yaml 未配)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/only")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/only", cfg.WorkingDir)
	})
}
