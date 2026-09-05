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
	// 首次 Load（无文件）→ 零值 + nil（首次启动，冥想门控从头）。
	a, err := s.Load()
	if err != nil || a.LastTurnEnd != 0 {
		t.Fatalf("首次 Load 应零值无错, got %+v err=%v", a, err)
	}
	// Save + Load roundtrip 保真。
	want := MeditationAnchors{LastUserInput: 100, LastTurnEnd: 200, LastMeditation: 300}
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
	_ = s1.Save(MeditationAnchors{LastUserInput: 1, LastTurnEnd: 2, LastMeditation: 3})
	// 重开（模拟重启）：Load 恢复三锚点（跨重启冥想门控连续性）。
	s2, _ := NewAnchorStore(path)
	got, _ := s2.Load()
	if got.LastUserInput != 1 || got.LastTurnEnd != 2 || got.LastMeditation != 3 {
		t.Fatalf("重启应恢复三锚点, got %+v", got)
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
	// 坏文件 Load 应 error（调用方 SetAnchorStore 保守用当前值，不阻断启动）。
	if _, err := s.Load(); err == nil {
		t.Fatal("坏文件 Load 应 error")
	}
}
