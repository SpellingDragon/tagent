package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// D1: saveResidentMeta persists parameters for named resident sessions only.
func TestSaveResidentMeta(t *testing.T) {
	ct := &ActionTool{}
	// redirect metaDir by writing into the real one? metaDir is fixed to
	// os.TempDir()/tagent-resident-meta — use the exported behavior via a
	// scratch instance is not possible; test the on-disk contract through a
	// real save + read cycle, then clean up.
	defer os.RemoveAll(ct.metaDir())

	resident := ActionArgs{Name: "dev-server", Mode: "resident", Watch: "ERROR", Probe: "curl -sf :1", ProbeIntervalSec: 30, ProbeFailures: 3}
	ct.saveResidentMeta("n-dev-server", resident)
	b, err := os.ReadFile(ct.metaPath("n-dev-server"))
	if err != nil {
		t.Fatalf("meta not saved: %v", err)
	}
	var m ResidentMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "dev-server" || m.Mode != "resident" || m.Watch != "ERROR" || m.Probe != "curl -sf :1" {
		t.Fatalf("meta mismatch: %+v", m)
	}

	// Oneshot sessions must NOT persist.
	oneshot := ActionArgs{Name: "one", Mode: ""}
	ct.saveResidentMeta("n-one", oneshot)
	if _, err := os.Stat(ct.metaPath("n-one")); !os.IsNotExist(err) {
		t.Fatal("oneshot session must not persist metadata")
	}
}

// D2: SweepStaleResidents kills sessions past TTL (and drops their records)
// but leaves fresh ones alone. Uses a fake now; kills only touch metadata of
// nonexistent tmux sessions (KillSession on absent id fails → record still
// dropped is NOT asserted; here we assert the decision logic via fresh vs stale).
func TestSweepStaleResidents(t *testing.T) {
	ct := &ActionTool{}
	defer os.RemoveAll(ct.metaDir())
	_ = os.MkdirAll(ct.metaDir(), 0o755)

	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	fresh := time.Now().Format(time.RFC3339)
	os.WriteFile(ct.metaPath("n-old"), []byte(`{"name":"old","mode":"resident","spawned_at":"`+old+`"}`), 0o600)
	os.WriteFile(ct.metaPath("n-new"), []byte(`{"name":"new","mode":"resident","spawned_at":"`+fresh+`"}`), 0o600)

	ct.SweepStaleResidents(time.Now())

	// Stale record dropped (kill of a nonexistent tmux session errors, but
	// the record is removed regardless of that error path).
	if _, err := os.Stat(ct.metaPath("n-old")); !os.IsNotExist(err) {
		t.Fatal("stale resident record should be removed")
	}
	// Fresh record kept.
	if _, err := os.Stat(ct.metaPath("n-new")); err != nil {
		t.Fatal("fresh resident record must survive the sweep")
	}
}

// D2: cap logic.
func TestCanSpawnResident(t *testing.T) {
	old := maxResidentSessions
	maxResidentSessions = 1
	defer func() { maxResidentSessions = old }()

	ct := &ActionTool{}
	ct.tmuxMonitor = NewTmuxMonitor(WithMonitorExecutor(nil))
	ct.tmuxMonitor.AddSession(&TmuxSession{ID: "n-a", Mode: ModeResident})
	if ct.CanSpawnResident() {
		t.Fatal("cap=1 with 1 resident must refuse")
	}
	maxResidentSessions = 2
	if !ct.CanSpawnResident() {
		t.Fatal("cap=2 with 1 resident must allow")
	}
}

// D1: reattach skips oneshot metadata and corrupt files without dying.
func TestReattachResidentSessions_Defensive(t *testing.T) {
	ct := &ActionTool{}
	defer os.RemoveAll(ct.metaDir())
	_ = os.MkdirAll(ct.metaDir(), 0o755)
	os.WriteFile(filepath.Join(ct.metaDir(), "sess-n-corrupt.json"), []byte("{not json"), 0o600)
	os.WriteFile(filepath.Join(ct.metaDir(), "bogus.txt"), []byte("x"), 0o600)

	// No tmux executor/monitor (nil) → returns 0, no panic.
	if n := ct.ReattachResidentSessions(); n != 0 {
		t.Fatalf("nil executor must return 0, got %d", n)
	}
	_ = strings.TrimSpace // keep strings import if assertions change
}

// hardening-review-batch2 2.1/2.2：ADOPT-FIRST 语义——运行多日的会话只要被
// 收养（LastAdoptedAt 刷新）就 MUST NOT 被 Sweep 清理；Sweep 只收割「无人
// 收养且超 TTL」的孤儿（freshness 自 LastAdoptedAt 起算，legacy 回退
// SpawnedAt）。SpawnedAt 年龄不再是孤儿证据。
func TestSweepStaleResidents_AdoptedLongRunnerSurvives(t *testing.T) {
	ct := &ActionTool{}
	defer os.RemoveAll(ct.metaDir())
	_ = os.MkdirAll(ct.metaDir(), 0o755)

	// 会话 48h 前 spawn，但 1h 前刚被收养（LastAdoptedAt）。
	oldSpawn := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	recentAdopt := time.Now().Add(-time.Hour).Format(time.RFC3339)
	os.WriteFile(ct.metaPath("n-longrun"), []byte(
		`{"name":"lr","mode":"resident","spawned_at":"`+oldSpawn+`","last_adopted_at":"`+recentAdopt+`"}`), 0o600)

	if killed := ct.SweepStaleResidents(time.Now()); killed != 0 {
		t.Fatalf("adopted long-runner must survive the sweep, killed=%d", killed)
	}
	if _, err := os.Stat(ct.metaPath("n-longrun")); err != nil {
		t.Fatal("adopted long-runner record must survive")
	}

	// 对照：无 LastAdoptedAt（legacy）且 SpawnedAt 超龄 → 被收割。
	// （零值 tool 无 executor，killed 恒 0——与既有 TestSweepStaleResidents
	// 同口径，以记录删除为裁决断言。）
	os.WriteFile(ct.metaPath("n-legacy-orphan"), []byte(
		`{"name":"lo","mode":"resident","spawned_at":"`+oldSpawn+`"}`), 0o600)
	_ = ct.SweepStaleResidents(time.Now())
	if _, err := os.Stat(ct.metaPath("n-legacy-orphan")); !os.IsNotExist(err) {
		t.Fatal("legacy orphan record must be removed")
	}
}

// 收养路径刷新 LastAdoptedAt：reattachOne + touchAdopted 后 meta 落盘。
func TestTouchAdopted_StampsLastAdoptedAt(t *testing.T) {
	ct := &ActionTool{}
	defer os.RemoveAll(ct.metaDir())
	_ = os.MkdirAll(ct.metaDir(), 0o755)
	os.WriteFile(ct.metaPath("n-x"), []byte(`{"name":"x","mode":"resident","spawned_at":"2026-01-01T00:00:00Z"}`), 0o600)

	ct.touchAdopted("n-x")

	b, err := os.ReadFile(ct.metaPath("n-x"))
	if err != nil {
		t.Fatal(err)
	}
	var m ResidentMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.LastAdoptedAt == "" {
		t.Fatal("LastAdoptedAt must be stamped by touchAdopted")
	}
	if _, err := time.Parse(time.RFC3339, m.LastAdoptedAt); err != nil {
		t.Fatalf("LastAdoptedAt not RFC3339: %v", err)
	}
}
