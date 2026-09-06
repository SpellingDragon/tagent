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
	NegFeedback   int    `json:"neg_feedback"`   // negative feedback 数（D1 design-report-closeout：任务成败/用户反馈负评）
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

// ActivationLog 记录 bundle 激活时刻（W4，§8.3）：ReleaseManager 在 canary SetActive 时写入，
// StoreEvidenceSource.Collect 读取作为证据窗口起点（bundleID→activationTs）——使后验评估只看该
// bundle 激活后的表现，而非固定回看窗（CanaryHold=0「激活即评估」时固定窗全是旧 bundle 数据，
// 对新 bundle 无判别力）。由 ReleaseManager 与 StoreEvidenceSource 共享同一实例（getter 接线）。
//
// Minor⑦（§8.9）重启回退语义（有意，非缺陷）：ActivationLog 是**内存态**（不持久化）——重启后
// 激活记录清零，Collect 对重启前激活的 bundle 回退固定回看窗（now-window）。这是可接受降级：
// 重启后 canary 通常已结算（active/rolledback），后验评估主要针对重启后新激活的 bundle（其激活
// 时刻会重新 Record）；持久化 ActivationLog 为后续增强（此处注释固化回退语义，非静默降级）。
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
	store         memory.MemoryStore
	partitionID   int
	window        time.Duration
	activationLog *ActivationLog // W4：可选，bundle 激活时刻表（Collect 窗口起点）
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
	// nil 守卫先行（Suggestion：typed-nil 装入 EvidenceSource 接口时方法仍可调，解引用前必判）。
	if s == nil || s.store == nil {
		return Evidence{BundleID: bundleID}, nil
	}
	ev := Evidence{BundleID: bundleID, WindowMs: s.window.Milliseconds()}
	// W4（§8.3）：窗口起点 = bundle 激活时刻（若已记录且落在回看窗内），而非固定 now-window。
	// 否则 CanaryHold=0（激活即评估）时固定回看窗全是旧 bundle 数据，judge 对新 bundle 无判别力，
	// "劣化即回滚"形同虚设。激活时刻早于回看窗时用 now-window 兜底（避免窗口无界扩大）。
	cutoff := time.Now().Add(-s.window).UnixMilli()
	if ts, ok := s.activationLog.Since(bundleID); ok && ts > cutoff {
		cutoff = ts
		ev.WindowMs = time.Now().UnixMilli() - ts
	}
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
		// D1-B（design-report-closeout）：bundle_id 精确 join——带章事件归属其真实
		// bundle（他 bundle 的事件不计入本窗口证据）；无章事件（历史/未启用自进化
		// 时期）回退时间窗归属。ActivationLog 时间窗从主判据降为缺章回退。
		if bid, ok := e.Metadata[event.MetaKeyBundleID]; ok && bid != bundleID {
			continue
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
		if e.EventType == event.TypeFeedback {
			// verdict 存于结构化 Content（BindFeedback 生成）；快速子串判定
			// negative，避免逐事件 JSON 反序列化。
			if strings.Contains(e.Content, `"verdict":"negative"`) {
				ev.NegFeedback++
			}
		}
	}
	return ev, nil
}

// GuardrailConfig 是指标闸阈值。
type GuardrailConfig struct {
	MaxDenialRate   float64 // canary 窗口治理拒绝率上限（默认 0.3）
	MaxCriticalRate float64 // critical 操作率上限（默认 0.2）
	MaxNegFbRate    float64 // negative feedback 率上限（默认 0.3，独立可配；禁用请显式设 >1 不可达值）
	MinSamples      int     // 最小样本数（不足不判，默认 5，防抖动错杀）
}

func (c GuardrailConfig) withDefaults() GuardrailConfig {
	if c.MaxDenialRate <= 0 {
		c.MaxDenialRate = 0.3
	}
	if c.MaxCriticalRate <= 0 {
		c.MaxCriticalRate = 0.2
	}
	if c.MaxNegFbRate <= 0 {
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
		return false, "" // 收集失败不误判 breach（保守不回滚）
	}
	if !ev.Sufficient(g.cfg.MinSamples) {
		return false, "" // 样本不足不判（防抖动错杀）
	}
	if ev.DenialRate() > g.cfg.MaxDenialRate {
		return true, fmt.Sprintf("canary 治理拒绝率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.DenialRate(), g.cfg.MaxDenialRate, ev.DenialCount, ev.TurnCount)
	}
	// D1（design-report-closeout §2.5）：负反馈率判据——feedback 事件（沿因果边 join 到
	// 本 bundle 的 parent）中 negative 占比超阈即回滚，与两率并列。
	if nf := float64(ev.NegFeedback) / float64(ev.TurnCount); nf > g.cfg.MaxNegFbRate {
		return true, fmt.Sprintf("canary 负反馈率 %.2f 超阈值 %.2f（%d/%d 事件）",
			nf, g.cfg.MaxNegFbRate, ev.NegFeedback, ev.TurnCount)
	}
	if ev.CriticalRate() > g.cfg.MaxCriticalRate {
		return true, fmt.Sprintf("canary critical 操作率 %.2f 超阈值 %.2f（%d/%d 事件）",
			ev.CriticalRate(), g.cfg.MaxCriticalRate, ev.CriticalCount, ev.TurnCount)
	}
	return false, ""
}

// 编译期确认 MetricGuardrail 满足 Guardrail 接口。
var _ Guardrail = (*MetricGuardrail)(nil)
