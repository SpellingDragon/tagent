// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the read face of the memory package: the
// delivery replay engine. The storage shapes are pinned beside their
// declarations in memory/ledger, the projection shapes in memory/replay and the
// meditation shapes in evolution/meditation; this file pins what this package
// itself declares, so a renamed field or a widened signature stops it from
// compiling.
package memory

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
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
