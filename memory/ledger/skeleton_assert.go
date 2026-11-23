// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the ledger shapes: every declared field
// name and type, every declared method signature, and every constant that must
// stay a compile-time constant. A rename or a widened signature stops this file
// from compiling, so the storage face cannot drift silently.
package ledger

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
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
)

// The declared ports must stay interface types.
var (
	_ OriginalStore       = (OriginalStore)(nil)
	_ ScopedOriginalStore = (ScopedOriginalStore)(nil)
)

// The recall outcomes and the finite segment bound must stay compile-time
// constants: a variable or a computed value stops this block from compiling.
const (
	_ = RecallUnclassified
	_ = RecallFound
	_ = RecallMissing
	_ = RecallExpired
	_ = RecallFailed

	_ = DefaultSegmentMaxBytes
)

// The durable fact surface one opened tier keeps its documented shapes: put,
// prefix scan and the sync barrier a commit reports its success behind.
var (
	_ func(*Tier) *TierFacts                                              = TierFactLog
	_ func(TierFacts, context.Context, string, []byte) error              = TierFacts.Put
	_ func(TierFacts, context.Context, string) (map[string][]byte, error) = TierFacts.Scan
	_ func(TierFacts, context.Context) error                              = TierFacts.Sync
)
