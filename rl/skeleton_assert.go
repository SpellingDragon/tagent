package rl

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
//
// Skeleton compile-time assertions for the phase 1 record, grouping and capture
// declarations of this package.
//
// The phase 2 implementation type that satisfies Capture does not exist yet, so
// there is no `var _ Capture = (*capture)(nil)` to write. The assertions below pin
// what the skeleton proves on its own: every declared method keeps its exact
// shape, every documented record field keeps its name and type, and the states and
// bounds stay compile-time constants.
var (
	_ Capture = (Capture)(nil)

	_ func(Capture, context.Context, model.Request) (ModelViewRef, error)     = Capture.Snapshot
	_ func(Capture, context.Context, AttemptRecord) (CaptureAdmission, error) = Capture.Record
	_ func(Capture, context.Context) (CaptureManifest, error)                 = Capture.Seal
)

// Record, grouping and capture types, naming every documented field so a renamed
// field or a changed field type stops the package from compiling.
var (
	_ = EpisodeRecord{
		Schema:    "",
		ID:        "",
		ParentID:  "",
		RootID:    "",
		Ancestors: []event.EpisodeID{},
		Sessions:  []event.SessionID{},
		DependsOn: []EpisodeDependency{},
		Lineage:   LineageUnknown,
		Timestamp: time.Time{},
	}

	_ = DecisionRecord{
		Schema:       "",
		ID:           "",
		InvocationID: "",
		EpisodeID:    "",
		InputEvents:  []event.EventID{},
		ModelView:    ModelViewRef{},
		Timestamp:    time.Time{},
	}

	_ = AttemptRecord{
		Schema:         "",
		ID:             "",
		DecisionID:     "",
		EpisodeID:      "",
		Response:       model.Response{},
		Terminal:       model.KindUnknown,
		Logprobs:       LogprobsUnknown,
		LogprobsSource: "",
		Timestamp:      time.Time{},
	}

	_ = ModelViewRef{
		Model:            "",
		ModelVersion:     "",
		ParametersDigest: "",
		PromptVersion:    "",
		VisibleTools:     []string{},
		VisibleSkills:    []string{},
		ContextDigest:    "",
	}

	_ = EpisodeDependency{
		EpisodeID: "",
		DependsOn: "",
		Kind:      DependencyChildCall,
		Lineage:   LineageUnknown,
	}

	_ = EpisodeGroup{
		GroupID: "",
		RootID:  "",
		Members: []event.EpisodeID{},
		Basis:   []EpisodeDependency{},
		Lineage: LineageUnknown,
	}

	_ = CaptureAdmission{
		Admitted: false,
		Bound:    BoundNone,
		Reason:   "",
	}

	_ = CaptureManifest{
		CaptureID: "",
		Sealed:    false,
		Partial:   false,
		Records:   0,
		Dropped:   0,
		InFlight:  0,
		Reasons:   []string{},
	}
)

// Record versions, lineages, logprobs states, dependency kinds and admission
// bounds must stay compile-time constants: a variable or a computed value stops
// this block from compiling.
const (
	_ = LineageUnknown
	_ = LineageKnown

	_ = LogprobsUnknown
	_ = LogprobsPresent
	_ = LogprobsUnsupported

	_ = DependencyChildCall
	_ = DependencyHandoff
	_ = DependencySharedHistory

	_ = BoundNone
	_ = BoundRecordSize
	_ = BoundInFlight
	_ = BoundBytes
	_ = BoundClosed
)
