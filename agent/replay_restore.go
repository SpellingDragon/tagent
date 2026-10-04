// 契约: docs/wiki/memory/memory-architecture.md#bridge-write-replay
package agent

import (
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// MarkMeditationEvent 重放侧冥想章派生（session.go 活路径语义的镜像：
// trigger_source=meditation 的 agent_output 事件 → MarkMeditationKey）。
func (ta *TagentAgent) MarkMeditationEvent(key int64) {
	if ta == nil || key == 0 || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return
	}
	ta.contextManager.contextCompressor.MarkMeditationKey(key)
}

// ReplayProjectionHandler 返回重放双写回调。两分支：普通事件 → Append（冥想的
// agent_output 先派生 Mark）；非投影记录 → 跳过。排除判定的唯一来源是 event 包的
// 谓词 IsNonProjectionRecord，与正常提交（persistBusEvent）、冷启动重建共用同一处——
// 若在此自带类型枚举，就会漏排 inbox_receipt，把内部回执注进投影，破坏
// 「投影＝事实链可回放折叠」这条不变量。本路径只做同点补投影，绝不在活投影上整表 Replace。
func ReplayProjectionHandler(ta *TagentAgent) func(memory.FullEvent) {
	return func(ev memory.FullEvent) {
		if tagentevent.IsNonProjectionRecord(ev.EventType, ev.Metadata) {
			return
		}
		if ev.EventType == tagentevent.TypeAgentOutput &&
			ev.Metadata[tagentevent.MetaKeyTriggerSource] == "meditation" {
			ta.MarkMeditationEvent(ev.EventKey)
		}
		ta.AppendProjectionRef(memory.EventReference{
			EventKey: ev.EventKey, PartitionID: ev.PartitionID,
			EventType: ev.EventType, EventSummary: ev.EventSummary,
			Timestamp: ev.Timestamp, Role: string(tagentevent.EventTypeRole(ev.EventType)),
		})
	}
}
