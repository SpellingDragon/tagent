package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
)

// TestMeditationManager_AnchorStoreRestore 验证 SetAnchorStore 从持久化恢复三锚点——跨重启
// 冥想门控连续性（重启后不立即误触发冥想、正确计算 novelty）。
func TestMeditationManager_AnchorStoreRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	// 预置锚点文件（模拟上次运行留下的门控状态）。
	pre, err := reliability.NewAnchorStore(path)
	if err != nil {
		t.Fatalf("NewAnchorStore: %v", err)
	}
	if err := pre.Save(reliability.MeditationAnchors{LastUserInput: 111, LastTurnEnd: 222, LastMeditation: 333}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 新 manager（模拟重启）+ SetAnchorStore → Load 恢复三锚点。
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	as, _ := reliability.NewAnchorStore(path)
	m.SetAnchorStore(as)

	if m.lastUserInput.Load() != 111 {
		t.Fatalf("应恢复 lastUserInput=111, got %d", m.lastUserInput.Load())
	}
	if m.lastTurnEnd.Load() != 222 {
		t.Fatalf("应恢复 lastTurnEnd=222, got %d", m.lastTurnEnd.Load())
	}
	if m.lastMeditation.Load() != 333 {
		t.Fatalf("应恢复 lastMeditation=333, got %d", m.lastMeditation.Load())
	}
}

// TestMeditationManager_AnchorStorePersist 验证锚点更新（UpdateLastTurnEnd）持久化落盘——
// 重启后可恢复（persistAnchors 在锚点更新时触发）。
func TestMeditationManager_AnchorStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchors.json")
	as, _ := reliability.NewAnchorStore(path)
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	m.SetAnchorStore(as)

	m.UpdateLastTurnEnd(time.UnixMilli(999))
	m.UpdateLastUserInput(time.UnixMilli(888))

	reloaded, err := as.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.LastTurnEnd != 999 {
		t.Fatalf("UpdateLastTurnEnd 应 persist, got %d", reloaded.LastTurnEnd)
	}
	if reloaded.LastUserInput != 888 {
		t.Fatalf("UpdateLastUserInput 应 persist, got %d", reloaded.LastUserInput)
	}
}

// TestMeditationManager_NoAnchorStoreInMemory 验证向后兼容：未注入 AnchorStore 时锚点纯内存
// （现状），Update 不 panic、不落盘。
func TestMeditationManager_NoAnchorStoreInMemory(t *testing.T) {
	m := NewMeditationManager(MeditationConfig{Enabled: true, Interval: time.Hour, MinGap: time.Minute}, &mockMessageInjector{})
	// 未 SetAnchorStore → anchorStore nil。
	m.UpdateLastTurnEnd(time.UnixMilli(500)) // persistAnchors no-op，不 panic
	if m.lastTurnEnd.Load() != 500 {
		t.Fatalf("内存锚点应更新, got %d", m.lastTurnEnd.Load())
	}
}
