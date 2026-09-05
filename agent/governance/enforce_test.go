package governance

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
)

func TestBudgetManager_LimitAndExhaust(t *testing.T) {
	b := NewBudgetManager(BudgetConfig{Window: time.Hour, BucketCount: 6, MaxHighRisk: 2, MaxMediumRisk: 100}, "")
	if err := b.Admit(RiskHigh); err != nil {
		t.Fatalf("第1次 high 应放行: %v", err)
	}
	if err := b.Admit(RiskHigh); err != nil {
		t.Fatalf("第2次 high 应放行: %v", err)
	}
	if err := b.Admit(RiskHigh); err == nil {
		t.Fatal("第3次 high 应超预算 ErrBudgetExhausted")
	}
	// critical/low 不占预算。
	if err := b.Admit(RiskCritical); err != nil {
		t.Fatalf("critical 不占预算: %v", err)
	}
	if err := b.Admit(RiskLow); err != nil {
		t.Fatalf("low 不占预算: %v", err)
	}
}

func TestBudgetManager_PersistEpochNoReset(t *testing.T) {
	dir := t.TempDir()
	b1 := NewBudgetManager(BudgetConfig{MaxHighRisk: 2}, dir)
	_ = b1.Admit(RiskHigh)
	_ = b1.Admit(RiskHigh)
	// 重开（模拟重启）：预算不应被重置（epoch 持久化）。
	b2 := NewBudgetManager(BudgetConfig{MaxHighRisk: 2}, dir)
	if err := b2.Admit(RiskHigh); err == nil {
		t.Fatal("重启后预算应保留（第3次 high 应耗尽），防重启刷预算")
	}
}

func TestApprovalManager_RequestCheckDecide(t *testing.T) {
	a := NewApprovalManager("", time.Minute) // 纯内存
	args := `{"command":"rm -rf /x"}`
	digest := ArgsDigest(args)

	// 未批准 → Check 返回 nil。
	if a.Check("exec", digest) != nil {
		t.Fatal("未批准时 Check 应 nil")
	}
	req, err := a.Request("exec", args, "rm -rf /x", "critical", "exec.destructive", "破坏性", "")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(a.Pending()) != 1 {
		t.Fatal("应有 1 个 pending")
	}
	// 批准 → Check 命中（同 digest）。
	if err := a.Decide(req.ID, ApprovalApproved, "human"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got := a.Check("exec", digest); got == nil || got.ID != req.ID {
		t.Fatal("批准后 Check 应命中")
	}
	// 换参数（digest 不同）→ Check 不命中（防「批准后换参」）。
	if a.Check("exec", ArgsDigest(`{"command":"rm -rf /y"}`)) != nil {
		t.Fatal("换参数后 digest 不匹配，不应命中批准")
	}
}

func TestDenialLedger_RecordAndGovernanceEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("gov-test")
	l := NewDenialLedger(store, pid)
	l.Record(DenialRecord{Subtype: SubtypeDenial, ToolName: "exec", Level: RiskHigh, RuleID: "exec.delete", Reason: "删除"})
	if l.Count() != 1 {
		t.Fatalf("账本应 1 条, got %d", l.Count())
	}
	// governance 事件已写入 store（可 recall 审计）。
	refs, _ := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}})
	found := false
	for _, r := range refs {
		if r.EventType == "governance" {
			found = true
		}
	}
	if !found {
		t.Fatal("应写入 governance 事件")
	}
	// 重建：新账本从 store 恢复。
	l2 := NewDenialLedger(store, pid)
	if l2.Count() != 1 {
		t.Fatalf("重建后应恢复 1 条, got %d", l2.Count())
	}
}

func TestGoalRegistry_DeclareResolveExpire(t *testing.T) {
	g := NewGoalRegistry()
	if g.HasActive() {
		t.Fatal("初始无 active goal")
	}
	id := g.Declare("部署服务", "agent", 0)
	if !g.HasActive() {
		t.Fatal("声明后应有 active goal")
	}
	g.Resolve(id, GoalAchieved)
	if g.HasActive() {
		t.Fatal("resolve 后应无 active goal")
	}
	// 过期 goal 不算 active。
	g.Declare("临时", "agent", time.Now().Add(-time.Minute).UnixMilli())
	if g.HasActive() {
		t.Fatal("过期 goal 不应算 active")
	}
}

func TestGovernanceGate_DisabledAllowsAll(t *testing.T) {
	g := NewGovernanceGate(GateDeps{Config: GateConfig{Enabled: false}})
	d := g.Evaluate(RiskContext{ToolName: "exec", ArgsJSON: `{"command":"rm -rf /"}`})
	if d.Denied || d.Disposition != DispositionAllow {
		t.Fatalf("治理关闭应全放行, got %+v", d)
	}
}

func TestGovernanceGate_CriticalHoldAndApproval(t *testing.T) {
	appr := NewApprovalManager("", time.Minute)
	g := NewGovernanceGate(GateDeps{
		Approval: appr,
		Config:   GateConfig{Enabled: true, Enforcement: EnforcementStrict},
	})
	ctx := RiskContext{ToolName: "exec", ArgsJSON: `{"command":"rm -rf /"}`, TriggerSource: "user"}

	// critical 未批准 → hold + denied（strict）。
	d := g.Evaluate(ctx)
	if d.Disposition != DispositionHold || !d.Denied || d.ApprovalID == "" {
		t.Fatalf("critical 未批准应 hold+denied+登记请求, got %+v", d)
	}
	// 批准后 → 放行（record）。
	if err := appr.Decide(d.ApprovalID, ApprovalApproved, "human"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	d2 := g.Evaluate(ctx)
	if d2.Denied || d2.Disposition != DispositionRecord {
		t.Fatalf("批准后应放行, got %+v", d2)
	}
}

func TestGovernanceGate_BudgetExhaustDenies(t *testing.T) {
	b := NewBudgetManager(BudgetConfig{MaxHighRisk: 1}, "")
	g := NewGovernanceGate(GateDeps{Budget: b, Config: GateConfig{Enabled: true, Enforcement: EnforcementWarn}})
	// high 但非 critical、非需 goal 的 trigger（user）→ 走预算闸。
	ctx := RiskContext{ToolName: "delete_file", ArgsJSON: `{"path":"a"}`, TriggerSource: "user"}
	if d := g.Evaluate(ctx); d.Denied {
		t.Fatalf("第1次 high 应放行, got %+v", d)
	}
	// 第2次 high → 预算耗尽 → denied（即便 warn，预算是硬闸）。
	if d := g.Evaluate(ctx); !d.Denied {
		t.Fatalf("预算耗尽应 denied, got %+v", d)
	}
}

func TestGovernanceGate_GoalRequiredStrict(t *testing.T) {
	goals := NewGoalRegistry()
	g := NewGovernanceGate(GateDeps{
		Goals:  goals,
		Config: GateConfig{Enabled: true, Enforcement: EnforcementStrict, GoalRequiredFor: []string{"meditation"}},
	})
	// high + meditation + 无 goal + strict → denied。
	ctx := RiskContext{ToolName: "delete_file", ArgsJSON: `{"path":"a"}`, TriggerSource: "meditation"}
	if d := g.Evaluate(ctx); !d.Denied {
		t.Fatalf("high+meditation 无 goal strict 应 denied, got %+v", d)
	}
	// 声明 goal 后 → 放行。
	goals.Declare("清理", "agent", 0)
	if d := g.Evaluate(ctx); d.Denied {
		t.Fatalf("有 goal 后应放行, got %+v", d)
	}
}
