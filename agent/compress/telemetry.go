package compress

import (
	"context"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TelemActive Telemetry disposition values for settle-notice refs (keyed by EventKey).
const (
	// TelemActive: not yet consumed (no reclaim turn produced after it).
	// It must never be demoted or L3-archived — at-least-once reaches the
	// model view intact (不可丢级的通道层落点).
	TelemActive int8 = iota
	// TelemInternal: consumed but the reclaim output is NOT deliverable to
	// the host (internal lineage: meditation-spawned, retired-settle,
	// unknown/absent lineage). Kept verbatim for up to keepRecent turns as
	// a short-term reminder, then demoted.
	TelemInternal
	// TelemDemote: consumed and externalized (or an internal notice that
	// aged past its reminder window) — demotes to a ticket card at the next
	// compaction act. Details stay recallable via the fact chain.
	TelemDemote
)

// internalLineageValues are trigger-source values whose reclaim output is NOT
// delivered to the host (the fail-closed delivery gate holds them). A notice
// carrying one was consumed by an internal turn: short reminder, then card.
var internalLineageValues = map[string]bool{
	"meditation":   true,
	"task-retired": true,
	"unknown":      true,
}

// TelemetryDispositions folds the projection refs into a per-settle-key
// disposition map. store may be nil (pure structural mode: externalization
// undecidable → treated internal, conservative per the unknown-withhold
// philosophy). keepRecent bounds the internal reminder window.
func TelemetryDispositions(ctx context.Context, store memory.MemoryStore, refs []memory.EventReference, keepRecent int) map[int64]int8 {
	out := map[int64]int8{}
	for i, ref := range refs {
		if !isSettleNoticeRef(ref) {
			continue
		}
		consumer := -1
		outputsAfter := 0
		for j := i + 1; j < len(refs); j++ {
			if refs[j].EventType != tagentevent.TypeAgentOutput {
				continue
			}
			if consumer < 0 {
				consumer = j
			} else {
				outputsAfter++
			}
		}
		switch {
		case consumer < 0:
			out[ref.EventKey] = TelemActive
		case isExternalizedNotice(ctx, store, ref):
			out[ref.EventKey] = TelemDemote
		case outputsAfter >= keepRecent:
			out[ref.EventKey] = TelemDemote
		default:
			out[ref.EventKey] = TelemInternal
		}
	}
	return out
}

// isExternalizedNotice reports whether the settle notice's lineage is a
// deliverable (host-facing) one. Missing metadata, store errors and unknown
// lineage all resolve to internal (conservative: keep the verbatim reminder
// longer rather than betting on a delivery the evidence cannot prove).
func isExternalizedNotice(ctx context.Context, store memory.MemoryStore, ref memory.EventReference) bool {
	if store == nil || ref.EventKey <= 0 {
		return false
	}
	evt, err := store.GetEvent(ref.EventKey)
	if err != nil || evt == nil {
		return false
	}
	if strings.EqualFold(evt.Metadata["lineage_absent"], "true") {
		return false
	}
	ts := evt.Metadata["meta_trigger_source"]
	if ts == "" {
		ts = evt.Metadata[tagentevent.MetaKeyTriggerSource]
	}
	if ts == "" || internalLineageValues[ts] {
		return false
	}
	return true
}
