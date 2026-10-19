// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the meditation shapes: the consumer faces
// a tick reads, the engine's pass, judgment and tick signatures, the pre-fold
// seam and the refusals each unwired face answers by name. A renamed field or a
// widened signature stops this file from compiling.
package meditation

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
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
)

// The consumer faces keep their declared signatures.
var (
	_ Condenser            = func(context.Context, MeditationRun, time.Time) (string, error) { return "", nil }
	_ MeditationCards      = func(context.Context) ([]MeditationCard, error) { return nil, nil }
	_ MeditationSink       = func(context.Context, MeditationArtifact) error { return nil }
	_ MeditationProjection = func(context.Context) (*replay.ReplayProjection, error) { return nil, nil }
	_ MeditationFolds      = func(context.Context, replay.CompactionFact) error { return nil }
	_ MeditationOption     = func(*Meditation) {}

	_ func(*memory.CompressRequests) MeditationOption              = WithCompressRequests
	_ func(MeditationCards) MeditationOption                       = WithMeditationCards
	_ func(MeditationSink) MeditationOption                        = WithMeditationProducts
	_ func(MeditationProjection, MeditationFolds) MeditationOption = WithMeditationPreFold

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
