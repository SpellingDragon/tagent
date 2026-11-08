// 契约: docs/wiki/memory/memory-architecture.md#overview
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Skeleton compile-time assertions for the meditation shapes: the reflection loop and
// the pass it reads keep their own declared surface here: the loop's assembly and tick
// operations, the observation face it reads, the digest it renders and the execution
// watermark it advances only on a consumed turn. One file holds the one shape pin of
// the package: a renamed field or a widened signature stops it from compiling.
package meditation

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
)

// The domain's judgments keep their declared shapes: one event in and one verdict
// out, beside the recording readings of the same trigger-source stamp.
var (
	_ func(NoveltyPolicy, event.Event) bool  = NoveltyPolicy.Novel
	_ func(DeliveryPolicy, event.Event) bool = DeliveryPolicy.Deliverable

	_ func(string) bool = DeliverableLineage
	_ func(string) bool = SelfManagedLineage
)

// The reflection loop keeps its declared operations and its refusal names.
var (
	_ func(ReflectionConfig, WatermarkGate, ObservedFacts, ReflectionInput) (*Reflection, error) = NewReflection

	_ func(*Reflection, context.Context, time.Time) TickOutcome              = (*Reflection).Tick
	_ func(*Reflection, context.Context, ReflectionReceipt) error            = (*Reflection).NoteOutcome
	_ func(*Reflection) []string                                             = (*Reflection).ObservedNamespaces
	_ func(*Reflection) ReflectionScope                                      = (*Reflection).Scope
	_ func(*Reflection, context.Context, time.Time) (bool, error)            = (*Reflection).FreshSince
	_ func(*Reflection) (event.EventID, bool)                                = (*Reflection).Pending
	_ func(*Reflection) int                                                  = (*Reflection).DeferredCount
	_ func(*Reflection) time.Time                                            = (*Reflection).LastDeferredAt
	_ func(*Reflection, context.Context, event.EventID) (event.Event, error) = (*Reflection).CommittedFact
	_ func(*Reflection, context.Context) (Watermark, error)                  = (*Reflection).Watermark

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
		Loop:     ReflectionLoop{Agent: "", Session: ""},
		Face:     ReflectionFace{ObservedNamespaces: []string{}, ReadNamespaces: []string{}, PageLimit: 0, FactLimit: 0},
		Cadence:  ReflectionCadence{Interval: 0, MinGap: 0},
		Prompt:   "",
		Delivery: ReflectionDelivery{Targets: []string{}, Sessions: nil},
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

// The event-driven face keeps its declared shapes: one commit identity in, the
// reflection pass it may fire out, beside the stop that ends the driver.
var (
	_ func(*Reflection, time.Duration, time.Duration, func(event.Address, event.InstanceID) (event.InvocationID, event.DecisionID, bool), event.InstanceID) *Driver = NewDriver

	_ func(*Driver) string                                = (*Driver).Name
	_ func(*Driver) plugin.Port                           = (*Driver).Port
	_ func(*Driver, context.Context, event.EventID) error = (*Driver).ObserveCommit
	_ func(*Driver)                                       = (*Driver).Stop

	_ plugin.Instance           = (*Driver)(nil)
	_ plugin.PostCommitObserver = (*Driver)(nil)
)

// The live-binding face a delivery target resolves against keeps its declared shape:
// the complete addresses of the sessions one agent is bound to.
var (
	_ func(SessionQuery, event.AgentID) []event.Address = SessionQuery.ActiveSessions

	_ = ReflectionDelivery{Targets: []string{}, Sessions: nil}
)

// The mount keeps its declared surface: one plan a document resolves to, the
// verdicts and instances it answers, and the run faces it is assembled over.
var (
	_ func(document.Document, string, string) (*Plan, error) = Resolve

	_ func(*Plan, func(string, string) bool, string) error   = (*Plan).ValidateDelivery
	_ func(*Plan, string) bool                               = (*Plan).GrantsSend
	_ func(*Plan, Faces, func(TickOutcome)) (*Driver, error) = (*Plan).Instances

	_ = Plan{
		Agent: "", Session: "", SelfReflection: false, Interval: time.Duration(0), MinGap: time.Duration(0),
		Prompt: "", Observed: []string{}, Reads: []string{}, Targets: []string{}, Anchors: "",
	}

	_ = Faces{
		Gate: nil, Observed: nil, Input: nil, ModelInFlight: nil, Instance: event.InstanceID(""), Sessions: nil,
	}

	_ time.Duration = DefaultInterval
	_ error         = ErrToolMisplaced
)
