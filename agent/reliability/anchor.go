package reliability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ==================== AnchorStore（T-G · 冥想门控锚点持久化）====================
//
// MeditationManager 的三个门控锚点（novelty/idle/last-meditation）是内存 atomic，重启即丢失
// → 重启后冥想门控「失忆」：可能立即误触发冥想（idle 锚点归零 → 认为已空闲很久），或错误
// 计算 novelty（lastMeditation 归零 → 认为从未冥想）。AnchorStore 把它们持久化到单 JSON 文件
// （原子写），启动时 Load 恢复，锚点更新时 Save——跨重启保留冥想门控连续性（常驻可靠性）。

// MeditationAnchors 是冥想门控锚点的持久化快照（Unix ms）。
type MeditationAnchors struct {
	LastUserInput  int64 `json:"last_user_input"`  // novelty gate 锚点
	LastTurnEnd    int64 `json:"last_turn_end"`    // idle gate 锚点
	LastMeditation int64 `json:"last_meditation"`  // 最近有效冥想时刻
}

// AnchorStore 持久化冥想锚点（单 JSON 文件，tmp+rename 原子写）。并发安全。
type AnchorStore struct {
	path string
	mu   sync.Mutex
}

// NewAnchorStore 构建锚点存储。path 为空返回 error（持久化必须有路径）。
func NewAnchorStore(path string) (*AnchorStore, error) {
	if path == "" {
		return nil, fmt.Errorf("reliability: AnchorStore requires non-empty path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("reliability: create anchor dir: %w", err)
		}
	}
	return &AnchorStore{path: path}, nil
}

// Load 读取持久化锚点。文件不存在返回零值 + nil（首次启动，无历史锚点，冥想门控从头开始）。
// 解析失败返回 error（调用方保守用零值，不因坏文件阻断启动）。
func (s *AnchorStore) Load() (MeditationAnchors, error) {
	var a MeditationAnchors
	if s == nil {
		return a, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return a, nil // 首次启动
		}
		return a, err
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return MeditationAnchors{}, fmt.Errorf("reliability: parse anchors %s: %w", s.path, err)
	}
	return a, nil
}

// Save 原子持久化锚点（tmp + rename，防半写被 Load 读到）。
func (s *AnchorStore) Save(a MeditationAnchors) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("reliability: anchor write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("reliability: anchor rename: %w", err)
	}
	return nil
}

// Path 返回锚点文件路径（诊断）。
func (s *AnchorStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}
