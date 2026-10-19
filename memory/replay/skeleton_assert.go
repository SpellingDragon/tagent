// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the replay shapes: the presentation
// decision, the chain-isolated replay source and relation store, the compaction
// payload and its refs, and the cold-start rebuild that replays a fact chain into
// an empty projection. A renamed field or a widened signature stops this file
// from compiling.
package replay

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// The declared shapes and their fields.
var (
	_ = ProjectionEntry{
		Key:     "",
		Type:    "",
		Summary: "",
		Content: "",
		Time:    time.Time{},
	}

	_ = PresentationDecision{
		Key:     "",
		Mode:    PresentationUnrecorded,
		Preview: "",
	}
)

// The presentation modes must stay compile-time constants: a variable or a
// computed value stops this block from compiling.
const (
	_ = PresentationUnrecorded
	_ = PresentationFull
	_ = PresentationSummary
	_ = PresentationTicket
)

// The replay projection keeps its declared methods.
var (
	_ func(*ReplayProjection, context.Context, ProjectionEntry) = (*ReplayProjection).Append

	_ func(*ReplayProjection, context.Context) (ProjectionEntry, error) = (*ReplayProjection).Fold

	_ func(*ReplayProjection, context.Context, int) ([]ProjectionEntry, error) = (*ReplayProjection).AssembleBudget

	_ func(*ReplayProjection) []ReplaySettle = (*ReplayProjection).Settles
)

// The projection replay keeps its declared shapes: the chain-isolated replay
// source and relation store, the compaction payload and its refs, and the
// cold-start rebuild that replays a fact chain into an empty projection.
var (
	_ = ChainKey{PartitionID: 0, SessionID: ""}
	_ = ChainFact{Key: "", Chain: ChainKey{}, Type: "", Summary: "", Content: "", Time: time.Time{}}
	_ = CompactionFact{Summary: CompactionRef{}, Refs: []CompactionRef{}, FullBoundary: "", ConsumedKeys: []event.EventID{}}
	_ = CompactionRef{Key: "", Type: "", Summary: "", Content: "", Time: time.Time{}}
	_ = ReplaySettle{ID: "", Entry: "", Mode: PresentationUnrecorded, Preview: "", At: time.Time{}}
	_ = RebuildOptions{PageSize: 0, FallbackBound: 0}
	_ = RebuildReport{Entries: 0, Fallback: false, Dropped: 0, Scanned: false, Skipped: "", Partial: false, Missing: []event.EventID{}, Consumed: []event.EventID{}, FullBoundary: ""}

	_ EventChain = (EventChain)(nil)
	_            = (*MemoryRelations)(nil)

	_ func(*MemoryRelations, event.EventID, event.EventID) error   = (*MemoryRelations).SetParent
	_ func(*MemoryRelations, event.EventID) (event.EventID, error) = (*MemoryRelations).GetParent
	_ func(*MemoryRelations) map[event.EventID]event.EventID       = (*MemoryRelations).Links
	_ func(*MemoryRelations, context.Context) error                = (*MemoryRelations).VerifyReplay
	_ func(string) (*MemoryRelations, error)                       = OpenMemoryRelations

	_ func() *ReplayProjection                                                                                                = NewReplayProjection
	_ func(context.Context, *ReplayProjection, EventChain, *MemoryRelations, ChainKey, RebuildOptions) (RebuildReport, error) = RebuildProjectionFromWAL
	_ func([]ProjectionEntry) []byte                                                                                          = RenderProjection
	_ func(string) bool                                                                                                       = RetainRefType
	_ func(PresentationDecision) ReplaySettle                                                                                 = ReplaySettleFor

	_ error = ErrChainKeyRequired
	_ error = ErrRebuildUnwired
	_ error = ErrChainPageLimit
	_ error = ErrChainScan
	_ error = ErrCompactionCorrupt
	_ error = ErrRebuildPartial
	_ error = ErrFoldEmpty
	_ error = ErrBudgetInvalid
	_ error = ErrRelationsJournalUnwired
	_ error = ErrRelationsReplayMismatch

	_ = ProjectionTypeRecoveryHint
)
