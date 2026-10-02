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

// settleNoticeBodyPrefix bounds the CANDIDATE FILTER only: a projection ref
// whose summary starts like a settle notice deserves one authoritative look.
// It is NOT the recognition authority — a forged body passes the filter and
// is then refused by the missing settle_notice mark (D10). Keeping the cheap
// prefix pass is what preserves the zero-extra-lookup budget: only candidates
// hit GetEvent, and that single read serves both the mark and the lineage.
const settleNoticeBodyPrefix = "[task settled"

// TelemetryDispositions folds the projection refs into a per-settle-key
// disposition map. store may be nil (pure structural mode: the authoritative
// mark cannot be verified, candidates are treated as notices but externalization
// is undecidable → internal, conservative per the unknown-withhold
// philosophy). keepRecent bounds the internal reminder window. A candidate
// whose event carries no settle_notice mark (forged body, or an event written
// before the mark existed) is never fold-eligible and stays verbatim.
func TelemetryDispositions(ctx context.Context, store memory.MemoryStore, refs []memory.EventReference, keepRecent int) map[int64]int8 {
	out := map[int64]int8{}
	for i, ref := range refs {
		if !isSettleNoticeCandidate(ref) {
			continue
		}
		evt := lookupNoticeEvent(store, ref)
		if evt != nil && !isMarkedSettleNotice(evt) {
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
		case isExternalizedNotice(evt):
			out[ref.EventKey] = TelemDemote
		case outputsAfter >= keepRecent:
			out[ref.EventKey] = TelemDemote
		default:
			out[ref.EventKey] = TelemInternal
		}
	}
	return out
}

// isSettleNoticeCandidate is the cheap structural candidate filter (see
// settleNoticeBodyPrefix). Recognition authority is the settle_notice
// metadata mark verified against the stored event — see TelemetryDispositions.
func isSettleNoticeCandidate(ref memory.EventReference) bool {
	if ref.EventType != tagentevent.TypeExternalInput {
		return false
	}
	s := strings.TrimSpace(tagentevent.StripEventKeyPrefix(ref.EventSummary))
	return strings.HasPrefix(s, settleNoticeBodyPrefix)
}

// isMarkedSettleNotice reads the authoritative production-side mark.
func isMarkedSettleNotice(evt *memory.FullEvent) bool {
	return strings.EqualFold(evt.Metadata["settle_notice"], "true")
}

// lookupNoticeEvent performs the ONE stored-event read per candidate (mark +
// lineage together). nil store or an unreadable event returns nil: the
// caller then treats the ref as an unverified candidate (structural mode)
// and cannot prove externalization — notices whose evidence is missing stay
// verbatim rather than betting on an undelivered reclaim.
func lookupNoticeEvent(store memory.MemoryStore, ref memory.EventReference) *memory.FullEvent {
	if store == nil || ref.EventKey <= 0 {
		return nil
	}
	evt, err := store.GetEvent(ref.EventKey)
	if err != nil {
		return nil
	}
	return evt
}

// isExternalizedNotice reports whether the settle notice's lineage is a
// deliverable (host-facing) one — the SAME whitelist the host delivery gate
// consumes. Missing evidence and non-deliverable lineages (including
// task-unstamped and any unknown value) all resolve to internal
// (conservative: keep the verbatim reminder longer rather than betting on a
// delivery the evidence cannot prove). evt nil (unverifiable) → internal.
func isExternalizedNotice(evt *memory.FullEvent) bool {
	if evt == nil {
		return false
	}
	if strings.EqualFold(evt.Metadata["lineage_absent"], "true") {
		return false
	}
	ts := evt.Metadata["meta_trigger_source"]
	if ts == "" {
		ts = evt.Metadata[tagentevent.MetaKeyTriggerSource]
	}
	return ts != "" && tagentevent.DeliverableLineage(ts)
}
