// 契约: docs/wiki/evolution/evolution-architecture.md#refine
//
// Skeleton compile-time assertions for the git-native refinement surface: the
// three operations of the channel, the ledger entry it answers with and the
// improvement its recorder takes. A renamed field or a widened signature stops
// this file from compiling.
package refine

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/evolution"
)

// The three operations of the refinement surface keep their declared shapes.
var (
	_ func(Refine, context.Context, []string, string) (RefineRecord, error) = Refine.Register

	_ func(Refine, context.Context) ([]RefineRecord, error) = Refine.Status

	_ func(Refine, context.Context, string) error = Refine.Rollback
)

// The refinement surface is an interface type and the git-native channel is one
// implementation of it, so neither a widened port nor an unimplemented operation
// compiles.
var (
	_ Refine = (Refine)(nil)

	_ Refine = (*GitRefine)(nil)
)

// The ledger entry and the improvement a registration hands its recorder keep
// every documented field.
var (
	_ = RefineRecord{
		Paths:       []string{},
		Note:        "",
		SHA:         "",
		CommittedAt: time.Time{},
		GateBasis:   evolution.GateReport{},
		Window:      evolution.EvidenceWindow{},
		Posterior:   evolution.PosteriorReport{},
	}

	_ = Improvement{
		SHA:   "",
		Paths: []string{},
		Note:  "",
	}

	_ ImprovementRecorder = func(ctx context.Context, improvement Improvement) error { return nil }
)

// The improvement marker stays a compile-time constant of its declared type.
const _ string = ImprovementMarker
