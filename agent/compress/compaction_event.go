package compress

import (
	"encoding/json"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// Compaction event metadata keys (event-sourced-projection D4/D6): the
// generation marker scopes supersede/snapshot selection to compaction
// events THIS system produced — legacy 固化物 (same event type, no marker,
// TTL-immortal) is never deleted nor selected. The payload JSON carries the
// rebuild state; the recall body stays the narrative itself.
const (
	CompactionMetaKey        = "compaction"
	CompactionGenV1          = "v1"
	CompactionPayloadMetaKey = "compaction_payload"
)

// RetainedEntry is one slot of the fold's ordered retained list. Positive
// keys carry only the key (the store is the source of truth: GetEvent
// rebuilds the ref byte-exact). Negative-key synthetic refs (tool_chain)
// carry the FULL ref verbatim — the store holds no event behind them and
// they are not recomputable (fresh-eyes: dropping them loses the
// `- 工具链:` render lines).
type RetainedEntry struct {
	Key int64 `json:"key"`
	// Ref is set only for negative-key synthetic entries (full identity +
	// EventSummary, byte-exact).
	Ref *memory.EventReference `json:"ref,omitempty"`
}

// CompactionPayload is the wire form of one compression fold, persisted as
// a context_compress_summary fact-chain event (event-sourced-projection D1).
// It captures everything about the fold that is NOT recomputable: the
// composed summary text, the summary ref identity, the ordered (interleaved)
// retained list, and fullBoundary.
type CompactionPayload struct {
	// SummaryRef is the projection's new negative-key summary reference —
	// full identity + composed EventSummary, byte-exact.
	SummaryRef memory.EventReference `json:"summary_ref"`
	// Retained is the ordered retained list (synthetic and positive entries
	// interleaved in projection order; the summary ref itself excluded).
	Retained []RetainedEntry `json:"retained"`
	// FullBoundary seeds the rebuilt compressor's full-render boundary.
	FullBoundary int64 `json:"full_boundary"`
}

// BuildCompactionPayload derives the persistable fold record from a fresh
// fold's projection refs. ok=false when retained does not start with a
// negative-key context_compress summary ref — i.e. no REAL fold happened
// this round (under-budget rounds must not emit; spec: 未折叠不写).
// Pure: no I/O, no LLM.
func BuildCompactionPayload(retained []memory.EventReference, fullBoundary int64) (p *CompactionPayload, ok bool) {
	if len(retained) == 0 {
		return nil, false
	}
	head := retained[0]
	if head.EventKey >= 0 || head.EventType != event.TypeContextCompress {
		return nil, false
	}
	p = &CompactionPayload{
		SummaryRef:   head,
		Retained:     make([]RetainedEntry, 0, len(retained)-1),
		FullBoundary: fullBoundary,
	}
	for _, ref := range retained[1:] {
		if ref.EventKey == 0 {
			continue
		}
		entry := RetainedEntry{Key: ref.EventKey}
		if ref.EventKey < 0 {
			cp := ref
			entry.Ref = &cp
		}
		p.Retained = append(p.Retained, entry)
	}
	return p, true
}

// RestoreRefs reconstructs the projection refs from the payload in STORED
// ORDER: [SummaryRef] + retained entries. Synthetic entries contribute
// their verbatim ref; positive entries return their keys separately — the
// caller resolves them via GetEvent (immutable store = byte-exact ref) and
// fills ordered[posIdx[i]]. Missing/tombstoned keys degrade to a skip (the
// caller logs WARN and drops the slot — never blocks the rebuild).
func (p *CompactionPayload) RestoreRefs() (ordered []memory.EventReference, posKeys []int64, posIdx []int) {
	ordered = make([]memory.EventReference, 0, len(p.Retained)+1)
	ordered = append(ordered, p.SummaryRef)
	for _, e := range p.Retained {
		if e.Key > 0 {
			posIdx = append(posIdx, len(ordered))
			posKeys = append(posKeys, e.Key)
			ordered = append(ordered, memory.EventReference{}) // placeholder
		} else if e.Ref != nil {
			ordered = append(ordered, *e.Ref)
		}
	}
	return ordered, posKeys, posIdx
}

// MarshalPayload serializes the payload for Metadata[CompactionPayloadMetaKey].
func (p *CompactionPayload) MarshalPayload() (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// UnmarshalPayload parses Metadata[CompactionPayloadMetaKey].
func UnmarshalPayload(raw string) (*CompactionPayload, error) {
	var p CompactionPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
