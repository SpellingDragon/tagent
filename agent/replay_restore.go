package agent

// replay_restore.go — spill 重放双写回调（event-sourced-projection D2）：
// append-only（幂等去重由投影侧保证）；build_agent 接线处
// SetReplayProjection(ReplayProjectionHandler(ta))。
// 投影的完整重建走启动期 RebuildProjectionFromWAL（见 projection_rebuild.go）——
// 本重放路径只做「同点补投影」，绝不在活投影上 Replace（Replace-over-live
// 已随 dev 快照补丁移除，spec: spill 恢复 SHALL 保持 append-only）。

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

// ReplayProjectionHandler 返回重放双写回调。两分支：普通事件 → Append（冥想
// agent_output 先派生 Mark）；非投影记录 → 跳过。§5.5：排除判定收敛到 event 包
// 单一谓词 IsNonProjectionRecord，与正常提交（persistBusEvent）、冷启动重建共用
// ——旧版本处理器自带类型枚举，漏排 inbox_receipt（spill 窗口回补会把内部回执
// 注进投影，破坏「投影=事实链可回放折叠」不变量）；历史快照/旧标记兼容分支
// 已删（运行时只认当前格式，旧数据走 §3.7 受管重置）。
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
