package evolution

import (
	"context"
	"testing"
)

// TestRefinePropose_ThroughReleaseMachine 验证 refine propose 经发布状态机裁决（agent 无
// 直接激活权）：快道 + 后验通过 → active；提案本身不直接改 active。
func TestRefinePropose_ThroughReleaseMachine(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    fixedEvaluator{pass: true, score: 0.9},
		ValidateGate: gate(true, ""),
	})
	res, err := refinePropose(context.Background(), store, rm, refineArgs{
		Op: "propose", PromptKey: "system", Content: "v1 改进", Note: "提升清晰度",
	})
	if err != nil {
		t.Fatalf("refinePropose: %v", err)
	}
	if !res.OK || res.Stage != string(StageActive) || res.Lane != string(LaneFast) {
		t.Fatalf("快道后验通过应 active, got %+v", res)
	}
	if res.BundleID == base.ID {
		t.Fatal("propose 应产生新 draft（非基线）")
	}
	if store.Active().Prompts["system"] != "v1 改进" {
		t.Fatal("激活后 active 提示词应更新")
	}
}

// TestRefinePropose_NoReleaseManagerStaysDraft 验证无发布状态机时 propose 只创建 draft，
// 不激活（agent 无直接激活权——激活必经 ReleaseManager）。
func TestRefinePropose_NoReleaseManagerStaysDraft(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	res, err := refinePropose(context.Background(), store, nil, refineArgs{
		Op: "propose", PromptKey: "system", Content: "v1",
	})
	if err != nil {
		t.Fatalf("refinePropose: %v", err)
	}
	if res.Stage != string(StageDraft) {
		t.Fatalf("无发布机应停留 draft, got %+v", res)
	}
	if store.Active().ID != base.ID {
		t.Fatal("propose 不应直接改 active（agent 无直接激活权，D1 铁律）")
	}
}

// TestRefinePropose_PosteriorFailRollsBack 验证 propose 经快道后验劣化 → 回滚（指令4）。
func TestRefinePropose_PosteriorFailRollsBack(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	rm, _ := NewReleaseManager(ReleaseDeps{
		Store: store, Router: fixedRouter{LaneFast},
		Evaluator:    fixedEvaluator{pass: false, reason: "劣化"},
		ValidateGate: gate(true, ""),
	})
	res, _ := refinePropose(context.Background(), store, rm, refineArgs{
		Op: "propose", PromptKey: "system", Content: "v1 劣化",
	})
	if res.Stage != string(StageRolledBack) {
		t.Fatalf("后验劣化应回滚, got %+v", res)
	}
	if store.Active().ID != base.ID {
		t.Fatal("回滚后 active 应恢复基线")
	}
}

func TestRefinePropose_RequiresContent(t *testing.T) {
	store := newTestStore(t)
	_, _ = store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	if _, err := refinePropose(context.Background(), store, nil, refineArgs{Op: "propose", PromptKey: "system"}); err == nil {
		t.Fatal("缺 content 应报错")
	}
}

func TestRefineDiffStatusRollback(t *testing.T) {
	store := newTestStore(t)
	base, _ := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	draft, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")

	// diff active vs target。
	dres, err := refineDiff(store, refineArgs{Op: "diff", TargetID: draft.ID})
	if err != nil || dres.Diff == nil {
		t.Fatalf("diff 应返回差异, err=%v res=%+v", err, dres)
	}
	if len(dres.Diff.PromptsChanged) != 1 {
		t.Fatalf("应有 1 处提示词变更, got %+v", dres.Diff)
	}

	// status：active + 历史。
	sres, err := refineStatus(store)
	if err != nil || sres.Active == nil {
		t.Fatalf("status 应返回 active, err=%v", err)
	}
	if len(sres.History) != 2 {
		t.Fatalf("历史应含 2 bundle, got %d", len(sres.History))
	}

	// rollback 到 draft。
	rres, err := refineRollback(store, refineArgs{Op: "rollback", TargetID: draft.ID})
	if err != nil || !rres.OK {
		t.Fatalf("rollback 应成功, err=%v res=%+v", err, rres)
	}
	if store.Active().ID != draft.ID {
		t.Fatal("回滚后 active 应为 draft")
	}
	// rollback 缺 target 报错。
	if _, err := refineRollback(store, refineArgs{Op: "rollback"}); err == nil {
		t.Fatal("rollback 缺 target_id 应报错")
	}
}

func TestNewRefineTool_Constructs(t *testing.T) {
	store := newTestStore(t)
	rm, _ := NewReleaseManager(ReleaseDeps{Store: store})
	if NewRefineTool(store, rm) == nil {
		t.Fatal("refine 工具构造失败")
	}
}
