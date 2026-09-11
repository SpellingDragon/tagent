package compress

// snapshot.go — 压缩事件溯源 payload（openspec: examples/wechat-bot/openspec/changes/
// tagent-compress-event-sourcing，design D4 schema v1）。
//
// 职责：把压缩器三态（fullBoundary / threshold / meditationKeys）与压缩区间账目
// 序列化为 context_compress 事件的 Metadata[SnapshotMetaKey]；重放侧经 ParseSnapshot
// 严格校验后回灌（Replace + SetFullBoundary + UpdateThreshold + MarkMeditationKey），
// 使重启恢复从「重推导碰运气收敛」升级为「回放精确还原」。
//
// D4 校验分级：
//   - schema_version 缺失/不识别                      → INVALID（L2 降级：不 Replace 不回灌）
//   - full_boundary/threshold/retained_refs 缺失或错  → INVALID（数值域判定：boundary<=0、
//     threshold∉(0,100)、retained_refs=null 均视为缺失——真压缩后 boundary 恒为正键）
//   - meditation_keys/compressed_keys/card_text 缺失  → 容忍为空（仅保真度降级，非正确性必需）

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/memory"
)

// SnapshotMetaKey 是 context_compress 事件 Metadata 中承载快照 JSON 的键。
const SnapshotMetaKey = "compress_snapshot"

// SnapshotSchemaV1 是当前快照 schema 版本（design D4；不识别的版本一律 L2 降级）。
const SnapshotSchemaV1 = 1

// CompressionSnapshot 是压缩事件的 payload：压缩器三态快照 + 压缩区间账目。
type CompressionSnapshot struct {
	SchemaVersion  int                     `json:"schema_version"`
	FullBoundary   int64                   `json:"full_boundary"`
	Threshold      float64                 `json:"threshold"`
	RetainedRefs   []memory.EventReference `json:"retained_refs"`
	MeditationKeys []int64                 `json:"meditation_keys,omitempty"`
	CompressedKeys []int64                 `json:"compressed_keys,omitempty"`
	CardText       string                  `json:"card_text,omitempty"`
	CreatedAt      int64                   `json:"created_at"` // UnixMilli，审计用
}

// Threshold 返回当前生效压缩阈值（原子读；从未 UpdateThreshold 时为 DefaultCompressThreshold）。
func (cc *ContextCompressor) Threshold() float64 { return cc.currentThreshold() }

// MeditationKeysSnapshot 返回冥想保护键的排序副本（锁内快照，调用方可安全持有/遍历）。
func (cc *ContextCompressor) MeditationKeysSnapshot() []int64 {
	cc.meditationMu.Lock()
	defer cc.meditationMu.Unlock()
	out := make([]int64, 0, len(cc.meditationKeys))
	for k := range cc.meditationKeys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// MarshalSnapshot 校验并序列化快照为 Metadata 值（JSON 字符串）。
func MarshalSnapshot(s *CompressionSnapshot) (string, error) {
	if s == nil {
		return "", fmt.Errorf("compress snapshot: nil")
	}
	if s.SchemaVersion != SnapshotSchemaV1 {
		return "", fmt.Errorf("compress snapshot: unsupported schema_version %d", s.SchemaVersion)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("compress snapshot: marshal: %w", err)
	}
	return string(b), nil
}

// ParseSnapshot 严格解析 Metadata 值（校验分级见文件头注释）。
func ParseSnapshot(raw string) (*CompressionSnapshot, error) {
	if raw == "" {
		return nil, fmt.Errorf("compress snapshot: empty payload")
	}
	var s CompressionSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("compress snapshot: unmarshal: %w", err)
	}
	if s.SchemaVersion != SnapshotSchemaV1 {
		return nil, fmt.Errorf("compress snapshot: unsupported schema_version %d", s.SchemaVersion)
	}
	if s.FullBoundary <= 0 {
		return nil, fmt.Errorf("compress snapshot: invalid full_boundary %d", s.FullBoundary)
	}
	if s.Threshold <= 0 || s.Threshold >= 100 {
		return nil, fmt.Errorf("compress snapshot: invalid threshold %v", s.Threshold)
	}
	if s.RetainedRefs == nil {
		return nil, fmt.Errorf("compress snapshot: missing retained_refs")
	}
	return &s, nil
}
