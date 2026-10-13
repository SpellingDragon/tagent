// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Package plugin declares the ordered per-phase pipeline through which tagent injects
// its differentiated capabilities into the framework event flow: request enrichment,
// risk policy, view selection, result externalization, post-commit observation and
// final presentation.
//
// A plugin is a registration unit: it reports the phase it serves and implements that
// phase's one narrow interface. The pre-execution faces form one Before pass a request
// runs exactly once; the post-commit faces form the After pass, which reads only facts
// whose canonical commit already happened.
package plugin

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/event"
)

// Phase names one stage of the single ordered plugin pipeline; the constant order is
// the pipeline order.
type Phase int

const (
	// PhaseRequestEnrich, PhaseRiskPolicy and PhaseViewSelection are the
	// pre-execution phases. Together they form the request's one Before pass,
	// which runs exactly once per request and is not run again when a fact is
	// replayed.
	PhaseRequestEnrich Phase = iota

	// PhaseRiskPolicy grades the risk of a tool call before its side effect and
	// advises the pre-execution ruling.
	PhaseRiskPolicy

	// PhaseViewSelection selects the view the request is assembled from, so the
	// model sees the selected view rather than the whole original.
	PhaseViewSelection

	// PhaseExternalizePolicy stores the original of a produced result, and a
	// reference to that original is published only after the original is
	// stored.
	PhaseExternalizePolicy

	// PhasePostCommitObserve observes a fact after its canonical commit, as a
	// read-only duty.
	PhasePostCommitObserve

	// PhaseFinalPresentation renders the final presentation of a fact for its
	// channel.
	PhaseFinalPresentation
)

// Plugin is a tagent registration unit, distinct from the framework's plugin.Plugin.
// It reports the stable name of one pipeline element and the single phase that element
// serves, and the element implements that phase's narrow interface.
//
// The composition root reads the phase of every registered Plugin and dispatches the
// element into that phase's narrow interface set — Enricher, RiskAssessor, ViewSelector,
// ExternalizePolicy, PostCommitObserver, FinalPresenter — in the declared order, so one
// plugin serves exactly one phase.
type Plugin interface {
	// Name returns the stable name of this plugin.
	Name() string

	// Phase reports the single phase this plugin serves, which both orders the
	// plugin inside the pipeline and selects the narrow interface it implements.
	Phase() Phase
}

// Enricher is the request-assembly face, and it is where the allowed parameter
// transform is carried: EnrichRequest returns the request enriched with this phase's
// prompts, skills and retrieved context, and it may transform the final request
// parameters. A transformed request is re-checked by the core for identity and
// permission before it runs, and the transformed request is the one both executed and
// recorded.
type Enricher interface {
	EnrichRequest(ctx context.Context, req *trpcmodel.Request) error
}

// RiskAssessor is the pre-side-effect risk face: AssessRisk grades one tool call before
// its side effect runs, from the parameters and identity already resolved for that
// call.
type RiskAssessor interface {
	AssessRisk(ctx context.Context, call *trpcmodel.ToolCall) RiskLevel
}

// ViewSelector is the view-selection face: SelectView names the view the request is
// assembled from, as projected decision-reference strings. The concrete reference type
// is settled in phase two (03.4); this face carries the reference strings so the plugin
// package keeps its single leaf edge to event.
type ViewSelector interface {
	SelectView(ctx context.Context, id event.EventID) ([]string, error)
}

// ExternalizePolicy is the result-externalization face: DecideExternalize reports
// whether a produced result exceeds the bound and must have its original stored before
// a reference to it is published, judged from the fact's identity and the result size.
type ExternalizePolicy interface {
	DecideExternalize(ctx context.Context, id event.EventID, size int) (bool, error)
}

// PostCommitObserver is the post-commit observation face: ObserveCommit observes one
// fact after its canonical commit, as a read-only duty. A failed observation is
// recorded as a visible gap and never rolls the committed fact back.
type PostCommitObserver interface {
	ObserveCommit(ctx context.Context, id event.EventID) error
}

// FinalPresenter is the final-presentation face: Present renders the presentation of
// one fact for its channel and reports the presentation text with whether it was
// dropped. Dropping a presentation drops the presentation only: the committed fact it
// was rendered from stays.
type FinalPresenter interface {
	Present(ctx context.Context, id event.EventID) (string, bool)
}
