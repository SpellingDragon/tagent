package governance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCognitiveAssetWriteRule 钉住写形态命中矩阵：写资产判 critical，只读不误伤。
// 契约: docs/wiki/platform/cognitive-asset-guard.md
func TestCognitiveAssetWriteRule(t *testing.T) {
	c := NewRiskClassifier(nil, 0)
	cases := []struct {
		name     string
		argsJSON string
		asset    bool
	}{
		{"重定向写 prompts", `{"command":"echo x > resources/prompts/action_tool_desc.md"}`, true},
		{"追加写 skills", `{"command":"echo x >> skills/foo/SKILL.md"}`, true},
		{"tee 写 scripts", `{"command":"echo x | tee scripts/deploy.sh"}`, true},
		{"sed -i 改 skills", `{"command":"sed -i 's/a/b/' skills/foo/SKILL.md"}`, true},
		{"python open(w) 改 prompts", `{"command":"python3 -c \"open('resources/prompts/desc.md','w').write('poison')\""}`, true},
		{"cp 覆盖 scripts", `{"command":"cp /tmp/new.sh scripts/run.sh"}`, true},
		{"rm 删 prompts", `{"command":"rm resources/prompts/important.md"}`, true},
		{"cat 只读 prompts", `{"command":"cat resources/prompts/desc.md"}`, false},
		{"grep 只读 skills", `{"command":"grep -rn todo skills/"}`, false},
		{"读资产重定向到 /tmp", `{"command":"cat resources/prompts/desc.md > /tmp/backup.txt"}`, false},
		{"写日志非资产", `{"command":"echo x > /var/log/app.log"}`, false},
		{"普通命令", `{"command":"ls -la"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ruleID, _ := c.Classify(RiskContext{ToolName: "exec", ArgsJSON: tc.argsJSON, TriggerSource: "user"})
			if tc.asset {
				require.Equal(t, "exec.cognitive-asset-write", ruleID, "应命中资产写规则")
			} else {
				require.NotEqual(t, "exec.cognitive-asset-write", ruleID, "不应命中资产写规则")
			}
		})
	}
}

// TestCognitiveAssetWriteLevelCritical 钉住命中即 critical，走异步批准而非记账放行。
func TestCognitiveAssetWriteLevelCritical(t *testing.T) {
	c := NewRiskClassifier(nil, 0)
	level, ruleID, reason := c.Classify(RiskContext{
		ToolName: "exec", ArgsJSON: `{"command":"echo x > resources/prompts/desc.md"}`, TriggerSource: "user",
	})
	require.Equal(t, RiskCritical, level)
	require.Equal(t, "exec.cognitive-asset-write", ruleID)
	require.NotEmpty(t, reason)
}

// TestCognitiveAssetWriteRefineNoExemption 钉住 refine/meditation 触发源不豁免，规则不看 TriggerSource。
func TestCognitiveAssetWriteRefineNoExemption(t *testing.T) {
	c := NewRiskClassifier(nil, 0)
	for _, src := range []string{"user", "meditation", "evolution", "refine", ""} {
		level, ruleID, _ := c.Classify(RiskContext{
			ToolName: "exec", ArgsJSON: `{"command":"sed -i x skills/foo.md"}`, TriggerSource: src,
		})
		require.Equal(t, RiskCritical, level, "触发源 %q 不得豁免", src)
		require.Equal(t, "exec.cognitive-asset-write", ruleID, "触发源 %q 不得豁免", src)
	}
}

// TestCognitiveAssetWriteNonExecUnaffected 钉住非 exec 工具（save_file）不因资产路径命中本规则。
func TestCognitiveAssetWriteNonExecUnaffected(t *testing.T) {
	c := NewRiskClassifier(nil, 0)
	_, ruleID, _ := c.Classify(RiskContext{ToolName: "save_file", ArgsJSON: `{"path":"resources/prompts/a.md"}`})
	require.NotEqual(t, "exec.cognitive-asset-write", ruleID)
}

// TestCognitiveAssetWriteGateHoldThenApprove 钉住 strict 下写资产挂起待批、批准后同命令重试放行。
func TestCognitiveAssetWriteGateHoldThenApprove(t *testing.T) {
	appr := NewApprovalManager("", time.Minute)
	g := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Approval:   appr,
		Config:     GateConfig{Enabled: true, Enforcement: EnforcementStrict},
	})
	ctx := RiskContext{ToolName: "exec", ArgsJSON: `{"command":"echo poison > resources/prompts/desc.md"}`, TriggerSource: "user"}

	d := g.Evaluate(ctx)
	require.Equal(t, "exec.cognitive-asset-write", d.RuleID)
	require.Equal(t, DispositionHold, d.Disposition, "未批应挂起")
	require.True(t, d.Denied, "strict 未批拒绝")
	require.NotEmpty(t, d.ApprovalID, "应登记审批请求")

	require.NoError(t, appr.Decide(d.ApprovalID, ApprovalApproved, "human"))
	d2 := g.Evaluate(ctx)
	require.False(t, d2.Denied, "批准后应放行")
	require.Equal(t, DispositionRecord, d2.Disposition)
}

// TestCognitiveAssetWriteWarnModeHangsNotDenies 钉住 warn 模式挂起待批但不硬拒，执行权在人。
func TestCognitiveAssetWriteWarnModeHangsNotDenies(t *testing.T) {
	appr := NewApprovalManager("", time.Minute)
	g := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Approval:   appr,
		Config:     GateConfig{Enabled: true, Enforcement: EnforcementWarn},
	})
	ctx := RiskContext{ToolName: "exec", ArgsJSON: `{"command":"cp /tmp/x skills/y"}`, TriggerSource: "user"}
	d := g.Evaluate(ctx)
	require.Equal(t, "exec.cognitive-asset-write", d.RuleID)
	require.Equal(t, DispositionHold, d.Disposition)
	require.False(t, d.Denied, "warn 模式挂起但不硬拒")
}

// TestCognitiveAssetWriteGovernanceDisabledZeroChange 钉住 governance 关闭时同类命令零行为变化。
func TestCognitiveAssetWriteGovernanceDisabledZeroChange(t *testing.T) {
	g := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Config:     GateConfig{Enabled: false},
	})
	ctx := RiskContext{ToolName: "exec", ArgsJSON: `{"command":"echo x > resources/prompts/desc.md"}`, TriggerSource: "user"}
	d := g.Evaluate(ctx)
	require.False(t, d.Denied, "关闭时不拒绝")
	require.NotEqual(t, DispositionHold, d.Disposition, "关闭时不挂起")
}
