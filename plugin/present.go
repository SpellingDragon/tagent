package plugin

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/view"
)

// After runs the After pass over one fact whose canonical commit already
// happened: every observer reads the fact first, then every presenter renders
// it. The pass runs after the commit and never before it, so a refused call and
// a call whose commit failed present nothing at all.
//
// An observation that fails is recorded as a named gap in the returned
// presentation and never rolls the committed fact back; a presenter that drops
// its text drops the presentation only. The pass carries no error return: a
// failure here is a gap or a dropped view, never a reason to touch the
// committed fact.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func (p Pipeline) After(ctx context.Context, fact event.EventID) view.Presentation {
	out := view.Presentation{Fact: fact}
	for _, face := range p.observers {
		if err := face.impl.ObserveCommit(ctx, fact); err != nil {
			out.Gaps = append(out.Gaps, view.ObservationGap{Plugin: face.name, Fact: fact, Err: err})
		}
	}
	for _, face := range p.presenters {
		text, dropped := face.impl.Present(ctx, fact)
		out.Views = append(out.Views, view.RenderedView{Plugin: face.name, Text: text, Dropped: dropped})
	}
	return out
}
