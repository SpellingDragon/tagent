package evolution

import (
	"context"
	"testing"
)

// TestReleaseHistory_PersistReload 是 N1（§8.9）回归：发布历史持久化到 releases.jsonl，重启
// （新 ReleaseManager 同 dir）后 loadHistory 恢复，wasActive 白名单跨重启有效。否则 history 仅
// 内存态，重启清零 → 所有 refine rollback 失效（E1 修复的后遗症）。
func TestReleaseHistory_PersistReload(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBundleStore(dir)
	if err != nil {
		t.Fatalf("NewBundleStore: %v", err)
	}
	base, err := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	if err != nil {
		t.Fatalf("InitBaseline: %v", err)
	}

	rm1, _ := NewReleaseManager(ReleaseDeps{Store: store})
	rm1.NoteActive(base.ID, "baseline init") // 模拟 buildAgent InitBaseline 后 seed 基线
	if !rm1.wasActive(base.ID) {
		t.Fatal("N1: NoteActive 后基线应在 wasActive 白名单")
	}

	// 重启：新 ReleaseManager 同 dir → loadHistory 从 releases.jsonl 恢复。
	rm2, _ := NewReleaseManager(ReleaseDeps{Store: store})
	if !rm2.wasActive(base.ID) {
		t.Fatal("N1: 重启后 wasActive 应从持久化 releases.jsonl 恢复基线（否则 rollback 全失效）")
	}
}

// TestRefineRollback_BaselineAllowed 是 N1 + ④（§8.10）回归：refine rollback 到基线必须可用——
// 修复前基线不在 wasActive 白名单 → rollback 到基线恒被拒。④ 后 NewReleaseManager 经
// seedActiveBaseline 自动把当前 active 基线入白名单（无需手动 NoteActive，修 InitBaseline 崩溃窗口：
// active.json 已写而 releases.jsonl 未写时重启仍能回滚基线）。同时验证 E1 白名单铁律不破：从未激活
// 的 draft（创建但未 Submit → 非 Stage=active）仍被拒，防绕过发布道直接激活被拒 draft。
func TestRefineRollback_BaselineAllowed(t *testing.T) {
	store := newTestStore(t)
	base, err := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	if err != nil {
		t.Fatalf("InitBaseline: %v", err)
	}
	rm, _ := NewReleaseManager(ReleaseDeps{Store: store})

	// ④：NewReleaseManager 经 seedActiveBaseline 自动 seed 当前 active 基线 → rollback 到基线可用
	// （无需手动 NoteActive；此前未 seed 时恒被拒即 N1 症状，④ 从启动路径根除）。
	res, err := refineRollback(store, rm, refineArgs{Op: "rollback", TargetID: base.ID})
	if err != nil || !res.OK {
		t.Fatalf("N1+④: rollback 到基线应成功(seedActiveBaseline 自动入白名单), err=%v res=%+v", err, res)
	}

	// E1 白名单铁律：从未激活的 draft（创建但未 Submit → 不在 wasActive 白名单）仍被拒——
	// 防 agent 经 rollback 直接 SetActive 任意在盘 bundle（含被拒 draft）绕过发布道。
	draft, _ := store.Create(base, map[string]string{"system": "v1 未发布"}, BundleParams{}, ModelRef{}, "refine", "")
	if _, err := refineRollback(store, rm, refineArgs{Op: "rollback", TargetID: draft.ID}); err == nil {
		t.Fatal("E1: rollback 到从未激活的 draft 应被拒(不在 wasActive 白名单，防绕过发布道直接激活)")
	}
}

// TestRelease_SubmitReloadRefineRollback 是 ②（§9.1）N1 集成回归：完整生命周期——Submit 一个
// draft 经快道正式 active（StageActive 持久化到 releases.jsonl）→ 重载（新 ReleaseManager 同 dir，
// loadHistory 恢复历史 + ④ seedActiveBaseline 补当前 active）→ refineRollback 回滚到该曾 active 的
// bundle 成功。此前 N1 单测只覆盖 NoteActive seed 的基线（PersistReload/BaselineAllowed），**未覆盖
// 经 Submit 发布道真正激活的版本跨重启可回滚**——即 wasActive 白名单对"发布过的版本"的持久化恢复，
// 同时联验 ④ seedActiveBaseline（NewReleaseManager 自动把当前 active 入白名单，无需手动 NoteActive）。
func TestRelease_SubmitReloadRefineRollback(t *testing.T) {
	store := newTestStore(t)
	base, err := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	if err != nil {
		t.Fatalf("InitBaseline: %v", err)
	}

	// rm1：④ seedActiveBaseline 自动把当前 active(base) 入白名单（无需手动 NoteActive）。
	rm1, _ := NewReleaseManager(ReleaseDeps{Store: store, Router: fixedRouter{LaneFast}, ValidateGate: gate(true, "")})
	if !rm1.wasActive(base.ID) {
		t.Fatal("④: NewReleaseManager 应经 seedActiveBaseline 自动把 active 基线入 wasActive 白名单")
	}

	// Submit 两个 draft（快道无评估器 → canary 即 active，各自 StageActive 持久化到 releases.jsonl）。
	draft1, _ := store.Create(base, map[string]string{"system": "v1"}, BundleParams{}, ModelRef{}, "refine", "")
	rec1, err := rm1.Submit(context.Background(), draft1)
	if err != nil || rec1.Stage != StageActive {
		t.Fatalf("Submit draft1 应快道 active, err=%v rec=%+v", err, rec1)
	}
	draft2, _ := store.Create(draft1, map[string]string{"system": "v2"}, BundleParams{}, ModelRef{}, "refine", "")
	rec2, err := rm1.Submit(context.Background(), draft2)
	if err != nil || rec2.Stage != StageActive {
		t.Fatalf("Submit draft2 应快道 active, err=%v rec=%+v", err, rec2)
	}
	if store.Active().ID != draft2.ID {
		t.Fatalf("active 应为 draft2, got %s", store.Active().ID)
	}

	// 重载：新 ReleaseManager 同 dir → loadHistory 从 releases.jsonl 恢复 + seedActiveBaseline。
	rm2, _ := NewReleaseManager(ReleaseDeps{Store: store, Router: fixedRouter{LaneFast}, ValidateGate: gate(true, "")})
	// 经 Submit 发布道激活的 draft1 跨重启仍在 wasActive 白名单（持久化恢复，非仅内存态）。
	if !rm2.wasActive(draft1.ID) {
		t.Fatal("②: 重载后 wasActive 应从 releases.jsonl 恢复经 Submit 激活的 draft1（非仅 seed 基线）")
	}

	// refineRollback 回滚到 draft1（曾 active）成功——完整 Submit→重载→rollback 生命周期闭环。
	res, err := refineRollback(store, rm2, refineArgs{Op: "rollback", TargetID: draft1.ID})
	if err != nil || !res.OK {
		t.Fatalf("②: 重载后 refineRollback 到曾 active 的 draft1 应成功, err=%v res=%+v", err, res)
	}
	if store.Active().ID != draft1.ID {
		t.Fatalf("②: rollback 后 active 应为 draft1, got %s", store.Active().ID)
	}
}
