package evolution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// EvalResult 是后验评估结果。
type EvalResult struct {
	Score  float64
	Pass   bool
	Reason string
}

// Evaluator 是后验评估器：对一个改进窗口（key=commit sha）的实际表现打分。
// nil = 跳过 judge（仅 guardrail）。
type Evaluator interface {
	Evaluate(ctx context.Context, key string) (EvalResult, error)
}

// Guardrail 是确定性指标闸：breach 即劣化信号。nil = 不监控。
// （git-native 后输出为建议式 evaluation 事件——P4 框架不动手。）
type Guardrail interface {
	Breach(key string) (breached bool, reason string)
}

// Evidence 是 canary 期间的表现证据（后验评估/Guardrail 的输入）。
type Evidence struct {
	BundleID string `json:"bundle_id"`
	// TurnCount 窗口内真实 turn 数（用户输入型事件；C1 口径修正）
	TurnCount int `json:"turn_count"`
	// DenialCount 治理拒绝数（行为变差信号：agent 频试危险操作）
	DenialCount int `json:"denial_count"`
	// CriticalCount critical 挂起数
	CriticalCount int `json:"critical_count"`
	// NegFeedback negative feedback 数（D1：任务成败/用户反馈负评）
	NegFeedback int   `json:"neg_feedback"`
	WindowMs    int64 `json:"window_ms"`
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

// ActivationLog 记录 bundle（或改进 sha）的激活时刻，供证据采集当作窗口起点：
// 只看激活后的表现，而非固定回看窗。它是内存态——重启后清零，对重启前激活者回退
// 固定回看窗（有意降级，理由见文档）。由发布管理器与证据源共享同一实例。
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
type ActivationLog struct {
	mu sync.Mutex
	ts map[string]int64
}

// NewActivationLog 构建激活时刻表。
func NewActivationLog() *ActivationLog { return &ActivationLog{ts: make(map[string]int64)} }

// Record 记录 bundle 激活时刻（UnixMilli）。nil-safe。
func (a *ActivationLog) Record(bundleID string, ts int64) {
	if a == nil || bundleID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ts == nil {
		a.ts = make(map[string]int64)
	}
	a.ts[bundleID] = ts
}

// Since 返回 bundle 激活时刻（UnixMilli）；未记录返回 (0,false)。nil-safe。
func (a *ActivationLog) Since(bundleID string) (int64, bool) {
	if a == nil {
		return 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ts, ok := a.ts[bundleID]
	return ts, ok
}

// StoreEvidenceSource 从 MemoryStore 读最近 window 的事件算证据。canary hold 期间调用即
// 近似该 bundle 激活后的表现（治理记录 + 事件量是主要信号）。
type StoreEvidenceSource struct {
	store       memory.MemoryStore
	partitionID int
	window      time.Duration
	// activationLog W4：可选，bundle 激活时刻表（Collect 窗口起点）
	activationLog *ActivationLog
}

// NewStoreEvidenceSource 构建证据源。window<=0 取默认 10m（canary 观察窗）。
func NewStoreEvidenceSource(store memory.MemoryStore, partitionID int, window time.Duration) *StoreEvidenceSource {
	if window <= 0 {
		window = 10 * time.Minute
	}
	return &StoreEvidenceSource{store: store, partitionID: partitionID, window: window}
}

// SetActivationLog 注入激活时刻表（W4）：Collect 以 bundle 激活时刻为窗口起点，而非固定回看。
func (s *StoreEvidenceSource) SetActivationLog(log *ActivationLog) {
	if s != nil {
		s.activationLog = log
	}
}

// Collect 读窗口事件算证据。store 为 nil 或查询失败返回空证据 + err（调用方保守不判劣化）。
func (s *StoreEvidenceSource) Collect(ctx context.Context, bundleID string) (Evidence, error) {
	if s == nil || s.store == nil {
		return Evidence{BundleID: bundleID}, nil
	}
	ev := Evidence{BundleID: bundleID, WindowMs: s.window.Milliseconds()}
	cutoff := time.Now().Add(-s.window).UnixMilli()
	if ts, ok := s.activationLog.Since(bundleID); ok && ts > cutoff {
		cutoff = ts
		ev.WindowMs = time.Now().UnixMilli() - ts
	}
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
			continue
		}
		if bid, ok := e.Metadata[event.MetaKeyBundleID]; ok && bid != bundleID {
			continue
		}
		if e.EventType == event.TypeExternalInput {
			ev.TurnCount++
		}
		if e.EventType == event.TypeGovernance {
			switch e.Metadata[event.MetaKeySubtype] {
			case event.SubtypeDenial:
				ev.DenialCount++
			case event.SubtypeApproval:
				ev.CriticalCount++
			}
		}
		if e.EventType == event.TypeFeedback {
			if e.Metadata["verdict"] == "negative" ||
				(e.Metadata["verdict"] == "" && strings.Contains(e.Content, `"verdict":"negative"`)) {
				ev.NegFeedback++
			}
		}
	}
	return ev, nil
}

// GuardrailConfig 是指标闸阈值。
type GuardrailConfig struct {
	// MaxDenialRate canary 窗口治理拒绝率上限（默认 0.3）
	MaxDenialRate float64
	// MaxCriticalRate critical 操作率上限（默认 0.2）
	MaxCriticalRate float64
	// MaxNegFbRate 率上限：0 取默认 0.3，负值表示显式禁用该判据（禁用无需再填一个不可达的 >1 值）。
	MaxNegFbRate float64
	// MinSamples 最小样本数（不足不判，默认 5，防抖动错杀）
	MinSamples int
}

func (c GuardrailConfig) withDefaults() GuardrailConfig {
	if c.MaxDenialRate <= 0 {
		c.MaxDenialRate = 0.3
	}
	if c.MaxCriticalRate <= 0 {
		c.MaxCriticalRate = 0.2
	}
	if c.MaxNegFbRate == 0 {
		c.MaxNegFbRate = 0.3
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
		return false, ""
	}
	if !ev.Sufficient(g.cfg.MinSamples) {
		return false, ""
	}
	if ev.DenialRate() > g.cfg.MaxDenialRate {
		return true, fmt.Sprintf("canary 治理拒绝率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.DenialRate(), g.cfg.MaxDenialRate, ev.DenialCount, ev.TurnCount)
	}
	if g.cfg.MaxNegFbRate > 0 {
		if nf := float64(ev.NegFeedback) / float64(ev.TurnCount); nf > g.cfg.MaxNegFbRate {
			return true, fmt.Sprintf("canary 负反馈率 %.2f 超阈值 %.2f（%d/%d 事件）",
				nf, g.cfg.MaxNegFbRate, ev.NegFeedback, ev.TurnCount)
		}
	}
	if ev.CriticalRate() > g.cfg.MaxCriticalRate {
		return true, fmt.Sprintf("canary critical 操作率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.CriticalRate(), g.cfg.MaxCriticalRate, ev.CriticalCount, ev.TurnCount)
	}
	return false, ""
}

// _ 编译期确认 MetricGuardrail 满足 Guardrail 接口。
var _ Guardrail = (*MetricGuardrail)(nil)
