package agent

// replay_restore.go — 压缩现场重放复原（tagent-compress-event-sourcing D3）：
// build_agent 接线层（可能跨包）所需的导出入口；unexported 字段访问收拢在本包。

import (
	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// RestoreCompressionSnapshot 将压缩快照回灌到投影与压缩器（重放状态机快照分支）：
// Replace(snap.RetainedRefs) + SetFullBoundary + UpdateThreshold + MarkMeditationKey×N。
// 依赖缺失（无投影/无压缩器）返回 false，调用方降级；成功返回 true。
func (ta *TagentAgent) RestoreCompressionSnapshot(snap *compress.CompressionSnapshot) bool {
	if ta == nil || snap == nil || ta.contextManager == nil ||
		ta.contextManager.contextCompressor == nil || ta.contextManager.projection == nil {
		return false
	}
	ta.contextManager.projection.Replace(snap.RetainedRefs)
	cc := ta.contextManager.contextCompressor
	cc.SetFullBoundary(snap.FullBoundary)
	cc.UpdateThreshold(snap.Threshold)
	for _, k := range snap.MeditationKeys {
		cc.MarkMeditationKey(k)
	}
	return true
}

// MarkMeditationEvent 重放侧冥想章派生（session.go 活路径语义的镜像：
// trigger_source=meditation 的 agent_output 事件 → MarkMeditationKey；
// 与快照回灌构成并集双保险——死亡落在 Mark 后/压缩前由事件章补齐）。
func (ta *TagentAgent) MarkMeditationEvent(key int64) {
	if ta == nil || key == 0 || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return
	}
	ta.contextManager.contextCompressor.MarkMeditationKey(key)
}


// ReplayProjectionHandler 返回重放双写回调（D3 重放状态机的可测化收口）：
// build_agent 接线处 SetReplayProjection(ReplayProjectionHandler(ta))。三分支：
// 快照事件 → 严格解析后 Replace+三态回灌（多次快照后者覆盖前者，天然收敛到最后快照）；
// 冥想 agent_output → 派生 Mark 后照常 Append（与快照回灌构成并集双保险）；
// 普通事件 → 现状 Append。快照解析失败（L2 降级）ERROR 留痕并跳过该事件。
func ReplayProjectionHandler(ta *TagentAgent) func(memory.FullEvent) {
	return func(ev memory.FullEvent) {
		if raw, ok := ev.Metadata[compress.SnapshotMetaKey]; ok {
			snap, err := compress.ParseSnapshot(raw)
			if err != nil {
				log.Errorf("[replay-projection] compress snapshot invalid key=%d: %v", ev.EventKey, err)
				return
			}
			if ta.RestoreCompressionSnapshot(snap) {
				log.Infof("[replay-projection] snapshot restored key=%d boundary=%d refs=%d",
					ev.EventKey, snap.FullBoundary, len(snap.RetainedRefs))
			} else {
				log.Warnf("[replay-projection] snapshot restore unavailable key=%d", ev.EventKey)
			}
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
