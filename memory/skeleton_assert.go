// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the phase 1 memory shapes. The phase 2
// implementation types that will satisfy the three ports do not exist yet, so
// there is no `var _ Iface = (*Impl)(nil)` to write; the assertions below prove
// what the skeleton can prove on its own, pinning every declared field name and
// type, every declared method signature, and every constant that must stay a
// compile-time constant.
package memory

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// The declared shapes and their fields.
var (
	_ = OriginalRef{
		Key:             "",
		Owner:           event.Address{},
		Format:          "",
		RetentionPolicy: "",
		Integrity:       Integrity{},
	}

	_ = Integrity{
		Digest: "",
		Length: 0,
	}

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

	_ = RecallResult{
		Key:     "",
		Outcome: RecallUnclassified,
		Body:    []byte{},
		Err:     nil,
	}

	_ = ReadScope{
		Owner:           event.Address{},
		Format:          "",
		RetentionPolicy: "",
	}

	_ = HolderID("")

	_ = ProtectionLease("")

	_ = SegmentPosition{Offset: 0}

	_ = SegmentRequest{
		From:     SegmentPosition{},
		MaxBytes: 0,
	}

	_ = SegmentResult{
		Body: []byte{},
		Next: SegmentPosition{},
		EOF:  false,
	}
)

// The declared contract refusals and the pure accessors they resolve through.
var (
	_ error                       = ErrScopeRequired
	_ error                       = ErrLeaseMismatch
	_ func(ReadScope) error       = ReadScope.Validate
	_ func(OriginalRef) ReadScope = OriginalRef.ReadScope
)

// The exported port methods must keep exactly these signatures.
var (
	_ func(OriginalStore, context.Context, OriginalRef, []byte) error = OriginalStore.Put

	_ func(OriginalStore, context.Context, event.EventID) RecallResult = OriginalStore.Get

	_ func(OriginalStore, context.Context, event.EventID, int, int) ([]byte, error) = OriginalStore.ReadSegment

	_ func(ScopedOriginalStore, context.Context, event.EventID, ReadScope) RecallResult = ScopedOriginalStore.GetScoped

	_ func(ScopedOriginalStore, context.Context, event.EventID, ReadScope, SegmentRequest) (SegmentResult, error) = ScopedOriginalStore.ReadSegmentScoped

	_ func(*ScopedReader, context.Context, event.EventID) RecallResult = (*ScopedReader).Get

	_ func(*ScopedReader, context.Context, event.EventID, SegmentRequest) (SegmentResult, error) = (*ScopedReader).ReadSegment

	_ func(*ReplayProjection, context.Context, ProjectionEntry) = (*ReplayProjection).Append

	_ func(*ReplayProjection, context.Context) (ProjectionEntry, error) = (*ReplayProjection).Fold

	_ func(*ReplayProjection, context.Context, []PresentationDecision) ([]ProjectionEntry, error) = (*ReplayProjection).Rebuild

	_ func(*ReplayProjection, context.Context, int) ([]ProjectionEntry, error) = (*ReplayProjection).AssembleBudget

	_ func(*ReplayProjection) []ReplaySettle = (*ReplayProjection).Settles

	_ func(context.Context, *ReplayProjection) (CompactionFact, error) = Prefold

	_ func(CompactionFact) event.EventID = CompactionFactKey

	_ func(ChainKey, CompactionFact, time.Time) (ChainFact, error) = CompactionChainFact

	_ func(Guard, context.Context, OriginalRef, HolderID) (ProtectionLease, error) = Guard.Hold

	_ func(Guard, context.Context, OriginalRef, HolderID, ProtectionLease) error = Guard.Release

	_ func(Guard, context.Context, OriginalRef) (bool, error) = Guard.Protected
)

// The declared ports must stay interface types.
var (
	_ OriginalStore       = (OriginalStore)(nil)
	_ ScopedOriginalStore = (ScopedOriginalStore)(nil)
	_ Guard               = (Guard)(nil)
)

// The presentation modes and recall outcomes must stay compile-time constants: a
// variable or a computed value stops this block from compiling.
const (
	_ = PresentationUnrecorded
	_ = PresentationFull
	_ = PresentationSummary
	_ = PresentationTicket

	_ = RecallUnclassified
	_ = RecallFound
	_ = RecallMissing
	_ = RecallExpired
	_ = RecallFailed

	_ = DefaultSegmentMaxBytes
)

// The unconfirmed-delivery replay face keeps its documented shapes: the
// built-in delivery record, the owner-resolution and sink seams, the replay
// engine's entry and the durable fact surface one tier opens over.
var (
	_ = DeliveryRecord{
		TaskID:         "",
		Round:          0,
		Kind:           "",
		Seq:            0,
		Event:          "",
		Target:         event.Address{},
		Source:         event.Address{},
		TargetInstance: "",
	}

	_ = ReplayOutcome{
		Delivery:  DeliveryRecord{},
		Owner:     "",
		Delivered: false,
		Skipped:   false,
		Refused:   nil,
	}

	_ = ReplayReport{
		Outcomes: []ReplayOutcome{},
	}

	_ DeliverySink = func(ctx context.Context, rec DeliveryRecord) error { return nil }

	_ OwnerLookup = func(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error) {
		return "", nil
	}

	_ error = ErrReplayUnwired

	_ func() *Replay                                                                                    = NewReplay
	_ func(*Replay, context.Context, []DeliveryRecord, OwnerLookup, DeliverySink) (ReplayReport, error) = (*Replay).Replay

	_ func(*Tier) *TierFacts                                              = TierFactLog
	_ func(TierFacts, context.Context, string, []byte) error              = TierFacts.Put
	_ func(TierFacts, context.Context, string) ([]byte, bool, error)      = TierFacts.Get
	_ func(TierFacts, context.Context, string) (map[string][]byte, error) = TierFacts.Scan
	_ func(TierFacts, context.Context) error                              = TierFacts.Sync
)

// The projection replay keeps its declared shapes: the chain-isolated replay
// source and relation store, the compaction payload and its refs, and the
// cold-start rebuild that replays a fact chain into an empty projection.
var (
	_ = ChainKey{PartitionID: 0, SessionID: ""}
	_ = ChainFact{Key: "", Chain: ChainKey{}, Type: "", Summary: "", Content: "", Time: time.Time{}}
	_ = CompactionFact{Summary: CompactionRef{}, Refs: []CompactionRef{}, FullBoundary: "", MeditationKeys: []event.EventID{}}
	_ = CompactionRef{Key: "", Type: "", Summary: "", Content: "", Time: time.Time{}}
	_ = ReplaySettle{ID: "", Entry: "", Mode: PresentationUnrecorded, Preview: "", At: time.Time{}}
	_ = RebuildOptions{PageSize: 0, FallbackBound: 0}
	_ = RebuildReport{Entries: 0, Fallback: false, Dropped: 0, Scanned: false, Skipped: "", Partial: false, Missing: []event.EventID{}, Meditation: []event.EventID{}, FullBoundary: ""}

	_ EventChain = (EventChain)(nil)
	_            = (*MemoryRelations)(nil)

	_ func(*MemoryRelations, event.EventID, event.EventID) error   = (*MemoryRelations).SetParent
	_ func(*MemoryRelations, event.EventID) (event.EventID, error) = (*MemoryRelations).GetParent

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
)

// The presentation decisions, the budget assembly and the retention leases
// keep their declared shapes: the layered decision record, the replayable
// projection a rebuild replays into, the registration and the declarative
// lifecycle policy.
var (
	_ Guard                  = (*MemoryGuard)(nil)
	_ func(Guard) *Retention = NewRetention

	_                        = RetentionRecord{Kind: "", Type: "", RecordedAt: time.Time{}}
	_                        = LifecycleConfig{GlobalTTLDays: 0, TypeTTL: map[string]int{}, CheckInterval: 0, MaxEventsPerPartition: 0}
	_ func() LifecycleConfig = DefaultLifecycleConfig

	_ func(*Retention, context.Context, event.EventID, HolderID, RetentionRecord) (ProtectionLease, error) = (*Retention).Register
	_ func(*Retention, context.Context, event.EventID, HolderID, ProtectionLease) error                    = (*Retention).Retire
	_ func(*Retention, context.Context, HolderID) (int, error)                                             = (*Retention).ReleaseHolder

	_ = RetentionUnconfirmed
	_ = RetentionRecovery
	_ = ProjectionTypeRecoveryHint
)

// The storage lifecycle keeps its declared shapes: the enumerated key, the
// declared capabilities, the coordination surface, the scan report and the
// lifecycle constructor that refuses a durable tier missing its capabilities.
var (
	_ = StoredKey{Key: "", Type: "", Partition: 0, RecordedAt: time.Time{}}
	_ = LifecycleCapabilities{DurableRecovery: false, Protection: false, Reclamation: false}
	_ = ScanReport{Tombstoned: []event.EventID{}, Protected: []event.EventID{}, Exempt: []event.EventID{}}

	_ LifecycleStore = (LifecycleStore)(nil)

	_ func(LifecycleConfig, *Retention, LifecycleStore, LifecycleCapabilities) (*Lifecycle, error) = NewLifecycle
	_ func(*Lifecycle, context.Context, StoredKey) error                                           = (*Lifecycle).RegisterRecovered
	_ func(*Lifecycle)                                                                             = (*Lifecycle).RecoveryComplete
	_ func(*Lifecycle, time.Time)                                                                  = (*Lifecycle).ArmGrace
	_ func(*Lifecycle, context.Context, time.Time) (ScanReport, error)                             = (*Lifecycle).Scan
	_ func(*Lifecycle, context.Context, event.EventID, time.Time) error                            = (*Lifecycle).Delete

	_ error = ErrCapabilityIncomplete
	_ error = ErrRecoveryIncomplete
	_ error = ErrEnumerationIncomplete
	_ error = ErrProtected
	_ error = ErrLifecycleUnwired
)

// The compress request family keeps its declared shapes: the judgment a producer
// files, the log that holds it, the consumer faces a curation tick reads and the
// product fact the tick commits.
var (
	_ = CompressRequest{ID: "", Producer: "", Reason: "", Subject: "", At: time.Time{}}
	_ = CurationTick{Read: []CompressRequest{}, Reports: []CurationReport{}, Products: []CurationArtifact{}, Folds: []CompactionFact{}, Failures: []error{}}

	_ CompressRequestFiling = func(context.Context, CompressRequest) (CompressRequest, error) { return CompressRequest{}, nil }
	_ CurationCards         = func(context.Context) ([]CurationCard, error) { return nil, nil }
	_ CurationSink          = func(context.Context, CurationArtifact) error { return nil }
	_ CurationProjection    = func(context.Context) (*ReplayProjection, error) { return nil, nil }
	_ CurationFolds         = func(context.Context, CompactionFact) error { return nil }
	_ CurationOption        = func(*Curation) {}

	_ ReplaySettle = ReplaySettle{ID: "", Entry: "", Mode: PresentationUnrecorded, Preview: "", At: time.Time{}}

	_ func(PresentationDecision) ReplaySettle                = ReplaySettleFor
	_ func(CurationProjection, CurationFolds) CurationOption = WithCurationPreFold

	_ func() *CompressRequests                                                           = NewCompressRequests
	_ func(*CompressRequests, context.Context, CompressRequest) (CompressRequest, error) = (*CompressRequests).Produce
	_ func(*CompressRequests, context.Context) ([]CompressRequest, error)                = (*CompressRequests).Pending
	_ func(*CompressRequests, context.Context, event.EventID) error                      = (*CompressRequests).Consume

	_ func(*Curation, context.Context, time.Time, []CurationCard) (CurationReport, error)        = (*Curation).Curate
	_ func(*Curation, context.Context, time.Time, []CurationCard) (CompressRequest, bool, error) = (*Curation).Judge
	_ func(*Curation, context.Context, time.Time) (CurationTick, error)                          = (*Curation).Tick

	_ error = ErrCompressRequestUnnamed
	_ error = ErrCompressRequestNotPending
	_ error = ErrCurationRequestsUnwired
	_ error = ErrCurationCardsUnwired
	_ error = ErrCurationProductsUnwired
	_ error = ErrPrefoldUnwired
	_ error = ErrCurationProjectionUnwired
	_ error = ErrCurationFoldsUnwired
)

// The archive original-text face keeps its documented shapes: the adapter over
// one original store beside the two methods the migration's original port
// matches structurally.
var (
	_ func(OriginalStore) *ArchiveOriginals = NewArchiveOriginals

	_ func(*ArchiveOriginals, context.Context, event.EventID, event.Address, string, string, string, int64, []byte) error = (*ArchiveOriginals).PutOriginal
	_ func(*ArchiveOriginals, context.Context, event.EventID) ([]byte, bool, error)                                       = (*ArchiveOriginals).GetOriginal
)
