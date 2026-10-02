package agent

import (
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// RecoveryResult is the structured outcome of a cold-start projection rebuild
// : observable by the host (diagnostics) AND by
// the model (a one-shot tail notice on the first request) — a log line alone
// never reached the actual consumers of the recovery.
type RecoveryResult struct {
	Mode          string   `json:"mode"`
	Status        string   `json:"status"`
	Scanned       int      `json:"scanned"`
	Projected     int      `json:"projected"`
	Truncated     int      `json:"truncated"`
	MissingKeys   []string `json:"missing_keys,omitempty"`
	PagesFailed   int      `json:"pages_failed"`
	BatchErrors   int      `json:"batch_errors"`
	PayloadErrors int      `json:"payload_errors"`
	DurationMS    int64    `json:"duration_ms"`
}

// recoveryStatusOf derives the verdict: full only when nothing was lost.
func recoveryStatusOf(r *RecoveryResult) string {
	if r == nil {
		return "unknown"
	}
	if r.PayloadErrors > 0 || (r.PagesFailed > 0 && r.Projected == 0 && r.Scanned == 0) {
		return "failed"
	}
	if r.Truncated > 0 || len(r.MissingKeys) > 0 || r.PagesFailed > 0 || r.BatchErrors > 0 || r.PayloadErrors > 0 {
		return "partial"
	}
	return "full"
}

func formatKeys(keys []int64) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, tagentevent.FormatEventKey(k))
	}
	return out
}

// tailPageSize bounds each QueryEvents page during tail replay. The default
// Limit=100 with asc ordering silently truncates the NEWEST end
// (fresh-eyes 🟡3) — pagination must run until a short page.
const tailPageSize = 500

// RebuildProjectionFromWAL rebuilds the projection from the fact chain at
// cold start (build_agent wiring; runs BEFORE spill replay is armed).
// Startup-only, once, into an EMPTY projection. No marker-tagged compaction
// event in the chain → D1 fallback full replay (rebuildProjectionFallback;
//
//	spec change: WAL is the durable record — context must be
//
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
	result := &RecoveryResult{Mode: "empty", Status: ""}
	defer func() {
		result.DurationMS = time.Since(rebuildStart).Milliseconds()
		if result.Status == "" {
			result.Status = recoveryStatusOf(result)
		}
		cm.recoveryMu.Lock()
		cm.recovery = result
		cm.recoveryNotice = recoveryNotice(result)
		cm.recoveryMu.Unlock()
	}()

	if cm.projection.Len() > 0 {
		result.Status = "skipped-nonempty"
		log.Warnf("[rebuild-projection] projection non-empty (len=%d), skipping rebuild", cm.projection.Len())
		return
	}

	snapKey := cm.latestCompactionKey()
	if snapKey == 0 {
		result.Mode = "fallback"
		cm.rebuildProjectionFallback(result)
		return
	}
	result.Mode = "snapshot"
	snapEv, err := cm.memStore.GetEvent(snapKey)
	if err != nil || snapEv == nil {
		result.PayloadErrors++
		log.Errorf("[rebuild-projection] compaction event %d unreadable: %v", snapKey, err)
		return
	}
	payload, err := compress.UnmarshalPayload(snapEv.Metadata[compress.CompactionPayloadMetaKey])
	if err != nil {
		result.PayloadErrors++
		log.Errorf("[rebuild-projection] compaction payload invalid key=%d: %v", snapKey, err)
		return
	}

	ordered, posKeys, posIdx := payload.RestoreRefs()
	lostKeys := 0
	for i, k := range posKeys {
		ev, err := cm.memStore.GetEvent(k)
		if err != nil || ev == nil {
			lostKeys++
			result.MissingKeys = append(result.MissingKeys, formatKeys([]int64{k})[0])
			log.Errorf("[rebuild-projection] retained key %d missing/tombstoned — SLOT LOST (count=%d): %v", k, lostKeys, err)
			ordered[posIdx[i]] = memory.EventReference{}
			continue
		}
		ordered[posIdx[i]] = memory.EventReference{
			EventKey: ev.EventKey, PartitionID: ev.PartitionID,
			EventType: ev.EventType, EventSummary: ev.EventSummary,
			Timestamp: ev.Timestamp, Role: string(tagentevent.EventTypeRole(ev.EventType)),
		}
		if ev.EventType == tagentevent.TypeAgentOutput &&
			ev.Metadata[tagentevent.MetaKeyTriggerSource] == "meditation" {
			cm.contextCompressor.MarkMeditationKey(ev.EventKey)
		}
	}
	result.Truncated += lostKeys
	final := make([]memory.EventReference, 0, len(ordered))
	for _, ref := range ordered {
		if ref.EventKey != 0 {
			final = append(final, ref)
		}
	}
	result.Projected += len(final)
	cm.projection.Replace(final)

	cm.contextCompressor.SetFullBoundary(payload.FullBoundary)

	tail, _ := cm.fetchTailEvents(snapKey, result)
	result.Scanned += len(tail)
	for _, ev := range tail {
		if tagentevent.IsNonProjectionRecord(ev.EventType, ev.Metadata) {
			continue
		}
		cm.appendProjectionRef(ev)
		result.Projected++
	}

	log.Infof("[rebuild-projection] rebuilt from compaction key=%d: mode=%s snapshot refs=%d tail=%d boundary=%d lostKeys=%d took=%v",
		snapKey, result.Mode, len(final), len(tail), payload.FullBoundary, lostKeys, time.Since(rebuildStart))
}

// fallbackCap 是历史遗留符号，完整回放的上限取消：重建时丢掉最旧事件属于数据丢失，不是内存治理。
// 约束长链是 compaction 锚点的职责。用户预期是「WAL 是持久记录——没有压缩锚点也要能复原」，
// 因此重启前能容纳的链，重启后必须同样能容纳。该符号仍被测试与注释引用，但不作为截断上限生效。
const fallbackCap = 0

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

// rebuildProjectionFallback rebuilds chains without any compaction anchor
// (snapKey==0) by full paginated replay. The required order of the two steps and
// why it must not be inverted are specified in the document below. The oldest kept
// key is seeded as fullBoundary so recent-window logic sees a consistent
// truncation marker; space stays bounded and scan cost stays O(history): there is
// no second checkpoint.
// 契约: docs/wiki/agent/compression-and-telemetry.md#projection-fold
func (cm *ContextManager) rebuildProjectionFallback(result *RecoveryResult) {
	all, _ := cm.fetchTailEvents(0, result)
	result.Scanned += len(all)
	valid := make([]memory.FullEvent, 0, len(all))
	for i := range all {
		if tagentevent.IsNonProjectionRecord(all[i].EventType, all[i].Metadata) {
			continue
		}
		valid = append(valid, all[i])
	}
	if len(valid) == 0 {
		if result.PagesFailed > 0 || result.BatchErrors > 0 {
			result.Status = "failed"
			log.Errorf("[rebuild-projection] fallback: chain unreadable (not empty) — see error counters")
			return
		}
		log.Infof("[rebuild-projection] fallback: fact chain empty, projection stays empty (mode=fallback)")
		return
	}
	truncated := false
	if fallbackCap > 0 && len(all) > fallbackCap {
		truncated = true
	}
	for i := range valid {
		if tagentevent.IsNonProjectionRecord(valid[i].EventType, valid[i].Metadata) {
			continue
		}
		cm.appendProjectionRef(valid[i])
		result.Projected++
	}
	boundary := valid[0].EventKey
	cm.contextCompressor.SetFullBoundary(boundary)
	log.Infof("[rebuild-projection] fallback rebuild: scanned=%d oldest_kept=%d boundary=%d truncated=%v (mode=fallback, no compaction anchor)",
		result.Scanned, all[0].EventKey, boundary, truncated)
	if truncated || result.PagesFailed > 0 || result.BatchErrors > 0 {
		log.Errorf("[rebuild-projection] VERDICT: PARTIAL (fallback truncated=%v to %d of %d events, fetchFailures=%v; no compaction anchor — run a compaction to bound future chains)", truncated, len(all), result.Scanned, result.PagesFailed+result.BatchErrors)
	} else {
		log.Infof("[rebuild-projection] VERDICT: FULL (fallback replay complete)")
	}
}

// tailFetchStats fetchTailEvents returns the events with EventKey strictly greater than
// afterKey (write axis), key-ascending. Pagination runs until a short page;
// refs are key-sorted before the batched GetEvents (which preserves input
// order and skips missing keys).
//
// KNOWN DEVIATION: rebuilt refs derive Role from
// EventTypeRole (tool-result events → "user"), while the runtime path
// appends the original message Role ("tool"). Rendering reads
// ref.EventType exclusively (renderTimelineMessage), so the rendered
// prefix is byte-identical; only the ref field differs (debug logs).
// Documented here rather than silently diverging.
// tailFetchStats carries the observability
// counters: a partial (page/batch failure) fetch must be visible to the
// caller so the rebuild verdict is PARTIAL, never silently short.
type tailFetchStats struct {
	pagesFailed int
	batchErrors int
	missing     []int64
}

func (s tailFetchStats) anyFailure() bool { return s.pagesFailed > 0 || s.batchErrors > 0 }

func (cm *ContextManager) fetchTailEvents(afterKey int64, result *RecoveryResult) ([]memory.FullEvent, tailFetchStats) {
	var refs []memory.EventReference
	var stats tailFetchStats
	for off := 0; ; off += tailPageSize {
		batch, err := cm.memStore.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{cm.partitionID},
			MinEventKey:  afterKey,
			Limit:        tailPageSize,
			Offset:       off,
		})
		if err != nil {
			stats.pagesFailed++
			if result != nil {
				result.PagesFailed++
			}
			log.Errorf("[rebuild-projection] tail page offset=%d failed: %v", off, err)
			break
		}
		refs = append(refs, batch...)
		if len(batch) < tailPageSize {
			break
		}
	}
	if len(refs) == 0 {
		return nil, stats
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].EventKey < refs[j].EventKey })
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey)
	}
	evs, err := cm.memStore.GetEvents(keys)
	if err != nil {
		stats.batchErrors++
		if result != nil {
			result.BatchErrors++
		}
		log.Errorf("[rebuild-projection] tail batch GetEvents failed: %v", err)
	}
	got := make(map[int64]bool, len(evs))
	for _, ev := range evs {
		got[ev.EventKey] = true
	}
	for _, k := range keys {
		if !got[k] {
			stats.missing = append(stats.missing, k)
			if result != nil {
				result.MissingKeys = append(result.MissingKeys, formatKeys([]int64{k})[0])
			}
		}
	}
	return evs, stats
}
