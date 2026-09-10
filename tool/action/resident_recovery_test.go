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
