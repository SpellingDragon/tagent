// 契约: docs/wiki/memory/memory-architecture.md#overview
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Skeleton compile-time assertions for the meditation shapes: the consumer faces a
// tick reads, the engine's pass, judgment and tick signatures, the pre-fold seam and
// the refusals each unwired face answers by name. The reflection loop and the pass it
// reads keep their own declared surface here as well: the loop's assembly and tick
// operations, the observation face it reads, the digest it renders and the execution
// watermark it advances only on a consumed turn. One file holds the one shape pin of
// the package: a renamed field or a widened signature stops it from compiling.
package meditation

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// The pass, the judgment and the tick keep their declared shapes.
var (
	_ = MeditationCard{Key: "", Type: "", Summary: "", RecordedAt: time.Time{}, Links: 0}
	_ = MeditationValue{Key: "", Score: 0}
	_ = MeditationPolicy{MaxCards: 0, Keep: 0, MinScore: 0}
	_ = MeditationArtifact{Key: "", Type: "", Summary: "", Refs: []event.EventID{}, Sunk: 0, CreatedAt: time.Time{}}
	_ = MeditationRun{Overflow: []MeditationCard{}, Keep: []MeditationCard{}}
	_ = MeditationReport{
		Triggered: false,
		Fates:     map[event.EventID]MeditationFate{},
		Artifact:  nil,
		Evicted:   []event.EventID{},
		Failures:  []error{},
	}
	_ = MeditationTick{
		Read:     []event.CompressRequest{},
		Reports:  []MeditationReport{},
		Products: []MeditationArtifact{},
		Folds:    []replay.CompactionFact{},
		Failures: []error{},
	}
)

// The fates must stay compile-time constants: a variable or a computed value
// stops this block from compiling.
const (
	_ = MeditationRetain
	_ = MeditationEvict
	_ = MeditationProtect

	_ = RetentionMeditation
	_ = MeditationArtifactType
	_ = MeditationHolderPrefix
)

// The consumer faces keep their declared signatures.
var (
	_ Condenser            = func(context.Context, MeditationRun, time.Time) (string, error) { return "", nil }
	_ MeditationCards      = func(context.Context) ([]MeditationCard, error) { return nil, nil }
	_ MeditationSink       = func(context.Context, MeditationArtifact) error { return nil }
	_ MeditationProjection = func(context.Context) (*replay.ReplayProjection, error) { return nil, nil }
	_ MeditationFolds      = func(context.Context, replay.CompactionFact) error { return nil }
	_ MeditationOption     = func(*Meditation) {}
	_ CompressPhase        = (CompressPhase)(nil)

	_ func(*memory.CompressRequests) MeditationOption              = WithCompressRequests
	_ func(MeditationCards) MeditationOption                       = WithMeditationCards
	_ func(MeditationSink) MeditationOption                        = WithMeditationProducts
	_ func(CompressPhase) MeditationOption                         = WithCompressPhase
	_ func(MeditationProjection, MeditationFolds) MeditationOption = WithMeditationPreFold

	_ func(CompressPhase, context.Context, event.CompressRequest) error = CompressPhase.TriggerCompress

	_ func(MeditationPolicy, *ledger.Retention, *ledger.Lifecycle, Condenser, ...MeditationOption) (*Meditation, error) = NewMeditation

	_ func(*Meditation, context.Context, time.Time, []MeditationCard) (MeditationReport, error)            = (*Meditation).Curate
	_ func(*Meditation, context.Context, time.Time, []MeditationCard) (event.CompressRequest, bool, error) = (*Meditation).Judge
	_ func(*Meditation, context.Context, time.Time) (MeditationTick, error)                                = (*Meditation).Tick
)

// The pre-fold face keeps its declared shapes and the refusals each unwired
// seam answers by name.
var (
	_ func(context.Context, *replay.ReplayProjection) (replay.CompactionFact, error)    = Prefold
	_ func(replay.CompactionFact) event.EventID                                         = CompactionFactKey
	_ func(replay.ChainKey, replay.CompactionFact, time.Time) (replay.ChainFact, error) = CompactionChainFact

	_ error = ErrMeditationUnwired
	_ error = ErrMeditationPolicy
	_ error = ErrMeditationCardKey
	_ error = ErrMeditationRequestsUnwired
	_ error = ErrMeditationCardsUnwired
	_ error = ErrMeditationProductsUnwired
	_ error = ErrPrefoldUnwired
	_ error = ErrMeditationProjectionUnwired
	_ error = ErrMeditationFoldsUnwired
)

// The reflection loop keeps its declared operations and its refusal names.
var (
	_ func(ReflectionConfig, WatermarkGate, ObservedFacts, ReflectionInput) (*Reflection, error) = NewReflection

	_ func(*Reflection, context.Context, time.Time) TickOutcome   = (*Reflection).Tick
	_ func(*Reflection, context.Context, ReflectionReceipt) error = (*Reflection).NoteOutcome
	_ func(*Reflection) []string                                  = (*Reflection).ObservedNamespaces
	_ func(*Reflection) ReflectionScope                           = (*Reflection).Scope
	_ func(*Reflection, context.Context, time.Time) (bool, error) = (*Reflection).FreshSince
	_ func(*Reflection) (event.EventID, bool)                     = (*Reflection).Pending
	_ func(*Reflection) int                                       = (*Reflection).DeferredCount
	_ func(*Reflection) time.Time                                 = (*Reflection).LastDeferredAt

	_ func(ReflectionInput, context.Context, event.Event) (ReflectionReceipt, error) = ReflectionInput.InjectReflection

	_ error = ErrReflectionAgentUnnamed
	_ error = ErrReflectionInputUnkeyed
	_ error = ErrReflectionNotPending
	_ error = ErrReflectionKeyMismatch
	_ error = ErrObservationScopeOutOfBounds
)

// The loop's config, receipt and tick records keep every documented field.
var (
	_ = ReflectionConfig{
		Loop:    ReflectionLoop{Agent: "", Session: ""},
		Face:    ReflectionFace{ObservedNamespaces: []string{}, ReadNamespaces: []string{}, PageLimit: 0, FactLimit: 0},
		Cadence: ReflectionCadence{Interval: 0, MinGap: 0},
		Prompt:  "",
	}

	_ = ReflectionReceipt{Key: "", Outcome: ReflectionConsumed, At: time.Time{}, Reason: ""}

	_ = ReflectionInjection{Key: "", At: time.Time{}, Event: event.Event{}, Digest: DigestSummary{}}

	_ = TickDecision{Disposition: TickFired, Reason: "", Cause: nil}

	_ = TickOutcome{Decision: TickDecision{}, Scan: nil, Digest: DigestSummary{}, Injection: nil}
)

// The two dispositions stay disjoint compile-time vocabularies.
const (
	_ TickDisposition = TickFired
	_ TickDisposition = TickPending
	_ TickDisposition = TickClosed
	_ TickDisposition = TickNoNovelty
	_ TickDisposition = TickRhythm

	_ ReflectionOutcome = ReflectionConsumed
	_ ReflectionOutcome = ReflectionDeferred
	_ ReflectionOutcome = ReflectionInFlight
)

// The observation face, the digest and the watermark keep their declared shapes.
var (
	_ func(string, []string, []string) (ReflectionScope, error) = ResolveObservationScope

	_ = ReflectionScope{Own: "", Names: []string{}, Partitions: []int{}}

	_ func(ObservedFacts, context.Context, memory.QueryOptions) ([]memory.EventReference, error) = ObservedFacts.QueryEvents

	_ func(ObservedFacts, context.Context, event.EventID) (event.Event, error) = ObservedFacts.CommittedFact

	_ func(string) bool = DeliverableLineage
	_ func(string) bool = SelfManagedLineage

	_ = PartitionActivity{Name: "", Partition: 0, References: 0, External: 0, SelfManaged: 0}

	_ = FactIdentity{Key: "", Partition: ""}

	_ = FactContent{Type: "", Summary: "", Timestamp: time.Time{}}

	_ = FactAttribution{Lineage: "", SelfManaged: false}

	_ = ScannedFact{Identity: FactIdentity{}, Content: FactContent{}, Attribution: FactAttribution{}}

	_ = ScanCounts{ReferenceTotal: 0, HydratedTotal: 0, Unreadable: 0, UnknownLineage: 0, Foreign: 0}

	_ = ScanVerdict{Novelty: false, Recent: nil}

	_ = ObservationScan{
		Watermark:  time.Time{},
		Partitions: []PartitionActivity{},
		Facts:      []ScannedFact{},
		Counts:     ScanCounts{},
		Verdict:    ScanVerdict{},
	}

	_ = ObservationScanRequest{Since: time.Time{}, PageLimit: 0, FactLimit: 0}

	_ Digest = (*ScanDigest)(nil)

	_ func(*ObservationScan, int) *ScanDigest = NewScanDigest

	_ func(*ScanDigest, context.Context, Watermark) (DigestSummary, error) = (*ScanDigest).Render

	_ = DigestSummary{Body: "", Omitted: 0}

	_ error = ErrObservedFaceUnwired
	_ error = ErrDigestFaceUnavailable
	_ error = ErrDigestScanMismatch
)

// The watermark gate keeps its two operations, and the anchor store is the gate
// the composition root wires.
var (
	_ WatermarkGate = (*AnchorStore)(nil)

	_ = Watermark{Moment: time.Time{}}

	_ = Anchors{LastTurnEnd: time.Time{}, LastMeditation: time.Time{}}

	_ func(string) (*AnchorStore, error) = NewAnchorStore

	_ func(*AnchorStore) string = (*AnchorStore).Path

	_ func(*AnchorStore) (Anchors, error) = (*AnchorStore).Load

	_ func(*AnchorStore, Anchors) error = (*AnchorStore).Save

	_ error = ErrAnchorStoreUnusable
	_ error = ErrAnchorUnreadable
	_ error = ErrWatermarkSourceUnwired
)
