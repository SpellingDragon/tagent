// 契约: docs/wiki/memory/memory-architecture.md#curation
package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// ConsolidationHintTracker 是 per-agent 的巩固容量触发器（并发安全）。
// 消费 engineBridge 的写入旁路计数（CapacityHookProvider）：
// 每分区的**边界事件**（external_input / agent_output，即任务回合的意图与产出）计数
// 超过 capacity_threshold 时，经 onHint 发一条 consolidation_hint 渗透消息——建议式，
// 执行权仍在 LLM + memory_consolidate 工具。snooze 窗内不重复打扰
// （内存态；重启后重新积累——最多多提示一次，可接受）。
//
// 不变量（容量观察真源）：本 tracker 的 counts 是**建议式 delta**，仅供 LLM 提示，
// MUST NOT 驱动容量淘汰——淘汰执行权的唯一真源是 store 的绝对 per-partition
// eventCount（`recomputePartition` 由完整记录链得出，unknown 分区不淘汰，见
// memory/lifecycle.go::checkCapacity）。因此本 delta 重启归零、巩固后随提示复位（Track 触发
// onHint 即将 counts[pid]=0），与绝对真源分叉不构成淘汰误删风险（既有
// TestCapacityHint_TriggerAndSnooze 锁定提示即复位、非边界不计数；锁定淘汰读绝对）。
// repaired/already 重放也不经此处二次增量——engineBridge.ReplayEvent 对 Already 跳过
// capacityHook（见 engine_bridge_idempotency_test.go）。
type ConsolidationHintTracker struct {
	threshold int
	snooze    time.Duration

	mu     sync.Mutex
	counts map[int]int
	// recent 保存每分区最近的边界事件 key（环形，容量 threshold），供冥想 digest 取候选清单。
	recent   map[int][]int64
	lastHint map[int]time.Time
	onHint   func(partitionID, count int)

	// now 为时钟源，测试可注入。
	now func() time.Time
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
// Within the snooze window the count is kept, so the next boundary event after the
// window expires hints again. While no hint callback is set (the assembly
// window between construction and wiring) nothing is reset and no snooze is
// recorded: the count survives, and the first boundary event after wiring
// emits the delayed hint. On a hint, counts and recent reset together so the
// candidate list stays aligned with the push hint path.
func (t *ConsolidationHintTracker) Track(eventKey int64, partitionID int, eventType string) {
	if t == nil {
		return
	}
	if eventType != tagentevent.TypeExternalInput && eventType != tagentevent.TypeAgentOutput {
		return
	}
	t.mu.Lock()
	t.counts[partitionID]++
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
		t.mu.Unlock()
		return
	}
	hintFn := t.onHint
	if hintFn == nil {
		t.mu.Unlock()
		return
	}
	t.counts[partitionID] = 0
	t.recent[partitionID] = nil
	t.lastHint[partitionID] = now
	t.mu.Unlock()
	hintFn(partitionID, count)
}

// CandidatesText渲染该分区的可巩固候选段（冥想 digest
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
