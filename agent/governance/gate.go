package governance

// ==================== GovernanceGate（T-G · 治理决策管线）====================
//
// 把 RiskClassifier(C5) + BudgetManager + GoalRegistry + ApprovalManager + DenialLedger
// 串成一条决策管线：classify → critical 批准门 → goal 检查 → 预算闸 → 记账/放行。
// GovernanceTool 装饰器（工具执行路径）与 T-EVO 发布道（版本风险路由）共用本管线。
//
// 治理关闭（Enabled=false，默认）时全放行——现状零行为变化。「闸不是墙」：默认
// enforcement=warn（记账放行 + 提示），strict 才拒绝；critical 恒走异步批准（不阻塞 loop）。

// Enforcement 是 goal/预算违规的处置模式。
type Enforcement string

const (
	EnforcementWarn   Enforcement = "warn"   // 记账放行 + 提示（默认，先收集数据）
	EnforcementStrict Enforcement = "strict" // 拒绝
)

// GateConfig 配置治理门。
type GateConfig struct {
	Enabled         bool          // 总开关（默认 false = 全放行，现状）
	Enforcement     Enforcement   // warn/strict（默认 warn）
	GoalRequiredFor []string      // 须挂 goal 的 trigger source（默认 meditation/task）
}

func (c GateConfig) withDefaults() GateConfig {
	if c.Enforcement == "" {
		c.Enforcement = EnforcementWarn
	}
	if c.GoalRequiredFor == nil {
		c.GoalRequiredFor = []string{"meditation", "task"}
	}
	return c
}

// Decision 是一次治理裁决。
type Decision struct {
	Disposition Disposition // allow/record/hold
	Level       RiskLevel
	RuleID      string
	Reason      string
	ApprovalID  string // hold 时的批准请求 id（外部审批者据此决策）
	Denied      bool   // 是否拒绝执行（strict 违规 / 预算耗尽 / critical 未批准且 strict）
	DenyReason  string // 拒绝理由（返回给模型的自纠材料——失败以 result 渗透）
}

// GovernanceGate 是治理决策管线（并发安全：各组件自身并发安全，Gate 无额外可变状态）。
type GovernanceGate struct {
	classifier *RiskClassifier
	budget     *BudgetManager
	approval   *ApprovalManager
	ledger     *DenialLedger
	goals      *GoalRegistry
	cfg        GateConfig
}

// GateDeps 是构建 GovernanceGate 的依赖集（nil 组件按各自降级语义处理）。
type GateDeps struct {
	Classifier *RiskClassifier
	Budget     *BudgetManager
	Approval   *ApprovalManager
	Ledger     *DenialLedger
	Goals      *GoalRegistry
	Config     GateConfig
}

// NewGovernanceGate 构建治理门。缺失组件用安全默认（classifier 默认规则，其余 nil 跳过）。
func NewGovernanceGate(deps GateDeps) *GovernanceGate {
	g := &GovernanceGate{
		classifier: deps.Classifier,
		budget:     deps.Budget,
		approval:   deps.Approval,
		ledger:     deps.Ledger,
		goals:      deps.Goals,
		cfg:        deps.Config.withDefaults(),
	}
	if g.classifier == nil {
		g.classifier = NewRiskClassifier(nil, 0)
	}
	if g.ledger == nil {
		g.ledger = NewDenialLedger(nil, 0) // 纯内存账本
	}
	return g
}

// Enabled 报告治理是否开启。
func (g *GovernanceGate) Enabled() bool { return g != nil && g.cfg.Enabled }

// Evaluate 对一个工具调用做治理裁决（管线：classify → 批准 → goal → 预算 → 记账）。
func (g *GovernanceGate) Evaluate(ctx RiskContext) Decision {
	if !g.Enabled() {
		return Decision{Disposition: DispositionAllow, Level: RiskLow, Reason: "治理未启用"}
	}
	level, ruleID, reason := g.classifier.Classify(ctx)
	disp := DispositionFor(level)
	digest := ArgsDigest(ctx.ArgsJSON)

	// ① critical → 异步批准门（不阻塞 loop：未批准则挂起 + 登记请求）。
	if level == RiskCritical {
		if g.approval == nil {
			// 无批准机制：critical 无法获批 → 拒绝（绝不放行不可逆操作，防治理绕过）。
			g.record(SubtypeDenial, ctx, level, ruleID, "critical 无批准机制", digest, "")
			return Decision{Disposition: DispositionHold, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "critical 操作需人工批准，但运行时未配置批准机制（ApprovalManager）"}
		}
		if appr := g.approval.Check(ctx.ToolName, digest); appr != nil {
			g.record(SubtypeAudit, ctx, level, ruleID, "critical 已批准放行", digest, "")
			return Decision{Disposition: DispositionRecord, Level: level, RuleID: ruleID, Reason: "已批准"}
		}
		req, _ := g.approval.Request(ctx.ToolName, ctx.ArgsJSON, ctx.ArgsJSON, level.String(), ruleID, reason, "")
		approvalID := ""
		if req != nil {
			approvalID = req.ID
		}
		g.record(SubtypeApproval, ctx, level, ruleID, "critical 挂起待批准", digest, "")
		denied := g.cfg.Enforcement == EnforcementStrict
		return Decision{
			Disposition: DispositionHold, Level: level, RuleID: ruleID, Reason: reason,
			ApprovalID: approvalID, Denied: denied,
			DenyReason: "critical 操作需人工批准（已登记请求 " + approvalID + "，批准后重试）",
		}
	}

	// ② goal 检查（high+ 且 trigger 须挂 goal）。
	if level >= RiskHigh && g.goalRequired(ctx.TriggerSource) && g.goals != nil && !g.goals.HasActive() {
		g.record(SubtypeDenial, ctx, level, ruleID, "缺 goal 登记", digest, "")
		if g.cfg.Enforcement == EnforcementStrict {
			return Decision{Disposition: disp, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "high+ 自治操作须先经 goal_declare 登记目标"}
		}
		// warn：记账放行（在工具结果附提醒由装饰器处理）。
	}

	// ③ 预算闸（high/medium）。
	if g.budget != nil {
		if err := g.budget.Admit(level); err != nil {
			g.record(SubtypeDenial, ctx, level, ruleID, "预算耗尽", digest, "")
			return Decision{Disposition: disp, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "本窗口 " + level.String() + " 级操作预算已耗尽，请稍后或降低风险"}
		}
	}

	// ④ 记账放行（high/medium）或直接放行（low）。
	if disp == DispositionRecord {
		g.record(SubtypeAudit, ctx, level, ruleID, reason, digest, "")
	}
	return Decision{Disposition: disp, Level: level, RuleID: ruleID, Reason: reason}
}

func (g *GovernanceGate) goalRequired(triggerSource string) bool {
	for _, s := range g.cfg.GoalRequiredFor {
		if s == triggerSource {
			return true
		}
	}
	return false
}

func (g *GovernanceGate) record(subtype string, ctx RiskContext, level RiskLevel, ruleID, reason, digest, goalID string) {
	if g.ledger == nil {
		return
	}
	g.ledger.Record(DenialRecord{
		Subtype: subtype, ToolName: ctx.ToolName, Level: level,
		RuleID: ruleID, Reason: reason, ArgsDigest: digest, GoalID: goalID,
	})
}
