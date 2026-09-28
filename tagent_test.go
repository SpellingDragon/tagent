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

// §5.8 composition-root aggregate barrier: a build raises ONE ref-counted
// registration hold per shared store touched (dedup across agents) and the
// top-level buildAgent releases every hold once all agents were constructed and
// reconciled. Stores without a retention lease (no destructive scanner) are
// skipped — the barrier only exists where forgetting exists.

type countingHoldStore struct {
	*memory.InMemoryStore
	begins, ends int
}

func (s *countingHoldStore) BeginHold() { s.begins++ }
func (s *countingHoldStore) EndHold()   { s.ends++ }

func TestRuntimeConfig_StoreBarrierAggregatesAndReleases(t *testing.T) {
	rc := &runtimeConfig{}
	shared := &countingHoldStore{InMemoryStore: memory.NewInMemoryStore()}
	plain := memory.NewInMemoryStore() // no lease → nothing to pause

	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(shared) // a second agent on the SAME store: deduped
	rc.raiseStoreBarrier(plain)  // non-holdable: silently skipped, no panic
	require.Equal(t, 1, shared.begins, "one registration window per store per build")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends, "the build top-level releases exactly what it raised")

	rc.releaseStoreBarriers() // re-entrant release: no double End
	require.Equal(t, 1, shared.ends)

	// A later build (hot-reload shell) re-raises on the same store: windows nest per build.
	rc.raiseStoreBarrier(shared)
	require.Equal(t, 2, shared.begins)
	rc.releaseStoreBarriers()
	require.Equal(t, 2, shared.ends)

	// nil-receiver safety (defensive; rc is normally always constructed).
	var nilRC *runtimeConfig
	require.NotPanics(t, func() { nilRC.raiseStoreBarrier(shared); nilRC.releaseStoreBarriers() })
	require.Equal(t, 2, shared.begins, "a nil rc never mutates the store's barrier")
}

// TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger 是 §8.2 + ③（§9.2/§9.1）+ §8.1 的
// buildAgent 级集成回归——此前仓内仅 governance 包「模拟双 gate」单测（gate_w3_test 手动 New 两
// gate），wire 测试只断言 New 成功，**没有**驱动真实 buildAgent 包裹路径证明「子 agent 的 exec
// leaf 工具确实过闸」。本测走真实 buildAgent（entry + 子 agent 各持独立 gate，共享 rc.govLedger），
// 经构建出的工具链驱动一次 critical exec，端到端断言三件事：
//
//	§8.2  两 agent 的 exec 均被治理闸拒绝（W3 前子 agent 主风险面绕闸）；
//	③     两 agent 治理记录落**同一** rc.govLedger（N2 共享账本，行为证明同指针）；
//	§8.1  记录按 AgentName 区分来源（共享 Ledger 下多 agent 事件可归因）。
func TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger(t *testing.T) {
	// 注册一个「声明名为 exec」的 plain 工具——命中 classifier 的 exec.destructive(critical) 规则。
	// 注册 ID 用独立名（test_gov_exec）避免与内建 exec 冲突；Declaration().Name="exec" 才是分级判据。
	agent.RegisterPlainTool("test_gov_exec", func(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return &mockCallableTool{name: "exec"}, nil
	})

	// 镜像 New() 的治理接线（tagent.go:256-273）：共享 Ledger（nil store，entry build 时延迟绑定）
	// + Enabled/strict gate。enforcement=strict 使 critical 确定性拒绝（result 渗透 [governance_denied]）。
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
			// 非内建名（worker）→ 无 ToolAgentFactory → 走 config-driven 工具构建 + 治理包裹路径。
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

	// §8.2：经**真实构建**的工具链驱动 critical exec——两 agent 均应被治理闸拒绝。
	entryRes := callBuiltExec(t, entry)
	subRes := callBuiltExec(t, sub)
	assert.Contains(t, entryRes, "[governance_denied]", "entry exec 应过闸被拒（真实 buildAgent 包裹路径）")
	assert.Contains(t, subRes, "[governance_denied]", "子 agent exec 应过闸被拒（W3 前子 agent 主风险面绕闸）")

	// ③ + §8.1：两 agent 的治理记录落同一共享 Ledger（N2），且按 AgentName 区分来源。
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

// TestGoalTools_EntryOnly: the five governance
// face tools are appended for the ENTRY agent only (governance enabled), and
// are themselves wrapped by the gate (appended before the wrapping loop).
// Sub-agents never get them — the governance surface converges on the main
// loop, same as refine.
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

// TestBuildPlainToolRef_MCPCallInjectsRegistry verifies buildPlainToolRef
// wires rc.mcpRegistry into the mcp_call factory.
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

// TestBuildPlainToolRef_MCPCallWithoutRegistry verifies the factory still
// succeeds when no registry was wired (empty stub behavior).
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

// TestMCPDiscoverFactory_PrefersRegistry verifies the discover factory
// consumes the injected live registry.
func TestMCPDiscoverFactory_PrefersRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })

	factory, ok := agent.GetPlainToolFactory("mcp_discover")
	require.True(t, ok)
	ct, err := factory(agent.PlainToolFactoryConfig{ID: "mcp_discover", MCPRegistry: reg})
	require.NoError(t, err)

	// Registered AFTER factory creation — must still be discoverable (live reads).
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

	// Valid declaration passes.
	cfg := base()
	cfg.MCPServers = map[string]MCPServerConfig{
		"ok": {Transport: "streamable-http", URL: "https://example.com/mcp"},
	}
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())
}

// TestWireMemoryEngine_NilEngineUnchanged 验证未配置 Engine 时 store 原样返回
// （不包裹引擎）——保证 T-A 对现状零影响。
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

// TestWireMemoryEngine_EmbeddingNilUnchanged 验证 Engine 配置但无 Embedding 时
// 不接线（无向量能力 = 纯关键词 = 现状）。
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
		_ = c.Close() // 回收引擎 worker，防 goroutine 泄漏
	} else {
		t.Fatal("包裹后的 store 应可 Close（agent.Closer）")
	}
}

// TestWireMemoryEngine_ZhipuNoKeyDegrades 验证 zhipu 嵌入无 API key 时优雅降级：
// 返回原 store（无引擎），不报错、不阻断 agent 构建（不变量：增强能力故障不传染主链路）。
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

// TestWireMemoryEngine_UnknownBackendErrors 验证未知 backend 报错（配置校验）。
func TestWireMemoryEngine_UnknownBackendErrors(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Backend: "bogus", Embedding: &EmbeddingConfig{Provider: "mock"}}}
	// 未知 backend：buildMemoryEngine 报错 → wireMemoryEngine 优雅降级返回原 store（不阻断）。
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

// TestNew_EvolutionEnabled_GitNativeSmoke 验证 git-native evolution 接线：启用时 New 成功
// 构造 GitEvolution 装配单元（refine 工具注册/章 provider/Stop closer 在 buildAgent 路径；
// git 仓自检 Warn 不阻断）。深度行为在 evolution 包 tempdir git 仓测试覆盖（2.4/3.3）。
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

// TestNew_GovernanceEnabled_Builds 验证 governance 接线：启用时 New 成功构建（govGate 构造 +
// leaf 工具包裹路径执行）。配置门控——默认关闭则不构造。
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

// TestNew_ReliableBusSpillDir 验证 ReliableBus 接线：配置 BusSpillDir 后 New 成功，且 entry
// agent 的 per-agent 目录 <BusSpillDir>/<entry> 被创建（NewReliableEventBus→NewInbox 建其下 inbox-v2）。
func TestNew_ReliableBusSpillDir(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	spillRoot := filepath.Join(t.TempDir(), "bus-spill")
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.BusSpillDir = spillRoot

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	// per-agent 溢出子目录应被创建（<spillRoot>/tagent）。
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

// TestNew_DegradationEnabled_Builds 验证 A2 接线：DegradationEnabled 时 New 成功构建
// （DegradationManager 构造 + ErrorTrackingStore 最外层包裹 memStore + agentCfg.Degradation
// 注入 + event_loop model 上报就绪）——补齐此前 DegradationManager 零接线的断点。
func TestNew_DegradationEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.DegradationEnabled = true

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestResolveMemoryStore_FileSamePathShared 是 M-1（四审）回归：type: file 同 path 必须返回
// 同一实例（与 memory/localfile 同构）——否则跨 agent read_namespaces 下 InMemRelationStore
// 内存图分歧（recall 因果链断链）+ 双 Compactor 基于独立视图并发覆盖同一 KV 键。
func TestResolveMemoryStore_FileSamePathShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-mem")
	s1, _, rel1, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	s2, _, rel2, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	require.Same(t, s1, s2, "file 后端同 path 必须共享同一实例（M-1：防因果链断链/双 Compactor 并发覆盖）")
	// 4.2：测试收尾释放租约（两次 acquire → 两次 release，第二次才真关）。
	rel1()
	rel2()
}

// TestConfig_WorkingDir 验证 C 方案(框架级 working_dir)配置层:yaml 解析 + 空默认(现状零变化)
// + TAGENT_WORKING_DIR env 覆盖(部署时免改 yaml 指定 clone 根)。
func TestConfig_WorkingDir(t *testing.T) {
	t.Run("yaml 解析 working_dir", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "") // 隔离 env(空=不触发覆盖)
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
