package plugin

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// ObservationGap is the visible gap one failed post-commit observation leaves
// behind: the plugin that failed, the committed fact it failed to observe and
// the error it failed with. The gap is the record of the failure; the committed
// fact is untouched by it.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
type ObservationGap struct {
	// Plugin is the stable name of the plugin whose observation failed.
	Plugin string

	// Fact is the committed fact the observation failed on.
	Fact event.EventID

	// Err is the error the observation failed with.
	Err error
}

// RenderedView is the final presentation one presenter rendered for a fact: the
// rendered text beside whether the channel dropped it. A dropped view drops the
// presentation only; the committed fact stays.
type RenderedView struct {
	// Plugin is the stable name of the presenter that rendered the view.
	Plugin string

	// Text is the presentation text the presenter rendered.
	Text string

	// Dropped reports whether the channel dropped this presentation.
	Dropped bool
}

// Presentation is the After pass outcome of one committed fact: the views the
// final presenters rendered beside the visible gaps the failed observers left.
// It never carries an error and never withholds the fact: the committed fact is
// already durable when the pass runs, and no outcome of this pass may retract
// it.
type Presentation struct {
	// Fact is the committed fact the pass read.
	Fact event.EventID

	// Views holds the rendered presentations, in pipeline order.
	Views []RenderedView

	// Gaps holds the visible observation gaps, in pipeline order.
	Gaps []ObservationGap
}

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
func (p Pipeline) After(ctx context.Context, fact event.EventID) Presentation {
	out := Presentation{Fact: fact}
	for _, face := range p.observers {
		if err := face.impl.ObserveCommit(ctx, fact); err != nil {
			out.Gaps = append(out.Gaps, ObservationGap{Plugin: face.name, Fact: fact, Err: err})
		}
	}
	for _, face := range p.presenters {
		text, dropped := face.impl.Present(ctx, fact)
		out.Views = append(out.Views, RenderedView{Plugin: face.name, Text: text, Dropped: dropped})
	}
	return out
}
