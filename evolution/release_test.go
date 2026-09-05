package evolution

import (
	"context"
	"fmt"
	"testing"
)

// === 测试替身 ===

type fixedRouter struct{ lane Lane }

func (r fixedRouter) Route(BundleDiff) Lane { return r.lane }

type fixedEvaluator struct {
	pass   bool
	score  float64
	reason string
}

func (e fixedEvaluator) Evaluate(context.Context, string) (EvalResult, error) {
	return EvalResult{Pass: e.pass, Score: e.score, Reason: e.reason}, nil
}

type fixedGuardrail struct {
	breach bool
	reason string
}

func (g fixedGuardrail) Breach(string) (bool, string) { return g.breach, g.reason }

func gate(pass bool, reason string) GateFunc {
	return func(context.Context, *Bundle) (bool, string) { return pass, reason }
}

func newTestStore(t *testing.T) *BundleStore {
	t.Helper()
	s, err := NewBundleStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleStore: %v", err)
	}
	return s
}

// TestRelease_FastLane_PosteriorPass 验证快道（指令4 后验评估）：低风险 → 先激活 canary
// → 后验评估通过 → 正式 active。这是「先生效、后验评估」的调和落地。
func TestRelease_FastLane_PosteriorPass(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1 措辞微调"}, BundleParams{}, ModelRef{}, "refine", "")

	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    fixedEvaluator{pass: true, score: 0.9},
		ValidateGate: gate(true, ""),
	})
	rec, err := rm.Submit(context.Background(), draft)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if rec.Lane != LaneFast || rec.Stage != StageActive {
		t.Fatalf("快道后验通过应 active, got %+v", rec)
	}
	if store.Active().ID != draft.ID {
		t.Fatal("active 应为新 bundle")
	}
}

// TestRelease_FastLane_PosteriorFailRollback 验证快道后验劣化 → 模型决策回滚（指令4）。
func TestRelease_FastLane_PosteriorFailRollback(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1 劣化"}, BundleParams{}, ModelRef{}, "refine", "")

	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    fixedEvaluator{pass: false, score: 0.2, reason: "质量下降"},
		ValidateGate: gate(true, ""),
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Stage != StageRolledBack {
		t.Fatalf("后验劣化应回滚, got %+v", rec)
	}
	if store.Active().ID != base.ID {
		t.Fatal("回滚后 active 应恢复基线")
	}
}

// TestRelease_FastLane_GuardrailBreachRollback 验证双回滚触发①：guardrail 确定性闸违约 → 回滚。
func TestRelease_FastLane_GuardrailBreachRollback(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")

	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Guardrail:    fixedGuardrail{breach: true, reason: "错误率飙升"},
		Evaluator:    fixedEvaluator{pass: true}, // 即便评估通过，guardrail 先违约
		ValidateGate: gate(true, ""),
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Stage != StageRolledBack {
		t.Fatalf("guardrail 违约应回滚, got %+v", rec)
	}
	if store.Active().ID != base.ID {
		t.Fatal("回滚后 active 应恢复基线")
	}
}

// TestRelease_SlowLane_GatedActivation 验证慢道（D1 门后生效）：高风险 → replay/shadow/approve
// 全过才 active；任一门失败则 rejected（不激活）。
func TestRelease_SlowLane_GatedActivation(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1 换模型"}, BundleParams{}, ModelRef{Name: "new"}, "refine", "")

	// 全门通过 → active。
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneSlow},
		ValidateGate: gate(true, ""), ReplayGate: gate(true, ""), ShadowGate: gate(true, ""),
		ApproveGate: gate(true, ""), Config: ReleaseConfig{RequireApproval: true},
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Lane != LaneSlow || rec.Stage != StageActive {
		t.Fatalf("慢道全门通过应 active, got %+v", rec)
	}

	// replay 门失败 → rejected，active 不变（门后生效：未过门不激活）。
	store2 := newTestStore(t)
	base2, _ := store2.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft2, _ := store2.Create(base2, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	rm2, _ := NewReleaseManager(ReleaseDeps{
		Store: store2, Router: fixedRouter{LaneSlow},
		ValidateGate: gate(true, ""), ReplayGate: gate(false, "回放发现回归"),
	})
	rec2, _ := rm2.Submit(context.Background(), draft2)
	if rec2.Stage != StageRejected {
		t.Fatalf("replay 失败应 rejected, got %+v", rec2)
	}
	if store2.Active().ID != base2.ID {
		t.Fatal("门失败不应激活（active 保持基线）")
	}
}

// TestRelease_ProtectedPromptForcesSlowLane 验证 protected 提示词改动强制走慢道（即便
// router 判低风险）——安全兜底。
func TestRelease_ProtectedPromptForcesSlowLane(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"soul": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"soul": "v1 改灵魂"}, BundleParams{}, ModelRef{}, "refine", "")

	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast}, // router 说快道
		ValidateGate: gate(true, ""), ReplayGate: gate(true, ""), ShadowGate: gate(true, ""),
		ApproveGate: gate(true, ""),
		Config:      ReleaseConfig{ProtectedPrompts: []string{"soul"}, RequireApproval: true},
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Lane != LaneSlow {
		t.Fatalf("protected 提示词应强制慢道, got lane=%s", rec.Lane)
	}
}

// TestRelease_ValidateGateAlwaysRequired 验证 validate 门恒过要求：失败则 rejected（两条道共用）。
func TestRelease_ValidateGateAlwaysRequired(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		ValidateGate: gate(false, "schema 非法"),
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Stage != StageRejected {
		t.Fatalf("validate 失败应 rejected, got %+v", rec)
	}
	if store.Active().ID != base.ID {
		t.Fatal("validate 失败不应激活")
	}
}

// TestRelease_AgentCannotDirectlyActivate 验证 D1 铁律：refine 只能 propose（Submit），
// 激活由状态机裁决——draft 创建后 active 不变，直到 Submit 走门。
func TestRelease_AgentCannotDirectlyActivate(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	// Create 不改 active（agent 无直接激活权）。
	if store.Active().ID != base.ID {
		t.Fatal("Create draft 不应改 active（agent 无直接激活权，D1 铁律）")
	}
	_ = draft
}

// errEvaluator 模拟评估器不可用（LLM 超时/ctx 取消）。
type errEvaluator struct{}

func (errEvaluator) Evaluate(context.Context, string) (EvalResult, error) {
	return EvalResult{}, fmt.Errorf("evaluator unavailable")
}

// TestRelease_FastLane_EvaluatorErrorRollsBack 是 Blocker 回归：评估器返回 error 时
// 绝不可被当「通过」造成假激活，须保守回滚到父版本。
func TestRelease_FastLane_EvaluatorErrorRollsBack(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    errEvaluator{}, // 评估失败
		ValidateGate: gate(true, ""),
	})
	rec, _ := rm.Submit(context.Background(), draft)
	if rec.Stage != StageRolledBack {
		t.Fatalf("评估器 error 应保守回滚（非假激活）, got stage=%s", rec.Stage)
	}
	if store.Active().ID != base.ID {
		t.Fatal("评估失败回滚后 active 应恢复基线")
	}
}

// TestRelease_FastLane_CanaryCtxCancelStaysCanary 是 Major 回归：canary 观察窗内 ctx 取消
// → 诚实停留 canary（不回滚也不提升为「后验通过 active」），杜绝未评估变更被假通过。
func TestRelease_FastLane_CanaryCtxCancelStaysCanary(t *testing.T) {
	store := newTestStore(t)
	_, _ = store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(nil, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    fixedEvaluator{pass: true}, // 即便会判通过，ctx 取消也不应走到评估
		ValidateGate: gate(true, ""),
		Config:       ReleaseConfig{CanaryHoldMs: 5000},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消 → canary hold 的 select 命中 ctx.Done
	rec, _ := rm.Submit(ctx, draft)
	if rec.Stage != StageCanary {
		t.Fatalf("ctx 取消应诚实停留 canary（非 active 假通过、非回滚）, got stage=%s", rec.Stage)
	}
}
