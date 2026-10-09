// 本文件负责门控锚点的跨重启持久性：缺失按 0 处理、历史文件多余键忽略，以及静默保活——
// 否则重启后无法把"长期活着但没写入"与"真空闲"区分开。
// 契约: docs/wiki/reliability/durable-delivery.md#anchor-persistence
package reliability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnchorStore_SaveLoadRoundtrip(t *testing.T) {
	s, err := NewAnchorStore(filepath.Join(t.TempDir(), "anchors.json"))
	if err != nil {
		t.Fatalf("NewAnchorStore: %v", err)
	}
	a, err := s.Load()
	if err != nil || a.LastTurnEnd != 0 {
		t.Fatalf("首次 Load 应零值无错, got %+v err=%v", a, err)
	}
	want := MeditationAnchors{LastTurnEnd: 200, LastMeditation: 300}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip 应保真, got %+v want %+v", got, want)
	}
}

func TestAnchorStore_PersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	s1, _ := NewAnchorStore(path)
	_ = s1.Save(MeditationAnchors{LastTurnEnd: 2, LastMeditation: 3})
	s2, _ := NewAnchorStore(path)
	got, _ := s2.Load()
	if got.LastTurnEnd != 2 || got.LastMeditation != 3 {
		t.Fatalf("重启应恢复两锚点, got %+v", got)
	}
}

// TestAnchorStore_LegacyFileIgnoresUnknownKey 钉住 带多余键的锚点文件在 Load 时自然兼容。
// - last_user_input 这类未知键被忽略、无错误，两锚照常恢复，不存在迁移步骤。
func TestAnchorStore_LegacyFileIgnoresUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	legacy := `{"last_user_input":111,"last_turn_end":222,"last_meditation":333}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	s, _ := NewAnchorStore(path)
	got, err := s.Load()
	if err != nil {
		t.Fatalf("legacy 三键文件 Load 应无错（未知键忽略）: %v", err)
	}
	if got.LastTurnEnd != 222 || got.LastMeditation != 333 {
		t.Fatalf("两锚应从旧文件照常恢复, got %+v", got)
	}
}

func TestAnchorStore_EmptyPathError(t *testing.T) {
	if _, err := NewAnchorStore(""); err == nil {
		t.Fatal("空 path 应 error")
	}
}

func TestAnchorStore_NilSafe(t *testing.T) {
	var s *AnchorStore
	if a, err := s.Load(); err != nil || a.LastTurnEnd != 0 {
		t.Fatal("nil Load 应零值无错")
	}
	if err := s.Save(MeditationAnchors{LastTurnEnd: 5}); err != nil {
		t.Fatal("nil Save 应无错（no-op）")
	}
	if s.Path() != "" {
		t.Fatal("nil Path 应空")
	}
}

func TestAnchorStore_CorruptFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	s, _ := NewAnchorStore(path)
	if _, err := s.Load(); err == nil {
		t.Fatal("坏文件 Load 应 error")
	}
}
