package reliability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MeditationAnchors 是冥想门控锚点的持久化快照（Unix ms）。
//
// 锚点跨重启持久；缺失即视为 0。
type MeditationAnchors struct {
	LastUserInput  int64 `json:"last_user_input"`
	LastTurnEnd    int64 `json:"last_turn_end"`
	LastMeditation int64 `json:"last_meditation"`
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
			return a, nil
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
