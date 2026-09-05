package evolution

import (
	"context"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// ==================== 后验评估证据与指标闸（T-EVO · 指令4「后验评估」数据基础）====================
//
// 快道发布「先生效、后验评估、劣化即回滚」需要 canary 期间的**表现证据**。本文件提供：
//   - Evidence：canary 窗口的表现度量（治理拒绝率、critical 频率、事件量）；
//   - StoreEvidenceSource：从 MemoryStore 读窗口事件算证据（治理记录是主要「行为变差」信号）；
//   - MetricGuardrail：确定性指标闸（实现 release.go 的 Guardrail）——超阈值即 breach 触发快回滚。
//
// 保守原则：样本不足或收集失败 → 不判劣化（不误回滚，避免抖动错杀）。evolution 不 import
// governance（分层独立，与 DiffLaneRouter 一致）；治理 subtype 值 "denial"/"approval" 是稳定契约。

// governance 事件 subtype 值的权威源在 event 包（event.SubtypeDenial/SubtypeApproval）——
// evolution 直接引用 event 常量（C4：消除此前复制 "denial"/"approval" 字面量的静默漂移风险，
// 漂移会使 DenialCount 归零、MetricGuardrail 永不 breach、快道确定性回滚防线失效）。

// Evidence 是 canary 期间的表现证据（后验评估/Guardrail 的输入）。
type Evidence struct {
	BundleID      string `json:"bundle_id"`
	TurnCount     int    `json:"turn_count"`     // 窗口内事件数（近似活动量）
	DenialCount   int    `json:"denial_count"`   // 治理拒绝数（行为变差信号：agent 频试危险操作）
	CriticalCount int    `json:"critical_count"` // critical 挂起数
	WindowMs      int64  `json:"window_ms"`
}

// DenialRate 返回治理拒绝率（DenialCount / TurnCount）。无活动返回 0。
func (e Evidence) DenialRate() float64 {
	if e.TurnCount == 0 {
		return 0
	}
	return float64(e.DenialCount) / float64(e.TurnCount)
}

// CriticalRate 返回 critical 操作率。
func (e Evidence) CriticalRate() float64 {
	if e.TurnCount == 0 {
		return 0
	}
	return float64(e.CriticalCount) / float64(e.TurnCount)
}

// Sufficient 报告样本是否足以判定（不足则保守不判劣化）。
func (e Evidence) Sufficient(minSamples int) bool { return e.TurnCount >= minSamples }

// EvidenceSource 收集 canary 证据。
type EvidenceSource interface {
	Collect(ctx context.Context, bundleID string) (Evidence, error)
}

// StoreEvidenceSource 从 MemoryStore 读最近 window 的事件算证据。canary hold 期间调用即
// 近似该 bundle 激活后的表现（治理记录 + 事件量是主要信号）。
type StoreEvidenceSource struct {
	store       memory.MemoryStore
	partitionID int
	window      time.Duration
}

// NewStoreEvidenceSource 构建证据源。window<=0 取默认 10m（canary 观察窗）。
func NewStoreEvidenceSource(store memory.MemoryStore, partitionID int, window time.Duration) *StoreEvidenceSource {
	if window <= 0 {
		window = 10 * time.Minute
	}
	return &StoreEvidenceSource{store: store, partitionID: partitionID, window: window}
}

// Collect 读窗口事件算证据。store 为 nil 或查询失败返回空证据 + err（调用方保守不判劣化）。
func (s *StoreEvidenceSource) Collect(ctx context.Context, bundleID string) (Evidence, error) {
	// nil 守卫先行（Suggestion：typed-nil 装入 EvidenceSource 接口时方法仍可调，解引用前必判）。
	if s == nil || s.store == nil {
		return Evidence{BundleID: bundleID}, nil
	}
	ev := Evidence{BundleID: bundleID, WindowMs: s.window.Milliseconds()}
	cutoff := time.Now().Add(-s.window).UnixMilli()
	// 服务端窗口过滤（StartTime）+ timestamp_desc：常驻 agent 分区事件量 >> Limit，asc 会返回
	// 最旧的 Limit 条（全部早于 cutoff 被客户端滤掉 → TurnCount=0 → 后验评估永久静默失效，Major）。
	// desc 截断时牺牲最旧、保住观察窗。客户端 cutoff 判断保留作兜底（段剪枝用 nominal bound）。
	refs, err := s.store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{s.partitionID},
		StartTime:    cutoff,
		Limit:        10000,
		OrderBy:      "timestamp_desc",
	})
	if err != nil {
		return ev, err
	}
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey)
	}
	events, err := s.store.GetEvents(keys)
	if err != nil {
		return ev, err
	}
	for _, e := range events {
		if e.Timestamp < cutoff {
			continue // 客户端时间窗口过滤（canary 观察窗）
		}
		ev.TurnCount++
		if e.EventType == event.TypeGovernance {
			switch e.Metadata[event.MetaKeySubtype] {
			case event.SubtypeDenial:
				ev.DenialCount++
			case event.SubtypeApproval:
				ev.CriticalCount++
			}
		}
	}
	return ev, nil
}

// GuardrailConfig 是指标闸阈值。
type GuardrailConfig struct {
	MaxDenialRate   float64 // canary 窗口治理拒绝率上限（默认 0.3）
	MaxCriticalRate float64 // critical 操作率上限（默认 0.2）
	MinSamples      int     // 最小样本数（不足不判，默认 5，防抖动错杀）
}

func (c GuardrailConfig) withDefaults() GuardrailConfig {
	if c.MaxDenialRate <= 0 {
		c.MaxDenialRate = 0.3
	}
	if c.MaxCriticalRate <= 0 {
		c.MaxCriticalRate = 0.2
	}
	if c.MinSamples <= 0 {
		c.MinSamples = 5
	}
	return c
}

// MetricGuardrail 是确定性指标闸（实现 release.go 的 Guardrail 接口）：canary 表现超阈值
// → breach → 快回滚。样本不足或收集失败 → 不 breach（保守，不误回滚）。无状态、并发安全。
type MetricGuardrail struct {
	src EvidenceSource
	cfg GuardrailConfig
}

// NewMetricGuardrail 构建指标闸。
func NewMetricGuardrail(src EvidenceSource, cfg GuardrailConfig) *MetricGuardrail {
	return &MetricGuardrail{src: src, cfg: cfg.withDefaults()}
}

// Breach 实现 Guardrail：检查 canary 表现是否违约（确定性阈值，快、廉价，先于 LLM-judge）。
func (g *MetricGuardrail) Breach(bundleID string) (bool, string) {
	if g == nil || g.src == nil {
		return false, ""
	}
	ev, err := g.src.Collect(context.Background(), bundleID)
	if err != nil {
		return false, "" // 收集失败不误判 breach（保守不回滚）
	}
	if !ev.Sufficient(g.cfg.MinSamples) {
		return false, "" // 样本不足不判（防抖动错杀）
	}
	if ev.DenialRate() > g.cfg.MaxDenialRate {
		return true, fmt.Sprintf("canary 治理拒绝率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.DenialRate(), g.cfg.MaxDenialRate, ev.DenialCount, ev.TurnCount)
	}
	if ev.CriticalRate() > g.cfg.MaxCriticalRate {
		return true, fmt.Sprintf("canary critical 操作率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.CriticalRate(), g.cfg.MaxCriticalRate, ev.CriticalCount, ev.TurnCount)
	}
	return false, ""
}

// 编译期确认 MetricGuardrail 满足 Guardrail 接口。
var _ Guardrail = (*MetricGuardrail)(nil)
