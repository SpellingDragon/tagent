// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
package governance

import (
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// Enforcement 是 goal/预算违规的处置模式。
type Enforcement string

const (
	// EnforcementWarn 只告警：违规被记账并暴露，但不阻断执行。缺省即此值。
	EnforcementWarn Enforcement = "warn"
	// EnforcementStrict 严格执行：违规判为拒绝。是否真的 denied 只由此档决定。
	EnforcementStrict Enforcement = "strict"
)

// GateConfig 配置治理门。
type GateConfig struct {
	Enabled         bool
	Enforcement     Enforcement
	GoalRequiredFor []string
}

func (c GateConfig) withDefaults() GateConfig {
	if c.Enforcement == "" {
		c.Enforcement = EnforcementWarn
	}
	return c
}

// Decision 是一次治理裁决。
type Decision struct {
	Disposition Disposition
	Level       RiskLevel
	RuleID      string
	Reason      string
	ApprovalID  string
	Denied      bool
	DenyReason  string
}

// GovernanceGate 是治理决策管线（并发安全：各组件自身并发安全，Gate 无额外可变状态）。
type GovernanceGate struct {
	classifier *RiskClassifier
	budget     *BudgetManager
	approval   *ApprovalManager
	ledger     *DenialLedger
	goals      *GoalRegistry
	cfg        GateConfig
	// agentName 是本 gate 服务的 agent 名：治理记录写事件时标注来源 agent——W3 后
	// 所有 agent 共享同一 entry Ledger，无此字段则多 agent 治理事件无法区分来源。
	agentName string
}

// GateDeps 是构建 GovernanceGate 的依赖集（nil 组件按各自降级语义处理）。
type GateDeps struct {
	Classifier *RiskClassifier
	Budget     *BudgetManager
	Approval   *ApprovalManager
	Ledger     *DenialLedger
	Goals      *GoalRegistry
	Config     GateConfig
	// AgentName 是本 gate 服务的 agent 名：治理记录写事件时标注来源 agent。W3 后所有
	// agent 共享同一 entry Ledger，无此字段则多 agent 治理事件无法区分来源。
	AgentName string
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
		agentName:  deps.AgentName,
	}
	if g.classifier == nil {
		g.classifier = NewRiskClassifier(nil, 0)
	}
	if g.ledger == nil {
		g.ledger = NewDenialLedger(nil, 0)
	}
	return g
}

// Enabled 报告治理是否开启。
func (g *GovernanceGate) Enabled() bool { return g != nil && g.cfg.Enabled }

// Approval 暴露审批管理器：供消息/CLI 审批通道调 Decide（批准/拒绝 pending）与
// Pending（展示待批）。审批送达形态（用户裁决）= **文件为主 + 预留微信接口**：外部审批者直接
// 写 <dir>/approvals/<id>.json（人工 digest 落盘）经 Check 节流重扫可见；微信交互通道则调本
// 访问器 Decide(id, status, by) 回写。此前 Gate 不暴露 Approval → Decide 全仓无调用方 →
// critical 恒 Hold（审批闭环断路）。
func (g *GovernanceGate) Approval() *ApprovalManager {
	if g == nil {
		return nil
	}
	return g.approval
}

// Classifier/Goals/Config 暴露跨 agent 共享组件（W3）：buildAgent 为每个 agent 构造独立
// GovernanceGate（per-agent BudgetManager，用户裁决子 agent 独立预算）时复用这些共享件——
// Classifier 纯函数无状态、Goals 全局注册表、Config 同策略。均 nil-safe。
func (g *GovernanceGate) Classifier() *RiskClassifier {
	if g == nil {
		return nil
	}
	return g.classifier
}

// Goals 交出目标登记表；门为 nil 时返回 nil，调用方据此跳过目标判定。
func (g *GovernanceGate) Goals() *GoalRegistry {
	if g == nil {
		return nil
	}
	return g.goals
}

// Config 交出当前治理门配置；门为 nil 时返回零值配置（即未启用），而不是 panic。
func (g *GovernanceGate) Config() GateConfig {
	if g == nil {
		return GateConfig{}
	}
	return g.cfg
}

// Evaluate 对一个工具调用做治理裁决（管线：classify → 批准 → goal → 预算 → 记账）。
func (g *GovernanceGate) Evaluate(ctx RiskContext) Decision {
	if !g.Enabled() {
		return Decision{Disposition: DispositionAllow, Level: RiskLow, Reason: "治理未启用"}
	}
	level, ruleID, reason := g.classifier.Classify(ctx)
	disp := DispositionFor(level)
	digest := ArgsDigest(ctx.ArgsJSON)

	if level == RiskCritical {
		if g.approval == nil {
			g.record(SubtypeDenial, ctx, level, ruleID, "critical 无批准机制", digest, "")
			return Decision{Disposition: DispositionHold, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "critical 操作需人工批准，但运行时未配置批准机制（ApprovalManager）"}
		}
		if appr := g.approval.Check(ctx.ToolName, digest); appr != nil {
			g.record(SubtypeAudit, ctx, level, ruleID, "critical 已批准放行", digest, "")
			return Decision{Disposition: DispositionRecord, Level: level, RuleID: ruleID, Reason: "已批准"}
		}
		req, reqErr := g.approval.Request(ctx.ToolName, ctx.ArgsJSON, ctx.ArgsJSON, level.String(), ruleID, reason, "")
		if reqErr != nil {
			log.Warnf("[governance] approval Request failed for tool %q: %v", ctx.ToolName, reqErr)
		}
		approvalID := ""
		if req != nil {
			approvalID = req.ID
		}
		g.record(SubtypeApproval, ctx, level, ruleID, "critical 挂起待批准", digest, "")
		denied := g.cfg.Enforcement == EnforcementStrict
		denyReason := "critical 操作需人工批准（已登记请求 " + approvalID + "，批准后重试）"
		if reqErr != nil {
			denyReason = "critical 操作需人工批准，但审批请求登记失败（" + reqErr.Error() + "）——审批通道故障，请检查 governance dir 可写性"
		}
		return Decision{
			Disposition: DispositionHold, Level: level, RuleID: ruleID, Reason: reason,
			ApprovalID: approvalID, Denied: denied,
			DenyReason: denyReason,
		}
	}

	if level >= RiskHigh && g.goalRequired(ctx.TriggerSource) && g.goals != nil && !g.goals.HasActive() {
		g.record(SubtypeDenial, ctx, level, ruleID, "缺 goal 登记", digest, "")
		if g.cfg.Enforcement == EnforcementStrict {
			return Decision{Disposition: disp, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "high+ 自治操作须挂 goal——请先用 goal_declare 工具登记自治目标，或由运维清空 goal_required_for（A7；goal_declare 属 entry 治理面工具）"}
		}
	}

	if g.budget != nil {
		if err := g.budget.Admit(level); err != nil {
			g.record(SubtypeDenial, ctx, level, ruleID, "预算耗尽", digest, "")
			return Decision{Disposition: disp, Level: level, RuleID: ruleID, Reason: reason,
				Denied: true, DenyReason: "本窗口 " + level.String() + " 级操作预算已耗尽，请稍后或降低风险"}
		}
	}

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
		AgentName: g.agentName,
	})
}

// Ledger 暴露治理账本（诊断/审计查询入口）。
func (g *GovernanceGate) Ledger() *DenialLedger {
	if g == nil {
		return nil
	}
	return g.ledger
}
