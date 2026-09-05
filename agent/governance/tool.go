package governance

import (
	"context"
	"fmt"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ==================== GovernanceTool（T-G · 治理闸装饰器）====================
//
// 装饰一个 tool.Tool：Call 前经 GovernanceGate 裁决。拒绝则以 result 渗透治理理由（失败
// 是一等资产——模型据此自纠，而非 panic/静默），不执行内层工具；放行则委托内层。
// Declaration 透传内层（工具声明恒定 → prefix-cache 稳定性不变量：治理不改声明区）。
//
// 包裹顺序：OutputLimitTool(GovernanceTool(rawTool))——agent.go 随后包 OutputLimitTool，
// 二者均实现 CallableTool，链式委托。sub-agent 包装器（*agent.AgentToolWrapper）不被包裹
// （下游需按具体类型断言接 parentProjection），治理聚焦 leaf 工具（exec/file/mcp 主风险面）。

// triggerSourceKeyType 是 ctx 中触发源的键类型（私有类型防冲突）。
type triggerSourceKeyType struct{}

var triggerSourceKey = triggerSourceKeyType{}

// WithTriggerSource 把触发源（user/meditation/task/tmux/subagent/inject）存入 ctx。
// event loop 每回合盖章，GovernanceTool 读取用于 goal-required 判定（meditation/task 须挂 goal）。
func WithTriggerSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, triggerSourceKey, source)
}

// TriggerSourceFrom 从 ctx 读触发源（未盖章则空——goal 检查据此不误触发）。
func TriggerSourceFrom(ctx context.Context) string {
	if s, ok := ctx.Value(triggerSourceKey).(string); ok {
		return s
	}
	return ""
}

// GovernanceTool 是治理闸装饰器（实现 trpctool.Tool + trpctool.CallableTool）。
type GovernanceTool struct {
	inner trpctool.Tool
	gate  *GovernanceGate
}

// NewGovernanceTool 包裹内层工具。gate 为 nil 或关闭时 Call 直接透传（零开销）。
func NewGovernanceTool(inner trpctool.Tool, gate *GovernanceGate) *GovernanceTool {
	return &GovernanceTool{inner: inner, gate: gate}
}

// Declaration 透传内层声明（治理不改工具声明 → prefix-cache 稳定）。
func (t *GovernanceTool) Declaration() *trpctool.Declaration { return t.inner.Declaration() }

// Inner 返回被包裹的内层工具（供下游按具体类型断言，如需要）。
func (t *GovernanceTool) Inner() trpctool.Tool { return t.inner }

// Call 先经治理闸裁决，放行才委托内层执行。
func (t *GovernanceTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	callable, ok := t.inner.(trpctool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("GovernanceTool: inner tool %T does not implement CallableTool", t.inner)
	}
	// 治理关闭 → 透传（零行为变化）。
	if t.gate == nil || !t.gate.Enabled() {
		return callable.Call(ctx, jsonArgs)
	}

	toolName := ""
	if decl := t.inner.Declaration(); decl != nil {
		toolName = decl.Name
	}
	decision := t.gate.Evaluate(RiskContext{
		ToolName:      toolName,
		ArgsJSON:      string(jsonArgs),
		TriggerSource: TriggerSourceFrom(ctx),
	})
	// 拒绝或挂起（Hold=critical 待批准）均不执行内层——纵深防御：即便 Denied 标志未置，
	// Hold 处置也意味着「不可现在执行」。以 result 渗透治理理由（模型自纠材料）。
	if decision.Denied || decision.Disposition == DispositionHold {
		reason := decision.DenyReason
		if reason == "" {
			reason = "操作被挂起（需批准）"
		}
		return fmt.Sprintf("[governance_denied] 操作被治理闸拒绝：%s（风险级别=%s，命中规则=%s）。"+
			"请调整操作或走批准/goal 登记流程后重试。",
			reason, decision.Level, decision.RuleID), nil
	}
	// 放行（allow / record / critical 已批准）：委托内层执行。
	return callable.Call(ctx, jsonArgs)
}
