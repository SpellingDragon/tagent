// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the replay shapes: the projection entry,
// the chain-isolated replay source and relation store, the compaction payload and
// its refs, the cold-start rebuild that replays a fact chain into an empty
// projection, and the segment-age axis the bands and their folds are declared
// under. A renamed field or a widened signature stops this file from compiling.
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
)

// The replay projection keeps its declared methods.
var (
	_ func(*ReplayProjection, context.Context, ProjectionEntry) = (*ReplayProjection).Append

	_ func(*ReplayProjection, context.Context) (ProjectionEntry, error) = (*ReplayProjection).Fold

	_ func(*ReplayProjection, context.Context, int) ([]ProjectionEntry, error) = (*ReplayProjection).AssembleBudget

	_ func(*ReplayProjection, context.Context, int, int) ([]ProjectionEntry, error) = (*ReplayProjection).AssembleBudgetPrefix

	_ func(*ReplayProjection, context.Context, AgeFoldPlan) (AgeFoldReport, error) = (*ReplayProjection).FoldByAge

	// The judgment, the execution and the restore of one fold keep their shapes:
	// a plan reads the projection, the products it answers are placed onto it,
	// and the products a chain recorded are read back from their facts.
	_ func(*ReplayProjection, context.Context, AgeFoldPlan) (AgeFoldPlanReport, error) = (*ReplayProjection).PlanByAge
	_ func(*ReplayProjection, []FoldProduct)                                           = (*ReplayProjection).ApplyProducts
	_ func(*ReplayProjection, []event.CompressProduct)                                 = (*ReplayProjection).RestoreProducts
	_                                                                                  = FoldProduct{Entry: ProjectionEntry{}, Covered: []event.EventID{}, Request: event.CompressRequest{}}
	_                                                                                  = AgeFoldPlanReport{Volume: 0, Threshold: 0, Triggered: false, Counts: FoldedCounts{}, Products: []FoldProduct{}}
)

// The segment-age axis keeps its declared shapes: one band per age band, the
// partition of a projection into task-turn segments, and the plan and report an
// age-band fold is judged under.
var (
	_ Band = BandNew
	_ Band = BandMid
	_ Band = BandOld

	_ = Segment{Index: 0, Age: 0, Level: 0, Band: BandNew, Complete: false, Entries: []ProjectionEntry{}}
	_ = AgeFoldPlan{KeepRecent: 0, VolumeThreshold: 0}
	_ = AgeFoldReport{
		Volume:    0,
		Threshold: 0,
		Triggered: false,
		NewFolded: 0,
		MidFolded: 0,
		Products:  []ProjectionEntry{},
		Requests:  []event.CompressRequest{},
	}

	_ func(age int, keepRecent int) int      = LevelOfAge
	_ func(level int) Band                   = BandOfLevel
	_ func(age int, keepRecent int) Band     = BandOfAge
	_ func([]ProjectionEntry, int) []Segment = SegmentsOf

	_ string = ProjectionTypeSegmentFold
	_ string = ProjectionTypeHistorySummary

	_ = DefaultAgeKeepRecent
	_ = DefaultAgeVolumeThreshold
	_ = BudgetCharsPerToken
)

// The projection replay keeps its declared shapes: the chain-isolated replay
// source and relation store, the compaction payload and its refs, and the
// cold-start rebuild that replays a fact chain into an empty projection.
var (
	_ = ChainKey{PartitionID: 0, SessionID: ""}
	_ = ChainFact{Key: "", Chain: ChainKey{}, Type: "", Summary: "", Content: "", Time: time.Time{}}
	_ = CompactionFact{Summary: CompactionRef{}, Refs: []CompactionRef{}, FullBoundary: "", ConsumedKeys: []event.EventID{}}
	_ = CompactionRef{Key: "", Type: "", Summary: "", Content: "", Time: time.Time{}}
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
