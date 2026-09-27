package compress

// Telemetry channel (attention-budget-architecture L1/L2): task-settle
// notifications are machine telemetry, not conversational input — their
// retention is governed by CONSUMPTION STATE, not segment age or adjacency.
// The dispositions here are a pure deterministic fold over (projection ref
// order + settle fact metadata): the consuming turn is the first
// agent_output after the notice; externalization is decided by the lineage
// the Origin courier stamped on the notice. Zero new persistence, zero LLM
// (specs/telemetry-channel: 消费状态决定遥测退出时点 / 跨重启消费状态重建).

import (
	"context"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// Telemetry disposition values for settle-notice refs (keyed by EventKey).
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
				consumer = j // the reclaim turn that consumed this notice
			} else {
				outputsAfter++ // further turns have completed since
			}
		}
		switch {
		case consumer < 0:
			out[ref.EventKey] = TelemActive
		case isExternalizedNotice(ctx, store, ref):
			out[ref.EventKey] = TelemDemote
		case outputsAfter >= keepRecent:
			out[ref.EventKey] = TelemDemote // internal notice aged out of its reminder window
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
	// task-sourced notices spawned BY a user/external turn are deliverable
	// back to that origin — the reclaim output externalizes the notice.
	return true
}
