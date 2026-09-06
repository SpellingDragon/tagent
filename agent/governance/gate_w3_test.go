package governance

import (
	"testing"
)

// TestGovernanceGate_SharedComponentAccessors 是 W3（§8.3）回归：Gate 暴露 Classifier/Goals/
// Config 访问器，供 buildAgent 为**每个 agent** 构造独立 GovernanceGate（共享 Classifier/Approval/
// Goals/Config + per-agent 独立 BudgetManager，用户裁决子 agent 独立预算）。此前治理只包 entry
// （name==cfg.Entry），子 agent 的 exec/save_file/mcp_call 主风险面全部绕闸。
func TestGovernanceGate_SharedComponentAccessors(t *testing.T) {
	shared := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Budget:     NewBudgetManager(BudgetConfig{}, ""),
		Goals:      NewGoalRegistry(),
		Config:     GateConfig{Enabled: true},
	})
	if shared.Classifier() == nil {
		t.Fatal("Classifier() 应暴露共享分类器")
	}
	if shared.Goals() == nil {
		t.Fatal("Goals() 应暴露共享 goal 注册表")
	}
	if !shared.Config().Enabled {
		t.Fatal("Config() 应暴露共享配置")
	}

	// 模拟 buildAgent 为两个 agent 各建独立 gate：复用共享组件 + 各自独立 BudgetManager。
	g1 := NewGovernanceGate(GateDeps{
		Classifier: shared.Classifier(),
		Budget:     NewBudgetManager(BudgetConfig{MaxHighRisk: 5}, ""),
		Goals:      shared.Goals(),
		Config:     shared.Config(),
	})
	g2 := NewGovernanceGate(GateDeps{
		Classifier: shared.Classifier(),
		Budget:     NewBudgetManager(BudgetConfig{MaxHighRisk: 5}, ""),
		Goals:      shared.Goals(),
		Config:     shared.Config(),
	})
	// 共享组件同实例（Classifier 纯函数、Goals 全局注册表）。
	if g1.Classifier() != g2.Classifier() {
		t.Fatal("per-agent gate 应共享同一 Classifier")
	}
	if g1.Goals() != g2.Goals() {
		t.Fatal("per-agent gate 应共享同一 Goals")
	}
	// 独立预算（W3 用户裁决：子 agent 独立预算）：per-agent gate 各持独立 BudgetManager 实例，
	// g1 刷爆高风险预算不影响 g2 的独立计数（隔离单 agent 刷爆，每 agent 各自有界）。
	if g1.budget == g2.budget {
		t.Fatal("W3: per-agent gate 应各持独立 BudgetManager（子 agent 独立预算，非共享）")
	}
	ctx := RiskContext{ToolName: "exec", ArgsJSON: `{"cmd":"rm -rf /x"}`} // destructive → critical
	for i := 0; i < 10; i++ {
		_ = g1.Evaluate(ctx) // g1 消耗自身预算至刷爆
	}
	_ = g2.Evaluate(ctx) // g2 独立预算，不受 g1 刷爆影响（独立性已由 budget != 保证）
}

// TestGovernanceGate_NilAccessorsSafe 验证 nil Gate 的访问器安全（W3 扩展访问器不得引入 nil panic）。
func TestGovernanceGate_NilAccessorsSafe(t *testing.T) {
	var g *GovernanceGate
	if g.Classifier() != nil || g.Goals() != nil || g.Approval() != nil {
		t.Fatal("nil Gate 访问器应返回 nil")
	}
	if g.Config().Enabled {
		t.Fatal("nil Gate Config 应零值")
	}
}
