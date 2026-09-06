package tagent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

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

	entry, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache)
	require.NoError(t, err)
	require.NotNil(t, entry)
	sub, err := buildAgent("worker", cfg.Agents["worker"], cfg, rc, loader, cache)
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
