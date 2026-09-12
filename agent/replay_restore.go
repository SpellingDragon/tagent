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

// legacySnapshotMetaKey is the metadata marker of the REMOVED dev snapshot
// patch (persistSnapshotEvent); kept only to skip such an event should one
// surface in a spill window — it is a fact-chain record, not a projection ref.
const legacySnapshotMetaKey = "compress_snapshot"

// MarkMeditationEvent 重放侧冥想章派生（session.go 活路径语义的镜像：
// trigger_source=meditation 的 agent_output 事件 → MarkMeditationKey）。
func (ta *TagentAgent) MarkMeditationEvent(key int64) {
	if ta == nil || key == 0 || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return
	}
	ta.contextManager.contextCompressor.MarkMeditationKey(key)
}

// ReplayProjectionHandler 返回重放双写回调。两分支：冥想 agent_output →
// 派生 Mark 后照常 Append；普通事件 → Append。compaction 事件（与 legacy 快照
// 事件）跳过——它们是事实链记录而非投影 ref，综述 ref 由 RebuildProjectionFromWAL
// 从 compaction 载荷重建（spec: compaction 事件本身 SHALL NOT 作为独立正 key ref
// 进投影；否则折叠被双重表示）。
func ReplayProjectionHandler(ta *TagentAgent) func(memory.FullEvent) {
	return func(ev memory.FullEvent) {
		if ev.EventType == tagentevent.TypeContextCompressSummary ||
			ev.Metadata[legacySnapshotMetaKey] != "" {
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
