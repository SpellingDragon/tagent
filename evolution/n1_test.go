package evolution

import (
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

// TestRefineRollback_BaselineAllowed 是 N1 回归：refine rollback 到基线（经 NoteActive seed）
// 必须可用——修复前基线不在 wasActive 白名单 → rollback 到基线恒被拒。
func TestRefineRollback_BaselineAllowed(t *testing.T) {
	store := newTestStore(t)
	base, err := store.InitBaseline(map[string]string{"system": "v0"}, BundleParams{}, ModelRef{})
	if err != nil {
		t.Fatalf("InitBaseline: %v", err)
	}
	rm, _ := NewReleaseManager(ReleaseDeps{Store: store})

	// 未 seed 前：基线不在白名单 → rollback 被拒（复现 N1 症状）。
	if _, err := refineRollback(store, rm, refineArgs{Op: "rollback", TargetID: base.ID}); err == nil {
		t.Fatal("N1 前置: 未 NoteActive 时 rollback 到基线应被拒(复现症状)")
	}

	// seed 基线（NoteActive）后：rollback 到基线可用。
	rm.NoteActive(base.ID, "baseline init")
	res, err := refineRollback(store, rm, refineArgs{Op: "rollback", TargetID: base.ID})
	if err != nil || !res.OK {
		t.Fatalf("N1: rollback 到基线应成功(基线经 NoteActive seed 入白名单), err=%v res=%+v", err, res)
	}
}
