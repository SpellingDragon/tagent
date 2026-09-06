package tagent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== 巩固容量触发（4.2 design-report-closeout） ====================
//
// ConsolidationHintTracker 消费 engineBridge 的写入旁路计数（CapacityHookProvider）：
// 每分区的**边界事件**（external_input / agent_output，即任务回合的意图与产出）计数
// 超过 capacity_threshold 时，经 onHint 发一条 consolidation_hint 渗透消息——建议式，
// 执行权仍在 LLM + memory_consolidate 工具（D2 核心主张）。snooze 窗内不重复打扰
// （内存态；重启后重新积累——最多多提示一次，可接受）。

// ConsolidationHintTracker 是 per-agent 的巩固容量触发器（并发安全）。
type ConsolidationHintTracker struct {
	threshold int
	snooze    time.Duration

	mu       sync.Mutex
	counts   map[int]int
	recent   map[int][]int64 // per-partition 最近边界事件 key（环形，容量=threshold；4.3 候选清单）
	lastHint map[int]time.Time
	onHint   func(partitionID, count int)
	now      func() time.Time // 可注入（测试）
}

// NewConsolidationHintTracker 构造触发器。threshold<=0 返回 nil（关闭，零行为变化）。
// onHint 可后设（SetOnHint）——装配期 agent 尚未构造。
func NewConsolidationHintTracker(threshold int, snooze time.Duration) *ConsolidationHintTracker {
	if threshold <= 0 {
		return nil
	}
	return &ConsolidationHintTracker{
		threshold: threshold,
		snooze:    snooze,
		counts:    map[int]int{},
		recent:    map[int][]int64{},
		lastHint:  map[int]time.Time{},
		now:       time.Now,
	}
}

// SetOnHint 回填提示回调（装配期，NewTagentAgent 之后）。
func (t *ConsolidationHintTracker) SetOnHint(fn func(partitionID, count int)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onHint = fn
}

// Track 是写入旁路计数入口（engineBridge capacityHook 签名）。仅边界事件计数；
// 非阻塞、永不失败（旁路产物）。
func (t *ConsolidationHintTracker) Track(eventKey int64, partitionID int, eventType string) {
	if t == nil {
		return
	}
	// 边界事件 = 任务回合的意图与产出（与压缩段模型同源语义）。
	if eventType != tagentevent.TypeExternalInput && eventType != tagentevent.TypeAgentOutput {
		return
	}
	t.mu.Lock()
	t.counts[partitionID]++
	// 4.3：记录最近边界事件 key（环形，容量=threshold）供冥想 digest 候选清单。
	if eventKey > 0 {
		keys := append(t.recent[partitionID], eventKey)
		if len(keys) > t.threshold {
			keys = keys[len(keys)-t.threshold:]
		}
		t.recent[partitionID] = keys
	}
	count := t.counts[partitionID]
	if count < t.threshold {
		t.mu.Unlock()
		return
	}
	now := t.now()
	if last, ok := t.lastHint[partitionID]; ok && t.snooze > 0 && now.Sub(last) < t.snooze {
		t.mu.Unlock() // snooze 窗内：不打扰（计数保留，窗过期后的下一次边界事件再提示）
		return
	}
	// 触发：清零重新积累 + 记 hint 时刻（snooze 起点）。
	t.counts[partitionID] = 0
	t.lastHint[partitionID] = now
	fn := t.onHint
	t.mu.Unlock()
	if fn != nil {
		fn(partitionID, count)
	} else {
		log.Infof("[consolidation-hint] partition %d reached %d boundary events (no hint sink wired)", partitionID, count)
	}
}

// CandidatesText（4.3 design-report-closeout）渲染该分区的可巩固候选段（冥想 digest
// 附加）。无候选返回空串（digest 不变）。建议式：仅列 key 与计数，执行权在 LLM。
func (t *ConsolidationHintTracker) CandidatesText(partitionID int) string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	count := t.counts[partitionID]
	keys := t.recent[partitionID]
	if count == 0 || len(keys) == 0 {
		return ""
	}
	hexes := make([]string, 0, len(keys))
	for _, k := range keys {
		hexes = append(hexes, tagentevent.FormatEventKey(k))
	}
	return fmt.Sprintf("〔巩固候选〕本分区自上次巩固提示后累计 %d 个边界事件；最近 %d 个可用 recall/memory_consolidate 处理: %s",
		count, len(hexes), strings.Join(hexes, ", "))
}
