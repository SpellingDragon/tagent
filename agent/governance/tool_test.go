package governance

import (
	"context"
	"strings"
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// fakeCallable 是记录调用的内层工具替身（实现 trpctool.Tool + CallableTool）。
type fakeCallable struct {
	name   string
	called bool
	result any
}

func (f *fakeCallable) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: f.name}
}

func (f *fakeCallable) Call(context.Context, []byte) (any, error) {
	f.called = true
	return f.result, nil
}

func TestGovernanceTool_DeniesCritical(t *testing.T) {
	inner := &fakeCallable{name: "exec", result: "should-not-run"}
	gate := NewGovernanceGate(GateDeps{Config: GateConfig{Enabled: true, Enforcement: EnforcementStrict}})
	gt := NewGovernanceTool(inner, gate)

	res, err := gt.Call(context.Background(), []byte(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if inner.called {
		t.Fatal("critical 未批准应拒绝——内层工具不应被执行")
	}
	s, _ := res.(string)
	if !strings.Contains(s, "governance_denied") {
		t.Fatalf("拒绝应以 result 渗透治理理由, got %v", res)
	}
}

func TestGovernanceTool_PassthroughLowRisk(t *testing.T) {
	inner := &fakeCallable{name: "read_file", result: "file-content"}
	gate := NewGovernanceGate(GateDeps{Config: GateConfig{Enabled: true, Enforcement: EnforcementStrict}})
	gt := NewGovernanceTool(inner, gate)

	res, err := gt.Call(context.Background(), []byte(`{"path":"a.txt"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !inner.called {
		t.Fatal("low 风险应放行——内层工具应被执行")
	}
	if res != "file-content" {
		t.Fatalf("应透传内层结果, got %v", res)
	}
}

func TestGovernanceTool_DisabledZeroOverhead(t *testing.T) {
	inner := &fakeCallable{name: "exec", result: "ran"}
	gate := NewGovernanceGate(GateDeps{Config: GateConfig{Enabled: false}}) // 治理关闭
	gt := NewGovernanceTool(inner, gate)

	// critical 参数但治理关闭 → 透传执行（现状零行为变化）。
	res, _ := gt.Call(context.Background(), []byte(`{"command":"rm -rf /"}`))
	if !inner.called {
		t.Fatal("治理关闭应透传（即便 critical 参数）")
	}
	if res != "ran" {
		t.Fatalf("应透传内层结果, got %v", res)
	}
}

func TestGovernanceTool_NilGatePassthrough(t *testing.T) {
	inner := &fakeCallable{name: "exec", result: "ran"}
	gt := NewGovernanceTool(inner, nil) // nil gate
	if _, err := gt.Call(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("nil gate 应透传: %v", err)
	}
	if !inner.called {
		t.Fatal("nil gate 应透传执行")
	}
}

func TestGovernanceTool_DeclarationPassthrough(t *testing.T) {
	inner := &fakeCallable{name: "exec"}
	gt := NewGovernanceTool(inner, nil)
	decl := gt.Declaration()
	if decl == nil || decl.Name != "exec" {
		t.Fatal("Declaration 应透传内层（治理不改声明区 → prefix-cache 稳定）")
	}
	if gt.Inner() != trpctool.Tool(inner) {
		t.Fatal("Inner 应返回内层工具")
	}
}

func TestGovernanceTool_TriggerSourceFromCtx(t *testing.T) {
	// goal-required 判定依赖 ctx 触发源：meditation 触发 goal 检查。
	goals := NewGoalRegistry()
	gate := NewGovernanceGate(GateDeps{
		Goals:  goals,
		Config: GateConfig{Enabled: true, Enforcement: EnforcementStrict, GoalRequiredFor: []string{"meditation"}},
	})
	inner := &fakeCallable{name: "delete_file", result: "deleted"}
	gt := NewGovernanceTool(inner, gate)

	// meditation 触发 + high 风险 + 无 goal + strict → 拒绝。
	ctx := WithTriggerSource(context.Background(), "meditation")
	res, _ := gt.Call(ctx, []byte(`{"path":"a"}`))
	if inner.called {
		t.Fatal("meditation+high+无goal+strict 应拒绝")
	}
	if s, _ := res.(string); !strings.Contains(s, "governance_denied") {
		t.Fatalf("应拒绝, got %v", res)
	}

	// user 触发（非 goal-required）→ 放行。
	inner2 := &fakeCallable{name: "delete_file", result: "deleted"}
	gt2 := NewGovernanceTool(inner2, gate)
	ctxUser := WithTriggerSource(context.Background(), "user")
	if _, err := gt2.Call(ctxUser, []byte(`{"path":"a"}`)); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !inner2.called {
		t.Fatal("user 触发不需 goal，应放行")
	}
}

func TestTriggerSourceCtxCarrier(t *testing.T) {
	ctx := WithTriggerSource(context.Background(), "task")
	if TriggerSourceFrom(ctx) != "task" {
		t.Fatal("应读回触发源")
	}
	if TriggerSourceFrom(context.Background()) != "" {
		t.Fatal("未盖章应返回空")
	}
}
