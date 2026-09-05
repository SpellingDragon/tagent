package evolution

// ==================== DiffRiskRouter（T-EVO · 发布道路由）====================
//
// 实现 RiskRouter：按 BundleDiff 内容把版本变更路由到发布道，是 T-E/T-F 调和的判据落地——
//   - 模型/参数变更 → 慢道（影响面大、可逆性差、需 replay/shadow/approve 门后生效）；
//   - 仅提示词变更 → 快道（低风险可逆，先生效后验评估守护，劣化即回滚）。
//
// protected 提示词的强制慢道由 ReleaseManager.route 的 touchesProtected 兜底（先于本路由器），
// 故本路由器只需处理「非 protected 的提示词/参数/模型」分级。

// DiffRiskRouter 是按 diff 内容路由发布道的默认路由器（无状态、纯函数、并发安全）。
type DiffRiskRouter struct{}

// NewDiffRiskRouter 构建默认发布道路由器。
func NewDiffRiskRouter() DiffRiskRouter { return DiffRiskRouter{} }

// Route 实现 RiskRouter：模型或参数变更走慢道，仅提示词变更走快道。
func (DiffRiskRouter) Route(diff BundleDiff) Lane {
	// 模型/参数变更：影响面大、可逆性差 → 慢道（门后生效）。
	if diff.ModelChanged || diff.ParamsChanged {
		return LaneSlow
	}
	// 仅提示词变更（含增/删/改）：低风险可逆 → 快道（先生效，后验评估 + guardrail 守护）。
	return LaneFast
}
