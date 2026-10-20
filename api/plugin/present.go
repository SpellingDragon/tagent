package plugin

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/view"
)

// After runs the one consuming pass over one fact whose canonical commit already
// happened: every fact->observation face reads the fact first, then every
// fact->view face renders it. The pass runs after the commit and never before
// it, so a refused call and a call whose commit failed present nothing at all.
// An observation that fails is recorded as a named gap in the returned
// presentation and never rolls the committed fact back; a presenter that drops
// its text drops its own view only. The pass carries no error return: a failure
// here is a gap or a dropped view, never a reason to touch the committed fact.
//
// The fact enters the presentation pass once and every presenter renders it:
// each answers its own view, the views stand in the order the presenters were
// declared, and each view names the presenter that wrote it beside the fact it
// was rendered from, so a fact answers one view, several, or none when the
// pipeline carries no presenter at all — and every view it answers stays
// traceable to its source.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func (p Pipeline) After(ctx context.Context, fact event.EventID) view.Presentation {
	out := view.Presentation{Fact: fact}
	for _, face := range p.observers {
		started := time.Now()
		err := face.impl.ObserveCommit(ctx, fact)
		observePhase(PhaseConsume, faceObserve, Port{Reads: MaterialFact, Produces: ProductObservation}, face.name, phaseResult(err), "fact="+string(fact), started)
		if err != nil {
			out.Gaps = append(out.Gaps, view.ObservationGap{Plugin: face.name, Fact: fact, Err: err})
		}
	}
	for _, face := range p.presenters {
		started := time.Now()
		text, dropped := face.impl.Present(ctx, fact)
		observePhase(PhaseConsume, facePresent, Port{Reads: MaterialFact, Produces: ProductView}, face.name, phasePresentation(dropped), "fact="+string(fact), started)
		out.Views = append(out.Views, view.RenderedView{Plugin: face.name, Text: text, Dropped: dropped})
	}
	return out
}

// phasePresentation spells the outcome one presenting face reported: the text it
// rendered, or the drop it declared instead of a silent empty view.
func phasePresentation(dropped bool) string {
	if dropped {
		return "dropped"
	}
	return "rendered"
}
