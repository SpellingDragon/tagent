package agent

import (
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// projection_rebuild.go — 冷启动投影重建（event-sourced-projection D2/D3）：
// 投影是事实链的纯回放（运行期 Add 增量 / 本函数全量，同一 fold 的两个入口）。
// 「加载 snapshot + 回放尾部」：snapshot = 最新代际标记的 compaction 事件；
// tail = 其后事件（MinEventKey 写序过滤，分页取全，按 EventKey（写入序）Add）。

// tailPageSize bounds each QueryEvents page during tail replay. The default
// Limit=100 with asc ordering silently truncates the NEWEST end
// (fresh-eyes 🟡3) — pagination must run until a short page.
const tailPageSize = 500

// RebuildProjectionFromWAL rebuilds the projection from the fact chain at
// cold start (build_agent wiring; runs BEFORE spill replay is armed).
// Startup-only, once, into an EMPTY projection. No marker-tagged compaction
// event in the chain → D1 fallback full replay (rebuildProjectionFallback;
// 2026-09-13 spec change: WAL is the durable record — context must be
// recoverable even without compaction; supersedes the old no-op).
func (ta *TagentAgent) RebuildProjectionFromWAL() {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.rebuildProjectionFromWAL()
}

func (cm *ContextManager) rebuildProjectionFromWAL() {
	rebuildStart := time.Now()
	if cm == nil || cm.memStore == nil || cm.projection == nil || cm.contextCompressor == nil {
		return
	}
	if cm.projection.Len() > 0 {
		// Non-empty projection at rebuild time (e.g. spill replay already
		// appended): WARN, never silently skip — and never Replace a live
		// projection (Replace-over-live is the dev-patch bug we removed).
		log.Warnf("[rebuild-projection] projection non-empty (len=%d), skipping rebuild", cm.projection.Len())
		return
	}

	snapKey := cm.latestCompactionKey()
	if snapKey == 0 {
		// D1 fallback (2026-09-13 spec change): WAL is the durable record —
		// recover context even without any compaction anchor; supersedes no-op.
		cm.rebuildProjectionFallback()
		return
	}
	snapEv, err := cm.memStore.GetEvent(snapKey)
	if err != nil || snapEv == nil {
		log.Errorf("[rebuild-projection] compaction event %d unreadable: %v", snapKey, err)
		return
	}
	payload, err := compress.UnmarshalPayload(snapEv.Metadata[compress.CompactionPayloadMetaKey])
	if err != nil {
		log.Errorf("[rebuild-projection] compaction payload invalid key=%d: %v", snapKey, err)
		return
	}

	// 1) Snapshot restore: synthetic refs verbatim (summary + tool_chain);
	//    positive keys resolved via GetEvent (immutable store → byte-exact
	//    ref). Missing/tombstoned keys degrade to a WARN + skip, never block.
	ordered, posKeys, posIdx := payload.RestoreRefs()
	lostKeys := 0
	for i, k := range posKeys {
		ev, err := cm.memStore.GetEvent(k)
		if err != nil || ev == nil {
			// S3 fix (systemic-α): each lost key is a fact-chain hole —
			// Error (not Warn) + count, so trajectory-comparison tools and
			// diagnostics can attribute byte-mismatch to known lost slots.
			lostKeys++
			log.Errorf("[rebuild-projection] retained key %d missing/tombstoned — SLOT LOST (count=%d): %v", k, lostKeys, err)
			ordered[posIdx[i]] = memory.EventReference{}
			continue
		}
		ordered[posIdx[i]] = memory.EventReference{
			EventKey: ev.EventKey, PartitionID: ev.PartitionID,
			EventType: ev.EventType, EventSummary: ev.EventSummary,
			Timestamp: ev.Timestamp, Role: string(tagentevent.EventTypeRole(ev.EventType)),
		}
		// Meditation reseed (snapshot side, event-sourced-projection D3 /
		// fresh-eyes C): trigger_source now persists in agent_output Metadata
		// (buildTurnAttribution) — unreadable before that stamp existed.
		if ev.EventType == tagentevent.TypeAgentOutput &&
			ev.Metadata[tagentevent.MetaKeyTriggerSource] == "meditation" {
			cm.contextCompressor.MarkMeditationKey(ev.EventKey)
		}
	}
	final := make([]memory.EventReference, 0, len(ordered))
	for _, ref := range ordered {
		if ref.EventKey != 0 { // dropped placeholders (invalid zero key)
			final = append(final, ref)
		}
	}
	cm.projection.Replace(final)

	// Seed fullBoundary from the payload — the exact runtime state at fold
	// time (under-budget rounds never touch it), NOT a recompute over
	// snapshot+tail (that would shift the recent window and change renders).
	cm.contextCompressor.SetFullBoundary(payload.FullBoundary)

	// 2) Tail replay: events written AFTER the compaction event. Three bans
	//    (fresh-eyes A/B): no StartTime approximation (dual-time divergence),
	//    no Timestamp-order replay (runtime append order ≡ EventKey write
	//    order; stragglers invert), no silent truncation (paginated).
	tail := cm.fetchTailEvents(snapKey)
	for _, ev := range tail {
		// Compaction/legacy-snapshot events are fact-chain records, never
		// projection refs (double-representation guard, same as the replay
		// handler). R2 task records likewise: task_spawned is registry data
		// (board renders live from the registry); task_inline_record settles
		// returned in-turn as tool results (appending would double-render).
		if skipProjectionEvent(ev) {
			continue
		}
		cm.appendProjectionRef(ev)
	}

	log.Infof("[rebuild-projection] rebuilt from compaction key=%d: snapshot refs=%d tail=%d boundary=%d lostKeys=%d took=%v",
		snapKey, len(final), len(tail), payload.FullBoundary, lostKeys, time.Since(rebuildStart))
	if lostKeys > 0 {
		log.Errorf("[rebuild-projection] LOST %d retained key(s) — projection has holes; trajectory prefix-match will show gaps at these slots", lostKeys)
	}
}

// fallbackCap bounds the D1 fallback full replay: a chain without any
// compaction anchor may be arbitrarily long, so keep the NEWEST fallbackCap
// events and seed the truncation point as the full boundary (2026-09-13
// user expectation: WAL is the durable record — recover even without
// compaction).
const fallbackCap = 500

// skipProjectionEvent reports whether ev is a fact-chain record that must
// never become a projection ref (compaction/legacy snapshots, registry
// data, inline tool records) — the double-representation guard shared by
// the tail replay and the fallback full replay.
func skipProjectionEvent(ev memory.FullEvent) bool {
	return ev.EventType == tagentevent.TypeContextCompressSummary ||
		ev.EventType == tagentevent.TypeTaskSpawned ||
		ev.EventType == tagentevent.TypeResidentSession ||
		ev.Metadata[legacySnapshotMetaKey] != "" ||
		ev.Metadata["task_inline_record"] != ""
}

// appendProjectionRef appends ev to the projection as an EventReference,
// stamping meditation outputs first (same treatment as the runtime path).
func (cm *ContextManager) appendProjectionRef(ev memory.FullEvent) {
	if ev.EventType == tagentevent.TypeAgentOutput &&
		ev.Metadata[tagentevent.MetaKeyTriggerSource] == "meditation" {
		cm.contextCompressor.MarkMeditationKey(ev.EventKey)
	}
	cm.projection.Append(memory.EventReference{
		EventKey: ev.EventKey, PartitionID: ev.PartitionID,
		EventType: ev.EventType, EventSummary: ev.EventSummary,
		Timestamp: ev.Timestamp, Role: string(tagentevent.EventTypeRole(ev.EventType)),
	})
}

// rebuildProjectionFallback is the D1 fallback for chains WITHOUT any
// compaction anchor (snapKey==0): full replay from key 0 using the same
// skip-set as the tail replay, newest-wins cap at fallbackCap. The oldest
// kept key is seeded as fullBoundary so recent-window logic sees a
// consistent "everything older is history" truncation marker. The mode is
// logged distinctly (fallback vs snapshot) for observability.
func (cm *ContextManager) rebuildProjectionFallback() {
	all := cm.fetchTailEvents(0)
	if len(all) == 0 {
		log.Infof("[rebuild-projection] fallback: fact chain empty, projection stays empty (mode=fallback)")
		return
	}
	total := len(all)
	truncated := false
	if total > fallbackCap {
		truncated = true
		all = all[total-fallbackCap:]
		log.Warnf("[rebuild-projection] fallback: chain=%d exceeds cap=%d, keeping newest %d",
			total, fallbackCap, fallbackCap)
	}
	for i := range all {
		if skipProjectionEvent(all[i]) {
			continue
		}
		cm.appendProjectionRef(all[i])
	}
	boundary := all[0].EventKey // oldest kept event = truncation marker
	cm.contextCompressor.SetFullBoundary(boundary)
	log.Infof("[rebuild-projection] fallback rebuild: scanned=%d oldest_kept=%d boundary=%d truncated=%v (mode=fallback, no compaction anchor)",
		total, all[0].EventKey, boundary, truncated)
	// hardening-review-batch2 7.1（partial 显式化）：截断必须可被调用方/日志
	// 辨识——VERDICT 行统一两模式的完整性结论（snapshot 模式的对应结论在
	// 主路径汇总行的 lostKeys 字段）。
	if truncated {
		log.Errorf("[rebuild-projection] VERDICT: PARTIAL (fallback truncated to %d of %d events; no compaction anchor — run a compaction to bound future chains)", fallbackCap, total)
	} else {
		log.Infof("[rebuild-projection] VERDICT: FULL (fallback replay complete)")
	}
}

// fetchTailEvents returns the events with EventKey strictly greater than
// afterKey (write axis), key-ascending. Pagination runs until a short page;
// refs are key-sorted before the batched GetEvents (which preserves input
// order and skips missing keys).
//
// KNOWN DEVIATION (review 🟡5): rebuilt refs derive Role from
// EventTypeRole (tool-result events → "user"), while the runtime path
// appends the original message Role ("tool"). Rendering reads
// ref.EventType exclusively (renderTimelineMessage), so the rendered
// prefix is byte-identical; only the ref field differs (debug logs).
// Documented here rather than silently diverging.
func (cm *ContextManager) fetchTailEvents(afterKey int64) []memory.FullEvent {
	var refs []memory.EventReference
	for off := 0; ; off += tailPageSize {
		batch, err := cm.memStore.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{cm.partitionID},
			MinEventKey:  afterKey,
			Limit:        tailPageSize,
			Offset:       off,
		})
		if err != nil {
			log.Errorf("[rebuild-projection] tail page offset=%d failed: %v", off, err)
			break
		}
		refs = append(refs, batch...)
		if len(batch) < tailPageSize {
			break
		}
	}
	if len(refs) == 0 {
		return nil
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].EventKey < refs[j].EventKey })
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey)
	}
	evs, err := cm.memStore.GetEvents(keys)
	if err != nil {
		log.Errorf("[rebuild-projection] tail batch GetEvents failed: %v", err)
		return nil
	}
	return evs
}
