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
)

// The exported port methods must keep exactly these signatures.
var (
	_ func(OriginalStore, context.Context, OriginalRef, []byte) error = OriginalStore.Put

	_ func(OriginalStore, context.Context, event.EventID) RecallResult = OriginalStore.Get

	_ func(OriginalStore, context.Context, event.EventID, int, int) ([]byte, error) = OriginalStore.ReadSegment

	_ func(Projection, context.Context, ProjectionEntry) = Projection.Append

	_ func(Projection, context.Context) (ProjectionEntry, error) = Projection.Fold

	_ func(Projection, context.Context, []PresentationDecision) ([]ProjectionEntry, error) = Projection.Rebuild

	_ func(Projection, context.Context, int) ([]ProjectionEntry, error) = Projection.AssembleBudget

	_ func(Guard, context.Context, OriginalRef) error = Guard.Hold

	_ func(Guard, context.Context, OriginalRef) error = Guard.Release

	_ func(Guard, context.Context, OriginalRef) (bool, error) = Guard.Protected
)

// The declared ports must stay interface types.
var (
	_ OriginalStore = (OriginalStore)(nil)
	_ Projection    = (Projection)(nil)
	_ Guard         = (Guard)(nil)
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
)
