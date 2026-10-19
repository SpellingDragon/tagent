// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the read face of the memory package: the
// delivery replay engine, the compress request family and the archive
// original-text adapter. The storage shapes are pinned beside their declarations
// in memory/ledger, the projection shapes in memory/replay and the meditation
// shapes in evolution/meditation; this file pins what this package itself
// declares, so a renamed field or a widened signature stops it from compiling.
package memory

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// The unconfirmed-delivery replay face keeps its documented shapes: the
// built-in delivery record, the owner-resolution and sink seams, the replay
// engine's entry and its report.
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
)

// The compress request filing surface keeps its declared shapes: the mechanism's
// producer and reason, the filing seam and the log that holds what is filed. The
// fact itself is pinned beside its declaration in the event package.
var (
	_ CompressRequestFiling = func(context.Context, event.CompressRequest) (event.CompressRequest, error) {
		return event.CompressRequest{}, nil
	}

	_ func() *CompressRequests                                                                       = NewCompressRequests
	_ func(*CompressRequests, context.Context, event.CompressRequest) (event.CompressRequest, error) = (*CompressRequests).Produce
	_ func(*CompressRequests, context.Context) ([]event.CompressRequest, error)                      = (*CompressRequests).Pending
	_ func(*CompressRequests, context.Context, event.EventID) error                                  = (*CompressRequests).Consume

	_ error = ErrCompressRequestUnnamed
	_ error = ErrCompressRequestNotPending
)

// The archive original-text face keeps its documented shapes: the adapter over
// one original store beside the two methods the migration's original port
// matches structurally.
var (
	_ func(ledger.OriginalStore) *ArchiveOriginals = NewArchiveOriginals

	_ func(*ArchiveOriginals, context.Context, event.EventID, event.Address, string, string, string, int64, []byte) error = (*ArchiveOriginals).PutOriginal
	_ func(*ArchiveOriginals, context.Context, event.EventID) ([]byte, bool, error)                                       = (*ArchiveOriginals).GetOriginal
)
