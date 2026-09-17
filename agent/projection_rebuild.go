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

// RecoveryResult is the structured outcome of a cold-start projection rebuild
// (resident-readiness-plan 3.8): observable by the host (diagnostics) AND by
// the model (a one-shot tail notice on the first request) — a log line alone
// never reached the actual consumers of the recovery.
type RecoveryResult struct {
	Mode          string   `json:"mode"`      // empty | snapshot | fallback
	Status        string   `json:"status"`    // full | partial | failed
	Scanned       int      `json:"scanned"`   // fact-chain events read
	Projected     int      `json:"projected"` // refs restored
	Truncated     int      `json:"truncated"` // VALID projection events dropped by the guardrail
	MissingKeys   []string `json:"missing_keys,omitempty"`
	PagesFailed   int      `json:"pages_failed"`
	BatchErrors   int      `json:"batch_errors"`
	PayloadErrors int      `json:"payload_errors"`
	DurationMS    int64    `json:"duration_ms"`
	// ReceiptedRequestIDs (cold-eyes Major 2): inbox receipt events seen during
	// the rebuild scan — the startup reconcile converges envelopes whose
	// receipt landed but whose ack did not (crash window), no re-execution.
	ReceiptedRequestIDs []string `json:"receipted_request_ids,omitempty"`
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
	// Status starts EMPTY: the deferred verdict only fills an unset status so
	// direct assignments ("failed", "skipped-nonempty") are never overridden
	// (cold-eyes R2 Minor 3).
	result := &RecoveryResult{Mode: "empty", Status: ""}
	defer func() {
		result.DurationMS = time.Since(rebuildStart).Milliseconds()
		// cold-eyes R2 Minor 3: direct assignments inside the rebuild paths
		// ("failed" = an error swallowed as an empty chain is a lie;
		// "skipped-nonempty" = a real precondition skip) must not be silently
		// overridden by the derived verdict — recoveryStatusOf may only fill
		// an unset status.
		if result.Status == "" {
			result.Status = recoveryStatusOf(result)
		}
		cm.recoveryMu.Lock()
		cm.recovery = result
		cm.recoveryNotice = recoveryNotice(result)
		cm.recoveryMu.Unlock()
	}()

	if cm.projection.Len() > 0 {
		// Non-empty projection at rebuild time (e.g. spill replay already
		// appended): WARN, never silently skip — and never Replace a live
		// projection (Replace-over-live is the dev-patch bug we removed).
		result.Status = "skipped-nonempty"
		log.Warnf("[rebuild-projection] projection non-empty (len=%d), skipping rebuild", cm.projection.Len())
		return
	}

	snapKey := cm.latestCompactionKey()
	if snapKey == 0 {
		// D1 fallback (2026-09-13 spec change): WAL is the durable record —
		// recover context even without any compaction anchor; supersedes no-op.
		result.Mode = "fallback"
		cm.rebuildProjectionFallback(result)
		return
	}
	result.Mode = "snapshot"
	snapEv, err := cm.memStore.GetEvent(snapKey)
	if err != nil || snapEv == nil {
		result.PayloadErrors++
		log.Errorf("[rebuild-projection] compaction event %d unreadable: %v", snapKey, err)
		return // failed
	}
	payload, err := compress.UnmarshalPayload(snapEv.Metadata[compress.CompactionPayloadMetaKey])
	if err != nil {
		result.PayloadErrors++
		log.Errorf("[rebuild-projection] compaction payload invalid key=%d: %v", snapKey, err)
		return // failed
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
		// Meditation reseed (snapshot side, event-sourced-projection D3 /
		// fresh-eyes C): trigger_source now persists in agent_output Metadata
		// (buildTurnAttribution) — unreadable before that stamp existed.
		if ev.EventType == tagentevent.TypeAgentOutput &&
			ev.Metadata[tagentevent.MetaKeyTriggerSource] == "meditation" {
			cm.contextCompressor.MarkMeditationKey(ev.EventKey)
		}
	}
	result.Truncated += lostKeys
	final := make([]memory.EventReference, 0, len(ordered))
	for _, ref := range ordered {
		if ref.EventKey != 0 { // dropped placeholders (invalid zero key)
			final = append(final, ref)
		}
	}
	result.Projected += len(final)
	cm.projection.Replace(final)

	// Seed fullBoundary from the payload — the exact runtime state at fold
	// time (under-budget rounds never touch it), NOT a recompute over
	// snapshot+tail (that would shift the recent window and change renders).
	cm.contextCompressor.SetFullBoundary(payload.FullBoundary)

	// 2) Tail replay: events written AFTER the compaction event. Three bans
	//    (fresh-eyes A/B): no StartTime approximation (dual-time divergence),
	//    no Timestamp-order replay (runtime append order ≡ EventKey write
	//    order; stragglers invert), no silent truncation (paginated).
	tail, _ := cm.fetchTailEvents(snapKey, result)
	result.Scanned += len(tail)
	for _, ev := range tail {
		if ev.EventType == tagentevent.TypeInboxReceipt {
			if rid := ev.Metadata["inbox_request_id"]; rid != "" {
				result.ReceiptedRequestIDs = append(result.ReceiptedRequestIDs, rid)
			}
		}
		// Compaction/legacy-snapshot events are fact-chain records, never
		// projection refs (double-representation guard, same as the replay
		// handler). R2 task records likewise: task_spawned is registry data
		// (board renders live from the registry); task_inline_record settles
		// returned in-turn as tool results (appending would double-render).
		if skipProjectionEvent(ev) {
			continue
		}
		cm.appendProjectionRef(ev)
		result.Projected++
	}

	log.Infof("[rebuild-projection] rebuilt from compaction key=%d: mode=%s snapshot refs=%d tail=%d boundary=%d lostKeys=%d took=%v",
		snapKey, result.Mode, len(final), len(tail), payload.FullBoundary, lostKeys, time.Since(rebuildStart))
}

// fallbackCap bounds the D1 fallback full replay: a chain without any
// compaction anchor may be arbitrarily long, so keep the NEWEST fallbackCap
// events and seed the truncation point as the full boundary (2026-09-13
// user expectation: WAL is the durable record — recover even without
// compaction).
// DEPRECATED 2026-09-16 (host directive): full-replay cap removed. A chain
// that fit before a restart must fit after it — dropping the oldest events on
// rebuild is data loss, not memory hygiene. Long-chain bounding is the job of
// compaction anchors, not an arbitrary replay cap. Symbol kept (referenced by
// tests/legacy comments) but no longer applied as a truncation bound.
const fallbackCap = 0

// skipProjectionEvent reports whether ev is a fact-chain record that must
// never become a projection ref (compaction/legacy snapshots, registry
// data, inline tool records) — the double-representation guard shared by
// the tail replay and the fallback full replay.
func skipProjectionEvent(ev memory.FullEvent) bool {
	return ev.EventType == tagentevent.TypeContextCompressSummary ||
		ev.EventType == tagentevent.TypeTaskSpawned ||
		ev.EventType == tagentevent.TypeInboxReceipt ||
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
// compaction anchor (snapKey==0): full paginated replay, then FILTER non-
// projection records FIRST, then keep the NEWEST fallbackCap VALID events
// (resident-readiness-plan 3.9 — the old take-500-then-filter let registry/
// receipt records crowd out real history). The oldest kept key is seeded as
// fullBoundary so recent-window logic sees a consistent truncation marker.
// Space stays bounded; scan cost stays O(history) — no second checkpoint.
func (cm *ContextManager) rebuildProjectionFallback(result *RecoveryResult) {
	all, _ := cm.fetchTailEvents(0, result)
	result.Scanned += len(all)
	valid := make([]memory.FullEvent, 0, len(all))
	for i := range all {
		if all[i].EventType == tagentevent.TypeInboxReceipt {
			if rid := all[i].Metadata["inbox_request_id"]; rid != "" {
				result.ReceiptedRequestIDs = append(result.ReceiptedRequestIDs, rid)
			}
		}
		if skipProjectionEvent(all[i]) {
			continue // task/receipt/snapshot records never occupy the 500 slots
		}
		valid = append(valid, all[i])
	}
	if len(valid) == 0 {
		if result.PagesFailed > 0 || result.BatchErrors > 0 {
			result.Status = "failed" // an error swallowed as "empty chain" is a lie
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
	for i := range valid { // fetchTailEvents already returns EventKey-ascending
		if skipProjectionEvent(valid[i]) {
			continue
		}
		cm.appendProjectionRef(valid[i])
		result.Projected++
	}
	boundary := valid[0].EventKey // oldest kept event = truncation marker
	cm.contextCompressor.SetFullBoundary(boundary)
	log.Infof("[rebuild-projection] fallback rebuild: scanned=%d oldest_kept=%d boundary=%d truncated=%v (mode=fallback, no compaction anchor)",
		result.Scanned, all[0].EventKey, boundary, truncated)
	// hardening-review-batch2 7.1（partial 显式化）：截断必须可被调用方/日志
	// 辨识——VERDICT 行统一两模式的完整性结论（snapshot 模式的对应结论在
	// 主路径汇总行的 lostKeys 字段）。
	if truncated || result.PagesFailed > 0 || result.BatchErrors > 0 {
		log.Errorf("[rebuild-projection] VERDICT: PARTIAL (fallback truncated=%v to %d of %d events, fetchFailures=%v; no compaction anchor — run a compaction to bound future chains)", truncated, len(all), result.Scanned, result.PagesFailed+result.BatchErrors)
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
// tailFetchStats carries the hardening-review-batch2 7.2 observability
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
		// partial events are still usable — keep going with what we got
	}
	// Key-set reconciliation (3.8): a no-error short read is STILL a hole —
	// request keys vs returned keys must match, else record the difference.
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
